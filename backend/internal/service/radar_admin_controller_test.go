package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (r *radarRunnerTestRepository) GetRadarAggregatorState(context.Context) (RadarMetricsSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.aggregatorState
	snapshot := RadarMetricsSnapshot{
		PublishedBucketCount: state.PublishedBucketCount,
		AggregatorLastRunAt:  state.CompletedAt,
		AggregatorNextFireAt: state.NextFireAt,
		AggregatorStateValid: true,
	}
	if state.RunVersion > 0 {
		snapshot.AggregatorLastAttemptAt = time.UnixMilli(state.RunVersion).UTC()
	}
	if state.Success {
		snapshot.AggregatorLastSuccessAt = state.CompletedAt
	}
	return snapshot, nil
}

func waitRadarRunnerAuthoritativeCadences(t *testing.T, repo *radarRunnerTestRepository, sources []RadarSourceKey) {
	t.Helper()
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		for _, source := range sources {
			cadence := repo.cadences[source]
			if cadence.Version == "" || cadence.NextFireAt.IsZero() {
				return false
			}
		}
		return true
	}, time.Second, time.Millisecond)
}

func TestRadarAdminControllerRequiresBoundStartedRunnerForRefresh(t *testing.T) {
	c, err := NewRadarAdminController(validRadarFetcherTestConfig(), newRadarRunnerTestRepository(), NewSettingService(&radarRuntimeSettingRepo{}, validRadarFetcherTestConfig()))
	require.NoError(t, err)

	_, err = c.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 41, RequestID: "req-1"})
	require.ErrorIs(t, err, ErrRadarAdminUnavailable)

	result, err := c.GetStatus(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Sources)
	require.Equal(t, RadarAdminStateNeverAttempted, result.Aggregator.Status)
	require.Nil(t, result.Aggregator.LastFailureAt)
	require.Nil(t, result.Aggregator.NextFireAt)
}

func TestRadarAdminStatusMapsOnlyCurrentFailureAndReadsEffectiveSetting(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	settingRepo := &radarRuntimeSettingRepo{values: []string{"false", "false"}}
	settings := NewSettingService(settingRepo, cfg)
	controller, err := NewRadarAdminController(cfg, repo, settings)
	require.NoError(t, err)
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{runtimeGate: staticRadarRuntimeSettingReader(false)}, fetcher)
	require.NoError(t, controller.BindRunner(runner))

	attempt := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	previousSuccess := attempt.Add(-time.Hour)
	next := attempt.Add(30 * time.Minute)
	failure := DataSourceErrorCodeNetworkError
	repo.mu.Lock()
	repo.metas[RadarSourceLMArena] = SourceFetchMeta{LastAttemptAt: attempt, LastSuccessAt: &previousSuccess, NextFireAt: &next, Error: &failure}
	repo.mu.Unlock()

	status, err := controller.GetStatus(context.Background())
	require.NoError(t, err)
	require.False(t, status.Enabled)
	require.Len(t, status.Sources, 1)
	require.Equal(t, RadarAdminStateFailed, status.Sources[0].Status)
	require.Equal(t, attempt, *status.Sources[0].LastFailureAt)
	require.Equal(t, DataSourceErrorCodeNetworkError, *status.Sources[0].Error)

	recovered := attempt.Add(time.Minute)
	repo.mu.Lock()
	repo.metas[RadarSourceLMArena] = SourceFetchMeta{LastAttemptAt: recovered, LastSuccessAt: &recovered, NextFireAt: &next}
	repo.mu.Unlock()
	status, err = controller.GetStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, RadarAdminStateHealthy, status.Sources[0].Status)
	require.Nil(t, status.Sources[0].LastFailureAt, "metadata does not retain historical failures after recovery")
	require.Nil(t, status.Sources[0].Error)
}

