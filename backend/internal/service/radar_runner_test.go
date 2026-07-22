package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

const radarRunnerTestMaxExactCadenceVersion int64 = 9007199254740991

type radarRunnerTestFetcher struct {
	source   RadarSourceKey
	interval time.Duration
	fn       func(context.Context) ([]byte, SourceFetchMeta, error)
	calls    atomic.Int32
}

func (f *radarRunnerTestFetcher) Source() RadarSourceKey  { return f.source }
func (f *radarRunnerTestFetcher) Interval() time.Duration { return f.interval }
func (f *radarRunnerTestFetcher) Fetch(ctx context.Context) ([]byte, SourceFetchMeta, error) {
	f.calls.Add(1)
	if f.fn != nil {
		return f.fn(ctx)
	}
	now := time.Now().UTC()
	status := 200
	return []byte(`{"ok":true}`), SourceFetchMeta{
		LastAttemptAt: now,
		LastSuccessAt: &now,
		HTTPStatus:    &status,
	}, nil
}

type radarRunnerLockCall struct {
	task  string
	owner string
	ttl   time.Duration
}

type radarRunnerCommitCall struct {
	source    RadarSourceKey
	payload   []byte
	retention time.Duration
	meta      SourceFetchMeta
}

type radarRunnerMetaCall struct {
	source RadarSourceKey
	meta   SourceFetchMeta
}

type radarRunnerRepoEvent struct {
	kind   string
	source RadarSourceKey
}

type radarRunnerCacheOnlyRepository struct {
	RadarCacheRepository
}

type radarRunnerTestRepository struct {
	mu sync.Mutex

	tryLockCalls       []radarRunnerLockCall
	releaseLockCalls   []radarRunnerLockCall
	commitCalls        []radarRunnerCommitCall
	failureCommitCalls []radarRunnerMetaCall
	metaCalls          []radarRunnerMetaCall
	setPayloadCalls    int
	releaseContextErr  []error
	payloads           map[RadarSourceKey][]byte
	metas              map[RadarSourceKey]SourceFetchMeta
	cadences           map[RadarSourceKey]RadarSourceCadence
	aggregatorState    RadarAggregatorRunState

	tryLockFn       func(context.Context, string, string, time.Duration) (bool, error)
	releaseLockFn   func(context.Context, string, string) error
	commitFn        func(context.Context, RadarSourceKey, []byte, time.Duration, SourceFetchMeta) (bool, error)
	commitFailureFn func(context.Context, RadarSourceKey, SourceFetchMeta) (bool, error)
	setMetaFn       func(context.Context, RadarSourceKey, SourceFetchMeta) error
	listMetaFn      func(context.Context) (map[RadarSourceKey]SourceFetchMeta, error)

	events chan radarRunnerRepoEvent
}

func newRadarRunnerTestRepository() *radarRunnerTestRepository {
	return &radarRunnerTestRepository{
		payloads: make(map[RadarSourceKey][]byte),
		metas:    make(map[RadarSourceKey]SourceFetchMeta),
		cadences: make(map[RadarSourceKey]RadarSourceCadence),
		events:   make(chan radarRunnerRepoEvent, 256),
	}
}

func (r *radarRunnerTestRepository) signal(kind string, source RadarSourceKey) {
	select {
	case r.events <- radarRunnerRepoEvent{kind: kind, source: source}:
	default:
	}
}

func (r *radarRunnerTestRepository) AppendBucketSnapshot(context.Context, BucketSnapshotDTO) error {
	return nil
}
func (r *radarRunnerTestRepository) ListBucketKeys(context.Context) ([]string, error) {
	return nil, nil
}
func (r *radarRunnerTestRepository) GetLatestBucket(context.Context, string) (*BucketSnapshotDTO, error) {
	return nil, ErrRadarCacheMiss
}
func (r *radarRunnerTestRepository) GetBucketTrend(context.Context, string, time.Time) ([]BucketSnapshotDTO, error) {
	return nil, nil
}
func (r *radarRunnerTestRepository) SetSourcePayload(_ context.Context, source RadarSourceKey, payload []byte, _ time.Duration) error {
	r.mu.Lock()
	r.setPayloadCalls++
	r.payloads[source] = append([]byte(nil), payload...)
	r.mu.Unlock()
	r.signal("set_payload", source)
	return nil
}
func (r *radarRunnerTestRepository) GetSourcePayload(_ context.Context, source RadarSourceKey) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	payload, ok := r.payloads[source]
	if !ok {
		return nil, ErrRadarCacheMiss
	}
	return append([]byte(nil), payload...), nil
}
func (r *radarRunnerTestRepository) CommitSourceSuccess(
	ctx context.Context,
	source RadarSourceKey,
	payload []byte,
	retention time.Duration,
	meta SourceFetchMeta,
) (bool, error) {
	r.mu.Lock()
	selectedMeta, selectedCadence, selectErr := selectRadarRunnerTestCadence(meta, r.cadences[source])
	if selectErr != nil {
		r.mu.Unlock()
		return false, selectErr
	}
	meta = selectedMeta
	r.cadences[source] = selectedCadence
	r.commitCalls = append(r.commitCalls, radarRunnerCommitCall{
		source:    source,
		payload:   append([]byte(nil), payload...),
		retention: retention,
		meta:      meta,
	})
	fn := r.commitFn
	r.mu.Unlock()
	r.signal("commit", source)

	applied := true
	var err error
	if fn != nil {
		applied, err = fn(ctx, source, payload, retention, meta)
	}
	if err == nil && applied {
		r.mu.Lock()
		r.payloads[source] = append([]byte(nil), payload...)
		r.metas[source] = cloneRadarRunnerTestMeta(meta)
		r.mu.Unlock()
	}
	return applied, err
}
func (r *radarRunnerTestRepository) CommitSourceFailure(
	ctx context.Context,
	source RadarSourceKey,
	meta SourceFetchMeta,
) (bool, error) {
	r.mu.Lock()
	selectedMeta, selectedCadence, selectErr := selectRadarRunnerTestCadence(meta, r.cadences[source])
	if selectErr != nil {
		r.mu.Unlock()
		return false, selectErr
	}
	meta = selectedMeta
	r.cadences[source] = selectedCadence
	r.failureCommitCalls = append(r.failureCommitCalls, radarRunnerMetaCall{source: source, meta: meta})
	fn := r.commitFailureFn
	r.mu.Unlock()
	r.signal("commit_failure", source)

	applied := true
	var err error
	if fn != nil {
		applied, err = fn(ctx, source, meta)
	}
	if err == nil && applied {
		r.mu.Lock()
		if current := r.metas[source]; current.LastSuccessAt != nil {
			lastSuccess := current.LastSuccessAt.UTC()
			meta.LastSuccessAt = &lastSuccess
		} else {
			meta.LastSuccessAt = nil
		}
		r.metas[source] = cloneRadarRunnerTestMeta(meta)
		r.mu.Unlock()
	}
	return applied, err
}
func (r *radarRunnerTestRepository) SetSourceMeta(ctx context.Context, source RadarSourceKey, meta SourceFetchMeta) error {
	r.mu.Lock()
	selectedMeta, selectedCadence, selectErr := selectRadarRunnerTestCadence(meta, r.cadences[source])
	if selectErr != nil {
		r.mu.Unlock()
		return selectErr
	}
	meta = selectedMeta
	r.cadences[source] = selectedCadence
	r.metaCalls = append(r.metaCalls, radarRunnerMetaCall{source: source, meta: meta})
	fn := r.setMetaFn
	r.mu.Unlock()
	r.signal("meta", source)

	var err error
	if fn != nil {
		err = fn(ctx, source, meta)
	}
	if err == nil {
		r.mu.Lock()
		r.metas[source] = cloneRadarRunnerTestMeta(meta)
		r.mu.Unlock()
	}
	return err
}

