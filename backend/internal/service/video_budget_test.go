package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type videoBudgetLoaderStub struct {
	videoAdmissionRateLoader
	snapshot *VideoBudgetSnapshot
	err      error
	platform string
}

type videoBudgetWarmCache struct {
	BillingCache
	reads int
}

func (cache *videoBudgetWarmCache) GetUserBalance(context.Context, int64) (float64, error) {
	cache.reads++
	return 1000, nil
}

func (cache *videoBudgetWarmCache) GetAPIKeyRateLimit(context.Context, int64) (*APIKeyRateLimitCacheData, error) {
	cache.reads++
	return &APIKeyRateLimitCacheData{}, nil
}

func (cache *videoBudgetWarmCache) GetUserPlatformQuotaCache(context.Context, int64, string) (*UserPlatformQuotaCacheEntry, bool, error) {
	cache.reads++
	return &UserPlatformQuotaCacheEntry{SchemaVersion: UserPlatformQuotaCacheSchemaV1}, true, nil
}

func (loader *videoBudgetLoaderStub) GetVideoBudgetSnapshot(_ context.Context, _, _ int64, platform string) (*VideoBudgetSnapshot, error) {
	loader.platform = platform
	return loader.snapshot, loader.err
}

func TestVideoBudgetSnapshotKeepsReservationsAfterWindowRollover(t *testing.T) {
	now := time.Now()
	expired := now.Add(-60 * 24 * time.Hour)
	for _, window := range []string{"total", "5h", "1d", "7d", "daily", "weekly", "monthly"} {
		t.Run(window, func(t *testing.T) {
			key := &APIKey{ID: 1, UserID: 42, Status: StatusActive}
			quota := &UserPlatformQuotaRecord{DailyWindowStart: &expired, WeeklyWindowStart: &expired, MonthlyWindowStart: &expired,
				DailyUsageUSD: 99, WeeklyUsageUSD: 99, MonthlyUsageUSD: 99}
			snapshot := &VideoBudgetSnapshot{APIKey: key, Platform: quota, KeyReserved: 5, PlatformReserved: 5}
			var expected error
			switch window {
			case "total":
				key.Quota = 5
				expected = ErrAPIKeyQuotaExhausted
			case "5h":
				key.RateLimit5h, key.Usage5h, key.Window5hStart = 5, 99, &expired
				expected = ErrAPIKeyRateLimit5hExceeded
			case "1d":
				key.RateLimit1d, key.Usage1d, key.Window1dStart = 5, 99, &expired
				expected = ErrAPIKeyRateLimit1dExceeded
			case "7d":
				key.RateLimit7d, key.Usage7d, key.Window7dStart = 5, 99, &expired
				expected = ErrAPIKeyRateLimit7dExceeded
			case "daily":
				quota.DailyLimitUSD = floatPointer(5)
				expected = ErrUserPlatformDailyQuotaExhausted
			case "weekly":
				quota.WeeklyLimitUSD = floatPointer(5)
				expected = ErrUserPlatformWeeklyQuotaExhausted
			case "monthly":
				quota.MonthlyLimitUSD = floatPointer(5)
				expected = ErrUserPlatformMonthlyQuotaExhausted
			}
			require.ErrorIs(t, evaluateVideoBudgetSnapshot(snapshot, key.UserID, key, now), expected)
			snapshot.KeyReserved, snapshot.PlatformReserved = 4, 4
			require.NoError(t, evaluateVideoBudgetSnapshot(snapshot, key.UserID, key, now))
		})
	}
}

func TestVideoBudgetSnapshotDoesNotMutateSpentFactsOrAcceptNonfiniteAmounts(t *testing.T) {
	now := time.Now()
	expired := now.Add(-60 * 24 * time.Hour)
	key := &APIKey{ID: 1, UserID: 42, Status: StatusActive, RateLimit5h: 10, Usage5h: 99, Window5hStart: &expired}
	snapshot := &VideoBudgetSnapshot{APIKey: key, KeyReserved: 4}
	require.NoError(t, evaluateVideoBudgetSnapshot(snapshot, key.UserID, key, now))
	require.Equal(t, 99.0, key.Usage5h)
	require.Equal(t, expired, *key.Window5hStart)
	for _, invalid := range []float64{math.NaN(), math.Inf(1), -1} {
		snapshot.KeyReserved = invalid
		require.ErrorIs(t, evaluateVideoBudgetSnapshot(snapshot, key.UserID, key, now), ErrBillingServiceUnavailable)
	}
}

