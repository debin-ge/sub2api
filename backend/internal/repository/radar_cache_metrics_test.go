package repository

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type radarRedisCommandHook struct {
	mu          sync.Mutex
	commands    []string
	commandArgs [][]any
	failCommand string
	failOnce    bool
}

func (*radarRedisCommandHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *radarRedisCommandHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.mu.Lock()
		h.commands = append(h.commands, cmd.Name())
		h.commandArgs = append(h.commandArgs, append([]any(nil), cmd.Args()...))
		fail := h.failOnce && cmd.Name() == h.failCommand
		if fail {
			h.failOnce = false
		}
		h.mu.Unlock()
		if fail {
			return errors.New("injected radar redis command failure")
		}
		return next(ctx, cmd)
	}
}
func (h *radarRedisCommandHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		var fail redis.Cmder
		h.mu.Lock()
		for _, cmd := range cmds {
			h.commands = append(h.commands, cmd.Name())
			h.commandArgs = append(h.commandArgs, append([]any(nil), cmd.Args()...))
			if fail == nil && h.failOnce && cmd.Name() == h.failCommand {
				fail = cmd
				h.failOnce = false
			}
		}
		h.mu.Unlock()
		err := next(ctx, cmds)
		if fail != nil {
			injected := errors.New("injected radar redis pipeline failure")
			fail.SetErr(injected)
			return injected
		}
		return err
	}
}
func (h *radarRedisCommandHook) reset() {
	h.mu.Lock()
	h.commands = nil
	h.commandArgs = nil
	h.mu.Unlock()
}

func (h *radarRedisCommandHook) argsSnapshot() [][]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([][]any, len(h.commandArgs))
	for index := range h.commandArgs {
		result[index] = append([]any(nil), h.commandArgs[index]...)
	}
	return result
}
func (h *radarRedisCommandHook) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.commands...)
}

func (h *radarRedisCommandHook) failNext(command string) {
	h.mu.Lock()
	h.failCommand = command
	h.failOnce = true
	h.mu.Unlock()
}

func TestRadarCacheRepositoryRecordsRedisResultsAndBoundedCacheMemory(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewRadarMetrics(registry)
	require.NoError(t, err)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	repoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	repo := repoValue.(*radarCacheRepository)
	repo.metrics = metrics

	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, repo.AppendBucketSnapshot(context.Background(), testRadarSnapshot("anthropic/private-plan", now)))
	_, err = repo.GetLatestBucket(context.Background(), "anthropic/private-plan")
	require.NoError(t, err)
	snapshot, err := repo.GetRadarMetricsSnapshot(context.Background())
	require.NoError(t, err)
	metrics.SetCacheMemoryTotals(snapshot.CacheMemoryBytes)

	require.NoError(t, rdb.Close())
	_, err = repo.GetSourcePayload(context.Background(), service.RadarSourceAA)
	require.Error(t, err)

	recorder := httptest.NewRecorder()
	observability.MetricsHandler(registry).ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()
	require.Contains(t, body, `radar_redis_operations_total{access="write",operation="append_bucket",result="success"} 1`)
	require.Contains(t, body, `radar_redis_operations_total{access="read",operation="get_latest_bucket",result="success"} 1`)
	require.Contains(t, body, `radar_redis_operations_total{access="read",operation="get_source",result="error"} 1`)
	require.Contains(t, body, `radar_cache_memory_bytes{cache="quota_bucket"}`)
	require.NotContains(t, body, "private-plan")
}

