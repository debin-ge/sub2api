package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type radarRunnerHydratingRepository struct {
	*radarRunnerTestRepository
	cacheHydrated atomic.Bool
	snapshotMu    sync.Mutex
	snapshot      RadarMetricsSnapshot
}

func (r *radarRunnerHydratingRepository) GetRadarMetricsSnapshot(context.Context) (RadarMetricsSnapshot, error) {
	r.cacheHydrated.Store(true)
	r.snapshotMu.Lock()
	defer r.snapshotMu.Unlock()
	return r.snapshot, nil
}

func (r *radarRunnerHydratingRepository) setSnapshot(snapshot RadarMetricsSnapshot) {
	r.snapshotMu.Lock()
	r.snapshot = snapshot
	r.snapshotMu.Unlock()
}

func TestRadarRunnerRecordsFetchAndAggregationMetricsFromExecutionPaths(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewRadarMetrics(registry)
	require.NoError(t, err)

	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusClaude, interval: time.Hour}
	report := RadarQuotaAggregationReport{
		BucketCount:          2,
		SkippedAccountCounts: map[string]int{radarQuotaSkipInvalidWindow: 3, "privacy_threshold": 2},
		InferenceCounts: map[RadarQuotaInferenceMetric]int{
			{Bucket: PlatformAnthropic, Result: "success"}:                                            2,
			{Bucket: PlatformAnthropic, Result: "rejected", Reason: InferenceRejectReasonUnknownPlan}: 1,
			{Bucket: PlatformOpenAI, Result: "rejected", Reason: InferenceRejectReasonHighDispersion}: 1,
		},
	}
	aggregator := &radarRunnerQuotaAggregatorFake{fn: func(context.Context) (RadarQuotaAggregationReport, error) {
		return report, nil
	}}
	runner := newRadarRunnerWithQuotaForTest(t, cfg, repo, aggregator, radarRunnerOptions{metrics: metrics}, fetcher)

	runner.fetchOnce(context.Background(), fetcher, fetcher.source, time.Now().Add(time.Hour))
	runner.runQuotaAggregatorOnce(context.Background())

	body := scrapeRadarMetricsForTest(t, registry)
	require.Contains(t, body, `radar_fetch_success_total{source="status_claude"} 1`)
	require.Contains(t, body, `radar_fetch_http_responses_total{source="status_claude",status="2xx"} 1`)
	require.Contains(t, body, `radar_source_age_seconds{source="status_claude"}`)
	require.Contains(t, body, `radar_aggregator_bucket_count 2`)
	require.Contains(t, body, `radar_aggregator_interval_seconds 900`)
	require.Contains(t, body, `radar_enabled 1`)
	require.NotContains(t, body, "radar_aggregator_last_run_timestamp_seconds 0\n")
	require.NotContains(t, body, "radar_aggregator_last_success_timestamp_seconds 0\n")
	require.Contains(t, body, `radar_aggregator_runs_total{reason="none",result="success"} 1`)
	require.Contains(t, body, `radar_aggregator_skipped_accounts_total{reason="invalid_window"} 3`)
	require.Contains(t, body, `radar_aggregator_skipped_accounts_total{reason="privacy_threshold"} 2`)
	require.Contains(t, body, `radar_inference_total{bucket="anthropic",reason="none",result="success"} 2`)
	require.Contains(t, body, `radar_inference_total{bucket="anthropic",reason="unknown_plan",result="rejected"} 1`)
	require.Contains(t, body, `radar_inference_total{bucket="openai",reason="high_dispersion",result="rejected"} 1`)
}

func TestRadarRunnerExportsDisabledGaugeFromConfig(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewRadarMetrics(registry)
	require.NoError(t, err)
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.Enabled = false
	repo := newRadarRunnerTestRepository()
	fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusClaude, interval: time.Hour}
	_ = newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{metrics: metrics}, fetcher)

	body := scrapeRadarMetricsForTest(t, registry)
	require.Contains(t, body, `radar_enabled 0`)
}

