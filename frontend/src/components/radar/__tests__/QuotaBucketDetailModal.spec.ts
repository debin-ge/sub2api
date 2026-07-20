import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import QuotaBucketDetailModal from '@/components/radar/QuotaBucketDetailModal.vue'
import { bucket, quotaTrend, windowStats } from './fixtures'

const { localeMock } = vi.hoisted(() => ({ localeMock: { value: 'en' } }))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ locale: localeMock, t: (_key: string, fallback?: string) => fallback ?? _key }),
  }
})

let wrapper: VueWrapper | null = null

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
  document.body.style.overflow = ''
  localeMock.value = 'en'
})

describe('QuotaBucketDetailModal', () => {
  it('is a teleported accessible dialog with focus trap, body lock, and focus restoration', async () => {
    document.body.innerHTML = '<button id="trigger">open</button>'
    const trigger = document.querySelector<HTMLButtonElement>('#trigger')!
    trigger.focus()
    wrapper = mount(QuotaBucketDetailModal, {
      attachTo: document.body,
      props: { show: true, bucket: bucket(), trend: quotaTrend() },
      global: { stubs: { Icon: true } },
    })
    await wrapper.vm.$nextTick()

    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    const close = document.body.querySelector<HTMLButtonElement>('[data-testid="quota-modal-close"]')!
    expect(dialog.getAttribute('aria-labelledby')).toBe('quota-bucket-modal-title')
    expect(close.getAttribute('aria-label')).toBe('Close quota details')
    expect(document.activeElement).toBe(close)
    expect(document.body.style.overflow).toBe('hidden')
    const accessibleTrend = document.body.querySelector<HTMLElement>('[data-testid="quota-modal-trend-summary"]')!
    expect(accessibleTrend.textContent).toContain('5-hour utilization')
    expect(accessibleTrend.textContent).toContain('Latest: 60%')
    expect(accessibleTrend.textContent).toContain('Minimum: 20%')
    expect(accessibleTrend.textContent).toContain('Maximum: 60%')
    expect(accessibleTrend.textContent).toMatch(/2026|Jul/)
    expect(accessibleTrend.closest('[aria-live]')).toBeNull()
    expect(document.body.querySelector('[data-testid="quota-modal-trend-data"]')).toBeNull()
    expect(document.body.querySelector('[data-testid="quota-modal-trend-toggle"]')).toBeNull()

    const lastFocusable = document.body.querySelector<HTMLButtonElement>('[data-window="5h"]')!
    lastFocusable.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab' }))
    expect(document.activeElement).toBe(close)
    close.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true }))
    expect(document.activeElement).toBe(lastFocusable)

    close.click()
    expect(wrapper.emitted('close')).toHaveLength(1)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(2)
    await wrapper.setProps({ show: false })
    expect(document.body.style.overflow).toBe('')
    expect(document.activeElement).toBe(trigger)
  })

  it('closes through the backdrop and renders synchronized window details', async () => {
    const item = bucket({
      seven_day_sonnet: { model: 'claude-sonnet', avg_utilization: 51, sample_size: 4 },
      seven_day_fable: { model: 'claude-fable', avg_utilization: 22, sample_size: 4 },
      model_breakdown_7d: [
        { model: 'claude-sonnet', avg_cost: 10, avg_requests: 20, percentage: 75 },
      ],
    })
    wrapper = mount(QuotaBucketDetailModal, {
      attachTo: document.body,
      props: { show: true, bucket: item, trend: quotaTrend() },
      global: { stubs: { Icon: true } },
    })
    await wrapper.vm.$nextTick()

    const sevenDay = document.body.querySelector<HTMLButtonElement>('[data-window="7d"]')!
    sevenDay.click()
    await wrapper.vm.$nextTick()
    expect(document.body.textContent).toContain('Sonnet')
    expect(document.body.textContent).toContain('Fable')
    expect(document.body.textContent).toContain('claude-sonnet')
    expect(document.body.textContent).toContain('75%')

    document.body.querySelector<HTMLElement>('[data-testid="quota-modal-backdrop"]')!.click()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it.each(['en', 'zh'])('locale-formats large inference sample counts in %s', async (locale) => {
    localeMock.value = locale
	const sampleSize = 12_345
    wrapper = mount(QuotaBucketDetailModal, {
      attachTo: document.body,
      props: {
        show: true,
		bucket: bucket({ five_hour: windowStats({ sample_size: sampleSize }) }),
        sampleSizeWarnBelow: 20_000,
      },
      global: { stubs: { Icon: true } },
    })
    await wrapper.vm.$nextTick()
	const formatted = new Intl.NumberFormat(locale).format(sampleSize)

    expect(document.body.textContent).toContain(`n=${formatted}`)
    expect(document.body.textContent).not.toContain('n=12345')
  })

  it('disables a missing window and exposes local trend loading/error/empty states safely', async () => {
    const item = bucket({ seven_day: null })
    wrapper = mount(QuotaBucketDetailModal, {
      attachTo: document.body,
      props: {
        show: true,
        bucket: item,
        trend: null,
        trendLoading: true,
        trendError: 'redis://secret/raw',
      },
      global: { stubs: { Icon: true } },
    })
    await wrapper.vm.$nextTick()

    expect(document.body.querySelector<HTMLButtonElement>('[data-window="7d"]')?.disabled).toBe(true)
    expect(document.body.textContent).toContain('Loading trend')
    expect(document.body.textContent).toContain('Unable to load trend')
    expect(document.body.textContent).toContain('No model breakdown data')
    expect(document.body.textContent).not.toContain('redis')
    const statuses = Array.from(document.body.querySelectorAll<HTMLElement>('[role="status"]'))
      .map((element) => element.textContent)
      .join(' ')
    expect(statuses).toContain('Loading trend')
    expect(statuses).toContain('Unable to load trend')
    await wrapper.setProps({ trendLoading: false, trendError: null })
    expect(document.body.textContent).toContain('No trend data')
  })

  it('treats legacy null breakdown and trend arrays as empty', async () => {
    const item = bucket()
    const trend = quotaTrend()
    // Historical Go snapshots encoded nil slices as JSON null even though the
    // current TypeScript contract exposes arrays.
    item.model_breakdown_5h = null as unknown as typeof item.model_breakdown_5h
    trend.data_points = null as unknown as typeof trend.data_points

    expect(() => {
      wrapper = mount(QuotaBucketDetailModal, {
        attachTo: document.body,
        props: { show: true, bucket: item, trend },
        global: { stubs: { Icon: true } },
      })
    }).not.toThrow()
    await wrapper!.vm.$nextTick()

    expect(document.body.textContent).toContain('No model breakdown data')
    expect(document.body.textContent).toContain('No trend data')
  })

  it('never renders Anthropic model utilization for another platform', async () => {
    wrapper = mount(QuotaBucketDetailModal, {
      attachTo: document.body,
      props: {
        show: true,
        bucket: bucket({
          bucket_key: 'openai/pro',
          platform: 'openai',
          plan_tier: 'pro',
          display_name: 'OpenAI Pro',
          seven_day_sonnet: { model: 'should-not-render', avg_utilization: 51, sample_size: 4 },
          seven_day_fable: { model: 'should-not-render', avg_utilization: 22, sample_size: 4 },
        }),
      },
      global: { stubs: { Icon: true } },
    })
    await wrapper.vm.$nextTick()
    document.body.querySelector<HTMLButtonElement>('[data-window="7d"]')!.click()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).not.toContain('Sonnet')
    expect(document.body.textContent).not.toContain('Fable')
    expect(document.body.textContent).not.toContain('should-not-render')
  })

  it('downsamples 672 visual points without rendering the full trend control or table', async () => {
    const item = bucket()
    const trend = quotaTrend(item.bucket_key)
    const start = Date.parse('2026-07-06T08:00:00.000Z')
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
    wrapper = mount(QuotaBucketDetailModal, {
      attachTo: document.body,
      props: { show: true, bucket: item, trend },
      global: { stubs: { Icon: true } },
    })
    await wrapper.vm.$nextTick()

    const bars = document.body.querySelector<HTMLElement>('[data-testid="quota-modal-trend-bars"]')
    expect(bars).not.toBeNull()
    expect(bars?.querySelectorAll('[data-trend-point]').length).toBeLessThanOrEqual(96)
    expect(bars?.querySelector('[data-trend-value="0"]')).not.toBeNull()
    expect(bars?.querySelector('[data-trend-value="99"]')).not.toBeNull()
    expect(bars?.querySelector(`[data-trend-timestamp="${trend.data_points[0].timestamp}"]`)).not.toBeNull()
    expect(bars?.querySelector(`[data-trend-timestamp="${trend.data_points.at(-1)!.timestamp}"]`)).not.toBeNull()
    expect(bars?.innerHTML).not.toContain('min-w-1')

    const summary = document.body.querySelector<HTMLElement>('[data-testid="quota-modal-trend-summary"]')
    expect(summary?.textContent).toContain('672 points')
    expect(summary?.textContent).toContain('Latest: 71%')
    expect(summary?.textContent).toContain('Minimum: 0%')
    expect(summary?.textContent).toContain('Maximum: 99%')
    expect(document.body.querySelectorAll('[data-testid="quota-modal-trend-data"] tbody tr')).toHaveLength(0)
    expect(document.body.querySelector('[data-testid="quota-modal-trend-toggle"]')).toBeNull()
  })

  it('selects seven-day details and trend when five-hour data is unavailable', async () => {
    const item = bucket({
	  bucket_key: 'openai/pro_20x',
	  platform: 'openai',
	  plan_tier: 'pro_20x',
	  display_name: 'ChatGPT Pro 20x',
      accounts_count: 1,
	  privacy_threshold: 1,
	  five_hour: null,
	  seven_day: windowStats({ avg_utilization: 35, sample_size: 1 }),
    })
    const trend = quotaTrend(item.bucket_key)
    trend.data_points = trend.data_points.map((point, index) => ({
      ...point,
      five_hour: null,
      seven_day: { avg_utilization: 20 + index * 10, avg_cost: 0, inferred_limit_usd: null, sample_size: 1 },
    }))
    wrapper = mount(QuotaBucketDetailModal, {
      attachTo: document.body,
      props: { show: true, bucket: item, trend },
      global: { stubs: { Icon: true } },
    })
    await wrapper.vm.$nextTick()

	  expect(document.body.querySelector('[data-window="5h"]')?.getAttribute('disabled')).not.toBeNull()
	  expect(document.body.querySelector('[data-window="7d"]')?.getAttribute('aria-selected')).toBe('true')
	  expect(document.body.textContent).toContain('Small sample: n=1')
    expect(document.body.querySelector('[data-testid="quota-modal-trend-summary"]')?.textContent).toContain('7-day utilization')
  })
})