func TestRadarAdminStatusUsesAuthoritativePerSourceStaleThresholds(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	performance, err := RadarAAPerformanceSource("model-a")
	require.NoError(t, err)
	tests := []struct {
		name      string
		source    RadarSourceKey
		threshold time.Duration
	}{
		{"aa models", RadarSourceAA, time.Duration(cfg.Radar.ArtificialAnalysisModelsStaleThresholdMinutes) * time.Minute},
		{"aa performance", performance, time.Duration(cfg.Radar.ArtificialAnalysisPerformanceStaleThresholdMinutes) * time.Minute},
		{"lmarena", RadarSourceLMArena, time.Duration(cfg.Radar.LMArenaStaleThresholdMinutes) * time.Minute},
		{"claude health", RadarSourceStatusClaude, time.Duration(cfg.Radar.HealthStaleThresholdMinutes) * time.Minute},
		{"openai health", RadarSourceStatusOpenAI, time.Duration(cfg.Radar.HealthStaleThresholdMinutes) * time.Minute},
	}
	now := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRadarRunnerTestRepository()
			controller, err := NewRadarAdminController(cfg, repo, NewSettingService(&radarRuntimeSettingRepo{}, cfg))
			require.NoError(t, err)
			controller.now = func() time.Time { return now }
			runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, &radarRunnerTestFetcher{source: tt.source, interval: time.Hour})
			require.NoError(t, controller.BindRunner(runner))
			status, err := controller.GetStatus(context.Background())
			require.NoError(t, err)
			require.Equal(t, RadarAdminStateNeverAttempted, status.Sources[0].Status)
			require.False(t, status.Sources[0].Stale)

			fresh := now.Add(-tt.threshold).Add(time.Nanosecond)
			repo.mu.Lock()
			repo.metas[tt.source] = SourceFetchMeta{LastAttemptAt: fresh, LastSuccessAt: &fresh}
			repo.mu.Unlock()
			status, err = controller.GetStatus(context.Background())
			require.NoError(t, err)
			require.False(t, status.Sources[0].Stale)

			boundary := now.Add(-tt.threshold)
			repo.mu.Lock()
			repo.metas[tt.source] = SourceFetchMeta{LastAttemptAt: boundary, LastSuccessAt: &boundary}
			repo.mu.Unlock()
			status, err = controller.GetStatus(context.Background())
			require.NoError(t, err)
			require.True(t, status.Sources[0].Stale, "exact threshold is stale")

			failure := DataSourceErrorCodeNetworkError
			recentSuccess := now.Add(-time.Minute)
			repo.mu.Lock()
			repo.metas[tt.source] = SourceFetchMeta{LastAttemptAt: now, LastSuccessAt: &recentSuccess, Error: &failure}
			repo.mu.Unlock()
			status, err = controller.GetStatus(context.Background())
			require.NoError(t, err)
			require.Equal(t, RadarAdminStateFailed, status.Sources[0].Status)
			require.False(t, status.Sources[0].Stale, "a fresh last success remains fresh even when the current attempt failed")
		})
	}
}

func TestRadarAdminStatusUsesQuotaStaleBoundaryAndNeverIsNotStale(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	controller, err := NewRadarAdminController(cfg, repo, NewSettingService(&radarRuntimeSettingRepo{}, cfg))
	require.NoError(t, err)
	now := time.Date(2026, 7, 15, 7, 0, 0, 0, time.UTC)
	controller.now = func() time.Time { return now }

	status, err := controller.GetStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, RadarAdminStateNeverAttempted, status.Aggregator.Status)
	require.False(t, status.Aggregator.Stale)

	threshold := time.Duration(cfg.Radar.QuotaStaleThresholdMinutes) * time.Minute
	lastSuccess := now.Add(-threshold).Add(time.Nanosecond)
	repo.mu.Lock()
	repo.aggregatorState = RadarAggregatorRunState{RunVersion: lastSuccess.UnixMilli(), CompletedAt: lastSuccess, NextFireAt: now.Add(time.Minute), Success: true}
	repo.mu.Unlock()
	status, err = controller.GetStatus(context.Background())
	require.NoError(t, err)
	require.False(t, status.Aggregator.Stale)

	lastSuccess = now.Add(-threshold)
	repo.mu.Lock()
	repo.aggregatorState = RadarAggregatorRunState{RunVersion: lastSuccess.UnixMilli(), CompletedAt: lastSuccess, NextFireAt: now.Add(time.Minute), Success: true}
	repo.mu.Unlock()
	status, err = controller.GetStatus(context.Background())
	require.NoError(t, err)
	require.True(t, status.Aggregator.Stale)

	freshSuccess := now.Add(-time.Minute)
	failed := radarAdminMapAggregator(now, threshold, RadarMetricsSnapshot{
		AggregatorLastAttemptAt: now,
		AggregatorLastRunAt:     now,
		AggregatorLastSuccessAt: freshSuccess,
		AggregatorNextFireAt:    now.Add(time.Minute),
		AggregatorStateValid:    true,
	})
	require.Equal(t, RadarAdminStateFailed, failed.Status)
	require.False(t, failed.Stale, "a fresh quota snapshot remains fresh after a failed current run")
}

