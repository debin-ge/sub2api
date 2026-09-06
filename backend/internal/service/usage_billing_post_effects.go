package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

type usageBillingAuthCacheInvalidator interface {
	InvalidateAuthCacheByUserID(ctx context.Context, userID int64)
}

// UsageBillingPostEffectsService projects a committed billing fact into
// caches and ancillary services. Every correctness-sensitive cache operation
// is an invalidation, so replay after an ambiguous acknowledgement is safe.
type UsageBillingPostEffectsService struct {
	billingCacheService   *BillingCacheService
	deferredService       *DeferredService
	userRepo              UserRepository
	accountRepo           AccountRepository
	userPlatformQuotaRepo UserPlatformQuotaRepository
	balanceNotifyService  *BalanceNotifyService
	authCacheInvalidator  usageBillingAuthCacheInvalidator
	cfg                   *config.Config
}

func NewUsageBillingPostEffectsService(
	billingCacheService *BillingCacheService,
	deferredService *DeferredService,
	userRepo UserRepository,
	accountRepo AccountRepository,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
	balanceNotifyService *BalanceNotifyService,
	apiKeyService *APIKeyService,
	cfg *config.Config,
) *UsageBillingPostEffectsService {
	return &UsageBillingPostEffectsService{
		billingCacheService:   billingCacheService,
		deferredService:       deferredService,
		userRepo:              userRepo,
		accountRepo:           accountRepo,
		userPlatformQuotaRepo: userPlatformQuotaRepo,
		balanceNotifyService:  balanceNotifyService,
		authCacheInvalidator:  apiKeyService,
		cfg:                   cfg,
	}
}

func usageBillingPlatformQuotaSnapshotFromCache(entry *UserPlatformQuotaCacheEntry) *UsageBillingPlatformQuotaSnapshot {
	if entry == nil {
		return nil
	}
	return &UsageBillingPlatformQuotaSnapshot{
		DailyUsageUSD:      entry.DailyUsageUSD,
		WeeklyUsageUSD:     entry.WeeklyUsageUSD,
		MonthlyUsageUSD:    entry.MonthlyUsageUSD,
		DailyWindowStart:   cloneTimePtr(entry.DailyWindowStart),
		WeeklyWindowStart:  cloneTimePtr(entry.WeeklyWindowStart),
		MonthlyWindowStart: cloneTimePtr(entry.MonthlyWindowStart),
	}
}

func usageBillingPlatformQuotaSnapshotFromRecord(rec *UserPlatformQuotaRecord) *UsageBillingPlatformQuotaSnapshot {
	if rec == nil {
		return nil
	}
	return &UsageBillingPlatformQuotaSnapshot{
		DailyUsageUSD:      rec.DailyUsageUSD,
		WeeklyUsageUSD:     rec.WeeklyUsageUSD,
		MonthlyUsageUSD:    rec.MonthlyUsageUSD,
		DailyWindowStart:   cloneTimePtr(rec.DailyWindowStart),
		WeeklyWindowStart:  cloneTimePtr(rec.WeeklyWindowStart),
		MonthlyWindowStart: cloneTimePtr(rec.MonthlyWindowStart),
	}
}

func userPlatformQuotaCacheEntryHasLimit(entry *UserPlatformQuotaCacheEntry) bool {
	return entry != nil &&
		(entry.DailyLimitUSD != nil || entry.WeeklyLimitUSD != nil || entry.MonthlyLimitUSD != nil)
}

func userPlatformQuotaRecordHasLimit(rec *UserPlatformQuotaRecord) bool {
	return rec != nil &&
		(rec.DailyLimitUSD != nil || rec.WeeklyLimitUSD != nil || rec.MonthlyLimitUSD != nil)
}

// captureUsageBillingPlatformQuotaSnapshot seals the current Redis-authoritative
// quota value into the billing intent. When the optional absolute-value flusher
// is enabled, a Redis read error cannot safely fall back to a potentially stale
// DB mirror; the intent is retained with SnapshotNeeded and the worker retries.
func captureUsageBillingPlatformQuotaSnapshot(
	ctx context.Context,
	userID int64,
	platform string,
	deps *billingDeps,
) (snapshot *UsageBillingPlatformQuotaSnapshot, snapshotNeeded bool, track bool) {
	platform = strings.TrimSpace(platform)
	if deps == nil || isNilInterfaceValue(deps.userPlatformQuotaRepo) || platform == "" {
		return nil, false, false
	}
	if deps.cfg != nil && deps.cfg.RunMode == config.RunModeSimple {
		return nil, false, false
	}

	flusherEnabled := deps.cfg != nil && deps.cfg.Database.UserPlatformQuotaFlusherEnabled
	if deps.billingCacheService != nil && !isNilInterfaceValue(deps.billingCacheService.cache) {
		entry, ok, err := deps.billingCacheService.cache.GetUserPlatformQuotaCache(ctx, userID, platform)
		if err != nil {
			if flusherEnabled {
				return nil, true, true
			}
		} else if ok && entry != nil {
			if !userPlatformQuotaCacheEntryHasLimit(entry) {
				return nil, false, false
			}
			return usageBillingPlatformQuotaSnapshotFromCache(entry), false, true
		}
	}

	rec, err := deps.userPlatformQuotaRepo.GetByUserPlatform(ctx, userID, platform)
	if err != nil {
		// Fail safe: preserve a billable platform-quota effect. In flusher mode
		// the worker must obtain the authoritative cache snapshot before apply.
		return nil, flusherEnabled, true
	}
	if rec == nil || !userPlatformQuotaRecordHasLimit(rec) {
		return nil, false, false
	}
	return usageBillingPlatformQuotaSnapshotFromRecord(rec), false, true
}

