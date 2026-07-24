// Package observability contains bounded-cardinality operational telemetry.
package observability

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultMetricsNamespace = "radar"
)

var (
	defaultRadarMetricsOnce sync.Once
	defaultRadarMetrics     *RadarMetrics
)

// RadarMetrics owns all Model Radar collectors. Every label is normalized by
// this type before it reaches Prometheus; callers must never build labels from
// account identifiers, Redis keys, URLs, credentials, or raw model names.
type RadarMetrics struct {
	fetchSuccess       *prometheus.CounterVec
	fetchFailure       *prometheus.CounterVec
	fetchDuration      *prometheus.HistogramVec
	fetchHTTPResponses *prometheus.CounterVec
	aggregatorDuration prometheus.Histogram
	aggregatorBuckets  *prometheus.GaugeVec
	aggregatorLastRun  prometheus.Gauge
	aggregatorLastOK   prometheus.Gauge
	aggregatorInterval prometheus.Gauge
	enabled            prometheus.Gauge
	aggregatorRuns     *prometheus.CounterVec
	aggregatorSkipped  *prometheus.CounterVec
	inference          *prometheus.CounterVec
	httpRequests       *prometheus.CounterVec
	httpDuration       *prometheus.HistogramVec
	redisOperations    *prometheus.CounterVec
	cacheMemory        *prometheus.GaugeVec

	mu                sync.RWMutex
	sourceLastSuccess map[string]time.Time
	cacheEntries      map[string]map[string]int
	now               func() time.Time
}

type radarSourceAgeCollector struct {
	metrics *RadarMetrics
	desc    *prometheus.Desc
}

func (c *radarSourceAgeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *radarSourceAgeCollector) Collect(ch chan<- prometheus.Metric) {
	c.metrics.mu.RLock()
	sources := make(map[string]time.Time, len(c.metrics.sourceLastSuccess))
	for source, lastSuccess := range c.metrics.sourceLastSuccess {
		sources[source] = lastSuccess
	}
	c.metrics.mu.RUnlock()
	type familyState struct {
		oldest  time.Time
		missing bool
	}
	families := make(map[string]familyState)
	for source, lastSuccess := range sources {
		family := normalizeSource(source)
		state := families[family]
		if lastSuccess.IsZero() {
			state.missing = true
		} else if state.oldest.IsZero() || lastSuccess.Before(state.oldest) {
			state.oldest = lastSuccess
		}
		families[family] = state
	}
	for family, state := range families {
		age := -1.0
		if !state.missing && !state.oldest.IsZero() {
			age = c.metrics.now().Sub(state.oldest).Seconds()
			if age < 0 {
				age = 0
			}
		}
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, age, family)
	}
}

// DefaultRadarMetrics is the process-wide production metrics instance.
func DefaultRadarMetrics() *RadarMetrics {
	defaultRadarMetricsOnce.Do(func() {
		metrics, err := NewRadarMetrics(prometheus.DefaultRegisterer)
		if err != nil {
			panic(err)
		}
		defaultRadarMetrics = metrics
	})
	return defaultRadarMetrics
}