func TestRadarCacheMemoryRefreshHydratesCurrentFamilyTotals(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewRadarMetrics(registry)
	require.NoError(t, err)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	repoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	repo := repoValue.(*radarCacheRepository)
	repo.metrics = metrics
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Millisecond)
	for _, snapshot := range []service.BucketSnapshotDTO{
		testRadarSnapshot("anthropic/pro", base),
		testRadarSnapshot("anthropic/pro", base.Add(time.Minute)),
		testRadarSnapshot("openai/team", base),
	} {
		require.NoError(t, repo.AppendBucketSnapshot(ctx, snapshot))
	}
	perfA, _ := service.RadarAAPerformanceSource("model-a")
	perfB, _ := service.RadarAAPerformanceSource("model-b")
	for source, payload := range map[service.RadarSourceKey][]byte{
		service.RadarSourceAA: []byte("aa-payload"), perfA: []byte("perf-a"), perfB: []byte("performance-b"),
	} {
		require.NoError(t, repo.SetSourcePayload(ctx, source, payload, time.Hour))
		now := base
		require.NoError(t, repo.SetSourceMeta(ctx, source, service.SourceFetchMeta{LastAttemptAt: now, LastSuccessAt: &now}))
	}

	// Simulate process restart with a fresh in-memory metrics state.
	restartedRegistry := prometheus.NewRegistry()
	restartedMetrics, err := observability.NewRadarMetrics(restartedRegistry)
	require.NoError(t, err)
	repo.metrics = restartedMetrics
	snapshot, err := repo.GetRadarMetricsSnapshot(ctx)
	require.NoError(t, err)
	secondRepoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	secondSnapshot, err := secondRepoValue.(*radarCacheRepository).GetRadarMetricsSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, snapshot, secondSnapshot, "replica count must not change shared bucket/cache totals")
	require.Equal(t, 2, snapshot.ActiveBucketCount)
	restartedMetrics.SetCacheMemoryTotals(snapshot.CacheMemoryBytes)
	body := scrapeRepositoryMetrics(t, restartedRegistry)

	quotaUsageA, _ := rdb.MemoryUsage(ctx, radarBucketRedisKey("anthropic/pro")).Result()
	quotaUsageB, _ := rdb.MemoryUsage(ctx, radarBucketRedisKey("openai/team")).Result()
	wantQuota := quotaUsageA + quotaUsageB
	require.Contains(t, body, fmt.Sprintf(`radar_cache_memory_bytes{cache="quota_bucket"} %d`, wantQuota))
	aaUsage, _ := rdb.MemoryUsage(ctx, radarAASourceKey).Result()
	perfAUsage, _ := rdb.MemoryUsage(ctx, radarAAPerfKeyPrefix+"model-a").Result()
	perfBUsage, _ := rdb.MemoryUsage(ctx, radarAAPerfKeyPrefix+"model-b").Result()
	require.Contains(t, body, fmt.Sprintf(`radar_cache_memory_bytes{cache="aa"} %d`, aaUsage))
	require.Contains(t, body, fmt.Sprintf(`radar_cache_memory_bytes{cache="aa_performance"} %d`, perfAUsage+perfBUsage))
	require.Regexp(t, `radar_cache_memory_bytes\{cache="metadata"\} [1-9][0-9]*`, body)
	require.NotContains(t, body, "model-a")
}

func TestRadarLatestHotPathNeverScansQuotaHistoryForMetrics(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	hook := &radarRedisCommandHook{}
	rdb.AddHook(hook)
	repoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	repo := repoValue.(*radarCacheRepository)
	base := time.Now().UTC().Truncate(time.Millisecond)
	for i := 0; i < 500; i++ {
		require.NoError(t, repo.AppendBucketSnapshot(context.Background(), testRadarSnapshot("anthropic/pro", base.Add(time.Duration(i)*time.Minute))))
	}
	hook.reset()

	_, err = repo.GetLatestBucket(context.Background(), "anthropic/pro")
	require.NoError(t, err)
	commands := hook.snapshot()
	require.Equal(t, []string{"zrevrange"}, commands, "latest public read must not scan history or refresh metrics")
}

