import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, post } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, put, post },
}))

import {
  getRadarAdminStatus,
  triggerRadarAdminRefresh,
  updateRadarAdminSettings,
  type RadarAdminStatus,
} from '@/api/admin/radar'

describe('admin radar api', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    post.mockReset()
  })

  it('loads status without serializing interceptor-injected query parameters', async () => {
    const controller = new AbortController()
    const payload = {
      enabled: true,
      sources: [],
      aggregator: {
        key: 'quota_aggregator',
        status: 'healthy',
        stale: true,
        last_attempt_at: '2026-07-15T02:00:00Z',
        last_success_at: '2026-07-15T02:00:00Z',
        last_failure_at: null,
        next_fire_at: '2026-07-15T02:15:00Z',
        http_status: null,
        error: null,
      },
    } satisfies RadarAdminStatus
    get.mockResolvedValueOnce({ data: payload })

    const result = await getRadarAdminStatus({ signal: controller.signal })

    expect(get).toHaveBeenCalledWith('/admin/radar/status', {
      params: {},
      paramsSerializer: expect.any(Function),
      signal: controller.signal,
    })
    const config = get.mock.calls[0]?.[1] as {
      paramsSerializer: (params: Record<string, unknown>) => string
    }
    expect(config.paramsSerializer({ timezone: 'Asia/Shanghai', unexpected: 'blocked' })).toBe('')
    expect(result).toBe(payload)
  })

  it('updates only the runtime enabled setting and forwards AbortSignal', async () => {
    const controller = new AbortController()
    const payload = { enabled: false }
    put.mockResolvedValueOnce({ data: payload })

    const result = await updateRadarAdminSettings(false, { signal: controller.signal })

    expect(put).toHaveBeenCalledWith(
      '/admin/radar/settings',
      { enabled: false },
      { signal: controller.signal },
    )
    expect(result).toBe(payload)
  })

  it('triggers a manual refresh with no request body', async () => {
    const controller = new AbortController()
    const payload = {
      refresh_id: 'refresh-0123456789abcdef0123456789abcdef',
      status: 'triggered',
      tasks: ['lmarena', 'quota_aggregator'],
    }
    post.mockResolvedValueOnce({ data: payload })

    const result = await triggerRadarAdminRefresh({ signal: controller.signal })

    expect(post).toHaveBeenCalledWith(
      '/admin/radar/refresh',
      undefined,
      { signal: controller.signal },
    )
    expect(result).toBe(payload)
  })
})
