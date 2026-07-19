import type {
  BucketSnapshotDTO,
  DataSourceErrorCode,
  DataSourceMetaDTO,
  DataSourceState,
  DegradationLatestDTO,
  DegradationMetric,
  DegradationModelDTO,
  DegradationTrendDTO,
  InferenceRejectReason,
  LMArenaDTO,
  LMArenaEntryDTO,
  MetricPointDTO,
  ModelCostBreakdownDTO,
  ModelWindowStatsDTO,
  QuotaRadarLatestDTO,
  QuotaTrendDTO,
  QuotaTrendPointDTO,
  QuotaTrendWindowDTO,
  RadarTimestamp,
  RadarIncidentDTO,
  RadarPlatform,
  ServiceKey,
  ServiceHealthDTO,
  ServiceStatus,
  StatusIndicator,
  WindowStatsDTO,
} from '../src/types/radar'

type Equal<Left, Right> =
  (<Value>() => Value extends Left ? 1 : 2) extends
  (<Value>() => Value extends Right ? 1 : 2)
    ? true
    : false

type Expect<Condition extends true> = Condition

type IsOptional<ObjectType, Key extends keyof ObjectType> =
  object extends Pick<ObjectType, Key> ? true : false

export type RadarContractAssertions = [
  Expect<Equal<RadarIncidentDTO['resolved_at'], string | null>>,
  Expect<Equal<ServiceHealthDTO['uptime_90d'], number | null>>,
  Expect<Equal<ServiceHealthDTO['last_incident'], RadarIncidentDTO | null>>,
  Expect<Equal<ServiceHealthDTO['last_updated_at'], string | null>>,
  Expect<Equal<WindowStatsDTO['inferred_limit_usd'], number | null>>,
  Expect<Equal<WindowStatsDTO['inferred_stdev'], number | null>>,
  Expect<Equal<IsOptional<WindowStatsDTO, 'inference_reject_reason'>, true>>,
  Expect<
    Equal<WindowStatsDTO['inference_reject_reason'], InferenceRejectReason | undefined>
  >,
  Expect<Equal<BucketSnapshotDTO['five_hour'], WindowStatsDTO | null>>,
  Expect<Equal<BucketSnapshotDTO['seven_day'], WindowStatsDTO | null>>,
  Expect<Equal<BucketSnapshotDTO['seven_day_sonnet'], ModelWindowStatsDTO | null>>,
  Expect<Equal<BucketSnapshotDTO['seven_day_fable'], ModelWindowStatsDTO | null>>,
  Expect<Equal<QuotaRadarLatestDTO['last_aggregated_at'], string | null>>,
  Expect<Equal<QuotaTrendWindowDTO['inferred_limit_usd'], number | null>>,
  Expect<Equal<QuotaTrendPointDTO['five_hour'], QuotaTrendWindowDTO | null>>,
  Expect<Equal<QuotaTrendPointDTO['seven_day'], QuotaTrendWindowDTO | null>>,
  Expect<Equal<DegradationModelDTO['intelligence_index'], number | null>>,
  Expect<Equal<DegradationModelDTO['coding_index'], number | null>>,
  Expect<Equal<DegradationModelDTO['agentic_index'], number | null>>,
  Expect<Equal<DegradationModelDTO['price_input_per_1m'], number | null>>,
  Expect<Equal<DegradationModelDTO['price_output_per_1m'], number | null>>,
  Expect<Equal<DegradationModelDTO['last_updated_at'], string | null>>,
  Expect<Equal<LMArenaEntryDTO['vendor'], string | null>>,
  Expect<Equal<LMArenaEntryDTO['elo'], number | null>>,
  Expect<Equal<LMArenaEntryDTO['ci_lower'], number | null>>,
  Expect<Equal<LMArenaEntryDTO['ci_upper'], number | null>>,
  Expect<Equal<LMArenaEntryDTO['votes'], number | null>>,
  Expect<Equal<LMArenaDTO['total_votes'], number | null>>,
  Expect<Equal<LMArenaDTO['fetched_at'], RadarTimestamp | null>>,
  Expect<Equal<LMArenaDTO['last_updated_at'], string | null>>,
  Expect<
    Equal<DegradationLatestDTO['sources_last_updated'], Record<string, string | null>>
  >,
  Expect<Equal<DataSourceMetaDTO['last_attempt_at'], string | null>>,
  Expect<Equal<DataSourceMetaDTO['last_success_at'], string | null>>,
  Expect<Equal<DataSourceMetaDTO['next_fire_at'], string | null>>,
  Expect<Equal<DataSourceMetaDTO['http_status'], number | null>>,
  Expect<Equal<IsOptional<DataSourceMetaDTO, 'error'>, true>>,
  Expect<Equal<DataSourceMetaDTO['error'], DataSourceErrorCode | undefined>>,
  Expect<Equal<ServiceKey, 'claude_api' | 'claude_code' | 'codex_web' | 'openai_api'>>,
  Expect<
    Equal<
      ServiceStatus,
      | 'operational'
      | 'degraded_performance'
      | 'partial_outage'
      | 'major_outage'
      | 'under_maintenance'
      | 'unknown'
    >
  >,
  Expect<Equal<StatusIndicator, 'none' | 'minor' | 'major' | 'critical' | 'unknown'>>,
  Expect<
    Equal<
      InferenceRejectReason,
      'insufficient_samples' | 'high_dispersion' | 'invalid_mean'
    >
  >,
  Expect<Equal<RadarPlatform, 'anthropic' | 'openai' | 'antigravity'>>,
  Expect<
    Equal<DegradationMetric, 'intelligence_index' | 'coding_index' | 'agentic_index'>
  >,
  Expect<Equal<DataSourceState, 'not_configured' | 'never_attempted' | 'healthy' | 'failed'>>,
  Expect<
    Equal<
      DataSourceErrorCode,
      | 'network_error'
      | 'unauthorized'
      | 'rate_limited'
      | 'invalid_response'
      | 'upstream_error'
      | 'aggregation_error'
    >
  >,
  Expect<Equal<QuotaRadarLatestDTO['buckets'], BucketSnapshotDTO[]>>,
  Expect<Equal<BucketSnapshotDTO['model_breakdown_5h'], ModelCostBreakdownDTO[]>>,
  Expect<Equal<BucketSnapshotDTO['model_breakdown_7d'], ModelCostBreakdownDTO[]>>,
  Expect<Equal<QuotaTrendDTO['data_points'], QuotaTrendPointDTO[]>>,
  Expect<Equal<DegradationLatestDTO['models'], DegradationModelDTO[]>>,
  Expect<Equal<DegradationLatestDTO['lmarena_top5'], LMArenaEntryDTO[]>>,
  Expect<Equal<DegradationTrendDTO['data_points'], MetricPointDTO[]>>,
  Expect<Equal<LMArenaDTO['leaderboard'], LMArenaEntryDTO[]>>,
]
