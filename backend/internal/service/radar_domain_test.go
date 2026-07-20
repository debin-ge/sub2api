package service

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRadarDomainEnumValues(t *testing.T) {
	t.Parallel()

	require.Equal(t, []ServiceKey{
		"claude_api", "claude_code", "codex_web", "openai_api", "windsurf", "deepseek", "kimi", "minimax",
	}, []ServiceKey{
		ServiceKeyClaudeAPI,
		ServiceKeyClaudeCode,
		ServiceKeyCodexWeb,
		ServiceKeyOpenAIAPI,
		ServiceKeyWindsurf,
		ServiceKeyDeepSeek,
		ServiceKeyKimi,
		ServiceKeyMiniMax,
	})

	require.Equal(t, []ServiceStatus{
		"operational",
		"degraded_performance",
		"partial_outage",
		"major_outage",
		"under_maintenance",
		"unknown",
	}, []ServiceStatus{
		ServiceStatusOperational,
		ServiceStatusDegradedPerformance,
		ServiceStatusPartialOutage,
		ServiceStatusMajorOutage,
		ServiceStatusUnderMaintenance,
		ServiceStatusUnknown,
	})

	require.Equal(t, []StatusIndicator{
		"none", "minor", "major", "critical", "unknown",
	}, []StatusIndicator{
		StatusIndicatorNone,
		StatusIndicatorMinor,
		StatusIndicatorMajor,
		StatusIndicatorCritical,
		StatusIndicatorUnknown,
	})

	require.Equal(t, []InferenceRejectReason{
		"insufficient_samples", "high_dispersion", "invalid_mean",
	}, []InferenceRejectReason{
		InferenceRejectReasonInsufficientSamples,
		InferenceRejectReasonHighDispersion,
		InferenceRejectReasonInvalidMean,
	})

	require.Equal(t, []DegradationMetric{
		"intelligence_index", "coding_index", "agentic_index",
	}, []DegradationMetric{
		DegradationMetricIntelligenceIndex,
		DegradationMetricCodingIndex,
		DegradationMetricAgenticIndex,
	})

	require.Equal(t, []DataSourceState{
		"not_configured", "never_attempted", "healthy", "failed",
	}, []DataSourceState{
		DataSourceStateNotConfigured,
		DataSourceStateNeverAttempted,
		DataSourceStateHealthy,
		DataSourceStateFailed,
	})

	require.Equal(t, []DataSourceErrorCode{
		"network_error", "unauthorized", "rate_limited", "invalid_response", "upstream_error", "aggregation_error",
	}, []DataSourceErrorCode{
		DataSourceErrorCodeNetworkError,
		DataSourceErrorCodeUnauthorized,
		DataSourceErrorCodeRateLimited,
		DataSourceErrorCodeInvalidResponse,
		DataSourceErrorCodeUpstreamError,
		DataSourceErrorCodeAggregation,
	})
}

func TestServiceHealthDTOCollectionContract(t *testing.T) {
	t.Parallel()

	descriptors := CanonicalRadarServices()
	services := make([]ServiceHealthDTO, 0, len(descriptors))
	for _, descriptor := range descriptors {
		services = append(services, ServiceHealthDTO{
			ServiceKey: descriptor.Key,
			Name:       descriptor.Name,
		})
	}

	encoded, err := json.Marshal(services)
	require.NoError(t, err)

	var decoded []map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	got := make([][2]string, 0, len(decoded))
	for _, service := range decoded {
		got = append(got, [2]string{
			service["service_key"].(string),
			service["name"].(string),
		})
	}
	require.Equal(t, [][2]string{
		{"claude_api", "Claude API"},
		{"claude_code", "Claude Code"},
		{"codex_web", "Codex Web"},
		{"openai_api", "OpenAI API"},
	}, got)
}

