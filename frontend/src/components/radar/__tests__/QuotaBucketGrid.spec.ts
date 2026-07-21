import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import QuotaBucketGrid from '@/components/radar/QuotaBucketGrid.vue'
import { bucket, quotaTrend, windowStats } from './fixtures'

const { localeMock } = vi.hoisted(() => ({ localeMock: { value: 'en' } }))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ locale: localeMock, t: (_key: string, fallback?: string) => fallback ?? _key }),
  }
})

describe('QuotaBucketGrid', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('renders an explicit collection empty state', () => {
    const wrapper = mount(QuotaBucketGrid, { props: { buckets: [] } })
    expect(wrapper.text()).toContain('No publishable quota data')
  })

  it('applies exact utilization boundaries and keeps the numeric value visible', () => {
    const values = [39.9, 40, 60, 80]
    const levels = ['low', 'moderate', 'high', 'critical']
    const wrapper = mount(QuotaBucketGrid, {
      props: {
        buckets: values.map((value, index) => bucket({
          bucket_key: `openai/tier-${index}`,
          platform: 'openai',
          plan_tier: `tier-${index}`,
          display_name: `Tier ${index}`,
          five_hour: windowStats({ avg_utilization: value }),
        })),
      },
    })

    const bars = wrapper.findAll('[data-utilization-level]')
    expect(bars.map((bar) => bar.attributes('data-utilization-level'))).toEqual(levels)
    for (const value of values) expect(wrapper.text()).toContain(`${value}%`)
  })

  it.each(['en', 'zh'])('locale-formats large inference sample counts in %s', (locale) => {
    localeMock.value = locale
    const sampleSize = 12_345
    const wrapper = mount(QuotaBucketGrid, {
      props: {
        buckets: [bucket({ five_hour: windowStats({ sample_size: sampleSize }) })],
        sampleSizeWarnBelow: 20_000,
      },
    })
    const formatted = new Intl.NumberFormat(locale).format(sampleSize)

    expect(wrapper.text()).toContain(`n=${formatted}`)
    expect(wrapper.text()).not.toContain('n=12345')
  })

  it('shows window, inference, sample, and trend states without stale warnings or fake zeroes', () => {
    const noWindow = bucket({
      bucket_key: 'openai/pro',
      platform: 'openai',
      plan_tier: 'pro',
      display_name: 'ChatGPT Pro',
      accounts_count: 2,
      five_hour: null,
      seven_day: null,
      stale: true,
    })
    const insufficient = bucket({
      bucket_key: 'anthropic/max_5x',
      plan_tier: 'max_5x',
      display_name: 'Claude Max 5x',
      five_hour: windowStats({
        inferred_limit_usd: null,
        inferred_stdev: null,
        inference_reject_reason: 'insufficient_samples',
		sample_size: 2,
      }),
    })
    const dispersed = bucket({
      bucket_key: 'antigravity/ultra',
      platform: 'antigravity',
      plan_tier: 'ultra',
      display_name: 'Ultra',
      five_hour: windowStats({
        inferred_limit_usd: null,
        inferred_stdev: null,
        inference_reject_reason: 'high_dispersion',
      }),
    })
    const invalid = bucket({
      bucket_key: 'openai/team',
      platform: 'openai',
      plan_tier: 'team',
      display_name: 'Team',
      five_hour: windowStats({
        inferred_limit_usd: null,
        inferred_stdev: null,
        inference_reject_reason: 'invalid_mean',
      }),
    })
    const staleTrend = quotaTrend('anthropic/max_5x')
    staleTrend.stale = true
    const wrapper = mount(QuotaBucketGrid, {
      props: {
        buckets: [noWindow, insufficient, dispersed, invalid],
        sampleSizeWarnBelow: 3,
        trends: { 'anthropic/max_5x': staleTrend },
        trendLoading: { 'anthropic/max_5x': true },
        trendErrors: { 'anthropic/max_5x': 'internal raw failure' },
      },
    })

    expect(wrapper.text()).toContain('No data for this window')
    expect(wrapper.text()).toContain('Small sample: n=2')
    expect(wrapper.text()).toContain('Insufficient samples')
    expect(wrapper.text()).toContain('Data is too dispersed')
    expect(wrapper.text()).toContain('No trusted result')
    expect(wrapper.text()).toContain('No trend data')
    expect(wrapper.text()).toContain('Loading trend')
    expect(wrapper.text()).toContain('Unable to load trend')
    expect(wrapper.text()).not.toContain('internal raw failure')
    expect(wrapper.text()).not.toContain('Snapshot data may be outdated')
    expect(wrapper.text()).not.toContain('Trend data may be outdated')
    expect(wrapper.find('[data-testid="quota-sparkline-anthropic/max_5x"]').exists()).toBe(true)
    const accessibleTrend = wrapper.get('[data-testid="quota-sparkline-data-anthropic/max_5x"]')
    expect(accessibleTrend.text()).toContain('5-hour utilization')
    expect(accessibleTrend.text()).toContain('20%')
    expect(accessibleTrend.text()).toMatch(/2026|Jul/)
    expect(wrapper.text()).not.toContain('$0')
  })

  it('falls back to seven-day utilization and trend when OpenAI has no five-hour window', () => {
    const item = bucket({
      bucket_key: 'openai/pro',
      platform: 'openai',
      plan_tier: 'pro',
      display_name: 'ChatGPT Pro',
      accounts_count: 1,
      privacy_threshold: 1,
      five_hour: null,
      seven_day: windowStats({ avg_utilization: 24, sample_size: 1 }),
    })
    const trend = quotaTrend(item.bucket_key)
    trend.data_points = trend.data_points.map((point, index) => ({
      ...point,
      five_hour: null,
      seven_day: windowStats({ avg_utilization: 20 + index * 10 }),
    }))

    const wrapper = mount(QuotaBucketGrid, {
      props: { buckets: [item], trends: { [item.bucket_key]: trend } },
    })

    expect(wrapper.text()).toContain('7-day utilization')
    expect(wrapper.text()).toContain('24%')
    expect(wrapper.text()).toContain('Small sample: n=1')
    expect(wrapper.text()).toContain('7-day quota utilization trend')
    expect(wrapper.find('[data-testid="quota-sparkline-openai/pro"]').exists()).toBe(true)
    const summary = wrapper.get('[data-testid="quota-sparkline-data-openai/pro"]')
    expect(summary.text()).toContain('7-day utilization')
    expect(summary.text()).toContain('Latest: 30%')
    expect(wrapper.text()).not.toContain('No data for this window')
  })

  it('keeps card content browseable and exposes a full-card native action', async () => {
    const item = bucket()
    const wrapper = mount(QuotaBucketGrid, {
      props: { buckets: [item], trends: { [item.bucket_key]: quotaTrend(item.bucket_key) } },
    })
    const card = wrapper.get('[data-bucket-key="anthropic/max_20x"]')
    const openButton = card.get('[data-testid="quota-open-anthropic/max_20x"]')

    expect(card.element.tagName).toBe('ARTICLE')
    expect(card.attributes('role')).toBeUndefined()
    expect(card.attributes('tabindex')).toBeUndefined()
    expect(card.attributes('aria-label')).toBeUndefined()
    expect(card.classes()).toContain('relative')
    expect(openButton.element.tagName).toBe('BUTTON')
    expect(openButton.attributes('type')).toBe('button')
    expect(openButton.attributes('aria-label')).toBe('View details: Claude Max 20x')
    expect(openButton.classes()).toEqual(expect.arrayContaining([
      'absolute',
      'inset-0',
      'focus-visible:ring-2',
      'focus-visible:ring-primary-500',
    ]))
    expect(card.findAll('button')).toHaveLength(1)
    const visualHint = card.get('[data-testid="quota-open-hint-anthropic/max_20x"]')
    expect(visualHint.attributes('aria-hidden')).toBe('true')
    expect(visualHint.text()).toBe('View details →')
    expect(card.text()).toContain('55%')
    const accessibleTrend = card.get('[data-testid="quota-sparkline-data-anthropic/max_20x"]')
    expect(accessibleTrend.text()).toContain('Quota utilization trend')
    expect(accessibleTrend.text()).toContain('Latest: 60%')
    await openButton.trigger('click')
    expect(wrapper.emitted('select')).toHaveLength(1)
    expect(wrapper.emitted('select')?.[0]).toEqual([item])
  })

  it('requests trend data only when a card approaches the viewport', async () => {
    let callback: IntersectionObserverCallback | undefined
    const observe = vi.fn()
    const unobserve = vi.fn()
    const disconnect = vi.fn()
    vi.stubGlobal('IntersectionObserver', class {
      readonly root = null
      readonly rootMargin = '200px 0px'
      readonly thresholds = [0.01]
      constructor(handler: IntersectionObserverCallback) { callback = handler }
      observe = observe
      unobserve = unobserve
      disconnect = disconnect
      takeRecords = () => []
    })
    const first = bucket()
    const second = bucket({
      bucket_key: 'openai/pro',
      platform: 'openai',
      plan_tier: 'pro',
      display_name: 'ChatGPT Pro',
    })
    const wrapper = mount(QuotaBucketGrid, { props: { buckets: [first, second] } })
    await nextTick()
    const cards = wrapper.findAll('[data-bucket-key]')

    expect(observe).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('requestTrend')).toBeUndefined()
    callback?.([
      { target: cards[0].element, isIntersecting: true } as IntersectionObserverEntry,
      { target: cards[1].element, isIntersecting: false } as IntersectionObserverEntry,
    ], {} as IntersectionObserver)
    await nextTick()

    expect(wrapper.emitted('requestTrend')).toEqual([[first.bucket_key]])
    expect(unobserve).toHaveBeenCalledWith(cards[0].element)
    callback?.([
      { target: cards[0].element, isIntersecting: true } as IntersectionObserverEntry,
    ], {} as IntersectionObserver)
    expect(wrapper.emitted('requestTrend')).toEqual([[first.bucket_key]])

    wrapper.unmount()
    expect(disconnect).toHaveBeenCalledOnce()
  })

  it('summarizes a full seven-day series without hundreds of hidden DOM rows', () => {
    const item = bucket()
    const start = Date.parse('2026-07-06T08:00:00.000Z')
    const trend = quotaTrend(item.bucket_key)
    trend.data_points = Array.from({ length: 672 }, (_, index) => ({
      timestamp: new Date(start + index * 15 * 60 * 1000).toISOString(),
      five_hour: {
        avg_utilization: index % 100,
        avg_cost: index,
        inferred_limit_usd: null,
        sample_size: 4,
      },
      seven_day: null,
    }))
    const wrapper = mount(QuotaBucketGrid, {
      props: { buckets: [item], trends: { [item.bucket_key]: trend } },
    })

    const summary = wrapper.get('[data-testid="quota-sparkline-data-anthropic/max_20x"]')
    expect(summary.findAll('tr')).toHaveLength(0)
    expect(summary.text()).toContain('672 points')
    expect(summary.text()).toContain('Latest: 71%')
    expect(summary.text()).toContain('Minimum: 0%')
    expect(summary.text()).toContain('Maximum: 99%')
    expect(summary.text()).toMatch(/Jul|2026/)
  })
})
