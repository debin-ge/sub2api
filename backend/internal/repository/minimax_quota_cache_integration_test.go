//go:build integration

package repository

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type MiniMaxQuotaCacheSuite struct {
	IntegrationRedisSuite
	cache service.MiniMaxQuotaCache
}

func (s *MiniMaxQuotaCacheSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.cache = NewMiniMaxQuotaCache(s.rdb)
}

func (s *MiniMaxQuotaCacheSuite) TestReserveAllowsRequestsWithinLimit() {
	allowed, used, err := s.cache.ReserveTextRequest(s.ctx, 1001, "req-1", 2, 5*60*60)
	require.NoError(s.T(), err)
	require.True(s.T(), allowed)
	require.Equal(s.T(), int64(1), used)

	allowed, used, err = s.cache.ReserveTextRequest(s.ctx, 1001, "req-2", 2, 5*60*60)
	require.NoError(s.T(), err)
	require.True(s.T(), allowed)
	require.Equal(s.T(), int64(2), used)
}

func (s *MiniMaxQuotaCacheSuite) TestReserveRejectsRequestsOverLimit() {
	allowed, used, err := s.cache.ReserveTextRequest(s.ctx, 1002, "req-1", 1, 5*60*60)
	require.NoError(s.T(), err)
	require.True(s.T(), allowed)
	require.Equal(s.T(), int64(1), used)

	allowed, used, err = s.cache.ReserveTextRequest(s.ctx, 1002, "req-2", 1, 5*60*60)
	require.NoError(s.T(), err)
	require.False(s.T(), allowed)
	require.Equal(s.T(), int64(1), used)
}

func (s *MiniMaxQuotaCacheSuite) TestReserveRejectsInvalidInputsWithoutWritingRedis() {
	tests := []struct {
		name          string
		accountID     int64
		requestID     string
		limit         int64
		windowSeconds int64
	}{
		{name: "zero_account_id", accountID: 0, requestID: "req-1", limit: 1, windowSeconds: 60},
		{name: "negative_account_id", accountID: -1, requestID: "req-1", limit: 1, windowSeconds: 60},
		{name: "empty_request_id", accountID: 1101, requestID: "", limit: 1, windowSeconds: 60},
		{name: "blank_request_id", accountID: 1101, requestID: " \t\n", limit: 1, windowSeconds: 60},
		{name: "zero_limit", accountID: 1101, requestID: "req-1", limit: 0, windowSeconds: 60},
		{name: "negative_limit", accountID: 1101, requestID: "req-1", limit: -1, windowSeconds: 60},
		{name: "zero_window", accountID: 1101, requestID: "req-1", limit: 1, windowSeconds: 0},
		{name: "negative_window", accountID: 1101, requestID: "req-1", limit: 1, windowSeconds: -60},
		{name: "too_large_window", accountID: 1101, requestID: "req-1", limit: 1, windowSeconds: minimaxQuotaMaxWindowSeconds + 1},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			allowed, used, err := s.cache.ReserveTextRequest(s.ctx, tt.accountID, tt.requestID, tt.limit, tt.windowSeconds)
			require.ErrorContains(s.T(), err, "invalid minimax quota")
			require.False(s.T(), allowed)
			require.Equal(s.T(), int64(0), used)

			keys, err := s.rdb.Keys(s.ctx, "minimax:tokenplan:*").Result()
			require.NoError(s.T(), err)
			require.Empty(s.T(), keys)
		})
	}
}

func (s *MiniMaxQuotaCacheSuite) TestConcurrentReserveAllowsExactlyLimit() {
	const (
		accountID   = int64(1102)
		limit       = int64(7)
		requests    = 25
		windowSecs  = int64(5 * 60 * 60)
		requestBase = "concurrent-req"
	)

	var successCount int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, requests)

	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start

			allowed, _, err := s.cache.ReserveTextRequest(s.ctx, accountID, fmt.Sprintf("%s-%d", requestBase, i), limit, windowSecs)
			if err != nil {
				errs <- err
				return
			}
			if allowed {
				atomic.AddInt64(&successCount, 1)
			}
		}(i)
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(s.T(), err)
	}
	require.Equal(s.T(), limit, successCount)

	count, err := s.cache.CountTextRequests(s.ctx, accountID, windowSecs)
	require.NoError(s.T(), err)
	require.Equal(s.T(), limit, count)
}

func (s *MiniMaxQuotaCacheSuite) TestRollbackDecrementsCount() {
	allowed, used, err := s.cache.ReserveTextRequest(s.ctx, 1003, "req-1", 2, 5*60*60)
	require.NoError(s.T(), err)
	require.True(s.T(), allowed)
	require.Equal(s.T(), int64(1), used)

	allowed, used, err = s.cache.ReserveTextRequest(s.ctx, 1003, "req-2", 2, 5*60*60)
	require.NoError(s.T(), err)
	require.True(s.T(), allowed)
	require.Equal(s.T(), int64(2), used)

	require.NoError(s.T(), s.cache.RollbackTextRequest(s.ctx, 1003, "req-2"))

	count, err := s.cache.CountTextRequests(s.ctx, 1003, 5*60*60)
	require.NoError(s.T(), err)
	require.Equal(s.T(), int64(1), count)
}

