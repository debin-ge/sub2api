import { apiClient } from './client'
import type { BenchmarkPublicRadar } from '@/types/benchmark'

export async function getCurrent(): Promise<BenchmarkPublicRadar> {
  const { data } = await apiClient.get<BenchmarkPublicRadar>('/public/radar')
  return data
}

export const radarAPI = {
  getCurrent,
}

export default radarAPI
