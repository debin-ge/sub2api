import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { degradationLatest, lmarena, now } from './fixtures'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ locale: ref('en'), t: (_key: string, fallback?: string) => fallback ?? _key }),
  }
})

vi.mock('chart.js', () => ({
  Chart: { register: vi.fn() },
  CategoryScale: {},
  Legend: {},
  LineController: {},
  LineElement: {},
  LinearScale: {},
  PointElement: {},
  RadarController: {},
  RadialLinearScale: {},
  Title: {},
  Tooltip: {},
}))

vi.mock('vue-chartjs', () => ({
  Radar: defineComponent({
    name: 'RadarChart',
    props: { data: { type: Object, required: true }, options: { type: Object, required: true } },
    setup: () => () => h('div', { 'data-testid': 'radar-chart' }),
  }),
  Line: defineComponent({
    name: 'LineChart',
    props: { data: { type: Object, required: true }, options: { type: Object, required: true } },
    setup: () => () => h('div', { 'data-testid': 'line-chart' }),
  }),
}))

import DegradationRadarTabs from '@/components/radar/DegradationRadarTabs.vue'

describe('DegradationRadarTabs', () => {
  it('limits radar series to six, preserves null metrics, and requests a 90-day trend', async () => {
    const wrapper = mount(DegradationRadarTabs, {
      props: {
        latest: degradationLatest,
        lmarena,
        trend: {
          model_slug: 'model-a',
          metric: 'intelligence_index',
          days: 90,
          data_points: [{ date: '2026-07-13', value: 91 }],
          stale: false,
        },
      },
    })
    await wrapper.vm.$nextTick()

    const radar = wrapper.findComponent({ name: 'RadarChart' })
    const data = radar.props('data') as { datasets: Array<{ data: Array<number | null> }> }
    expect(data.datasets).toHaveLength(6)
    expect(data.datasets[0].data).toEqual([91, null, 73])
    expect(wrapper.findAll('#degradation-panel-overview article')).toHaveLength(6)
    expect(wrapper.emitted('request-trend')?.[0]).toEqual(['model-a', 'intelligence_index', 90])

    const modelSelect = wrapper.get('[data-testid="model-select"]')
    expect(modelSelect.findAll('option')).toHaveLength(7)
    await modelSelect.setValue('model-7')
    expect(wrapper.emitted('request-trend')?.at(-1)).toEqual(['model-7', 'intelligence_index', 90])

    await modelSelect.setValue('model-a')
    await wrapper.get('[data-testid="metric-select"]').setValue('agentic_index')
    expect(wrapper.emitted('request-trend')?.at(-1)).toEqual(['model-a', 'agentic_index', 90])
  })

  it('renders automatically selected overview models without requesting unavailable trends', async () => {
    const wrapper = mount(DegradationRadarTabs, {
      props: {
        latest: { ...degradationLatest, trend_available: false },
        lmarena,
      },
    })
    await wrapper.vm.$nextTick()

    expect(wrapper.findComponent({ name: 'RadarChart' }).exists()).toBe(true)
    expect(wrapper.find('[data-testid="model-select"]').exists()).toBe(false)
    expect(wrapper.emitted('request-trend')).toBeUndefined()
  })

  it('recomputes chart colors when the root dark class changes', async () => {
    let callback: MutationCallback | null = null
    const observe = vi.fn()
    const disconnect = vi.fn()
    vi.stubGlobal('MutationObserver', class {
      constructor(cb: MutationCallback) { callback = cb }
      observe = observe
      disconnect = disconnect
    })
    const wrapper = mount(DegradationRadarTabs, {
      props: {
        latest: degradationLatest,
        lmarena,
        trend: {
          model_slug: 'model-a',
          metric: 'intelligence_index',
          days: 90,
          data_points: [{ date: '2026-07-13', value: 91 }],
          stale: false,
        },
      },
    })
    const before = wrapper.findComponent({ name: 'RadarChart' }).props('options') as {
      scales: { r: { grid: { color: string } } }
    }
    const lineBefore = wrapper.findComponent({ name: 'LineChart' }).props('options') as {
      scales: { y: { grid: { color: string } } }
    }

    document.documentElement.classList.add('dark')
    callback?.([], {} as MutationObserver)
    await wrapper.vm.$nextTick()
    const after = wrapper.findComponent({ name: 'RadarChart' }).props('options') as {
      scales: { r: { grid: { color: string } } }
    }
    const lineAfter = wrapper.findComponent({ name: 'LineChart' }).props('options') as {
      scales: { y: { grid: { color: string } } }
    }
    expect(after.scales.r.grid.color).not.toBe(before.scales.r.grid.color)
    expect(lineAfter.scales.y.grid.color).not.toBe(lineBefore.scales.y.grid.color)
    expect(observe).toHaveBeenCalledWith(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalled()
    vi.unstubAllGlobals()
    document.documentElement.classList.remove('dark')
  })

  it('supports keyboard tabs and renders a sorted, horizontally scrollable LMArena table', async () => {
    const arenaWithSourceRanks = {
      ...lmarena,
      leaderboard: lmarena.leaderboard.map((entry) => ({
        ...entry,
        rank: entry.model === 'First' ? 17 : 42,
      })),
    }
    const wrapper = mount(DegradationRadarTabs, { props: { latest: degradationLatest, lmarena: arenaWithSourceRanks } })
    expect(wrapper.text()).toContain('Model leaderboard')
    expect(wrapper.text()).not.toContain('LMArena Elo')
    const overviewTab = wrapper.get('[data-tab="overview"]')
    expect(overviewTab.attributes('role')).toBe('tab')
    await overviewTab.trigger('keydown', { key: 'ArrowRight' })
    await wrapper.vm.$nextTick()

    const arenaTab = wrapper.get('[data-tab="lmarena"]')
    expect(arenaTab.attributes('aria-selected')).toBe('true')
    const tableRegion = wrapper.get('[data-testid="lmarena-scroll"]')
    expect(tableRegion.classes()).toContain('overflow-x-auto')
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('First')
    expect(rows[1].text()).toContain('Second')
    expect(rows[0].get('td').text()).toBe('1')
    expect(rows[1].get('td').text()).toBe('2')
    expect(rows[1].text()).toContain('—')
    expect(wrapper.text()).toContain('12,345')
    expect(wrapper.text()).toContain('Leaderboard model vote sum')
    expect(wrapper.get('[data-testid="lmarena-fetched-at"]').text()).toContain('Fetched at')
    expect(wrapper.text()).toMatch(/2026|Jul/)
  })

  it('shows the LMArena fetch time without source-update metadata', async () => {
    const wrapper = mount(DegradationRadarTabs, {
      props: { latest: degradationLatest, lmarena: { ...lmarena, last_updated_at: null } },
    })

    await wrapper.get('[data-tab="lmarena"]').trigger('click')

    expect(wrapper.find('[data-testid="lmarena-source-updated"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Source updated')
    expect(wrapper.get('[data-testid="lmarena-fetched-at"]').text()).not.toContain('—')
  })

  it('shows isolated trend loading/error/empty states without exposing raw errors', () => {
    const wrapper = mount(DegradationRadarTabs, {
      props: {
        latest: { ...degradationLatest, sources_last_updated: { aa: now } },
        lmarena,
        trend: {
          model_slug: 'model-a',
          metric: 'intelligence_index',
          days: 90,
          data_points: [{ date: '2026-07-13', value: 91 }],
          stale: true,
        },
        trendLoading: true,
        trendError: 'https://internal/token=secret',
      },
    })

    expect(wrapper.text()).toContain('Loading trend')
    expect(wrapper.text()).toContain('Unable to load trend')
    expect(wrapper.find('[data-testid="degradation-trend-stale"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Trend data may be outdated')
    expect(wrapper.text()).not.toContain('internal')
    expect(wrapper.find('[data-testid="line-chart"]').exists()).toBe(true)
    const accessibleTrend = wrapper.get('[data-testid="degradation-trend-data"]')
    expect(accessibleTrend.text()).toContain('Intelligence index')
    expect(accessibleTrend.text()).toContain('91')
    expect(accessibleTrend.text()).toMatch(/2026|Jul/)
    expect(accessibleTrend.element.closest('[aria-live]')).toBeNull()
    const statuses = wrapper.findAll('[role="status"]').map((status) => status.text()).join(' ')
    expect(statuses).toContain('Loading trend')
    expect(statuses).toContain('Unable to load trend')
  })

  it('keeps latest loading/error local to overview while LMArena remains usable', async () => {
    const wrapper = mount(DegradationRadarTabs, {
      props: {
        latest: degradationLatest,
        latestLoading: true,
        latestError: 'https://internal/latest-secret',
        lmarena,
      },
    })

    expect(wrapper.get('[data-testid="degradation-latest-loading"]').text()).toContain('Loading')
    expect(wrapper.get('[data-testid="degradation-latest-error"]').text()).toContain('Unable')
    expect(wrapper.find('[data-testid="radar-chart"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('latest-secret')

    await wrapper.get('[data-tab="lmarena"]').trigger('click')
    expect(wrapper.find('[data-testid="degradation-latest-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="degradation-latest-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="lmarena-scroll"]').exists()).toBe(true)
  })

  it('keeps LMArena loading/error local to its tab while overview remains usable', async () => {
    const wrapper = mount(DegradationRadarTabs, {
      props: {
        latest: degradationLatest,
        lmarena,
        lmarenaLoading: true,
        lmarenaError: 'postgres://internal/lmarena-secret',
      },
    })

    expect(wrapper.find('[data-testid="radar-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="lmarena-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="lmarena-error"]').exists()).toBe(false)

    await wrapper.get('[data-tab="lmarena"]').trigger('click')
    expect(wrapper.get('[data-testid="lmarena-loading"]').text()).toContain('Loading')
    expect(wrapper.get('[data-testid="lmarena-error"]').text()).toContain('Unable')
    expect(wrapper.find('[data-testid="lmarena-scroll"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('lmarena-secret')
  })
})
