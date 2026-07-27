package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
)

const (
	radarRunnerFetchValidationMargin      = time.Second
	radarRunnerDefaultPersistenceTimeout  = 2 * time.Second
	radarRunnerDefaultCleanupTimeout      = 2 * time.Second
	radarRunnerDefaultShutdownTimeout     = 10 * time.Second
	radarRunnerDefaultMetricsSyncInterval = 30 * time.Second
	radarRunnerDefaultRuntimeGateTimeout  = 2 * time.Second
	radarQuotaAggregatorTask              = "quota_aggregator"
	radarManualRefreshTask                = "manual_refresh"
	radarManualRefreshSafetyMargin        = time.Second
)

// radarRunnerTimer and radarRunnerClock keep scheduling deterministic in unit
// tests without exposing test-only controls through the production provider.
type radarRunnerTimer interface {
	C() <-chan time.Time
	Stop() bool
}

type radarRunnerClock interface {
	Now() time.Time
	NewTimer(time.Duration) radarRunnerTimer
}

type realRadarRunnerClock struct{}

func (realRadarRunnerClock) Now() time.Time { return time.Now() }
func (realRadarRunnerClock) NewTimer(duration time.Duration) radarRunnerTimer {
	return &realRadarRunnerTimer{timer: time.NewTimer(duration)}
}

type realRadarRunnerTimer struct {
	timer *time.Timer
}

func (t *realRadarRunnerTimer) C() <-chan time.Time { return t.timer.C }
func (t *realRadarRunnerTimer) Stop() bool          { return t.timer.Stop() }

// radarRunnerOptions is an internal test seam. Production always uses the
// zero value through NewRadarRunner, which derives its execution budget from
// the configured HTTP timeout and generates a unique lock owner.
type radarRunnerOptions struct {
	clock               radarRunnerClock
	owner               string
	fetchBudget         time.Duration
	quotaInterval       time.Duration
	quotaTimeout        time.Duration
	skipQuotaScheduler  bool
	persistenceTimeout  time.Duration
	cleanupTimeout      time.Duration
	shutdownTimeout     time.Duration
	logger              *slog.Logger
	metrics             *observability.RadarMetrics
	metricsSyncInterval time.Duration
	runtimeGate         RadarRuntimeSettingReader
	// Test seam only. Production leaves this zero and derives a complete batch
	// budget from the configured task graph below.
	manualRefreshTimeout time.Duration
}

// RadarRuntimeSettingReader is the narrow SettingService contract consumed by
// the scheduler. Keeping the runner on this interface makes every fire
// independently testable while production injects the application's single
// SettingService instance.
type RadarRuntimeSettingReader interface {
	IsRadarEnabled(context.Context) bool
}

var _ RadarRuntimeSettingReader = (*SettingService)(nil)

type staticRadarRuntimeSettingReader bool

func (enabled staticRadarRuntimeSettingReader) IsRadarEnabled(context.Context) bool {
	return bool(enabled)
}

type radarQuotaAggregationRunner interface {
	RunOnceWithReport(context.Context) (RadarQuotaAggregationReport, error)
}

type radarMetricsSnapshotReader interface {
	GetRadarMetricsSnapshot(context.Context) (RadarMetricsSnapshot, error)
}

type radarAggregatorMetricsStateWriter interface {
	CommitRadarAggregatorRun(context.Context, RadarAggregatorRunState) (bool, error)
}

// RadarRunner independently schedules each external Radar source. Each source
// has exactly one serial loop and its own distributed source lock.
type RadarRunner struct {
	repo            RadarCacheRepository
	cadenceRepo     RadarSourceCadenceRepository
	fetchers        []RadarFetcher
	quotaAggregator radarQuotaAggregationRunner
	runtimeGate     RadarRuntimeSettingReader

	owner                string
	fetchBudget          time.Duration
	persistenceTimeout   time.Duration
	cleanupTimeout       time.Duration
	shutdownTimeout      time.Duration
	hardRetention        time.Duration
	quotaInterval        time.Duration
	quotaTimeout         time.Duration
	quotaLockTTL         time.Duration
	skipQuotaScheduler   bool
	lockTTLs             map[RadarSourceKey]time.Duration
	intervals            map[RadarSourceKey]time.Duration
	sources              []RadarSourceKey
	clock                radarRunnerClock
	logger               *slog.Logger
	metrics              *observability.RadarMetrics
	metricsSyncInterval  time.Duration
	manualRefreshTimeout time.Duration
	manualRefreshLockTTL time.Duration
	scheduleMu           sync.RWMutex
	sourceCadence        map[RadarSourceKey]RadarSourceCadence
	quotaNextFire        time.Time

	ctx    context.Context
	cancel context.CancelFunc

	lifecycleMu sync.Mutex
	started     bool
	stopped     bool
	wg          sync.WaitGroup
	done        chan struct{}
	doneOnce    sync.Once
}

// NewRadarRunner creates the production Radar source scheduler. Construction
// fails when the fetch, persistence, and cleanup critical budget cannot fit
// strictly below a crash-safety lock TTL and the source interval.
func NewRadarRunner(
	cfg *config.Config,
	repo RadarCacheRepository,
	fetchers []RadarFetcher,
	quotaAggregator *RadarQuotaAggregator,
	runtimeGate RadarRuntimeSettingReader,
) (*RadarRunner, error) {
	if isNilRadarRuntimeSettingReader(runtimeGate) {
		return nil, errors.New("radar runner requires runtime setting reader")
	}
	return newRadarRunner(cfg, repo, fetchers, quotaAggregator, radarRunnerOptions{runtimeGate: runtimeGate})
}

