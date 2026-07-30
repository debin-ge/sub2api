import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
    put,
  },
}))

import {
  batchUpdateLimits,
  bindUserAuthIdentity,
  createVIPReconcileJob,
  getGroupCatalog,
  getVIPAudit,
  getVIPReconcileJob,
  getVIPReconcilePreview,
  list as listUsers,
  updateVIPMode,
  type AdminBindAuthIdentityRequest,
  type AdminBoundAuthIdentity,
  type BatchUpdateUserLimitsRequest,
  type BatchUpdateUserLimitsResponse,
} from '@/api/admin/users'

type Assert<T extends true> = T
type IsExact<T, U> = (
  (<G>() => G extends T ? 1 : 2) extends (<G>() => G extends U ? 1 : 2)
    ? ((<G>() => G extends U ? 1 : 2) extends (<G>() => G extends T ? 1 : 2) ? true : false)
    : false
)

type ExpectedAdminBindAuthIdentityRequest = {
  provider_type: string
  provider_key: string
  provider_subject: string
  issuer?: string
  metadata?: Record<string, unknown>
  channel?: {
    channel: string
    channel_app_id: string
    channel_subject: string
    metadata?: Record<string, unknown>
  }
}

type ExpectedAdminBoundAuthIdentity = {
  user_id: number
  provider_type: string
  provider_key: string
  provider_subject: string
  verified_at?: string | null
  issuer?: string | null
  metadata: Record<string, unknown> | null
  created_at: string
  updated_at: string
  channel?: {
    channel: string
    channel_app_id: string
    channel_subject: string
    metadata: Record<string, unknown> | null
    created_at: string
    updated_at: string
  } | null
}

const requestContractExact: Assert<
  IsExact<AdminBindAuthIdentityRequest, ExpectedAdminBindAuthIdentityRequest>
> = true
const responseContractExact: Assert<
  IsExact<AdminBoundAuthIdentity, ExpectedAdminBoundAuthIdentity>
> = true
const batchRequestContractExact: Assert<
  IsExact<
    BatchUpdateUserLimitsRequest,
    {
      user_ids: number[]
      all?: boolean
      concurrency?: number
      rpm_limit?: number
    }
  >
> = true
const batchResponseContractExact: Assert<
  IsExact<BatchUpdateUserLimitsResponse, { affected: number }>
> = true

describe('admin users api auth identity binding', () => {
  beforeEach(() => {
    post.mockReset()
  })

  it('posts the backend-compatible auth identity bind payload and returns the backend response shape', async () => {
    const payload: AdminBindAuthIdentityRequest = {
      provider_type: 'wechat',
      provider_key: 'wechat-main',
      provider_subject: 'union-123',
      metadata: { source: 'admin-repair' },
      channel: {
        channel: 'open',
        channel_app_id: 'wx-open',
        channel_subject: 'openid-123',
        metadata: { scene: 'migration' },
      },
    }

    const response: AdminBoundAuthIdentity = {
      user_id: 9,
      provider_type: 'wechat',
      provider_key: 'wechat-main',
      provider_subject: 'union-123',
      verified_at: '2026-04-22T00:00:00Z',
      issuer: null,
      metadata: { source: 'admin-repair' },
      created_at: '2026-04-22T00:00:00Z',
      updated_at: '2026-04-22T00:00:00Z',
      channel: {
        channel: 'open',
        channel_app_id: 'wx-open',
        channel_subject: 'openid-123',
        metadata: { scene: 'migration' },
        created_at: '2026-04-22T00:00:00Z',
        updated_at: '2026-04-22T00:00:00Z',
      },
    }
    post.mockResolvedValue({ data: response })

    const result = await bindUserAuthIdentity(9, payload)

    expect(post).toHaveBeenCalledWith('/admin/users/9/auth-identities', payload)
    expect(result).toEqual(response)
  })

  it('keeps bind auth identity request and response types aligned with the backend contract', () => {
    expect(requestContractExact).toBe(true)
    expect(responseContractExact).toBe(true)
  })

  it('posts batch limit updates once with only the supplied limit fields', async () => {
    const request: BatchUpdateUserLimitsRequest = {
      user_ids: [4, 7],
      all: false,
      rpm_limit: 0,
    }
    post.mockResolvedValue({ data: { affected: 2 } satisfies BatchUpdateUserLimitsResponse })

    const result = await batchUpdateLimits(request)

    expect(post).toHaveBeenCalledWith('/admin/users/batch-limits', request)
    expect(result).toEqual({ affected: 2 })
    expect(batchRequestContractExact).toBe(true)
    expect(batchResponseContractExact).toBe(true)
  })
})