func zeroUsageBillingPlatformQuotaSnapshot(at time.Time) *UsageBillingPlatformQuotaSnapshot {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	daily := timezone.StartOfDay(at)
	weekly := timezone.StartOfWeek(at)
	monthly := at
	return &UsageBillingPlatformQuotaSnapshot{
		DailyWindowStart:   &daily,
		WeeklyWindowStart:  &weekly,
		MonthlyWindowStart: &monthly,
	}
}

// PreparePlatformQuotaSnapshot resolves an intent that was sealed while the
// Redis-authoritative quota snapshot was unavailable. It is called before the
// billing transaction; the repository persists the enriched command while the
// worker still owns the lease.
func (s *UsageBillingPostEffectsService) PreparePlatformQuotaSnapshot(
	ctx context.Context,
	cmd *UsageBillingCommand,
) (bool, error) {
	if cmd == nil || cmd.PlatformQuotaCost <= 0 || !cmd.PlatformQuotaSnapshotNeeded {
		return false, nil
	}
	if s == nil || isNilInterfaceValue(s.userPlatformQuotaRepo) {
		return false, ErrUsageBillingPlatformQuotaSnapshotRequired
	}

	platform := strings.TrimSpace(cmd.Platform)
	if platform == "" {
		return false, errors.New("usage billing platform quota platform is empty")
	}
	if s.billingCacheService != nil && !isNilInterfaceValue(s.billingCacheService.cache) {
		entry, ok, err := s.billingCacheService.cache.GetUserPlatformQuotaCache(ctx, cmd.UserID, platform)
		if err != nil {
			return false, fmt.Errorf("load authoritative platform quota cache snapshot: %w", err)
		}
		if ok && entry != nil {
			cmd.PlatformQuotaSnapshot = usageBillingPlatformQuotaSnapshotFromCache(entry)
			cmd.PlatformQuotaSnapshotNeeded = false
			return true, nil
		}
	}

	rec, err := s.userPlatformQuotaRepo.GetByUserPlatform(ctx, cmd.UserID, platform)
	if err != nil {
		return false, fmt.Errorf("load platform quota DB snapshot: %w", err)
	}
	if rec == nil {
		cmd.PlatformQuotaSnapshot = zeroUsageBillingPlatformQuotaSnapshot(cmd.OccurredAt)
	} else {
		cmd.PlatformQuotaSnapshot = usageBillingPlatformQuotaSnapshotFromRecord(rec)
	}
	cmd.PlatformQuotaSnapshotNeeded = false
	return true, nil
}

func (s *UsageBillingPostEffectsService) Finalize(
	ctx context.Context,
	cmd *UsageBillingCommand,
	result *UsageBillingApplyResult,
) error {
	if s == nil {
		return errors.New("usage billing post-effects service is nil")
	}
	if cmd == nil {
		return fmt.Errorf("%w: usage billing command is nil", ErrUsageBillingPayloadInvalid)
	}
	if result == nil {
		return fmt.Errorf("%w: usage billing settlement result is nil", ErrDurableUsageBillingRequired)
	}
	return s.finalize(ctx, cmd, result, nil, nil, nil)
}