func TestRadarSharedMemoryLedgerDropsExpiredEntries(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	repoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	repo := repoValue.(*radarCacheRepository)
	ctx := context.Background()
	require.NoError(t, repo.AppendBucketSnapshot(ctx, testRadarSnapshot("anthropic/pro", time.Now().UTC().Truncate(time.Millisecond))))
	require.NoError(t, repo.SetSourcePayload(ctx, service.RadarSourceAA, []byte("payload"), time.Hour))
	before, err := repo.GetRadarMetricsSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, before.ActiveBucketCount)
	require.Positive(t, before.CacheMemoryBytes["quota_bucket"])
	require.Positive(t, before.CacheMemoryBytes["aa"])

	mr.FastForward(testRadarHistoryRetention + 2*testRadarAggregationGap + time.Second)
	after, err := repo.GetRadarMetricsSnapshot(ctx)
	require.NoError(t, err)
	require.Zero(t, after.ActiveBucketCount)
	require.Zero(t, after.CacheMemoryBytes["quota_bucket"])
	require.Zero(t, after.CacheMemoryBytes["aa"])
}

func TestRadarAppendSurvivesLedgerFailureAndPeriodicSnapshotRepairsExistingQuotaEntry(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewRadarMetrics(registry)
	require.NoError(t, err)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	hook := &radarRedisCommandHook{}
	rdb.AddHook(hook)
	repoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	repo := repoValue.(*radarCacheRepository)
	repo.metrics = metrics
	ctx := context.Background()
	bucket := "anthropic/private-plan"
	key := radarBucketRedisKey(bucket)
	field := radarMetricsLedgerField("quota_bucket", key)

	require.NoError(t, rdb.Set(ctx, radarMetricsMemoryKey, "wrongtype", 0).Err())
	require.NoError(t, repo.AppendBucketSnapshot(ctx, testRadarSnapshot(bucket, time.Now().UTC().Truncate(time.Millisecond))))
	latest, err := repo.GetLatestBucket(ctx, bucket)
	require.NoError(t, err)
	require.NotNil(t, latest, "metrics ledger failure must not roll back quota data")

	body := scrapeRepositoryMetrics(t, registry)
	require.Contains(t, body, `radar_redis_operations_total{access="read",operation="cache_size",result="success"}`)
	require.Contains(t, body, `radar_redis_operations_total{access="write",operation="cache_size",result="error"}`)
	require.NotContains(t, body, `radar_redis_operations_total{access="write",operation="cache_size",result="success"}`)
	require.NotContains(t, body, bucket)

	require.NoError(t, rdb.Del(ctx, radarMetricsMemoryKey).Err())
	require.NoError(t, rdb.HSet(ctx, radarMetricsMemoryKey, field, 1).Err())
	wantUsage, err := rdb.MemoryUsage(ctx, key).Result()
	require.NoError(t, err)
	hook.reset()
	snapshot, err := repo.GetRadarMetricsSnapshot(ctx)
	require.NoError(t, err)
	commands := hook.snapshot()
	require.Contains(t, commands, "memory")
	require.Contains(t, commands, "hset")
	require.NotContains(t, commands, "zrange", "periodic metrics repair must not scan quota history")
	require.NotContains(t, commands, "zrevrange", "periodic metrics repair must not read quota payloads")
	require.Equal(t, int(wantUsage), snapshot.CacheMemoryBytes["quota_bucket"])
	storedUsage, err := rdb.HGet(ctx, radarMetricsMemoryKey, field).Int64()
	require.NoError(t, err)
	require.Equal(t, wantUsage, storedUsage, "periodic snapshot must overwrite an existing stale ledger value")
}

