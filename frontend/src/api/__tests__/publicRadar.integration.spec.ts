import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'

import { apiClient } from '@/api/client'
import {
  getDataSources,
  getDegradationLatest,
  getLMArena,
  getQuotaBucketsLatest,
  getQuotaBucketsTrend,
  getServiceHealth,
} from '@/api/publicRadar'
import type { QuotaRadarLatestDTO, QuotaTrendDTO } from '@/types/radar'

type StorageSnapshot = Array<readonly [string, string]>

function snapshotStorage(storage: Storage): StorageSnapshot {
  const snapshot: StorageSnapshot = []
  for (let index = 0; index < storage.length; index += 1) {
    const key = storage.key(index)
    if (key !== null) {
      const value = storage.getItem(key)
      if (value !== null) {
        snapshot.push([key, value])
      }
    }
  }
  return snapshot
}

function restoreStorage(storage: Storage, snapshot: StorageSnapshot): void {
  storage.clear()
  for (const [key, value] of snapshot) {
    storage.setItem(key, value)
  }
}

function successfulAdapter<T>(
  payload: T,
  inspect: (config: InternalAxiosRequestConfig) => void = () => undefined
): AxiosAdapter {
  return async (config) => {
    inspect(config)
    return {
      data: { code: 0, message: 'success', data: payload },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
}

describe('public radar api with the real Axios client', () => {
  let originalAdapter: typeof apiClient.defaults.adapter
  let originalBaseURL: typeof apiClient.defaults.baseURL
  let localStorageSnapshot: StorageSnapshot
  let sessionStorageSnapshot: StorageSnapshot

  beforeEach(() => {
    originalAdapter = apiClient.defaults.adapter
    originalBaseURL = apiClient.defaults.baseURL
    localStorageSnapshot = snapshotStorage(localStorage)
    sessionStorageSnapshot = snapshotStorage(sessionStorage)

    localStorage.clear()
    sessionStorage.clear()
    apiClient.defaults.baseURL = '/api/v1'
  })

  afterEach(() => {
    apiClient.defaults.adapter = originalAdapter
    apiClient.defaults.baseURL = originalBaseURL
    restoreStorage(localStorage, localStorageSnapshot)
    restoreStorage(sessionStorage, sessionStorageSnapshot)
    vi.restoreAllMocks()
  })

  it('keeps quota latest query-free after the request interceptor injects timezone', async () => {
    const payload: QuotaRadarLatestDTO = {
      buckets: [],
      last_aggregated_at: null,
      sample_size_warn_below: 3,
      stale: false,
    }
    let interceptedParams: Record<string, unknown> = {}
    let finalURL = ''
    const adapter = vi.fn(
      successfulAdapter(payload, (config) => {
        const params = config.params as Record<string, unknown>
        interceptedParams = { ...params }
        params.evil = 'must-not-leak'
        finalURL = apiClient.getUri(config)
      })
    )
    apiClient.defaults.adapter = adapter

    const result = await getQuotaBucketsLatest()

    expect(interceptedParams).toEqual(
      expect.objectContaining({ timezone: expect.any(String) })
    )
    expect(finalURL).toBe('/api/v1/public/radar/quota-buckets/latest')
    expect(finalURL).not.toContain('?')
    expect(finalURL).not.toContain('timezone')
    expect(finalURL).not.toContain('evil')
    expect(result).toBe(payload)
    expect(adapter).toHaveBeenCalledOnce()
  })

  it('serializes only the allowlisted quota trend params after interceptor injection', async () => {
    const payload: QuotaTrendDTO = {
      bucket_key: 'anthropic/max_20x',
      days: 7,
      data_points: [],
      stale: false,
    }
    let interceptedParams: Record<string, unknown> = {}
    let finalURL = ''
    const adapter = vi.fn(
      successfulAdapter(payload, (config) => {
        const params = config.params as Record<string, unknown>
        interceptedParams = { ...params }
        params.evil = 'must-not-leak'
        finalURL = apiClient.getUri(config)
      })
    )
    apiClient.defaults.adapter = adapter

    const result = await getQuotaBucketsTrend('anthropic/max_20x')

    expect(interceptedParams).toEqual(
      expect.objectContaining({
        bucket: 'anthropic/max_20x',
        days: 7,
        timezone: expect.any(String),
      })
    )
    expect(finalURL).toBe(
      '/api/v1/public/radar/quota-buckets/trend?bucket=anthropic%2Fmax_20x&days=7'
    )
    expect(finalURL).not.toContain('timezone')
    expect(finalURL).not.toContain('evil')
    expect(result).toBe(payload)
    expect(adapter).toHaveBeenCalledOnce()
  })

  it.each([
    {
      name: 'service health',
      request: (signal: AbortSignal) => getServiceHealth({ signal }),
      expectedURL: '/api/v1/public/radar/service-health',
    },
    {
      name: 'quota latest',
      request: (signal: AbortSignal) => getQuotaBucketsLatest({ signal }),
      expectedURL: '/api/v1/public/radar/quota-buckets/latest',
    },
    {
      name: 'quota trend',
      request: (signal: AbortSignal) =>
        getQuotaBucketsTrend('anthropic/max_20x', 7, { signal }),
      expectedURL:
        '/api/v1/public/radar/quota-buckets/trend?bucket=anthropic%2Fmax_20x&days=7',
    },
    {
      name: 'degradation latest',
      request: (signal: AbortSignal) => getDegradationLatest({ signal }),
      expectedURL: '/api/v1/public/radar/degradation/latest',
    },
    {
      name: 'LMArena',
      request: (signal: AbortSignal) => getLMArena({ signal }),
      expectedURL: '/api/v1/public/radar/lmarena',
    },
    {
      name: 'sources',
      request: (signal: AbortSignal) => getDataSources({ signal }),
      expectedURL: '/api/v1/public/radar/sources',
    },
  ])('keeps the $name wire query on its strict backend allowlist', async ({ request, expectedURL }) => {
    const controller = new AbortController()
    let finalURL = ''
    let observedSignal: AbortSignal | undefined
    const adapter = vi.fn(
      successfulAdapter({}, (config) => {
        observedSignal = config.signal
        const params = config.params as Record<string, unknown>
        expect(params.timezone).toEqual(expect.any(String))
        params.evil = 'must-not-leak'
        finalURL = apiClient.getUri(config)
      })
    )
    apiClient.defaults.adapter = adapter

    await request(controller.signal)

    expect(observedSignal).toBe(controller.signal)
    expect(finalURL).toBe(expectedURL)
    expect(finalURL).not.toContain('timezone')
    expect(finalURL).not.toContain('evil')
    expect(adapter).toHaveBeenCalledOnce()
  })

  it('preserves ERR_CANCELED for an already-aborted signal', async () => {
    const payload: QuotaRadarLatestDTO = {
      buckets: [],
      last_aggregated_at: null,
      sample_size_warn_below: 3,
      stale: false,
    }
    const adapter = vi.fn(successfulAdapter(payload))
    apiClient.defaults.adapter = adapter
    const controller = new AbortController()
    controller.abort()

    let cancellation: unknown
    try {
      await getQuotaBucketsLatest({ signal: controller.signal })
    } catch (error) {
      cancellation = error
    }

    expect(cancellation).toMatchObject({ code: 'ERR_CANCELED' })
    expect(adapter).not.toHaveBeenCalled()
  })
})
