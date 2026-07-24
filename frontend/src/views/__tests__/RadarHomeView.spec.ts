import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, defineComponent, nextTick, ref, shallowRef, type Component } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type {
  PublicRadarErrorCode,
  RadarResourceState,
  UsePublicRadarReturn,
} from '@/composables/usePublicRadar'
import type {
  DataSourceMetaDTO,
  DegradationLatestDTO,
  LMArenaDTO,
  QuotaRadarLatestDTO,
  QuotaTrendDTO,
  ServiceHealthDTO,
} from '@/types/radar'
import {
  bucket,
  degradationLatest,
  lmarena,
  now,
  quotaTrend,
  service,
  source,
} from '@/components/radar/__tests__/fixtures'

const { usePublicRadarMock } = vi.hoisted(() => ({
  usePublicRadarMock: vi.fn(),
}))
const { getPublicChannelsMock } = vi.hoisted(() => ({
  getPublicChannelsMock: vi.fn(),
}))
const { localeMock } = vi.hoisted(() => ({ localeMock: { value: 'en' } }))
const { useAppStoreMock } = vi.hoisted(() => ({
  useAppStoreMock: vi.fn(() => ({
    cachedPublicSettings: { site_name: 'TikToken' },
    siteName: 'Sub2API',
  })),
}))

vi.mock('@/composables/usePublicRadar', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/usePublicRadar')>()
  return {
    ...actual,
    usePublicRadar: usePublicRadarMock,
  }
})

vi.mock('@/api/channels', () => ({
  default: { getPublic: getPublicChannelsMock },
}))

vi.mock('@/stores', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/stores')>(),
  useAppStore: useAppStoreMock,
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      locale: localeMock,
      t: (key: string, fallback?: string) => (
        key === 'home.footer.allRightsReserved'
          ? 'net is owned by Jerrywell Pte. Ltd.'
          : fallback ?? key
      ),
    }),
  }
})

import RadarHomeView from '@/views/public/RadarHomeView.vue'

interface ResourceOptions {
  loading?: boolean
  error?: PublicRadarErrorCode | null
  hasSucceeded?: boolean
}

function resource<T>(data: T | null, options: ResourceOptions = {}): RadarResourceState<T> {
  return {
    data: shallowRef(data),
    loading: ref(options.loading ?? false),
    error: ref(options.error ?? null),
    hasSucceeded: ref(options.hasSucceeded ?? data !== null),
  }
}

interface RadarFixtureOptions {
  health?: RadarResourceState<ServiceHealthDTO[]>
  quotaLatest?: RadarResourceState<QuotaRadarLatestDTO>
  degradationLatest?: RadarResourceState<DegradationLatestDTO>
  lmarena?: RadarResourceState<LMArenaDTO>
  sources?: RadarResourceState<DataSourceMetaDTO[]>
  hasCompletedRefresh?: boolean
  refresh?: ReturnType<typeof vi.fn>
  quotaTrendState?: RadarResourceState<QuotaTrendDTO>
}

function makeRadar(options: RadarFixtureOptions = {}): UsePublicRadarReturn {
  const health = options.health ?? resource<ServiceHealthDTO[]>([])
  const quotaLatest = options.quotaLatest ?? resource<QuotaRadarLatestDTO>({
    buckets: [bucket()],
    last_aggregated_at: now,
    sample_size_warn_below: 3,
    stale: false,
  })
  const latest = options.degradationLatest ?? resource(degradationLatest)
  const arena = options.lmarena ?? resource(lmarena)
  const sources = options.sources ?? resource([source()])
  const states = [health, quotaLatest, latest, arena, sources] as const
  const hasCompletedRefresh = ref(options.hasCompletedRefresh ?? true)
  const quotaTrendState = options.quotaTrendState ?? resource<QuotaTrendDTO>(null, {
    hasSucceeded: false,
  })

  return {
    health,
    quotaLatest,
    degradationLatest: latest,
    lmarena: arena,
    sources,
    hasAnySuccess: computed(() => states.some((state) => state.hasSucceeded.value)),
    allInitialFailed: computed(() => (
      hasCompletedRefresh.value
      && !states.some((state) => state.loading.value)
      && !states.some((state) => state.hasSucceeded.value)
    )),
    isRefreshing: computed(() => states.some((state) => state.loading.value)),
    hasCompletedRefresh,
    lastFetchedAt: shallowRef(new Date(now)),
    refresh: options.refresh ?? vi.fn().mockResolvedValue(undefined),
    getQuotaTrendState: vi.fn(() => quotaTrendState),
    loadQuotaTrend: vi.fn().mockResolvedValue(quotaTrend()),
    dispose: vi.fn(),
  }
}

