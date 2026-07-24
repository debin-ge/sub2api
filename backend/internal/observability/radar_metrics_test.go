package observability

import (
	"errors"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestRadarMetricsFreshRegistryExposesRequiredRealZeroSamples(t *testing.T) {
	registry := prometheus.NewRegistry()
	_, err := NewRadarMetrics(registry)
	require.NoError(t, err)
	body := scrapeRegistry(t, registry)

	families := []string{
		"radar_fetch_success_total",
		"radar_fetch_failure_total",
		"radar_fetch_duration_seconds",
		"radar_fetch_http_responses_total",
		"radar_source_age_seconds",
		"radar_aggregator_duration_seconds",
		"radar_aggregator_bucket_count",
		"radar_aggregator_last_run_timestamp_seconds",
		"radar_aggregator_last_success_timestamp_seconds",
		"radar_aggregator_interval_seconds",
		"radar_enabled",
		"radar_aggregator_runs_total",
		"radar_aggregator_skipped_accounts_total",
		"radar_inference_total",
		"radar_http_requests_total",
		"radar_http_request_duration_seconds",
		"radar_redis_operations_total",
		"radar_cache_memory_bytes",
	}
	for _, family := range families {
		t.Run(family, func(t *testing.T) {
			quoted := regexp.QuoteMeta(family)
			realSample := regexp.MustCompile(`(?m)^` + quoted + `(?:\{[^}]*\})?\s+[-+0-9.eE]+$|^` + quoted + `_(?:bucket|sum|count)(?:\{[^}]*\})?\s+[-+0-9.eE]+$`)
			require.Regexp(t, realSample, body, "HELP/TYPE without a sample is insufficient")
		})
	}

	for _, zeroSample := range []string{
		`radar_fetch_success_total{source="status_claude"} 0`,
		`radar_fetch_failure_total{reason="network_error",source="status_claude"} 0`,
		`radar_fetch_duration_seconds_count{source="status_claude"} 0`,
		`radar_fetch_duration_seconds_sum{source="status_claude"} 0`,
		`radar_fetch_http_responses_total{source="status_claude",status="none"} 0`,
		`radar_source_age_seconds{source="status_claude"} -1`,
		`radar_aggregator_duration_seconds_count 0`,
		`radar_aggregator_duration_seconds_sum 0`,
		`radar_aggregator_runs_total{reason="none",result="success"} 0`,
		`radar_aggregator_skipped_accounts_total{reason="usage_read_error"} 0`,
		`radar_inference_total{bucket="anthropic",reason="none",result="success"} 0`,
		`radar_http_requests_total{route="/api/v1/public/radar/service-health",status="none"} 0`,
		`radar_http_request_duration_seconds_count{route="/api/v1/public/radar/service-health"} 0`,
		`radar_http_request_duration_seconds_sum{route="/api/v1/public/radar/service-health"} 0`,
		`radar_redis_operations_total{access="read",operation="list_meta",result="success"} 0`,
		`radar_cache_memory_bytes{cache="metadata"} 0`,
	} {
		require.Contains(t, body, zeroSample)
	}
}

func TestRadarMetricsExposeRequiredLowCardinalityContract(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewRadarMetrics(registry)
	require.NoError(t, err)

	metrics.RecordFetchSuccess("aa", 200, 25*time.Millisecond, time.Now().Add(-2*time.Second))
	metrics.RecordFetchFailure("attacker-controlled", "credential=secret", 599, 10*time.Millisecond)
	metrics.RecordAggregator("success", "", 30*time.Millisecond, 4)
	metrics.RecordAggregatorCompletion(time.Now(), true, 4)
	metrics.SetAggregatorInterval(15 * time.Minute)
	metrics.SetEnabled(true)
	metrics.AddAggregatorSkipped("account-123", 2)
	metrics.AddInference("openai/private-plan", "success", "", 1)
	metrics.RecordRedis("unknown-operation-with-key", "read", errors.New("redis: radar:quota:bucket:private"))
	metrics.SetCacheMemory("radar:quota:bucket:private", 512)
	metrics.ObserveHTTP("/api/v1/public/radar/private", 503, 5*time.Millisecond)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/metrics", nil)
	MetricsHandler(registry).ServeHTTP(recorder, request)
	require.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()

	for _, metricName := range []string{
		"radar_fetch_success_total",
		"radar_fetch_failure_total",
		"radar_fetch_duration_seconds",
		"radar_fetch_http_responses_total",
		"radar_source_age_seconds",
		"radar_aggregator_duration_seconds",
		"radar_aggregator_bucket_count",
		"radar_aggregator_last_run_timestamp_seconds",
		"radar_aggregator_last_success_timestamp_seconds",
		"radar_aggregator_interval_seconds",
		"radar_enabled",
		"radar_aggregator_runs_total",
		"radar_aggregator_skipped_accounts_total",
		"radar_inference_total",
		"radar_http_requests_total",
		"radar_http_request_duration_seconds",
		"radar_redis_operations_total",
		"radar_cache_memory_bytes",
	} {
		require.Contains(t, body, metricName)
	}

	require.Contains(t, body, `source="aa"`)
	require.Contains(t, body, `source="other"`)
	require.Contains(t, body, `reason="other"`)
	require.Contains(t, body, `bucket="openai"`)
	require.Contains(t, body, `route="other"`)
	require.Contains(t, body, `operation="other"`)
	require.Contains(t, body, `cache="other"`)
	require.NotContains(t, body, "secret-model")
	require.NotContains(t, body, "credential=secret")
	require.NotContains(t, body, "account-123")
	require.NotContains(t, body, "private-plan")
	require.NotContains(t, body, "radar:quota:bucket:private")
	require.False(t, strings.Contains(body, "private\""))
}

func TestRadarCacheMemorySumsPrivateEntriesWithinFixedFamily(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewRadarMetrics(registry)
	require.NoError(t, err)

	metrics.SetCacheMemoryEntry("quota_bucket", "private-hash-a", 100)
	metrics.SetCacheMemoryEntry("quota_bucket", "private-hash-b", 250)
	metrics.SetCacheMemoryEntry("quota_bucket", "private-hash-a", 175)
	body := scrapeRegistry(t, registry)
	require.Contains(t, body, `radar_cache_memory_bytes{cache="quota_bucket"} 425`)
	require.NotContains(t, body, "private-hash")
}

func scrapeRegistry(t *testing.T, registry prometheus.Gatherer) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	MetricsHandler(registry).ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	require.Equal(t, 200, recorder.Code)
	return recorder.Body.String()
}
