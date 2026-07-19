package observability

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type radarAlertRulesFile struct {
	Groups []struct {
		Name  string `yaml:"name"`
		Rules []struct {
			Alert string `yaml:"alert"`
			Expr  string `yaml:"expr"`
			For   string `yaml:"for"`
		} `yaml:"rules"`
	} `yaml:"groups"`
}

func TestRadarAlertRulesCoverLaunchTargetsAndParseAsYAML(t *testing.T) {
	path := filepath.Join("..", "..", "..", "deploy", "monitoring", "radar-alerts.yml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var rules radarAlertRulesFile
	require.NoError(t, yaml.Unmarshal(raw, &rules))
	require.NotEmpty(t, rules.Groups)

	alerts := make(map[string]string)
	for _, group := range rules.Groups {
		require.NotEmpty(t, group.Name)
		for _, rule := range group.Rules {
			require.NotEmpty(t, rule.Alert)
			require.NotEmpty(t, rule.Expr)
			require.NotEmpty(t, rule.For)
			alerts[rule.Alert] = rule.Expr
		}
	}

	for _, name := range []string{
		"RadarSourceSustainedFailure", "RadarSourceDataStale",
		"RadarLatestSuccessVisibilityBelowTarget", "RadarAggregatorP95Slow",
		"RadarAggregatorFailure", "RadarAggregatorNoRecentRun", "RadarBucketCountAnomalousDrop",
		"RadarQuotaSnapshotSuccessBelowTarget", "RadarCoreAvailabilityBelowTarget",
		"RadarAPI5xxRateHigh", "RadarAPIP95AboveTarget", "RadarRedisErrors",
		"RadarCacheMemoryHigh",
	} {
		require.Contains(t, alerts, name)
		require.Contains(t, alerts[name], "job", name+" must preserve Prometheus job isolation")
	}

	allExpressions := strings.Join(mapValues(alerts), "\n")
	for _, metric := range []string{
		"radar_fetch_failure_total", "radar_source_age_seconds",
		"radar_aggregator_duration_seconds_bucket", "radar_aggregator_bucket_count",
		"radar_aggregator_runs_total", "radar_http_requests_total",
		"radar_http_request_duration_seconds_bucket", "radar_redis_operations_total",
		"radar_cache_memory_bytes", "radar_enabled",
	} {
		require.Contains(t, allExpressions, metric)
	}
	require.Contains(t, alerts["RadarQuotaSnapshotSuccessBelowTarget"], "0.99")
	require.Contains(t, alerts["RadarCoreAvailabilityBelowTarget"], "0.995")
	require.Contains(t, alerts["RadarLatestSuccessVisibilityBelowTarget"], "< 1")
	require.Contains(t, alerts["RadarAPIP95AboveTarget"], "> 0.1")
	stale := alerts["RadarSourceDataStale"]
	require.Contains(t, stale, `source=~"status_claude|status_openai"`)
	require.Contains(t, stale, "> 3600")
	require.Contains(t, stale, `source="aa"`)
	require.Contains(t, stale, "> 43200")
	require.Contains(t, stale, `source=~"aa_performance|lmarena"`)
	require.Contains(t, stale, "> 172800")
	failures := alerts["RadarSourceSustainedFailure"]
	require.Contains(t, failures, `[2h]`)
	require.Contains(t, failures, `[18h]`)
	require.Contains(t, failures, `[3d]`)
	visibility := alerts["RadarLatestSuccessVisibilityBelowTarget"]
	for _, threshold := range []string{"3600", "43200", "172800"} {
		require.Contains(t, visibility, threshold)
	}
	require.Contains(t, stale, "min by (job, source)", "source age must be reduced within each job")
	require.Contains(t, visibility, "min by (job, source)", "visibility must preserve job isolation")
	require.GreaterOrEqual(t, strings.Count(visibility, "or on (job)"), 3, "every visibility category must complete missing fresh series with zero")
	bucketDrop := alerts["RadarBucketCountAnomalousDrop"]
	require.Contains(t, bucketDrop, "min by (job) (radar_aggregator_bucket_count)")
	require.Contains(t, bucketDrop, "avg_over_time((min by (job) (radar_aggregator_bucket_count))[24h:30s])", "24h baseline must be de-replicated before the range subquery")
	require.NotContains(t, bucketDrop, "min(avg_over_time", "replica series must not be averaged before de-replication")
	require.Contains(t, alerts["RadarCacheMemoryHigh"], "sum by (job) (max by (job, cache) (radar_cache_memory_bytes))")
	require.Contains(t, alerts["RadarAggregatorNoRecentRun"], "radar_aggregator_last_run_timestamp_seconds")
	require.Contains(t, alerts["RadarAggregatorNoRecentRun"], "radar_aggregator_interval_seconds")
	require.Contains(t, alerts["RadarAggregatorNoRecentRun"], "radar_enabled")
	require.Contains(t, alerts["RadarQuotaSnapshotSuccessBelowTarget"], "or on (job)")
	require.Contains(t, alerts["RadarCoreAvailabilityBelowTarget"], "or on (job)")
	require.Contains(t, alerts["RadarAPI5xxRateHigh"], "or on (job)")
}

func TestDefaultCaddyDeniesPublicMetricsProxy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "Caddyfile"))
	require.NoError(t, err)
	content := string(raw)
	require.Contains(t, content, "path /metrics")
	require.Contains(t, content, "respond @metrics 404")
	require.Less(t, strings.Index(content, "respond @metrics 404"), strings.Index(content, "reverse_proxy"))
}

