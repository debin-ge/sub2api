import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({
  get: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get
  }
}))

import resellerAPI from '@/api/admin/reseller'

describe('admin reseller api', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('loads upstream balance', async () => {
    get.mockResolvedValueOnce({
      data: {
        enabled: true,
        configured: true,
        status: 'ok',
        balance: 12.34,
        user_id: 42,
        upstream_endpoint: 'https://parent.example.com'
      }
    })

    const result = await resellerAPI.getUpstreamBalance()

    expect(get).toHaveBeenCalledWith('/admin/reseller/upstream-balance')
    expect(result.status).toBe('ok')
    expect(result.balance).toBe(12.34)
  })
})
