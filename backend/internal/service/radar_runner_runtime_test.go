package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type radarRuntimeGateStub struct {
	enabled atomic.Bool
	calls   atomic.Int32
}

func newRadarRuntimeGateStub(enabled bool) *radarRuntimeGateStub {
	gate := &radarRuntimeGateStub{}
	gate.enabled.Store(enabled)
	return gate
}

func (g *radarRuntimeGateStub) IsRadarEnabled(context.Context) bool {
	g.calls.Add(1)
	return g.enabled.Load()
}

func TestRadarRunnerRuntimeGateDynamicallyStopsAndRestartsSourceWork(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	clock := newRadarRunnerControllableClock(time.Now().UTC())
	gate := newRadarRuntimeGateStub(false)
	repo := newRadarRunnerTestRepository()
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{clock: clock, runtimeGate: gate}, fetcher)

	runner.Start()
	firstTimer := waitRadarRunnerTimer(t, clock, time.Hour)
	// Constructor + metrics sync + this source fire must all have observed the
	// disabled value before the test enables it.
	require.Eventually(t, func() bool { return gate.calls.Load() >= 3 }, time.Second, time.Millisecond)
	require.Zero(t, fetcher.calls.Load())
	locks, _, commits, failures, payloadWrites := repo.snapshot()
	require.Empty(t, locks)
	require.Empty(t, commits)
	require.Empty(t, failures)
	require.Zero(t, payloadWrites)

	gate.enabled.Store(true)
	firstTimer.fire(time.Now())
	waitRadarRunnerRepoEvent(t, repo, "commit", RadarSourceLMArena)
	secondTimer := waitRadarRunnerTimer(t, clock, time.Hour)
	require.Equal(t, int32(1), fetcher.calls.Load())

	gate.enabled.Store(false)
	callsBeforeDisabledFire := gate.calls.Load()
	secondTimer.fire(time.Now())
	_ = waitRadarRunnerTimer(t, clock, time.Hour)
	require.Eventually(t, func() bool { return gate.calls.Load() > callsBeforeDisabledFire }, time.Second, time.Millisecond)
	require.Equal(t, int32(1), fetcher.calls.Load(), "disabled fire must skip new source work")
	locks, _, commits, failures, payloadWrites = repo.snapshot()
	require.Len(t, locks, 1)
	require.Len(t, commits, 1)
	require.Empty(t, failures)
	require.Zero(t, payloadWrites)
}

func TestRadarRunnerRuntimeGateDynamicallyStopsAndRestartsQuotaWork(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	clock := newRadarRunnerControllableClock(time.Now().UTC())
	gate := newRadarRuntimeGateStub(false)
	repo := newRadarRunnerTestRepository()
	aggregator := &radarRunnerQuotaAggregatorFake{}
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour}
	quotaInterval := 30 * time.Minute
	runner := newRadarRunnerWithQuotaForTest(t, cfg, repo, aggregator, radarRunnerOptions{
		clock: clock, runtimeGate: gate, quotaInterval: quotaInterval, quotaTimeout: time.Minute,
	}, fetcher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.wg.Add(1)
	go runner.runQuotaAggregator(ctx)
	quotaTimer := waitRadarRunnerTimer(t, clock, quotaInterval)
	require.Eventually(t, func() bool { return gate.calls.Load() >= 2 }, time.Second, time.Millisecond,
		"constructor and immediate quota fire must both observe disabled")
	require.Zero(t, aggregator.calls.Load())
	locks, _, commits, failures, payloadWrites := repo.snapshot()
	require.Empty(t, locks)
	require.Empty(t, commits)
	require.Empty(t, failures)
	require.Zero(t, payloadWrites)

	gate.enabled.Store(true)
	quotaTimer.fire(time.Now())
	waitRadarRunnerRepoEvent(t, repo, "try_lock", RadarSourceKey(radarQuotaAggregatorTask))
	require.Eventually(t, func() bool { return aggregator.calls.Load() == 1 }, time.Second, time.Millisecond)
	cancel()
	waitDone := make(chan struct{})
	go func() {
		runner.wg.Wait()
		close(waitDone)
	}()
	waitRadarRunnerSignal(t, waitDone, "quota runtime test scheduler to stop")
}

func TestRadarRunnerDisableDoesNotCancelInFlightWork(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	gate := newRadarRuntimeGateStub(true)
	repo := newRadarRunnerTestRepository()
	started := make(chan struct{})
	release := make(chan struct{})
	unexpectedCancellation := make(chan error, 1)
	fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusClaude, interval: time.Hour, fn: func(ctx context.Context) ([]byte, SourceFetchMeta, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			unexpectedCancellation <- ctx.Err()
			return nil, SourceFetchMeta{}, ctx.Err()
		}
		now := time.Now().UTC()
		return []byte(`{"ok":true}`), SourceFetchMeta{LastAttemptAt: now, LastSuccessAt: &now}, nil
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{runtimeGate: gate, fetchBudget: time.Second}, fetcher)
	runner.Start()
	waitRadarRunnerSignal(t, started, "in-flight runtime-gated fetch")

	gate.enabled.Store(false)
	close(release)
	waitRadarRunnerRepoEvent(t, repo, "commit", RadarSourceStatusClaude)
	require.Equal(t, int32(1), fetcher.calls.Load())
	select {
	case err := <-unexpectedCancellation:
		t.Fatalf("runtime disable canceled an in-flight source: %v", err)
	default:
	}
}