func TestRadarAdminManualRefreshBypassesToggleUsesLifecycleAndPreservesRealDeadlines(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	clockNow := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	fetched := make(chan struct{}, 1)
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
		fetched <- struct{}{}
		attempt := clockNow.Add(5 * time.Minute)
		return []byte(`{"leaderboard":[]}`), SourceFetchMeta{LastAttemptAt: attempt, LastSuccessAt: &attempt}, nil
	}}
	aggregated := make(chan struct{}, 1)
	aggregator := &radarRunnerQuotaAggregatorFake{fn: func(context.Context) (RadarQuotaAggregationReport, error) {
		aggregated <- struct{}{}
		return RadarQuotaAggregationReport{BucketCount: 2}, nil
	}}
	runner := newRadarRunnerWithQuotaForTest(t, cfg, repo, aggregator, radarRunnerOptions{
		clock:              radarRunnerFixedTestClock{now: clockNow},
		runtimeGate:        staticRadarRuntimeSettingReader(false),
		skipQuotaScheduler: true,
	}, fetcher)
	controller, err := NewRadarAdminController(cfg, repo, NewSettingService(&radarRuntimeSettingRepo{values: []string{"false"}}, cfg))
	require.NoError(t, err)
	require.NoError(t, controller.BindRunner(runner))
	runner.Start()
	waitRadarRunnerAuthoritativeCadences(t, repo, runner.sources)

	result, err := controller.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 7, RequestID: "req-manual"})
	require.NoError(t, err)
	require.Equal(t, RadarAdminRefreshTriggered, result.Status)
	require.Equal(t, []string{"lmarena", "quota_aggregator"}, result.Tasks)
	select {
	case <-fetched:
	case <-time.After(time.Second):
		t.Fatal("manual source did not run while runtime toggle was disabled")
	}
	select {
	case <-aggregated:
	case <-time.After(time.Second):
		t.Fatal("manual quota aggregation did not run")
	}
	waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceKey(radarManualRefreshTask))

	_, _, commits, _, _ := repo.snapshot()
	require.NotEmpty(t, commits)
	require.Equal(t, clockNow.Add(time.Hour), *commits[len(commits)-1].meta.NextFireAt)
	repo.mu.Lock()
	aggregatorState := repo.aggregatorState
	repo.mu.Unlock()
	require.Equal(t, clockNow.Add(15*time.Minute), aggregatorState.NextFireAt)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(ctx))
	_, err = controller.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 7})
	require.ErrorIs(t, err, ErrRadarAdminUnavailable)
}

func TestRadarAdminManualRefreshDispatchesAllSourcesBeforeAggregator(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	release := make(chan struct{})
	started := make(chan RadarSourceKey, 4)
	makeFetcher := func(source RadarSourceKey) RadarFetcher {
		return &radarRunnerTestFetcher{source: source, interval: time.Hour, fn: func(ctx context.Context) ([]byte, SourceFetchMeta, error) {
			started <- source
			select {
			case <-release:
			case <-ctx.Done():
				return nil, SourceFetchMeta{}, ctx.Err()
			}
			attempt := time.Now().UTC()
			return nil, SourceFetchMeta{LastAttemptAt: attempt}, errors.New("slow source failure")
		}}
	}
	fetchers := []RadarFetcher{
		makeFetcher(RadarSourceAA),
		makeFetcher(RadarSourceLMArena),
		makeFetcher(RadarSourceStatusClaude),
		makeFetcher(RadarSourceStatusOpenAI),
	}
	aggregated := make(chan struct{}, 1)
	aggregator := &radarRunnerQuotaAggregatorFake{fn: func(context.Context) (RadarQuotaAggregationReport, error) {
		aggregated <- struct{}{}
		return RadarQuotaAggregationReport{}, nil
	}}
	runner := newRadarRunnerWithQuotaForTest(t, cfg, repo, aggregator, radarRunnerOptions{
		runtimeGate:        staticRadarRuntimeSettingReader(false),
		skipQuotaScheduler: true,
		fetchBudget:        time.Second,
	}, fetchers...)
	controller, err := NewRadarAdminController(cfg, repo, NewSettingService(&radarRuntimeSettingRepo{values: []string{"false"}}, cfg))
	require.NoError(t, err)
	require.NoError(t, controller.BindRunner(runner))
	runner.Start()
	waitRadarRunnerAuthoritativeCadences(t, repo, runner.sources)
	result, err := controller.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 8})
	require.NoError(t, err)
	require.Equal(t, []string{"aa", "lmarena", "status_claude", "status_openai", "quota_aggregator"}, result.Tasks)

	seen := map[RadarSourceKey]bool{}
	deadline := time.NewTimer(200 * time.Millisecond)
	defer deadline.Stop()
	for len(seen) < len(fetchers) {
		select {
		case source := <-started:
			seen[source] = true
		case <-deadline.C:
			t.Fatalf("manual refresh did not dispatch every configured source concurrently; started=%v", seen)
		}
	}
	select {
	case <-aggregated:
		t.Fatal("aggregator ran before all source tasks converged")
	default:
	}
	close(release)
	select {
	case <-aggregated:
	case <-time.After(time.Second):
		t.Fatal("aggregator was not attempted after all source tasks converged")
	}
}

