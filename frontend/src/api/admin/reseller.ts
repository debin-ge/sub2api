import { apiClient } from '../client'

export type ResellerBalanceStatus =
  | 'disabled'
  | 'not_configured'
  | 'ok'
  | 'auth_failed'
  | 'upstream_unreachable'
  | 'invalid_response'
  | 'upstream_error'

export interface ResellerUpstreamBalance {
  enabled: boolean
  configured: boolean
  upstream_endpoint: string
  balance: number
  user_id?: number
  status: ResellerBalanceStatus
  checked_at?: string
}

export async function getUpstreamBalance(): Promise<ResellerUpstreamBalance> {
  const { data } = await apiClient.get<ResellerUpstreamBalance>('/admin/reseller/upstream-balance')
  return data
}

export const resellerAPI = {
  getUpstreamBalance
}

export default resellerAPI
