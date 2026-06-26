import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import HomeView from '../HomeView.vue'
import { radarAPI } from '@/api/radar'

const fetchPublicSettings = vi.fn()

const appState = {
  cachedPublicSettings: {
    site_name: 'Sub2API',
    site_logo: '',
    site_subtitle: 'AI API Gateway Platform',
    home_content: '',
    benchmark_home_enabled: false,
  },
  siteName: 'Sub2API',
  siteLogo: '',
  publicSettingsLoaded: true,
}

const authState = {
  user: null as null | { email: string; role: string },
  isAuthenticated: false,
  isAdmin: false,
  checkAuth: vi.fn(),
}

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: appState.cachedPublicSettings,
    siteName: appState.siteName,
    siteLogo: appState.siteLogo,
    publicSettingsLoaded: appState.publicSettingsLoaded,
    fetchPublicSettings,
  }),
  useAuthStore: () => authState,
}))

vi.mock('@/api/radar', () => ({
  radarAPI: {
    getCurrent: vi.fn(),
  },
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  const messages: Record<string, string> = {
    'benchmark.public.home.eyebrow': 'HOME_RADAR_EYEBROW',
    'benchmark.public.home.title': 'HOME_RADAR_TITLE',
    'benchmark.public.home.description': 'HOME_RADAR_DESCRIPTION',
    'benchmark.public.home.loadingTitle': 'HOME_RADAR_LOADING_TITLE',
    'benchmark.public.home.loadingDescription': 'HOME_RADAR_LOADING_DESCRIPTION',
    'benchmark.public.home.errorTitle': 'HOME_RADAR_ERROR_TITLE',
    'benchmark.public.home.loadError': 'HOME_RADAR_LOAD_FAILED',
    'benchmark.public.empty.title': 'RADAR_EMPTY_TITLE',
    'benchmark.public.empty.description': 'RADAR_EMPTY_DESCRIPTION',
  }
  const locale = ref('en-GB')
  return {
    ...actual,
    useI18n: () => ({
      locale,
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

function createHomeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: HomeView },
      { path: '/docs', component: { template: '<div>Docs</div>' } },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/dashboard', component: { template: '<div>Dashboard</div>' } },
      { path: '/admin/dashboard', component: { template: '<div>Admin Dashboard</div>' } },
    ],
  })
}

async function mountHome() {
  const router = createHomeRouter()
  router.push('/')
  await router.isReady()

  const wrapper = mount(HomeView, {
    global: {
      plugins: [router],
      stubs: {
        LocaleSwitcher: true,
      },
    },
  })

  await flushPromises()
  return wrapper
}

describe('HomeView Radar home', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })
    appState.cachedPublicSettings = {
      site_name: 'Sub2API',
      site_logo: '',
      site_subtitle: 'AI API Gateway Platform',
      home_content: '',
      benchmark_home_enabled: false,
    }
    appState.publicSettingsLoaded = true
    authState.user = null
    authState.isAuthenticated = false
    authState.isAdmin = false
    vi.mocked(radarAPI.getCurrent).mockResolvedValue({
      ranking_basis: 'ability_score_only',
      latest_run: {
        id: 9,
        suite_id: 1,
        profile_id: 2,
        status: 'completed',
        completed_at: '2026-06-24T08:00:00Z',
      },
      targets: [
        {
          rank: 1,
          model: 'gpt-4o',
          channel_id: 11,
          channel_name: 'OpenAI',
          display_name: 'GPT-4o',
          overall_score: 92.4,
          dimensions: { reasoning: 91, coding: 94 },
          score_basis: {
            planned_tasks: 20,
            scored_tasks: 20,
            invalid_tasks: 0,
            coverage_rate: 1,
            confidence_level: 'high',
            insufficient_sample: false,
          },
          metrics: {
            success_rate: 0.98,
            latency_p50_ms: 890,
            latency_p95_ms: 1640,
            avg_total_tokens: 1460,
            estimated_cost: 0.028,
          },
        },
      ],
    })
  })

  it('renders custom home_content before the Radar home', async () => {
    appState.cachedPublicSettings.home_content = '<section>Custom Home</section>'
    appState.cachedPublicSettings.benchmark_home_enabled = true

    const wrapper = await mountHome()

    expect(wrapper.text()).toContain('Custom Home')
    expect(wrapper.text()).not.toContain('HOME_RADAR_TITLE')
    expect(radarAPI.getCurrent).not.toHaveBeenCalled()
  })

  it('renders the Radar home when home_content is empty and benchmark_home_enabled is true', async () => {
    appState.cachedPublicSettings.benchmark_home_enabled = true

    const wrapper = await mountHome()

    expect(radarAPI.getCurrent).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('HOME_RADAR_EYEBROW')
    expect(wrapper.text()).toContain('HOME_RADAR_TITLE')
    expect(wrapper.text()).toContain('HOME_RADAR_DESCRIPTION')
    expect(wrapper.text()).toContain('GPT-4o')
  })

  it('keeps the legacy home when Radar home is disabled', async () => {
    const wrapper = await mountHome()

    expect(wrapper.text()).toContain('AI API Gateway Platform')
    expect(wrapper.text()).not.toContain('HOME_RADAR_TITLE')
    expect(radarAPI.getCurrent).not.toHaveBeenCalled()
  })

  it('treats whitespace-only home_content as empty before showing the Radar home', async () => {
    appState.cachedPublicSettings.home_content = ' \n\t '
    appState.cachedPublicSettings.benchmark_home_enabled = true

    const wrapper = await mountHome()

    expect(radarAPI.getCurrent).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('HOME_RADAR_TITLE')
    expect(wrapper.text()).toContain('GPT-4o')
    expect(wrapper.text()).not.toContain('Custom Home')
  })

  it('shows a page-level empty state when the Radar snapshot has no targets', async () => {
    appState.cachedPublicSettings.benchmark_home_enabled = true
    vi.mocked(radarAPI.getCurrent).mockResolvedValue({
      ranking_basis: 'ability_score_only',
      latest_run: null,
      targets: [],
    })

    const wrapper = await mountHome()

    expect(wrapper.text()).toContain('RADAR_EMPTY_TITLE')
    expect(wrapper.text()).toContain('RADAR_EMPTY_DESCRIPTION')
    expect(wrapper.text()).not.toContain('参评模型')
    expect(wrapper.text()).not.toContain('能力维度')
  })

  it('uses translated fallback copy when public radar loading fails without an Error object', async () => {
    appState.cachedPublicSettings.benchmark_home_enabled = true
    vi.mocked(radarAPI.getCurrent).mockRejectedValue('boom')

    const wrapper = await mountHome()

    expect(wrapper.text()).toContain('HOME_RADAR_ERROR_TITLE')
    expect(wrapper.text()).toContain('HOME_RADAR_LOAD_FAILED')
  })
})