func newRadarRunner(
	cfg *config.Config,
	repo RadarCacheRepository,
	fetchers []RadarFetcher,
	quotaAggregator radarQuotaAggregationRunner,
	options radarRunnerOptions,
) (*RadarRunner, error) {
	if cfg == nil {
		return nil, errors.New("radar runner requires config")
	}
	if err := cfg.Radar.Validate(); err != nil {
		return nil, errors.New("radar runner requires valid radar config")
	}
	if isNilRadarCacheRepository(repo) {
		return nil, errors.New("radar runner requires cache repository")
	}
	if isNilRadarQuotaAggregationRunner(quotaAggregator) {
		return nil, errors.New("radar runner requires quota aggregator")
	}
	if len(fetchers) == 0 {
		return nil, errors.New("radar runner requires at least one fetcher")
	}
	if options.runtimeGate == nil {
		// The internal constructor is a test seam. Production must pass the
		// explicit SettingService-backed reader through NewRadarRunner.
		options.runtimeGate = staticRadarRuntimeSettingReader(cfg.Radar.Enabled)
	}
	if isNilRadarRuntimeSettingReader(options.runtimeGate) {
		return nil, errors.New("radar runner requires runtime setting reader")
	}

	if options.clock == nil {
		options.clock = realRadarRunnerClock{}
	}
	if options.fetchBudget == 0 {
		options.fetchBudget = radarRunnerProductionFetchBudget(cfg.Radar)
	}
	if options.fetchBudget <= 0 {
		return nil, errors.New("radar runner fetch budget must be positive")
	}
	if options.persistenceTimeout == 0 {
		options.persistenceTimeout = radarRunnerDefaultPersistenceTimeout
	}
	if options.persistenceTimeout <= 0 {
		return nil, errors.New("radar runner persistence timeout must be positive")
	}
	if options.cleanupTimeout == 0 {
		options.cleanupTimeout = radarRunnerDefaultCleanupTimeout
	}
	if options.cleanupTimeout <= 0 {
		return nil, errors.New("radar runner cleanup timeout must be positive")
	}
	if options.quotaInterval == 0 {
		options.quotaInterval = time.Duration(cfg.Radar.QuotaAggregatorIntervalMin) * time.Minute
	}
	if options.quotaInterval <= 0 {
		return nil, errors.New("radar runner quota interval must be positive")
	}
	if options.quotaTimeout == 0 {
		options.quotaTimeout = radarRunnerDefaultQuotaTimeout(options.quotaInterval)
	}
	if options.quotaTimeout <= 0 || options.quotaTimeout >= options.quotaInterval {
		return nil, errors.New("radar runner quota timeout must be positive and less than its interval")
	}
	quotaCriticalBudget, ok := radarRunnerDurationSum(options.quotaTimeout, options.cleanupTimeout)
	if !ok {
		return nil, errors.New("radar runner quota critical budget is invalid")
	}
	quotaLockTTL, ok := radarRunnerLockTTL(quotaCriticalBudget, options.quotaInterval)
	if !ok {
		return nil, errors.New("radar runner quota interval cannot safely contain timeout, cleanup, and lock TTL")
	}
	if options.shutdownTimeout == 0 {
		options.shutdownTimeout = radarRunnerDefaultShutdownTimeout
	}
	if options.shutdownTimeout <= 0 {
		return nil, errors.New("radar runner shutdown timeout must be positive")
	}
	if options.logger == nil {
		options.logger = slog.Default()
	}
	if options.metrics == nil {
		options.metrics = observability.DefaultRadarMetrics()
	}
	if options.metricsSyncInterval == 0 {
		options.metricsSyncInterval = radarRunnerDefaultMetricsSyncInterval
	}
	if options.metricsSyncInterval <= 0 {
		return nil, errors.New("radar runner metrics sync interval must be positive")
	}
	if options.manualRefreshTimeout < 0 {
		return nil, errors.New("radar runner manual refresh timeout must be positive")
	}
	criticalBudget, ok := radarRunnerCriticalBudget(
		options.fetchBudget,
		options.persistenceTimeout,
		options.cleanupTimeout,
	)
	if !ok {
		return nil, errors.New("radar runner critical budget is invalid")
	}

	owner := options.owner
	if owner == "" {
		var err error
		owner, err = newRadarRunnerOwner()
		if err != nil {
			return nil, errors.New("radar runner could not create lock owner")
		}
	} else if strings.TrimSpace(owner) == "" {
		return nil, errors.New("radar runner lock owner must not be blank")
	}

	seen := make(map[RadarSourceKey]struct{}, len(fetchers))
	lockTTLs := make(map[RadarSourceKey]time.Duration, len(fetchers))
	intervals := make(map[RadarSourceKey]time.Duration, len(fetchers))
	sources := make([]RadarSourceKey, len(fetchers))
	fetcherCopy := make([]RadarFetcher, len(fetchers))
	for i, fetcher := range fetchers {
		if isNilRadarFetcher(fetcher) {
			return nil, errors.New("radar runner contains nil fetcher")
		}
		source := fetcher.Source()
		if !isCanonicalRadarRunnerSource(source) {
			return nil, errors.New("radar runner contains invalid fetcher source")
		}
		if _, duplicate := seen[source]; duplicate {
			return nil, errors.New("radar runner contains duplicate fetcher source")
		}
		seen[source] = struct{}{}

		interval := fetcher.Interval()
		lockTTL, ok := radarRunnerLockTTL(criticalBudget, interval)
		if !ok {
			return nil, errors.New("radar runner source interval cannot safely contain critical budget and lock TTL")
		}
		lockTTLs[source] = lockTTL
		intervals[source] = interval
		sources[i] = source
		fetcherCopy[i] = fetcher
	}
	cadenceRepo, ok := repo.(RadarSourceCadenceRepository)
	if !ok || isNilRadarSourceCadenceRepository(cadenceRepo) {
		return nil, errors.New("radar runner requires source cadence repository")
	}
	if options.manualRefreshTimeout == 0 {
		var valid bool
		options.manualRefreshTimeout, valid = radarRunnerManualRefreshBudget(
			len(fetchers),
			options.fetchBudget,
			options.persistenceTimeout,
			options.cleanupTimeout,
			options.quotaTimeout,
		)
		if !valid {
			return nil, errors.New("radar runner manual refresh budget is invalid")
		}
	}
	manualRefreshLockTTL, ok := radarRunnerManualRefreshLockTTL(options.manualRefreshTimeout, options.cleanupTimeout)
	if !ok {
		return nil, errors.New("radar runner manual refresh lock TTL is invalid")
	}

	ctx, cancel := context.WithCancel(context.Background())
	options.metrics.SetAggregatorInterval(options.quotaInterval)
	runner := &RadarRunner{
		repo:                 repo,
		cadenceRepo:          cadenceRepo,
		fetchers:             fetcherCopy,
		quotaAggregator:      quotaAggregator,
		runtimeGate:          options.runtimeGate,
		owner:                owner,
		fetchBudget:          options.fetchBudget,
		persistenceTimeout:   options.persistenceTimeout,
		cleanupTimeout:       options.cleanupTimeout,
		shutdownTimeout:      options.shutdownTimeout,
		hardRetention:        time.Duration(cfg.Radar.SourceHardRetentionDays) * 24 * time.Hour,
		quotaInterval:        options.quotaInterval,
		quotaTimeout:         options.quotaTimeout,
		quotaLockTTL:         quotaLockTTL,
		skipQuotaScheduler:   options.skipQuotaScheduler,
		lockTTLs:             lockTTLs,
		intervals:            intervals,
		sources:              sources,
		clock:                options.clock,
		logger:               options.logger,
		metrics:              options.metrics,
		metricsSyncInterval:  options.metricsSyncInterval,
		manualRefreshTimeout: options.manualRefreshTimeout,
		manualRefreshLockTTL: manualRefreshLockTTL,
		sourceCadence:        make(map[RadarSourceKey]RadarSourceCadence, len(sources)),
		ctx:                  ctx,
		cancel:               cancel,
		done:                 make(chan struct{}),
	}
	runner.updateRuntimeMetrics(runner.readRuntimeEnabled(context.Background()))
	return runner, nil
}

