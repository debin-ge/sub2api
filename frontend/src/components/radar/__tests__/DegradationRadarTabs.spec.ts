import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { degradationLatest, lmarena } from './fixtures'
import type { DegradationLatestDTO, DegradationModelDTO } from '@/types/radar'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ locale: ref('en'), t: (_key: string, fallback?: string) => fallback ?? _key }),
  }
})

vi.mock('chart.js', () => ({
  Chart: { register: vi.fn() },
  BarController: {},
  BarElement: {},
  CategoryScale: {},
  Legend: {},
  LinearScale: {},
  Tooltip: {},
}))

vi.mock('vue-chartjs', () => ({
  Bar: defineComponent({
    name: 'BenchmarkBarChart',
    props: { data: { type: Object, required: true }, options: { type: Object, required: true } },
    setup: () => () => h('div', { 'data-testid': 'benchmark-chart' }),
  }),
}))

import DegradationRadarTabs from '@/components/radar/DegradationRadarTabs.vue'

function model(index: number): DegradationModelDTO {
  return {
    slug: `model-${index}`,
    name: `Model ${index}`,
    vendor: index % 2 === 0 ? 'OpenAI' : 'Anthropic',
    intelligence_index: 100 - index,
    coding_index: 90 - index,
    agentic_index: 80 - index,
    price_input_per_1m: null,
    price_output_per_1m: null,
    last_updated_at: null,
    catalog_matches: [{ platform: 'openai', model_id: `provider/model-${index}-high` }],
  }
}

function latestWithModels(count: number): DegradationLatestDTO {
  const available = Array.from({ length: count }, (_, index) => model(index + 1))
  return {
    ...degradationLatest,
    available_models: available,
    models: available.slice(0, 6),
    default_model_slugs: available.slice(0, 6).map((item) => item.slug),
  }
}