func (r *radarRunnerTestRepository) AdvanceSourceNextFire(_ context.Context, source RadarSourceKey, nextFireAt time.Time) (RadarSourceCadence, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.cadences[source]
	nextVersion := int64(1)
	if current.Version != "" {
		currentVersion, err := radarRunnerTestCadenceVersion(current)
		if err != nil {
			return RadarSourceCadence{}, err
		}
		if currentVersion >= radarRunnerTestMaxExactCadenceVersion {
			return RadarSourceCadence{}, errors.New("radar source cadence overflow")
		}
		nextVersion = currentVersion + 1
	}
	cadence := RadarSourceCadence{NextFireAt: nextFireAt.UTC(), Version: strconv.FormatInt(nextVersion, 10)}
	r.cadences[source] = cadence
	if meta, ok := r.metas[source]; ok {
		meta.NextFireAt = radarRunnerTimePointer(cadence.NextFireAt)
		meta.CadenceVersion = cadence.Version
		r.metas[source] = cloneRadarRunnerTestMeta(meta)
	}
	return cadence, nil
}

func (r *radarRunnerTestRepository) GetSourceCadence(_ context.Context, source RadarSourceKey) (RadarSourceCadence, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cadence := r.cadences[source]
	if cadence.Version == "" || cadence.NextFireAt.IsZero() {
		return RadarSourceCadence{}, ErrRadarCacheMiss
	}
	return cadence, nil
}

func selectRadarRunnerTestCadence(meta SourceFetchMeta, persisted RadarSourceCadence) (SourceFetchMeta, RadarSourceCadence, error) {
	candidate := RadarSourceCadence{Version: meta.CadenceVersion}
	if meta.NextFireAt != nil {
		candidate.NextFireAt = meta.NextFireAt.UTC()
	}
	candidateHasDeadline := !candidate.NextFireAt.IsZero()
	candidateHasVersion := candidate.Version != ""
	persistedPresent := persisted.Version != "" || !persisted.NextFireAt.IsZero()
	if candidateHasVersion && !candidateHasDeadline {
		return SourceFetchMeta{}, RadarSourceCadence{}, errors.New("incomplete radar source cadence")
	}
	if candidateHasVersion {
		if _, err := radarRunnerTestCadenceVersion(candidate); err != nil {
			return SourceFetchMeta{}, RadarSourceCadence{}, err
		}
	}
	if persistedPresent {
		if _, err := radarRunnerTestCadenceVersion(persisted); err != nil {
			return SourceFetchMeta{}, RadarSourceCadence{}, err
		}
	}
	selected := candidate
	if candidateHasDeadline && !candidateHasVersion {
		nextVersion := int64(1)
		if persistedPresent {
			persistedVersion, _ := radarRunnerTestCadenceVersion(persisted)
			if persistedVersion >= radarRunnerTestMaxExactCadenceVersion {
				return SourceFetchMeta{}, RadarSourceCadence{}, errors.New("radar source cadence overflow")
			}
			nextVersion = persistedVersion + 1
		}
		selected.Version = strconv.FormatInt(nextVersion, 10)
	} else if persistedPresent && !candidateHasDeadline {
		selected = persisted
	} else if persistedPresent && candidateHasVersion {
		candidateVersion, _ := radarRunnerTestCadenceVersion(candidate)
		persistedVersion, _ := radarRunnerTestCadenceVersion(persisted)
		switch {
		case persistedVersion > candidateVersion:
			selected = persisted
		case persistedVersion == candidateVersion && !persisted.NextFireAt.Equal(candidate.NextFireAt):
			return SourceFetchMeta{}, RadarSourceCadence{}, errors.New("conflicting radar source cadence")
		}
	}
	if selected.Version != "" {
		meta.NextFireAt = radarRunnerTimePointer(selected.NextFireAt)
		meta.CadenceVersion = selected.Version
	}
	return meta, selected, nil
}

func radarRunnerTestCadenceVersion(cadence RadarSourceCadence) (int64, error) {
	if cadence.NextFireAt.IsZero() || cadence.Version == "" {
		return 0, errors.New("incomplete radar source cadence")
	}
	version, err := strconv.ParseInt(cadence.Version, 10, 64)
	if err != nil || version <= 0 || version > radarRunnerTestMaxExactCadenceVersion || strconv.FormatInt(version, 10) != cadence.Version {
		return 0, errors.New("invalid radar source cadence version")
	}
	return version, nil
}
func (r *radarRunnerTestRepository) ListSourceMeta(ctx context.Context) (map[RadarSourceKey]SourceFetchMeta, error) {
	r.mu.Lock()
	fn := r.listMetaFn
	if fn == nil {
		result := make(map[RadarSourceKey]SourceFetchMeta, len(r.metas))
		for source, meta := range r.metas {
			result[source] = cloneRadarRunnerTestMeta(meta)
		}
		r.mu.Unlock()
		return result, nil
	}
	r.mu.Unlock()
	return fn(ctx)
}
func (r *radarRunnerTestRepository) TryLock(ctx context.Context, task, owner string, ttl time.Duration) (bool, error) {
	r.mu.Lock()
	r.tryLockCalls = append(r.tryLockCalls, radarRunnerLockCall{task: task, owner: owner, ttl: ttl})
	fn := r.tryLockFn
	r.mu.Unlock()
	r.signal("try_lock", RadarSourceKey(task))
	if fn != nil {
		return fn(ctx, task, owner, ttl)
	}
	return true, nil
}
func (r *radarRunnerTestRepository) ReleaseLock(ctx context.Context, task, owner string) error {
	r.mu.Lock()
	r.releaseLockCalls = append(r.releaseLockCalls, radarRunnerLockCall{task: task, owner: owner})
	r.releaseContextErr = append(r.releaseContextErr, ctx.Err())
	fn := r.releaseLockFn
	r.mu.Unlock()
	r.signal("release", RadarSourceKey(task))
	if fn != nil {
		return fn(ctx, task, owner)
	}
	return nil
}

func (r *radarRunnerTestRepository) CommitRadarAggregatorRun(_ context.Context, state RadarAggregatorRunState) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state.RunVersion <= r.aggregatorState.RunVersion {
		return false, nil
	}
	if !state.Success {
		state.PublishedBucketCount = r.aggregatorState.PublishedBucketCount
	}
	r.aggregatorState = state
	return true, nil
}

func cloneRadarRunnerTestMeta(meta SourceFetchMeta) SourceFetchMeta {
	cloned := meta
	if meta.LastSuccessAt != nil {
		value := *meta.LastSuccessAt
		cloned.LastSuccessAt = &value
	}
	if meta.NextFireAt != nil {
		value := *meta.NextFireAt
		cloned.NextFireAt = &value
	}
	if meta.HTTPStatus != nil {
		value := *meta.HTTPStatus
		cloned.HTTPStatus = &value
	}
	if meta.Error != nil {
		value := *meta.Error
		cloned.Error = &value
	}
	return cloned
}

func (r *radarRunnerTestRepository) snapshot() (
	[]radarRunnerLockCall,
	[]radarRunnerLockCall,
	[]radarRunnerCommitCall,
	[]radarRunnerMetaCall,
	int,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]radarRunnerLockCall(nil), r.tryLockCalls...),
		append([]radarRunnerLockCall(nil), r.releaseLockCalls...),
		append([]radarRunnerCommitCall(nil), r.commitCalls...),
		append([]radarRunnerMetaCall(nil), r.metaCalls...),
		r.setPayloadCalls
}

type radarRunnerRealTestTimer struct{ timer *time.Timer }

func (t *radarRunnerRealTestTimer) C() <-chan time.Time { return t.timer.C }
func (t *radarRunnerRealTestTimer) Stop() bool          { return t.timer.Stop() }

type radarRunnerFixedTestClock struct{ now time.Time }

func (c radarRunnerFixedTestClock) Now() time.Time { return c.now }
func (c radarRunnerFixedTestClock) NewTimer(duration time.Duration) radarRunnerTimer {
	return &radarRunnerRealTestTimer{timer: time.NewTimer(duration)}
}

type radarRunnerQuotaAggregatorFake struct {
	calls atomic.Int32
	fn    func(context.Context) (RadarQuotaAggregationReport, error)
}

func (f *radarRunnerQuotaAggregatorFake) RunOnceWithReport(ctx context.Context) (RadarQuotaAggregationReport, error) {
	f.calls.Add(1)
	if f.fn != nil {
		return f.fn(ctx)
	}
	return RadarQuotaAggregationReport{}, nil
}

type radarRunnerControllableTimer struct {
	duration time.Duration
	ch       chan time.Time
	stopped  atomic.Bool
}

func (t *radarRunnerControllableTimer) C() <-chan time.Time { return t.ch }
func (t *radarRunnerControllableTimer) Stop() bool {
	return !t.stopped.Swap(true)
}
func (t *radarRunnerControllableTimer) fire(at time.Time) {
	select {
	case t.ch <- at:
	default:
	}
}

