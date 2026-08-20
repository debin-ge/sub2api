package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const modelPriceOverrideRefreshChannel = "model_price_overrides_updated"

type modelPriceOverrideCache struct {
	rdb *redis.Client
}

func NewModelPriceOverrideCache(rdb *redis.Client) service.ModelPriceOverrideCache {
	return &modelPriceOverrideCache{rdb: rdb}
}

func (c *modelPriceOverrideCache) BroadcastRefresh(ctx context.Context) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Publish(ctx, modelPriceOverrideRefreshChannel, "refresh").Err()
}

func (c *modelPriceOverrideCache) SubscribeRefresh(ctx context.Context, handler func()) {
	if c == nil || c.rdb == nil || handler == nil {
		return
	}
	go func() {
		sub := c.rdb.Subscribe(ctx, modelPriceOverrideRefreshChannel)
		defer func() { _ = sub.Close() }()
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok || msg == nil {
					return
				}
				handler()
			}
		}
	}()
}
