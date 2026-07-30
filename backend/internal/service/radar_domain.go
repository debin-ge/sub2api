package service

import "time"

// RadarMetricsSnapshot is shared Redis-backed operational state synchronized
// into every application replica. It contains no account or raw cache keys.
type RadarMetricsSnapshot struct {
	ActiveBucketCount       int
	PublishedBucketCount    int
	AggregatorLastAttemptAt time.Time
	AggregatorLastRunAt     time.Time
	AggregatorLastSuccessAt time.Time
	AggregatorNextFireAt    time.Time
	AggregatorStateValid    bool
	CacheMemoryBytes        map[string]int
	CacheMemoryValid        bool
	Partial                 bool
	SourceLastSuccess       map[RadarSourceKey]time.Time
}

// RadarAggregatorRunState is the bounded shared state committed by the lock
// winner after an aggregation attempt. RunVersion is derived from the run
// start time and provides monotonic compare-and-set ordering across replicas.
type RadarAggregatorRunState struct {
	RunVersion           int64
	CompletedAt          time.Time
	NextFireAt           time.Time
	Success              bool
	PublishedBucketCount int
}

// ServiceKey is the stable public identifier for a radar service.
type ServiceKey string

const (
	ServiceKeyClaudeAPI  ServiceKey = "claude_api"
	ServiceKeyClaudeCode ServiceKey = "claude_code"
	ServiceKeyCodexWeb   ServiceKey = "codex_web"
	ServiceKeyOpenAIAPI  ServiceKey = "openai_api"
	ServiceKeyWindsurf   ServiceKey = "windsurf"
	ServiceKeyDeepSeek   ServiceKey = "deepseek"
	ServiceKeyKimi       ServiceKey = "kimi"
	ServiceKeyMiniMax    ServiceKey = "minimax"
)

// RadarServiceDescriptor pairs a stable service key with its public name.
type RadarServiceDescriptor struct {
	Key  ServiceKey
	Name string
}

// CanonicalRadarServices returns the public services in their stable display order.
// Each call returns an independent slice that callers may safely modify.
func CanonicalRadarServices() []RadarServiceDescriptor {
	result := make([]RadarServiceDescriptor, 0, 4)
	for _, registration := range radarServiceRegistrations() {
		if registration.AlwaysPresent {
			result = append(result, registration.Descriptor)
		}
	}
	return result
}

// ServiceStatus is the normalized operational status of a public service.
type ServiceStatus string

const (
	ServiceStatusOperational         ServiceStatus = "operational"
	ServiceStatusDegradedPerformance ServiceStatus = "degraded_performance"
	ServiceStatusPartialOutage       ServiceStatus = "partial_outage"
	ServiceStatusMajorOutage         ServiceStatus = "major_outage"
	ServiceStatusUnderMaintenance    ServiceStatus = "under_maintenance"
	ServiceStatusUnknown             ServiceStatus = "unknown"
)

// StatusIndicator is the normalized severity reported by a status source.
type StatusIndicator string

const (
	StatusIndicatorNone     StatusIndicator = "none"
	StatusIndicatorMinor    StatusIndicator = "minor"
	StatusIndicatorMajor    StatusIndicator = "major"
	StatusIndicatorCritical StatusIndicator = "critical"
	StatusIndicatorUnknown  StatusIndicator = "unknown"
)

// InferenceRejectReason explains why an API-equivalent value could not be inferred.
type InferenceRejectReason string

const (
	InferenceRejectReasonInsufficientSamples InferenceRejectReason = "insufficient_samples"
	InferenceRejectReasonHighDispersion      InferenceRejectReason = "high_dispersion"
	InferenceRejectReasonInvalidMean         InferenceRejectReason = "invalid_mean"
	InferenceRejectReasonUnknownPlan         InferenceRejectReason = "unknown_plan"
)

// InferenceConfidence communicates how much independent account evidence
// supports an inferred API-equivalent quota value. A singleton estimate is
// intentionally low confidence even though it remains publishable.
type InferenceConfidence string