type radarRunnerControllableClock struct {
	now         time.Time
	nilDuration time.Duration
	created     chan *radarRunnerControllableTimer
}

func newRadarRunnerControllableClock(now time.Time) *radarRunnerControllableClock {
	return &radarRunnerControllableClock{
		now:     now,
		created: make(chan *radarRunnerControllableTimer, 64),
	}
}

func (c *radarRunnerControllableClock) Now() time.Time { return c.now }
func (c *radarRunnerControllableClock) NewTimer(duration time.Duration) radarRunnerTimer {
	if duration == c.nilDuration && c.nilDuration > 0 {
		return nil
	}
	timer := &radarRunnerControllableTimer{duration: duration, ch: make(chan time.Time, 1)}
	c.created <- timer
	return timer
}

func waitRadarRunnerTimer(t *testing.T, clock *radarRunnerControllableClock, duration time.Duration) *radarRunnerControllableTimer {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case timer := <-clock.created:
			if timer.duration == duration {
				return timer
			}
		case <-timeout.C:
			t.Fatalf("timed out waiting for Radar runner timer %s", duration)
		}
	}
}

func newRadarRunnerWithQuotaForTest(
	t *testing.T,
	cfg *config.Config,
	repo RadarCacheRepository,
	aggregator radarQuotaAggregationRunner,
	options radarRunnerOptions,
	fetchers ...RadarFetcher,
) *RadarRunner {
	t.Helper()
	if options.fetchBudget == 0 {
		options.fetchBudget = 5 * time.Millisecond
	}
	if options.persistenceTimeout == 0 {
		options.persistenceTimeout = 5 * time.Millisecond
	}
	if options.cleanupTimeout == 0 {
		options.cleanupTimeout = 5 * time.Millisecond
	}
	if options.shutdownTimeout == 0 {
		options.shutdownTimeout = 500 * time.Millisecond
	}
	if options.owner == "" {
		options.owner = "runner-quota-test-owner"
	}
	runner, err := newRadarRunner(cfg, repo, fetchers, aggregator, options)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runner.Stop(ctx)
	})
	return runner
}

func newRadarRunnerForTest(
	t *testing.T,
	cfg *config.Config,
	repo RadarCacheRepository,
	options radarRunnerOptions,
	fetchers ...RadarFetcher,
) *RadarRunner {
	t.Helper()
	if options.fetchBudget == 0 {
		options.fetchBudget = 5 * time.Millisecond
	}
	if options.persistenceTimeout == 0 {
		options.persistenceTimeout = 5 * time.Millisecond
	}
	if options.cleanupTimeout == 0 {
		options.cleanupTimeout = 5 * time.Millisecond
	}
	if options.shutdownTimeout == 0 {
		options.shutdownTimeout = 500 * time.Millisecond
	}
	if options.owner == "" {
		options.owner = "runner-test-owner"
	}
	options.skipQuotaScheduler = true
	runner, err := newRadarRunner(cfg, repo, fetchers, &radarRunnerQuotaAggregatorFake{}, options)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runner.Stop(ctx)
	})
	return runner
}

func waitRadarRunnerRepoEvent(t *testing.T, repo *radarRunnerTestRepository, kind string, source RadarSourceKey) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-repo.events:
			if event.kind == kind && event.source == source {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for repository event %s/%s", kind, source)
		}
	}
}

func waitRadarRunnerSignal(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func TestRadarRunnerImmediatePeriodicAndStartIdempotent(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	fetched := make(chan struct{}, 4)
	fetcher := &radarRunnerTestFetcher{
		source:   RadarSourceLMArena,
		interval: 30 * time.Millisecond,
		fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
			now := time.Now().UTC()
			status := 200
			fetched <- struct{}{}
			return []byte(`{"leaderboard":[]}`), SourceFetchMeta{LastAttemptAt: now, LastSuccessAt: &now, HTTPStatus: &status}, nil
		},
	}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, fetcher)

	runner.Start()
	runner.Start()
	waitRadarRunnerSignal(t, fetched, "immediate fetch")
	waitRadarRunnerSignal(t, fetched, "periodic fetch")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(ctx))
	require.GreaterOrEqual(t, fetcher.calls.Load(), int32(2))

	locks, _, commits, _, _ := repo.snapshot()
	require.GreaterOrEqual(t, len(locks), 2)
	for _, call := range locks {
		require.Equal(t, string(RadarSourceLMArena), call.task)
		require.NotEmpty(t, call.owner)
		require.Greater(t, call.ttl, 15*time.Millisecond)
		require.Less(t, call.ttl, fetcher.interval)
	}
	require.GreaterOrEqual(t, len(commits), 2)
}

func TestRadarRunnerDisabledSchedulesNothing(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.Enabled = false
	repo := newRadarRunnerTestRepository()
	called := make(chan struct{}, 1)
	fetcher := &radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
		called <- struct{}{}
		return nil, SourceFetchMeta{}, nil
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, fetcher)

	runner.Start()
	runner.Start()
	select {
	case <-called:
		t.Fatal("disabled runner fetched a source")
	case <-time.After(25 * time.Millisecond):
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(ctx))
	locks, releases, commits, metas, _ := repo.snapshot()
	require.Empty(t, locks)
	require.Empty(t, releases)
	require.Empty(t, commits)
	require.Empty(t, metas)
}

func TestRadarRunnerStartTwiceDoesNotDuplicateSourceLoop(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	started := make(chan struct{}, 2)
	unblock := make(chan struct{})
	fetcher := &radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
		started <- struct{}{}
		<-unblock
		now := time.Now().UTC()
		return []byte(`{}`), SourceFetchMeta{LastAttemptAt: now, LastSuccessAt: &now}, nil
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{fetchBudget: time.Second}, fetcher)

	runner.Start()
	runner.Start()
	waitRadarRunnerSignal(t, started, "first source loop")
	select {
	case <-started:
		t.Fatal("Start created a duplicate source loop")
	case <-time.After(25 * time.Millisecond):
	}
	close(unblock)
}

func TestRadarRunnerStopCancelsFetchAndReleasesWithIndependentContext(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	started := make(chan struct{})
	canceled := make(chan struct{})
	fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusClaude, interval: time.Second, fn: func(ctx context.Context) ([]byte, SourceFetchMeta, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, SourceFetchMeta{LastAttemptAt: time.Now()}, errors.New("raw-secret-in-fetch-error")
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{fetchBudget: 500 * time.Millisecond}, fetcher)
	runner.Start()
	waitRadarRunnerSignal(t, started, "in-flight fetch")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(ctx))
	waitRadarRunnerSignal(t, canceled, "fetch cancellation")
	waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceStatusClaude)

	repo.mu.Lock()
	require.Len(t, repo.releaseContextErr, 1)
	require.NoError(t, repo.releaseContextErr[0], "lock cleanup must not reuse the canceled job context")
	require.Empty(t, repo.failureCommitCalls, "parent shutdown cancellation must not persist a source failure")
	require.Empty(t, repo.metaCalls)
	repo.mu.Unlock()
	require.NoError(t, runner.Stop(ctx), "Stop must be repeatable")
}

func TestRadarRunnerConcurrentStopIsSafe(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	started := make(chan struct{})
	fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusClaude, interval: time.Hour, fn: func(ctx context.Context) ([]byte, SourceFetchMeta, error) {
		close(started)
		<-ctx.Done()
		return nil, SourceFetchMeta{LastAttemptAt: time.Now()}, ctx.Err()
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{fetchBudget: time.Second}, fetcher)
	runner.Start()
	waitRadarRunnerSignal(t, started, "concurrent Stop fetch")

	const callers = 12
	errorsByCaller := make(chan error, callers)
	var callersWG sync.WaitGroup
	callersWG.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer callersWG.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			errorsByCaller <- runner.Stop(ctx)
		}()
	}
	callersWG.Wait()
	close(errorsByCaller)
	for err := range errorsByCaller {
		require.NoError(t, err)
	}
}