func radarRunnerProductionFetchBudget(cfg config.RadarConfig) time.Duration {
	budget := time.Duration(radarFetchMaxAttempts*cfg.ExternalRequestTimeoutSeconds) * time.Second
	for _, backoff := range radarFetchBackoffs {
		budget += backoff
	}
	return budget + radarRunnerFetchValidationMargin
}

func radarRunnerDefaultQuotaTimeout(interval time.Duration) time.Duration {
	return interval - interval/5
}

func radarRunnerDurationSum(left, right time.Duration) (time.Duration, bool) {
	if left <= 0 || right <= 0 || left > time.Duration(1<<63-1)-right {
		return 0, false
	}
	return left + right, true
}

// radarRunnerManualRefreshBudget proves that every concurrently dispatched
// source lane can consume its lock-acquisition and execution budgets before a
// full quota attempt begins.
func radarRunnerManualRefreshBudget(
	sourceCount int,
	fetchBudget time.Duration,
	persistenceTimeout time.Duration,
	cleanupTimeout time.Duration,
	quotaTimeout time.Duration,
) (time.Duration, bool) {
	if sourceCount <= 0 {
		return 0, false
	}
	sourceLane, ok := radarRunnerDurationSum(fetchBudget, fetchBudget)
	if !ok {
		return 0, false
	}
	sourceLane, ok = radarRunnerDurationSum(sourceLane, persistenceTimeout)
	if !ok {
		return 0, false
	}
	sourceLane, ok = radarRunnerDurationSum(sourceLane, cleanupTimeout)
	if !ok {
		return 0, false
	}
	sourcePhase := sourceLane
	quotaPhase, ok := radarRunnerDurationSum(quotaTimeout, persistenceTimeout)
	if !ok {
		return 0, false
	}
	quotaPhase, ok = radarRunnerDurationSum(quotaPhase, cleanupTimeout)
	if !ok {
		return 0, false
	}
	total, ok := radarRunnerDurationSum(sourcePhase, quotaPhase)
	if !ok {
		return 0, false
	}
	return radarRunnerDurationSum(total, radarManualRefreshSafetyMargin)
}

func radarRunnerManualRefreshLockTTL(timeout, cleanupTimeout time.Duration) (time.Duration, bool) {
	ttl, ok := radarRunnerDurationSum(timeout, cleanupTimeout)
	if !ok {
		return 0, false
	}
	ttl, ok = radarRunnerDurationSum(ttl, cleanupTimeout)
	if !ok {
		return 0, false
	}
	return radarRunnerDurationSum(ttl, radarManualRefreshSafetyMargin)
}

func radarRunnerCriticalBudget(fetchBudget, persistenceTimeout, cleanupMargin time.Duration) (time.Duration, bool) {
	if fetchBudget <= 0 || persistenceTimeout <= 0 || cleanupMargin <= 0 {
		return 0, false
	}
	if fetchBudget > time.Duration(1<<63-1)-persistenceTimeout {
		return 0, false
	}
	total := fetchBudget + persistenceTimeout
	if total > time.Duration(1<<63-1)-cleanupMargin {
		return 0, false
	}
	return total + cleanupMargin, true
}

