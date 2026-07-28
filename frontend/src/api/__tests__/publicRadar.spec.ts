import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get },
}))

import {
  getDataSources,
  getDegradationLatest,
  getLMArena,
  getQuotaBucketsLatest,
  getQuotaBucketsTrend,
  getServiceHealth,
} from '@/api/publicRadar'

describe('public radar api', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it.each([
    ['service health', getServiceHealth, '/public/radar/service-health'],
    ['degradation latest', getDegradationLatest, '/public/radar/degradation/latest'],
    ['LMArena', getLMArena, '/public/radar/lmarena'],
    ['data sources', getDataSources, '/public/radar/sources'],
  ] as const)('GETs %s from the public endpoint and forwards AbortSignal', async (_, request, path) => {
    const payload = { path }
    const controller = new AbortController()
    const options = Object.freeze({ signal: controller.signal })
    get.mockResolvedValueOnce({ data: payload })

    const result = await request(options)

    expect(get).toHaveBeenCalledOnce()
    expect(get).toHaveBeenCalledWith(path, {
      params: {},
      paramsSerializer: expect.any(Function),
      signal: controller.signal,
    })
    const config = get.mock.calls[0][1] as {
      paramsSerializer: (params: Record<string, unknown>) => string
    }
    expect(config.paramsSerializer({ timezone: 'Asia/Shanghai', evil: 'must-not-leak' })).toBe('')
    expect(result).toBe(payload)
    expect(options).toEqual({ signal: controller.signal })
  })

  it('GETs quota latest without serializing interceptor-injected or unknown params', async () => {
    const payload = { buckets: [] }
    const controller = new AbortController()
    const options = Object.freeze({ signal: controller.signal })
    get.mockResolvedValueOnce({ data: payload })

    const result = await getQuotaBucketsLatest(options)

    expect(get).toHaveBeenCalledWith('/public/radar/quota-buckets/latest', {
      params: {},
      paramsSerializer: expect.any(Function),
      signal: controller.signal,
    })
    const config = get.mock.calls[0][1] as {
      paramsSerializer: (params: Record<string, unknown>) => string
    }
    expect(
      config.paramsSerializer({ timezone: 'Asia/Shanghai', evil: 'must-not-leak' })
    ).toBe('')
    expect(result).toBe(payload)
    expect(options).toEqual({ signal: controller.signal })
  })

  it('GETs the quota trend with the default seven-day query', async () => {
    const payload = { bucket_key: 'anthropic/max_20x' }
    get.mockResolvedValueOnce({ data: payload })

    const result = await getQuotaBucketsTrend('anthropic/max_20x')

    expect(get).toHaveBeenCalledWith('/public/radar/quota-buckets/trend', {
      params: { bucket: 'anthropic/max_20x', days: 7 },
      paramsSerializer: expect.any(Function),
      signal: undefined,
    })
    const config = get.mock.calls[0][1] as {
      paramsSerializer: (params: Record<string, unknown>) => string
    }
    expect(
      config.paramsSerializer({
        bucket: 'anthropic/max_20x',
        days: 7,
        timezone: 'Asia/Shanghai',
        evil: 'must-not-leak',
      })
    ).toBe('bucket=anthropic%2Fmax_20x&days=7')
    expect(result).toBe(payload)
  })

  it('GETs the quota trend with explicit days and forwards AbortSignal', async () => {
    const controller = new AbortController()
    const options = Object.freeze({ signal: controller.signal })
    get.mockResolvedValueOnce({ data: { days: 3 } })

    await getQuotaBucketsTrend('openai/pro_20x', 3, options)

    expect(get).toHaveBeenCalledWith('/public/radar/quota-buckets/trend', {
      params: { bucket: 'openai/pro_20x', days: 3 },
      paramsSerializer: expect.any(Function),
      signal: controller.signal,
    })
    expect(options).toEqual({ signal: controller.signal })
  })

  it('returns response.data rather than the Axios response wrapper', async () => {
    const data = [{ service_key: 'claude_api' }]
    const response = { data, status: 200, headers: { etag: 'radar' } }
    get.mockResolvedValueOnce(response)

    const result = await getServiceHealth()

    expect(result).toBe(data)
    expect(result).not.toBe(response)
  })

  it.each([
    ['backend error', Object.freeze({ status: 503, message: 'unavailable' })],
    ['cancellation', Object.freeze({ code: 'ERR_CANCELED', message: 'canceled' })],
  ])('propagates the original %s object', async (_, error) => {
    get.mockRejectedValueOnce(error)

    await expect(getDataSources()).rejects.toBe(error)
  })
})