func TestRadarRunnerManualOnceCanExplicitlyBypassRuntimeGate(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	gate := newRadarRuntimeGateStub(false)
	repo := newRadarRunnerTestRepository()
	fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusOpenAI, interval: time.Hour}
	aggregator := &radarRunnerQuotaAggregatorFake{}
	runner := newRadarRunnerWithQuotaForTest(t, cfg, repo, aggregator, radarRunnerOptions{runtimeGate: gate}, fetcher)
	callsBefore := gate.calls.Load()

	runner.fetchOnceWithRuntimeGate(context.Background(), fetcher, fetcher.source, time.Now().Add(time.Hour), true)
	runner.runQuotaAggregatorOnceWithRuntimeGate(context.Background(), true)

	require.Equal(t, int32(1), fetcher.calls.Load())
	require.Equal(t, int32(1), aggregator.calls.Load())
	require.Equal(t, callsBefore, gate.calls.Load(), "manual bypass must not be re-gated inside the common once paths")
}

func TestRadarRunnerMetricsUseEffectiveRuntimeValue(t *testing.T) {
	tests := []struct {
		name          string
		staticEnabled bool
		runtime       bool
		wantGauge     string
		wantSource    bool
	}{
		{name: "static false runtime true", runtime: true, wantGauge: "radar_enabled 1", wantSource: true},
		{name: "static true runtime false", staticEnabled: true, wantGauge: "radar_enabled 0", wantSource: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			metrics, err := observability.NewRadarMetrics(registry)
			require.NoError(t, err)
			cfg := validRadarFetcherTestConfig()
			cfg.Radar.Enabled = tt.staticEnabled
			gate := newRadarRuntimeGateStub(tt.runtime)
			repo := newRadarRunnerTestRepository()
			source := RadarSourceAA
			fetcher := &radarRunnerTestFetcher{source: source, interval: time.Hour}
			_ = newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{metrics: metrics, runtimeGate: gate}, fetcher)

			body := scrapeRadarMetricsForTest(t, registry)
			require.Contains(t, body, tt.wantGauge)
			if tt.wantSource {
				require.Contains(t, body, `radar_source_age_seconds{source="aa"}`)
			} else {
				require.NotContains(t, body, `radar_source_age_seconds{source="aa"}`)
			}
		})
	}
}

func TestRadarRunnerStoredRuntimeTrueOverridesStaticFalse(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.Enabled = false
	settingRepo := &radarRuntimeSettingRepo{values: []string{"true"}}
	settingService := NewSettingService(settingRepo, cfg)
	repo := newRadarRunnerTestRepository()
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{runtimeGate: settingService}, fetcher)

	runner.Start()
	waitRadarRunnerRepoEvent(t, repo, "commit", RadarSourceLMArena)
	require.Equal(t, int32(1), fetcher.calls.Load())
	settingRepo.mu.Lock()
	writes := append([]string(nil), settingRepo.writes...)
	settingRepo.mu.Unlock()
	require.Empty(t, writes, "runtime fallback/reads must not materialize settings")
}

func TestRadarRunnerRuntimeTransitionUpdatesGaugeAndRegistersConfiguredSources(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewRadarMetrics(registry)
	require.NoError(t, err)
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.Enabled = true // runtime false must override the static default
	gate := newRadarRuntimeGateStub(false)
	clock := newRadarRunnerControllableClock(time.Now().UTC())
	repo := newRadarRunnerTestRepository()
	source := RadarSourceAA
	fetcher := &radarRunnerTestFetcher{source: source, interval: time.Hour}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{
		clock: clock, metrics: metrics, runtimeGate: gate,
	}, fetcher)

	runner.Start()
	firstTimer := waitRadarRunnerTimer(t, clock, time.Hour)
	before := scrapeRadarMetricsForTest(t, registry)
	require.Contains(t, before, "radar_enabled 0")
	require.NotContains(t, before, `radar_source_age_seconds{source="aa"}`)

	gate.enabled.Store(true)
	firstTimer.fire(time.Now())
	waitRadarRunnerRepoEvent(t, repo, "commit", source)
	after := scrapeRadarMetricsForTest(t, registry)
	require.Contains(t, after, "radar_enabled 1")
	require.Contains(t, after, `radar_source_age_seconds{source="aa"}`)
}
