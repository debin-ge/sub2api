package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// These scripts use the existing standalone Redis client and preserve the
// image_task:<id> keys so previously accepted tasks remain readable. Deploying
// Redis Cluster would require co-locating task and owner keys before enabling
// these multi-key operations.
var imageTaskAcquirePendingScript = redis.NewScript(`
local time = redis.call('TIME')
local now = tonumber(time[1]) * 1000 + math.floor(tonumber(time[2]) / 1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
if redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 1 end
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[2]) then return 0 end
redis.call('ZADD', KEYS[1], now + tonumber(ARGV[3]), ARGV[1])
local previousTTL = redis.call('PTTL', KEYS[1])
if previousTTL < tonumber(ARGV[3]) then redis.call('PEXPIRE', KEYS[1], ARGV[3]) end
return 1
`)

var imageTaskCommitPendingScript = redis.NewScript(`
local time = redis.call('TIME')
local now = tonumber(time[1]) * 1000 + math.floor(tonumber(time[2]) / 1000)
local expires = redis.call('ZSCORE', KEYS[1], ARGV[1])
if not expires or tonumber(expires) <= now then return 0 end
local existing = redis.call('GET', KEYS[2])
if existing then
  if existing == ARGV[2] then return 1 end
  return -1
end
redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3])
return 1
`)

var imageTaskReleasePendingScript = redis.NewScript(`
redis.call('ZREM', KEYS[1], ARGV[1])
if redis.call('ZCARD', KEYS[1]) == 0 then redis.call('DEL', KEYS[1]) end
return 1
`)

var imageTaskFinishScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return 0 end
local current = cjson.decode(raw)
if current.id ~= ARGV[1] or current.user_id ~= tonumber(ARGV[2]) or current.api_key_id ~= tonumber(ARGV[3]) then return -1 end
if (current.pending_token or '') ~= ARGV[4] then return -1 end
if current.status ~= 'processing' then return 2 end
redis.call('SET', KEYS[1], ARGV[5], 'PX', ARGV[6])
if ARGV[4] ~= '' then
  redis.call('ZREM', KEYS[2], ARGV[1] .. ':' .. ARGV[4])
  if redis.call('ZCARD', KEYS[2]) == 0 then redis.call('DEL', KEYS[2]) end
end
return 1
`)

func imageTaskPendingKey(userID int64) string {
	return "image_task:pending:" + strconv.FormatInt(userID, 10)
}

func imageTaskPendingMember(id, token string) string { return id + ":" + token }

func (s *imageTaskStore) AcquireImageTaskPending(ctx context.Context, reservation service.ImageTaskReservation, limit int, ttl time.Duration) (bool, error) {
	if reservation.Owner.UserID <= 0 || reservation.Owner.APIKeyID <= 0 || reservation.ID == "" || reservation.Token == "" || limit <= 0 || ttl < time.Millisecond {
		return false, service.ErrImageTaskUnavailable
	}
	result, err := imageTaskAcquirePendingScript.Run(ctx, s.rdb, []string{imageTaskPendingKey(reservation.Owner.UserID)},
		imageTaskPendingMember(reservation.ID, reservation.Token), limit, ttl.Milliseconds()).Int()
	return result == 1, err
}

func (s *imageTaskStore) CommitImageTaskPending(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	if task == nil || task.UserID <= 0 || task.APIKeyID <= 0 || task.PendingToken == "" || task.Status != service.ImageTaskStatusProcessing || ttl < time.Millisecond {
		return service.ErrImageTaskUnavailable
	}
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	result, err := imageTaskCommitPendingScript.Run(ctx, s.rdb, []string{imageTaskPendingKey(task.UserID), imageTaskKey(task.ID)},
		imageTaskPendingMember(task.ID, task.PendingToken), data, ttl.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return service.ErrImageTaskReservationExpired
	}
	return nil
}

func (s *imageTaskStore) ReleaseImageTaskPending(ctx context.Context, reservation service.ImageTaskReservation) error {
	if reservation.Owner.UserID <= 0 || reservation.ID == "" || reservation.Token == "" {
		return service.ErrImageTaskUnavailable
	}
	return imageTaskReleasePendingScript.Run(ctx, s.rdb, []string{imageTaskPendingKey(reservation.Owner.UserID)},
		imageTaskPendingMember(reservation.ID, reservation.Token)).Err()
}

func (s *imageTaskStore) FinishImageTask(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	if task == nil || (task.Status != service.ImageTaskStatusCompleted && task.Status != service.ImageTaskStatusFailed) || ttl < time.Millisecond {
		return service.ErrImageTaskUnavailable
	}
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	result, err := imageTaskFinishScript.Run(ctx, s.rdb, []string{imageTaskKey(task.ID), imageTaskPendingKey(task.UserID)},
		task.ID, task.UserID, task.APIKeyID, task.PendingToken, data, ttl.Milliseconds()).Int()
	if err != nil {
		return err
	}
	switch result {
	case 0:
		return service.ErrImageTaskNotFound
	case -1:
		return service.ErrImageTaskForbidden
	case 1, 2:
		return nil
	default:
		return errors.New("unexpected image task finish result")
	}
}

var _ service.ImageTaskPendingStore = (*imageTaskStore)(nil)
var _ service.ImageTaskTerminalStore = (*imageTaskStore)(nil)
