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
  DegradationTrendDTO,
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

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      locale: localeMock,
      t: (_key: string, fallback?: string) => fallback ?? _key,
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
  degradationTrendState?: RadarResourceState<DegradationTrendDTO>
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
  const degradationTrendState = options.degradationTrendState
    ?? resource<DegradationTrendDTO>(null, { hasSucceeded: false })

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
    getDegradationTrendState: vi.fn(() => degradationTrendState),
    loadDegradationTrend: vi.fn().mockResolvedValue({
      model_slug: 'model-a',
      metric: 'intelligence_index',
      days: 90,
      data_points: [],
      stale: false,
    }),
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
  props: ['buckets', 'sampleSizeWarnBelow', 'trends', 'trendLoading', 'trendErrors'],
  emits: ['select', 'requestTrend'],
  template: '<div data-testid="quota-grid"><template v-for="(bucket, index) in buckets" :key="bucket.bucket_key"><button :data-testid="`load-bucket-${index}`" @click="$emit(\'requestTrend\', bucket.bucket_key)">load trend</button><button :data-testid="`select-bucket-${index}`" @click="$emit(\'select\', bucket)">detail</button></template></div>',
})

const QuotaModalStub = defineComponent({
  name: 'QuotaBucketDetailModal',
  props: ['show', 'bucket', 'trend', 'trendLoading', 'trendError', 'sampleSizeWarnBelow'],
  emits: ['close'],
  template: '<div data-testid="quota-modal" :data-open="String(show)"><button data-testid="close-modal" @click="$emit(\'close\')">close</button></div>',
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
    'trend',
    'trendLoading',
    'trendError',
  ],
  emits: ['request-trend'],
  template: '<div data-testid="degradation"><button data-testid="request-trend" @click="$emit(\'request-trend\', \'model-a\', \'coding_index\', 90)">trend</button></div>',
})