func TestRadarAdminManualRefreshBoundsAAWorkersAndAttemptsEveryTask(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	var calls atomic.Int32
	fetchers := make([]RadarFetcher, 0, 7)
	for i := 0; i < 7; i++ {
		source, err := RadarAAPerformanceSource(fmt.Sprintf("model-%d", i))
		require.NoError(t, err)
		fetchers = append(fetchers, &radarRunnerTestFetcher{source: source, interval: time.Hour, fn: func(ctx context.Context) ([]byte, SourceFetchMeta, error) {
			current := active.Add(1)
			for {
				seen := maximum.Load()
				if current <= seen || maximum.CompareAndSwap(seen, current) {
					break
				}
			}
			calls.Add(1)
			select {
			case <-release:
			case <-ctx.Done():
			}
			active.Add(-1)
			attempt := time.Now().UTC()
			return []byte(`{}`), SourceFetchMeta{LastAttemptAt: attempt, LastSuccessAt: &attempt}, nil
		}})
	}
	aggregated := make(chan struct{}, 1)
	aggregator := &radarRunnerQuotaAggregatorFake{fn: func(context.Context) (RadarQuotaAggregationReport, error) {
		aggregated <- struct{}{}
		return RadarQuotaAggregationReport{}, nil
	}}
	runner := newRadarRunnerWithQuotaForTest(t, cfg, repo, aggregator, radarRunnerOptions{
		runtimeGate:        staticRadarRuntimeSettingReader(false),
		skipQuotaScheduler: true,
		fetchBudget:        time.Second,
	}, fetchers...)
	controller, err := NewRadarAdminController(cfg, repo, NewSettingService(&radarRuntimeSettingRepo{values: []string{"false"}}, cfg))
	require.NoError(t, err)
	require.NoError(t, controller.BindRunner(runner))
	runner.Start()
	waitRadarRunnerAuthoritativeCadences(t, repo, runner.sources)
	_, err = controller.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 9})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return calls.Load() == 3 }, time.Second, time.Millisecond)
	require.Equal(t, int32(3), maximum.Load())
	close(release)
	select {
	case <-aggregated:
	case <-time.After(time.Second):
		t.Fatal("aggregator was not attempted after every AA task")
	}
	require.Equal(t, int32(7), calls.Load())
	require.LessOrEqual(t, maximum.Load(), int32(radarRunnerDefaultAAPerformanceConcurrency))
}

func TestRadarAdminManualRefreshUsesGenerationTokenAndSafeBatchTTL(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
		attempt := time.Now().UTC()
		return []byte(`{}`), SourceFetchMeta{LastAttemptAt: attempt, LastSuccessAt: &attempt}, nil
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{runtimeGate: staticRadarRuntimeSettingReader(false)}, fetcher)
	controller, err := NewRadarAdminController(cfg, repo, NewSettingService(&radarRuntimeSettingRepo{values: []string{"false"}}, cfg))
	require.NoError(t, err)
	require.NoError(t, controller.BindRunner(runner))
	runner.Start()
	waitRadarRunnerAuthoritativeCadences(t, repo, runner.sources)

	_, err = controller.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 10})
	require.NoError(t, err)
	waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceKey(radarManualRefreshTask))
	_, err = controller.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 10})
	require.NoError(t, err)
	waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceKey(radarManualRefreshTask))

	locks, releases, _, _, _ := repo.snapshot()
	var batchLocks, batchReleases []radarRunnerLockCall
	for _, call := range locks {
		if call.task == radarManualRefreshTask {
			batchLocks = append(batchLocks, call)
		}
	}
	for _, call := range releases {
		if call.task == radarManualRefreshTask {
			batchReleases = append(batchReleases, call)
		}
	}
	require.Len(t, batchLocks, 2)
	require.Len(t, batchReleases, 2)
	require.NotEqual(t, batchLocks[0].owner, batchLocks[1].owner)
	require.Equal(t, batchLocks[0].owner, batchReleases[0].owner)
	require.Equal(t, batchLocks[1].owner, batchReleases[1].owner)
	minimumTTL := runner.manualRefreshTimeout + 2*runner.cleanupTimeout + time.Second
	for _, call := range batchLocks {
		require.GreaterOrEqual(t, call.ttl, minimumTTL)
		require.NotEqual(t, runner.owner, call.owner)
	}
}