func TestRadarRunnerCommitOutcomesAndFetchOnlyDurationMetrics(t *testing.T) {
	tests := []struct {
		name   string
		commit func(context.Context, RadarSourceKey, []byte, time.Duration, SourceFetchMeta) (bool, error)
		reason string
	}{
		{name: "storage error", reason: "storage_error", commit: func(context.Context, RadarSourceKey, []byte, time.Duration, SourceFetchMeta) (bool, error) {
			time.Sleep(150 * time.Millisecond)
			return false, errors.New("private redis key")
		}},
		{name: "superseded", reason: "superseded", commit: func(context.Context, RadarSourceKey, []byte, time.Duration, SourceFetchMeta) (bool, error) {
			time.Sleep(150 * time.Millisecond)
			return false, nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			metrics, err := observability.NewRadarMetrics(registry)
			require.NoError(t, err)
			repo := newRadarRunnerTestRepository()
			repo.commitFn = tt.commit
			fetcher := &radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour}
			runner := newRadarRunnerForTest(t, validRadarFetcherTestConfig(), repo, radarRunnerOptions{
				metrics: metrics, persistenceTimeout: time.Second,
			}, fetcher)
			runner.fetchOnce(context.Background(), fetcher, fetcher.source, time.Now().Add(time.Hour))

			body := scrapeRadarMetricsForTest(t, registry)
			require.NotContains(t, body, `radar_fetch_success_total{source="aa"}`)
			require.Contains(t, body, `radar_fetch_failure_total{reason="`+tt.reason+`",source="aa"} 1`)
			require.Contains(t, body, `radar_fetch_duration_seconds_bucket{source="aa",le="0.05"} 1`, "commit latency must not enter fetch duration")
		})
	}
}

func TestRadarRunnerStartHydratesPersistedAgeAndCacheBeforeFetch(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewRadarMetrics(registry)
	require.NoError(t, err)
	now := time.Now().UTC()
	base := newRadarRunnerTestRepository()
	base.metas[RadarSourceStatusClaude] = SourceFetchMeta{LastAttemptAt: now.Add(-time.Hour), LastSuccessAt: radarRunnerTimePointer(now.Add(-30 * time.Minute))}
	repo := &radarRunnerHydratingRepository{radarRunnerTestRepository: base, snapshot: RadarMetricsSnapshot{
		ActiveBucketCount:    6,
		PublishedBucketCount: 2,
		AggregatorStateValid: true,
		CacheMemoryBytes:     map[string]int{"quota_bucket": 100},
		CacheMemoryValid:     true,
		SourceLastSuccess:    map[RadarSourceKey]time.Time{RadarSourceStatusClaude: now.Add(-30 * time.Minute)},
	}}
	started := make(chan struct{})
	release := make(chan struct{})
	fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusClaude, interval: time.Hour, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
		close(started)
		<-release
		return nil, SourceFetchMeta{}, errors.New("network")
	}}
	runner := newRadarRunnerForTest(t, validRadarFetcherTestConfig(), repo, radarRunnerOptions{metrics: metrics}, fetcher)
	runner.Start()
	<-started
	require.Eventually(t, func() bool {
		body := scrapeRadarMetricsForTest(t, registry)
		return !strings.Contains(body, `radar_source_age_seconds{source="status_claude"} -1`) &&
			strings.Contains(body, `radar_aggregator_bucket_count 2`) && repo.cacheHydrated.Load()
	}, time.Second, 10*time.Millisecond)
	close(release)
	waitRadarRunnerRepoEvent(t, base, "commit_failure", RadarSourceStatusClaude)
	body := scrapeRadarMetricsForTest(t, registry)
	require.NotContains(t, body, `radar_source_age_seconds{source="status_claude"} -1`, "a failed refresh must retain hydrated success age")
}

func TestRadarRunnerPeriodicSyncConvergesTwoReplicasToSharedState(t *testing.T) {
	base := newRadarRunnerTestRepository()
	repo := &radarRunnerHydratingRepository{
		radarRunnerTestRepository: base,
		snapshot: RadarMetricsSnapshot{
			ActiveBucketCount: 6, PublishedBucketCount: 4, AggregatorStateValid: true,
			CacheMemoryBytes: map[string]int{"quota_bucket": 100}, CacheMemoryValid: true,
		},
	}
	base.tryLockFn = func(context.Context, string, string, time.Duration) (bool, error) { return false, nil }
	registries := []*prometheus.Registry{prometheus.NewRegistry(), prometheus.NewRegistry()}
	metrics := make([]*observability.RadarMetrics, 2)
	runners := make([]*RadarRunner, 2)
	for i := range runners {
		var err error
		metrics[i], err = observability.NewRadarMetrics(registries[i])
		require.NoError(t, err)
		fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusClaude, interval: time.Hour}
		runners[i] = newRadarRunnerForTest(t, validRadarFetcherTestConfig(), repo, radarRunnerOptions{
			metrics: metrics[i], metricsSyncInterval: 10 * time.Millisecond,
		}, fetcher)
		runners[i].Start()
	}

	for i := range runners {
		i := i
		require.Eventually(t, func() bool {
			body := scrapeRadarMetricsForTest(t, registries[i])
			return strings.Contains(body, `radar_source_age_seconds{source="status_claude"} -1`) &&
				strings.Contains(body, `radar_aggregator_bucket_count 4`) &&
				strings.Contains(body, `radar_cache_memory_bytes{cache="quota_bucket"} 100`)
		}, time.Second, 10*time.Millisecond)
	}

	now := time.Now().UTC()
	repo.setSnapshot(RadarMetricsSnapshot{
		ActiveBucketCount: 6, PublishedBucketCount: 1, AggregatorStateValid: true,
		CacheMemoryBytes: map[string]int{"quota_bucket": 250}, CacheMemoryValid: true,
		SourceLastSuccess: map[RadarSourceKey]time.Time{RadarSourceStatusClaude: now},
	})
	for i := range runners {
		i := i
		require.Eventually(t, func() bool {
			body := scrapeRadarMetricsForTest(t, registries[i])
			return !strings.Contains(body, `radar_source_age_seconds{source="status_claude"} -1`) &&
				strings.Contains(body, `radar_aggregator_bucket_count 1`) &&
				strings.Contains(body, `radar_cache_memory_bytes{cache="quota_bucket"} 250`)
		}, time.Second, 10*time.Millisecond)
	}
}