func TestRadarRunnerStopTimeoutCanBeRetried(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	started := make(chan struct{})
	unblock := make(chan struct{})
	fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusOpenAI, interval: time.Second, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
		close(started)
		<-unblock // deliberately violate the Fetch context contract to exercise bounded Stop
		now := time.Now().UTC()
		return nil, SourceFetchMeta{LastAttemptAt: now}, context.Canceled
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{
		fetchBudget:     500 * time.Millisecond,
		shutdownTimeout: 20 * time.Millisecond,
	}, fetcher)
	runner.Start()
	waitRadarRunnerSignal(t, started, "uncooperative fetch")

	err := runner.Stop(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(unblock)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(ctx))
}

func TestRadarRunnerStopBeforeStartPreventsRestart(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	fetcher := &radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, fetcher)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(ctx))
	require.NoError(t, runner.Stop(ctx))
	runner.Start()
	time.Sleep(10 * time.Millisecond)
	require.Zero(t, fetcher.calls.Load())
}

func TestRadarRunnerLockMissSkipsFetchAndRelease(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	repo.tryLockFn = func(context.Context, string, string, time.Duration) (bool, error) { return false, nil }
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, fetcher)
	runner.Start()
	waitRadarRunnerRepoEvent(t, repo, "try_lock", RadarSourceLMArena)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(ctx))
	require.Zero(t, fetcher.calls.Load())
	_, releases, commits, metas, _ := repo.snapshot()
	require.Empty(t, releases)
	require.Empty(t, commits)
	require.Empty(t, metas)
}

func TestRadarRunnerSuccessUsesAtomicCommitWithSafeIndependentMeta(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.SourceHardRetentionDays = 9
	cfg.Radar.QuotaStaleThresholdMinutes = 30
	cfg.Radar.HealthStaleThresholdMinutes = 60
	cfg.Radar.ArtificialAnalysisModelsStaleThresholdMinutes = 720
	cfg.Radar.LMArenaStaleThresholdMinutes = 2880
	repo := newRadarRunnerTestRepository()
	location := time.FixedZone("unsafe-local", 8*60*60)
	attempt := time.Date(2026, 7, 13, 9, 30, 0, 0, location)
	success := attempt.Add(time.Second)
	status := 204
	errorCode := DataSourceErrorCodeUnauthorized
	clockNow := time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)
	fetcher := &radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
		return []byte(`{"secret_payload":"must-not-be-logged"}`), SourceFetchMeta{
			LastAttemptAt: attempt,
			LastSuccessAt: &success,
			HTTPStatus:    &status,
			Error:         &errorCode,
		}, nil
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{
		clock: radarRunnerFixedTestClock{now: clockNow},
	}, fetcher)
	runner.Start()
	waitRadarRunnerRepoEvent(t, repo, "commit", RadarSourceAA)

	repo.mu.Lock()
	require.Len(t, repo.commitCalls, 1)
	call := repo.commitCalls[0]
	require.Equal(t, 9*24*time.Hour, call.retention)
	require.Nil(t, call.meta.Error)
	require.Equal(t, attempt.UTC(), call.meta.LastAttemptAt)
	require.NotNil(t, call.meta.LastSuccessAt)
	require.Equal(t, success.UTC(), *call.meta.LastSuccessAt)
	require.NotNil(t, call.meta.NextFireAt)
	require.Equal(t, clockNow.Add(time.Hour), *call.meta.NextFireAt)
	require.NotNil(t, call.meta.HTTPStatus)
	require.Equal(t, 204, *call.meta.HTTPStatus)
	require.Equal(t, time.UTC, call.meta.LastAttemptAt.Location())
	require.Equal(t, time.UTC, call.meta.LastSuccessAt.Location())
	require.Equal(t, time.UTC, call.meta.NextFireAt.Location())
	require.Zero(t, repo.setPayloadCalls)
	require.Empty(t, repo.metaCalls, "success must not split payload and metadata writes")
	repo.mu.Unlock()

	// Returned pointers belong to the fetcher. Mutating them must not mutate the
	// metadata handed to the repository.
	success = success.Add(24 * time.Hour)
	status = 500
	errorCode = DataSourceErrorCodeNetworkError
	repo.mu.Lock()
	require.Equal(t, time.Date(2026, 7, 13, 1, 30, 1, 0, time.UTC), *repo.commitCalls[0].meta.LastSuccessAt)
	require.Equal(t, 204, *repo.commitCalls[0].meta.HTTPStatus)
	repo.mu.Unlock()
}

func TestRadarRunnerSuccessCommitUsesFreshBoundedPersistenceContext(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	repo.commitFn = func(ctx context.Context, _ RadarSourceKey, _ []byte, _ time.Duration, _ SourceFetchMeta) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if _, ok := ctx.Deadline(); !ok {
			return false, errors.New("persistence context is unbounded")
		}
		return true, nil
	}
	fetcher := &radarRunnerTestFetcher{source: RadarSourceAA, interval: 100 * time.Millisecond, fn: func(ctx context.Context) ([]byte, SourceFetchMeta, error) {
		<-ctx.Done()
		now := time.Now().UTC()
		status := 200
		// Simulate validation completing at the deadline edge: the payload is
		// valid even though the original Fetch context has just expired.
		return []byte(`{"validated":true}`), SourceFetchMeta{
			LastAttemptAt: now,
			LastSuccessAt: &now,
			HTTPStatus:    &status,
		}, nil
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{
		fetchBudget:        5 * time.Millisecond,
		persistenceTimeout: 20 * time.Millisecond,
		cleanupTimeout:     5 * time.Millisecond,
	}, fetcher)
	runner.Start()
	waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceAA)

	repo.mu.Lock()
	storedPayload := append([]byte(nil), repo.payloads[RadarSourceAA]...)
	repo.mu.Unlock()
	require.Equal(t, []byte(`{"validated":true}`), storedPayload)
}

func TestRadarRunnerFailureUpdatesOnlySafeMetaAndPreservesOldPayload(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	oldSuccess := time.Date(2026, 7, 12, 3, 4, 5, 0, time.UTC)
	repo.payloads[RadarSourceLMArena] = []byte(`{"old":true}`)
	repo.metas[RadarSourceLMArena] = SourceFetchMeta{LastAttemptAt: oldSuccess, LastSuccessAt: &oldSuccess}
	const rawSecret = "https://upstream.invalid/path?api_key=super-secret"
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
		return []byte(`{"new":"must-be-ignored"}`), SourceFetchMeta{LastAttemptAt: time.Now()}, errors.New(rawSecret)
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, fetcher)
	runner.Start()
	waitRadarRunnerRepoEvent(t, repo, "commit_failure", RadarSourceLMArena)
	waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceLMArena)

	repo.mu.Lock()
	require.Equal(t, []byte(`{"old":true}`), repo.payloads[RadarSourceLMArena])
	require.Empty(t, repo.commitCalls)
	require.Zero(t, repo.setPayloadCalls)
	require.Empty(t, repo.metaCalls, "runner failures must use the atomic failure API")
	require.Len(t, repo.failureCommitCalls, 1)
	meta := cloneRadarRunnerTestMeta(repo.failureCommitCalls[0].meta)
	stored := cloneRadarRunnerTestMeta(repo.metas[RadarSourceLMArena])
	repo.mu.Unlock()
	require.NotNil(t, meta.Error)
	require.Equal(t, DataSourceErrorCodeNetworkError, *meta.Error)
	require.Nil(t, meta.LastSuccessAt, "repository owns atomic last-success preservation")
	require.NotNil(t, meta.NextFireAt)
	require.NotNil(t, stored.LastSuccessAt)
	require.Equal(t, oldSuccess, *stored.LastSuccessAt)
	encoded, err := json.Marshal(meta)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), rawSecret)
}

