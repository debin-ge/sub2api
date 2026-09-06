import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get, post } }))

import {
  decideProviderIdentity,
  getProviderIdentity,
  proposeProviderIdentity,
  revokeProviderIdentity
} from '@/api/admin/accounts'

describe('admin account provider identity API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: { account_id: 42, identity_version: 3, isolation_state: 'unverified', reviews: [] } })
    post.mockResolvedValue({ data: { state: { account_id: 42, identity_version: 3, isolation_state: 'unverified', reviews: [] }, replayed: false } })
  })

  it('uses exact version and idempotency headers without changing the principal payload', async () => {
    const payload = {
      principal_kind: 'project' as const,
      principal: 'proj_abc-123',
      reason: 'Provider console identity verified',
      evidence_ref: 'ticket:IDENTITY-1'
    }

    await getProviderIdentity(42)
    await proposeProviderIdentity(42, 3, payload, 'identity:propose')
    await decideProviderIdentity(42, 7, 3, true, 'Independent verification completed', 'identity:approve')
    await revokeProviderIdentity(42, { reason: 'Provider principal compromised', evidence_ref: 'incident:IDENTITY-1' }, 'identity:revoke')

    expect(get).toHaveBeenCalledWith('/admin/accounts/42/provider-identity')
    expect(post).toHaveBeenNthCalledWith(1, '/admin/accounts/42/provider-identity/reviews', payload, {
      headers: { 'If-Match': '"3"', 'Idempotency-Key': 'identity:propose' }
    })
    expect(post).toHaveBeenNthCalledWith(2, '/admin/accounts/42/provider-identity/reviews/7/approve', {
      reason: 'Independent verification completed'
    }, { headers: { 'If-Match': '"3"', 'Idempotency-Key': 'identity:approve' } })
    expect(post).toHaveBeenNthCalledWith(3, '/admin/accounts/42/provider-identity/revoke', {
      reason: 'Provider principal compromised', evidence_ref: 'incident:IDENTITY-1'
    }, { headers: { 'Idempotency-Key': 'identity:revoke' } })
  })

  it('fails before sending invalid mutation metadata', async () => {
    await expect(proposeProviderIdentity(42, 0, {
      principal_kind: 'project', principal: 'proj_abc', reason: 'verified', evidence_ref: 'ticket:1'
    }, 'identity:propose')).rejects.toThrow('valid provider identity version')
    await expect(revokeProviderIdentity(42, { reason: 'revoked', evidence_ref: 'ticket:1' }, '')).rejects.toThrow('operation key')
    expect(post).not.toHaveBeenCalled()
  })
})
