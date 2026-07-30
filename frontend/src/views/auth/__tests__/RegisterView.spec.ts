import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RegisterView from '@/views/auth/RegisterView.vue'

const {
  routeState,
  pushMock,
  registerMock,
  showSuccessMock,
  showErrorMock,
  showWarningMock,
  getPublicSettingsMock,
  appStoreState,
  validateAffiliateCodeMock,
  validateInvitationCodeMock,
  validatePromoCodeMock
} = vi.hoisted(() => ({
  routeState: { query: {} as Record<string, string> },
  pushMock: vi.fn(),
  registerMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
  showWarningMock: vi.fn(),
  getPublicSettingsMock: vi.fn(),
  appStoreState: {
    siteName: 'Sub2API'
  },
  validateAffiliateCodeMock: vi.fn(),
  validateInvitationCodeMock: vi.fn(),
  validatePromoCodeMock: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock }),
  useRoute: () => routeState
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: {
      t: (key: string) => key
    }
  }),
  useI18n: () => ({
    t: (key: string, params?: Record<string, string>) => {
      if (key === 'auth.signUpToStart') {
        return `Sign up to start using ${params?.siteName}`
      }
      return key
    },
    locale: { value: 'en' }
  })
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    register: (...args: unknown[]) => registerMock(...args)
  }),
  useAppStore: () => ({
    siteName: appStoreState.siteName,
    fetchPublicSettings: (...args: unknown[]) => getPublicSettingsMock(...args),
    showSuccess: (...args: unknown[]) => showSuccessMock(...args),
    showError: (...args: unknown[]) => showErrorMock(...args),
    showWarning: (...args: unknown[]) => showWarningMock(...args)
  })
}))

vi.mock('@/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/api/auth')>('@/api/auth')
  return {
    ...actual,
    getPublicSettings: (...args: unknown[]) => getPublicSettingsMock(...args),
    validateAffiliateCode: (...args: unknown[]) => validateAffiliateCodeMock(...args),
    validateInvitationCode: (...args: unknown[]) => validateInvitationCodeMock(...args),
    validatePromoCode: (...args: unknown[]) => validatePromoCodeMock(...args),
    isWeChatWebOAuthEnabled: () => false
  }
})

function publicSettings(overrides: Record<string, unknown> = {}) {
  return {
    registration_enabled: true,
    email_verify_enabled: false,
    promo_code_enabled: false,
    invitation_code_enabled: false,
    invitation_code_required: true,
    affiliate_enabled: true,
    turnstile_enabled: false,
    turnstile_site_key: '',
    site_name: 'Sub2API',
    linuxdo_oauth_enabled: false,
    wechat_oauth_enabled: false,
    oidc_oauth_enabled: false,
    oidc_oauth_provider_name: 'OIDC',
    github_oauth_enabled: false,
    google_oauth_enabled: false,
    registration_email_suffix_blacklist: [],
    login_agreement_enabled: false,
    login_agreement_documents: [],
    ...overrides
  }
}

function mountRegisterView() {
  return mount(RegisterView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        Icon: true,
        TurnstileWidget: { template: '<div data-testid="turnstile-widget" />' },
        LoginAgreementPrompt: true,
        EmailOAuthButtons: true,
        LinuxDoOAuthSection: true,
        WechatOAuthSection: true,
        OidcOAuthSection: true,
        RouterLink: { template: '<a><slot /></a>' },
        transition: false
      }
    }
  })
}