func TestRadarRunnerRejectsEmptyAAPageAndPreservesOldSnapshot(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	oldSuccess := time.Date(2026, 7, 12, 3, 4, 5, 0, time.UTC)
	repo.payloads[RadarSourceAA] = []byte(validAAModelsPayload)
	repo.metas[RadarSourceAA] = SourceFetchMeta{LastAttemptAt: oldSuccess, LastSuccessAt: &oldSuccess}

	emptyPayload := `{"tier":"free","intelligence_index_version":4.1,"pagination":{"page":1,"page_size":200,"total_pages":1,"has_more":false},"data":[]}`
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Body: newRadarTrackingBody(emptyPayload), ContentLength: int64(len(emptyPayload)),
		}, nil
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(cfg, client)
	require.NoError(t, err)
	setRadarFetcherSleep(t, fetcher, func(context.Context, time.Duration) error { return nil })
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{fetchBudget: time.Second}, fetcher)

	runner.Start()
	waitRadarRunnerRepoEvent(t, repo, "commit_failure", RadarSourceAA)
	waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceAA)

	repo.mu.Lock()
	storedPayload := append([]byte(nil), repo.payloads[RadarSourceAA]...)
	storedMeta := cloneRadarRunnerTestMeta(repo.metas[RadarSourceAA])
	commits := append([]radarRunnerCommitCall(nil), repo.commitCalls...)
	repo.mu.Unlock()
	require.Equal(t, []byte(validAAModelsPayload), storedPayload)
	require.Empty(t, commits)
	require.NotNil(t, storedMeta.LastSuccessAt)
	require.Equal(t, oldSuccess, storedMeta.LastSuccessAt.UTC())
	require.NotNil(t, storedMeta.Error)
	require.Equal(t, DataSourceErrorCodeInvalidResponse, *storedMeta.Error)
}

func TestRadarRunnerFetchDeadlinePersistsFailureWithIndependentBoundedContext(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	oldSuccess := time.Date(2026, 7, 12, 3, 4, 5, 0, time.UTC)
	repo.metas[RadarSourceStatusOpenAI] = SourceFetchMeta{
		LastAttemptAt: oldSuccess,
		LastSuccessAt: &oldSuccess,
	}
	repo.commitFailureFn = func(ctx context.Context, _ RadarSourceKey, _ SourceFetchMeta) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if _, ok := ctx.Deadline(); !ok {
			return false, errors.New("persistence context is unbounded")
		}
		return true, nil
	}
	fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusOpenAI, interval: time.Hour, fn: func(ctx context.Context) ([]byte, SourceFetchMeta, error) {
		<-ctx.Done()
		return nil, SourceFetchMeta{LastAttemptAt: time.Now()}, ctx.Err()
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{
		fetchBudget:        5 * time.Millisecond,
		persistenceTimeout: 50 * time.Millisecond,
		cleanupTimeout:     5 * time.Millisecond,
	}, fetcher)
	runner.Start()
	waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceStatusOpenAI)

	repo.mu.Lock()
	stored := cloneRadarRunnerTestMeta(repo.metas[RadarSourceStatusOpenAI])
	failureCalls := len(repo.failureCommitCalls)
	legacyMetaCalls := len(repo.metaCalls)
	repo.mu.Unlock()
	require.Equal(t, 1, failureCalls)
	require.Zero(t, legacyMetaCalls)
	require.True(t, stored.LastAttemptAt.After(oldSuccess), "deadline failure metadata must be persisted")
	require.NotNil(t, stored.LastSuccessAt)
	require.Equal(t, oldSuccess, *stored.LastSuccessAt)
	require.NotNil(t, stored.Error)
	require.Equal(t, DataSourceErrorCodeNetworkError, *stored.Error)
}

func TestRadarRunnerOneSourceFailureDoesNotBlockOthers(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	failing := &radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
		return nil, SourceFetchMeta{LastAttemptAt: time.Now()}, errors.New("private failure")
	}}
	successful := &radarRunnerTestFetcher{source: RadarSourceStatusClaude, interval: time.Hour}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, failing, successful)
	runner.Start()
	wantFailure := true
	wantSuccess := true
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for wantFailure || wantSuccess {
		select {
		case event := <-repo.events:
			if event.kind == "commit_failure" && event.source == RadarSourceAA {
				wantFailure = false
			}
			if event.kind == "commit" && event.source == RadarSourceStatusClaude {
				wantSuccess = false
			}
		case <-timer.C:
			t.Fatal("timed out waiting for independent source results")
		}
	}
	require.Equal(t, int32(1), failing.calls.Load())
	require.Equal(t, int32(1), successful.calls.Load())
}

func TestRadarRunnerShutdownCancelsLongTimers(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo2 := newRadarRunnerTestRepository()
	fast := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour}
	runner2 := newRadarRunnerForTest(t, cfg, repo2, radarRunnerOptions{}, fast)
	runner2.Start()
	waitRadarRunnerRepoEvent(t, repo2, "commit", RadarSourceLMArena)
	startedAt := time.Now()
	require.NoError(t, runner2.Stop(context.Background()))
	require.Less(t, time.Since(startedAt), 100*time.Millisecond)
}

func TestRadarRunnerUsesUniqueOwnersAndCanonicalTasks(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	firstFetcher := &radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Minute}
	secondFetcher := &radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Minute}
	first, err := newRadarRunner(cfg, repo, []RadarFetcher{firstFetcher}, &radarRunnerQuotaAggregatorFake{}, radarRunnerOptions{skipQuotaScheduler: true})
	require.NoError(t, err)
	second, err := newRadarRunner(cfg, repo, []RadarFetcher{secondFetcher}, &radarRunnerQuotaAggregatorFake{}, radarRunnerOptions{skipQuotaScheduler: true})
	require.NoError(t, err)
	first.Start()
	second.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = first.Stop(ctx)
		_ = second.Stop(ctx)
	})
	waitRadarRunnerRepoEvent(t, repo, "try_lock", RadarSourceAA)
	waitRadarRunnerRepoEvent(t, repo, "try_lock", RadarSourceAA)

	repo.mu.Lock()
	require.GreaterOrEqual(t, len(repo.tryLockCalls), 2)
	firstCall := repo.tryLockCalls[0]
	secondCall := repo.tryLockCalls[1]
	repo.mu.Unlock()
	require.Equal(t, string(RadarSourceAA), firstCall.task)
	require.Equal(t, string(RadarSourceAA), secondCall.task)
	require.NotEmpty(t, firstCall.owner)
	require.NotEmpty(t, secondCall.owner)
	require.NotEqual(t, firstCall.owner, secondCall.owner)
}

func TestRadarRunnerCommitFalseIsBenignAndStillReleasesLock(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	repo.commitFn = func(context.Context, RadarSourceKey, []byte, time.Duration, SourceFetchMeta) (bool, error) {
		return false, nil
	}
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, fetcher)
	runner.Start()
	waitRadarRunnerRepoEvent(t, repo, "commit", RadarSourceLMArena)
	waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceLMArena)
	_, releases, _, metas, _ := repo.snapshot()
	require.Len(t, releases, 1)
	require.Empty(t, metas)
}

func TestRadarRunnerFailureCommitFalseIsBenignAndStillReleasesLock(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	repo.commitFailureFn = func(context.Context, RadarSourceKey, SourceFetchMeta) (bool, error) {
		return false, nil
	}
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
		return nil, SourceFetchMeta{LastAttemptAt: time.Now()}, errors.New("private failure")
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, fetcher)
	runner.Start()
	waitRadarRunnerRepoEvent(t, repo, "commit_failure", RadarSourceLMArena)
	waitRadarRunnerRepoEvent(t, repo, "release", RadarSourceLMArena)
	repo.mu.Lock()
	require.Len(t, repo.failureCommitCalls, 1)
	require.Empty(t, repo.metaCalls)
	require.Empty(t, repo.commitCalls)
	repo.mu.Unlock()
}

func TestRadarRunnerRepositoryErrorsDoNotPanicOrLeakLocks(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*radarRunnerTestRepository)
		fetchErr  bool
		waitKind  string
	}{
		{
			name: "try lock",
			configure: func(repo *radarRunnerTestRepository) {
				repo.tryLockFn = func(context.Context, string, string, time.Duration) (bool, error) {
					return false, errors.New("redis-secret")
				}
			},
			waitKind: "try_lock",
		},
		{
			name: "commit",
			configure: func(repo *radarRunnerTestRepository) {
				repo.commitFn = func(context.Context, RadarSourceKey, []byte, time.Duration, SourceFetchMeta) (bool, error) {
					return false, errors.New("redis-secret")
				}
			},
			waitKind: "release",
		},
		{
			name: "failure metadata",
			configure: func(repo *radarRunnerTestRepository) {
				repo.commitFailureFn = func(context.Context, RadarSourceKey, SourceFetchMeta) (bool, error) {
					return false, errors.New("redis-secret")
				}
			},
			fetchErr: true,
			waitKind: "release",
		},
		{
			name: "release",
			configure: func(repo *radarRunnerTestRepository) {
				repo.releaseLockFn = func(context.Context, string, string) error {
					return errors.New("redis-secret")
				}
			},
			waitKind: "release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validRadarFetcherTestConfig()
			repo := newRadarRunnerTestRepository()
			tt.configure(repo)
			fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusOpenAI, interval: time.Hour}
			if tt.fetchErr {
				fetcher.fn = func(context.Context) ([]byte, SourceFetchMeta, error) {
					return nil, SourceFetchMeta{LastAttemptAt: time.Now()}, errors.New("fetch-secret")
				}
			}
			runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, fetcher)
			runner.Start()
			waitRadarRunnerRepoEvent(t, repo, tt.waitKind, RadarSourceStatusOpenAI)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, runner.Stop(ctx))
		})
	}
}

