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
  validatePromoCodeMock,
  verifyActionMock
} = vi.hoisted(() => ({
  routeState: { query: {} as Record<string, string> },
  pushMock: vi.fn(),
  getPublicSettingsMock: vi.fn(),
  registerMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
  showWarningMock: vi.fn(),
  appStoreState: {
    siteName: 'Sub2API',
    cachedPublicSettings: null as { promo_code_enabled?: boolean } | null
  },
  validateAffiliateCodeMock: vi.fn(),
  validateInvitationCodeMock: vi.fn(),
  validatePromoCodeMock: vi.fn(),
  verifyActionMock: vi.fn()
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
      if (key === 'auth.emailDomainRegistrationLimit') {
        return '该邮箱域名无法注册新账户。请使用主流邮箱注册；如需使用企业邮箱，请联系客服添加域名白名单。'
      }
      if (key === 'auth.signUpToStart') {
        return `Sign up to start using ${params?.siteName}`
      }
      return key
    },
    locale: { value: 'en' }
  })
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({ register: (...args: unknown[]) => registerMock(...args) }),
  useAppStore: () => ({
    siteName: appStoreState.siteName,
    cachedPublicSettings: appStoreState.cachedPublicSettings,
    fetchPublicSettings: (...args: unknown[]) => getPublicSettingsMock(...args),
    showError: (...args: unknown[]) => showErrorMock(...args),
    showSuccess: (...args: unknown[]) => showSuccessMock(...args),
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
    registration_email_suffix_whitelist: [],
    registration_email_domain_quota_enabled: false,
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
        TurnstileWidget: {
          template: '<div data-testid="turnstile-widget" />',
          methods: { verifyAction: verifyActionMock, reset: vi.fn() }
        },
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

describe('RegisterView', () => {
  beforeEach(() => {
    routeState.query = {}
    pushMock.mockReset()
    registerMock.mockReset()
    showSuccessMock.mockReset()
    showErrorMock.mockReset()
    showWarningMock.mockReset()
    getPublicSettingsMock.mockReset()
    verifyActionMock.mockReset()
    appStoreState.siteName = 'Sub2API'
    appStoreState.cachedPublicSettings = null
    validateAffiliateCodeMock.mockReset()
    validateInvitationCodeMock.mockReset()
    validatePromoCodeMock.mockReset()
    localStorage.clear()
    sessionStorage.clear()
    getPublicSettingsMock.mockResolvedValue(publicSettings())
    validateAffiliateCodeMock.mockResolvedValue({ valid: true })
    validateInvitationCodeMock.mockResolvedValue({ valid: true })
    validatePromoCodeMock.mockResolvedValue({ valid: true })
    verifyActionMock.mockResolvedValue({ token: 'ticket', randstr: 'randstr' })
    registerMock.mockResolvedValue({})
  })

  it('does not flash the promo-code field before disabled settings finish loading', async () => {
    let resolveSettings!: (settings: ReturnType<typeof publicSettings>) => void
    getPublicSettingsMock.mockReturnValueOnce(
      new Promise<ReturnType<typeof publicSettings>>((resolve) => {
        resolveSettings = resolve
      })
    )

    const wrapper = mountRegisterView()

    expect(wrapper.find('#promo_code').exists()).toBe(false)

    resolveSettings(publicSettings())
    await flushPromises()

    expect(wrapper.find('#promo_code').exists()).toBe(false)
  })

  it('uses injected public settings to show an enabled promo-code field on first render', () => {
    appStoreState.cachedPublicSettings = { promo_code_enabled: true }
    getPublicSettingsMock.mockReturnValueOnce(new Promise(() => {}))

    const wrapper = mountRegisterView()

    expect(wrapper.find('#promo_code').exists()).toBe(true)
  })

  it.each([
    ['', 'auth.confirmPasswordRequired'],
    ['different-password', 'auth.passwordsDoNotMatch']
  ])('blocks invalid confirmation %j before captcha and allows correction', async (confirmation, error) => {
    getPublicSettingsMock.mockResolvedValueOnce(
      publicSettings({
        turnstile_enabled: false,
        tencent_captcha_enabled: true,
        tencent_captcha_app_id: 'app-id'
      })
    )
    const wrapper = mountRegisterView()
    await flushPromises()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue(confirmation)
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(showErrorMock).toHaveBeenCalledWith(error)
    expect(wrapper.get('#confirmPassword').classes()).toContain('input-error')
    expect(registerMock).not.toHaveBeenCalled()
    expect(verifyActionMock).not.toHaveBeenCalled()
    expect(pushMock).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('register_data')).toBeNull()

    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.get('#confirmPassword').classes()).not.toContain('input-error')
    expect(verifyActionMock).toHaveBeenCalledOnce()
    expect(registerMock).toHaveBeenCalledWith({
      email: 'user@example.com',
      password: 'secret-123',
      turnstile_token: undefined,
      tencent_captcha_ticket: 'ticket',
      tencent_captcha_randstr: 'randstr',
      promo_code: undefined,
      invitation_code: undefined
    })
    expect(pushMock).toHaveBeenCalledWith('/dashboard')
  })

  it('requires matching confirmation before storing only the registration fields for email verification', async () => {
    getPublicSettingsMock.mockResolvedValueOnce(
      publicSettings({
        turnstile_enabled: false,
        email_verify_enabled: true
      })
    )
    const wrapper = mountRegisterView()
    await flushPromises()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('different-password')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(sessionStorage.getItem('register_data')).toBeNull()
    expect(pushMock).not.toHaveBeenCalled()

    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(JSON.parse(sessionStorage.getItem('register_data')!)).toEqual({
      email: 'user@example.com',
      password: 'secret-123'
    })
    expect(pushMock).toHaveBeenCalledWith('/email-verify')
    expect(registerMock).not.toHaveBeenCalled()
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

  it('submits a non-whitelist email domain so the backend can enforce its registration quota', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings(),
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com'],
      registration_email_domain_quota_enabled: true
    })

    const wrapper = mountRegisterView()
    await flushPromises()
    await wrapper.get('#email').setValue('first@custom.example')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).toHaveBeenCalledWith(
      expect.objectContaining({ email: 'first@custom.example' })
    )
    expect(showErrorMock).not.toHaveBeenCalled()
  })

  it('shows the localized registration domain quota message returned by the backend', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings(),
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com'],
      registration_email_domain_quota_enabled: true
    })
    registerMock.mockRejectedValueOnce({
      reason: 'EMAIL_DOMAIN_REGISTRATION_LIMIT',
      message: 'raw backend message'
    })

    const wrapper = mountRegisterView()
    await flushPromises()
    await wrapper.get('#email').setValue('second@custom.example')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(showErrorMock).toHaveBeenCalledWith(
      '该邮箱域名无法注册新账户。请使用主流邮箱注册；如需使用企业邮箱，请联系客服添加域名白名单。'
    )
  })

  // 域名限量注册开关默认关闭：恢复 PR5423 之前的客户端白名单预检，非白名单域名不发起注册请求。
  it('rejects a non-whitelist email domain locally when the domain quota switch is disabled', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings(),
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com']
    })

    const wrapper = mountRegisterView()
    await flushPromises()
    await wrapper.get('#email').setValue('first@custom.example')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).not.toHaveBeenCalled()
    // 校验失败通过 validationToastMessage watcher 弹 toast
    expect(showErrorMock).toHaveBeenCalledWith('auth.emailSuffixNotAllowedWithAllowed')
    expect(wrapper.get('#email').classes()).toContain('input-error')
  })

  it('still submits whitelisted email domains when the domain quota switch is disabled', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings(),
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com']
    })

    const wrapper = mountRegisterView()
    await flushPromises()
    await wrapper.get('#email').setValue('user@allowed.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).toHaveBeenCalledWith(
      expect.objectContaining({ email: 'user@allowed.com' })
    )
    expect(showErrorMock).not.toHaveBeenCalled()
  })
})