func TestCanonicalRadarServicesReturnsIndependentRegistry(t *testing.T) {
	t.Parallel()

	want := []RadarServiceDescriptor{
		{Key: ServiceKeyClaudeAPI, Name: "Claude API"},
		{Key: ServiceKeyClaudeCode, Name: "Claude Code"},
		{Key: ServiceKeyCodexWeb, Name: "Codex Web"},
		{Key: ServiceKeyOpenAIAPI, Name: "OpenAI API"},
	}

	first := CanonicalRadarServices()
	require.Equal(t, want, first)

	first[0].Key = ServiceKeyOpenAIAPI
	first[0].Name = "mutated"
	first = append(first, RadarServiceDescriptor{Key: ServiceKey("extra"), Name: "Extra"})

	require.Equal(t, want, CanonicalRadarServices())
}

func TestRadarDomainJSONKeysUseSnakeCase(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 8, 9, 10, 0, time.UTC)
	value := 1.5
	count := int64(42)
	requestCount := int64(3)
	httpStatus := 200
	vendor := "Anthropic"
	reason := InferenceRejectReasonHighDispersion
	sourceError := DataSourceErrorCodeUpstreamError

	tests := []struct {
		name string
		got  any
		want []string
	}{
		{
			name: "incident",
			got: RadarIncidentDTO{
				Name:       "Elevated errors",
				Status:     "resolved",
				Impact:     "minor",
				CreatedAt:  now,
				ResolvedAt: &now,
			},
			want: []string{"created_at", "impact", "name", "resolved_at", "status"},
		},
		{
			name: "service health history day",
			got: ServiceHealthHistoryDayDTO{
				Date: "2026-07-12", Status: ServiceStatusDegradedPerformance, Incidents: []RadarIncidentDTO{},
			},
			want: []string{"date", "incidents", "status"},
		},
		{
			name: "service health",
			got: ServiceHealthDTO{
				ServiceKey:      ServiceKeyOpenAIAPI,
				Name:            "OpenAI API",
				Status:          ServiceStatusOperational,
				StatusIndicator: StatusIndicatorNone,
				Uptime90d:       &value,
				LastIncident:    &RadarIncidentDTO{},
				LastUpdatedAt:   &now,
				SourceURL:       "https://status.openai.com",
				Stale:           false,
			},
			want: []string{"history_30d", "last_incident", "last_updated_at", "name", "service_key", "source_url", "stale", "status", "status_indicator", "uptime_90d"},
		},
		{
			name: "window stats",
			got: WindowStatsDTO{
				AvgUtilization:        20,
				MinUtilization:        10,
				MaxUtilization:        30,
				AvgCost:               2,
				InferredLimitUSD:      &value,
				InferredStdev:         &value,
				SampleSize:            3,
				InferenceRejectReason: &reason,
			},
			want: []string{"avg_cost", "avg_utilization", "contributors_count", "inference_reject_reason", "inferred_limit_usd", "inferred_stdev", "max_utilization", "min_utilization", "sample_size"},
		},
		{
			name: "model window stats",
			got:  ModelWindowStatsDTO{Model: "claude-sonnet", AvgUtilization: 2, SampleSize: 3},
			want: []string{"avg_utilization", "model", "sample_size"},
		},
		{
			name: "model cost breakdown",
			got:  ModelCostBreakdownDTO{Model: "claude-sonnet", AvgCost: 2, AvgRequests: requestCount, Percentage: 4},
			want: []string{"avg_cost", "avg_requests", "contributors_count", "model", "percentage"},
		},
		{
			name: "bucket snapshot",
			got: BucketSnapshotDTO{
				BucketKey:        "anthropic:pro",
				Platform:         "anthropic",
				PlanTier:         "pro",
				DisplayName:      "Anthropic Pro",
				AccountsCount:    3,
				FiveHour:         &WindowStatsDTO{},
				SevenDay:         &WindowStatsDTO{},
				SevenDaySonnet:   &ModelWindowStatsDTO{},
				SevenDayFable:    &ModelWindowStatsDTO{},
				ModelBreakdown5h: []ModelCostBreakdownDTO{},
				ModelBreakdown7d: []ModelCostBreakdownDTO{},
				CapturedAt:       now,
				Stale:            false,
			},
			want: []string{"accounts_count", "bucket_key", "captured_at", "display_name", "five_hour", "model_breakdown_5h", "model_breakdown_7d", "plan_tier", "platform", "privacy_threshold", "seven_day", "seven_day_fable", "seven_day_sonnet", "stale"},
		},
		{
			name: "quota latest",
			got: QuotaRadarLatestDTO{
				Buckets:             []BucketSnapshotDTO{},
				LastAggregatedAt:    &now,
				SampleSizeWarnBelow: 3,
				Stale:               false,
			},
			want: []string{"buckets", "last_aggregated_at", "sample_size_warn_below", "stale"},
		},
		{
			name: "quota trend window",
			got:  QuotaTrendWindowDTO{AvgUtilization: 1, AvgCost: 2, InferredLimitUSD: &value, SampleSize: 3},
			want: []string{"avg_cost", "avg_utilization", "inferred_limit_usd", "sample_size"},
		},
		{
			name: "quota trend point",
			got:  QuotaTrendPointDTO{Timestamp: now, FiveHour: &QuotaTrendWindowDTO{}, SevenDay: &QuotaTrendWindowDTO{}},
			want: []string{"five_hour", "seven_day", "timestamp"},
		},
		{
			name: "quota trend",
			got:  QuotaTrendDTO{BucketKey: "anthropic:pro", Days: 7, DataPoints: []QuotaTrendPointDTO{}, Stale: false},
			want: []string{"bucket_key", "data_points", "days", "stale"},
		},
		{
			name: "degradation model",
			got: DegradationModelDTO{
				Slug:              "claude-sonnet",
				Name:              "Claude Sonnet",
				Vendor:            "Anthropic",
				IntelligenceIndex: &value,
				CodingIndex:       &value,
				AgenticIndex:      &value,
				PriceInputPer1M:   &value,
				PriceOutputPer1M:  &value,
				LastUpdatedAt:     &now,
			},
			want: []string{"agentic_index", "coding_index", "intelligence_index", "last_updated_at", "name", "price_input_per_1m", "price_output_per_1m", "slug", "vendor"},
		},
		{
			name: "lmarena entry",
			got:  LMArenaEntryDTO{Rank: 1, Model: "claude-sonnet", Vendor: &vendor, Elo: &value, CILower: &value, CIUpper: &value, Votes: &count},
			want: []string{"ci_lower", "ci_upper", "elo", "model", "rank", "vendor", "votes"},
		},
		{
			name: "degradation latest",
			got: DegradationLatestDTO{
				Models:             []DegradationModelDTO{},
				LMArenaTop5:        []LMArenaEntryDTO{},
				SourcesLastUpdated: map[string]*time.Time{"lmarena": &now},
				Stale:              false,
			},
			want: []string{"lmarena_top5", "models", "sources_last_updated", "stale", "trend_available"},
		},
		{
			name: "metric point",
			got:  MetricPointDTO{Date: "2026-07-12", Value: 1},
			want: []string{"date", "value"},
		},
		{
			name: "degradation trend",
			got:  DegradationTrendDTO{ModelSlug: "claude-sonnet", Metric: DegradationMetricCodingIndex, Days: 30, DataPoints: []MetricPointDTO{}, Stale: false},
			want: []string{"data_points", "days", "metric", "model_slug", "stale"},
		},
		{
			name: "lmarena",
			got:  LMArenaDTO{Leaderboard: []LMArenaEntryDTO{}, TotalVotes: &count, LastUpdatedAt: &now, FetchedAt: &now, Stale: false},
			want: []string{"fetched_at", "last_updated_at", "leaderboard", "stale", "total_votes"},
		},
		{
			name: "data source meta",
			got: DataSourceMetaDTO{
				Key:           "lmarena",
				Name:          "LMArena",
				URL:           "https://lmarena.ai",
				Interval:      "24h",
				LastAttemptAt: &now,
				LastSuccessAt: &now,
				NextFireAt:    &now,
				HTTPStatus:    &httpStatus,
				Error:         &sourceError,
				State:         DataSourceStateHealthy,
				IsHealthy:     true,
				Stale:         false,
			},
			want: []string{"error", "http_status", "interval", "is_healthy", "key", "last_attempt_at", "last_success_at", "name", "next_fire_at", "stale", "state", "url"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(tt.got)
			require.NoError(t, err)

			var decoded map[string]any
			require.NoError(t, json.Unmarshal(encoded, &decoded))

			got := make([]string, 0, len(decoded))
			for key := range decoded {
				got = append(got, key)
			}
			sort.Strings(got)
			sort.Strings(tt.want)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBucketSnapshotDTONestedJSONShapes(t *testing.T) {
	t.Parallel()

	encoded := marshalRadarObject(t, BucketSnapshotDTO{
		SevenDaySonnet: &ModelWindowStatsDTO{
			Model:          "claude-sonnet",
			AvgUtilization: 12.5,
			SampleSize:     3,
		},
		SevenDayFable: &ModelWindowStatsDTO{
			Model:          "claude-fable",
			AvgUtilization: 7.5,
			SampleSize:     2,
		},
		ModelBreakdown5h: []ModelCostBreakdownDTO{
			{Model: "claude-sonnet", AvgCost: 1.25, AvgRequests: int64(9), Percentage: 75},
		},
		ModelBreakdown7d: []ModelCostBreakdownDTO{
			{Model: "claude-fable", AvgCost: 0.75, AvgRequests: int64(3), Percentage: 25},
		},
	})

	require.Equal(t, map[string]any{
		"model":           "claude-sonnet",
		"avg_utilization": 12.5,
		"sample_size":     float64(3),
	}, encoded["seven_day_sonnet"])
	require.Equal(t, map[string]any{
		"model":           "claude-fable",
		"avg_utilization": 7.5,
		"sample_size":     float64(2),
	}, encoded["seven_day_fable"])

	assertModelCostBreakdownJSON(t, encoded, "model_breakdown_5h", map[string]any{
		"model":              "claude-sonnet",
		"avg_cost":           1.25,
		"avg_requests":       float64(9),
		"percentage":         float64(75),
		"contributors_count": float64(0),
	})
	assertModelCostBreakdownJSON(t, encoded, "model_breakdown_7d", map[string]any{
		"model":              "claude-fable",
		"avg_cost":           0.75,
		"avg_requests":       float64(3),
		"percentage":         float64(25),
		"contributors_count": float64(0),
	})
}

func TestRadarDomainJSONPreservesNullsAndEmptyCollections(t *testing.T) {
	t.Parallel()

	incident := marshalRadarObject(t, RadarIncidentDTO{})
	require.Nil(t, incident["resolved_at"])

	health := marshalRadarObject(t, ServiceHealthDTO{})
	for _, key := range []string{"uptime_90d", "last_incident", "last_updated_at"} {
		require.Contains(t, health, key)
		require.Nil(t, health[key])
	}

	bucket := marshalRadarObject(t, BucketSnapshotDTO{
		ModelBreakdown5h: []ModelCostBreakdownDTO{},
		ModelBreakdown7d: []ModelCostBreakdownDTO{},
	})
	for _, key := range []string{"five_hour", "seven_day", "seven_day_sonnet", "seven_day_fable"} {
		require.Contains(t, bucket, key)
		require.Nil(t, bucket[key])
	}
	require.Equal(t, []any{}, bucket["model_breakdown_5h"])
	require.Equal(t, []any{}, bucket["model_breakdown_7d"])

	window := marshalRadarObject(t, WindowStatsDTO{})
	require.Contains(t, window, "inferred_limit_usd")
	require.Nil(t, window["inferred_limit_usd"])
	require.Contains(t, window, "inferred_stdev")
	require.Nil(t, window["inferred_stdev"])
	require.NotContains(t, window, "inference_reject_reason")

	trendWindow := marshalRadarObject(t, QuotaTrendWindowDTO{})
	require.Contains(t, trendWindow, "inferred_limit_usd")
	require.Nil(t, trendWindow["inferred_limit_usd"])

	trendPoint := marshalRadarObject(t, QuotaTrendPointDTO{})
	require.Nil(t, trendPoint["five_hour"])
	require.Nil(t, trendPoint["seven_day"])

	model := marshalRadarObject(t, DegradationModelDTO{})
	for _, key := range []string{"intelligence_index", "coding_index", "agentic_index", "price_input_per_1m", "price_output_per_1m", "last_updated_at"} {
		require.Contains(t, model, key)
		require.Nil(t, model[key])
	}

	entry := marshalRadarObject(t, LMArenaEntryDTO{})
	for _, key := range []string{"vendor", "elo", "ci_lower", "ci_upper", "votes"} {
		require.Contains(t, entry, key)
		require.Nil(t, entry[key])
	}

	latest := marshalRadarObject(t, QuotaRadarLatestDTO{Buckets: []BucketSnapshotDTO{}})
	require.Equal(t, []any{}, latest["buckets"])
	require.Nil(t, latest["last_aggregated_at"])

	trend := marshalRadarObject(t, QuotaTrendDTO{DataPoints: []QuotaTrendPointDTO{}})
	require.Equal(t, []any{}, trend["data_points"])

	degradation := marshalRadarObject(t, DegradationLatestDTO{
		Models:             []DegradationModelDTO{},
		LMArenaTop5:        []LMArenaEntryDTO{},
		SourcesLastUpdated: map[string]*time.Time{},
	})
	require.Equal(t, []any{}, degradation["models"])
	require.Equal(t, []any{}, degradation["lmarena_top5"])
	require.Equal(t, map[string]any{}, degradation["sources_last_updated"])

	degradationTrend := marshalRadarObject(t, DegradationTrendDTO{DataPoints: []MetricPointDTO{}})
	require.Equal(t, []any{}, degradationTrend["data_points"])

	lmarena := marshalRadarObject(t, LMArenaDTO{Leaderboard: []LMArenaEntryDTO{}})
	require.Equal(t, []any{}, lmarena["leaderboard"])
	require.Nil(t, lmarena["total_votes"])
	require.Nil(t, lmarena["last_updated_at"])
	require.Nil(t, lmarena["fetched_at"])

	source := marshalRadarObject(t, DataSourceMetaDTO{State: DataSourceStateNeverAttempted})
	for _, key := range []string{"last_attempt_at", "last_success_at", "next_fire_at"} {
		require.Contains(t, source, key)
		require.Nil(t, source[key])
	}
	require.Contains(t, source, "http_status")
	require.Nil(t, source["http_status"])
	require.NotContains(t, source, "error")
	require.Equal(t, "never_attempted", source["state"])
}

func TestRadarDomainJSONUsesRFC3339Timestamps(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 8, 9, 10, 123_000_000, time.UTC)
	encoded := marshalRadarObject(t, RadarIncidentDTO{CreatedAt: now, ResolvedAt: &now})

	require.Equal(t, "2026-07-12T08:09:10.123Z", encoded["created_at"])
	require.Equal(t, "2026-07-12T08:09:10.123Z", encoded["resolved_at"])
}

func TestDataSourceMetaDTOJSONUsesSafeErrorCode(t *testing.T) {
	t.Parallel()

	errorCode := DataSourceErrorCodeNetworkError
	encoded := marshalRadarObject(t, DataSourceMetaDTO{
		Error: &errorCode,
		State: DataSourceStateFailed,
	})

	require.Equal(t, "network_error", encoded["error"])
	require.Equal(t, "failed", encoded["state"])
	for _, rawErrorField := range []string{"error_message", "message", "details"} {
		require.NotContains(t, encoded, rawErrorField)
	}
}

func marshalRadarObject(t *testing.T, value any) map[string]any {
	t.Helper()

	encoded, err := json.Marshal(value)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	return decoded
}

func assertModelCostBreakdownJSON(t *testing.T, object map[string]any, key string, want map[string]any) {
	t.Helper()

	breakdown, ok := object[key].([]any)
	require.True(t, ok)
	require.Len(t, breakdown, 1)
	require.Equal(t, want, breakdown[0])
}
