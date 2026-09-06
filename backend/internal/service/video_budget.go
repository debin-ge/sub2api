package service

import (
	"context"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

type VideoBudgetSnapshot struct {
	APIKey           *APIKey
	Platform         *UserPlatformQuotaRecord
	KeyReserved      float64
	PlatformReserved float64
}

type VideoBudgetSnapshotLoader interface {
	GetVideoBudgetSnapshot(context.Context, int64, int64, string) (*VideoBudgetSnapshot, error)
}

func (s *BillingCacheService) checkVideoBudgetProjection(ctx context.Context, userID int64, key *APIKey, platform string) error {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.Video.Enabled || key == nil {
		return nil
	}
	loader, ok := s.apiKeyRateLimitLoader.(VideoBudgetSnapshotLoader)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	snapshot, err := loader.GetVideoBudgetSnapshot(ctx, userID, key.ID, platform)
	if err != nil {
		return ErrBillingServiceUnavailable.WithCause(err)
	}
	return evaluateVideoBudgetSnapshot(snapshot, userID, key, time.Now())
}

func evaluateVideoBudgetSnapshot(snapshot *VideoBudgetSnapshot, userID int64, requestedKey *APIKey, now time.Time) error {
	if snapshot == nil || snapshot.APIKey == nil || requestedKey == nil {
		return ErrBillingServiceUnavailable
	}
	key := snapshot.APIKey
	if key.ID != requestedKey.ID || key.UserID != userID ||
		(key.Status != StatusActive && key.Status != StatusAPIKeyQuotaExhausted) {
		return ErrVideoInvalidRequest
	}
	if key.ExpiresAt != nil && !now.Before(*key.ExpiresAt) {
		return ErrAPIKeyExpired
	}
	for _, value := range []float64{snapshot.KeyReserved, snapshot.PlatformReserved, key.Quota, key.QuotaUsed,
		key.RateLimit5h, key.RateLimit1d, key.RateLimit7d, key.Usage5h, key.Usage1d, key.Usage7d} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return ErrBillingServiceUnavailable
		}
	}
	if key.Status == StatusAPIKeyQuotaExhausted || (key.Quota > 0 && key.QuotaUsed+snapshot.KeyReserved >= key.Quota) {
		return ErrAPIKeyQuotaExhausted
	}
	for _, window := range []struct {
		limit, spent float64
		start        *time.Time
		duration     time.Duration
		exceeded     error
	}{
		{key.RateLimit5h, key.Usage5h, key.Window5hStart, RateLimitWindow5h, ErrAPIKeyRateLimit5hExceeded},
		{key.RateLimit1d, key.Usage1d, key.Window1dStart, RateLimitWindow1d, ErrAPIKeyRateLimit1dExceeded},
		{key.RateLimit7d, key.Usage7d, key.Window7dStart, RateLimitWindow7d, ErrAPIKeyRateLimit7dExceeded},
	} {
		if window.start == nil || !now.Before(window.start.Add(window.duration)) {
			window.spent = 0
		}
		if window.limit > 0 && window.spent+snapshot.KeyReserved >= window.limit {
			return window.exceeded
		}
	}
	quota := snapshot.Platform
	if quota == nil {
		return nil
	}
	daily, weekly, monthly := quota.DailyUsageUSD, quota.WeeklyUsageUSD, quota.MonthlyUsageUSD
	if quotaWindowExpired(quota.DailyWindowStart, timezone.StartOfDay(now)) {
		daily = 0
	}
	if quotaWindowExpired(quota.WeeklyWindowStart, timezone.StartOfWeek(now)) {
		weekly = 0
	}
	if monthlyQuotaWindowExpired(quota.MonthlyWindowStart, now) {
		monthly = 0
	}
	for _, window := range []struct {
		limit    *float64
		spent    float64
		exceeded error
		reset    time.Time
	}{
		{quota.DailyLimitUSD, daily, ErrUserPlatformDailyQuotaExhausted, nextDailyReset(now)},
		{quota.WeeklyLimitUSD, weekly, ErrUserPlatformWeeklyQuotaExhausted, nextWeeklyReset(now)},
		{quota.MonthlyLimitUSD, monthly, ErrUserPlatformMonthlyQuotaExhausted, nextMonthlyResetFrom(quota.MonthlyWindowStart, now)},
	} {
		if math.IsNaN(window.spent) || math.IsInf(window.spent, 0) || window.spent < 0 ||
			(window.limit != nil && (math.IsNaN(*window.limit) || math.IsInf(*window.limit, 0) || *window.limit < 0)) {
			return ErrBillingServiceUnavailable
		}
		if window.limit != nil && window.spent+snapshot.PlatformReserved >= *window.limit {
			return withWindowResetsMetadata(window.exceeded, window.reset)
		}
	}
	return nil
}
