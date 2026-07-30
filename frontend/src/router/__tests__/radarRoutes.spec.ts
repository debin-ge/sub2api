import { beforeEach, describe, expect, it, vi } from 'vitest'

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: false,
  isAdmin: false,
  isSimpleMode: false,
  hasPendingAuthSession: false,
}))

const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API',
  backendModeEnabled: false,
  cachedPublicSettings: null as null | Record<string, unknown>,
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

vi.mock('@/router/title', () => ({
  resolveRouteDocumentTitle: () => 'Sub2API',
}))

describe('Model Radar routes', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.clearAllMocks()
    window.scrollTo = vi.fn()
    window.history.replaceState({}, '', '/')
    authStore.isAuthenticated = false
    authStore.isAdmin = false
    authStore.isSimpleMode = false
    authStore.hasPendingAuthSession = false
    appStore.backendModeEnabled = false
    appStore.cachedPublicSettings = null
  })

  it('registers / as the public Radar homepage and redirects legacy /home visits to it', async () => {
    const [{ default: router }, radarModule] = await Promise.all([
      import('@/router'),
      import('@/views/public/RadarHomeView.vue'),
    ])
    const rootRoute = router.getRoutes().find((record) => record.path === '/')
    const homeRoute = router.getRoutes().find((record) => record.path === '/home')

    expect(rootRoute?.name).toBe('RadarHome')
    expect(rootRoute?.redirect).toBeUndefined()
    expect(rootRoute?.meta).toMatchObject({
      requiresAuth: false,
      title: 'Model Radar',
      titleKey: 'radar.pageTitle',
    })
    expect(homeRoute?.name).toBe('LegacyHomeRedirect')
    expect(homeRoute?.redirect).toBe('/')
    expect(homeRoute?.components?.default).toBeUndefined()

    const loadRoot = rootRoute?.components?.default
    expect(loadRoot).toBeTypeOf('function')
    await expect((loadRoot as () => Promise<unknown>)()).resolves.toMatchObject({
      default: radarModule.default,
    })

    await router.push('/home')
    expect(router.currentRoute.value.name).toBe('RadarHome')
    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('keeps the original /plaza route and does not register /model-plaza', async () => {
    const [{ default: router }, plazaModule] = await Promise.all([
      import('@/router'),
      import('@/views/PlazaView.vue'),
    ])
    const plazaRoute = router.getRoutes().find((record) => record.path === '/plaza')

    expect(router.resolve('/plaza').name).toBe('ModelPlaza')
    expect(plazaRoute?.meta.requiresAuth).toBe(false)
    expect(router.getRoutes().some((record) => record.path === '/model-plaza')).toBe(false)

    const loadPlaza = plazaRoute?.components?.default
    expect(loadPlaza).toBeTypeOf('function')
    await expect((loadPlaza as () => Promise<unknown>)()).resolves.toMatchObject({
      default: plazaModule.default,
    })
  })

  it('allows only the exact root path through the backend-mode root allowlist entry', async () => {
    const { isBackendModePublicRouteAllowed } = await import('@/router')

    expect(isBackendModePublicRouteAllowed('/', false)).toBe(true)
    expect(isBackendModePublicRouteAllowed('/dashboard', false)).toBe(false)
    expect(isBackendModePublicRouteAllowed('/anything', false)).toBe(false)
  })

  it('preserves descendant matching for existing backend-mode allowlist entries', async () => {
    const { isBackendModePublicRouteAllowed } = await import('@/router')

    expect(isBackendModePublicRouteAllowed('/docs', false)).toBe(true)
    expect(isBackendModePublicRouteAllowed('/docs/quickstart', false)).toBe(true)
    expect(isBackendModePublicRouteAllowed('/docs-admin', false)).toBe(false)
    expect(isBackendModePublicRouteAllowed('/legal/privacy', false)).toBe(true)
  })

  describe('registered navigation guard integration', () => {
    it('keeps the unauthenticated backend-mode root navigation on RadarHome', async () => {
      appStore.backendModeEnabled = true
      const { default: router } = await import('@/router')

      await router.push('/')

      expect(router.currentRoute.value.name).toBe('RadarHome')
      expect(router.currentRoute.value.fullPath).toBe('/')
      expect(authStore.checkAuth).toHaveBeenCalledTimes(1)
    })

    it('redirects an authenticated non-admin from /dashboard to /login in backend mode', async () => {
      authStore.isAuthenticated = true
      appStore.backendModeEnabled = true
      const { default: router } = await import('@/router')

      await router.push('/dashboard')

      expect(router.currentRoute.value.name).toBe('Login')
      expect(router.currentRoute.value.fullPath).toBe('/login')
    })

    it('allows the same authenticated non-admin to enter /dashboard outside backend mode', async () => {
      authStore.isAuthenticated = true
      appStore.backendModeEnabled = false
      const { default: router } = await import('@/router')

      await router.push('/dashboard')

      expect(router.currentRoute.value.name).toBe('Dashboard')
      expect(router.currentRoute.value.fullPath).toBe('/dashboard')
    })

    it('preserves the auth-guard redirect query for an unauthenticated /dashboard visit', async () => {
      appStore.backendModeEnabled = true
      const { default: router } = await import('@/router')

      await router.push('/dashboard')

      expect(router.currentRoute.value.name).toBe('Login')
      expect(router.currentRoute.value.query).toEqual({ redirect: '/dashboard' })
    })
  })
})