describe('RegisterView affiliate referral code', () => {
  beforeEach(() => {
    routeState.query = {}
    pushMock.mockReset()
    registerMock.mockReset()
    showSuccessMock.mockReset()
    showErrorMock.mockReset()
    showWarningMock.mockReset()
    getPublicSettingsMock.mockReset()
    appStoreState.siteName = 'Sub2API'
    validateAffiliateCodeMock.mockReset()
    validateInvitationCodeMock.mockReset()
    validatePromoCodeMock.mockReset()
    localStorage.clear()
    sessionStorage.clear()

    getPublicSettingsMock.mockResolvedValue(publicSettings())
    validateAffiliateCodeMock.mockResolvedValue({ valid: true })
    validateInvitationCodeMock.mockResolvedValue({ valid: true })
    validatePromoCodeMock.mockResolvedValue({ valid: true })
    registerMock.mockResolvedValue({})
  })

  it('renders the app-store site name on the first render', () => {
    appStoreState.siteName = 'Acme Gateway'
    getPublicSettingsMock.mockReturnValue(new Promise(() => undefined))

    const wrapper = mountRegisterView()

    expect(wrapper.text()).toContain('Sign up to start using Acme Gateway')
    expect(wrapper.text()).not.toContain('Sub2API')
  })

  it('shows and validates a referral code restored from the URL', async () => {
    routeState.query = { aff: 'AFF123' }

    const wrapper = mountRegisterView()
    await flushPromises()

    expect(wrapper.get('#aff_code').element).toHaveProperty('value', 'AFF123')
    expect(validateAffiliateCodeMock).toHaveBeenCalledWith('AFF123')
    expect(wrapper.text()).toContain('auth.affiliateCodeValid')
    expect(localStorage.getItem('affiliate_referral_code')).toContain('AFF123')
  })

  it('hides the referral field when affiliate rewards are disabled', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({ affiliate_enabled: false }))

    const wrapper = mountRegisterView()
    await flushPromises()

    expect(wrapper.find('#aff_code').exists()).toBe(false)
  })

  it('blocks registration for an email domain on the blacklist', async () => {
    getPublicSettingsMock.mockResolvedValue(
      publicSettings({
        registration_email_suffix_blacklist: ['@nimail.cn', '*.edu.cn']
      })
    )

    const wrapper = mountRegisterView()
    await flushPromises()
    await wrapper.get('#email').setValue('blocked@nimail.cn')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('auth.emailSuffixNotAllowed')
  })

  it('allows a validated referral code to satisfy the invitation requirement', async () => {
    routeState.query = { aff_code: 'AFF123' }
    getPublicSettingsMock.mockResolvedValue(
      publicSettings({ invitation_code_enabled: true })
    )

    const wrapper = mountRegisterView()
    await flushPromises()
    await wrapper.get('#email').setValue('invitee@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).toHaveBeenCalledWith({
      email: 'invitee@example.com',
      password: 'secret-123',
      turnstile_token: undefined,
      promo_code: undefined,
      invitation_code: undefined,
      aff_code: 'AFF123'
    })
    expect(pushMock).toHaveBeenCalledWith('/dashboard')
    expect(localStorage.getItem('affiliate_referral_code')).toBeNull()
  })

  it('disables native form validation so the affiliate-only path is not blocked', async () => {
    getPublicSettingsMock.mockResolvedValue(
      publicSettings({ invitation_code_enabled: true })
    )

    const wrapper = mountRegisterView()
    await flushPromises()

    // Without novalidate the browser blocks submit on the empty required
    // invitation input before validateForm can accept the affiliate code.
    expect(wrapper.get('form').attributes('novalidate')).toBeDefined()
  })

  it('allows registration without an invitation code when the field is optional', async () => {
    getPublicSettingsMock.mockResolvedValue(
      publicSettings({
        invitation_code_enabled: true,
        invitation_code_required: false
      })
    )

    const wrapper = mountRegisterView()
    await flushPromises()
    await wrapper.get('#email').setValue('optional-invite@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.text()).toContain('common.optional')
    expect(registerMock).toHaveBeenCalledWith({
      email: 'optional-invite@example.com',
      password: 'secret-123',
      turnstile_token: undefined,
      promo_code: undefined,
      invitation_code: undefined,
      aff_code: undefined
    })
  })

  it('still rejects a supplied invalid invitation code when the field is optional', async () => {
    getPublicSettingsMock.mockResolvedValue(
      publicSettings({
        invitation_code_enabled: true,
        invitation_code_required: false
      })
    )
    validateInvitationCodeMock.mockResolvedValue({
      valid: false,
      error_code: 'INVITATION_CODE_INVALID'
    })

    const wrapper = mountRegisterView()
    await flushPromises()
    await wrapper.get('#email').setValue('invalid-invite@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#invitation_code').setValue('BADCODE')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(validateInvitationCodeMock).toHaveBeenCalledWith('BADCODE')
    expect(registerMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('auth.invitationCodeInvalid')
  })

  it('blocks registration when a supplied referral code is invalid', async () => {
    routeState.query = { aff: 'BADCODE' }
    validateAffiliateCodeMock.mockResolvedValue({
      valid: false,
      error_code: 'AFFILIATE_CODE_INVALID'
    })

    const wrapper = mountRegisterView()
    await flushPromises()

    expect(wrapper.text()).toContain('auth.affiliateCodeInvalid')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    expect(registerMock).not.toHaveBeenCalled()
    // An invalid code must not linger in localStorage, where it would otherwise be
    // carried into a later OAuth start URL for 30 days.
    expect(localStorage.getItem('affiliate_referral_code')).toBeNull()
  })

  it('does not persist an unvalidated code on each keystroke', async () => {
    // Validation is debounced; before it resolves, nothing should be stored.
    const wrapper = mountRegisterView()
    await flushPromises()

    await wrapper.get('#aff_code').setValue('TYPING')
    expect(localStorage.getItem('affiliate_referral_code')).toBeNull()
  })

  it('keeps the optional affiliate invitation field before Turnstile', async () => {
    getPublicSettingsMock.mockResolvedValue(
      publicSettings({
        turnstile_enabled: true,
        turnstile_site_key: 'site-key'
      })
    )

    const wrapper = mountRegisterView()
    await flushPromises()

    const invitationField = wrapper.get('[data-testid="affiliate-invitation-field"]')
    const turnstile = wrapper.get('[data-testid="registration-turnstile"]')

    expect(invitationField.get('input').attributes('id')).toBe('aff_code')
    expect(invitationField.text()).toContain('common.optional')
    expect(
      invitationField.element.compareDocumentPosition(turnstile.element) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  it('keeps invitation and affiliate referral codes as distinct fields when both are enabled', async () => {
    getPublicSettingsMock.mockResolvedValue(
      publicSettings({ invitation_code_enabled: true })
    )

    const wrapper = mountRegisterView()
    await flushPromises()

    expect(wrapper.get('#invitation_code').exists()).toBe(true)
    expect(wrapper.get('[data-testid="affiliate-invitation-field"] #aff_code').exists()).toBe(true)
  })
})
