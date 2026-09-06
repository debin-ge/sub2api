package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

type VideoTaskAdmission interface {
	CheckVideoAdmission(context.Context, *APIKey, *Group, string, string) error
	InvalidateVideoHold(context.Context, int64) error
}

type VideoIdempotentRPMCache interface {
	IncrementUserGroupRPMOnce(context.Context, int64, int64, string) (int, error)
	IncrementUserRPMOnce(context.Context, int64, string) (int, error)
}

func (s *BillingCacheService) CheckVideoAdmission(ctx context.Context, key *APIKey, group *Group, platform, operationToken string) error {
	if s == nil || s.cfg == nil || key == nil || key.User == nil || group == nil || key.User.ID != key.UserID || strings.TrimSpace(platform) == "" || strings.TrimSpace(operationToken) == "" {
		return ErrBillingServiceUnavailable
	}
	if !key.User.IsActive() || (key.Status != StatusActive && key.Status != StatusAPIKeyQuotaExhausted) {
		return ErrVideoInvalidRequest
	}
	if key.IsExpired() {
		return ErrAPIKeyExpired
	}
	if group.IsSubscriptionType() {
		return ErrVideoSubscriptionUnsupported
	}
	if key.Status == StatusAPIKeyQuotaExhausted || key.IsQuotaExhausted() {
		return ErrAPIKeyQuotaExhausted
	}
	if s.cfg.RunMode == config.RunModeSimple {
		return nil
	}
	if s.userRepo == nil || s.userPlatformQuotaRepo == nil {
		return ErrBillingServiceUnavailable
	}
	if s.circuitBreaker != nil && !s.circuitBreaker.Allow() {
		return ErrBillingServiceUnavailable
	}
	if err := s.checkVideoBudgetProjection(ctx, key.UserID, key, platform); err != nil {
		return err
	}
	if err := s.checkBalanceEligibility(ctx, key.UserID); err != nil {
		return err
	}
	if err := s.checkVideoPlatformQuota(ctx, key.UserID, platform); err != nil {
		return err
	}
	if key.HasRateLimits() {
		if s.apiKeyRateLimitLoader == nil {
			return ErrBillingServiceUnavailable
		}
		data, err := s.apiKeyRateLimitLoader.GetRateLimitData(ctx, key.ID)
		if err != nil || data == nil {
			return ErrBillingServiceUnavailable
		}
		if err := s.evaluateRateLimits(ctx, key, data.Usage5h, data.Usage1d, data.Usage7d, data.Window5hStart, data.Window1dStart, data.Window7dStart); err != nil {
			return err
		}
	}
	return s.checkVideoAdmissionRPM(ctx, key, group, operationToken)
}

func (s *BillingCacheService) checkVideoAdmissionRPM(ctx context.Context, key *APIKey, group *Group, operationToken string) error {
	if s == nil || key == nil || key.User == nil || group == nil || key.User.ID != key.UserID || operationToken == "" {
		return ErrBillingServiceUnavailable
	}
	groupLimit := group.RPMLimit
	override := key.User.UserGroupRPMOverride
	if override == nil && s.userGroupRateRepo != nil {
		var err error
		override, err = s.userGroupRateRepo.GetRPMOverrideByUserAndGroup(ctx, key.UserID, group.ID)
		if err != nil {
			return ErrBillingServiceUnavailable.WithCause(err)
		}
	}
	if override != nil {
		groupLimit = *override
	}
	if groupLimit <= 0 && key.User.RPMLimit <= 0 {
		return nil
	}
	rpm, ok := s.userRPMCache.(VideoIdempotentRPMCache)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	if groupLimit > 0 {
		count, err := rpm.IncrementUserGroupRPMOnce(ctx, key.UserID, group.ID, operationToken)
		if err != nil {
			return ErrBillingServiceUnavailable.WithCause(err)
		}
		if count > groupLimit {
			return ErrGroupRPMExceeded
		}
	}
	if key.User.RPMLimit > 0 {
		count, err := rpm.IncrementUserRPMOnce(ctx, key.UserID, operationToken)
		if err != nil {
			return ErrBillingServiceUnavailable.WithCause(err)
		}
		if count > key.User.RPMLimit {
			return ErrUserRPMExceeded
		}
	}
	return nil
}

func (s *BillingCacheService) checkVideoPlatformQuota(ctx context.Context, userID int64, platform string) error {
	record, err := s.userPlatformQuotaRepo.GetByUserPlatform(ctx, userID, platform)
	if err != nil {
		return ErrBillingServiceUnavailable.WithCause(err)
	}
	if record == nil {
		return nil
	}
	now := time.Now()
	daily, weekly, monthly := record.DailyUsageUSD, record.WeeklyUsageUSD, record.MonthlyUsageUSD
	if quotaWindowExpired(record.DailyWindowStart, timezone.StartOfDay(now)) {
		daily = 0
	}
	if quotaWindowExpired(record.WeeklyWindowStart, timezone.StartOfWeek(now)) {
		weekly = 0
	}
	if monthlyQuotaWindowExpired(record.MonthlyWindowStart, now) {
		monthly = 0
	}
	if record.DailyLimitUSD != nil && daily >= *record.DailyLimitUSD {
		return withWindowResetsMetadata(ErrUserPlatformDailyQuotaExhausted, nextDailyReset(now))
	}
	if record.WeeklyLimitUSD != nil && weekly >= *record.WeeklyLimitUSD {
		return withWindowResetsMetadata(ErrUserPlatformWeeklyQuotaExhausted, nextWeeklyReset(now))
	}
	if record.MonthlyLimitUSD != nil && monthly >= *record.MonthlyLimitUSD {
		return withWindowResetsMetadata(ErrUserPlatformMonthlyQuotaExhausted, nextMonthlyResetFrom(record.MonthlyWindowStart, now))
	}
	return nil
}

func (s *BillingCacheService) InvalidateVideoHold(ctx context.Context, userID int64) error {
	if s == nil {
		return ErrBillingServiceUnavailable
	}
	return s.InvalidateUserBalance(ctx, userID)
}