// radarRunnerLockTTL chooses a duration strictly between the total critical
// budget and source interval. A one-nanosecond gap is intentionally rejected
// because no duration can satisfy both strict inequalities within it.
func radarRunnerLockTTL(criticalBudget, interval time.Duration) (time.Duration, bool) {
	if criticalBudget <= 0 || interval <= criticalBudget {
		return 0, false
	}
	gap := interval - criticalBudget
	lockTTL := criticalBudget + gap/2
	if lockTTL <= criticalBudget {
		lockTTL = criticalBudget + time.Nanosecond
	}
	if lockTTL <= criticalBudget || lockTTL >= interval {
		return 0, false
	}
	return lockTTL, true
}

func newRadarRunnerOwner() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "radar-" + hex.EncodeToString(random[:]), nil
}

func newRadarManualRefreshLockToken() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "radar-manual-" + hex.EncodeToString(random[:]), nil
}

func isNilRadarCacheRepository(repo RadarCacheRepository) bool {
	if repo == nil {
		return true
	}
	value := reflect.ValueOf(repo)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func isNilRadarQuotaAggregationRunner(aggregator radarQuotaAggregationRunner) bool {
	if aggregator == nil {
		return true
	}
	value := reflect.ValueOf(aggregator)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func isNilRadarRuntimeSettingReader(reader RadarRuntimeSettingReader) bool {
	if reader == nil {
		return true
	}
	value := reflect.ValueOf(reader)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func isNilRadarSourceCadenceRepository(repo RadarSourceCadenceRepository) bool {
	if repo == nil {
		return true
	}
	value := reflect.ValueOf(repo)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func isNilRadarFetcher(fetcher RadarFetcher) bool {
	if fetcher == nil {
		return true
	}
	value := reflect.ValueOf(fetcher)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func isCanonicalRadarRunnerSource(source RadarSourceKey) bool {
	switch source {
	case RadarSourceAA, RadarSourceLMArena, RadarSourceStatusClaude, RadarSourceStatusOpenAI,
		RadarSourceStatusWindsurf, RadarSourceStatusDeepSeek, RadarSourceStatusKimi,
		RadarSourceStatusMiniMaxGlobal, RadarSourceStatusMiniMaxChina:
		return true
	}
	return false
}

// Start is idempotent. Scheduler loops always remain alive so a runtime switch
// can resume work on the next fire without restarting the process.
func (r *RadarRunner) Start() {
	if r == nil {
		return
	}
	r.lifecycleMu.Lock()
	if r.started || r.stopped {
		r.lifecycleMu.Unlock()
		return
	}
	r.started = true
	now := r.clock.Now().UTC()
	r.scheduleMu.Lock()
	r.quotaNextFire = now.Add(r.quotaInterval).UTC()
	r.scheduleMu.Unlock()
	r.wg.Add(1)
	go r.runMetricsSync(r.ctx)
	for i, fetcher := range r.fetchers {
		r.wg.Add(1)
		source := r.sources[i]
		go r.runFetcher(r.ctx, fetcher, source, r.intervals[source])
	}
	if !r.skipQuotaScheduler {
		r.wg.Add(1)
		go r.runQuotaAggregator(r.ctx)
	}
	go func() {
		r.wg.Wait()
		r.closeDone()
	}()
	r.lifecycleMu.Unlock()
}

func (r *RadarRunner) runMetricsSync(ctx context.Context) {
	defer r.wg.Done()
	r.syncMetricsOnce(ctx)
	ticker := time.NewTicker(r.metricsSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.syncMetricsOnce(ctx)
		}
	}
}

func (r *RadarRunner) syncMetricsOnce(parent context.Context) {
	if !r.runtimeEnabled(parent) {
		return
	}
	ctx, cancel := context.WithTimeout(parent, r.persistenceTimeout)
	defer cancel()
	reader, ok := r.repo.(radarMetricsSnapshotReader)
	if ok {
		snapshot, err := reader.GetRadarMetricsSnapshot(ctx)
		if err != nil {
			r.logger.Warn("radar_runner_metrics_sync_failed", "component", "shared_snapshot", "class", "storage_error")
			return
		}
		for _, source := range r.sources {
			r.metrics.SyncSourceSuccess(string(source), snapshot.SourceLastSuccess[source])
		}
		if snapshot.AggregatorStateValid {
			r.metrics.SyncAggregatorState(snapshot.PublishedBucketCount, snapshot.AggregatorLastRunAt, snapshot.AggregatorLastSuccessAt)
		}
		if snapshot.CacheMemoryValid {
			r.metrics.SetCacheMemoryTotals(snapshot.CacheMemoryBytes)
		}
		if snapshot.Partial {
			r.logger.Warn("radar_runner_metrics_sync_partial", "component", "cache_memory", "class", "storage_error")
		}
		return
	}
	metas, err := r.repo.ListSourceMeta(ctx)
	if err != nil {
		r.logger.Warn("radar_runner_metrics_sync_failed", "component", "source_meta", "class", "storage_error")
		return
	}
	for _, source := range r.sources {
		meta, exists := metas[source]
		lastSuccess := time.Time{}
		if exists && meta.LastSuccessAt != nil && !meta.LastSuccessAt.IsZero() {
			lastSuccess = *meta.LastSuccessAt
		}
		r.metrics.SyncSourceSuccess(string(source), lastSuccess)
	}
}

func (r *RadarRunner) readRuntimeEnabled(parent context.Context) bool {
	if r == nil || isNilRadarRuntimeSettingReader(r.runtimeGate) {
		return false
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, radarRunnerDefaultRuntimeGateTimeout)
	defer cancel()
	return r.runtimeGate.IsRadarEnabled(ctx)
}

func (r *RadarRunner) updateRuntimeMetrics(enabled bool) {
	if r == nil || r.metrics == nil {
		return
	}
	r.metrics.SetEnabled(enabled)
	if enabled {
		for _, source := range r.sources {
			r.metrics.RegisterSource(string(source))
		}
	}
}

func (r *RadarRunner) runtimeEnabled(parent context.Context) bool {
	enabled := r.readRuntimeEnabled(parent)
	r.updateRuntimeMetrics(enabled)
	return enabled
}

// Stop permanently stops the runner. It is safe before Start, after a prior
// Stop, and from multiple goroutines. The caller's context is honored and an
// internal upper bound prevents an unbounded wait even with Background.
func (r *RadarRunner) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.lifecycleMu.Lock()
	if !r.stopped {
		r.stopped = true
		r.cancel()
		if !r.started {
			r.closeDone()
		}
	}
	done := r.done
	shutdownTimeout := r.shutdownTimeout
	r.lifecycleMu.Unlock()

	select {
	case <-done:
		return nil
	default:
	}

	waitCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	select {
	case <-done:
		return nil
	case <-waitCtx.Done():
		return waitCtx.Err()
	}
}

func (r *RadarRunner) closeDone() {
	r.doneOnce.Do(func() { close(r.done) })
}

func (r *RadarRunner) configuredSources() []RadarSourceKey {
	if r == nil {
		return []RadarSourceKey{}
	}
	return append([]RadarSourceKey(nil), r.sources...)
}

func (r *RadarRunner) setSourceCadence(source RadarSourceKey, cadence RadarSourceCadence) {
	r.scheduleMu.Lock()
	cadence.NextFireAt = cadence.NextFireAt.UTC()
	r.sourceCadence[source] = cadence
	r.scheduleMu.Unlock()
}

func (r *RadarRunner) advanceSourceNextFire(parent context.Context, source RadarSourceKey, nextFireAt time.Time) RadarSourceCadence {
	unassigned := RadarSourceCadence{NextFireAt: nextFireAt.UTC()}
	r.setSourceCadence(source, unassigned)
	ctx, cancel := context.WithTimeout(parent, r.persistenceTimeout)
	defer cancel()
	cadence, err := r.cadenceRepo.AdvanceSourceNextFire(ctx, source, nextFireAt.UTC())
	if err != nil {
		r.logOperationFailure(source, "advance_cadence", "storage_error")
		return unassigned
	}
	r.setSourceCadence(source, cadence)
	return cadence
}

func (r *RadarRunner) sourceScheduledCadence(source RadarSourceKey) RadarSourceCadence {
	r.scheduleMu.RLock()
	defer r.scheduleMu.RUnlock()
	return r.sourceCadence[source]
}

func (r *RadarRunner) sourceAuthoritativeCadence(parent context.Context, source RadarSourceKey) RadarSourceCadence {
	if cadence := r.sourceScheduledCadence(source); cadence.Version != "" && !cadence.NextFireAt.IsZero() {
		return cadence
	}
	ctx, cancel := context.WithTimeout(parent, r.persistenceTimeout)
	defer cancel()
	cadence, err := r.cadenceRepo.GetSourceCadence(ctx, source)
	if err != nil {
		return RadarSourceCadence{}
	}
	r.setSourceCadence(source, cadence)
	return cadence
}

func (r *RadarRunner) setQuotaNextFire(at time.Time) {
	r.scheduleMu.Lock()
	r.quotaNextFire = at.UTC()
	r.scheduleMu.Unlock()
}

func (r *RadarRunner) quotaScheduledNextFire() time.Time {
	r.scheduleMu.RLock()
	defer r.scheduleMu.RUnlock()
	return r.quotaNextFire.UTC()
}

func (r *RadarRunner) runFetcher(
	ctx context.Context,
	fetcher RadarFetcher,
	source RadarSourceKey,
	interval time.Duration,
) {
	defer r.wg.Done()

	for {
		if ctx.Err() != nil {
			return
		}

		if ctx.Err() != nil {
			return
		}

		nextFireAt := r.clock.Now().UTC().Add(interval)
		timer := r.clock.NewTimer(interval)
		cadence := r.advanceSourceNextFire(ctx, source, nextFireAt)
		r.fetchOnceWithCadence(ctx, fetcher, source, cadence)

		if timer == nil {
			return
		}
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
		}
		timer.Stop()
	}
}

func (r *RadarRunner) runQuotaAggregator(ctx context.Context) {
	defer r.wg.Done()

	completed := make(chan struct{}, 1)
	running := false
	start := func() {
		running = true
		go func() {
			r.runQuotaAggregatorOnce(ctx)
			completed <- struct{}{}
		}()
	}

	// Fire immediately; the independent cadence timer starts at the same time so
	// ticks that cross a long run can be consumed and skipped instead of queued.
	start()
	r.setQuotaNextFire(r.clock.Now().UTC().Add(r.quotaInterval))
	timer := r.clock.NewTimer(r.quotaInterval)
	if timer == nil {
		<-completed
		return
	}

	for {
		select {
		case <-ctx.Done():
			timer.Stop()
			if running {
				<-completed
			}
			return
		case <-completed:
			running = false
		case <-timer.C():
			timer.Stop()
			r.setQuotaNextFire(r.clock.Now().UTC().Add(r.quotaInterval))
			timer = r.clock.NewTimer(r.quotaInterval)
			if running {
				r.logger.Info("radar_quota_aggregation_overlap_skipped")
			} else {
				start()
			}
			if timer == nil {
				if running {
					<-completed
				}
				return
			}
		}
	}
}

func (r *RadarRunner) runQuotaAggregatorOnce(ctx context.Context) {
	r.runQuotaAggregatorOnceWithRuntimeGate(ctx, false)
}

// runQuotaAggregatorOnceWithRuntimeGate is the common once path. T4-03 can
// explicitly bypass only this runtime gate for an authorized manual refresh;
// distributed locking and all other safety controls still apply.
func (r *RadarRunner) runQuotaAggregatorOnceWithRuntimeGate(ctx context.Context, bypassRuntimeGate bool) {
	if !bypassRuntimeGate && !r.runtimeEnabled(ctx) {
		return
	}
	startedAt := time.Now()
	attemptCtx, cancel := context.WithTimeout(ctx, r.quotaTimeout)
	defer cancel()

	acquired, err := r.repo.TryLock(attemptCtx, radarQuotaAggregatorTask, r.owner, r.quotaLockTTL)
	if err != nil {
		r.metrics.RecordAggregator("failure", radarQuotaLockFailureClass(err), time.Since(startedAt), 0)
		r.logQuotaAggregationFailure("try_lock", radarQuotaLockFailureClass(err), RadarQuotaAggregationReport{}, startedAt)
		return
	}
	if !acquired {
		r.logger.Info("radar_quota_aggregation_lock_skipped",
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
		return
	}
	defer r.releaseQuotaAggregatorLock(startedAt)

	report, err := r.quotaAggregator.RunOnceWithReport(attemptCtx)
	if err != nil {
		r.recordQuotaAggregationMetrics("failure", radarQuotaRunFailureClass(err), report, startedAt)
		r.commitQuotaAggregatorMetricsState(startedAt, false, report.BucketCount)
		r.logQuotaAggregationFailure("run", radarQuotaRunFailureClass(err), report, startedAt)
		return
	}
	r.recordQuotaAggregationMetrics("success", "", report, startedAt)
	r.commitQuotaAggregatorMetricsState(startedAt, true, report.BucketCount)
	r.logQuotaAggregationSuccess(report, startedAt)
}

func (r *RadarRunner) commitQuotaAggregatorMetricsState(startedAt time.Time, success bool, bucketCount int) {
	writer, ok := r.repo.(radarAggregatorMetricsStateWriter)
	if !ok {
		r.logger.Warn("radar_quota_metrics_state_failed", "class", "unsupported_repository")
		return
	}
	if bucketCount < 0 {
		bucketCount = 0
	}
	completedAt := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), r.persistenceTimeout)
	defer cancel()
	applied, err := writer.CommitRadarAggregatorRun(ctx, RadarAggregatorRunState{
		RunVersion:           startedAt.UTC().UnixMilli(),
		CompletedAt:          completedAt,
		NextFireAt:           r.quotaScheduledNextFire(),
		Success:              success,
		PublishedBucketCount: bucketCount,
	})
	if err != nil {
		r.logger.Warn("radar_quota_metrics_state_failed", "class", "storage_error")
		return
	}
	if applied {
		r.metrics.RecordAggregatorCompletion(completedAt, success, bucketCount)
	}
}

// TriggerManualRefresh atomically coalesces refreshes across replicas, then
// returns a stable task list while executing configured fetchers with bounded
// concurrency, followed by quota aggregation. Work is attached to the runner
// lifecycle, not an HTTP request.
func (r *RadarRunner) TriggerManualRefresh() (bool, []string, error) {
	if r == nil {
		return false, nil, ErrRadarAdminUnavailable
	}
	tasks := make([]string, 0, len(r.sources)+1)
	for _, source := range r.sources {
		tasks = append(tasks, string(source))
	}
	tasks = append(tasks, radarQuotaAggregatorTask)

	r.lifecycleMu.Lock()
	if !r.started || r.stopped {
		r.lifecycleMu.Unlock()
		return false, tasks, ErrRadarAdminUnavailable
	}
	r.lifecycleMu.Unlock()
	lockToken, err := newRadarManualRefreshLockToken()
	if err != nil {
		return false, tasks, ErrRadarAdminUnavailable
	}

	lockCtx, cancelLock := context.WithTimeout(r.ctx, r.persistenceTimeout)
	acquired, err := r.repo.TryLock(lockCtx, radarManualRefreshTask, lockToken, r.manualRefreshLockTTL)
	cancelLock()
	if err != nil {
		return false, tasks, ErrRadarAdminUnavailable
	}
	if !acquired {
		return false, tasks, nil
	}

	r.lifecycleMu.Lock()
	if !r.started || r.stopped {
		r.lifecycleMu.Unlock()
		r.releaseManualRefreshLock(lockToken)
		return false, tasks, ErrRadarAdminUnavailable
	}
	refreshCtx, cancel := context.WithTimeout(r.ctx, r.manualRefreshTimeout)
	r.wg.Add(1)
	r.lifecycleMu.Unlock()

	go func() {
		defer r.wg.Done()
		defer cancel()
		defer r.releaseManualRefreshLock(lockToken)
		var sourceWG sync.WaitGroup
		for i, fetcher := range r.fetchers {
			source := r.sources[i]
			sourceWG.Add(1)
			go func(fetcher RadarFetcher, source RadarSourceKey) {
				defer sourceWG.Done()
				r.runManualSource(refreshCtx, fetcher, source)
			}(fetcher, source)
		}
		sourceWG.Wait()
		// The derived lifecycle budget reserves a complete quota phase after all
		// configured source lanes converge. Call unconditionally so cancellation
		// and storage failures are still recorded as an attempted aggregation.
		r.runQuotaAggregatorOnceWithRuntimeGate(refreshCtx, true)
	}()
	return true, tasks, nil
}

func (r *RadarRunner) runManualSource(ctx context.Context, fetcher RadarFetcher, source RadarSourceKey) {
	cadence := r.sourceAuthoritativeCadence(ctx, source)
	if cadence.NextFireAt.IsZero() || cadence.Version == "" {
		return
	}
	r.fetchOnceWithCadenceAndRuntimeGate(ctx, fetcher, source, cadence, true)
}

func (r *RadarRunner) releaseManualRefreshLock(lockToken string) {
	ctx, cancel := context.WithTimeout(context.Background(), r.cleanupTimeout)
	defer cancel()
	if err := r.repo.ReleaseLock(ctx, radarManualRefreshTask, lockToken); err != nil {
		r.logger.Warn("radar_manual_refresh_lock_release_failed", "class", "storage_error")
	}
}

func (r *RadarRunner) recordQuotaAggregationMetrics(result, reason string, report RadarQuotaAggregationReport, startedAt time.Time) {
	r.metrics.RecordAggregator(result, reason, time.Since(startedAt), report.BucketCount)
	for skipReason, count := range report.SkippedAccountCounts {
		r.metrics.AddAggregatorSkipped(skipReason, count)
	}
	for metric, count := range report.InferenceCounts {
		r.metrics.AddInference(metric.Bucket, metric.Result, string(metric.Reason), count)
	}
}

func radarQuotaLockFailureClass(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return "storage_error"
}

func radarQuotaRunFailureClass(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return "aggregation_error"
}

func (r *RadarRunner) releaseQuotaAggregatorLock(startedAt time.Time) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), r.cleanupTimeout)
	defer cancel()
	if err := r.repo.ReleaseLock(cleanupCtx, radarQuotaAggregatorTask, r.owner); err != nil {
		r.logQuotaAggregationFailure("release_lock", "storage_error", RadarQuotaAggregationReport{}, startedAt)
	}
}

