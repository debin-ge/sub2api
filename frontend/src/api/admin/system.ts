/**
 * System API wrappers. Update/rollback wrappers were intentionally removed from
 * the frontend console; those backend endpoints remain for non-UI callers.
 */
import { apiClient } from '../client'

export interface SystemBuildInfo {
  /** This project's release tag (e.g. 2.1.12). */
  version: string
  /** Upstream Wei-Shaw/sub2api release merged into this build (e.g. 0.2.23). */
  upstream_version: string
  commit: string
  build_date: string
  build_type: string
}

export async function getBuildInfo(): Promise<SystemBuildInfo> {
  const { data } = await apiClient.get<SystemBuildInfo>('/admin/system/version')
  return data
}

export const systemAPI = {
  getBuildInfo
}

export default systemAPI
