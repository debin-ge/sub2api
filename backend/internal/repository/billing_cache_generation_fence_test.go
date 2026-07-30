//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestBillingCacheGenerationFence_RejectsLateBalanceSetAndDeduct(t *testing.T) {
	cache, _ := newMiniRedisCache(t)
	ctx := context.Background()

	staleGeneration, err := cache.GetUserBalanceCacheGeneration(ctx, 101)
	require.NoError(t, err)
	require.Zero(t, staleGeneration)
	require.NoError(t, cache.SetUserBalance(ctx, 101, 100))
	require.NoError(t, cache.InvalidateUserBalance(ctx, 101))

	require.NoError(t, cache.SetUserBalanceIfGeneration(ctx, 101, 100, staleGeneration))
	_, err = cache.GetUserBalance(ctx, 101)
	require.Error(t, err, "pre-invalidation balance fill must not resurrect the key")

	currentGeneration, err := cache.GetUserBalanceCacheGeneration(ctx, 101)
	require.NoError(t, err)
	require.Greater(t, currentGeneration, staleGeneration)
	require.NoError(t, cache.SetUserBalanceIfGeneration(ctx, 101, 90, currentGeneration))

	require.NoError(t, cache.InvalidateUserBalance(ctx, 101))
	newGeneration, err := cache.GetUserBalanceCacheGeneration(ctx, 101)
	require.NoError(t, err)
	require.NoError(t, cache.SetUserBalanceIfGeneration(ctx, 101, 80, newGeneration))
	require.NoError(t, cache.DeductUserBalanceIfGeneration(ctx, 101, 10, currentGeneration))

	balance, err := cache.GetUserBalance(ctx, 101)
	require.NoError(t, err)
	require.InDelta(t, 80, balance, 1e-9, "stale queued deduction must be fenced")
}

func TestBillingCacheGenerationFence_RejectsLateSubscriptionSetAndIncrement(t *testing.T) {
	cache, _ := newMiniRedisCache(t)
	ctx := context.Background()
	expiresAt := time.Now().Add(time.Hour)
	staleData := &service.SubscriptionCacheData{
		Status:       "active",
		ExpiresAt:    expiresAt,
		DailyUsage:   1,
		WeeklyUsage:  1,
		MonthlyUsage: 1,
		Version:      1,
	}

	staleGeneration, err := cache.GetSubscriptionCacheGeneration(ctx, 102, 12)
	require.NoError(t, err)
	require.NoError(t, cache.SetSubscriptionCache(ctx, 102, 12, staleData))
	require.NoError(t, cache.InvalidateSubscriptionCache(ctx, 102, 12))
	require.NoError(t, cache.SetSubscriptionCacheIfGeneration(ctx, 102, 12, staleData, staleGeneration))
	_, err = cache.GetSubscriptionCache(ctx, 102, 12)
	require.Error(t, err, "pre-invalidation subscription fill must not resurrect the key")

	currentGeneration, err := cache.GetSubscriptionCacheGeneration(ctx, 102, 12)
	require.NoError(t, err)
	currentData := &service.SubscriptionCacheData{
		Status:       "active",
		ExpiresAt:    expiresAt,
		DailyUsage:   2,
		WeeklyUsage:  2,
		MonthlyUsage: 2,
		Version:      2,
	}
	require.NoError(t, cache.SetSubscriptionCacheIfGeneration(ctx, 102, 12, currentData, currentGeneration))
	require.NoError(t, cache.InvalidateSubscriptionCache(ctx, 102, 12))
	newGeneration, err := cache.GetSubscriptionCacheGeneration(ctx, 102, 12)
	require.NoError(t, err)
	currentData.DailyUsage = 3
	currentData.WeeklyUsage = 3
	currentData.MonthlyUsage = 3
	require.NoError(t, cache.SetSubscriptionCacheIfGeneration(ctx, 102, 12, currentData, newGeneration))
	require.NoError(t, cache.UpdateSubscriptionUsageIfGeneration(ctx, 102, 12, 1, currentGeneration))

	got, err := cache.GetSubscriptionCache(ctx, 102, 12)
	require.NoError(t, err)
	require.InDelta(t, 3, got.DailyUsage, 1e-9)
	require.InDelta(t, 3, got.WeeklyUsage, 1e-9)
	require.InDelta(t, 3, got.MonthlyUsage, 1e-9)
}

func TestBillingCacheGenerationFence_RejectsLateRateLimitSetAndIncrement(t *testing.T) {
	cache, _ := newMiniRedisCache(t)
	ctx := context.Background()
	window := time.Now().Unix()
	staleData := &service.APIKeyRateLimitCacheData{
		Usage5h:  1,
		Usage1d:  1,
		Usage7d:  1,
		Window5h: window,
		Window1d: window,
		Window7d: window,
	}

	staleGeneration, err := cache.GetAPIKeyRateLimitCacheGeneration(ctx, 103)
	require.NoError(t, err)
	require.NoError(t, cache.SetAPIKeyRateLimit(ctx, 103, staleData))
	require.NoError(t, cache.InvalidateAPIKeyRateLimit(ctx, 103))
	require.NoError(t, cache.SetAPIKeyRateLimitIfGeneration(ctx, 103, staleData, staleGeneration))
	_, err = cache.GetAPIKeyRateLimit(ctx, 103)
	require.Error(t, err, "pre-invalidation rate-limit fill must not resurrect the key")

	currentGeneration, err := cache.GetAPIKeyRateLimitCacheGeneration(ctx, 103)
	require.NoError(t, err)
	currentData := &service.APIKeyRateLimitCacheData{
		Usage5h:  2,
		Usage1d:  2,
		Usage7d:  2,
		Window5h: window,
		Window1d: window,
		Window7d: window,
	}
	require.NoError(t, cache.SetAPIKeyRateLimitIfGeneration(ctx, 103, currentData, currentGeneration))
	require.NoError(t, cache.InvalidateAPIKeyRateLimit(ctx, 103))
	newGeneration, err := cache.GetAPIKeyRateLimitCacheGeneration(ctx, 103)
	require.NoError(t, err)
	currentData.Usage5h = 3
	currentData.Usage1d = 3
	currentData.Usage7d = 3
	require.NoError(t, cache.SetAPIKeyRateLimitIfGeneration(ctx, 103, currentData, newGeneration))
	require.NoError(t, cache.UpdateAPIKeyRateLimitUsageIfGeneration(ctx, 103, 1, currentGeneration))

	got, err := cache.GetAPIKeyRateLimit(ctx, 103)
	require.NoError(t, err)
	require.InDelta(t, 3, got.Usage5h, 1e-9)
	require.InDelta(t, 3, got.Usage1d, 1e-9)
	require.InDelta(t, 3, got.Usage7d, 1e-9)
}