func TestRadarRunnerLogsOnlySafeOperationClassifications(t *testing.T) {
	const rawSecret = "https://secret.invalid/private?api_key=SUPER_SECRET_QUERY"
	tests := []struct {
		name      string
		operation string
		safeClass string
		fetchErr  bool
		waitKind  string
		configure func(*radarRunnerTestRepository)
	}{
		{
			name:      "try lock",
			operation: "try_lock",
			safeClass: "storage_error",
			waitKind:  "try_lock",
			configure: func(repo *radarRunnerTestRepository) {
				repo.tryLockFn = func(context.Context, string, string, time.Duration) (bool, error) {
					return false, errors.New(rawSecret)
				}
			},
		},
		{
			name:      "fetch",
			operation: "fetch",
			safeClass: string(DataSourceErrorCodeNetworkError),
			fetchErr:  true,
			waitKind:  "release",
			configure: func(*radarRunnerTestRepository) {},
		},
		{
			name:      "commit success",
			operation: "commit_success",
			safeClass: "storage_error",
			waitKind:  "release",
			configure: func(repo *radarRunnerTestRepository) {
				repo.commitFn = func(context.Context, RadarSourceKey, []byte, time.Duration, SourceFetchMeta) (bool, error) {
					return false, errors.New(rawSecret)
				}
			},
		},
		{
			name:      "commit failure",
			operation: "commit_failure",
			safeClass: "storage_error",
			fetchErr:  true,
			waitKind:  "release",
			configure: func(repo *radarRunnerTestRepository) {
				repo.commitFailureFn = func(context.Context, RadarSourceKey, SourceFetchMeta) (bool, error) {
					return false, errors.New(rawSecret)
				}
			},
		},
		{
			name:      "release lock",
			operation: "release_lock",
			safeClass: "storage_error",
			waitKind:  "release",
			configure: func(repo *radarRunnerTestRepository) {
				repo.releaseLockFn = func(context.Context, string, string) error {
					return errors.New(rawSecret)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validRadarFetcherTestConfig()
			repo := newRadarRunnerTestRepository()
			tt.configure(repo)
			var logBuffer bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))
			fetcher := &radarRunnerTestFetcher{source: RadarSourceStatusOpenAI, interval: time.Hour}
			if tt.fetchErr {
				fetcher.fn = func(context.Context) ([]byte, SourceFetchMeta, error) {
					return []byte(rawSecret), SourceFetchMeta{LastAttemptAt: time.Now()}, errors.New(rawSecret)
				}
			}
			runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{logger: logger}, fetcher)
			runner.Start()
			waitRadarRunnerRepoEvent(t, repo, tt.waitKind, RadarSourceStatusOpenAI)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, runner.Stop(ctx))

			logged := logBuffer.String()
			require.Contains(t, logged, `"source":"status_openai"`)
			require.Contains(t, logged, `"operation":"`+tt.operation+`"`)
			require.Contains(t, logged, `"class":"`+tt.safeClass+`"`)
			require.NotContains(t, logged, rawSecret)
			require.NotContains(t, logged, "SUPER_SECRET_QUERY")
			require.NotContains(t, logged, "api_key")
		})
	}
}

func TestRadarRunnerLockMissDoesNotLogFailure(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	repo.tryLockFn = func(context.Context, string, string, time.Duration) (bool, error) { return false, nil }
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{logger: logger}, fetcher)
	runner.Start()
	waitRadarRunnerRepoEvent(t, repo, "try_lock", RadarSourceLMArena)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(ctx))
	require.Empty(t, logBuffer.String())
}

func TestNewRadarRunnerRejectsInvalidDependenciesDuplicatesAndUnsafeIntervals(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	valid := &radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Minute}
	var typedNilFetcher *radarRunnerTestFetcher
	var typedNilRepo *radarRunnerTestRepository

	tests := []struct {
		name     string
		cfg      *config.Config
		repo     RadarCacheRepository
		fetchers []RadarFetcher
		options  radarRunnerOptions
	}{
		{name: "nil config", repo: repo, fetchers: []RadarFetcher{valid}},
		{name: "nil repository", cfg: cfg, fetchers: []RadarFetcher{valid}},
		{name: "typed nil repository", cfg: cfg, repo: typedNilRepo, fetchers: []RadarFetcher{valid}},
		{name: "repository without cadence contract", cfg: cfg, repo: radarRunnerCacheOnlyRepository{RadarCacheRepository: repo}, fetchers: []RadarFetcher{valid}},
		{name: "nil fetcher slice", cfg: cfg, repo: repo},
		{name: "nil fetcher", cfg: cfg, repo: repo, fetchers: []RadarFetcher{nil}},
		{name: "typed nil fetcher", cfg: cfg, repo: repo, fetchers: []RadarFetcher{typedNilFetcher}},
		{name: "duplicate source", cfg: cfg, repo: repo, fetchers: []RadarFetcher{valid, valid}},
		{name: "zero interval", cfg: cfg, repo: repo, fetchers: []RadarFetcher{&radarRunnerTestFetcher{source: RadarSourceAA}}},
		{
			name:     "interval equals budget",
			cfg:      cfg,
			repo:     repo,
			fetchers: []RadarFetcher{&radarRunnerTestFetcher{source: RadarSourceAA, interval: 5 * time.Millisecond}},
			options:  radarRunnerOptions{fetchBudget: 5 * time.Millisecond},
		},
		{
			name:     "invalid source",
			cfg:      cfg,
			repo:     repo,
			fetchers: []RadarFetcher{&radarRunnerTestFetcher{source: RadarSourceKey("unsafe/source"), interval: time.Minute}},
			options:  radarRunnerOptions{fetchBudget: 5 * time.Millisecond},
		},
		{
			name:     "removed AA performance source",
			cfg:      cfg,
			repo:     repo,
			fetchers: []RadarFetcher{&radarRunnerTestFetcher{source: RadarSourceKey("aa_perf:model-a"), interval: time.Minute}},
			options:  radarRunnerOptions{fetchBudget: 5 * time.Millisecond},
		},
		{name: "negative manual timeout", cfg: cfg, repo: repo, fetchers: []RadarFetcher{valid}, options: radarRunnerOptions{manualRefreshTimeout: -time.Second}},
		{name: "overflowing manual lock TTL", cfg: cfg, repo: repo, fetchers: []RadarFetcher{valid}, options: radarRunnerOptions{manualRefreshTimeout: time.Duration(1<<63 - 1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newRadarRunner(tt.cfg, tt.repo, tt.fetchers, &radarRunnerQuotaAggregatorFake{}, tt.options)
			require.Error(t, err)
		})
	}
}

func TestRadarRunnerManualRefreshBudgetCoversEveryLaneQuotaAndCleanup(t *testing.T) {
	fetchBudget := 10 * time.Second
	persistence := 2 * time.Second
	cleanup := 3 * time.Second
	quota := 20 * time.Second
	got, ok := radarRunnerManualRefreshBudget(11, fetchBudget, persistence, cleanup, quota)
	require.True(t, ok)
	// Concurrent source lane = 2*fetch + persistence + cleanup = 25s. Then a
	// full quota timeout + persistence + cleanup (25s) and the 1s safety margin.
	require.Equal(t, 51*time.Second, got)
	ttl, ok := radarRunnerManualRefreshLockTTL(got, cleanup)
	require.True(t, ok)
	require.Equal(t, 58*time.Second, ttl)

	_, ok = radarRunnerManualRefreshLockTTL(time.Duration(1<<63-1)-time.Second, time.Second)
	require.False(t, ok, "overflow must fail closed")
}