describe('admin users API list filters', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
  })

  it('sends user_id as an exact list filter', async () => {
    get.mockResolvedValue({
      data: {
        items: [],
        total: 0,
        page: 1,
        page_size: 20,
        pages: 0,
      },
    })

    await listUsers(1, 20, { user_id: 42 })

    expect(get).toHaveBeenCalledWith(
      '/admin/users',
      expect.objectContaining({
        params: expect.objectContaining({ user_id: 42 }),
      })
    )
  })

  it('sends effective VIP and explicit mode filters without deriving one from the other', async () => {
    get.mockResolvedValue({
      data: {
        items: [],
        total: 0,
        page: 1,
        page_size: 20,
        pages: 0,
      },
    })

    await listUsers(1, 20, { is_vip: false, vip_mode: 'FORCE_OFF' })

    expect(get).toHaveBeenCalledWith(
      '/admin/users',
      expect.objectContaining({
        params: expect.objectContaining({
          is_vip: false,
          vip_mode: 'FORCE_OFF',
        }),
      })
    )
  })
})

describe('admin VIP entitlement API', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
  })

  it('updates the explicit mode with a required audit reason', async () => {
    const response = { id: 9, vip_mode: 'FORCE_OFF', is_vip: false }
    put.mockResolvedValue({ data: response })

    await expect(updateVIPMode(9, {
      vip_mode: 'FORCE_OFF',
      vip_override_reason: 'risk review',
    })).resolves.toEqual(response)

    expect(put).toHaveBeenCalledWith('/admin/users/9/vip-mode', {
      vip_mode: 'FORCE_OFF',
      vip_override_reason: 'risk review',
    })
  })

  it('loads newest-first audit pagination and the target-user catalog endpoints', async () => {
    get
      .mockResolvedValueOnce({
        data: { items: [], total: 0, page: 2, page_size: 10, pages: 0 },
      })
      .mockResolvedValueOnce({ data: [] })

    await getVIPAudit(9, 2, 10)
    await getGroupCatalog(9)

    expect(get).toHaveBeenNthCalledWith(
      1,
      '/admin/users/9/vip-audit',
      { params: { page: 2, page_size: 10 } }
    )
    expect(get).toHaveBeenNthCalledWith(2, '/admin/users/9/group-catalog')
  })
})

describe('admin VIP reconciliation API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('passes the opaque preview cursor through unchanged with the selected limit', async () => {
    const response = {
      as_of: '2026-07-29T12:00:00Z',
      total: 1,
      stats: {
        eligibility_repair: 1,
        effective_change: 1,
        force_off_unchanged: 0,
        invalid_order: 0,
        deleted_user: 0,
      },
      items: [],
      next_cursor: 'opaque-next-cursor',
    }
    get.mockResolvedValue({ data: response })

    await expect(getVIPReconcilePreview('opaque-current-cursor', 100)).resolves.toEqual(response)

    expect(get).toHaveBeenCalledWith('/admin/users/vip-reconcile/preview', {
      params: {
        cursor: 'opaque-current-cursor',
        limit: 100,
      },
    })
  })

  it('omits an empty initial cursor while retaining the default page size', async () => {
    get.mockResolvedValue({
      data: {
        as_of: '2026-07-29T12:00:00Z',
        total: 0,
        stats: {
          eligibility_repair: 0,
          effective_change: 0,
          force_off_unchanged: 0,
          invalid_order: 0,
          deleted_user: 0,
        },
        items: [],
        next_cursor: '',
      },
    })

    await getVIPReconcilePreview()

    expect(get).toHaveBeenCalledWith('/admin/users/vip-reconcile/preview', {
      params: {
        cursor: undefined,
        limit: 50,
      },
    })
  })

  it('creates and loads a resumable reconciliation job with the backend contract', async () => {
    const job = {
      id: 17,
      request_id: 'request-17',
      actor_user_id: 1,
      actor_snapshot: 'admin@example.com',
      reason: 'repair historical drift',
      status: 'queued',
      as_of: '2026-07-29T12:00:00Z',
      cursor_completed_at: '0001-01-01T00:00:00Z',
      cursor_order_id: 0,
      scanned: 0,
      eligibility_repaired: 0,
      effective_changed: 0,
      force_off_unchanged: 0,
      already_correct: 0,
      deleted: 0,
      invalid_order: 0,
      failed: 0,
      attempts: 0,
      last_error: '',
      started_at: null,
      finished_at: null,
      created_at: '2026-07-29T12:00:00Z',
      updated_at: '2026-07-29T12:00:00Z',
    }
    post.mockResolvedValue({ data: { job_id: 17, job } })
    get.mockResolvedValue({ data: { ...job, status: 'running', attempts: 1 } })

    await expect(createVIPReconcileJob({
      request_id: 'request-17',
      reason: 'repair historical drift',
    })).resolves.toEqual({ job_id: 17, job })
    await expect(getVIPReconcileJob(17)).resolves.toEqual({
      ...job,
      status: 'running',
      attempts: 1,
    })

    expect(post).toHaveBeenCalledWith('/admin/users/vip-reconcile/jobs', {
      request_id: 'request-17',
      reason: 'repair historical drift',
    })
    expect(get).toHaveBeenCalledWith('/admin/users/vip-reconcile/jobs/17')
  })
})