func TestVideoBudgetProjectionGuardsOrdinaryAndVideoAdmission(t *testing.T) {
	for _, entry := range []string{"ordinary", "video"} {
		for _, scenario := range []string{"reserved", "missing_loader", "storage_failure"} {
			t.Run(entry+"/"+scenario, func(t *testing.T) {
				user := &User{ID: 42, Status: StatusActive, Balance: 100}
				group := &Group{ID: 7, Status: StatusActive, Platform: PlatformOpenAI}
				key := &APIKey{ID: 1, UserID: user.ID, Status: StatusActive, User: user, GroupID: &group.ID, Group: group, Quota: 5}
				loader := &videoBudgetLoaderStub{snapshot: &VideoBudgetSnapshot{APIKey: key, KeyReserved: 5}}
				cache := &videoBudgetWarmCache{}
				svc := &BillingCacheService{cfg: &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{Enabled: true}}},
					cache: cache, apiKeyRateLimitLoader: loader, userRepo: &videoAdmissionUserRepo{user: user}, userPlatformQuotaRepo: &videoAdmissionQuotaRepo{}}
				expected := ErrAPIKeyQuotaExhausted
				if scenario == "missing_loader" {
					svc.apiKeyRateLimitLoader = nil
					expected = ErrBillingServiceUnavailable
				}
				if scenario == "storage_failure" {
					loader.err = errors.New("snapshot unavailable")
					expected = ErrBillingServiceUnavailable
				}
				var err error
				if entry == "ordinary" {
					err = svc.CheckBillingEligibility(context.Background(), user, key, group, nil, PlatformOpenAI)
				} else {
					err = svc.CheckVideoAdmission(context.Background(), key, group, PlatformOpenAI, "idempotent-intent")
				}
				require.ErrorIs(t, err, expected)
				require.Zero(t, cache.reads)
			})
		}
	}
}

func TestVideoBudgetProjectionPreservesDisabledFeatureAndSubscriptionScope(t *testing.T) {
	user := &User{ID: 42}
	key := &APIKey{ID: 1, UserID: 42, Status: StatusActive}
	svc := &BillingCacheService{cfg: &config.Config{}}
	require.NoError(t, svc.checkVideoBudgetProjection(context.Background(), user.ID, key, PlatformOpenAI))
	loader := &videoBudgetLoaderStub{snapshot: &VideoBudgetSnapshot{APIKey: key, KeyReserved: 5}}
	key.Quota = 5
	svc.apiKeyRateLimitLoader = loader
	svc.cfg.Gateway.Video.Enabled = true
	group := &Group{SubscriptionType: SubscriptionTypeSubscription}
	err := svc.CheckBillingEligibility(context.Background(), user, key, group, &UserSubscription{}, PlatformOpenAI)
	require.ErrorIs(t, err, ErrAPIKeyQuotaExhausted)
	require.Empty(t, loader.platform)
}

func TestVideoBudgetSnapshotDoesNotRejectAuthorizedFallbackGroupClone(t *testing.T) {
	primaryID, fallbackID := int64(7), int64(8)
	original := &APIKey{ID: 1, UserID: 42, Status: StatusActive, GroupID: &primaryID, Quota: 10}
	fallback := *original
	fallback.GroupID = &fallbackID
	snapshot := &VideoBudgetSnapshot{APIKey: original, KeyReserved: 4}
	require.NoError(t, evaluateVideoBudgetSnapshot(snapshot, original.UserID, &fallback, time.Now()))
	snapshot.KeyReserved = 10
	require.ErrorIs(t, evaluateVideoBudgetSnapshot(snapshot, original.UserID, &fallback, time.Now()), ErrAPIKeyQuotaExhausted)
}