const (
	InferenceConfidenceLow    InferenceConfidence = "low"
	InferenceConfidenceMedium InferenceConfidence = "medium"
	InferenceConfidenceHigh   InferenceConfidence = "high"
)

// DataSourceState is the public lifecycle state of a radar data source.
type DataSourceState string

const (
	DataSourceStateNotConfigured  DataSourceState = "not_configured"
	DataSourceStateNeverAttempted DataSourceState = "never_attempted"
	DataSourceStateHealthy        DataSourceState = "healthy"
	DataSourceStateFailed         DataSourceState = "failed"
)

// DataSourceErrorCode is a safe public classification of a source failure.
type DataSourceErrorCode string

const (
	DataSourceErrorCodeNetworkError    DataSourceErrorCode = "network_error"
	DataSourceErrorCodeUnauthorized    DataSourceErrorCode = "unauthorized"
	DataSourceErrorCodeRateLimited     DataSourceErrorCode = "rate_limited"
	DataSourceErrorCodeInvalidResponse DataSourceErrorCode = "invalid_response"
	DataSourceErrorCodeUpstreamError   DataSourceErrorCode = "upstream_error"
	DataSourceErrorCodeAggregation     DataSourceErrorCode = "aggregation_error"
)

// RadarIncidentDTO describes the latest public incident for a service.
type RadarIncidentDTO struct {
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Impact     string     `json:"impact"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
}

// ServiceHealthHistoryDayDTO summarizes official incidents affecting one
// service on a UTC calendar day. Incidents is always a non-nil slice.
type ServiceHealthHistoryDayDTO struct {
	Date      string             `json:"date"`
	Status    ServiceStatus      `json:"status"`
	Incidents []RadarIncidentDTO `json:"incidents"`
}

// ServiceHealthDTO describes the current health of a public service.
type ServiceHealthDTO struct {
	ServiceKey           ServiceKey                   `json:"service_key"`
	Platform             string                       `json:"platform"`
	PlatformOrder        int                          `json:"platform_order"`
	Name                 string                       `json:"name"`
	Status               ServiceStatus                `json:"status"`
	StatusIndicator      StatusIndicator              `json:"status_indicator"`
	Uptime90d            *float64                     `json:"uptime_90d"`
	LastIncident         *RadarIncidentDTO            `json:"last_incident"`
	LastUpdatedAt        *time.Time                   `json:"last_updated_at"`
	History30d           []ServiceHealthHistoryDayDTO `json:"history_30d"`
	HistoryDays          int                          `json:"history_days"`
	UptimeWindowDays     int                          `json:"uptime_window_days"`
	RecentIncidentDays   int                          `json:"recent_incident_days"`
	IncidentPreviewLimit int                          `json:"incident_preview_limit"`
	SourceURL            string                       `json:"source_url"`
	Stale                bool                         `json:"stale"`
}

// WindowStatsDTO contains aggregate quota statistics for one time window.
type WindowStatsDTO struct {
	AvgUtilization        float64                `json:"avg_utilization"`
	MinUtilization        float64                `json:"min_utilization"`
	MaxUtilization        float64                `json:"max_utilization"`
	AvgCost               float64                `json:"avg_cost"`
	InferredLimitUSD      *float64               `json:"inferred_limit_usd"`
	InferredStdev         *float64               `json:"inferred_stdev"`
	SampleSize            int                    `json:"sample_size"`
	ContributorsCount     int                    `json:"contributors_count"`
	InferenceConfidence   InferenceConfidence    `json:"inference_confidence,omitempty"`
	InferenceRejectReason *InferenceRejectReason `json:"inference_reject_reason,omitempty"`
}

// ModelWindowStatsDTO contains a model's aggregate utilization in a window.
type ModelWindowStatsDTO struct {
	Model          string  `json:"model"`
	AvgUtilization float64 `json:"avg_utilization"`
	SampleSize     int     `json:"sample_size"`
}

// ModelCostBreakdownDTO contains a model's contribution to aggregate cost.
type ModelCostBreakdownDTO struct {
	Model             string  `json:"model"`
	AvgCost           float64 `json:"avg_cost"`
	AvgRequests       int64   `json:"avg_requests"`
	Percentage        float64 `json:"percentage"`
	ContributorsCount int     `json:"contributors_count"`
}

// QuotaWindowDTO describes one supported quota window without coupling clients
// to platform-specific field names.
type QuotaWindowDTO struct {
	Key             string                  `json:"key"`
	Label           string                  `json:"label"`
	DurationSeconds int64                   `json:"duration_seconds"`
	Currency        string                  `json:"currency"`
	Stats           *WindowStatsDTO         `json:"stats"`
	ModelWindows    []ModelWindowStatsDTO   `json:"model_windows"`
	ModelBreakdown  []ModelCostBreakdownDTO `json:"model_breakdown"`
}

// BucketSnapshotDTO contains the latest public aggregate for one quota bucket.
type BucketSnapshotDTO struct {
	CalculationVersion int              `json:"calculation_version"`
	BucketKey          string           `json:"bucket_key"`
	Platform           string           `json:"platform"`
	PlanTier           string           `json:"plan_tier"`
	DisplayName        string           `json:"display_name"`
	AccountsCount      int              `json:"accounts_count"`
	PrivacyThreshold   int              `json:"privacy_threshold"`
	Windows            []QuotaWindowDTO `json:"windows"`
	// Legacy fields remain during the Redis snapshot compatibility window.
	FiveHour         *WindowStatsDTO         `json:"five_hour"`
	SevenDay         *WindowStatsDTO         `json:"seven_day"`
	SevenDaySonnet   *ModelWindowStatsDTO    `json:"seven_day_sonnet"`
	SevenDayFable    *ModelWindowStatsDTO    `json:"seven_day_fable"`
	ModelBreakdown5h []ModelCostBreakdownDTO `json:"model_breakdown_5h"`
	ModelBreakdown7d []ModelCostBreakdownDTO `json:"model_breakdown_7d"`
	CapturedAt       time.Time               `json:"captured_at"`
	Stale            bool                    `json:"stale"`
}

// QuotaRadarLatestDTO is the latest public quota radar snapshot.
type QuotaRadarLatestDTO struct {
	Buckets             []BucketSnapshotDTO `json:"buckets"`
	LastAggregatedAt    *time.Time          `json:"last_aggregated_at"`
	SampleSizeWarnBelow int                 `json:"sample_size_warn_below"`
	Stale               bool                `json:"stale"`
}

// QuotaTrendWindowDTO contains one window's aggregate values at a trend point.
type QuotaTrendWindowDTO struct {
	AvgUtilization      float64             `json:"avg_utilization"`
	AvgCost             float64             `json:"avg_cost"`
	InferredLimitUSD    *float64            `json:"inferred_limit_usd"`
	SampleSize          int                 `json:"sample_size"`
	InferenceConfidence InferenceConfidence `json:"inference_confidence,omitempty"`
}

// QuotaTrendPointWindowDTO identifies one window within a trend point.
type QuotaTrendPointWindowDTO struct {
	Key             string               `json:"key"`
	Label           string               `json:"label"`
	DurationSeconds int64                `json:"duration_seconds"`
	Currency        string               `json:"currency"`
	Stats           *QuotaTrendWindowDTO `json:"stats"`
}

// QuotaTrendPointDTO contains quota window aggregates captured at one time.
type QuotaTrendPointDTO struct {
	Timestamp time.Time                  `json:"timestamp"`
	Windows   []QuotaTrendPointWindowDTO `json:"windows"`
	// Legacy fields remain for clients deployed before the generic window DTO.
	FiveHour *QuotaTrendWindowDTO `json:"five_hour"`
	SevenDay *QuotaTrendWindowDTO `json:"seven_day"`
}

// QuotaTrendDTO is the public trend series for one quota bucket.
type QuotaTrendDTO struct {
	BucketKey  string               `json:"bucket_key"`
	Days       int                  `json:"days"`
	DataPoints []QuotaTrendPointDTO `json:"data_points"`
	Stale      bool                 `json:"stale"`
}

// DegradationModelDTO contains quality and price metrics for one model.
type DegradationCatalogMatchDTO struct {
	ModelID  string `json:"model_id"`
	Platform string `json:"platform"`
}

type DegradationModelDTO struct {
	Slug              string                       `json:"slug"`
	Name              string                       `json:"name"`
	Vendor            string                       `json:"vendor"`
	IntelligenceIndex *float64                     `json:"intelligence_index"`
	CodingIndex       *float64                     `json:"coding_index"`
	AgenticIndex      *float64                     `json:"agentic_index"`
	PriceInputPer1M   *float64                     `json:"price_input_per_1m"`
	PriceOutputPer1M  *float64                     `json:"price_output_per_1m"`
	LastUpdatedAt     *time.Time                   `json:"last_updated_at"`
	CatalogMatches    []DegradationCatalogMatchDTO `json:"catalog_matches"`
}

// LMArenaEntryDTO is one public LMArena leaderboard entry.
type LMArenaEntryDTO struct {
	Rank    int      `json:"rank"`
	Model   string   `json:"model"`
	Vendor  *string  `json:"vendor"`
	Elo     *float64 `json:"elo"`
	CILower *float64 `json:"ci_lower"`
	CIUpper *float64 `json:"ci_upper"`
	Votes   *int64   `json:"votes"`
}

// DegradationLatestDTO is the latest public model degradation snapshot.
type DegradationLatestDTO struct {
	Models                   []DegradationModelDTO  `json:"models"`
	AvailableModels          []DegradationModelDTO  `json:"available_models"`
	DefaultModelSlugs        []string               `json:"default_model_slugs"`
	Metrics                  []DegradationMetricDTO `json:"metrics"`
	MaxSelectedModels        int                    `json:"max_selected_models"`
	DefaultModelCount        int                    `json:"default_model_count"`
	ScoreMin                 float64                `json:"score_min"`
	ScoreMax                 float64                `json:"score_max"`
	ScoreStep                float64                `json:"score_step"`
	IntelligenceIndexVersion *float64               `json:"intelligence_index_version"`
	LMArenaTop5              []LMArenaEntryDTO      `json:"lmarena_top5"`
	SourcesLastUpdated       map[string]*time.Time  `json:"sources_last_updated"`
	Stale                    bool                   `json:"stale"`
}

// DegradationMetricDTO declares an ordered benchmark metric exposed by the
// backend. Presentation details remain localized by the frontend.
type DegradationMetricDTO struct {
	Key string `json:"key"`
}

// LMArenaDTO is the public LMArena leaderboard snapshot.
type LMArenaDTO struct {
	Leaderboard   []LMArenaEntryDTO `json:"leaderboard"`
	TotalVotes    *int64            `json:"total_votes"`
	LastUpdatedAt *time.Time        `json:"last_updated_at"`
	FetchedAt     *time.Time        `json:"fetched_at"`
	Stale         bool              `json:"stale"`
}

// DataSourceMetaDTO describes the freshness and health of a radar data source.
type DataSourceMetaDTO struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// URL is an allowlisted canonical public source URL, never a configured fetch URL.
	URL           string     `json:"url"`
	Platform      *string    `json:"platform"`
	PlatformOrder *int       `json:"platform_order"`
	Interval      string     `json:"interval"`
	LastAttemptAt *time.Time `json:"last_attempt_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	NextFireAt    *time.Time `json:"next_fire_at"`
	HTTPStatus    *int       `json:"http_status"`
	// Error is a sanitized public code and never contains raw operational error text.
	Error     *DataSourceErrorCode `json:"error,omitempty"`
	State     DataSourceState      `json:"state"`
	IsHealthy bool                 `json:"is_healthy"`
	Stale     bool                 `json:"stale"`
}
