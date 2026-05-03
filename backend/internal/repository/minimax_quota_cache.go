package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const minimaxQuotaTextRequestsKeyPrefix = "minimax:tokenplan:"

var (
	minimaxQuotaReserveScript = redis.NewScript(`
		local key = KEYS[1]
		local requestID = ARGV[1]
		local limit = tonumber(ARGV[2])
		local windowSeconds = tonumber(ARGV[3])

		local timeResult = redis.call('TIME')
		local now = tonumber(timeResult[1])
		local cutoff = now - windowSeconds

		redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)

		if redis.call('ZSCORE', key, requestID) then
			return {1, redis.call('ZCARD', key)}
		end

		local count = redis.call('ZCARD', key)
		if count >= limit then
			redis.call('EXPIRE', key, windowSeconds + 60)
			return {0, count}
		end

		redis.call('ZADD', key, now, requestID)
		redis.call('EXPIRE', key, windowSeconds + 60)
		return {1, count + 1}
	`)

	minimaxQuotaCountScript = redis.NewScript(`
		local key = KEYS[1]
		local windowSeconds = tonumber(ARGV[1])

		local timeResult = redis.call('TIME')
		local now = tonumber(timeResult[1])
		local cutoff = now - windowSeconds

		redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
		local count = redis.call('ZCARD', key)
		if count == 0 then
			redis.call('DEL', key)
		else
			redis.call('EXPIRE', key, windowSeconds + 60)
		end
		return count
	`)
)

type minimaxQuotaCache struct {
	rdb *redis.Client
}

func NewMiniMaxQuotaCache(rdb *redis.Client) service.MiniMaxQuotaCache {
	return &minimaxQuotaCache{rdb: rdb}
}

func (c *minimaxQuotaCache) ReserveTextRequest(ctx context.Context, accountID int64, requestID string, limit int64, windowSeconds int64) (bool, int64, error) {
	result, err := minimaxQuotaReserveScript.Run(ctx, c.rdb, []string{minimaxQuotaTextRequestsKey(accountID)}, requestID, limit, windowSeconds).Result()
	if err != nil {
		return false, 0, err
	}

	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return false, 0, fmt.Errorf("unexpected minimax quota reserve result: %T", result)
	}

	allowed, err := redisInt64(values[0])
	if err != nil {
		return false, 0, fmt.Errorf("parse minimax quota reserve allowed: %w", err)
	}
	used, err := redisInt64(values[1])
	if err != nil {
		return false, 0, fmt.Errorf("parse minimax quota reserve used: %w", err)
	}

	return allowed == 1, used, nil
}

func (c *minimaxQuotaCache) RollbackTextRequest(ctx context.Context, accountID int64, requestID string) error {
	return c.rdb.ZRem(ctx, minimaxQuotaTextRequestsKey(accountID), requestID).Err()
}

func (c *minimaxQuotaCache) CountTextRequests(ctx context.Context, accountID int64, windowSeconds int64) (int64, error) {
	return minimaxQuotaCountScript.Run(ctx, c.rdb, []string{minimaxQuotaTextRequestsKey(accountID)}, windowSeconds).Int64()
}

func minimaxQuotaTextRequestsKey(accountID int64) string {
	return minimaxQuotaTextRequestsKeyPrefix + strconv.FormatInt(accountID, 10) + ":text:reqs"
}

func redisInt64(value interface{}) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected redis integer type %T", value)
	}
}
