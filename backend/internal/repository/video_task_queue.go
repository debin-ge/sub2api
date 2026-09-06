package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	videoTaskQueueReadyKey   = "video_task:queue:ready"
	videoTaskQueueDelayedKey = "video_task:queue:delayed"
	videoTaskQueueActiveKey  = "video_task:queue:active"
	videoTaskQueueQueuedKey  = "video_task:queue:queued"
	videoTaskQueueDedupTTL   = 7 * 24 * time.Hour
)

var videoTaskQueueEnqueueScript = redis.NewScript(`
if redis.call("SET", KEYS[1], ARGV[1], "NX", "PX", ARGV[2]) then
  redis.call("LPUSH", KEYS[2], ARGV[1])
  return 1
end
return 0
`)

var videoTaskQueueReserveScript = redis.NewScript(`
local task = redis.call("RPOP", KEYS[1])
if not task then
  return nil
end
redis.call("ZADD", KEYS[2], ARGV[1], task)
return task
`)

var videoTaskQueueMoveDueScript = redis.NewScript(`
local tasks = redis.call("ZRANGEBYSCORE", KEYS[1], "-inf", ARGV[1], "LIMIT", 0, ARGV[2])
for _, task in ipairs(tasks) do
  redis.call("ZREM", KEYS[1], task)
  redis.call("LPUSH", KEYS[2], task)
end
return #tasks
`)

var videoTaskQueueRequeueScript = redis.NewScript(`
redis.call("ZREM", KEYS[1], ARGV[1])
redis.call("ZREM", KEYS[2], ARGV[1])
if tonumber(ARGV[2]) <= 0 then
  redis.call("LPUSH", KEYS[3], ARGV[1])
else
  redis.call("ZADD", KEYS[2], ARGV[2], ARGV[1])
end
return 1
`)

var videoTaskQueueAckScript = redis.NewScript(`
redis.call("ZREM", KEYS[1], ARGV[1])
redis.call("ZREM", KEYS[2], ARGV[1])
redis.call("LREM", KEYS[3], 0, ARGV[1])
redis.call("DEL", KEYS[4])
return 1
`)

type videoTaskQueue struct {
	rdb *redis.Client
}

func NewVideoTaskQueue(rdb *redis.Client) service.VideoTaskQueue {
	return &videoTaskQueue{rdb: rdb}
}

func (q *videoTaskQueue) Enqueue(ctx context.Context, taskID string) (bool, error) {
	if !service.IsValidVideoTaskID(taskID) {
		return false, service.ErrVideoQueueInvalidPayload
	}
	if q == nil || q.rdb == nil {
		return false, errors.New("video task queue redis client is nil")
	}
	applied, err := videoTaskQueueEnqueueScript.Run(
		ctx,
		q.rdb,
		[]string{videoTaskQueueQueuedKey + ":" + taskID, videoTaskQueueReadyKey},
		taskID,
		videoTaskQueueDedupTTL.Milliseconds(),
	).Int()
	return applied == 1, err
}

func (q *videoTaskQueue) Reserve(ctx context.Context) (string, error) {
	if q == nil || q.rdb == nil {
		return "", errors.New("video task queue redis client is nil")
	}
	value, err := videoTaskQueueReserveScript.Run(
		ctx,
		q.rdb,
		[]string{videoTaskQueueReadyKey, videoTaskQueueActiveKey},
		time.Now().UTC().UnixMilli(),
	).Result()
	if errors.Is(err, redis.Nil) {
		return "", service.ErrVideoQueueEmpty
	}
	if err != nil {
		return "", err
	}
	taskID, ok := value.(string)
	if !ok || !service.IsValidVideoTaskID(taskID) {
		if ok && taskID != "" {
			_ = q.rdb.ZRem(ctx, videoTaskQueueActiveKey, taskID).Err()
			_ = q.rdb.Del(ctx, videoTaskQueueQueuedKey+":"+taskID).Err()
		}
		return "", service.ErrVideoQueueInvalidPayload
	}
	return taskID, nil
}

func (q *videoTaskQueue) RequeueAfter(ctx context.Context, taskID string, delay time.Duration) error {
	if !service.IsValidVideoTaskID(taskID) {
		return service.ErrVideoQueueInvalidPayload
	}
	dueAt := int64(0)
	if delay > 0 {
		dueAt = time.Now().UTC().Add(delay).UnixMilli()
	}
	return videoTaskQueueRequeueScript.Run(
		ctx,
		q.rdb,
		[]string{videoTaskQueueActiveKey, videoTaskQueueDelayedKey, videoTaskQueueReadyKey},
		taskID,
		dueAt,
	).Err()
}

func (q *videoTaskQueue) Ack(ctx context.Context, taskID string) error {
	if !service.IsValidVideoTaskID(taskID) {
		return service.ErrVideoQueueInvalidPayload
	}
	return videoTaskQueueAckScript.Run(
		ctx,
		q.rdb,
		[]string{
			videoTaskQueueActiveKey,
			videoTaskQueueDelayedKey,
			videoTaskQueueReadyKey,
			videoTaskQueueQueuedKey + ":" + taskID,
		},
		taskID,
	).Err()
}

func (q *videoTaskQueue) MoveDueToReady(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	return videoTaskQueueMoveDueScript.Run(
		ctx,
		q.rdb,
		[]string{videoTaskQueueDelayedKey, videoTaskQueueReadyKey},
		time.Now().UTC().UnixMilli(),
		limit,
	).Int()
}

func (q *videoTaskQueue) RecoverStale(ctx context.Context, staleAfter time.Duration, limit int) (int, error) {
	if staleAfter <= 0 {
		staleAfter = 2 * time.Minute
	}
	if limit <= 0 {
		limit = 100
	}
	return videoTaskQueueMoveDueScript.Run(
		ctx,
		q.rdb,
		[]string{videoTaskQueueActiveKey, videoTaskQueueReadyKey},
		time.Now().UTC().Add(-staleAfter).UnixMilli(),
		limit,
	).Int()
}

func (q *videoTaskQueue) VideoTaskQueueStats(ctx context.Context) (service.VideoTaskQueueStats, error) {
	if q == nil || q.rdb == nil {
		return service.VideoTaskQueueStats{}, errors.New("video task queue redis client is nil")
	}
	pipe := q.rdb.Pipeline()
	ready := pipe.LLen(ctx, videoTaskQueueReadyKey)
	delayed := pipe.ZCard(ctx, videoTaskQueueDelayedKey)
	active := pipe.ZCard(ctx, videoTaskQueueActiveKey)
	if _, err := pipe.Exec(ctx); err != nil {
		return service.VideoTaskQueueStats{}, err
	}
	return service.VideoTaskQueueStats{
		Ready: ready.Val(), Delayed: delayed.Val(), Active: active.Val(),
	}, nil
}
