import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import QuotaBucketGrid from '@/components/radar/QuotaBucketGrid.vue'
import { bucket, windowStats } from './fixtures'

const { localeMock } = vi.hoisted(() => ({ localeMock: { value: 'en' } }))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ locale: localeMock, t: (_key: string, fallback?: string) => fallback ?? _key }),
  }
})

describe('QuotaBucketGrid', () => {
  it('renders an explicit collection empty state', () => {
    const wrapper = mount(QuotaBucketGrid, { props: { buckets: [] } })
    expect(wrapper.text()).toContain('No publishable quota data')
  })

  it('shows backend-declared quota windows, estimates, sample sizes, and a details entry', async () => {
    const item = bucket({
      five_hour: windowStats({
        inferred_limit_usd: 100.25,
        inferred_stdev: 12,
        sample_size: 4,
      }),
      seven_day: windowStats({
        inferred_limit_usd: 300,
        inferred_stdev: 25,
        sample_size: 9,
      }),
    })
    const wrapper = mount(QuotaBucketGrid, { props: { buckets: [item] } })
    const fiveHour = wrapper.get('[data-testid="quota-window-anthropic/max_20x-5h"]')
    const sevenDay = wrapper.get('[data-testid="quota-window-anthropic/max_20x-7d"]')

    expect(wrapper.text()).toContain('Anthropic')
    expect(wrapper.text()).toContain('Claude Max 20x')
    expect(fiveHour.text()).toContain('5H')
    expect(fiveHour.text()).toContain('Estimated API-equivalent value')
    expect(fiveHour.text()).toContain('$100.25')
    expect(fiveHour.text()).toContain('Sample size')
    expect(fiveHour.text()).toContain('4')
    expect(sevenDay.text()).toContain('7D')
    expect(sevenDay.text()).toContain('$300')
    expect(sevenDay.text()).toContain('9')

    expect(wrapper.text()).not.toContain('55%')
    expect(wrapper.text()).not.toContain('Accounts')
    expect(wrapper.text()).not.toContain('Trend')
    expect(wrapper.text()).toContain('View details')
    expect(wrapper.text()).not.toContain('±')
    await wrapper.get('[data-testid="quota-view-details"]').trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual([item])
    expect(wrapper.find('svg').exists()).toBe(false)
  })

  it('renders a backend-added window without a frontend platform switch', () => {
    const item = bucket({
      windows: [{
        key: '24h',
        label: '24H',
        duration_seconds: 86400,
        currency: 'EUR',
        stats: windowStats({ inferred_limit_usd: 250 }),
        model_windows: [],
        model_breakdown: [],
      }],
    })
    const wrapper = mount(QuotaBucketGrid, { props: { buckets: [item] } })

    expect(wrapper.get('[data-testid="quota-window-anthropic/max_20x-24h"]').text()).toContain('24H')
    expect(wrapper.text()).toContain('€250')
    expect(wrapper.find('[data-testid$="-5h"]').exists()).toBe(false)
  })

  it.each(['en', 'zh'])('locale-formats large sample counts in %s', (locale) => {
    localeMock.value = locale
    const sampleSize = 12_345
    const wrapper = mount(QuotaBucketGrid, {
      props: {
        buckets: [bucket({ five_hour: windowStats({ sample_size: sampleSize }) })],
      },
    })

    expect(wrapper.get('[data-testid="quota-window-anthropic/max_20x-5h"]').text())
      .toContain(new Intl.NumberFormat(locale).format(sampleSize))
  })

  it('uses dashes for unavailable limits or windows without inventing zero values', () => {
    const wrapper = mount(QuotaBucketGrid, {
      props: {
        buckets: [bucket({
          five_hour: windowStats({ inferred_limit_usd: null, sample_size: 2 }),
          seven_day: null,
        })],
      },
    })
    const fiveHour = wrapper.get('[data-testid="quota-window-anthropic/max_20x-5h"]')
    const sevenDay = wrapper.get('[data-testid="quota-window-anthropic/max_20x-7d"]')

    expect(fiveHour.text()).toContain('—')
    expect(fiveHour.text()).toContain('2')
    expect(sevenDay.text().match(/—/g)).toHaveLength(2)
    expect(wrapper.text()).not.toContain('$0')
  })

  it('labels a published generic Claude singleton estimate as low confidence', () => {
    const wrapper = mount(QuotaBucketGrid, {
      props: {
        buckets: [bucket({
          bucket_key: 'anthropic/generic',
          plan_tier: 'generic',
          display_name: 'Claude Subscription',
          five_hour: windowStats({
            inferred_limit_usd: 80,
            inferred_stdev: null,
            sample_size: 1,
            inference_confidence: 'low',
          }),
        })],
      },
    })

    const fiveHour = wrapper.get('[data-testid="quota-window-anthropic/generic-5h"]')
    expect(fiveHour.text()).toContain('$80')
    expect(fiveHour.text()).toContain('Single-sample estimate · Low confidence')
    expect(fiveHour.text()).not.toContain('±')
  })

  it('uses the warning threshold returned by the quota API', () => {
    const wrapper = mount(QuotaBucketGrid, {
      props: {
        buckets: [bucket({
          five_hour: windowStats({
            sample_size: 4,
            inference_confidence: 'medium',
          }),
        })],
        sampleSizeWarnBelow: 5,
      },
    })

    expect(wrapper.get('[data-testid="quota-window-anthropic/max_20x-5h"]').text())
      .toContain('Small sample')
  })

  it('explains an unavailable estimate from a legacy unknown-plan snapshot', () => {
    const wrapper = mount(QuotaBucketGrid, {
      props: {
        buckets: [bucket({
          bucket_key: 'anthropic/generic',
          plan_tier: 'generic',
          display_name: 'Claude Subscription',
          five_hour: windowStats({
            inferred_limit_usd: null,
            inferred_stdev: null,
            inference_confidence: undefined,
            inference_reject_reason: 'unknown_plan',
          }),
        })],
      },
    })

    const fiveHour = wrapper.get('[data-testid="quota-window-anthropic/generic-5h"]')
    expect(fiveHour.text()).toContain('—')
    expect(fiveHour.text()).toContain('Plan is unknown; estimation is unavailable')
  })
})
