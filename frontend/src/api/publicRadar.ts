import { apiClient } from './client'
import type {
  DataSourceMetaDTO,
  DegradationLatestDTO,
  DegradationMetric,
  DegradationTrendDTO,
  LMArenaDTO,
  QuotaRadarLatestDTO,
  QuotaTrendDTO,
  ServiceHealthDTO,
} from '@/types/radar'

// apiClient already owns the /api/v1 prefix, yielding /api/v1/public/radar/* on the wire.
const PUBLIC_RADAR_PATH = '/public/radar'

export interface PublicRadarRequestOptions {
  readonly signal?: AbortSignal
}

// The shared GET interceptor adds `timezone` to params. Radar quota handlers reject
// unknown query keys, so these endpoint-local serializers emit only their allowlist.
function serializeNoQueryParams(): string {
  return ''
}

function serializeQuotaTrendParams(params: Record<string, unknown>): string {
  const query = new URLSearchParams()
  const bucket = params.bucket
  const days = params.days

  if (typeof bucket === 'string') {
    query.set('bucket', bucket)
  }
  if (typeof days === 'number' || typeof days === 'string') {
    query.set('days', String(days))
  }

  return query.toString()
}

function serializeDegradationTrendParams(params: Record<string, unknown>): string {
  const query = new URLSearchParams()
  const model = params.model
  const metric = params.metric
  const days = params.days

  if (typeof model === 'string') {
    query.set('model', model)
  }
  if (typeof metric === 'string') {
    query.set('metric', metric)
  }
  if (typeof days === 'number' || typeof days === 'string') {
    query.set('days', String(days))
  }

  return query.toString()
}

export async function getServiceHealth(
  options?: PublicRadarRequestOptions
): Promise<ServiceHealthDTO[]> {
  const { data } = await apiClient.get<ServiceHealthDTO[]>(`${PUBLIC_RADAR_PATH}/service-health`, {
    params: {},
    paramsSerializer: serializeNoQueryParams,
    signal: options?.signal,
  })
  return data
}

export async function getQuotaBucketsLatest(
  options?: PublicRadarRequestOptions
): Promise<QuotaRadarLatestDTO> {
  const { data } = await apiClient.get<QuotaRadarLatestDTO>(
    `${PUBLIC_RADAR_PATH}/quota-buckets/latest`,
    {
      params: {},
      paramsSerializer: serializeNoQueryParams,
      signal: options?.signal,
    }
  )
  return data
}

export async function getQuotaBucketsTrend(
  bucketKey: string,
  days = 7,
  options?: PublicRadarRequestOptions
): Promise<QuotaTrendDTO> {
  const { data } = await apiClient.get<QuotaTrendDTO>(`${PUBLIC_RADAR_PATH}/quota-buckets/trend`, {
    params: { bucket: bucketKey, days },
    paramsSerializer: serializeQuotaTrendParams,
    signal: options?.signal,
  })
  return data
}

export async function getDegradationLatest(
  options?: PublicRadarRequestOptions
): Promise<DegradationLatestDTO> {
  const { data } = await apiClient.get<DegradationLatestDTO>(
    `${PUBLIC_RADAR_PATH}/degradation/latest`,
    {
      params: {},
      paramsSerializer: serializeNoQueryParams,
      signal: options?.signal,
    }
  )
  return data
}

export async function getDegradationTrend(
  model: string,
  metric: DegradationMetric,
  days = 90,
  options?: PublicRadarRequestOptions
): Promise<DegradationTrendDTO> {
  const { data } = await apiClient.get<DegradationTrendDTO>(
    `${PUBLIC_RADAR_PATH}/degradation/trend`,
    {
      params: { model, metric, days },
      paramsSerializer: serializeDegradationTrendParams,
      signal: options?.signal,
    }
  )
  return data
}

export async function getLMArena(options?: PublicRadarRequestOptions): Promise<LMArenaDTO> {
  const { data } = await apiClient.get<LMArenaDTO>(`${PUBLIC_RADAR_PATH}/lmarena`, {
    params: {},
    paramsSerializer: serializeNoQueryParams,
    signal: options?.signal,
  })
  return data
}

export async function getDataSources(
  options?: PublicRadarRequestOptions
): Promise<DataSourceMetaDTO[]> {
  const { data } = await apiClient.get<DataSourceMetaDTO[]>(`${PUBLIC_RADAR_PATH}/sources`, {
    params: {},
    paramsSerializer: serializeNoQueryParams,
    signal: options?.signal,
  })
  return data
}
