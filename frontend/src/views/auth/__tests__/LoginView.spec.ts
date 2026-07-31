import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import LoginView from '@/views/auth/LoginView.vue'

const {
  loginMock,
  loginWithPasskeyMock,
  login2FAMock,
  showSuccessMock,
  showErrorMock,
  showWarningMock,
  getPublicSettingsMock,
  finishAuthenticatedNavigationMock,
  turnstileResetMock,
  routeState
} = vi.hoisted(() => ({
  loginMock: vi.fn(),
  loginWithPasskeyMock: vi.fn(),
  login2FAMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
  showWarningMock: vi.fn(),
  getPublicSettingsMock: vi.fn(),
  finishAuthenticatedNavigationMock: vi.fn(),
  turnstileResetMock: vi.fn(),
  routeState: {
    query: {} as Record<string, string>
  }
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    currentRoute: {
      value: routeState
    }
  })
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: {
      t: (key: string) => key
    }
  }),
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    login: (...args: unknown[]) => loginMock(...args),
    loginWithPasskey: (...args: unknown[]) => loginWithPasskeyMock(...args),
    login2FA: (...args: unknown[]) => login2FAMock(...args)
  }),
  useAppStore: () => ({
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
    isWeChatWebOAuthEnabled: () => false
  }
})

vi.mock('@/utils/authNavigation', () => ({
  finishAuthenticatedNavigation: (...args: unknown[]) =>
    finishAuthenticatedNavigationMock(...args)
}))

const TurnstileStub = defineComponent({
  name: 'TurnstileWidget',
  emits: ['verify'],
  setup(_, { emit, expose }) {
    expose({
      reset: turnstileResetMock
    })

    return () =>
      h(
        'button',
        {
          type: 'button',
          'data-testid': 'turnstile-verify',
          onClick: () => emit('verify', 'turnstile-token')
        },
        'verify'
      )
  }
})

function publicSettings() {
  return {
    turnstile_enabled: true,
    turnstile_site_key: 'site-key',
    linuxdo_oauth_enabled: false,
    dingtalk_oauth_enabled: false,
    wechat_oauth_enabled: false,
    backend_mode_enabled: false,
    oidc_oauth_enabled: false,
    oidc_oauth_provider_name: 'OIDC',
    github_oauth_enabled: false,
    google_oauth_enabled: false,
    password_reset_enabled: false,
    passkey_enabled: false,
    login_agreement_enabled: false,
    login_agreement_documents: []
  }
}

function mountLoginView() {
  return mount(LoginView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        Icon: true,
        TurnstileWidget: TurnstileStub,
        LoginAgreementPrompt: true,
        TotpLoginModal: true,
        EmailOAuthButtons: true,
        LinuxDoOAuthSection: true,
        DingTalkOAuthSection: true,
        WechatOAuthSection: true,
        OidcOAuthSection: true,
        RouterLink: { template: '<a><slot /></a>' }
      }
    }
  })
}

async function completeLoginForm(wrapper: ReturnType<typeof mountLoginView>) {
  await flushPromises()
  await wrapper.get('[data-testid="turnstile-verify"]').trigger('click')
  await wrapper.get('#email').setValue('user@example.com')
  await wrapper.get('#password').setValue('secret-123')
  await wrapper.get('form').trigger('submit.prevent')
  await flushPromises()
}

describe('LoginView authentication and navigation boundaries', () => {
  beforeEach(() => {
    routeState.query = {}
    loginMock.mockReset()
    loginWithPasskeyMock.mockReset()
    login2FAMock.mockReset()
    showSuccessMock.mockReset()
    showErrorMock.mockReset()
    showWarningMock.mockReset()
    getPublicSettingsMock.mockReset()
    finishAuthenticatedNavigationMock.mockReset()
    turnstileResetMock.mockReset()
    localStorage.clear()
    sessionStorage.clear()

    getPublicSettingsMock.mockResolvedValue(publicSettings())
  })

  it('resets Turnstile when the authentication request fails', async () => {
    loginMock.mockRejectedValue(new Error('invalid credentials'))
    const wrapper = mountLoginView()

    await completeLoginForm(wrapper)

    expect(turnstileResetMock).toHaveBeenCalledOnce()
    expect(showErrorMock).toHaveBeenCalledWith('invalid credentials')
    expect(finishAuthenticatedNavigationMock).not.toHaveBeenCalled()
  })

  it('does not reset Turnstile when authenticated navigation fails', async () => {
    loginMock.mockResolvedValue({
      access_token: 'access-token',
      user: { id: 1, email: 'user@example.com' }
    })
    finishAuthenticatedNavigationMock.mockImplementation(
      async (options: {
        onNavigationFailure: (error: unknown) => void
      }) => {
        options.onNavigationFailure(new Error('navigation failed'))
        return 'failed'
      }
    )
    const wrapper = mountLoginView()

    await completeLoginForm(wrapper)

    expect(turnstileResetMock).not.toHaveBeenCalled()
    expect(showErrorMock).not.toHaveBeenCalled()
    expect(showWarningMock).toHaveBeenCalledWith(
      'auth.loginSucceededNavigationFailed'
    )
  })

  it('shows login success only after authenticated navigation succeeds', async () => {
    loginMock.mockResolvedValue({
      access_token: 'access-token',
      user: { id: 1, email: 'user@example.com' }
    })
    finishAuthenticatedNavigationMock.mockImplementation(
      async (options: { onSuccess: () => void }) => {
        expect(showSuccessMock).not.toHaveBeenCalled()
        options.onSuccess()
        return 'completed'
      }
    )
    const wrapper = mountLoginView()

    await completeLoginForm(wrapper)

    expect(showSuccessMock).toHaveBeenCalledWith('auth.loginSuccess')
    expect(showErrorMock).not.toHaveBeenCalled()
    expect(turnstileResetMock).not.toHaveBeenCalled()
  })
})