func TestRadarPrometheusSampleUsesBearerCredentialsFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "monitoring", "prometheus-radar.yml"))
	require.NoError(t, err)
	content := string(raw)
	require.Contains(t, content, "authorization:")
	require.Contains(t, content, "type: Bearer")
	require.Contains(t, content, "credentials_file:")
	require.NotContains(t, content, "credentials: ", "sample config must not embed the secret")
}

func TestRadarMonitoringConfigWithPromtoolWhenAvailable(t *testing.T) {
	promtool, err := exec.LookPath("promtool")
	if err != nil {
		t.Skip("promtool is not installed")
	}
	monitoringDir := filepath.Join("..", "..", "..", "deploy", "monitoring")
	for _, args := range [][]string{
		{"check", "rules", "radar-alerts.yml"},
		{"check", "config", "--syntax-only", "prometheus-radar.yml"},
		{"test", "rules", "radar-rules.test.yml"},
	} {
		cmd := exec.Command(promtool, args...)
		cmd.Dir = monitoringDir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
	}
}

func TestRadarMonitoringScriptPinsAndVerifiesOfficialPromtoolArchives(t *testing.T) {
	monitoringDir := filepath.Join("..", "..", "..", "deploy", "monitoring")
	scriptPath := filepath.Join(monitoringDir, "check-radar-monitoring.sh")
	raw, err := os.ReadFile(scriptPath)
	require.NoError(t, err)
	content := string(raw)
	for _, digest := range []string{
		"0c046a68e51c0e7245b7cc37a83c3db69cc0af8224de9947b24c48512f120462",
		"194e57f02dd2d1e3691eafc6f14b11cdc2c569d64f9cdefd0bf18b561843e097",
		"630177c6ad011193987904f09ffafec29d531abfeb5e43fa3714e376e5f28ddc",
		"6c4ba48d2efe582bd70c296a2184fbb1adf03c1cb3ef8e8b61bb009ed3d73c85",
	} {
		require.Contains(t, content, digest)
	}
	require.Contains(t, content, "sha256sum")
	require.Contains(t, content, "shasum -a 256")
	require.Contains(t, content, "SHA-256 mismatch")
	require.Contains(t, content, "--syntax-only")

	promtool, err := exec.LookPath("promtool")
	if err != nil {
		t.Skip("promtool is not installed")
	}
	cmd := exec.Command("sh", "check-radar-monitoring.sh")
	cmd.Dir = monitoringDir
	cmd.Env = append(os.Environ(), "PROMTOOL_BIN="+promtool)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}

func TestRadarGrafanaDashboardIsImportableAndCoversCoreMetrics(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "monitoring", "radar-dashboard.json"))
	require.NoError(t, err)
	var dashboard struct {
		Title  string `json:"title"`
		Panels []struct {
			Targets []struct {
				Expr string `json:"expr"`
			} `json:"targets"`
		} `json:"panels"`
	}
	require.NoError(t, json.Unmarshal(raw, &dashboard))
	require.Equal(t, "Sub2API Model Radar", dashboard.Title)
	require.GreaterOrEqual(t, len(dashboard.Panels), 8)
	expressions := make([]string, 0)
	for _, panel := range dashboard.Panels {
		for _, target := range panel.Targets {
			expressions = append(expressions, target.Expr)
			require.Contains(t, target.Expr, "job", "every dashboard query must retain or filter the job dimension")
		}
	}
	all := strings.Join(expressions, "\n")
	for _, metric := range []string{"radar_source_age_seconds", "radar_http_request_duration_seconds_bucket", "radar_aggregator_runs_total", "radar_redis_operations_total", "radar_cache_memory_bytes"} {
		require.Contains(t, all, metric)
	}
	require.Contains(t, all, "min by (job, source)")
	require.Contains(t, all, "min by (job) (radar_aggregator_bucket_count")
	require.Contains(t, all, "avg_over_time((min by (job) (radar_aggregator_bucket_count")
	require.Contains(t, all, "max by (job, cache)")
	require.Contains(t, all, "radar_aggregator_last_run_timestamp_seconds")
	require.Contains(t, all, "$job")
}

func mapValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}
