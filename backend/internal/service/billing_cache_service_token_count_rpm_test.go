//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// tokenCountRPMCacheStub 记录 IncrementUserGroupRPM 收到的 groupID，验证计数桶与生成 RPM 隔离。
type tokenCountRPMCacheStub struct {
	userRPMCacheStub
	lastGroupID int64
}

func (s *tokenCountRPMCacheStub) IncrementUserGroupRPM(ctx context.Context, userID, groupID int64) (int, error) {
	s.lastGroupID = groupID
	return s.userRPMCacheStub.IncrementUserGroupRPM(ctx, userID, groupID)
}

func newBillingServiceForTokenCount(t *testing.T, cache UserRPMCache, rpm int) *BillingCacheService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Gateway.TokenCountRPM = rpm
	svc := NewBillingCacheService(nil, nil, nil, nil, cache, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	return svc
}

func TestBillingCacheService_CheckTokenCountRate_DisabledSkipsCounter(t *testing.T) {
	cache := &tokenCountRPMCacheStub{}
	svc := newBillingServiceForTokenCount(t, cache, 0)

	for i := 0; i < 5; i++ {
		require.NoError(t, svc.CheckTokenCountRate(context.Background(), 1))
	}
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userGroupCalls), "rpm=0 时不应触碰计数器")
}

func TestBillingCacheService_CheckTokenCountRate_RejectsAboveLimit(t *testing.T) {
	cache := &tokenCountRPMCacheStub{userRPMCacheStub: userRPMCacheStub{userGroupCounts: []int{1, 2, 3}}}
	svc := newBillingServiceForTokenCount(t, cache, 2)

	require.NoError(t, svc.CheckTokenCountRate(context.Background(), 42))
	require.NoError(t, svc.CheckTokenCountRate(context.Background(), 42))
	err := svc.CheckTokenCountRate(context.Background(), 42)
	require.ErrorIs(t, err, ErrTokenCountRPMExceeded)
	require.Equal(t, tokenCountRPMBucketGroupID, cache.lastGroupID, "必须使用独立伪分组桶，不能消耗生成请求 RPM")
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userCalls), "不得触碰 user.rpm_limit 计数器")
}

func TestBillingCacheService_CheckTokenCountRate_RedisErrorFailsClosed(t *testing.T) {
	cache := &tokenCountRPMCacheStub{userRPMCacheStub: userRPMCacheStub{userGroupErr: errors.New("redis down")}}
	svc := newBillingServiceForTokenCount(t, cache, 60)

	require.ErrorIs(t, svc.CheckTokenCountRate(context.Background(), 7), ErrTokenCountRPMExceeded)
}

func TestBillingCacheService_CheckTokenCountRate_MissingCounterFailsClosed(t *testing.T) {
	svc := newBillingServiceForTokenCount(t, nil, 60)
	require.ErrorIs(t, svc.CheckTokenCountRate(context.Background(), 7), ErrTokenCountRPMExceeded)
}