func TestRadarAdminOldGenerationCleanupCannotReleaseNewBatch(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	var lockMu sync.Mutex
	currentBatchToken := ""
	firstBatchToken := ""
	allowExpiredHandoff := false
	oldCleanupEntered := make(chan struct{})
	releaseOldCleanup := make(chan struct{})
	repo.tryLockFn = func(_ context.Context, task, owner string, _ time.Duration) (bool, error) {
		if task != radarManualRefreshTask {
			return true, nil
		}
		lockMu.Lock()
		defer lockMu.Unlock()
		if currentBatchToken == "" {
			currentBatchToken = owner
			firstBatchToken = owner
			return true, nil
		}
		if allowExpiredHandoff {
			allowExpiredHandoff = false
			currentBatchToken = owner
			return true, nil
		}
		return false, nil
	}
	repo.releaseLockFn = func(_ context.Context, task, owner string) error {
		if task != radarManualRefreshTask {
			return nil
		}
		if owner == firstBatchToken {
			close(oldCleanupEntered)
			<-releaseOldCleanup
		}
		lockMu.Lock()
		defer lockMu.Unlock()
		if currentBatchToken == owner {
			currentBatchToken = ""
		}
		return nil
	}
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	var calls atomic.Int32
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour, fn: func(ctx context.Context) ([]byte, SourceFetchMeta, error) {
		if calls.Add(1) == 2 {
			close(secondStarted)
			select {
			case <-releaseSecond:
			case <-ctx.Done():
				return nil, SourceFetchMeta{}, ctx.Err()
			}
		}
		attempt := time.Now().UTC()
		return []byte(`{}`), SourceFetchMeta{LastAttemptAt: attempt, LastSuccessAt: &attempt}, nil
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{runtimeGate: staticRadarRuntimeSettingReader(false), fetchBudget: time.Second}, fetcher)
	controller, err := NewRadarAdminController(cfg, repo, NewSettingService(&radarRuntimeSettingRepo{values: []string{"false"}}, cfg))
	require.NoError(t, err)
	require.NoError(t, controller.BindRunner(runner))
	runner.Start()
	waitRadarRunnerAuthoritativeCadences(t, repo, runner.sources)

	first, err := controller.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 12})
	require.NoError(t, err)
	require.Equal(t, RadarAdminRefreshTriggered, first.Status)
	waitRadarRunnerSignal(t, oldCleanupEntered, "old generation cleanup")
	lockMu.Lock()
	allowExpiredHandoff = true
	lockMu.Unlock()
	second, err := controller.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 12})
	require.NoError(t, err)
	require.Equal(t, RadarAdminRefreshTriggered, second.Status)
	waitRadarRunnerSignal(t, secondStarted, "new generation source")
	lockMu.Lock()
	newBatchToken := currentBatchToken
	lockMu.Unlock()
	require.NotEmpty(t, newBatchToken)
	require.NotEqual(t, firstBatchToken, newBatchToken)

	close(releaseOldCleanup)
	require.Eventually(t, func() bool {
		lockMu.Lock()
		defer lockMu.Unlock()
		return currentBatchToken == newBatchToken
	}, time.Second, time.Millisecond, "old cleanup must not delete the reacquired generation")
	close(releaseSecond)
	require.Eventually(t, func() bool {
		lockMu.Lock()
		defer lockMu.Unlock()
		return currentBatchToken == ""
	}, time.Second, time.Millisecond)
}

