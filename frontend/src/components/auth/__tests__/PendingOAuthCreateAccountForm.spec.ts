import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import PendingOAuthCreateAccountForm from '../PendingOAuthCreateAccountForm.vue'

const sendVerifyCode = vi.fn()
const sendPendingOAuthVerifyCode = vi.fn()
const getPublicSettings = vi.fn()
const showError = vi.fn()

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/api/auth')>('@/api/auth')
  return {
    ...actual,
    sendVerifyCode: (...args: any[]) => sendVerifyCode(...args),
    sendPendingOAuthVerifyCode: (...args: any[]) => sendPendingOAuthVerifyCode(...args),
    getPublicSettings: (...args: any[]) => getPublicSettings(...args)
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError
  })
}))

describe('PendingOAuthCreateAccountForm', () => {
  beforeEach(() => {
    sendVerifyCode.mockReset()
    sendPendingOAuthVerifyCode.mockReset()
    getPublicSettings.mockReset()
    showError.mockReset()
    sessionStorage.clear()
    getPublicSettings.mockResolvedValue({
      turnstile_enabled: false,
      turnstile_site_key: ''
    })
  })

  it('emits trimmed email, password, and verify code on submit', async () => {
    const wrapper = mount(PendingOAuthCreateAccountForm, {
      props: {
        providerName: 'LinuxDo',
        testIdPrefix: 'linuxdo',
        initialEmail: 'prefill@example.com',
        isSubmitting: false
      }
    })

    await wrapper.get('[data-testid="linuxdo-create-account-email"]').setValue('  user@example.com  ')
    await wrapper.get('[data-testid="linuxdo-create-account-password"]').setValue('secret-123')
    await wrapper.get('[data-testid="linuxdo-create-account-verify-code"]').setValue(' 246810 ')
    await wrapper.get('form').trigger('submit.prevent')

    expect(wrapper.emitted('submit')).toEqual([
      [
        {
          email: 'user@example.com',
          password: 'secret-123',
          verifyCode: '246810'
        }
      ]
    ])
  })

  it('renders action labels through i18n keys', () => {
    const wrapper = mount(PendingOAuthCreateAccountForm, {
      props: {
        testIdPrefix: 'linuxdo',
        initialEmail: '',
        isSubmitting: false
      }
    })

    expect(wrapper.text()).toContain('auth.createAccount')
    expect(wrapper.text()).toContain('auth.alreadyHaveAccount')
  })

  it('hides email verification controls when public settings disable email verification', async () => {
    getPublicSettings.mockResolvedValue({
      email_verify_enabled: false,
      turnstile_enabled: false,
      turnstile_site_key: ''
    })

    const wrapper = mount(PendingOAuthCreateAccountForm, {
      props: {
        testIdPrefix: 'linuxdo',
        initialEmail: 'prefill@example.com',
        isSubmitting: false
      }
    })

    await flushPromises()
    await wrapper.get('[data-testid="linuxdo-create-account-password"]').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')

    expect(wrapper.find('[data-testid="linuxdo-create-account-verify-code"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="linuxdo-create-account-send-code"]').exists()).toBe(false)
    expect(wrapper.emitted('submit')).toEqual([
      [
        {
          email: 'prefill@example.com',
          password: 'secret-123',
          verifyCode: ''
        }
      ]
    ])
  })

  it('shows and emits invitation code when invitation-only signup is enabled', async () => {
    getPublicSettings.mockResolvedValue({
      invitation_code_enabled: true,
      email_verify_enabled: true,
      turnstile_enabled: false,
      turnstile_site_key: ''
    })

    const wrapper = mount(PendingOAuthCreateAccountForm, {
      props: {
        providerName: 'LinuxDo',
        testIdPrefix: 'linuxdo',
        initialEmail: 'prefill@example.com',
        isSubmitting: false
      }
    })

    await flushPromises()
    await wrapper.get('[data-testid="linuxdo-create-account-password"]').setValue('secret-123')
    await wrapper.get('[data-testid="linuxdo-create-account-verify-code"]').setValue('246810')
    await wrapper.get('[data-testid="linuxdo-create-account-invitation-code"]').setValue(' INVITE123 ')
    await wrapper.get('form').trigger('submit.prevent')

    expect(wrapper.emitted('submit')).toEqual([
      [
        {
          email: 'prefill@example.com',
          password: 'secret-123',
          verifyCode: '246810',
          invitationCode: 'INVITE123'
        }
      ]
    ])
  })

  it('lets a stored referral code satisfy the required invitation gate', async () => {
    // A referral code captured during the OAuth /start hop lives in sessionStorage
    // and is attached to the create-account request; the backend accepts it in
    // place of an invitation code, so the button must not stay disabled.
    sessionStorage.setItem('oauth_aff_code', 'AFF123')
    getPublicSettings.mockResolvedValue({
      invitation_code_enabled: true,
      invitation_code_required: true,
      affiliate_enabled: true,
      email_verify_enabled: false,
      turnstile_enabled: false,
      turnstile_site_key: ''
    })

    const wrapper = mount(PendingOAuthCreateAccountForm, {
      props: {
        providerName: 'LinuxDo',
        testIdPrefix: 'linuxdo',
        initialEmail: 'referred@example.com',
        isSubmitting: false
      }
    })

    await flushPromises()
    await wrapper.get('[data-testid="linuxdo-create-account-password"]').setValue('secret-123')
    const submit = wrapper.get('[data-testid="linuxdo-create-account-submit"]')

    expect(wrapper.text()).toContain('auth.invitationSatisfiedByAffiliate')
    expect(submit.attributes('disabled')).toBeUndefined()
  })

  it('still requires an invitation code when required and no referral code is stored', async () => {
    getPublicSettings.mockResolvedValue({
      invitation_code_enabled: true,
      invitation_code_required: true,
      affiliate_enabled: true,
      email_verify_enabled: false,
      turnstile_enabled: false,
      turnstile_site_key: ''
    })

    const wrapper = mount(PendingOAuthCreateAccountForm, {
      props: {
        providerName: 'LinuxDo',
        testIdPrefix: 'linuxdo',
        initialEmail: 'referred@example.com',
        isSubmitting: false
      }
    })

    await flushPromises()
    await wrapper.get('[data-testid="linuxdo-create-account-password"]').setValue('secret-123')
    const submit = wrapper.get('[data-testid="linuxdo-create-account-submit"]')

    expect(submit.attributes('disabled')).toBeDefined()
  })

  it('allows an empty invitation code when invitation signup is optional', async () => {
    getPublicSettings.mockResolvedValue({
      invitation_code_enabled: true,
      invitation_code_required: false,
      email_verify_enabled: false,
      turnstile_enabled: false,
      turnstile_site_key: ''
    })

    const wrapper = mount(PendingOAuthCreateAccountForm, {
      props: {
        providerName: 'LinuxDo',
        testIdPrefix: 'linuxdo',
        initialEmail: 'optional@example.com',
        isSubmitting: false
      }
    })

    await flushPromises()
    await wrapper.get('[data-testid="linuxdo-create-account-password"]').setValue('secret-123')
    const submit = wrapper.get('[data-testid="linuxdo-create-account-submit"]')

    expect(wrapper.text()).toContain('common.optional')
    expect(submit.attributes('disabled')).toBeUndefined()

    await submit.trigger('click')
    expect(wrapper.emitted('submit')).toEqual([
      [
        {
          email: 'optional@example.com',
          password: 'secret-123',
          verifyCode: '',
          invitationCode: undefined
        }
      ]
    ])
  })

  it('sends a verify code for the trimmed email value', async () => {
    sendPendingOAuthVerifyCode.mockResolvedValue({
      message: 'sent',
      countdown: 60
    })

    const wrapper = mount(PendingOAuthCreateAccountForm, {
      props: {
        providerName: 'LinuxDo',
        testIdPrefix: 'linuxdo',
        initialEmail: '',
        isSubmitting: false
      }
    })

    await wrapper.get('[data-testid="linuxdo-create-account-email"]').setValue('  user@example.com  ')
    await wrapper.get('[data-testid="linuxdo-create-account-send-code"]').trigger('click')
    await flushPromises()

    expect(sendPendingOAuthVerifyCode).toHaveBeenCalledWith({
      email: 'user@example.com'
    })
  })

  it('shows send-code failures via toast without rendering inline error text', async () => {
    sendPendingOAuthVerifyCode.mockRejectedValue(new Error('send failed'))

    const wrapper = mount(PendingOAuthCreateAccountForm, {
      props: {
        testIdPrefix: 'linuxdo',
        initialEmail: '',
        isSubmitting: false
      }
    })

    await wrapper.get('[data-testid="linuxdo-create-account-email"]').setValue('user@example.com')
    await wrapper.get('[data-testid="linuxdo-create-account-send-code"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('send failed')
    expect(wrapper.text()).not.toContain('send failed')
  })

  it('requires a turnstile token before sending a verify code when turnstile is enabled', async () => {
    getPublicSettings.mockResolvedValue({
      turnstile_enabled: true,
      turnstile_site_key: 'site-key'
    })
    sendPendingOAuthVerifyCode.mockResolvedValue({
      message: 'sent',
      countdown: 60
    })

    const wrapper = mount(PendingOAuthCreateAccountForm, {
      props: {
        providerName: 'LinuxDo',
        testIdPrefix: 'linuxdo',
        initialEmail: '',
        isSubmitting: false
      },
      global: {
        stubs: {
          TurnstileWidget: {
            template: '<button data-testid="turnstile-verify" @click="$emit(\'verify\', \'turnstile-token\')">verify</button>'
          }
        }
      }
    })

    await flushPromises()
    await wrapper.get('[data-testid="linuxdo-create-account-email"]').setValue('  user@example.com  ')

    expect(wrapper.get('[data-testid="linuxdo-create-account-send-code"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="turnstile-verify"]').trigger('click')
    await wrapper.get('[data-testid="linuxdo-create-account-send-code"]').trigger('click')
    await flushPromises()

    expect(sendPendingOAuthVerifyCode).toHaveBeenCalledWith({
      email: 'user@example.com',
      turnstile_token: 'turnstile-token'
    })
  })
})