// NewRadarMetrics registers an isolated collector set. It is primarily useful
// for tests and for applications that provide their own Prometheus registry.
func NewRadarMetrics(registerer prometheus.Registerer) (*RadarMetrics, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	m := &RadarMetrics{
		fetchSuccess: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: defaultMetricsNamespace, Name: "fetch_success_total",
			Help: "Successful external Radar source fetches.",
		}, []string{"source"}),
		fetchFailure: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: defaultMetricsNamespace, Name: "fetch_failure_total",
			Help: "Failed external Radar source fetches by safe reason.",
		}, []string{"source", "reason"}),
		fetchDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: defaultMetricsNamespace, Name: "fetch_duration_seconds",
			Help:    "External Radar source fetch duration.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30},
		}, []string{"source"}),
		fetchHTTPResponses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: defaultMetricsNamespace, Name: "fetch_http_responses_total",
			Help: "External Radar source HTTP responses by status class.",
		}, []string{"source", "status"}),
		aggregatorDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: defaultMetricsNamespace, Name: "aggregator_duration_seconds",
			Help:    "Radar quota aggregation duration.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60},
		}),
		aggregatorBuckets: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: defaultMetricsNamespace, Name: "aggregator_bucket_count",
			Help: "Public quota buckets published by the latest successful aggregation.",
		}, []string{}),
		aggregatorLastRun: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: defaultMetricsNamespace, Name: "aggregator_last_run_timestamp_seconds",
			Help: "Unix timestamp of the latest completed quota aggregation run; zero before the first run.",
		}),
		aggregatorLastOK: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: defaultMetricsNamespace, Name: "aggregator_last_success_timestamp_seconds",
			Help: "Unix timestamp of the latest successful quota aggregation run; zero before the first success.",
		}),
		aggregatorInterval: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: defaultMetricsNamespace, Name: "aggregator_interval_seconds",
			Help: "Configured quota aggregation interval in seconds.",
		}),
		enabled: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: defaultMetricsNamespace, Name: "enabled",
			Help: "Whether Model Radar background scheduling is enabled (1) or disabled (0).",
		}),
		aggregatorRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: defaultMetricsNamespace, Name: "aggregator_runs_total",
			Help: "Radar quota aggregation runs by result and safe reason.",
		}, []string{"result", "reason"}),
		aggregatorSkipped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: defaultMetricsNamespace, Name: "aggregator_skipped_accounts_total",
			Help: "Accounts skipped during Radar aggregation by safe reason.",
		}, []string{"reason"}),
		inference: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: defaultMetricsNamespace, Name: "inference_total",
			Help: "Quota inference outcomes by bounded bucket family, result, and reason.",
		}, []string{"bucket", "result", "reason"}),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: defaultMetricsNamespace, Name: "http_requests_total",
			Help: "Public Radar HTTP requests by fixed route and status class.",
		}, []string{"route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: defaultMetricsNamespace, Name: "http_request_duration_seconds",
			Help:    "Public Radar HTTP response duration by fixed route.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 1},
		}, []string{"route"}),
		redisOperations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: defaultMetricsNamespace, Name: "redis_operations_total",
			Help: "Radar Redis operations by bounded operation, access, and result.",
		}, []string{"operation", "access", "result"}),
		cacheMemory: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: defaultMetricsNamespace, Name: "cache_memory_bytes",
			Help: "Current Redis MEMORY USAGE bytes summed by bounded Radar cache family.",
		}, []string{"cache"}),
		sourceLastSuccess: make(map[string]time.Time),
		cacheEntries:      make(map[string]map[string]int),
		now:               time.Now,
	}
	m.initializeZeroSeries()

	collectors := []prometheus.Collector{
		m.fetchSuccess, m.fetchFailure, m.fetchDuration, m.fetchHTTPResponses,
		m.aggregatorDuration, m.aggregatorBuckets, m.aggregatorLastRun,
		m.aggregatorLastOK, m.aggregatorInterval, m.enabled, m.aggregatorRuns,
		m.aggregatorSkipped, m.inference, m.httpRequests, m.httpDuration,
		m.redisOperations, m.cacheMemory,
		&radarSourceAgeCollector{
			metrics: m,
			desc: prometheus.NewDesc(
				prometheus.BuildFQName(defaultMetricsNamespace, "", "source_age_seconds"),
				"Age in seconds of the latest successful source data; -1 before first success.",
				[]string{"source"}, nil,
			),
		},
	}
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// initializeZeroSeries makes every required collector visible before its first
// event. The labels are fixed, canonical values from the bounded production
// domains; creating a child does not increment a counter or observe a
// histogram, so event and duration totals remain exact zeroes.
func (m *RadarMetrics) initializeZeroSeries() {
	for _, source := range []string{
		"status_claude", "status_openai", "status_windsurf", "status_deepseek",
		"status_kimi", "status_minimax_global", "status_minimax_china",
	} {
		m.fetchSuccess.WithLabelValues(source)
		m.fetchFailure.WithLabelValues(source, "network_error")
		m.fetchDuration.WithLabelValues(source)
		m.fetchHTTPResponses.WithLabelValues(source, "none")
		m.sourceLastSuccess[source] = time.Time{}
	}
	m.aggregatorBuckets.WithLabelValues().Set(0)
	m.aggregatorRuns.WithLabelValues("success", "none")
	m.aggregatorSkipped.WithLabelValues("usage_read_error")
	m.inference.WithLabelValues("anthropic", "success", "none")
	m.httpRequests.WithLabelValues("/api/v1/public/radar/service-health", "none")
	m.httpDuration.WithLabelValues("/api/v1/public/radar/service-health")
	m.redisOperations.WithLabelValues("list_meta", "read", "success")
	m.cacheMemory.WithLabelValues("metadata").Set(0)
}