func (s *UsageBillingPostEffectsService) finalize(
	ctx context.Context,
	cmd *UsageBillingCommand,
	result *UsageBillingApplyResult,
	user *User,
	account *Account,
	authInvalidator usageBillingAuthCacheInvalidator,
) error {
	if s == nil {
		return errors.New("usage billing post-effects service is nil")
	}
	if cmd == nil {
		return fmt.Errorf("%w: usage billing command is nil", ErrUsageBillingPayloadInvalid)
	}
	if result == nil {
		return fmt.Errorf("%w: usage billing settlement result is nil", ErrDurableUsageBillingRequired)
	}
	projectionRepairOnly := !result.Applied && result.ProjectionRepairRequired
	if !result.Applied && !projectionRepairOnly {
		return nil
	}
	if result.Applied && s.deferredService != nil && cmd.AccountID > 0 {
		s.deferredService.ScheduleLastUsedUpdate(cmd.AccountID)
	}

	var projectionErrors []error
	holdBalanceProjection := result.FrozenBalance != nil && strings.EqualFold(strings.TrimSpace(cmd.MediaType), "video")
	cacheProjectionRequired := cmd.BalanceCost > 0 || holdBalanceProjection ||
		(cmd.SubscriptionCost > 0 && cmd.GroupID != nil) ||
		cmd.APIKeyRateLimitCost > 0 ||
		(cmd.PlatformQuotaCost > 0 && strings.TrimSpace(cmd.Platform) != "")
	if cacheProjectionRequired && s.billingCacheService == nil {
		projectionErrors = append(projectionErrors,
			errors.New("usage billing cache projection service is not configured"))
	} else if s.billingCacheService != nil {
		if cmd.BalanceCost > 0 || holdBalanceProjection {
			if err := s.billingCacheService.InvalidateUserBalance(ctx, cmd.UserID); err != nil {
				projectionErrors = append(projectionErrors, err)
			}
		}
		if cmd.SubscriptionCost > 0 && cmd.GroupID != nil {
			if err := s.billingCacheService.InvalidateSubscription(ctx, cmd.UserID, *cmd.GroupID); err != nil {
				projectionErrors = append(projectionErrors, err)
			}
		}
		if cmd.APIKeyRateLimitCost > 0 {
			if err := s.billingCacheService.InvalidateAPIKeyRateLimit(ctx, cmd.APIKeyID); err != nil {
				projectionErrors = append(projectionErrors, err)
			}
		}
		if cmd.PlatformQuotaCost > 0 && strings.TrimSpace(cmd.Platform) != "" {
			if err := s.billingCacheService.RefreshUserPlatformQuotaProjection(ctx, cmd.UserID, cmd.Platform); err != nil {
				projectionErrors = append(projectionErrors, err)
			}
		}
	}
	if len(projectionErrors) > 0 {
		return errors.Join(projectionErrors...)
	}

	if authInvalidator == nil {
		authInvalidator = s.authCacheInvalidator
	}
	authInvalidationRequired := result.APIKeyQuotaExhausted || result.BalanceOverdrafted ||
		(projectionRepairOnly && cmd.APIKeyQuotaCost > 0)
	if authInvalidationRequired && authInvalidator == nil {
		return errors.New("usage billing auth cache invalidator is not configured")
	}
	if authInvalidationRequired {
		authInvalidator.InvalidateAuthCacheByUserID(ctx, cmd.UserID)
	}

	// A pre-outbox process may have committed billing and then crashed before
	// usage logging or cache projection. Replaying invalidations is safe, but
	// notifications and last-used updates may already have happened and must
	// not be emitted again.
	if projectionRepairOnly {
		return nil
	}

	// Notifications remain advisory, but recovery must not silently omit them.
	// Load fresh metadata when the original request snapshots are unavailable.
	if s.balanceNotifyService != nil {
		if user == nil && s.userRepo != nil {
			loaded, err := s.userRepo.GetByID(ctx, cmd.UserID)
			if err != nil {
				slog.Warn("load user for recovered usage billing notification failed",
					"user_id", cmd.UserID, "request_id", cmd.RequestID, "error", err)
			} else {
				user = loaded
			}
		}
		if account == nil && s.accountRepo != nil {
			loaded, err := s.accountRepo.GetByID(ctx, cmd.AccountID)
			if err != nil {
				slog.Warn("load account for recovered usage billing notification failed",
					"account_id", cmd.AccountID, "request_id", cmd.RequestID, "error", err)
			} else {
				account = loaded
			}
		}
		p := &postUsageBillingParams{
			Cost: &CostBreakdown{
				ActualCost: cmd.ActualCost,
				TotalCost:  cmd.TotalCost,
			},
			User:               user,
			Account:            account,
			IsSubscriptionBill: cmd.IsSubscriptionBilling,
		}
		if cmd.TotalCost > 0 {
			p.AccountRateMultiplier = cmd.AccountQuotaCost / cmd.TotalCost
		}
		deps := &billingDeps{balanceNotifyService: s.balanceNotifyService}
		go notifyBalanceLow(p, deps, result)
		go notifyAccountQuota(p, deps, result)
	}
	return nil
}

func finalizeDurablePostUsageBilling(
	ctx context.Context,
	cmd *UsageBillingCommand,
	p *postUsageBillingParams,
	deps *billingDeps,
	result *UsageBillingApplyResult,
) error {
	if deps == nil {
		return nil
	}
	finalizer := &UsageBillingPostEffectsService{
		billingCacheService:   deps.billingCacheService,
		deferredService:       deps.deferredService,
		userRepo:              deps.userRepo,
		accountRepo:           deps.accountRepo,
		userPlatformQuotaRepo: deps.userPlatformQuotaRepo,
		balanceNotifyService:  deps.balanceNotifyService,
		cfg:                   deps.cfg,
	}
	var (
		user        *User
		account     *Account
		invalidator usageBillingAuthCacheInvalidator
	)
	if p != nil {
		user = p.User
		account = p.Account
		invalidator, _ = p.APIKeyService.(usageBillingAuthCacheInvalidator)
	}
	return finalizer.finalize(ctx, cmd, result, user, account, invalidator)
}