func (r *RadarRunner) logQuotaAggregationSuccess(report RadarQuotaAggregationReport, startedAt time.Time) {
	r.logger.Info("radar_quota_aggregation_succeeded", radarQuotaAggregationLogArgs(report, startedAt)...)
}

func (r *RadarRunner) logQuotaAggregationFailure(
	operation string,
	safeClass string,
	report RadarQuotaAggregationReport,
	startedAt time.Time,
) {
	args := []any{"operation", operation, "class", safeClass}
	args = append(args, radarQuotaAggregationLogArgs(report, startedAt)...)
	r.logger.Warn("radar_quota_aggregation_failed", args...)
}

func radarQuotaAggregationLogArgs(report RadarQuotaAggregationReport, startedAt time.Time) []any {
	return []any{
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"scanned_account_count", report.ScannedAccountCount,
		"candidate_account_count", report.CandidateAccountCount,
		"usable_account_count", report.UsableAccountCount,
		"bucket_count", report.BucketCount,
		"skipped_account_count", report.SkippedAccountCount,
		"privacy_filtered_bucket_count", report.PrivacyFilteredBucketCount,
		"inference_reject_insufficient_samples", report.InferenceRejectCounts[InferenceRejectReasonInsufficientSamples],
		"inference_reject_high_dispersion", report.InferenceRejectCounts[InferenceRejectReasonHighDispersion],
		"inference_reject_invalid_mean", report.InferenceRejectCounts[InferenceRejectReasonInvalidMean],
		"inference_reject_unknown_plan", report.InferenceRejectCounts[InferenceRejectReasonUnknownPlan],
	}
}

