//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestVideoTaskQueueLifecycleAndValidation(t *testing.T) {
	ctx := context.Background()
	queue := &videoTaskQueue{rdb: testRedis(t)}
	taskID := service.NewVideoTaskID()

	enqueued, err := queue.Enqueue(ctx, taskID)
	require.NoError(t, err)
	require.True(t, enqueued)
	enqueued, err = queue.Enqueue(ctx, taskID)
	require.NoError(t, err)
	require.False(t, enqueued)

	reserved, err := queue.Reserve(ctx)
	require.NoError(t, err)
	require.Equal(t, taskID, reserved)
	require.NoError(t, queue.RequeueAfter(ctx, taskID, 0))
	reserved, err = queue.Reserve(ctx)
	require.NoError(t, err)
	require.Equal(t, taskID, reserved)
	require.NoError(t, queue.Ack(ctx, taskID))

	_, err = queue.Reserve(ctx)
	require.ErrorIs(t, err, service.ErrVideoQueueEmpty)
	_, err = queue.Enqueue(ctx, "upstream_video_id")
	require.ErrorIs(t, err, service.ErrVideoQueueInvalidPayload)
}

func TestVideoTaskQueueMovesDelayedAndRecoversStale(t *testing.T) {
	ctx := context.Background()
	queue := &videoTaskQueue{rdb: testRedis(t)}
	taskID := service.NewVideoTaskID()
	_, err := queue.Enqueue(ctx, taskID)
	require.NoError(t, err)
	_, err = queue.Reserve(ctx)
	require.NoError(t, err)
	require.NoError(t, queue.RequeueAfter(ctx, taskID, time.Millisecond))
	time.Sleep(5 * time.Millisecond)
	moved, err := queue.MoveDueToReady(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, moved)
	_, err = queue.Reserve(ctx)
	require.NoError(t, err)

	require.NoError(t, queue.rdb.ZAdd(ctx, videoTaskQueueActiveKey, redis.Z{
		Score: float64(time.Now().UTC().Add(-time.Hour).UnixMilli()), Member: taskID,
	}).Err())
	recovered, err := queue.RecoverStale(ctx, time.Minute, 10)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)
	require.NoError(t, queue.Ack(ctx, taskID))
}