const RadarHeroStub = defineComponent({
  name: 'RadarHero',
  props: ['lastFetchedAt'],
  template: '<div data-testid="hero" />',
})

const ServiceHealthGridStub = defineComponent({
  name: 'ServiceHealthGrid',
  props: ['services', 'platforms'],
  template: '<div data-testid="health-grid" :data-platforms="platforms?.join(\',\') ?? \'\'">{{ platforms?.length ?? 0 }}</div>',
})

const QuotaBucketGridStub = defineComponent({
  name: 'QuotaBucketGrid',
  props: ['buckets'],
  template: '<div data-testid="quota-grid" />',
})

const DegradationStub = defineComponent({
  name: 'DegradationRadarTabs',
  props: [
    'latest',
    'latestLoading',
    'latestError',
    'lmarena',
    'lmarenaLoading',
    'lmarenaError',
  ],
  template: '<div data-testid="degradation" />',
})

const stubs: Record<string, Component> = {
  RadarPageHeader: defineComponent({ name: 'RadarPageHeader', template: '<header data-testid="header" />' }),
  RadarHero: RadarHeroStub,
  ServiceHealthGrid: ServiceHealthGridStub,
  QuotaBucketGrid: QuotaBucketGridStub,
  DegradationRadarTabs: DegradationStub,
  Icon: true,
}

const mountedWrappers: VueWrapper[] = []

function mountView(radar: UsePublicRadarReturn): VueWrapper {
  usePublicRadarMock.mockReturnValue(radar)
  const wrapper = mount(RadarHomeView, { global: { stubs } })
  mountedWrappers.push(wrapper)
  return wrapper
}