func TestRadarMemoryLedgerClassifiesMemoryReadFailure(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewRadarMetrics(registry)
	require.NoError(t, err)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	hook := &radarRedisCommandHook{}
	rdb.AddHook(hook)
	repoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	repo := repoValue.(*radarCacheRepository)
	repo.metrics = metrics
	ctx := context.Background()
	key := radarAASourceKey
	require.NoError(t, rdb.Set(ctx, key, "payload", 0).Err())
	hook.failNext("memory")

	_, _, err = repo.writeRadarMemoryLedgerEntry(ctx, "aa", key)
	require.Error(t, err)
	body := scrapeRepositoryMetrics(t, registry)
	require.Contains(t, body, `radar_redis_operations_total{access="read",operation="cache_size",result="error"} 1`)
	require.NotContains(t, body, `radar_redis_operations_total{access="read",operation="cache_size",result="success"}`)
	require.NotContains(t, body, `radar_redis_operations_total{access="write",operation="cache_size"`)
}

func TestRadarMemoryLedgerClassifiesHDelWriteFailure(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewRadarMetrics(registry)
	require.NoError(t, err)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	repoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	repo := repoValue.(*radarCacheRepository)
	repo.metrics = metrics
	ctx := context.Background()
	require.NoError(t, rdb.Set(ctx, radarMetricsMemoryKey, "wrongtype", 0).Err())

	_, _, err = repo.writeRadarMemoryLedgerEntry(ctx, "aa", "radar:source:missing")
	require.Error(t, err)
	body := scrapeRepositoryMetrics(t, registry)
	require.Contains(t, body, `radar_redis_operations_total{access="read",operation="cache_size",result="success"} 1`)
	require.Contains(t, body, `radar_redis_operations_total{access="write",operation="cache_size",result="error"} 1`)
	require.NotContains(t, body, `radar_redis_operations_total{access="write",operation="cache_size",result="success"}`)
}

func TestRadarAggregatorMetricsStateIsMonotonicAndIndependentOfRetainedBuckets(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	repoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	repo := repoValue.(*radarCacheRepository)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Millisecond)
	for i := 0; i < 6; i++ {
		bucket := fmt.Sprintf("anthropic/retained-%d", i)
		require.NoError(t, repo.AppendBucketSnapshot(ctx, testRadarSnapshot(bucket, base)))
	}
	applied, err := repo.CommitRadarAggregatorRun(ctx, service.RadarAggregatorRunState{
		RunVersion: 200, CompletedAt: base, NextFireAt: base.Add(15 * time.Minute), Success: true, PublishedBucketCount: 4,
	})
	require.NoError(t, err)
	require.True(t, applied)
	applied, err = repo.CommitRadarAggregatorRun(ctx, service.RadarAggregatorRunState{
		RunVersion: 100, CompletedAt: base.Add(time.Minute), NextFireAt: base.Add(15 * time.Minute), Success: true, PublishedBucketCount: 10,
	})
	require.NoError(t, err)
	require.False(t, applied, "an older run must not overwrite the latest published count")
	failureAt := base.Add(2 * time.Minute)
	applied, err = repo.CommitRadarAggregatorRun(ctx, service.RadarAggregatorRunState{
		RunVersion: 300, CompletedAt: failureAt, NextFireAt: base.Add(15 * time.Minute), Success: false, PublishedBucketCount: 0,
	})
	require.NoError(t, err)
	require.True(t, applied)

	snapshot, err := repo.GetRadarMetricsSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 6, snapshot.ActiveBucketCount)
	require.Equal(t, 4, snapshot.PublishedBucketCount)
	require.Equal(t, failureAt, snapshot.AggregatorLastRunAt)
	require.Equal(t, base, snapshot.AggregatorLastSuccessAt)
	require.True(t, snapshot.AggregatorStateValid)
	require.Equal(t, time.UnixMilli(300).UTC(), snapshot.AggregatorLastAttemptAt)
	require.Equal(t, base.Add(15*time.Minute), snapshot.AggregatorNextFireAt)
}

