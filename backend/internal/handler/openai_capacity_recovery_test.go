package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newCapacityShedError() *service.UpstreamFailoverError {
	return &service.UpstreamFailoverError{
		RetryableOnSameAccount: true,
		RequestScopedTransient: true,
		ResponseBody:           []byte(`{"error":{"message":"Our servers are currently overloaded"}}`),
	}
}

func TestOpenAICapacityRecoveryDefaultsAreUnbounded(t *testing.T) {
	recovery := NewOpenAICapacityRecoveryState(&config.Config{})
	require.True(t, recovery.Enabled())
	require.Equal(t, 3, recovery.MaxAttempts())
	require.Equal(t, 0, recovery.MaxRounds(), "默认不限轮次")
	require.False(t, recovery.hasDeadline, "默认不设墙钟上限")
	require.Equal(t, time.Duration(-1), recovery.Remaining())
	require.False(t, recovery.Exhausted())
}

func TestOpenAICapacityRecoveryHonorsOptionalBudgetAndKillSwitch(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICapacityRetryBudgetSeconds = 30
	recovery := NewOpenAICapacityRecoveryState(cfg)
	require.True(t, recovery.hasDeadline)
	require.InDelta(t, 30*time.Second, recovery.deadline.Sub(recovery.startedAt), float64(50*time.Millisecond))

	cfg.Gateway.OpenAICapacityRetryMaxAttempts = 99
	require.Equal(t, 3, NewOpenAICapacityRecoveryState(cfg).MaxAttempts(), "同账号重试仍有硬顶")

	cfg.Gateway.OpenAICapacityRetryMaxRounds = -1
	disabled := NewOpenAICapacityRecoveryState(cfg)
	require.False(t, disabled.Enabled())
	require.True(t, disabled.Exhausted())
	require.Equal(t, openAICapacityExhaustedReasonDisabled, disabled.ExhaustedReason())
}

func TestOpenAICapacityRecoveryBreadthFirstOrdering(t *testing.T) {
	recovery := NewOpenAICapacityRecoveryState(&config.Config{})
	err := newCapacityShedError()

	// 第 1 轮：不做同账号重试，直接快速换号。
	_, _, ok := recovery.BeginSameAccountRetry(42, err)
	require.False(t, ok, "第 1 轮应优先换号")
	require.Equal(t, OpenAICapacityRetryNextAccount, recovery.Decide(context.Background(), 42, err))

	// 整池扫完后进入第 2 轮，此时才启用同账号退避重试。
	excluded := map[int64]struct{}{42: {}}
	removed, delay, ok := recovery.BeginNextRound(context.Background(), excluded)
	require.True(t, ok)
	require.Equal(t, []int64{42}, removed)
	require.Equal(t, 2, recovery.Round())
	require.Equal(t, 500*time.Millisecond, delay)

	for _, want := range []struct {
		count int
		delay time.Duration
	}{{1, 500 * time.Millisecond}, {2, time.Second}, {3, 2 * time.Second}} {
		count, retryDelay, ok := recovery.BeginSameAccountRetry(42, err)
		require.True(t, ok)
		require.Equal(t, want.count, count)
		require.Equal(t, want.delay, retryDelay)
	}
	_, _, ok = recovery.BeginSameAccountRetry(42, err)
	require.False(t, ok)
}

func TestOpenAICapacityRecoveryNextRoundKeepsNonCapacityExclusions(t *testing.T) {
	recovery := NewOpenAICapacityRecoveryState(&config.Config{})
	recovery.MarkAccountFailed(1)
	// 99 是非降载排除（限流/封禁等），不能被降载恢复放回候选池。
	excluded := map[int64]struct{}{1: {}, 99: {}}

	removed, delay, ok := recovery.BeginNextRound(context.Background(), excluded)
	require.True(t, ok, "混合排除集同样应能进入下一轮")
	require.Equal(t, []int64{1}, removed)
	require.Equal(t, 2, recovery.Round())
	require.Equal(t, 500*time.Millisecond, delay)

	// 没有任何降载排除时不推进轮次。
	_, _, ok = recovery.BeginNextRound(context.Background(), map[int64]struct{}{99: {}})
	require.False(t, ok)
}

func TestOpenAICapacityRoundBackoffCapped(t *testing.T) {
	require.Equal(t, time.Duration(0), openAICapacityRoundBackoffFor(1))
	require.Equal(t, 500*time.Millisecond, openAICapacityRoundBackoffFor(2))
	require.Equal(t, time.Second, openAICapacityRoundBackoffFor(3))
	require.Equal(t, 2*time.Second, openAICapacityRoundBackoffFor(4))
	require.Equal(t, 4*time.Second, openAICapacityRoundBackoffFor(5))
	require.Equal(t, openAICapacityRoundBackoffMax, openAICapacityRoundBackoffFor(6))
	require.Equal(t, openAICapacityRoundBackoffMax, openAICapacityRoundBackoffFor(50))
}

func TestOpenAICapacityRecoveryTotalAttemptHardMax(t *testing.T) {
	recovery := NewOpenAICapacityRecoveryState(&config.Config{})
	err := newCapacityShedError()
	for i := 0; i < openAICapacityTotalAttemptHardMax; i++ {
		recovery.noteAttempt(int64(i))
	}
	require.True(t, recovery.Exhausted())
	require.Equal(t, openAICapacityExhaustedReasonTotalAttemptCap, recovery.ExhaustedReason())
	require.Equal(t, OpenAICapacityRetryExhausted, recovery.Decide(context.Background(), 1, err))
}

func TestOpenAICapacityRecoveryDecideCanceledOnClientDisconnect(t *testing.T) {
	recovery := NewOpenAICapacityRecoveryState(&config.Config{})
	err := newCapacityShedError()
	// 推进到第 2 轮，让同账号退避（含睡眠）生效。
	recovery.MarkAccountFailed(7)
	_, _, ok := recovery.BeginNextRound(context.Background(), map[int64]struct{}{7: {}})
	require.True(t, ok)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Equal(t, OpenAICapacityRetryCanceled, recovery.Decide(ctx, 7, err))
}

func TestOpenAICapacityRecoveryExhaustedReasonRoundCap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICapacityRetryMaxRounds = 1
	recovery := NewOpenAICapacityRecoveryState(cfg)
	recovery.MarkAccountFailed(1)
	_, _, ok := recovery.BeginNextRound(context.Background(), map[int64]struct{}{1: {}})
	require.False(t, ok, "轮次上限为 1 时不应进入第 2 轮")
	require.Equal(t, openAICapacityExhaustedReasonNoMoreAccounts, recovery.ExhaustedReason())

	recovery.round = 2
	require.True(t, recovery.Exhausted())
	require.Equal(t, openAICapacityExhaustedReasonRoundCap, recovery.ExhaustedReason())
}