func TestRadarRunnerUsesRedisCadenceSequenceAcrossReplicasAndClockSkew(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	source := RadarSourceLMArena
	base := time.Date(2026, 7, 15, 10, 0, 0, 100, time.UTC)
	newRunner := func(now time.Time, owner string) *RadarRunner {
		return newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{
			clock: radarRunnerFixedTestClock{now: now},
			owner: owner,
		}, &radarRunnerTestFetcher{source: source, interval: time.Hour})
	}
	first := newRunner(base.Add(800*time.Nanosecond), "replica-a")
	second := newRunner(base.Add(100*time.Nanosecond), "replica-b")
	ctx := context.Background()
	firstCadence := first.advanceSourceNextFire(ctx, source, base.Add(time.Hour))
	secondCadence := second.advanceSourceNextFire(ctx, source, base.Add(2*time.Hour))
	require.Equal(t, base.UnixMicro(), base.Add(800*time.Nanosecond).UnixMicro(), "fixture clocks must share the same Unix microsecond")
	require.Equal(t, "1", firstCadence.Version)
	require.Equal(t, "2", secondCadence.Version, "Redis command order, not local clock order, defines cadence order")

	repo.mu.Lock()
	repo.cadences[source] = RadarSourceCadence{NextFireAt: base.Add(3 * time.Hour), Version: "100"}
	repo.mu.Unlock()
	restartedBehind := newRunner(base.Add(-24*time.Hour), "replica-restarted-behind")
	restartedCadence := restartedBehind.advanceSourceNextFire(ctx, source, base.Add(30*time.Minute))
	require.Equal(t, "101", restartedCadence.Version)
	require.Equal(t, base.Add(30*time.Minute), restartedCadence.NextFireAt, "new sequence may intentionally reschedule earlier despite clock skew")
}

func TestNewRadarRunnerProductionBudgetMustFitStrictlyInsideInterval(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ExternalRequestTimeoutSeconds = 10
	repo := newRadarRunnerTestRepository()

	// Three 10s attempts + 1s/2s backoff + 1s validation/scheduling
	// margin + 2s persistence + 2s cleanup requires a critical budget of 38s.
	for _, interval := range []time.Duration{33 * time.Second, 34 * time.Second, 38 * time.Second} {
		unsafe := &radarRunnerTestFetcher{source: RadarSourceAA, interval: interval}
		_, err := newRadarRunner(cfg, repo, []RadarFetcher{unsafe}, &radarRunnerQuotaAggregatorFake{}, radarRunnerOptions{})
		require.Error(t, err)
	}

	safe := &radarRunnerTestFetcher{source: RadarSourceAA, interval: 39 * time.Second}
	runner, err := newRadarRunner(cfg, repo, []RadarFetcher{safe}, &radarRunnerQuotaAggregatorFake{}, radarRunnerOptions{})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(ctx))
}

func TestRadarRunnerSameSourceNeverOverlapsWhenExecutionExceedsInterval(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	started := make(chan struct{}, 2)
	unblock := make(chan struct{})
	var current atomic.Int32
	var maximum atomic.Int32
	fetcher := &radarRunnerTestFetcher{source: RadarSourceAA, interval: 20 * time.Millisecond, fn: func(context.Context) ([]byte, SourceFetchMeta, error) {
		active := current.Add(1)
		if active > maximum.Load() {
			maximum.Store(active)
		}
		started <- struct{}{}
		<-unblock
		current.Add(-1)
		now := time.Now().UTC()
		return []byte(`{}`), SourceFetchMeta{LastAttemptAt: now, LastSuccessAt: &now}, nil
	}}
	runner := newRadarRunnerForTest(t, cfg, repo, radarRunnerOptions{}, fetcher)
	runner.Start()
	waitRadarRunnerSignal(t, started, "long source execution")
	select {
	case <-started:
		t.Fatal("same source overlapped while its prior run was active")
	case <-time.After(30 * time.Millisecond):
	}
	require.Equal(t, int32(1), maximum.Load())
	close(unblock)
}

func TestNewRadarRunnerRejectsTypedNilQuotaAggregator(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	fetcher := &radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour}
	var aggregator *RadarQuotaAggregator

	runner, err := NewRadarRunner(cfg, repo, []RadarFetcher{fetcher}, aggregator, staticRadarRuntimeSettingReader(true))
	require.Error(t, err)
	require.Nil(t, runner)

	validRunner, err := newRadarRunner(
		cfg,
		repo,
		[]RadarFetcher{fetcher},
		&radarRunnerQuotaAggregatorFake{},
		radarRunnerOptions{skipQuotaScheduler: true},
	)
	require.NoError(t, err)
	require.Equal(t, 15*time.Minute, validRunner.quotaInterval)
	require.Positive(t, validRunner.quotaTimeout)
	require.Less(t, validRunner.quotaTimeout, validRunner.quotaInterval)
	require.Greater(t, validRunner.quotaLockTTL, validRunner.quotaTimeout+validRunner.cleanupTimeout)
	require.Less(t, validRunner.quotaLockTTL, validRunner.quotaInterval)
	require.NoError(t, validRunner.Stop(context.Background()))
}

func TestRadarRunnerQuotaRunUsesIndependentLockBudgetAndSafeLowCardinalityLogs(t *testing.T) {
	const privateFailure = "account=42 raw_model=secret-model bucket=private/tier"
	tests := []struct {
		name            string
		configureRepo   func(*radarRunnerTestRepository)
		aggregatorError error
		wantCalls       int32
		wantReleases    int
		wantEvent       string
		wantClass       string
	}{
		{
			name:          "success",
			configureRepo: func(*radarRunnerTestRepository) {},
			wantCalls:     1,
			wantReleases:  1,
			wantEvent:     "radar_quota_aggregation_succeeded",
		},
		{
			name: "lock miss",
			configureRepo: func(repo *radarRunnerTestRepository) {
				repo.tryLockFn = func(context.Context, string, string, time.Duration) (bool, error) { return false, nil }
			},
			wantEvent: "radar_quota_aggregation_lock_skipped",
		},
		{
			name: "lock error",
			configureRepo: func(repo *radarRunnerTestRepository) {
				repo.tryLockFn = func(context.Context, string, string, time.Duration) (bool, error) {
					return false, errors.New(privateFailure)
				}
			},
			wantEvent: "radar_quota_aggregation_failed",
			wantClass: "storage_error",
		},
		{
			name:            "aggregation error",
			configureRepo:   func(*radarRunnerTestRepository) {},
			aggregatorError: errors.New(privateFailure),
			wantCalls:       1,
			wantReleases:    1,
			wantEvent:       "radar_quota_aggregation_failed",
			wantClass:       "aggregation_error",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := validRadarFetcherTestConfig()
			repo := newRadarRunnerTestRepository()
			testCase.configureRepo(repo)
			var logs bytes.Buffer
			aggregator := &radarRunnerQuotaAggregatorFake{fn: func(context.Context) (RadarQuotaAggregationReport, error) {
				return RadarQuotaAggregationReport{
					ScannedAccountCount:        10,
					CandidateAccountCount:      6,
					UsableAccountCount:         3,
					BucketCount:                3,
					SkippedAccountCount:        4,
					PrivacyFilteredBucketCount: 2,
					InferenceRejectCounts: map[InferenceRejectReason]int{
						InferenceRejectReasonInsufficientSamples: 5,
						InferenceRejectReasonHighDispersion:      6,
						InferenceRejectReasonInvalidMean:         7,
					},
				}, testCase.aggregatorError
			}}
			interval := 100 * time.Millisecond
			timeout := 60 * time.Millisecond
			cleanupTimeout := 10 * time.Millisecond
			runner := newRadarRunnerWithQuotaForTest(
				t,
				cfg,
				repo,
				aggregator,
				radarRunnerOptions{
					quotaInterval:  interval,
					quotaTimeout:   timeout,
					cleanupTimeout: cleanupTimeout,
					logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
				},
				&radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour},
			)

			runner.runQuotaAggregatorOnce(context.Background())

			locks, releases, _, _, _ := repo.snapshot()
			require.Len(t, locks, 1)
			require.Equal(t, radarQuotaAggregatorTask, locks[0].task)
			require.Equal(t, "runner-quota-test-owner", locks[0].owner)
			require.Greater(t, locks[0].ttl, timeout+cleanupTimeout)
			require.Less(t, locks[0].ttl, interval)
			require.Equal(t, testCase.wantCalls, aggregator.calls.Load())
			require.Len(t, releases, testCase.wantReleases)
			for _, release := range releases {
				require.Equal(t, radarQuotaAggregatorTask, release.task)
			}

			logged := logs.String()
			require.Contains(t, logged, `"msg":"`+testCase.wantEvent+`"`)
			require.Contains(t, logged, `"duration_ms":`)
			if testCase.wantClass != "" {
				require.Contains(t, logged, `"class":"`+testCase.wantClass+`"`)
			}
			if testCase.wantCalls > 0 {
				require.Contains(t, logged, `"scanned_account_count":10`)
				require.Contains(t, logged, `"candidate_account_count":6`)
				require.Contains(t, logged, `"usable_account_count":3`)
				require.Contains(t, logged, `"bucket_count":3`)
				require.Contains(t, logged, `"skipped_account_count":4`)
				require.Contains(t, logged, `"privacy_filtered_bucket_count":2`)
				require.Contains(t, logged, `"inference_reject_insufficient_samples":5`)
				require.Contains(t, logged, `"inference_reject_high_dispersion":6`)
				require.Contains(t, logged, `"inference_reject_invalid_mean":7`)
			}
			require.NotContains(t, logged, privateFailure)
			require.NotContains(t, logged, "secret-model")
			require.NotContains(t, logged, "private/tier")
		})
	}
}