func TestRadarAggregatorStateReadIsNarrowAndDoesNotScanUnrelatedCaches(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	hook := &radarRedisCommandHook{}
	rdb.AddHook(hook)
	repoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	repo := repoValue.(*radarCacheRepository)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	applied, err := repo.CommitRadarAggregatorRun(ctx, service.RadarAggregatorRunState{
		RunVersion: now.UnixMilli(), CompletedAt: now, NextFireAt: now.Add(15 * time.Minute), Success: true, PublishedBucketCount: 4,
	})
	require.NoError(t, err)
	require.True(t, applied)
	unknown := make(map[string]any, radarMetricsMaxKeys+500)
	for index := 0; index < radarMetricsMaxKeys+500; index++ {
		unknown[fmt.Sprintf("unknown_%04d", index)] = "ignored"
	}
	require.NoError(t, rdb.HSet(ctx, radarMetricsStateKey, unknown).Err())
	hook.reset()

	snapshot, err := repo.GetRadarAggregatorState(ctx)
	require.NoError(t, err)
	require.True(t, snapshot.AggregatorStateValid)
	require.Equal(t, now, snapshot.AggregatorLastAttemptAt)
	require.Equal(t, now, snapshot.AggregatorLastRunAt)
	require.Equal(t, now, snapshot.AggregatorLastSuccessAt)
	require.Equal(t, now.Add(15*time.Minute), snapshot.AggregatorNextFireAt)
	require.Equal(t, 4, snapshot.PublishedBucketCount)
	require.Equal(t, []string{"hmget"}, hook.snapshot())
	require.Equal(t, [][]any{{
		"hmget",
		radarMetricsStateKey,
		"run_version",
		"last_run_at",
		"last_success_at",
		"published_bucket_count",
		"next_fire_at",
	}}, hook.argsSnapshot())
}

func TestRadarAggregatorStateReadsLegacyLedgerWithoutInventingNextFire(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	repoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	repo := repoValue.(*radarCacheRepository)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, rdb.HSet(ctx, radarMetricsStateKey, map[string]any{
		"run_version":            now.UnixMilli(),
		"last_run_at":            now.Format(time.RFC3339Nano),
		"last_success_at":        now.Format(time.RFC3339Nano),
		"published_bucket_count": 3,
	}).Err())

	snapshot, err := repo.GetRadarAggregatorState(ctx)
	require.NoError(t, err)
	require.True(t, snapshot.AggregatorStateValid)
	require.True(t, snapshot.AggregatorNextFireAt.IsZero(), "repository must not fabricate a historical scheduler deadline")
}

func TestRadarMetricsSnapshotDegradesCacheMemoryButKeepsCoreState(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	hook := &radarRedisCommandHook{}
	rdb.AddHook(hook)
	repoValue, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	repo := repoValue.(*radarCacheRepository)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, repo.AppendBucketSnapshot(ctx, testRadarSnapshot("anthropic/pro", now)))
	require.NoError(t, repo.SetSourceMeta(ctx, service.RadarSourceAA, service.SourceFetchMeta{LastAttemptAt: now, LastSuccessAt: &now}))
	applied, err := repo.CommitRadarAggregatorRun(ctx, service.RadarAggregatorRunState{
		RunVersion: now.UnixMilli(), CompletedAt: now, NextFireAt: now.Add(15 * time.Minute), Success: true, PublishedBucketCount: 4,
	})
	require.NoError(t, err)
	require.True(t, applied)
	good, err := repo.GetRadarMetricsSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, good.CacheMemoryValid)
	require.Positive(t, good.CacheMemoryBytes["quota_bucket"])

	hook.failNext("memory")
	partial, err := repo.GetRadarMetricsSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, partial.Partial)
	require.False(t, partial.CacheMemoryValid)
	require.True(t, partial.AggregatorStateValid)
	require.Equal(t, 4, partial.PublishedBucketCount)
	require.Equal(t, now, partial.SourceLastSuccess[service.RadarSourceAA])
}

func scrapeRepositoryMetrics(t *testing.T, gatherer prometheus.Gatherer) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	observability.MetricsHandler(gatherer).ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	require.Equal(t, 200, recorder.Code)
	return recorder.Body.String()
}