func (s *MiniMaxQuotaCacheSuite) TestReserveIsIdempotentForSameRequestID() {
	allowed, used, err := s.cache.ReserveTextRequest(s.ctx, 1004, "req-1", 2, 5*60*60)
	require.NoError(s.T(), err)
	require.True(s.T(), allowed)
	require.Equal(s.T(), int64(1), used)

	allowed, used, err = s.cache.ReserveTextRequest(s.ctx, 1004, "req-1", 2, 5*60*60)
	require.NoError(s.T(), err)
	require.True(s.T(), allowed)
	require.Equal(s.T(), int64(1), used)

	count, err := s.cache.CountTextRequests(s.ctx, 1004, 5*60*60)
	require.NoError(s.T(), err)
	require.Equal(s.T(), int64(1), count)
}

func (s *MiniMaxQuotaCacheSuite) TestCountCleansRequestsOutsideWindow() {
	accountID := int64(1005)
	key := minimaxQuotaTextRequestsKey(accountID)
	now, err := s.rdb.Time(s.ctx).Result()
	require.NoError(s.T(), err)

	require.NoError(s.T(), s.rdb.ZAdd(s.ctx, key,
		redis.Z{Score: float64(now.Unix() - 61), Member: "expired-req"},
		redis.Z{Score: float64(now.Unix() - 1), Member: "active-req"},
	).Err())

	count, err := s.cache.CountTextRequests(s.ctx, accountID, 60)
	require.NoError(s.T(), err)
	require.Equal(s.T(), int64(1), count)

	remaining, err := s.rdb.ZRange(s.ctx, key, 0, -1).Result()
	require.NoError(s.T(), err)
	require.Equal(s.T(), []string{"active-req"}, remaining)
}

func (s *MiniMaxQuotaCacheSuite) TestReserveCleansRequestsOutsideWindowBeforeLimitCheck() {
	accountID := int64(1006)
	key := minimaxQuotaTextRequestsKey(accountID)
	now, err := s.rdb.Time(s.ctx).Result()
	require.NoError(s.T(), err)

	require.NoError(s.T(), s.rdb.ZAdd(s.ctx, key,
		redis.Z{Score: float64(now.Unix() - 61), Member: "expired-req"},
		redis.Z{Score: float64(now.Unix() - 1), Member: "active-req"},
	).Err())

	allowed, used, err := s.cache.ReserveTextRequest(s.ctx, accountID, "new-req", 2, 60)
	require.NoError(s.T(), err)
	require.True(s.T(), allowed)
	require.Equal(s.T(), int64(2), used)
}

func (s *MiniMaxQuotaCacheSuite) TestUsesPerAccountKeys() {
	allowed, used, err := s.cache.ReserveTextRequest(s.ctx, 1007, "req-1", 1, 5*60*60)
	require.NoError(s.T(), err)
	require.True(s.T(), allowed)
	require.Equal(s.T(), int64(1), used)

	allowed, used, err = s.cache.ReserveTextRequest(s.ctx, 1008, "req-1", 1, 5*60*60)
	require.NoError(s.T(), err)
	require.True(s.T(), allowed)
	require.Equal(s.T(), int64(1), used)
}

func (s *MiniMaxQuotaCacheSuite) TestRollbackRejectsInvalidInputsWithoutMutatingRedis() {
	accountID := int64(1103)
	allowed, used, err := s.cache.ReserveTextRequest(s.ctx, accountID, "req-1", 2, 60)
	require.NoError(s.T(), err)
	require.True(s.T(), allowed)
	require.Equal(s.T(), int64(1), used)

	tests := []struct {
		name      string
		accountID int64
		requestID string
	}{
		{name: "zero_account_id", accountID: 0, requestID: "req-1"},
		{name: "negative_account_id", accountID: -1, requestID: "req-1"},
		{name: "empty_request_id", accountID: accountID, requestID: ""},
		{name: "blank_request_id", accountID: accountID, requestID: " \t\n"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			require.ErrorContains(s.T(), s.cache.RollbackTextRequest(s.ctx, tt.accountID, tt.requestID), "invalid minimax quota")

			count, err := s.cache.CountTextRequests(s.ctx, accountID, 60)
			require.NoError(s.T(), err)
			require.Equal(s.T(), int64(1), count)
		})
	}
}

func (s *MiniMaxQuotaCacheSuite) TestCountRejectsInvalidInputsWithoutMutatingRedis() {
	accountID := int64(1104)
	key := minimaxQuotaTextRequestsKey(accountID)
	require.NoError(s.T(), s.rdb.ZAdd(s.ctx, key, redis.Z{Score: 1, Member: "req-1"}).Err())

	tests := []struct {
		name          string
		accountID     int64
		windowSeconds int64
	}{
		{name: "zero_account_id", accountID: 0, windowSeconds: 60},
		{name: "negative_account_id", accountID: -1, windowSeconds: 60},
		{name: "zero_window", accountID: accountID, windowSeconds: 0},
		{name: "negative_window", accountID: accountID, windowSeconds: -60},
		{name: "too_large_window", accountID: accountID, windowSeconds: minimaxQuotaMaxWindowSeconds + 1},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			count, err := s.cache.CountTextRequests(s.ctx, tt.accountID, tt.windowSeconds)
			require.ErrorContains(s.T(), err, "invalid minimax quota")
			require.Equal(s.T(), int64(0), count)

			card, err := s.rdb.ZCard(s.ctx, key).Result()
			require.NoError(s.T(), err)
			require.Equal(s.T(), int64(1), card)
		})
	}
}

func TestMiniMaxQuotaCacheSuite(t *testing.T) {
	suite.Run(t, new(MiniMaxQuotaCacheSuite))
}

func TestMiniMaxQuotaTextRequestsKey(t *testing.T) {
	accountID := int64(1234)
	require.Equal(t, "minimax:tokenplan:"+strconv.FormatInt(accountID, 10)+":text:reqs", minimaxQuotaTextRequestsKey(accountID))
}
