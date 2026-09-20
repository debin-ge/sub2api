//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVideoRPMReservationIsIdempotentAndSharesExistingCounters(t *testing.T) {
	ctx := context.Background()
	cache := &userRPMCacheImpl{rdb: testRedis(t)}
	waitForSafeMinuteWindow(ctx, t, cache)
	userID := time.Now().UnixNano()
	count, err := cache.IncrementUserRPM(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, err = cache.IncrementUserRPMOnce(ctx, userID, "video-operation")
	require.NoError(t, err)
	require.Equal(t, 2, count)
	count, err = cache.IncrementUserRPMOnce(ctx, userID, "video-operation")
	require.NoError(t, err)
	require.Equal(t, 2, count)
	var workers sync.WaitGroup
	errors := make(chan error, 16)
	for index := 0; index < 16; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := cache.IncrementUserGroupRPMOnce(ctx, userID, 7, "shared-video-operation")
			errors <- err
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	count, err = cache.GetUserGroupRPM(ctx, userID, 7)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

// waitForSafeMinuteWindow 避免本测试的连续调用跨越 Redis 分钟窗口边界。
//
// userRPMCacheImpl.minuteTS 在每次调用时独立查询 Redis 服务端时间来派生计数器 key
// （rpm:u:{uid}:{minute}），本测试连续发起多次调用；如果恰好在边界附近，中途分钟
// 翻转会导致前后调用落在不同的 key 上，计数器被重置，从而让本该确定性通过的断言
// 偶发失败。这里在测试正式开始前，确保当前分钟至少还剩余一段安全余量。
func waitForSafeMinuteWindow(ctx context.Context, t *testing.T, cache *userRPMCacheImpl) {
	t.Helper()
	now, err := cache.rdb.Time(ctx).Result()
	require.NoError(t, err)
	if now.Second() >= 55 {
		time.Sleep(time.Duration(61-now.Second()) * time.Second)
	}
}
