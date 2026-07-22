/** Stable identifiers for public service-health cards. */
export type ServiceKey =
  | 'claude_api'
  | 'claude_code'
  | 'codex_web'
  | 'openai_api'
  | 'windsurf'
  | 'deepseek'
  | 'kimi'
  | 'minimax'

export type ServiceStatus =
  | 'operational'
  | 'degraded_performance'
  | 'partial_outage'
  | 'major_outage'
  | 'under_maintenance'
  | 'unknown'

export type StatusIndicator = 'none' | 'minor' | 'major' | 'critical' | 'unknown'

export type InferenceRejectReason =
  | 'insufficient_samples'
  | 'high_dispersion'
  | 'invalid_mean'

export type RadarPlatform = 'anthropic' | 'openai' | 'antigravity'

export type DegradationMetric = 'intelligence_index' | 'coding_index' | 'agentic_index'

export type DataSourceState = 'not_configured' | 'never_attempted' | 'healthy' | 'failed'

export type DataSourceErrorCode =
  | 'network_error'
  | 'unauthorized'
  | 'rate_limited'
  | 'invalid_response'
  | 'upstream_error'
  | 'aggregation_error'

/** ISO 8601 UTC timestamp serialized by Go's time.Time JSON encoder. */
export type RadarTimestamp = string

export interface RadarIncidentDTO {
  name: string
  status: string
  impact: string
  created_at: RadarTimestamp
  resolved_at: RadarTimestamp | null
}

export interface ServiceHealthHistoryDayDTO {
  date: string
  status: ServiceStatus
  incidents: RadarIncidentDTO[]
}

export interface ServiceHealthDTO {
  service_key: ServiceKey
  name: string
  status: ServiceStatus
  status_indicator: StatusIndicator
  uptime_90d: number | null
  last_incident: RadarIncidentDTO | null
  last_updated_at: RadarTimestamp | null
  history_30d: ServiceHealthHistoryDayDTO[]
  source_url: string
  stale: boolean
}

export interface WindowStatsDTO {
  avg_utilization: number
  min_utilization: number
  max_utilization: number
  avg_cost: number
  inferred_limit_usd: number | null
  inferred_stdev: number | null
  sample_size: number
  contributors_count: number
  inference_reject_reason?: InferenceRejectReason
}

export interface ModelWindowStatsDTO {
  model: string
  avg_utilization: number
  sample_size: number
}

export interface ModelCostBreakdownDTO {
  model: string
  avg_cost: number
  avg_requests: number
  percentage: number
  contributors_count: number
}

export interface BucketSnapshotDTO {
  bucket_key: string
  platform: RadarPlatform
  plan_tier: string
  display_name: string
  accounts_count: number
  privacy_threshold: number
  five_hour: WindowStatsDTO | null
  seven_day: WindowStatsDTO | null
  seven_day_sonnet: ModelWindowStatsDTO | null
  seven_day_fable: ModelWindowStatsDTO | null
  model_breakdown_5h: ModelCostBreakdownDTO[]
  model_breakdown_7d: ModelCostBreakdownDTO[]
  captured_at: RadarTimestamp
  stale: boolean
}

export interface QuotaRadarLatestDTO {
  buckets: BucketSnapshotDTO[]
  last_aggregated_at: RadarTimestamp | null
  sample_size_warn_below: number
  stale: boolean
}

export interface QuotaTrendWindowDTO {
  avg_utilization: number
  avg_cost: number
  inferred_limit_usd: number | null
  sample_size: number
}

export interface QuotaTrendPointDTO {
  timestamp: RadarTimestamp
  five_hour: QuotaTrendWindowDTO | null
  seven_day: QuotaTrendWindowDTO | null
}

export interface QuotaTrendDTO {
  bucket_key: string
  days: number
  data_points: QuotaTrendPointDTO[]
  stale: boolean
}

export interface DegradationModelDTO {
  slug: string
  name: string
  vendor: string
  intelligence_index: number | null
  coding_index: number | null
  agentic_index: number | null
  price_input_per_1m: number | null
  price_output_per_1m: number | null
  last_updated_at: RadarTimestamp | null
  catalog_matches: Array<{
    model_id: string
    platform: string
  }>
}

export interface LMArenaEntryDTO {
  rank: number
  model: string
  vendor: string | null
  elo: number | null
  ci_lower: number | null
  ci_upper: number | null
  votes: number | null
}

export interface DegradationLatestDTO {
  models: DegradationModelDTO[]
  available_models: DegradationModelDTO[]
  default_model_slugs: string[]
  intelligence_index_version: number | null
  lmarena_top5: LMArenaEntryDTO[]
  sources_last_updated: Record<string, RadarTimestamp | null>
  stale: boolean
}

export interface LMArenaDTO {
  leaderboard: LMArenaEntryDTO[]
  total_votes: number | null
  last_updated_at: RadarTimestamp | null
  fetched_at: RadarTimestamp | null
  stale: boolean
}

export interface DataSourceMetaDTO {
  key: string
  name: string
  url: string
  interval: string
  last_attempt_at: RadarTimestamp | null
  last_success_at: RadarTimestamp | null
  next_fire_at: RadarTimestamp | null
  http_status: number | null
  error?: DataSourceErrorCode
  state: DataSourceState
  is_healthy: boolean
  stale: boolean
}
