package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func newGoCaptchaCacheForTest(t *testing.T) (service.GoCaptchaCache, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })

	return NewGoCaptchaCache(rdb), mr
}

func TestGoCaptchaCacheChallengeIsTakenAtomically(t *testing.T) {
	cache, _ := newGoCaptchaCacheForTest(t)
	ctx := context.Background()

	require.NoError(t, cache.SaveChallenge(ctx, "abc", []byte(`{"mode":"click"}`), time.Minute))

	payload, err := cache.TakeChallenge(ctx, "abc")
	require.NoError(t, err)
	require.JSONEq(t, `{"mode":"click"}`, string(payload))

	// 取出即删：一个挑战只有一次作答机会
	payload, err = cache.TakeChallenge(ctx, "abc")
	require.NoError(t, err)
	require.Empty(t, payload)
}

func TestGoCaptchaCacheMissingChallengeIsNotAnError(t *testing.T) {
	cache, _ := newGoCaptchaCacheForTest(t)

	payload, err := cache.TakeChallenge(context.Background(), "never-existed")

	require.NoError(t, err)
	require.Empty(t, payload)
}

func TestGoCaptchaCacheChallengeExpires(t *testing.T) {
	cache, mr := newGoCaptchaCacheForTest(t)
	ctx := context.Background()

	require.NoError(t, cache.SaveChallenge(ctx, "abc", []byte("payload"), 30*time.Second))
	mr.FastForward(31 * time.Second)

	payload, err := cache.TakeChallenge(ctx, "abc")
	require.NoError(t, err)
	require.Empty(t, payload)
}

func TestGoCaptchaCacheTokenIsTakenAtomically(t *testing.T) {
	cache, _ := newGoCaptchaCacheForTest(t)
	ctx := context.Background()

	require.NoError(t, cache.SaveToken(ctx, "hash", []byte(`{"created_at":1}`), time.Minute))

	payload, err := cache.TakeToken(ctx, "hash")
	require.NoError(t, err)
	require.JSONEq(t, `{"created_at":1}`, string(payload))

	payload, err = cache.TakeToken(ctx, "hash")
	require.NoError(t, err)
	require.Empty(t, payload)
}

func TestGoCaptchaCacheFailureCounterTriggersCooldown(t *testing.T) {
	cache, _ := newGoCaptchaCacheForTest(t)
	ctx := context.Background()
	const clientIP = "203.0.113.10"

	cooling, err := cache.IsCoolingDown(ctx, clientIP)
	require.NoError(t, err)
	require.False(t, cooling)

	for i := 0; i < 2; i++ {
		count, cooled, err := cache.RecordFailure(ctx, clientIP, 3, 10*time.Minute, 10*time.Minute)
		require.NoError(t, err)
		require.Equal(t, i+1, count)
		require.False(t, cooled)
		cooling, err = cache.IsCoolingDown(ctx, clientIP)
		require.NoError(t, err)
		require.False(t, cooling, "cooldown should not trigger before the threshold")
	}

	count, cooled, err := cache.RecordFailure(ctx, clientIP, 3, 10*time.Minute, 10*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 3, count)
	require.True(t, cooled)
	cooling, err = cache.IsCoolingDown(ctx, clientIP)
	require.NoError(t, err)
	require.True(t, cooling)
}

func TestGoCaptchaCacheCooldownExpires(t *testing.T) {
	cache, mr := newGoCaptchaCacheForTest(t)
	ctx := context.Background()
	const clientIP = "203.0.113.10"

	_, _, err := cache.RecordFailure(ctx, clientIP, 1, time.Minute, 5*time.Minute)
	require.NoError(t, err)
	cooling, err := cache.IsCoolingDown(ctx, clientIP)
	require.NoError(t, err)
	require.True(t, cooling)

	mr.FastForward(6 * time.Minute)

	cooling, err = cache.IsCoolingDown(ctx, clientIP)
	require.NoError(t, err)
	require.False(t, cooling)
}

func TestGoCaptchaCacheFailureCounterResetsAfterWindow(t *testing.T) {
	cache, mr := newGoCaptchaCacheForTest(t)
	ctx := context.Background()
	const clientIP = "203.0.113.10"

	_, _, err := cache.RecordFailure(ctx, clientIP, 2, time.Minute, 10*time.Minute)
	require.NoError(t, err)
	mr.FastForward(61 * time.Second)

	// 窗口过期后重新计数，单次失败不应触发冷却
	count, cooled, err := cache.RecordFailure(ctx, clientIP, 2, time.Minute, 10*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.False(t, cooled)
	cooling, err := cache.IsCoolingDown(ctx, clientIP)
	require.NoError(t, err)
	require.False(t, cooling)
}

func TestGoCaptchaCacheClearFailures(t *testing.T) {
	cache, _ := newGoCaptchaCacheForTest(t)
	ctx := context.Background()
	const clientIP = "203.0.113.10"

	_, _, err := cache.RecordFailure(ctx, clientIP, 2, time.Minute, time.Minute)
	require.NoError(t, err)
	require.NoError(t, cache.ClearFailures(ctx, clientIP))

	// 计数已清零，再失败一次仍不该触发冷却
	count, cooled, err := cache.RecordFailure(ctx, clientIP, 2, time.Minute, time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.False(t, cooled)
	cooling, err := cache.IsCoolingDown(ctx, clientIP)
	require.NoError(t, err)
	require.False(t, cooling)
}

func TestGoCaptchaCacheIgnoresEmptyIP(t *testing.T) {
	cache, _ := newGoCaptchaCacheForTest(t)
	ctx := context.Background()

	// 拿不到 IP 的请求不参与失败计数，否则它们会共用一个计数器互相误伤
	count, cooled, err := cache.RecordFailure(ctx, "", 1, time.Minute, time.Minute)
	require.NoError(t, err)
	require.Zero(t, count)
	require.False(t, cooled)
	cooling, err := cache.IsCoolingDown(ctx, "")
	require.NoError(t, err)
	require.False(t, cooling)
}

func TestGoCaptchaCacheFailureCounterUsesSlidingWindow(t *testing.T) {
	cache, mr := newGoCaptchaCacheForTest(t)
	ctx := context.Background()
	const clientIP = "203.0.113.10"
	startedAt := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	mr.SetTime(startedAt)

	_, _, err := cache.RecordFailure(ctx, clientIP, 3, time.Minute, 10*time.Minute)
	require.NoError(t, err)
	mr.SetTime(startedAt.Add(40 * time.Second))
	_, _, err = cache.RecordFailure(ctx, clientIP, 3, time.Minute, 10*time.Minute)
	require.NoError(t, err)
	mr.SetTime(startedAt.Add(70 * time.Second))

	// 第一条记录已滑出 60 秒窗口，第三次失败时窗口内实际只有两次。
	count, cooled, err := cache.RecordFailure(ctx, clientIP, 3, time.Minute, 10*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.False(t, cooled)
}
