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