describe('RadarHomeView', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    localStorage.clear()
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    localeMock.value = 'en'
    getPublicChannelsMock.mockResolvedValue([
      {
        name: 'Public',
        description: '',
        platforms: [
          {
            platform: 'anthropic',
            groups: [],
            supported_models: [{ name: 'claude-fable-5', platform: 'anthropic', pricing: null }],
          },
          {
            platform: 'deepseek',
            groups: [],
            supported_models: [{ name: 'deepseek-v4-pro', platform: 'deepseek', pricing: null }],
          },
        ],
      },
    ])
  })

  afterEach(() => {
    for (const wrapper of mountedWrappers.splice(0)) wrapper.unmount()
    vi.runOnlyPendingTimers()
    vi.useRealTimers()
  })

  it('shows only the initial loading state until a core resource succeeds', async () => {
    const radar = makeRadar({
      health: resource<ServiceHealthDTO[]>(null, { loading: true, hasSucceeded: false }),
      quotaLatest: resource<QuotaRadarLatestDTO>(null, { loading: true, hasSucceeded: false }),
      degradationLatest: resource<DegradationLatestDTO>(null, { loading: true, hasSucceeded: false }),
      lmarena: resource<LMArenaDTO>(null, { loading: true, hasSucceeded: false }),
      sources: resource<DataSourceMetaDTO[]>(null, { loading: true, hasSucceeded: false }),
      hasCompletedRefresh: false,
    })

    const wrapper = mountView(radar)
    await nextTick()

    expect(wrapper.get('[data-testid="radar-initial-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quota-grid"]').exists()).toBe(false)
    expect(radar.refresh).toHaveBeenCalledTimes(1)
  })

  it('shows a safe page-level failure state without an in-page retry action', async () => {
    const failed = { error: 'load_failed' as const, hasSucceeded: false }
    const radar = makeRadar({
      health: resource<ServiceHealthDTO[]>(null, failed),
      quotaLatest: resource<QuotaRadarLatestDTO>(null, failed),
      degradationLatest: resource<DegradationLatestDTO>(null, failed),
      lmarena: resource<LMArenaDTO>(null, failed),
      sources: resource<DataSourceMetaDTO[]>(null, failed),
    })
    const wrapper = mountView(radar)

    expect(wrapper.get('[data-testid="radar-all-failed"]').text()).toContain('Unable to load radar data')
    expect(wrapper.text()).not.toContain('postgres://')
    expect(wrapper.find('[data-testid="radar-retry"]').exists()).toBe(false)
    expect(radar.refresh).toHaveBeenCalledTimes(1)
  })

  it('preserves successful content while only failed modules degrade', async () => {
    const radar = makeRadar({
      health: resource([service('claude_api')], { loading: true, error: 'load_failed' }),
      quotaLatest: resource<QuotaRadarLatestDTO>(null, {
        error: 'load_failed',
        hasSucceeded: false,
      }),
      sources: resource([source({ stale: true })], { error: 'load_failed' }),
    })
    const wrapper = mountView(radar)
    await flushPromises()

    expect(wrapper.get('[data-testid="health-grid"]').text()).toContain('2')
    expect(wrapper.find('[data-testid="quota-grid"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Unable to load this section')
    expect(wrapper.getComponent(RadarHeroStub).props()).not.toHaveProperty('stale')
    expect(wrapper.find('[data-testid="radar-section-retry"]').exists()).toBe(false)
    expect(radar.refresh).toHaveBeenCalledTimes(1)
  })

  it('keeps service health visible when the public catalog request fails', async () => {
    getPublicChannelsMock.mockRejectedValueOnce(new Error('catalog unavailable'))
    const radar = makeRadar({
      health: resource([service('openai_api'), service('deepseek')]),
    })

    const wrapper = mountView(radar)
    await flushPromises()

    const grid = wrapper.get('[data-testid="health-grid"]')
    expect(grid.attributes('data-platforms')).toBe('deepseek,openai')
    expect(grid.text()).toBe('2')
    expect(wrapper.text()).not.toContain('Unable to load this section')
  })

  it('does not render the data-source footer', () => {
    const wrapper = mountView(makeRadar())
    expect(wrapper.find('[data-testid="sources"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="disclaimer"]').exists()).toBe(false)
  })

  it('renders the same localized copyright footer as the home page', () => {
    const wrapper = mountView(makeRadar())

    expect(wrapper.get('[data-testid="radar-footer"]').text()).toBe(
      `© ${new Date().getFullYear()} TikToken.net is owned by Jerrywell Pte. Ltd.`,
    )
  })

  it('renders complete content and successful empty payloads without zero-value flashes', async () => {
    const contentWrapper = mountView(makeRadar({ health: resource([service('claude_api')]) }))
    await flushPromises()
    expect(contentWrapper.get('[data-testid="hero"]').exists()).toBe(true)
    expect(contentWrapper.get('[data-testid="health-grid"]').exists()).toBe(true)
    expect(contentWrapper.get('[data-testid="quota-grid"]').exists()).toBe(true)
    expect(contentWrapper.get('[data-testid="degradation"]').exists()).toBe(true)
    expect(contentWrapper.find('[data-testid="sources"]').exists()).toBe(false)

    const emptyWrapper = mountView(makeRadar({
      health: resource<ServiceHealthDTO[]>([]),
      quotaLatest: resource({
        buckets: [],
        last_aggregated_at: null,
        sample_size_warn_below: 3,
        stale: false,
      }),
      degradationLatest: resource({
        models: [],
        available_models: [],
        default_model_slugs: [],
        intelligence_index_version: null,
        lmarena_top5: [],
        sources_last_updated: {},
        stale: false,
      }),
      lmarena: resource({ leaderboard: [], total_votes: null, last_updated_at: null, fetched_at: now, stale: false }),
      sources: resource<DataSourceMetaDTO[]>([]),
    }))
    await flushPromises()
    expect(emptyWrapper.text()).toContain('No quota data yet')
    expect(emptyWrapper.get('[data-testid="degradation"]').exists()).toBe(true)
    expect(emptyWrapper.find('[data-testid="sources"]').exists()).toBe(false)
  })

  it('distinguishes a completed empty aggregation from a pending first run', () => {
    const emptyQuota = resource<QuotaRadarLatestDTO>({
      buckets: [],
      last_aggregated_at: null,
      sample_size_warn_below: 3,
      stale: true,
    })
    const completed = mountView(makeRadar({
      quotaLatest: emptyQuota,
      sources: resource([source({
        key: 'quota_aggregator',
        state: 'healthy',
        last_success_at: now,
      })]),
    }))
    expect(completed.text()).toContain('No publishable quota data')
    expect(completed.text()).toContain('configured minimum sample')

    const failed = mountView(makeRadar({
      quotaLatest: emptyQuota,
      sources: resource([source({
        key: 'quota_aggregator',
        state: 'failed',
        last_success_at: null,
        is_healthy: false,
      })]),
    }))
    expect(failed.text()).toContain('Quota aggregation is temporarily unavailable')
  })

  it('keeps every header anchor target below the mobile header and restores the compact offset at sm', () => {
    const wrapper = mountView(makeRadar())

    for (const id of ['health', 'quota', 'degradation']) {
      expect(wrapper.get(`#${id}`).classes()).toEqual(expect.arrayContaining([
        'scroll-mt-44',
        'sm:scroll-mt-32',
      ]))
    }
  })

  it('renders quota buckets without requesting trends or mounting a details modal', async () => {
    const radar = makeRadar()
    const wrapper = mountView(radar)
    await flushPromises()

    expect(wrapper.getComponent(QuotaBucketGridStub).props()).toEqual({
      buckets: radar.quotaLatest.data.value?.buckets,
    })
    expect(radar.getQuotaTrendState).not.toHaveBeenCalled()
    expect(radar.loadQuotaTrend).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="quota-modal"]').exists()).toBe(false)
  })

  it('passes the backend-filtered leaderboard through without waiting for or filtering by the catalog', async () => {
    getPublicChannelsMock.mockReturnValueOnce(new Promise(() => undefined))
    const radar = makeRadar({
      degradationLatest: resource(degradationLatest, {
        loading: true,
        error: 'load_failed',
      }),
      lmarena: resource(lmarena, { loading: false, error: null }),
    })
    const wrapper = mountView(radar)
    await flushPromises()

    expect(wrapper.getComponent(DegradationStub).props()).toMatchObject({
      latest: degradationLatest,
      latestLoading: true,
      latestError: 'load_failed',
      lmarena,
      lmarenaLoading: false,
      lmarenaError: null,
    })
  })

  it('fetches only once on mount and exposes no refresh controls or timers', async () => {
    localStorage.setItem('radar-refresh', JSON.stringify({ enabled: true, interval_seconds: 30 }))
    const refresh = vi.fn().mockResolvedValue(undefined)
    const wrapper = mountView(makeRadar({ refresh }))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(10 * 60 * 1000)

    expect(refresh).toHaveBeenCalledTimes(1)
    expect(getPublicChannelsMock).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="auto-refresh-toggle"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="refresh-interval"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="manual-refresh"]').exists()).toBe(false)
    expect(localStorage.getItem('radar-refresh')).toBe(JSON.stringify({ enabled: true, interval_seconds: 30 }))

    const radar = usePublicRadarMock.mock.results.at(-1)?.value as UsePublicRadarReturn

    wrapper.unmount()
    mountedWrappers.splice(mountedWrappers.indexOf(wrapper), 1)
    expect(radar.dispose).toHaveBeenCalledTimes(1)
  })
})
