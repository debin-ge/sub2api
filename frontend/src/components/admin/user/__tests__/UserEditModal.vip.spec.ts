import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { AdminUser } from '@/types'
import UserEditModal from '../UserEditModal.vue'

const {
  updateUser,
  updateVIPMode,
  updateUserAttributeValues,
  showError,
  showSuccess
} = vi.hoisted(() => ({
  updateUser: vi.fn(),
  updateVIPMode: vi.fn(),
  updateUserAttributeValues: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      update: updateUser,
      updateVIPMode
    },
    userAttributes: {
      updateUserAttributeValues
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn() })
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({
    visible: { value: false },
    blockedReason: { value: '' },
    run: (action: () => Promise<unknown>) => action()
  }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => ''
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key
    })
  }
})

const user = {
  id: 19,
  email: 'vip-admin-target@example.com',
  username: 'target',
  role: 'user',
  balance: 0,
  concurrency: 2,
  rpm_limit: 0,
  status: 'active',
  is_vip: true,
  vip_mode: 'AUTO',
  allowed_groups: [],
  balance_notify_enabled: false,
  balance_notify_threshold: null,
  balance_notify_extra_emails: [],
  created_at: '2026-07-29T00:00:00Z',
  updated_at: '2026-07-29T00:00:00Z',
  notes: ''
} satisfies AdminUser

const mountModal = () => mount(UserEditModal, {
  props: { show: true, user },
  global: {
    stubs: {
      BaseDialog: {
        props: ['show'],
        template: '<div v-if="show"><slot /><slot name="footer" /></div>'
      },
      UserAttributeForm: true,
      TotpStepUpDialog: true,
      Icon: true
    }
  }
})

describe('UserEditModal VIP mode', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    updateUser.mockResolvedValue(user)
    updateVIPMode.mockResolvedValue({ ...user, vip_mode: 'FORCE_OFF', is_vip: false })
    updateUserAttributeValues.mockResolvedValue({})
  })

  it('requires a non-empty reason before changing any VIP mode', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-test="vip-mode-select"]').setValue('FORCE_OFF')
    await wrapper.get('#edit-user-form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.users.vip.reasonRequired')
    expect(updateUser).not.toHaveBeenCalled()
    expect(updateVIPMode).not.toHaveBeenCalled()
  })

  it('calls the dedicated mode endpoint when the mode changes', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-test="vip-mode-select"]').setValue('FORCE_OFF')
    await wrapper.get('[data-test="vip-reason"]').setValue('manual compliance hold')
    await wrapper.get('#edit-user-form').trigger('submit')
    await flushPromises()

    expect(updateUser).toHaveBeenCalledWith(
      19,
      expect.objectContaining({ email: user.email })
    )
    expect(updateVIPMode).toHaveBeenCalledWith(19, {
      vip_mode: 'FORCE_OFF',
      vip_override_reason: 'manual compliance hold'
    })
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('does not create a VIP audit event when ordinary profile fields are saved unchanged', async () => {
    const wrapper = mountModal()
    await wrapper.get('#edit-user-form').trigger('submit')
    await flushPromises()

    expect(updateUser).toHaveBeenCalledTimes(1)
    expect(updateVIPMode).not.toHaveBeenCalled()
  })
})