func TestRadarRunnerPartialSnapshotKeepsLastGoodCacheAndSyncsCoreState(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewRadarMetrics(registry)
	require.NoError(t, err)
	now := time.Now().UTC()
	base := newRadarRunnerTestRepository()
	base.tryLockFn = func(context.Context, string, string, time.Duration) (bool, error) { return false, nil }
	repo := &radarRunnerHydratingRepository{radarRunnerTestRepository: base, snapshot: RadarMetricsSnapshot{
		PublishedBucketCount: 10, AggregatorStateValid: true,
		CacheMemoryBytes: map[string]int{"quota_bucket": 500}, CacheMemoryValid: true,
	}}
	fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusClaude, interval: time.Hour}
	runner := newRadarRunnerForTest(t, validRadarFetcherTestConfig(), repo, radarRunnerOptions{
		metrics: metrics, metricsSyncInterval: 10 * time.Millisecond,
	}, fetcher)
	runner.Start()
	require.Eventually(t, func() bool {
		body := scrapeRadarMetricsForTest(t, registry)
		return strings.Contains(body, `radar_aggregator_bucket_count 10`) &&
			strings.Contains(body, `radar_cache_memory_bytes{cache="quota_bucket"} 500`)
	}, time.Second, 10*time.Millisecond)

	repo.setSnapshot(RadarMetricsSnapshot{
		PublishedBucketCount: 4, AggregatorLastRunAt: now, AggregatorLastSuccessAt: now,
		AggregatorStateValid: true, CacheMemoryBytes: map[string]int{"quota_bucket": 0},
		CacheMemoryValid: false, Partial: true,
		SourceLastSuccess: map[RadarSourceKey]time.Time{RadarSourceStatusClaude: now},
	})
	require.Eventually(t, func() bool {
		body := scrapeRadarMetricsForTest(t, registry)
		return strings.Contains(body, `radar_aggregator_bucket_count 4`) &&
			strings.Contains(body, `radar_cache_memory_bytes{cache="quota_bucket"} 500`) &&
			!strings.Contains(body, `radar_source_age_seconds{source="status_claude"} -1`)
	}, time.Second, 10*time.Millisecond)
}

func TestRadarRunnerStartDoesNotBlockOnMetricsSync(t *testing.T) {
	repo := newRadarRunnerTestRepository()
	syncStarted := make(chan struct{})
	releaseSync := make(chan struct{})
	repo.listMetaFn = func(context.Context) (map[RadarSourceKey]SourceFetchMeta, error) {
		close(syncStarted)
		<-releaseSync
		return map[RadarSourceKey]SourceFetchMeta{}, nil
	}
	repo.tryLockFn = func(context.Context, string, string, time.Duration) (bool, error) { return false, nil }
	fetcher := &radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour}
	runner := newRadarRunnerForTest(t, validRadarFetcherTestConfig(), repo, radarRunnerOptions{}, fetcher)

	returned := make(chan struct{})
	go func() {
		runner.Start()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Start blocked on metrics synchronization")
	}
	select {
	case <-syncStarted:
	case <-time.After(time.Second):
		t.Fatal("background metrics synchronization did not start")
	}
	close(releaseSync)
}

func TestRadarRunnerStopCancelsInFlightMetricsSync(t *testing.T) {
	repo := newRadarRunnerTestRepository()
	syncStarted := make(chan struct{})
	repo.listMetaFn = func(ctx context.Context) (map[RadarSourceKey]SourceFetchMeta, error) {
		close(syncStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	repo.tryLockFn = func(context.Context, string, string, time.Duration) (bool, error) { return false, nil }
	fetcher := &radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour}
	runner := newRadarRunnerForTest(t, validRadarFetcherTestConfig(), repo, radarRunnerOptions{}, fetcher)
	runner.Start()
	<-syncStarted
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(ctx))
}

func scrapeRadarMetricsForTest(t *testing.T, registry prometheus.Gatherer) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	observability.MetricsHandler(registry).ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	require.Equal(t, 200, recorder.Code)
	return recorder.Body.String()
}
