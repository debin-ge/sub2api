package repository

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// dashboardStatsCacheKeyPrefix is followed by the caller-specific scope (see
// service.DashboardStatsCacheScope). v2: entries became per-"today"-window when
// the dashboard started honouring the viewer's timezone.
const dashboardStatsCacheKeyPrefix = "dashboard:stats:v2:"

type dashboardCache struct {
	rdb       *redis.Client
	keyPrefix string
}

func NewDashboardCache(rdb *redis.Client, cfg *config.Config) service.DashboardStatsCache {
	prefix := "sub2api:"
	if cfg != nil {
		prefix = strings.TrimSpace(cfg.Dashboard.KeyPrefix)
	}
	if prefix != "" && !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	return &dashboardCache{
		rdb:       rdb,
		keyPrefix: prefix,
	}
}

func (c *dashboardCache) GetDashboardStats(ctx context.Context, scope string) (string, error) {
	val, err := c.rdb.Get(ctx, c.buildKey(scope)).Result()
	if err != nil {
		if err == redis.Nil {
			return "", service.ErrDashboardStatsCacheMiss
		}
		return "", err
	}
	return val, nil
}

func (c *dashboardCache) SetDashboardStats(ctx context.Context, scope string, data string, ttl time.Duration) error {
	return c.rdb.Set(ctx, c.buildKey(scope), data, ttl).Err()
}

func (c *dashboardCache) buildKey(scope string) string {
	key := dashboardStatsCacheKeyPrefix + scope
	if c.keyPrefix == "" {
		return key
	}
	return c.keyPrefix + key
}

func (c *dashboardCache) DeleteDashboardStats(ctx context.Context, scope string) error {
	return c.rdb.Del(ctx, c.buildKey(scope)).Err()
}