describe('DegradationRadarTabs', () => {
  it('uses backend Top 6, all three AA indices, original AA names, and complete catalog IDs', async () => {
    const wrapper = mount(DegradationRadarTabs, { props: { latest: degradationLatest, lmarena } })
    await flushPromises()

    const radar = wrapper.findComponent({ name: 'BenchmarkBarChart' })
    const data = radar.props('data') as { datasets: Array<{ label: string; data: number[] }> }
    expect(data.datasets).toHaveLength(6)
    expect(data.datasets[0]).toEqual(expect.objectContaining({
      label: 'Model A',
      data: [91, 82, 73],
    }))
    expect(wrapper.findAll('#degradation-panel-overview article')).toHaveLength(6)
    expect(wrapper.text()).toContain('openai / model-a-high')
    expect(wrapper.text()).toContain('openai / model-a-low')
    expect(wrapper.get('[data-testid="aa-fetched-at"]').text()).not.toContain('—')
    expect(wrapper.find('[data-testid="aa-index-version"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Intelligence Index version')
    expect(wrapper.text()).not.toContain('Artificial Analysis official source')
    expect(wrapper.find('a[href="https://artificialanalysis.ai"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('never writes the selected models into the page URL', async () => {
    const wrapper = mount(DegradationRadarTabs, { props: { latest: degradationLatest, lmarena } })
    await flushPromises()

    const optionsInputs = wrapper.findAll('[data-testid="model-options"] input[type="checkbox"]')
    await optionsInputs[6].trigger('change')
    await flushPromises()

    expect(window.location.search).toBe('')
    wrapper.unmount()
  })

  it('searches AA and Model Plaza fields and caps selection at ten models', async () => {
    const latest = latestWithModels(12)
    const wrapper = mount(DegradationRadarTabs, { props: { latest, lmarena } })
    await flushPromises()
    const search = wrapper.get('[data-testid="model-search"]')

    await search.setValue('provider/model-11-high')
    let options = wrapper.findAll('[data-testid="model-options"] input[type="checkbox"]')
    expect(options).toHaveLength(1)
    expect(options[0].attributes('value')).toBe('model-11')

    await search.setValue('')
    options = wrapper.findAll('[data-testid="model-options"] input[type="checkbox"]')
    for (const index of [6, 7, 8, 9]) await options[index].trigger('change')
    await flushPromises()

    const data = wrapper.findComponent({ name: 'BenchmarkBarChart' }).props('data') as { datasets: unknown[] }
    expect(data.datasets).toHaveLength(10)
    expect(options[10].attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('uses backend metric, selection, default-count, and score-scale metadata', async () => {
    const latest = {
      ...latestWithModels(9),
      default_model_slugs: [],
      metrics: [{ key: 'coding_index' as const }],
      max_selected_models: 4,
      default_model_count: 3,
      score_min: 50,
      score_max: 90,
      score_step: 10,
    }
    const wrapper = mount(DegradationRadarTabs, { props: { latest, lmarena } })
    await flushPromises()

    const chart = wrapper.findComponent({ name: 'BenchmarkBarChart' })
    const data = chart.props('data') as { labels: string[]; datasets: Array<{ data: number[] }> }
    const options = chart.props('options') as {
      scales: { y: { min: number; max: number; ticks: { stepSize: number } } }
    }
    expect(data.labels).toEqual(['Coding'])
    expect(data.datasets).toHaveLength(3)
    expect(data.datasets[0].data).toEqual([89])
    expect(options.scales.y).toEqual(expect.objectContaining({ min: 50, max: 90 }))
    expect(options.scales.y.ticks.stepSize).toBe(10)
    expect(wrapper.text()).toContain('Compare up to 4 models')

    const optionsInputs = wrapper.findAll('[data-testid="model-options"] input[type="checkbox"]')
    await optionsInputs[3].trigger('change')
    await flushPromises()
    expect(chart.props('data')).toEqual(expect.objectContaining({
      datasets: expect.arrayContaining([expect.objectContaining({ label: 'Model 4' })]),
    }))
    expect(optionsInputs[4].attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('defaults to Top 6 and never permits an empty selection', async () => {
    const wrapper = mount(DegradationRadarTabs, { props: { latest: degradationLatest, lmarena } })
    await flushPromises()

    expect(wrapper.findComponent({ name: 'BenchmarkBarChart' }).props('data')).toEqual(
      expect.objectContaining({ datasets: expect.arrayContaining([expect.objectContaining({ label: 'Model A' })]) })
    )
    const selected = wrapper.findAll('[data-testid="selected-models"] button')
    for (let index = selected.length - 1; index > 0; index--) await selected[index].trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="selected-models"] button')).toHaveLength(1)
    expect(wrapper.get('[data-testid="selected-models"] button').attributes('disabled')).toBeDefined()
    wrapper.unmount()
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
    const wrapper = mount(DegradationRadarTabs, { props: { latest: degradationLatest, lmarena } })
    await flushPromises()
    const before = wrapper.findComponent({ name: 'BenchmarkBarChart' }).props('options') as {
      scales: { y: { grid: { color: string } } }
    }

    document.documentElement.classList.add('dark')
    callback?.([], {} as MutationObserver)
    await wrapper.vm.$nextTick()
    const after = wrapper.findComponent({ name: 'BenchmarkBarChart' }).props('options') as {
      scales: { y: { grid: { color: string } } }
    }
    expect(after.scales.y.grid.color).not.toBe(before.scales.y.grid.color)
    expect(observe).toHaveBeenCalledWith(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalled()
    vi.unstubAllGlobals()
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
    await flushPromises()
    await wrapper.get('[data-tab="overview"]').trigger('keydown', { key: 'ArrowRight' })
    await wrapper.vm.$nextTick()

    expect(wrapper.get('[data-tab="lmarena"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="lmarena-scroll"]').classes()).toContain('overflow-x-auto')
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('First')
    expect(rows[1].text()).toContain('Second')
    expect(rows[0].get('td').text()).toBe('1')
    expect(rows[1].get('td').text()).toBe('2')
    expect(wrapper.text()).toContain('12,345')
    wrapper.unmount()
  })

  it('keeps latest and LMArena loading or error states isolated and never exposes raw errors', async () => {
    const wrapper = mount(DegradationRadarTabs, {
      props: {
        latest: { ...degradationLatest, stale: true },
        latestLoading: true,
        latestError: 'https://internal/latest-secret',
        lmarena,
        lmarenaLoading: true,
        lmarenaError: 'postgres://internal/lmarena-secret',
      },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="degradation-latest-loading"]').text()).toContain('Loading')
    expect(wrapper.get('[data-testid="degradation-latest-error"]').text()).toContain('Unable')
    expect(wrapper.get('[data-testid="degradation-stale"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('latest-secret')
    expect(wrapper.find('[data-testid="lmarena-loading"]').exists()).toBe(false)

    await wrapper.get('[data-tab="lmarena"]').trigger('click')
    expect(wrapper.get('[data-testid="lmarena-loading"]').text()).toContain('Loading')
    expect(wrapper.get('[data-testid="lmarena-error"]').text()).toContain('Unable')
    expect(wrapper.text()).not.toContain('lmarena-secret')
    wrapper.unmount()
  })
})
