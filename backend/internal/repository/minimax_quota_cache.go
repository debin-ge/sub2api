package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const minimaxQuotaTextRequestsKeyPrefix = "minimax:tokenplan:"
const minimaxQuotaExpiryBufferSeconds int64 = 60
const minimaxQuotaMaxWindowSeconds int64 = 7 * 24 * 60 * 60

var (
	minimaxQuotaReserveScript = redis.NewScript(`
		local key = KEYS[1]
		local requestID = ARGV[1]
		local limit = tonumber(ARGV[2])
		local windowSeconds = tonumber(ARGV[3])
		local accountID = tonumber(ARGV[4])
		local maxWindowSeconds = tonumber(ARGV[5])
		local expiryBufferSeconds = tonumber(ARGV[6])

		if accountID == nil or accountID <= 0 then
			return redis.error_reply('invalid minimax quota account id')
		end
		if requestID == nil or string.match(requestID, '^%s*$') then
			return redis.error_reply('invalid minimax quota request id')
		end
		if limit == nil or limit <= 0 then
			return redis.error_reply('invalid minimax quota limit')
		end
		if maxWindowSeconds == nil or maxWindowSeconds <= 0 then
			return redis.error_reply('invalid minimax quota max window seconds')
		end
		if expiryBufferSeconds == nil or expiryBufferSeconds < 0 then
			return redis.error_reply('invalid minimax quota expiry buffer seconds')
		end
		if windowSeconds == nil or windowSeconds <= 0 or windowSeconds > maxWindowSeconds then
			return redis.error_reply('invalid minimax quota window seconds')
		end
		local ttlSeconds = windowSeconds + expiryBufferSeconds
		if ttlSeconds <= 0 then
			return redis.error_reply('invalid minimax quota ttl seconds')
		end

		local timeResult = redis.call('TIME')
		local now = tonumber(timeResult[1])
		local cutoff = now - windowSeconds

		redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)

		if redis.call('ZSCORE', key, requestID) then
			return {1, redis.call('ZCARD', key)}
		end

		local count = redis.call('ZCARD', key)
		if count >= limit then
			redis.call('EXPIRE', key, ttlSeconds)
			return {0, count}
		end

		redis.call('ZADD', key, now, requestID)
		redis.call('EXPIRE', key, ttlSeconds)
		return {1, count + 1}
	`)

	minimaxQuotaCountScript = redis.NewScript(`
		local key = KEYS[1]
		local windowSeconds = tonumber(ARGV[1])
		local accountID = tonumber(ARGV[2])
		local maxWindowSeconds = tonumber(ARGV[3])
		local expiryBufferSeconds = tonumber(ARGV[4])

		if accountID == nil or accountID <= 0 then
			return redis.error_reply('invalid minimax quota account id')
		end
		if maxWindowSeconds == nil or maxWindowSeconds <= 0 then
			return redis.error_reply('invalid minimax quota max window seconds')
		end
		if expiryBufferSeconds == nil or expiryBufferSeconds < 0 then
			return redis.error_reply('invalid minimax quota expiry buffer seconds')
		end
		if windowSeconds == nil or windowSeconds <= 0 or windowSeconds > maxWindowSeconds then
			return redis.error_reply('invalid minimax quota window seconds')
		end
		local ttlSeconds = windowSeconds + expiryBufferSeconds
		if ttlSeconds <= 0 then
			return redis.error_reply('invalid minimax quota ttl seconds')
		end

		local timeResult = redis.call('TIME')
		local now = tonumber(timeResult[1])
		local cutoff = now - windowSeconds

		redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
		local count = redis.call('ZCARD', key)
		if count == 0 then
			redis.call('DEL', key)
		else
			redis.call('EXPIRE', key, ttlSeconds)
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
	if err := validateMiniMaxQuotaAccountID(accountID); err != nil {
		return false, 0, err
	}
	if err := validateMiniMaxQuotaRequestID(requestID); err != nil {
		return false, 0, err
	}
	if limit <= 0 {
		return false, 0, fmt.Errorf("invalid minimax quota limit: %d", limit)
	}
	if err := validateMiniMaxQuotaWindowSeconds(windowSeconds); err != nil {
		return false, 0, err
	}

	result, err := minimaxQuotaReserveScript.Run(ctx, c.rdb, []string{minimaxQuotaTextRequestsKey(accountID)}, requestID, limit, windowSeconds, accountID, minimaxQuotaMaxWindowSeconds, minimaxQuotaExpiryBufferSeconds).Result()
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
	if err := validateMiniMaxQuotaAccountID(accountID); err != nil {
		return err
	}
	if err := validateMiniMaxQuotaRequestID(requestID); err != nil {
		return err
	}

	return c.rdb.ZRem(ctx, minimaxQuotaTextRequestsKey(accountID), requestID).Err()
}

func (c *minimaxQuotaCache) CountTextRequests(ctx context.Context, accountID int64, windowSeconds int64) (int64, error) {
	if err := validateMiniMaxQuotaAccountID(accountID); err != nil {
		return 0, err
	}
	if err := validateMiniMaxQuotaWindowSeconds(windowSeconds); err != nil {
		return 0, err
	}

	return minimaxQuotaCountScript.Run(ctx, c.rdb, []string{minimaxQuotaTextRequestsKey(accountID)}, windowSeconds, accountID, minimaxQuotaMaxWindowSeconds, minimaxQuotaExpiryBufferSeconds).Int64()
}

func minimaxQuotaTextRequestsKey(accountID int64) string {
	return minimaxQuotaTextRequestsKeyPrefix + strconv.FormatInt(accountID, 10) + ":text:reqs"
}

func validateMiniMaxQuotaAccountID(accountID int64) error {
	if accountID <= 0 {
		return fmt.Errorf("invalid minimax quota account id: %d", accountID)
	}
	return nil
}

func validateMiniMaxQuotaRequestID(requestID string) error {
	if strings.TrimSpace(requestID) == "" {
		return fmt.Errorf("invalid minimax quota request id")
	}
	return nil
}

func validateMiniMaxQuotaWindowSeconds(windowSeconds int64) error {
	if windowSeconds <= 0 || windowSeconds > minimaxQuotaMaxWindowSeconds {
		return fmt.Errorf("invalid minimax quota window seconds: %d", windowSeconds)
	}
	return nil
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