func TestRadarAdminManualRefreshCommitUsesCadenceAdvancedByScheduledTimer(t *testing.T) {
	for _, failManual := range []bool{false, true} {
		name := "success"
		if failManual {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			cfg := validRadarFetcherTestConfig()
			repo := newRadarRunnerTestRepository()
			var lockMu sync.Mutex
			locks := map[string]string{}
			repo.tryLockFn = func(_ context.Context, task, owner string, _ time.Duration) (bool, error) {
				lockMu.Lock()
				defer lockMu.Unlock()
				if _, held := locks[task]; held {
					return false, nil
				}
				locks[task] = owner
				return true, nil
			}
			repo.releaseLockFn = func(_ context.Context, task, owner string) error {
				lockMu.Lock()
				defer lockMu.Unlock()
				if locks[task] == owner {
					delete(locks, task)
				}
				return nil
			}
			base := time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)
			clock := newRadarRunnerControllableClock(base)
			manualStarted := make(chan struct{})
			releaseManual := make(chan struct{})
			var calls atomic.Int32
			fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
				call := calls.Add(1)
				attempt := base.Add(time.Duration(call) * time.Minute)
				if call == 2 {
					close(manualStarted)
					<-releaseManual
					if failManual {
						return nil, SourceFetchMeta{LastAttemptAt: attempt}, errors.New("upstream failed")
					}
				}
				return []byte(`{}`), SourceFetchMeta{LastAttemptAt: attempt, LastSuccessAt: &attempt}, nil
			}}
			runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{
				clock:              clock,
				fetchBudget:        time.Second,
				persistenceTimeout: 50 * time.Millisecond,
				cleanupTimeout:     50 * time.Millisecond,
				runtimeGate:        staticRadarRuntimeSettingReader(true),
			}, fetcher)
			controller, err := NewRadarAdminController(cfg, repo, NewSettingService(&radarRuntimeSettingRepo{values: []string{"true"}}, cfg))
			require.NoError(t, err)
			require.NoError(t, controller.BindRunner(runner))
			runner.Start()
			firstTimer := waitRadarRunnerTimer(t, clock, time.Hour)
			waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceLMArena)

			_, err = controller.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 11})
			require.NoError(t, err)
			waitRadarRunnerSignal(t, manualStarted, "manual source fetch")
			clock.now = base.Add(time.Hour)
			firstTimer.fire(clock.now)
			_ = waitRadarRunnerTimer(t, clock, time.Hour)
			require.Eventually(t, func() bool {
				locks, _, _, _, _ := repo.snapshot()
				count := 0
				for _, call := range locks {
					if call.task == string(RadarSourceLMArena) {
						count++
					}
				}
				return count >= 3
			}, time.Second, time.Millisecond)
			close(releaseManual)
			waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceKey(radarManualRefreshTask))

			_, _, commits, _, _ := repo.snapshot()
			repo.mu.Lock()
			failures := append([]radarRunnerMetaCall(nil), repo.failureCommitCalls...)
			repo.mu.Unlock()
			wantNext := base.Add(2 * time.Hour)
			if failManual {
				require.NotEmpty(t, failures)
				require.Equal(t, wantNext, *failures[len(failures)-1].meta.NextFireAt)
			} else {
				require.GreaterOrEqual(t, len(commits), 2)
				require.Equal(t, wantNext, *commits[len(commits)-1].meta.NextFireAt)
			}
			select {
			case extra := <-clock.created:
				t.Fatalf("manual refresh changed scheduler timer: %+v", extra)
			default:
			}
		})
	}
}

