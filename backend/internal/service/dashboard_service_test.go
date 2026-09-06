package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type usageRepoStub struct {
	UsageLogRepository
	stats      *usagestats.DashboardStats
	rangeStats *usagestats.DashboardStats
	err        error
	rangeErr   error
	calls      int32
	rangeCalls int32
	rangeStart time.Time
	rangeEnd   time.Time
	onCall     chan struct{}
	tzMu       sync.Mutex
	seenTZ     []string
}

func (s *usageRepoStub) recordTZ(userTZ string) {
	s.tzMu.Lock()
	s.seenTZ = append(s.seenTZ, userTZ)
	s.tzMu.Unlock()
}

func (s *usageRepoStub) timezonesSeen() []string {
	s.tzMu.Lock()
	defer s.tzMu.Unlock()
	return append([]string(nil), s.seenTZ...)
}

func (s *usageRepoStub) GetDashboardStats(ctx context.Context, userTZ string) (*usagestats.DashboardStats, error) {
	atomic.AddInt32(&s.calls, 1)
	s.recordTZ(userTZ)
	if s.onCall != nil {
		select {
		case s.onCall <- struct{}{}:
		default:
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.stats, nil
}

func (s *usageRepoStub) GetDashboardStatsWithRange(ctx context.Context, start, end time.Time, userTZ string) (*usagestats.DashboardStats, error) {
	atomic.AddInt32(&s.rangeCalls, 1)
	s.recordTZ(userTZ)
	s.rangeStart = start
	s.rangeEnd = end
	if s.rangeErr != nil {
		return nil, s.rangeErr
	}
	if s.rangeStats != nil {
		return s.rangeStats, nil
	}
	return s.stats, nil
}

type dashboardCacheStub struct {
	get       func(ctx context.Context, scope string) (string, error)
	set       func(ctx context.Context, scope string, data string, ttl time.Duration) error
	del       func(ctx context.Context, scope string) error
	getCalls  int32
	setCalls  int32
	delCalls  int32
	lastSetMu sync.Mutex
	lastSet   string
	setScopes []string
}

func (c *dashboardCacheStub) GetDashboardStats(ctx context.Context, scope string) (string, error) {
	atomic.AddInt32(&c.getCalls, 1)
	if c.get != nil {
		return c.get(ctx, scope)
	}
	return "", ErrDashboardStatsCacheMiss
}

func (c *dashboardCacheStub) SetDashboardStats(ctx context.Context, scope string, data string, ttl time.Duration) error {
	atomic.AddInt32(&c.setCalls, 1)
	c.lastSetMu.Lock()
	c.lastSet = data
	c.setScopes = append(c.setScopes, scope)
	c.lastSetMu.Unlock()
	if c.set != nil {
		return c.set(ctx, scope, data, ttl)
	}
	return nil
}

func (c *dashboardCacheStub) DeleteDashboardStats(ctx context.Context, scope string) error {
	atomic.AddInt32(&c.delCalls, 1)
	if c.del != nil {
		return c.del(ctx, scope)
	}
	return nil
}

func (c *dashboardCacheStub) scopesSet() []string {
	c.lastSetMu.Lock()
	defer c.lastSetMu.Unlock()
	return append([]string(nil), c.setScopes...)
}

type dashboardAggregationRepoStub struct {
	watermark time.Time
	err       error
}

func (s *dashboardAggregationRepoStub) AggregateRange(ctx context.Context, start, end time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) RecomputeRange(ctx context.Context, start, end time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) GetAggregationWatermark(ctx context.Context) (time.Time, error) {
	if s.err != nil {
		return time.Time{}, s.err
	}
	return s.watermark, nil
}

func (s *dashboardAggregationRepoStub) UpdateAggregationWatermark(ctx context.Context, aggregatedAt time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) CleanupAggregates(ctx context.Context, hourlyCutoff, dailyCutoff time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) CleanupUsageLogs(ctx context.Context, cutoff time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) CleanupUsageBillingDedup(ctx context.Context, cutoff time.Time) error {
	return nil
}

func (s *dashboardAggregationRepoStub) EnsureUsageLogsPartitions(ctx context.Context, now time.Time) error {
	return nil
}

func (c *dashboardCacheStub) readLastEntry(t *testing.T) dashboardStatsCacheEntry {
	t.Helper()
	c.lastSetMu.Lock()
	data := c.lastSet
	c.lastSetMu.Unlock()

	var entry dashboardStatsCacheEntry
	err := json.Unmarshal([]byte(data), &entry)
	require.NoError(t, err)
	return entry
}

func TestDashboardService_CacheHitFresh(t *testing.T) {
	stats := &usagestats.DashboardStats{
		TotalUsers:     10,
		StatsUpdatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		StatsStale:     true,
	}
	entry := dashboardStatsCacheEntry{
		Stats:     stats,
		UpdatedAt: time.Now().Unix(),
	}
	payload, err := json.Marshal(entry)
	require.NoError(t, err)

	cache := &dashboardCacheStub{
		get: func(ctx context.Context, scope string) (string, error) {
			return string(payload), nil
		},
	}
	repo := &usageRepoStub{
		stats: &usagestats.DashboardStats{TotalUsers: 99},
	}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: true},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	got, err := svc.GetDashboardStats(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, stats, got)
	require.Equal(t, int32(0), atomic.LoadInt32(&repo.calls))
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.getCalls))
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.setCalls))
}

