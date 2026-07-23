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

  it('shows only the 5H and 7D quota limits and sample sizes for each plan', () => {
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
    expect(fiveHour.text()).toContain('Quota limit')
    expect(fiveHour.text()).toContain('$100.25')
    expect(fiveHour.text()).toContain('Sample size')
    expect(fiveHour.text()).toContain('4')
    expect(sevenDay.text()).toContain('7D')
    expect(sevenDay.text()).toContain('$300')
    expect(sevenDay.text()).toContain('9')

    expect(wrapper.text()).not.toContain('55%')
    expect(wrapper.text()).not.toContain('Accounts')
    expect(wrapper.text()).not.toContain('Trend')
    expect(wrapper.text()).not.toContain('View details')
    expect(wrapper.text()).not.toContain('±')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.find('svg').exists()).toBe(false)
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
})
