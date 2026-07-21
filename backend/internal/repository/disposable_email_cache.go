package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	// Redis key for disposable email domains
	redisKeyDisposableDomains = "disposable_email:domains"
	// 刷新黑名单的分布式锁 key
	redisKeyDisposableRefreshLock = redisKeyDisposableDomains + ":refresh_lock"
	// 批量写入时每批的域名数量，避免单次命令过大
	disposableDomainsBatchSize = 1000
)

type disposableEmailCache struct {
	rdb *redis.Client
}

// NewDisposableEmailCache 创建临时邮箱黑名单缓存（Redis 实现）
func NewDisposableEmailCache(rdb *redis.Client) service.DisposableEmailCache {
	return &disposableEmailCache{rdb: rdb}
}

func (c *disposableEmailCache) ReplaceDomains(ctx context.Context, domains []string, ttl time.Duration) error {
	pipe := c.rdb.Pipeline()

	// 删除旧数据后分批重建 Set
	pipe.Del(ctx, redisKeyDisposableDomains)
	for i := 0; i < len(domains); i += disposableDomainsBatchSize {
		end := i + disposableDomainsBatchSize
		if end > len(domains) {
			end = len(domains)
		}
		batch := domains[i:end]
		members := make([]any, len(batch))
		for j, domain := range batch {
			members[j] = domain
		}
		pipe.SAdd(ctx, redisKeyDisposableDomains, members...)
	}
	pipe.Expire(ctx, redisKeyDisposableDomains, ttl)

	_, err := pipe.Exec(ctx)
	return err
}

func (c *disposableEmailCache) IsDisposableDomain(ctx context.Context, domain string) (bool, error) {
	return c.rdb.SIsMember(ctx, redisKeyDisposableDomains, domain).Result()
}

func (c *disposableEmailCache) DomainCount(ctx context.Context) (int64, error) {
	return c.rdb.SCard(ctx, redisKeyDisposableDomains).Result()
}

func (c *disposableEmailCache) DomainsTTL(ctx context.Context) (time.Duration, error) {
	return c.rdb.TTL(ctx, redisKeyDisposableDomains).Result()
}

func (c *disposableEmailCache) AcquireRefreshLock(ctx context.Context, ttl time.Duration) (bool, error) {
	return c.rdb.SetNX(ctx, redisKeyDisposableRefreshLock, "1", ttl).Result()
}

func (c *disposableEmailCache) ReleaseRefreshLock(ctx context.Context) {
	c.rdb.Del(ctx, redisKeyDisposableRefreshLock)
}