func (r *RadarRunner) fetchOnce(
	ctx context.Context,
	fetcher RadarFetcher,
	source RadarSourceKey,
	nextFireAt time.Time,
) {
	r.fetchOnceWithCadence(ctx, fetcher, source, radarRunnerUnassignedTestCadence(nextFireAt))
}

func (r *RadarRunner) fetchOnceWithCadence(ctx context.Context, fetcher RadarFetcher, source RadarSourceKey, cadence RadarSourceCadence) {
	r.fetchOnceWithCadenceAndRuntimeGate(ctx, fetcher, source, cadence, false)
}

// fetchOnceWithRuntimeGate is shared by scheduled and future authorized manual
// refreshes. Bypass affects only the runtime switch; the distributed lock,
// budgets, validation, persistence and cleanup paths remain identical.
func (r *RadarRunner) fetchOnceWithRuntimeGate(
	ctx context.Context,
	fetcher RadarFetcher,
	source RadarSourceKey,
	nextFireAt time.Time,
	bypassRuntimeGate bool,
) {
	r.fetchOnceWithCadenceAndRuntimeGate(ctx, fetcher, source, radarRunnerUnassignedTestCadence(nextFireAt), bypassRuntimeGate)
}

func (r *RadarRunner) fetchOnceWithCadenceAndRuntimeGate(
	ctx context.Context,
	fetcher RadarFetcher,
	source RadarSourceKey,
	cadence RadarSourceCadence,
	bypassRuntimeGate bool,
) {
	if !bypassRuntimeGate && !r.runtimeEnabled(ctx) {
		return
	}
	task := string(source)

	lockCtx, cancelLock := context.WithTimeout(ctx, r.fetchBudget)
	acquired, err := r.repo.TryLock(lockCtx, task, r.owner, r.lockTTLs[source])
	cancelLock()
	if err != nil {
		r.logOperationFailure(source, "try_lock", "storage_error")
		return
	}
	if !acquired {
		return
	}
	defer r.releaseLock(source)

	jobCtx, cancelJob := context.WithTimeout(ctx, r.fetchBudget)
	defer cancelJob()
	attemptStartedAt := r.clock.Now().UTC()
	metricsStartedAt := time.Now()
	payload, meta, fetchErr := fetcher.Fetch(jobCtx)
	fetchDuration := time.Since(metricsStartedAt)
	if fetchErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			r.metrics.RecordFetchFailure(string(source), "canceled", radarRunnerHTTPStatus(meta), fetchDuration)
			r.logOperationFailure(source, "fetch", "canceled")
			return
		}
		failureCode := radarRunnerFailureCode(meta)
		r.metrics.RecordFetchFailure(string(source), string(failureCode), radarRunnerHTTPStatus(meta), fetchDuration)
		r.logOperationFailure(source, "fetch", string(failureCode))
		r.persistFailureMeta(source, meta, attemptStartedAt, cadence)
		return
	}

	successMeta := radarRunnerSuccessMeta(meta, attemptStartedAt, cadence)
	persistenceCtx, cancelPersistence := context.WithTimeout(context.Background(), r.persistenceTimeout)
	defer cancelPersistence()
	committed, commitErr := r.repo.CommitSourceSuccess(persistenceCtx, source, payload, r.hardRetention, successMeta)
	if commitErr != nil {
		r.logOperationFailure(source, "commit_success", "storage_error")
		r.metrics.RecordFetchFailure(string(source), "storage_error", radarRunnerHTTPStatus(successMeta), fetchDuration)
		return
	}
	if !committed {
		r.logOperationFailure(source, "commit_success", "superseded")
		r.metrics.RecordFetchFailure(string(source), "superseded", radarRunnerHTTPStatus(successMeta), fetchDuration)
		return
	}
	r.metrics.RecordFetchSuccess(string(source), radarRunnerHTTPStatus(successMeta), fetchDuration, *successMeta.LastSuccessAt)
	r.logger.Info("radar_runner_fetch_succeeded",
		"source", radarRunnerSafeSource(source),
		"duration_ms", fetchDuration.Milliseconds(),
		"http_status", radarRunnerHTTPStatus(successMeta),
		"cache_committed", true,
	)
}