func TestDashboardService_CacheMiss_StoresCache(t *testing.T) {
	stats := &usagestats.DashboardStats{
		TotalUsers:     7,
		StatsUpdatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		StatsStale:     true,
	}
	cache := &dashboardCacheStub{
		get: func(ctx context.Context, scope string) (string, error) {
			return "", ErrDashboardStatsCacheMiss
		},
	}
	repo := &usageRepoStub{stats: stats}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: true},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	got, err := svc.GetDashboardStats(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, stats, got)
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.calls))
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.getCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.setCalls))
	entry := cache.readLastEntry(t)
	require.Equal(t, stats, entry.Stats)
	require.WithinDuration(t, time.Now(), time.Unix(entry.UpdatedAt, 0), time.Second)
}

// Callers in different timezones see different "today" figures, so the cache
// must be scoped per today-window: distinct windows get distinct entries and
// the repo is asked with each caller's timezone, while zones that share an
// offset (and therefore a window) reuse a single entry.
func TestDashboardService_CacheScopedByCallerTodayWindow(t *testing.T) {
	stats := &usagestats.DashboardStats{TotalUsers: 5}
	cache := &dashboardCacheStub{}
	repo := &usageRepoStub{stats: stats}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard:    config.DashboardCacheConfig{Enabled: true},
		DashboardAgg: config.DashboardAggregationConfig{Enabled: true},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	_, err := svc.GetDashboardStats(context.Background(), "UTC")
	require.NoError(t, err)
	_, err = svc.GetDashboardStats(context.Background(), "Asia/Shanghai")
	require.NoError(t, err)

	require.Equal(t, []string{"UTC", "Asia/Shanghai"}, repo.timezonesSeen())
	scopes := cache.scopesSet()
	require.Len(t, scopes, 2)
	require.NotEqual(t, scopes[0], scopes[1], "UTC and Asia/Shanghai never share a local day boundary")
	require.Equal(t, DashboardStatsCacheScope("UTC"), scopes[0])
	require.Equal(t, DashboardStatsCacheScope("Asia/Shanghai"), scopes[1])

	// Same offset ⇒ same window ⇒ same cache entry.
	require.Equal(t, DashboardStatsCacheScope("Asia/Shanghai"), DashboardStatsCacheScope("Asia/Singapore"))
	// Empty and unknown zones fall back to the server timezone's window.
	require.Equal(t, DashboardStatsCacheScope(""), DashboardStatsCacheScope("Not/AZone"))
}

func TestDashboardService_CacheDisabled_SkipsCache(t *testing.T) {
	stats := &usagestats.DashboardStats{
		TotalUsers:     3,
		StatsUpdatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		StatsStale:     true,
	}
	cache := &dashboardCacheStub{
		get: func(ctx context.Context, scope string) (string, error) {
			return "", nil
		},
	}
	repo := &usageRepoStub{stats: stats}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: false},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	got, err := svc.GetDashboardStats(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, stats, got)
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.calls))
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.getCalls))
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.setCalls))
}