func TestRadarRunnerManualRefreshBudgetCoversQueuedScheduledAndManualAAWaves(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	gate := newRadarRuntimeGateStub(true)
	base := time.Date(2026, 7, 15, 7, 0, 0, 0, time.UTC)
	clock := newRadarRunnerControllableClock(base)
	type startEvent struct {
		source RadarSourceKey
		call   int32
	}
	starts := make(chan startEvent, 32)
	release := make(chan struct{}, 32)
	const sourceCount = 7
	const concurrency = 3
	fetchers := make([]RadarFetcher, 0, sourceCount)
	counters := make([]*atomic.Int32, 0, sourceCount)
	for i := 0; i < sourceCount; i++ {
		source, err := RadarAAPerformanceSource(fmt.Sprintf("model-%d", i))
		require.NoError(t, err)
		counter := &atomic.Int32{}
		counters = append(counters, counter)
		fetchers = append(fetchers, &radarRunnerTestFetcher{
			source:   source,
			interval: time.Hour,
			fn: func(ctx context.Context) ([]byte, SourceFetchMeta, error) {
				call := counter.Add(1)
				starts <- startEvent{source: source, call: call}
				select {
				case <-release:
				case <-ctx.Done():
					return nil, SourceFetchMeta{}, ctx.Err()
				}
				attempt := base.Add(time.Duration(call) * time.Minute)
				return []byte(`{}`), SourceFetchMeta{LastAttemptAt: attempt, LastSuccessAt: &attempt}, nil
			},
		})
	}
	quotaRemaining := make(chan time.Duration, 1)
	aggregator := &radarRunnerQuotaAggregatorFake{fn: func(ctx context.Context) (RadarQuotaAggregationReport, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			quotaRemaining <- 0
		} else {
			quotaRemaining <- time.Until(deadline)
		}
		return RadarQuotaAggregationReport{}, nil
	}}
	runner := newRadarRunnerWithQuotaForTest(t, cfg, repo, aggregator, radarRunnerOptions{
		clock:                    clock,
		runtimeGate:              gate,
		aaPerformanceConcurrency: concurrency,
		fetchBudget:              2 * time.Second,
		persistenceTimeout:       50 * time.Millisecond,
		cleanupTimeout:           50 * time.Millisecond,
		quotaTimeout:             500 * time.Millisecond,
		skipQuotaScheduler:       true,
	}, fetchers...)
	wantBudget, ok := radarRunnerManualRefreshBudget(
		sourceCount,
		sourceCount,
		concurrency,
		2*time.Second,
		50*time.Millisecond,
		50*time.Millisecond,
		500*time.Millisecond,
	)
	require.True(t, ok)
	require.Equal(t, wantBudget, runner.manualRefreshTimeout)
	for _, fetcher := range fetchers {
		cadence, err := repo.AdvanceSourceNextFire(context.Background(), fetcher.Source(), base.Add(time.Hour))
		require.NoError(t, err)
		runner.setSourceCadence(fetcher.Source(), cadence)
	}

	gate.calls.Store(0)
	runner.lifecycleMu.Lock()
	runner.started = true
	runner.lifecycleMu.Unlock()
	scheduledCtx, cancelScheduled := context.WithCancel(runner.ctx)
	for _, fetcher := range fetchers {
		runner.wg.Add(1)
		go runner.runFetcher(scheduledCtx, fetcher, fetcher.Source(), time.Hour)
	}
	readStarts := func(count int) []startEvent {
		t.Helper()
		result := make([]startEvent, 0, count)
		for len(result) < count {
			select {
			case event := <-starts:
				result = append(result, event)
			case <-time.After(2 * time.Second):
				t.Fatalf("timed out after %d/%d AA starts", len(result), count)
			}
		}
		return result
	}
	releaseActive := func(count int) {
		t.Helper()
		for i := 0; i < count; i++ {
			select {
			case release <- struct{}{}:
			case <-time.After(2 * time.Second):
				t.Fatalf("timed out releasing AA source %d/%d", i, count)
			}
		}
	}

	firstWave := readStarts(concurrency)
	for _, event := range firstWave {
		require.Equal(t, int32(1), event.call)
	}
	require.Eventually(t, func() bool {
		return gate.calls.Load() >= sourceCount+concurrency
	}, time.Second, time.Millisecond, "all scheduled AA waiters must queue before manual refresh")

	triggered, tasks, err := runner.TriggerManualRefresh()
	require.NoError(t, err)
	require.True(t, triggered)
	require.Len(t, tasks, sourceCount+1)
	go func() {
		runner.wg.Wait()
		runner.closeDone()
	}()

	releaseActive(concurrency)
	secondWave := readStarts(concurrency)
	for _, event := range secondWave {
		require.Equal(t, int32(1), event.call, "scheduled waiters queued first must consume the second wave")
	}
	releaseActive(2 * sourceCount)
	remainingStarts := readStarts(2*sourceCount - 2*concurrency)
	remainingScheduled := 0
	remainingManual := 0
	for _, event := range remainingStarts {
		if event.call == 1 {
			remainingScheduled++
		} else if event.call == 2 {
			remainingManual++
		}
	}
	require.Equal(t, 1, remainingScheduled, "all seven scheduled AA tasks must be attempted")
	require.Equal(t, sourceCount, remainingManual, "all seven manual AA tasks must be attempted")

	select {
	case remaining := <-quotaRemaining:
		require.Greater(t, remaining, 400*time.Millisecond, "quota must receive its complete timeout phase")
	case <-time.After(2 * time.Second):
		t.Fatal("quota phase was not attempted after all manual sources")
	}
	for index, counter := range counters {
		require.Equalf(t, int32(2), counter.Load(), "AA source %d must be attempted once scheduled and once manually", index)
	}
	require.Equal(t, int32(2*sourceCount), gate.calls.Load(), "scheduled attempts must check the runtime gate before and after the shared semaphore while manual attempts bypass it")
	cancelScheduled()
}

