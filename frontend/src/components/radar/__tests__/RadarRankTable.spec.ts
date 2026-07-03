import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import RadarRankTable from '../RadarRankTable.vue'
import type { BenchmarkRadarTarget } from '@/types/benchmark'

const translations: Record<string, string> = {
  'benchmark.public.table.abilityScore': 'RADAR_SCORE_LABEL',
  'benchmark.public.table.passed': 'RADAR_PASSED_LABEL',
  'benchmark.public.table.latency': 'RADAR_LATENCY_LABEL',
  'benchmark.public.table.token': 'RADAR_TOKEN_LABEL',
  'benchmark.public.table.cost': 'RADAR_COST_LABEL',
  'benchmark.public.empty.title': 'RADAR_EMPTY_TITLE',
  'benchmark.public.empty.description': 'RADAR_EMPTY_DESCRIPTION',
}

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  const locale = ref('de-DE')
  return {
    ...actual,
    useI18n: () => ({
      locale,
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'benchmark.fallback.channel') return `RADAR_CHANNEL_${params?.id as number}`
        return translations[key] ?? key
      },
    }),
  }
})

function target(overrides: Partial<BenchmarkRadarTarget> = {}): BenchmarkRadarTarget {
  return {
    rank: 1,
    model: 'claude-3-5-sonnet',
    channel_id: 7,
    channel_name: 'Anthropic',
    display_name: 'Claude 3.5 Sonnet',
    overall_score: 91.25,
    passed_count: 18,
    total_count: 20,
    dimensions: {
      reasoning: 95,
      coding: 88,
    },
    metrics: {
      avg_latency_ms: 1234,
      avg_total_tokens: 1820,
      total_cost: 0.037,
    },
    ...overrides,
  }
}

describe('RadarRankTable', () => {
  it('renders a row for each target by display_name', () => {
    const wrapper = mount(RadarRankTable, {
      props: {
        targets: [
          target({ rank: 1, display_name: 'Claude 3.5 Sonnet' }),
          target({ rank: 2, display_name: 'GPT-4o', model: 'gpt-4o' }),
        ],
      },
    })

    expect(wrapper.text()).toContain('Claude 3.5 Sonnet')
    expect(wrapper.text()).toContain('GPT-4o')
  })

  it('labels the primary score as 能力分 and never as 综合分', () => {
    const wrapper = mount(RadarRankTable, {
      props: {
        targets: [target()],
      },
    })

    expect(wrapper.text()).toContain('RADAR_SCORE_LABEL')
    expect(wrapper.text()).not.toContain('综合分')
  })

  it('shows translated runtime columns and locale-aware number formatting', () => {
    const wrapper = mount(RadarRankTable, {
      props: {
        targets: [target()],
      },
    })

    expect(wrapper.text()).toContain('RADAR_PASSED_LABEL')
    expect(wrapper.text()).toContain('RADAR_LATENCY_LABEL')
    expect(wrapper.text()).toContain('RADAR_TOKEN_LABEL')
    expect(wrapper.text()).toContain('RADAR_COST_LABEL')
    // avg latency 1234ms -> "1,234s" in de-DE locale (>=1000 formats as seconds)
    expect(wrapper.text()).toContain(`${new Intl.NumberFormat('de-DE', {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(1.234)}s`)
    // passed count summary
    expect(wrapper.text()).toContain('18')
    expect(wrapper.text()).toContain('20')
    expect(wrapper.text()).toContain('1.820')
    expect(wrapper.text()).toContain(new Intl.NumberFormat('de-DE', {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 4,
      maximumFractionDigits: 4,
    }).format(0.037))
  })

  it('localizes channel fallback label when channel name is missing', () => {
    const wrapper = mount(RadarRankTable, {
      props: {
        targets: [
          target({ channel_name: '' }),
        ],
      },
    })

    expect(wrapper.text()).toContain('RADAR_CHANNEL_7')
  })

  it('renders an empty state for empty data without an error stack', () => {
    const wrapper = mount(RadarRankTable, {
      props: {
        targets: [],
      },
    })

    expect(wrapper.text()).toContain('RADAR_EMPTY_TITLE')
    expect(wrapper.text()).toContain('RADAR_EMPTY_DESCRIPTION')
    expect(wrapper.text()).not.toContain('TypeError')
    expect(wrapper.text()).not.toContain('Error:')
  })
})
