import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AccountProviderIdentityPanel from '../AccountProviderIdentityPanel.vue'
import type { Account } from '@/types'
import type { StepUpController } from '@/composables/useStepUp'

const api = vi.hoisted(() => ({
  getProviderIdentity: vi.fn(),
  proposeProviderIdentity: vi.fn(),
  decideProviderIdentity: vi.fn(),
  revokeProviderIdentity: vi.fn()
}))
const notifications = vi.hoisted(() => ({ showError: vi.fn(), showSuccess: vi.fn() }))

vi.mock('@/api/admin', () => ({ adminAPI: { accounts: api } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => notifications }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 7 } }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/auth/TotpStepUpDialog.vue', () => ({
  default: { name: 'TotpStepUpDialog', props: ['controller'], template: '<div />' }
}))

const account = {
  id: 42,
  name: 'Dedicated OpenAI',
  platform: 'openai',
  type: 'apikey',
  ownership_mode: 'user_dedicated',
  owner_user_id: 100,
  isolation_state: 'unverified',
  provider_identity_version: 3
} as Account

describe('AccountProviderIdentityPanel', () => {
  beforeEach(() => {
    sessionStorage.clear()
    vi.clearAllMocks()
    api.getProviderIdentity.mockResolvedValue({ account_id: 42, identity_version: 3, isolation_state: 'unverified', reviews: [] })
  })

  it('submits the principal once, clears it after success, and never renders it back', async () => {
    api.proposeProviderIdentity.mockResolvedValue({
      state: {
        account_id: 42,
        identity_version: 3,
        isolation_state: 'unverified',
        reviews: [{
          id: 9, account_id: 42, account_identity_version: 3, platform: 'openai', principal_kind: 'project',
          issuer_fingerprint: 'aaaaaaaaaaaaaaaa', principal_fingerprint: 'bbbbbbbbbbbbbbbb', status: 'pending',
          proposed_by: 7, reason: 'Provider console identity verified', evidence_ref: 'ticket:IDENTITY-1', created_at: '2026-09-05T00:00:00Z'
        }]
      },
      replayed: false
    })
    const wrapper = mount(AccountProviderIdentityPanel, { props: { account } })
    await flushPromises()

    await wrapper.get('[data-testid="provider-identity-principal"]').setValue('proj_sensitive-123')
    await wrapper.get('[data-testid="provider-identity-evidence"]').setValue('ticket:IDENTITY-1')
    await wrapper.get('[data-testid="provider-identity-reason"]').setValue('Provider console identity verified')
    await wrapper.get('[data-testid="provider-identity-propose"]').trigger('click')
    await flushPromises()

    expect(api.proposeProviderIdentity).toHaveBeenCalledWith(42, 3, {
      principal_kind: 'project', principal: 'proj_sensitive-123', reason: 'Provider console identity verified', evidence_ref: 'ticket:IDENTITY-1'
    }, expect.stringContaining('provider-identity-42-propose-'))
    expect((wrapper.get('[data-testid="provider-identity-principal"]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.html()).not.toContain('proj_sensitive-123')
    expect(wrapper.text()).toContain('bbbbbbbbbbbbbbbb')
  })

  it('prevents the proposer from approving their own pending review', async () => {
    api.getProviderIdentity.mockResolvedValue({
      account_id: 42,
      identity_version: 3,
      isolation_state: 'unverified',
      reviews: [{
        id: 4, account_id: 42, account_identity_version: 3, platform: 'openai', principal_kind: 'project',
        issuer_fingerprint: 'aaaaaaaaaaaaaaaa', principal_fingerprint: 'bbbbbbbbbbbbbbbb', status: 'pending',
        proposed_by: 7, reason: 'Verified', evidence_ref: 'ticket:1', created_at: '2026-09-05T00:00:00Z'
      }]
    })
    const wrapper = mount(AccountProviderIdentityPanel, { props: { account } })
    await flushPromises()
    const approve = wrapper.get('[data-testid="provider-identity-approve-4"]')
    expect((approve.element as HTMLButtonElement).disabled).toBe(true)
  })

  it.each(['verify', 'cancel'] as const)('handles step-up %s without losing the proposal idempotency key', async (action) => {
    api.proposeProviderIdentity.mockRejectedValueOnce({ code: 'STEP_UP_REQUIRED' })
    api.proposeProviderIdentity.mockResolvedValueOnce({
      state: { account_id: 42, identity_version: 3, isolation_state: 'unverified', reviews: [] }, replayed: false
    })
    const wrapper = mount(AccountProviderIdentityPanel, { props: { account } })
    await flushPromises()
    await wrapper.get('[data-testid="provider-identity-principal"]').setValue('proj_stepup')
    await wrapper.get('[data-testid="provider-identity-evidence"]').setValue('ticket:stepup')
    await wrapper.get('[data-testid="provider-identity-reason"]').setValue('Verify provider identity')
    await wrapper.get('[data-testid="provider-identity-propose"]').trigger('click')
    await flushPromises()
    const controller = wrapper.getComponent({ name: 'TotpStepUpDialog' }).props('controller') as StepUpController
    expect(controller.visible.value).toBe(true)
    const originalCall = api.proposeProviderIdentity.mock.calls[0]
    if (action === 'verify') {
      controller.onVerified()
      await flushPromises()
      expect(api.proposeProviderIdentity).toHaveBeenCalledTimes(2)
      expect(api.proposeProviderIdentity.mock.calls[1]).toEqual(originalCall)
    } else {
      controller.onCancel()
      await flushPromises()
      expect(api.proposeProviderIdentity).toHaveBeenCalledTimes(1)
      expect(sessionStorage.getItem('sub2api:provider-identity:42:propose')).toBe(originalCall[3])
      expect((wrapper.get('[data-testid="provider-identity-propose"]').element as HTMLButtonElement).disabled).toBe(false)
    }
    expect(notifications.showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['approve', 'reject', 'revoke'] as const)('retries %s through step-up with unchanged arguments', async (action) => {
    const state = {
      account_id: 42, identity_version: 3,
      isolation_state: action === 'revoke' ? 'verified' : 'unverified',
      reviews: [{
        id: 4, account_id: 42, account_identity_version: 3, platform: 'openai', principal_kind: 'project',
        issuer_fingerprint: 'aaaaaaaaaaaaaaaa', principal_fingerprint: 'bbbbbbbbbbbbbbbb', status: 'pending',
        proposed_by: 8, reason: 'Verified', evidence_ref: 'ticket:1', created_at: '2026-09-05T00:00:00Z'
      }]
    }
    api.getProviderIdentity.mockResolvedValue(state)
    const mutation = action === 'revoke' ? api.revokeProviderIdentity : api.decideProviderIdentity
    mutation.mockRejectedValueOnce({ code: 'STEP_UP_REQUIRED' }).mockResolvedValueOnce({ state, replayed: false })
    const wrapper = mount(AccountProviderIdentityPanel, { props: { account } })
    await flushPromises()
    if (action === 'revoke') {
      await wrapper.get('[data-testid="provider-identity-revoke-evidence"]').setValue('incident:1')
      await wrapper.get('[data-testid="provider-identity-revoke-reason"]').setValue('Compromised identity')
      await wrapper.get('[data-testid="provider-identity-revoke"]').trigger('click')
    } else {
      await wrapper.get('input[placeholder="admin.accounts.providerIdentity.decisionPlaceholder"]').setValue('Independent verification')
      await wrapper.get(`[data-testid="provider-identity-${action}-4"]`).trigger('click')
    }
    await flushPromises()
    const controller = wrapper.getComponent({ name: 'TotpStepUpDialog' }).props('controller') as StepUpController
    expect(controller.visible.value).toBe(true)
    expect(mutation).toHaveBeenCalledTimes(1)
    controller.onVerified()
    await flushPromises()
    expect(mutation).toHaveBeenCalledTimes(2)
    expect(mutation.mock.calls[1]).toEqual(mutation.mock.calls[0])
    expect(notifications.showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