func TestRadarAdminManualRefreshCoalescesAcrossReplicasAndStopCancelsWork(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	var lockMu sync.Mutex
	locks := map[string]string{}
	repo.tryLockFn = func(_ context.Context, task, owner string, _ time.Duration) (bool, error) {
		lockMu.Lock()
		defer lockMu.Unlock()
		if _, exists := locks[task]; exists {
			return false, nil
		}
		locks[task] = owner
		return true, nil
	}
	repo.releaseLockFn = func(_ context.Context, task, owner string) error {
		lockMu.Lock()
		defer lockMu.Unlock()
		if locks[task] == owner {
			delete(locks, task)
		}
		return nil
	}
	started := make(chan struct{})
	canceled := make(chan struct{})
	blocking := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour, fn: func(ctx context.Context) ([]byte, SourceFetchMeta, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, SourceFetchMeta{}, ctx.Err()
	}}
	other := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour}
	newReplica := func(fetcher RadarFetcher) (*RadarRunner, *RadarAdminController) {
		runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{runtimeGate: staticRadarRuntimeSettingReader(false)}, fetcher)
		controller, err := NewRadarAdminController(cfg, repo, NewSettingService(&radarRuntimeSettingRepo{values: []string{"false"}}, cfg))
		require.NoError(t, err)
		require.NoError(t, controller.BindRunner(runner))
		runner.Start()
		waitRadarRunnerAuthoritativeCadences(t, repo, runner.sources)
		return runner, controller
	}
	runner1, controller1 := newReplica(blocking)
	_, controller2 := newReplica(other)
	first, err := controller1.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 1})
	require.NoError(t, err)
	require.Equal(t, RadarAdminRefreshTriggered, first.Status)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("manual refresh did not begin")
	}
	second, err := controller2.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 2})
	require.NoError(t, err)
	require.Equal(t, RadarAdminRefreshCoalesced, second.Status)
	require.NotEqual(t, first.RefreshID, second.RefreshID)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner1.Stop(ctx))
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel lifecycle-owned manual work")
	}
}

func TestRadarAdminManualRefreshHasLifecycleOwnedSystemTimeout(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	started := make(chan struct{})
	timedOut := make(chan error, 1)
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour, fn: func(ctx context.Context) ([]byte, SourceFetchMeta, error) {
		close(started)
		<-ctx.Done()
		timedOut <- ctx.Err()
		return nil, SourceFetchMeta{}, ctx.Err()
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{
		fetchBudget:          time.Second,
		manualRefreshTimeout: 25 * time.Millisecond,
		runtimeGate:          staticRadarRuntimeSettingReader(false),
	}, fetcher)
	controller, err := NewRadarAdminController(cfg, repo, NewSettingService(&radarRuntimeSettingRepo{values: []string{"false"}}, cfg))
	require.NoError(t, err)
	require.NoError(t, controller.BindRunner(runner))
	runner.Start()
	waitRadarRunnerAuthoritativeCadences(t, repo, runner.sources)
	result, err := controller.TriggerRefresh(RadarAdminAuditContext{AdminUserID: 3})
	require.NoError(t, err)
	require.Equal(t, RadarAdminRefreshTriggered, result.Status)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("manual refresh did not start")
	}
	select {
	case err := <-timedOut:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("manual refresh did not honor system timeout")
	}
	waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceKey(radarManualRefreshTask))
}

func TestRadarAdminStatusReturnsFixedUnavailableError(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	repo.listMetaFn = func(context.Context) (map[RadarSourceKey]SourceFetchMeta, error) {
		return nil, errors.New("redis://secret@internal/radar")
	}
	controller, err := NewRadarAdminController(cfg, repo, NewSettingService(&radarRuntimeSettingRepo{}, cfg))
	require.NoError(t, err)
	_, err = controller.GetStatus(context.Background())
	require.EqualError(t, err, "radar admin control unavailable")
	require.NotContains(t, err.Error(), "secret")
}

func TestRadarAdminAuditLogsOnlyExplicitSafeFields(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	settingRepo := &radarRuntimeSettingRepo{setError: errors.New("postgres://user:secret@internal")}
	var logs bytes.Buffer
	controller, err := newRadarAdminController(
		cfg,
		repo,
		NewSettingService(settingRepo, cfg),
		slog.New(slog.NewJSONHandler(&logs, nil)),
	)
	require.NoError(t, err)
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour})
	require.NoError(t, controller.BindRunner(runner))
	audit := RadarAdminAuditContext{AdminUserID: 91, RequestID: "req\nAuthorization: Bearer secret"}
	_, err = controller.SetEnabled(context.Background(), true, audit)
	require.ErrorIs(t, err, ErrRadarAdminUnavailable)
	_, err = controller.TriggerRefresh(audit)
	require.ErrorIs(t, err, ErrRadarAdminUnavailable)

	body := logs.String()
	require.Contains(t, body, `"admin_user_id":91`)
	require.Contains(t, body, `"status":"failed"`)
	require.Contains(t, body, `"status":"unavailable"`)
	require.Contains(t, body, `"request_id":"unknown"`)
	require.Contains(t, body, `"tasks":["lmarena","quota_aggregator"]`)
	require.NotContains(t, body, "postgres")
	require.NotContains(t, body, "Authorization")
	require.NotContains(t, body, "Bearer")
	require.NotContains(t, body, "secret")
}
