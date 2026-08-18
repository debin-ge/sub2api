package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var goCaptchaRecordFailureScript = redis.NewScript(`
local redis_time = redis.call('TIME')
local now_ms = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
local window_ms = tonumber(ARGV[1])
local cooldown_ms = tonumber(ARGV[2])
local max_failures = tonumber(ARGV[3])
local member = ARGV[4]

redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now_ms - window_ms)
redis.call('ZADD', KEYS[1], now_ms, member)
local count = redis.call('ZCARD', KEYS[1])
redis.call('PEXPIRE', KEYS[1], window_ms)

if count >= max_failures then
  redis.call('PSETEX', KEYS[2], cooldown_ms, '1')
  redis.call('DEL', KEYS[1])
  return {count, 1}
end

return {count, 0}
`)

const (
	goCaptchaChallengeKeyPrefix = "gocaptcha:challenge:"
	goCaptchaTokenKeyPrefix     = "gocaptcha:token:"
	goCaptchaFailKeyPrefix      = "gocaptcha:fail:"
	goCaptchaCooldownKeyPrefix  = "gocaptcha:cooldown:"
)

type goCaptchaCache struct {
	rdb *redis.Client
}

func NewGoCaptchaCache(rdb *redis.Client) service.GoCaptchaCache {
	return &goCaptchaCache{rdb: rdb}
}

func (c *goCaptchaCache) SaveChallenge(ctx context.Context, id string, payload []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, goCaptchaChallengeKeyPrefix+id, payload, ttl).Err()
}

func (c *goCaptchaCache) TakeChallenge(ctx context.Context, id string) ([]byte, error) {
	return c.take(ctx, goCaptchaChallengeKeyPrefix+id)
}

func (c *goCaptchaCache) SaveToken(ctx context.Context, hash string, payload []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, goCaptchaTokenKeyPrefix+hash, payload, ttl).Err()
}

func (c *goCaptchaCache) TakeToken(ctx context.Context, hash string) ([]byte, error) {
	return c.take(ctx, goCaptchaTokenKeyPrefix+hash)
}

// take 原子取出并删除。挑战与令牌都靠它保证一次性：
// 挑战被取走后答错也无法重试，令牌被取走后无法重放。
func (c *goCaptchaCache) take(ctx context.Context, key string) ([]byte, error) {
	val, err := c.rdb.GetDel(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (c *goCaptchaCache) IsCoolingDown(ctx context.Context, ip string) (bool, error) {
	key, ok := goCaptchaIPKey(goCaptchaCooldownKeyPrefix, ip)
	if !ok {
		return false, nil
	}
	n, err := c.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RecordFailure 在 Redis 内原子维护真正的滑动窗口：先删除窗口外记录，
// 再写入本次失败并统计剩余数量；达到阈值时同时进入冷却并清空窗口。
func (c *goCaptchaCache) RecordFailure(
	ctx context.Context,
	ip string,
	maxFailures int,
	window, cooldown time.Duration,
) (int, bool, error) {
	failKey, ok := goCaptchaIPKey(goCaptchaFailKeyPrefix, ip)
	if !ok {
		return 0, false, nil
	}
	if maxFailures <= 0 {
		return 0, false, nil
	}

	cooldownKey, _ := goCaptchaIPKey(goCaptchaCooldownKeyPrefix, ip)
	values, err := goCaptchaRecordFailureScript.Run(
		ctx,
		c.rdb,
		[]string{failKey, cooldownKey},
		durationMilliseconds(window),
		durationMilliseconds(cooldown),
		maxFailures,
		uuid.NewString(),
	).Slice()
	if err != nil {
		return 0, false, err
	}
	if len(values) != 2 {
		return 0, false, fmt.Errorf("record captcha failure returned %d values", len(values))
	}
	count, err := redisInt(values[0])
	if err != nil {
		return 0, false, err
	}
	cooled, err := redisInt(values[1])
	if err != nil {
		return 0, false, err
	}
	return int(count), cooled == 1, nil
}

func (c *goCaptchaCache) ClearFailures(ctx context.Context, ip string) error {
	key, ok := goCaptchaIPKey(goCaptchaFailKeyPrefix, ip)
	if !ok {
		return nil
	}
	return c.rdb.Del(ctx, key).Err()
}

// goCaptchaIPKey 空 IP 不参与失败计数，避免所有拿不到 IP 的请求共用一个计数器而互相误伤。
func goCaptchaIPKey(prefix, ip string) (string, bool) {
	trimmed := strings.TrimSpace(ip)
	if trimmed == "" {
		return "", false
	}
	return prefix + trimmed, true
}

func durationMilliseconds(value time.Duration) int64 {
	milliseconds := value.Milliseconds()
	if milliseconds < 1 {
		return 1
	}
	return milliseconds
}

func redisInt(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse redis integer %q: %w", typed, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unexpected redis integer type %T", value)
	}
}
