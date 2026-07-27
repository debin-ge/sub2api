import type {
  BucketSnapshotDTO,
  DataSourceMetaDTO,
  DegradationLatestDTO,
  LMArenaDTO,
  QuotaTrendDTO,
  ServiceHealthDTO,
  ServiceHealthHistoryDayDTO,
  ServiceKey,
  ServiceStatus,
  WindowStatsDTO,
} from '@/types/radar'

export const now = '2026-07-13T08:00:00.000Z'

export function serviceHistory(
  overrides: Readonly<Record<string, Partial<ServiceHealthHistoryDayDTO>>> = {}
): ServiceHealthHistoryDayDTO[] {
  const end = new Date('2026-07-13T00:00:00.000Z')
  return Array.from({ length: 30 }, (_, index) => {
    const date = new Date(end)
    date.setUTCDate(end.getUTCDate() - (29 - index))
    const key = date.toISOString().slice(0, 10)
    return { date: key, status: 'operational', incidents: [], ...overrides[key] }
  })
}

export function windowStats(overrides: Partial<WindowStatsDTO> = {}): WindowStatsDTO {
  return {
    avg_utilization: 55,
    min_utilization: 20,
    max_utilization: 80,
    avg_cost: 12.5,
    inferred_limit_usd: 100,
    inferred_stdev: 5,
    sample_size: 4,
    contributors_count: 4,
    inference_confidence: 'high',
    ...overrides,
  }
}

export function bucket(overrides: Partial<BucketSnapshotDTO> = {}): BucketSnapshotDTO {
  return {
    calculation_version: 2,
    bucket_key: 'anthropic/max_20x',
    platform: 'anthropic',
    plan_tier: 'max_20x',
    display_name: 'Claude Max 20x',
    accounts_count: 4,
    privacy_threshold: 2,
    five_hour: windowStats(),
    seven_day: windowStats({ avg_utilization: 35 }),
    seven_day_sonnet: null,
    seven_day_fable: null,
    model_breakdown_5h: [],
    model_breakdown_7d: [],
    captured_at: now,
    stale: false,
    ...overrides,
  }
}

export function quotaTrend(bucketKey = 'anthropic/max_20x'): QuotaTrendDTO {
  return {
    bucket_key: bucketKey,
    days: 7,
    data_points: [
      {
        timestamp: '2026-07-12T08:00:00.000Z',
        five_hour: { avg_utilization: 20, avg_cost: 4, inferred_limit_usd: 20, sample_size: 3, inference_confidence: 'high' },
        seven_day: null,
      },
      {
        timestamp: now,
        five_hour: { avg_utilization: 60, avg_cost: 12, inferred_limit_usd: 20, sample_size: 3, inference_confidence: 'high' },
        seven_day: null,
      },
    ],
    stale: false,
  }
}

export function service(
  serviceKey: ServiceKey,
  status: ServiceStatus = 'operational'
): ServiceHealthDTO {
  return {
    service_key: serviceKey,
    name: 'untrusted upstream name',
    status,
    status_indicator: 'none',
    uptime_90d: null,
    last_incident: null,
    last_updated_at: now,
    history_30d: serviceHistory(),
    source_url: 'https://status.claude.com',
    stale: false,
  }
}

export const degradationLatest: DegradationLatestDTO = {
  available_models: [
    {
      slug: 'model-a',
      name: 'Model A',
      vendor: 'Vendor A',
      intelligence_index: 91,
      coding_index: 82,
      agentic_index: 73,
      price_input_per_1m: null,
      price_output_per_1m: null,
      last_updated_at: now,
      catalog_matches: [
        { platform: 'openai', model_id: 'model-a-high' },
        { platform: 'openai', model_id: 'model-a-low' },
      ],
    },
    ...Array.from({ length: 6 }, (_, index) => ({
      slug: `model-${index + 2}`,
      name: `Model ${index + 2}`,
      vendor: 'Vendor',
      intelligence_index: 80 - index,
      coding_index: 70 - index,
      agentic_index: 60 - index,
      price_input_per_1m: null,
      price_output_per_1m: null,
      last_updated_at: now,
      catalog_matches: [{ platform: 'openai', model_id: `model-${index + 2}` }],
    })),
  ],
  models: [],
  default_model_slugs: ['model-a', 'model-2', 'model-3', 'model-4', 'model-5', 'model-6'],
  intelligence_index_version: 4.1,
  lmarena_top5: [],
  sources_last_updated: { aa: now },
  stale: false,
}

degradationLatest.models = degradationLatest.available_models.slice(0, 6)

export const lmarena: LMArenaDTO = {
  leaderboard: [
    { rank: 2, model: 'Second', vendor: null, elo: 1200, ci_lower: null, ci_upper: null, votes: null },
    { rank: 1, model: 'First', vendor: 'Vendor', elo: 1250, ci_lower: 1240, ci_upper: 1260, votes: 12345 },
  ],
  total_votes: 12345,
  last_updated_at: now,
  fetched_at: now,
  stale: false,
}

export function source(overrides: Partial<DataSourceMetaDTO> = {}): DataSourceMetaDTO {
  return {
    key: 'aa',
    name: 'Artificial Analysis',
    url: 'https://evil.example/secret',
    interval: '6h',
    last_attempt_at: now,
    last_success_at: now,
    next_fire_at: null,
    http_status: 200,
    state: 'healthy',
    is_healthy: true,
    stale: false,
    ...overrides,
  }
}
