package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyEmailVerificationSendCountUsesFixedWindow(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewAPIKeyEmailVerificationCache(client)
	ctx := context.Background()
	window := 10 * time.Minute

	count, err := cache.IncrementSendCount(ctx, 7, window)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	server.FastForward(3 * time.Minute)
	count, err = cache.IncrementSendCount(ctx, 7, window)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)

	remaining, err := client.TTL(ctx, apiKeyEmailRatePrefix+"7").Result()
	require.NoError(t, err)
	require.Greater(t, remaining, 6*time.Minute)
	require.Less(t, remaining, 8*time.Minute)
}
