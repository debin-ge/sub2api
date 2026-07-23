//go:build integration

package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestEmailRateLimitScriptSemantics 验证 Lua 脚本的关键语义（需真实 Redis）：
// 1. check-then-increment：被拒绝的请求不增加任何一层计数
// 2. TTL 只在 key 首次创建时设置，不被后续请求刷新（窗口不会被顺延）
// 3. 域名与地址两层独立计数
func TestEmailRateLimitScriptSemantics(t *testing.T) {
	ctx := context.Background()
	rdb := startRedis(t, ctx)

	domainKey := "rate:verify-code:email-domain:gmail.com"
	emailKey := "rate:verify-code:email:alice@gmail.com"
	window := int64(2000) // 2s

	// 前2次放行
	for i := 0; i < 2; i++ {
		layer, err := emailRateLimitRun(ctx, rdb, domainKey, emailKey, window, 100, window, 2)
		require.NoError(t, err)
		require.Empty(t, layer)
	}
	ttlBefore, err := rdb.PTTL(ctx, emailKey).Result()
	require.NoError(t, err)
	require.Greater(t, ttlBefore, time.Duration(0))

	time.Sleep(50 * time.Millisecond)

	// 第3次被地址层拒绝
	layer, err := emailRateLimitRun(ctx, rdb, domainKey, emailKey, window, 100, window, 2)
	require.NoError(t, err)
	require.Equal(t, "email", layer)

	// 拒绝的请求不增加地址计数，也不增加域名计数
	count, err := rdb.Get(ctx, emailKey).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
	dcount, err := rdb.Get(ctx, domainKey).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(2), dcount)

	// TTL 未被刷新（持续递减）
	ttlAfter, err := rdb.PTTL(ctx, emailKey).Result()
	require.NoError(t, err)
	require.Less(t, ttlAfter, ttlBefore)

	// 同域名的其他地址仍放行
	layer, err = emailRateLimitRun(ctx, rdb, domainKey, "rate:verify-code:email:bob@gmail.com", window, 100, window, 2)
	require.NoError(t, err)
	require.Empty(t, layer)

	// 窗口过期后计数自动重置，同一地址恢复放行
	time.Sleep(2100 * time.Millisecond)
	layer, err = emailRateLimitRun(ctx, rdb, domainKey, emailKey, window, 100, window, 2)
	require.NoError(t, err)
	require.Empty(t, layer)
}

// TestCheckAndIncrScriptSemantics 验证 IP 层单 key 脚本的同样语义
func TestCheckAndIncrScriptSemantics(t *testing.T) {
	ctx := context.Background()
	rdb := startRedis(t, ctx)

	key := "rate:registration:ip:203.0.113.10"
	window := int64(2000)

	allowed, err := checkAndIncrRun(ctx, rdb, key, window, 1)
	require.NoError(t, err)
	require.True(t, allowed)

	ttlBefore, err := rdb.PTTL(ctx, key).Result()
	require.NoError(t, err)
	require.Greater(t, ttlBefore, time.Duration(0))

	time.Sleep(50 * time.Millisecond)

	// 已达上限：拒绝且不增加计数、不刷新 TTL
	allowed, err = checkAndIncrRun(ctx, rdb, key, window, 1)
	require.NoError(t, err)
	require.False(t, allowed)

	count, err := rdb.Get(ctx, key).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	ttlAfter, err := rdb.PTTL(ctx, key).Result()
	require.NoError(t, err)
	require.Less(t, ttlAfter, ttlBefore)
}
