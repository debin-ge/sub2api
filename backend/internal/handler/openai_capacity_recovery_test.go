package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAICapacityRecoveryDefaultsAndHardCeilings(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICapacityRetryMaxAttempts = 99
	cfg.Gateway.OpenAICapacityRetryBudgetSeconds = 99
	cfg.Gateway.OpenAICapacityRetryMaxRounds = 99

	recovery := NewOpenAICapacityRecoveryState(cfg)
	require.Equal(t, 3, recovery.MaxAttempts())
	require.Equal(t, 3, recovery.MaxRounds())
	require.InDelta(t, 15*time.Second, recovery.deadline.Sub(recovery.startedAt), float64(50*time.Millisecond))
}

func TestOpenAICapacityRecoveryRetriesThreeTimesPerAccount(t *testing.T) {
	recovery := NewOpenAICapacityRecoveryState(&config.Config{})
	err := &service.UpstreamFailoverError{
		RetryableOnSameAccount: true,
		RequestScopedTransient: true,
		ResponseBody:           []byte(`{"error":{"message":"Our servers are currently overloaded"}}`),
	}

	for _, want := range []struct {
		count int
		delay time.Duration
	}{{1, 500 * time.Millisecond}, {2, time.Second}, {3, 2 * time.Second}} {
		count, delay, ok := recovery.BeginSameAccountRetry(42, err)
		require.True(t, ok)
		require.Equal(t, want.count, count)
		require.Equal(t, want.delay, delay)
	}
	_, _, ok := recovery.BeginSameAccountRetry(42, err)
	require.False(t, ok)
}

func TestOpenAICapacityRecoveryNextRoundOnlyResetsCapacityExclusions(t *testing.T) {
	recovery := NewOpenAICapacityRecoveryState(&config.Config{})
	recovery.MarkAccountFailed(1)
	excluded := map[int64]struct{}{1: {}}

	removed, delay, ok := recovery.BeginNextRound(context.Background(), excluded)
	require.True(t, ok)
	require.Equal(t, []int64{1}, removed)
	require.Equal(t, 2, recovery.Round())
	require.Equal(t, 500*time.Millisecond, delay)

	recovery.MarkAccountFailed(2)
	_, _, ok = recovery.BeginNextRound(context.Background(), map[int64]struct{}{2: {}, 99: {}})
	require.False(t, ok)
}
