//go:build integration

package repository

import (
	"strconv"
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
		redis.Z{Score: float64(now.Unix() - 11), Member: "expired-req"},
		redis.Z{Score: float64(now.Unix() - 5), Member: "active-req"},
	).Err())

	count, err := s.cache.CountTextRequests(s.ctx, accountID, 10)
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
		redis.Z{Score: float64(now.Unix() - 11), Member: "expired-req"},
		redis.Z{Score: float64(now.Unix() - 5), Member: "active-req"},
	).Err())

	allowed, used, err := s.cache.ReserveTextRequest(s.ctx, accountID, "new-req", 2, 10)
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

func TestMiniMaxQuotaCacheSuite(t *testing.T) {
	suite.Run(t, new(MiniMaxQuotaCacheSuite))
}

func TestMiniMaxQuotaTextRequestsKey(t *testing.T) {
	accountID := int64(1234)
	require.Equal(t, "minimax:tokenplan:"+strconv.FormatInt(accountID, 10)+":text:reqs", minimaxQuotaTextRequestsKey(accountID))
}