func TestRadarRunnerQuotaTimeoutAndShutdownCancellationReleaseLockWithFreshContext(t *testing.T) {
	t.Run("run timeout", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		repo := newRadarRunnerTestRepository()
		contextErr := make(chan error, 1)
		aggregator := &radarRunnerQuotaAggregatorFake{fn: func(ctx context.Context) (RadarQuotaAggregationReport, error) {
			<-ctx.Done()
			contextErr <- ctx.Err()
			return RadarQuotaAggregationReport{}, ctx.Err()
		}}
		runner := newRadarRunnerWithQuotaForTest(
			t,
			cfg,
			repo,
			aggregator,
			radarRunnerOptions{quotaInterval: 100 * time.Millisecond, quotaTimeout: 20 * time.Millisecond},
			&radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour},
		)

		runner.runQuotaAggregatorOnce(context.Background())
		require.ErrorIs(t, <-contextErr, context.DeadlineExceeded)
		repo.mu.Lock()
		require.Len(t, repo.releaseContextErr, 1)
		require.NoError(t, repo.releaseContextErr[0], "release must not inherit the expired run context")
		repo.mu.Unlock()
	})

	t.Run("shutdown cancellation", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		repo := newRadarRunnerTestRepository()
		started := make(chan struct{})
		canceled := make(chan struct{})
		aggregator := &radarRunnerQuotaAggregatorFake{fn: func(ctx context.Context) (RadarQuotaAggregationReport, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			return RadarQuotaAggregationReport{}, ctx.Err()
		}}
		runner := newRadarRunnerWithQuotaForTest(
			t,
			cfg,
			repo,
			aggregator,
			radarRunnerOptions{quotaInterval: time.Hour, quotaTimeout: 30 * time.Minute},
			&radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour},
		)
		runner.Start()
		waitRadarRunnerSignal(t, started, "immediate quota aggregation")

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, runner.Stop(ctx))
		waitRadarRunnerSignal(t, canceled, "quota aggregation cancellation")
		_, releases, _, _, _ := repo.snapshot()
		foundQuotaRelease := false
		for _, release := range releases {
			if release.task == radarQuotaAggregatorTask {
				foundQuotaRelease = true
			}
		}
		require.True(t, foundQuotaRelease)
		repo.mu.Lock()
		for index, release := range repo.releaseLockCalls {
			if release.task == radarQuotaAggregatorTask {
				require.NoError(t, repo.releaseContextErr[index])
			}
		}
		repo.mu.Unlock()
	})
}

func TestRadarRunnerQuotaSchedulerSkipsCrossTickRunsWithoutOverlapOrBacklog(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	repo := newRadarRunnerTestRepository()
	interval := 2 * time.Second
	clock := newRadarRunnerControllableClock(time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC))
	firstRelease := make(chan struct{})
	entered := make(chan int32, 4)
	finished := make(chan int32, 4)
	var active atomic.Int32
	var maximum atomic.Int32
	var callSequence atomic.Int32
	aggregator := &radarRunnerQuotaAggregatorFake{fn: func(context.Context) (RadarQuotaAggregationReport, error) {
		call := callSequence.Add(1)
		recordRadarAggregatorActive(&active, &maximum)
		entered <- call
		if call == 1 {
			<-firstRelease
		}
		active.Add(-1)
		finished <- call
		return RadarQuotaAggregationReport{}, nil
	}}
	runner := newRadarRunnerWithQuotaForTest(
		t,
		cfg,
		repo,
		aggregator,
		radarRunnerOptions{clock: clock, quotaInterval: interval, quotaTimeout: time.Second},
		&radarRunnerTestFetcher{source: RadarSourceLMArena, interval: time.Hour},
	)
	runner.Start()
	require.Equal(t, int32(1), <-entered)

	firstTimer := waitRadarRunnerTimer(t, clock, interval)
	firstTimer.fire(clock.now.Add(interval))
	secondTimer := waitRadarRunnerTimer(t, clock, interval)
	secondTimer.fire(clock.now.Add(2 * interval))
	thirdTimer := waitRadarRunnerTimer(t, clock, interval)
	require.Equal(t, int32(1), aggregator.calls.Load(), "ticks crossing an active run must be skipped")
	require.True(t, firstTimer.stopped.Load())
	require.True(t, secondTimer.stopped.Load())

	close(firstRelease)
	require.Equal(t, int32(1), <-finished)
	select {
	case unexpected := <-entered:
		t.Fatalf("skipped ticks accumulated an immediate quota run %d", unexpected)
	case <-time.After(20 * time.Millisecond):
	}

	thirdTimer.fire(clock.now.Add(3 * interval))
	require.Equal(t, int32(2), <-entered)
	require.Equal(t, int32(2), <-finished)
	fourthTimer := waitRadarRunnerTimer(t, clock, interval)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(ctx))
	require.True(t, thirdTimer.stopped.Load())
	require.True(t, fourthTimer.stopped.Load(), "shutdown must stop the live quota timer")
	require.Equal(t, int32(1), maximum.Load())
}

func recordRadarAggregatorActive(active, maximum *atomic.Int32) {
	current := active.Add(1)
	for {
		observed := maximum.Load()
		if current <= observed || maximum.CompareAndSwap(observed, current) {
			break
		}
	}
}

func TestRadarRunnerQuotaSchedulerHandlesNilTimerAndDisabledRunner(t *testing.T) {
	t.Run("nil timer stops after immediate run", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		repo := newRadarRunnerTestRepository()
		interval := 35 * time.Millisecond
		clock := newRadarRunnerControllableClock(time.Now())
		clock.nilDuration = interval
		called := make(chan struct{}, 2)
		aggregator := &radarRunnerQuotaAggregatorFake{fn: func(context.Context) (RadarQuotaAggregationReport, error) {
			called <- struct{}{}
			return RadarQuotaAggregationReport{}, nil
		}}
		runner := newRadarRunnerWithQuotaForTest(
			t,
			cfg,
			repo,
			aggregator,
			radarRunnerOptions{clock: clock, quotaInterval: interval, quotaTimeout: 20 * time.Millisecond},
			&radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour},
		)
		runner.Start()
		waitRadarRunnerSignal(t, called, "immediate quota run with nil timer")
		select {
		case <-called:
			t.Fatal("nil timer scheduled another quota run")
		case <-time.After(20 * time.Millisecond):
		}
	})

	t.Run("disabled runner never invokes quota aggregator", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		cfg.Radar.Enabled = false
		repo := newRadarRunnerTestRepository()
		aggregator := &radarRunnerQuotaAggregatorFake{}
		runner := newRadarRunnerWithQuotaForTest(
			t,
			cfg,
			repo,
			aggregator,
			radarRunnerOptions{},
			&radarRunnerTestFetcher{source: RadarSourceAA, interval: time.Hour},
		)
		runner.Start()
		time.Sleep(20 * time.Millisecond)
		require.Zero(t, aggregator.calls.Load())
	})
}