// MetricsHandler exposes a Prometheus gatherer without request/body logging.
func MetricsHandler(gatherer prometheus.Gatherer) http.Handler {
	if gatherer == nil {
		gatherer = prometheus.DefaultGatherer
	}
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

func (m *RadarMetrics) RecordFetchSuccess(source string, status int, duration time.Duration, lastSuccess time.Time) {
	rawSource := source
	source = normalizeSource(rawSource)
	m.fetchSuccess.WithLabelValues(source).Inc()
	m.fetchDuration.WithLabelValues(source).Observe(nonNegativeSeconds(duration))
	m.fetchHTTPResponses.WithLabelValues(source, normalizeStatus(status)).Inc()
	m.mu.Lock()
	if previous, exists := m.sourceLastSuccess[rawSource]; !exists || previous.IsZero() || lastSuccess.After(previous) {
		m.sourceLastSuccess[rawSource] = lastSuccess.UTC()
	}
	m.mu.Unlock()
}

func (m *RadarMetrics) RecordFetchFailure(source, reason string, status int, duration time.Duration) {
	rawSource := source
	source = normalizeSource(rawSource)
	m.RegisterSource(rawSource)
	m.fetchFailure.WithLabelValues(source, normalizeFetchReason(reason)).Inc()
	m.fetchDuration.WithLabelValues(source).Observe(nonNegativeSeconds(duration))
	m.fetchHTTPResponses.WithLabelValues(source, normalizeStatus(status)).Inc()
}

// RegisterSource records one validated canonical runner source. Raw AA model
// identifiers remain private map keys and are collapsed at collection time.
func (m *RadarMetrics) RegisterSource(source string) {
	m.mu.Lock()
	if _, exists := m.sourceLastSuccess[source]; !exists {
		m.sourceLastSuccess[source] = time.Time{}
	}
	m.mu.Unlock()
}

// HydrateSourceSuccess restores the last committed success observed in Redis.
func (m *RadarMetrics) HydrateSourceSuccess(source string, lastSuccess time.Time) {
	m.RegisterSource(source)
	if lastSuccess.IsZero() {
		return
	}
	m.mu.Lock()
	if previous := m.sourceLastSuccess[source]; previous.IsZero() || lastSuccess.After(previous) {
		m.sourceLastSuccess[source] = lastSuccess.UTC()
	}
	m.mu.Unlock()
}

// SyncSourceSuccess replaces local source freshness with the persisted shared
// state. A zero timestamp means the configured source has never succeeded.
func (m *RadarMetrics) SyncSourceSuccess(source string, lastSuccess time.Time) {
	m.mu.Lock()
	m.sourceLastSuccess[source] = lastSuccess.UTC()
	m.mu.Unlock()
}

func (m *RadarMetrics) RecordAggregator(result, reason string, duration time.Duration, _ int) {
	result = normalizeResult(result)
	m.aggregatorDuration.Observe(nonNegativeSeconds(duration))
	m.aggregatorRuns.WithLabelValues(result, normalizeAggregatorReason(reason, result)).Inc()
}

func (m *RadarMetrics) SetAggregatorBucketCount(bucketCount int) {
	if bucketCount < 0 {
		bucketCount = 0
	}
	m.aggregatorBuckets.WithLabelValues().Set(float64(bucketCount))
}

func (m *RadarMetrics) SetAggregatorInterval(interval time.Duration) {
	seconds := interval.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	m.aggregatorInterval.Set(seconds)
}

func (m *RadarMetrics) SetEnabled(enabled bool) {
	value := 0.0
	if enabled {
		value = 1
	}
	m.enabled.Set(value)
}

func (m *RadarMetrics) SyncAggregatorState(bucketCount int, lastRunAt, lastSuccessAt time.Time) {
	m.SetAggregatorBucketCount(bucketCount)
	lastRun := 0.0
	if !lastRunAt.IsZero() {
		lastRun = float64(lastRunAt.UTC().Unix())
	}
	lastSuccess := 0.0
	if !lastSuccessAt.IsZero() {
		lastSuccess = float64(lastSuccessAt.UTC().Unix())
	}
	m.aggregatorLastRun.Set(lastRun)
	m.aggregatorLastOK.Set(lastSuccess)
}

func (m *RadarMetrics) RecordAggregatorCompletion(completedAt time.Time, success bool, bucketCount int) {
	if completedAt.IsZero() {
		return
	}
	m.aggregatorLastRun.Set(float64(completedAt.UTC().Unix()))
	if success {
		m.SetAggregatorBucketCount(bucketCount)
		m.aggregatorLastOK.Set(float64(completedAt.UTC().Unix()))
	}
}

func (m *RadarMetrics) AddAggregatorSkipped(reason string, count int) {
	if count > 0 {
		m.aggregatorSkipped.WithLabelValues(normalizeSkipReason(reason)).Add(float64(count))
	}
}

func (m *RadarMetrics) AddInference(bucket, result, reason string, count int) {
	if count > 0 {
		result = normalizeResult(result)
		m.inference.WithLabelValues(normalizeBucket(bucket), result, normalizeInferenceReason(reason, result)).Add(float64(count))
	}
}

func (m *RadarMetrics) ObserveHTTP(route string, status int, duration time.Duration) {
	route = normalizeRoute(route)
	m.httpRequests.WithLabelValues(route, normalizeStatus(status)).Inc()
	m.httpDuration.WithLabelValues(route).Observe(nonNegativeSeconds(duration))
}

func (m *RadarMetrics) RecordRedis(operation, access string, err error) {
	result := "success"
	if err != nil {
		result = "error"
	}
	m.redisOperations.WithLabelValues(normalizeRedisOperation(operation), normalizeRedisAccess(access), result).Inc()
}

func (m *RadarMetrics) SetCacheMemory(cache string, bytes int) {
	m.SetCacheMemoryEntry(cache, "singleton", bytes)
}

// SetCacheMemoryEntry updates one private cache entry while exporting only its
// fixed family total. Entry identities never become Prometheus labels.
func (m *RadarMetrics) SetCacheMemoryEntry(cache, identity string, bytes int) {
	cache = normalizeCache(cache)
	if bytes < 0 {
		bytes = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.cacheEntries[cache]
	if entries == nil {
		entries = make(map[string]int)
		m.cacheEntries[cache] = entries
	}
	entries[identity] = bytes
	total := 0
	for _, entryBytes := range entries {
		total += entryBytes
	}
	m.cacheMemory.WithLabelValues(cache).Set(float64(total))
}

// ReplaceCacheMemoryEntries atomically hydrates one fixed cache family.
func (m *RadarMetrics) ReplaceCacheMemoryEntries(cache string, entries map[string]int) {
	cache = normalizeCache(cache)
	copied := make(map[string]int, len(entries))
	total := 0
	for identity, bytes := range entries {
		if bytes < 0 {
			bytes = 0
		}
		copied[identity] = bytes
		total += bytes
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cacheEntries[cache] = copied
	m.cacheMemory.WithLabelValues(cache).Set(float64(total))
}

// SetCacheMemoryTotals applies a shared Redis snapshot to this process.
func (m *RadarMetrics) SetCacheMemoryTotals(totals map[string]int) {
	for _, cache := range []string{"quota_bucket", "aa", "lmarena", "service_health", "metadata"} {
		m.ReplaceCacheMemoryEntries(cache, map[string]int{"shared": totals[cache]})
	}
}

func nonNegativeSeconds(duration time.Duration) float64 {
	if duration < 0 {
		return 0
	}
	return duration.Seconds()
}

func normalizeSource(source string) string {
	switch source {
	case "aa", "lmarena", "status_claude", "status_openai", "status_windsurf", "status_deepseek",
		"status_kimi", "status_minimax_global", "status_minimax_china":
		return source
	}
	return "other"
}

func normalizeFetchReason(reason string) string {
	switch reason {
	case "network_error", "unauthorized", "rate_limited", "invalid_response", "upstream_error", "canceled", "deadline_exceeded", "storage_error", "superseded":
		return reason
	default:
		return "other"
	}
}

func normalizeStatus(status int) string {
	if status >= 100 && status <= 599 {
		return strconv.Itoa(status/100) + "xx"
	}
	return "none"
}

func normalizeResult(result string) string {
	if result == "success" || result == "rejected" || result == "failure" {
		return result
	}
	return "failure"
}

func normalizeAggregatorReason(reason, result string) string {
	if result == "success" {
		return "none"
	}
	switch reason {
	case "canceled", "deadline_exceeded", "storage_error", "aggregation_error", "lock_skipped", "overlap_skipped":
		return reason
	default:
		return "other"
	}
}

func normalizeSkipReason(reason string) string {
	switch reason {
	case "usage_read_error", "invalid_window", "invalid_bucket", "duplicate_account", "privacy_threshold":
		return reason
	default:
		return "other"
	}
}

func normalizeInferenceReason(reason, result string) string {
	if result == "success" {
		return "none"
	}
	switch reason {
	case "insufficient_samples", "high_dispersion", "invalid_mean":
		return reason
	default:
		return "other"
	}
}

func normalizeBucket(bucket string) string {
	platform := strings.ToLower(strings.TrimSpace(strings.SplitN(bucket, "/", 2)[0]))
	switch platform {
	case "anthropic", "openai", "antigravity":
		return platform
	default:
		return "other"
	}
}

func normalizeRoute(route string) string {
	switch route {
	case "/api/v1/public/radar/service-health",
		"/api/v1/public/radar/quota-buckets/latest",
		"/api/v1/public/radar/quota-buckets/trend",
		"/api/v1/public/radar/degradation/latest",
		"/api/v1/public/radar/lmarena",
		"/api/v1/public/radar/sources":
		return route
	default:
		return "other"
	}
}

func normalizeRedisOperation(operation string) string {
	switch operation {
	case "append_bucket", "list_buckets", "get_latest_bucket", "get_bucket_trend",
		"set_source", "get_source", "commit_success", "commit_failure",
		"set_meta", "list_meta", "source_cadence", "try_lock", "release_lock", "cache_size", "aggregator_state":
		return operation
	default:
		return "other"
	}
}

func normalizeRedisAccess(access string) string {
	if access == "read" || access == "write" || access == "lock" {
		return access
	}
	return "other"
}

func normalizeCache(cache string) string {
	switch cache {
	case "quota_bucket", "aa", "lmarena", "service_health", "metadata":
		return cache
	default:
		return "other"
	}
}
