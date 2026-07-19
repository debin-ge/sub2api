import { apiClient } from '@/api/client'

const ADMIN_RADAR_PATH = '/admin/radar'

export type RadarAdminState = 'never_attempted' | 'healthy' | 'failed'

export type RadarAdminSafeError =
  | 'network_error'
  | 'unauthorized'
  | 'rate_limited'
  | 'invalid_response'
  | 'upstream_error'
  | 'aggregation_error'

export interface RadarAdminSourceStatus {
  key: string
  status: RadarAdminState
  stale: boolean
  last_attempt_at: string | null
  last_success_at: string | null
  last_failure_at: string | null
  next_fire_at: string | null
  http_status: number | null
  error: RadarAdminSafeError | null
}

export interface RadarAdminStatus {
  enabled: boolean
  sources: RadarAdminSourceStatus[]
  aggregator: RadarAdminSourceStatus
}

export interface RadarAdminSettings {
  enabled: boolean
}

export interface RadarAdminRefreshResult {
  refresh_id: string
  status: 'triggered' | 'coalesced'
  tasks: string[]
}

export interface RadarAdminRequestOptions {
  readonly signal?: AbortSignal
}

function serializeNoQueryParams(): string {
  return ''
}

export async function getRadarAdminStatus(
  options?: RadarAdminRequestOptions,
): Promise<RadarAdminStatus> {
  const { data } = await apiClient.get<RadarAdminStatus>(`${ADMIN_RADAR_PATH}/status`, {
    params: {},
    paramsSerializer: serializeNoQueryParams,
    signal: options?.signal,
  })
  return data
}

export async function updateRadarAdminSettings(
  enabled: boolean,
  options?: RadarAdminRequestOptions,
): Promise<RadarAdminSettings> {
  const { data } = await apiClient.put<RadarAdminSettings>(
    `${ADMIN_RADAR_PATH}/settings`,
    { enabled },
    { signal: options?.signal },
  )
  return data
}

export async function triggerRadarAdminRefresh(
  options?: RadarAdminRequestOptions,
): Promise<RadarAdminRefreshResult> {
  const { data } = await apiClient.post<RadarAdminRefreshResult>(
    `${ADMIN_RADAR_PATH}/refresh`,
    undefined,
    { signal: options?.signal },
  )
  return data
}

const radarAdminAPI = {
  getStatus: getRadarAdminStatus,
  updateSettings: updateRadarAdminSettings,
  triggerRefresh: triggerRadarAdminRefresh,
}

export default radarAdminAPI