func TestDashboardService_CacheHitStale_TriggersAsyncRefresh(t *testing.T) {
	staleStats := &usagestats.DashboardStats{
		TotalUsers:     11,
		StatsUpdatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		StatsStale:     true,
	}
	entry := dashboardStatsCacheEntry{
		Stats:     staleStats,
		UpdatedAt: time.Now().Add(-defaultDashboardStatsFreshTTL * 2).Unix(),
	}
	payload, err := json.Marshal(entry)
	require.NoError(t, err)

	cache := &dashboardCacheStub{
		get: func(ctx context.Context, scope string) (string, error) {
			return string(payload), nil
		},
	}
	refreshCh := make(chan struct{}, 1)
	repo := &usageRepoStub{
		stats:  &usagestats.DashboardStats{TotalUsers: 22},
		onCall: refreshCh,
	}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: true},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	got, err := svc.GetDashboardStats(context.Background(), "America/New_York")
	require.NoError(t, err)
	require.Equal(t, staleStats, got)

	select {
	case <-refreshCh:
	case <-time.After(1 * time.Second):
		t.Fatal("等待异步刷新超时")
	}
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&cache.setCalls) >= 1
	}, 1*time.Second, 10*time.Millisecond)
	// The background refresh must recompute for the same caller timezone and
	// write back under the same scope it was served from.
	require.Equal(t, []string{"America/New_York"}, repo.timezonesSeen())
	require.Equal(t, []string{DashboardStatsCacheScope("America/New_York")}, cache.scopesSet())
}

func TestDashboardService_CacheParseError_EvictsAndRefetches(t *testing.T) {
	cache := &dashboardCacheStub{
		get: func(ctx context.Context, scope string) (string, error) {
			return "not-json", nil
		},
	}
	stats := &usagestats.DashboardStats{TotalUsers: 9}
	repo := &usageRepoStub{stats: stats}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: true},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	got, err := svc.GetDashboardStats(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, stats, got)
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.delCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.calls))
}

func TestDashboardService_CacheParseError_RepoFailure(t *testing.T) {
	cache := &dashboardCacheStub{
		get: func(ctx context.Context, scope string) (string, error) {
			return "not-json", nil
		},
	}
	repo := &usageRepoStub{err: errors.New("db down")}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: true},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: true,
		},
	}
	svc := NewDashboardService(repo, aggRepo, cache, cfg)

	_, err := svc.GetDashboardStats(context.Background(), "")
	require.Error(t, err)
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.delCalls))
}

func TestDashboardService_StatsUpdatedAtEpochWhenMissing(t *testing.T) {
	stats := &usagestats.DashboardStats{}
	repo := &usageRepoStub{stats: stats}
	aggRepo := &dashboardAggregationRepoStub{watermark: time.Unix(0, 0).UTC()}
	cfg := &config.Config{Dashboard: config.DashboardCacheConfig{Enabled: false}}
	svc := NewDashboardService(repo, aggRepo, nil, cfg)

	got, err := svc.GetDashboardStats(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "1970-01-01T00:00:00Z", got.StatsUpdatedAt)
	require.True(t, got.StatsStale)
}

func TestDashboardService_StatsStaleFalseWhenFresh(t *testing.T) {
	aggNow := time.Now().UTC().Truncate(time.Second)
	stats := &usagestats.DashboardStats{}
	repo := &usageRepoStub{stats: stats}
	aggRepo := &dashboardAggregationRepoStub{watermark: aggNow}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: false},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled:         true,
			IntervalSeconds: 60,
			LookbackSeconds: 120,
		},
	}
	svc := NewDashboardService(repo, aggRepo, nil, cfg)

	got, err := svc.GetDashboardStats(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, aggNow.Format(time.RFC3339), got.StatsUpdatedAt)
	require.False(t, got.StatsStale)
}

func TestDashboardService_AggDisabled_UsesUsageLogsFallback(t *testing.T) {
	expected := &usagestats.DashboardStats{TotalUsers: 42}
	repo := &usageRepoStub{
		rangeStats: expected,
		err:        errors.New("should not call aggregated stats"),
	}
	cfg := &config.Config{
		Dashboard: config.DashboardCacheConfig{Enabled: false},
		DashboardAgg: config.DashboardAggregationConfig{
			Enabled: false,
			Retention: config.DashboardAggregationRetentionConfig{
				UsageLogsDays: 7,
			},
		},
	}
	svc := NewDashboardService(repo, nil, nil, cfg)

	got, err := svc.GetDashboardStats(context.Background(), "Asia/Tokyo")
	require.NoError(t, err)
	require.Equal(t, int64(42), got.TotalUsers)
	require.Equal(t, int32(0), atomic.LoadInt32(&repo.calls))
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.rangeCalls))
	require.False(t, repo.rangeEnd.IsZero())
	require.Equal(t, truncateToDayUTC(repo.rangeEnd.AddDate(0, 0, -7)), repo.rangeStart)
	// The raw fallback must still anchor "today" on the caller's timezone.
	require.Equal(t, []string{"Asia/Tokyo"}, repo.timezonesSeen())
}