const stubs: Record<string, Component> = {
  RadarPageHeader: defineComponent({ name: 'RadarPageHeader', template: '<header data-testid="header" />' }),
  RadarHero: RadarHeroStub,
  ServiceHealthGrid: ServiceHealthGridStub,
  QuotaBucketGrid: QuotaBucketGridStub,
  QuotaBucketDetailModal: QuotaModalStub,
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
        lmarena_top5: [],
        sources_last_updated: {},
        trend_available: false,
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

  it('loads quota trends on grid demand, retries failures, and reuses bucket state in the modal', async () => {
    const firstBucket = bucket()
    const secondBucket = bucket({
      bucket_key: 'openai/pro',
      platform: 'openai',
      plan_tier: 'pro',
      display_name: 'OpenAI Pro',
    })
    const thirdBucket = bucket({
      bucket_key: 'antigravity/ultra',
      platform: 'antigravity',
      plan_tier: 'ultra',
      display_name: 'Antigravity Ultra',
    })
    const quotaStates: Record<string, RadarResourceState<QuotaTrendDTO>> = {
      [firstBucket.bucket_key]: resource<QuotaTrendDTO>(null, { hasSucceeded: false }),
      [secondBucket.bucket_key]: resource<QuotaTrendDTO>(null, { hasSucceeded: false }),
      [thirdBucket.bucket_key]: resource<QuotaTrendDTO>(null, { hasSucceeded: false }),
    }
    const quotaLatestState = resource<QuotaRadarLatestDTO>({
      buckets: [firstBucket, secondBucket],
      last_aggregated_at: now,
      sample_size_warn_below: 3,
      stale: false,
    })
    const degradationState = resource<DegradationTrendDTO>({
      model_slug: 'model-a',
      metric: 'coding_index',
      days: 90,
      data_points: [{ date: '2026-07-13', value: 88 }],
      stale: false,
    })
    const radar = makeRadar({
      quotaLatest: quotaLatestState,
      degradationTrendState: degradationState,
    })
    const attempts: Record<string, number> = {}
    vi.mocked(radar.getQuotaTrendState).mockImplementation((bucketKey) => quotaStates[bucketKey])
    vi.mocked(radar.loadQuotaTrend).mockImplementation((bucketKey) => {
      attempts[bucketKey] = (attempts[bucketKey] ?? 0) + 1
      const state = quotaStates[bucketKey]
      if (bucketKey === secondBucket.bucket_key && attempts[bucketKey] === 1) {
        state.error.value = 'load_failed'
        return Promise.reject(new Error('upstream failed'))
      }
      const trend = quotaTrend(bucketKey)
      state.data.value = trend
      state.error.value = null
      state.hasSucceeded.value = true
      return Promise.resolve(trend)
    })
    const wrapper = mountView(radar)
    await flushPromises()

    expect(attempts).toEqual({})
    await wrapper.get('[data-testid="load-bucket-0"]').trigger('click')
    await wrapper.get('[data-testid="load-bucket-1"]').trigger('click')
    await flushPromises()
    expect(attempts).toEqual({
      [firstBucket.bucket_key]: 1,
      [secondBucket.bucket_key]: 1,
    })
    expect(wrapper.getComponent(QuotaBucketGridStub).props('trends')).toMatchObject({
      [firstBucket.bucket_key]: quotaStates[firstBucket.bucket_key].data.value,
      [secondBucket.bucket_key]: null,
    })
    expect(wrapper.getComponent(QuotaBucketGridStub).props('trendErrors')).toMatchObject({
      [secondBucket.bucket_key]: 'load_failed',
    })
    expect(wrapper.getComponent(QuotaBucketGridStub).props('trendLoading')).toMatchObject({
      [firstBucket.bucket_key]: false,
      [secondBucket.bucket_key]: false,
    })

    quotaLatestState.data.value = {
      ...quotaLatestState.data.value!,
      buckets: [firstBucket, secondBucket, thirdBucket],
    }
    await nextTick()
    await flushPromises()
    expect(attempts).toEqual({
      [firstBucket.bucket_key]: 1,
      [secondBucket.bucket_key]: 1,
    })
    await wrapper.get('[data-testid="load-bucket-1"]').trigger('click')
    await wrapper.get('[data-testid="load-bucket-2"]').trigger('click')
    await flushPromises()
    expect(attempts).toEqual({
      [firstBucket.bucket_key]: 1,
      [secondBucket.bucket_key]: 2,
      [thirdBucket.bucket_key]: 1,
    })
    expect(wrapper.getComponent(QuotaBucketGridStub).props('trends')).toMatchObject({
      [firstBucket.bucket_key]: quotaStates[firstBucket.bucket_key].data.value,
      [secondBucket.bucket_key]: quotaStates[secondBucket.bucket_key].data.value,
      [thirdBucket.bucket_key]: quotaStates[thirdBucket.bucket_key].data.value,
    })

    quotaStates[secondBucket.bucket_key].data.value = null
    quotaStates[secondBucket.bucket_key].hasSucceeded.value = false
    quotaStates[secondBucket.bucket_key].error.value = 'load_failed'
    await wrapper.get('[data-testid="select-bucket-1"]').trigger('click')
    await flushPromises()
    expect(attempts[secondBucket.bucket_key]).toBe(3)
    expect(wrapper.getComponent(QuotaModalStub).props()).toMatchObject({
      show: true,
      bucket: expect.objectContaining({ bucket_key: secondBucket.bucket_key }),
      trend: quotaStates[secondBucket.bucket_key].data.value,
    })

    const refreshedSecondBucket = {
      ...secondBucket,
      display_name: 'OpenAI Pro (refreshed)',
    }
    quotaLatestState.data.value = {
      ...quotaLatestState.data.value!,
      buckets: [firstBucket, refreshedSecondBucket],
    }
    await nextTick()
    expect(wrapper.getComponent(QuotaBucketGridStub).props('trends'))
      .not.toHaveProperty(thirdBucket.bucket_key)
    expect(wrapper.getComponent(QuotaModalStub).props()).toMatchObject({
      show: true,
      bucket: expect.objectContaining({
        bucket_key: secondBucket.bucket_key,
        display_name: 'OpenAI Pro (refreshed)',
      }),
    })

    quotaLatestState.data.value = {
      ...quotaLatestState.data.value,
      buckets: [firstBucket],
    }
    await nextTick()
    expect(wrapper.getComponent(QuotaModalStub).props()).toMatchObject({
      show: false,
      bucket: null,
    })

    await wrapper.get('[data-testid="request-trend"]').trigger('click')
    expect(radar.getDegradationTrendState).toHaveBeenCalledWith('model-a', 'coding_index', 90)
    expect(radar.loadDegradationTrend).toHaveBeenCalledWith('model-a', 'coding_index', 90)
    expect(wrapper.getComponent(DegradationStub).props('trend')).toEqual(degradationState.data.value)
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