func radarRunnerHTTPStatus(meta SourceFetchMeta) int {
	if meta.HTTPStatus == nil {
		return 0
	}
	return *meta.HTTPStatus
}

func (r *RadarRunner) persistFailureMeta(
	source RadarSourceKey,
	meta SourceFetchMeta,
	fallbackAttempt time.Time,
	cadence RadarSourceCadence,
) {
	persistenceCtx, cancel := context.WithTimeout(context.Background(), r.persistenceTimeout)
	defer cancel()
	failureMeta := radarRunnerFailureMeta(meta, fallbackAttempt, cadence)
	if _, err := r.repo.CommitSourceFailure(persistenceCtx, source, failureMeta); err != nil {
		r.logOperationFailure(source, "commit_failure", "storage_error")
	}
}

func (r *RadarRunner) releaseLock(source RadarSourceKey) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), r.cleanupTimeout)
	defer cancel()
	if err := r.repo.ReleaseLock(cleanupCtx, string(source), r.owner); err != nil {
		r.logOperationFailure(source, "release_lock", "storage_error")
	}
}

func (r *RadarRunner) logOperationFailure(source RadarSourceKey, operation, safeClass string) {
	r.logger.Warn("radar_runner_operation_failed",
		"source", radarRunnerSafeSource(source),
		"operation", operation,
		"class", safeClass,
	)
}

func radarRunnerSafeSource(source RadarSourceKey) string {
	if isCanonicalRadarRunnerSource(source) {
		return string(source)
	}
	return "other"
}

func radarRunnerSuccessMeta(meta SourceFetchMeta, fallbackAttempt time.Time, cadence RadarSourceCadence) SourceFetchMeta {
	result := radarRunnerBaseMeta(meta, fallbackAttempt, cadence)
	lastSuccessAt := result.LastAttemptAt
	if meta.LastSuccessAt != nil && !meta.LastSuccessAt.IsZero() {
		lastSuccessAt = meta.LastSuccessAt.UTC()
	}
	result.LastSuccessAt = radarRunnerTimePointer(lastSuccessAt)
	result.Error = nil
	return result
}

func radarRunnerFailureMeta(meta SourceFetchMeta, fallbackAttempt time.Time, cadence RadarSourceCadence) SourceFetchMeta {
	result := radarRunnerBaseMeta(meta, fallbackAttempt, cadence)
	result.LastSuccessAt = nil
	errorCode := radarRunnerFailureCode(meta)
	result.Error = radarRunnerErrorPointer(errorCode)
	return result
}

func radarRunnerFailureCode(meta SourceFetchMeta) DataSourceErrorCode {
	if meta.Error != nil && isSafeRadarRunnerErrorCode(*meta.Error) {
		return *meta.Error
	}
	return DataSourceErrorCodeNetworkError
}

func radarRunnerBaseMeta(meta SourceFetchMeta, fallbackAttempt time.Time, cadence RadarSourceCadence) SourceFetchMeta {
	attemptAt := fallbackAttempt.UTC()
	if !meta.LastAttemptAt.IsZero() {
		attemptAt = meta.LastAttemptAt.UTC()
	}
	result := SourceFetchMeta{
		LastAttemptAt:  attemptAt,
		NextFireAt:     radarRunnerTimePointer(cadence.NextFireAt.UTC()),
		CadenceVersion: cadence.Version,
	}
	if meta.HTTPStatus != nil {
		status := *meta.HTTPStatus
		result.HTTPStatus = &status
	}
	return result
}

func radarRunnerUnassignedTestCadence(nextFireAt time.Time) RadarSourceCadence {
	return RadarSourceCadence{NextFireAt: nextFireAt.UTC()}
}

func isSafeRadarRunnerErrorCode(code DataSourceErrorCode) bool {
	switch code {
	case DataSourceErrorCodeNetworkError,
		DataSourceErrorCodeUnauthorized,
		DataSourceErrorCodeRateLimited,
		DataSourceErrorCodeInvalidResponse,
		DataSourceErrorCodeUpstreamError:
		return true
	default:
		return false
	}
}

func radarRunnerTimePointer(value time.Time) *time.Time {
	utc := value.UTC()
	return &utc
}

func radarRunnerErrorPointer(value DataSourceErrorCode) *DataSourceErrorCode {
	cloned := value
	return &cloned
}
