import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import RadarRankTable from '../RadarRankTable.vue'
import type { BenchmarkRadarTarget } from '@/types/benchmark'

const translations: Record<string, string> = {
  'benchmark.public.table.abilityScore': 'RADAR_SCORE_LABEL',
  'benchmark.public.table.latency': 'RADAR_LATENCY_LABEL',
  'benchmark.public.table.successRate': 'RADAR_SUCCESS_LABEL',
  'benchmark.public.table.token': 'RADAR_TOKEN_LABEL',
  'benchmark.public.table.cost': 'RADAR_COST_LABEL',
  'benchmark.public.table.insufficientSample': 'RADAR_INSUFFICIENT_SAMPLE',
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
        if (key === 'benchmark.public.table.p95Prefix') return 'RADAR_P95'
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
    dimensions: {
      reasoning: 95,
      coding: 88,
    },
    score_basis: {
      planned_tasks: 20,
      scored_tasks: 18,
      invalid_tasks: 2,
      coverage_rate: 0.9,
      confidence_level: 'high',
      insufficient_sample: false,
    },
    metrics: {
      success_rate: 0.96,
      latency_p50_ms: 1234,
      latency_p95_ms: 2410,
      avg_total_tokens: 1820,
      estimated_cost: 0.037,
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

    expect(wrapper.text()).toContain('RADAR_LATENCY_LABEL')
    expect(wrapper.text()).toContain('RADAR_SUCCESS_LABEL')
    expect(wrapper.text()).toContain('RADAR_TOKEN_LABEL')
    expect(wrapper.text()).toContain('RADAR_COST_LABEL')
    expect(wrapper.text()).toContain(`${new Intl.NumberFormat('de-DE', {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(1.234)}s`)
    expect(wrapper.text()).toContain(new Intl.NumberFormat('de-DE', { style: 'percent', minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(0.96))
    expect(wrapper.text()).toContain('1.820')
    expect(wrapper.text()).toContain(new Intl.NumberFormat('de-DE', {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 4,
      maximumFractionDigits: 4,
    }).format(0.037))
  })

  it('localizes insufficient sample and channel fallback labels', () => {
    const wrapper = mount(RadarRankTable, {
      props: {
        targets: [
          target({
            channel_name: '',
            score_basis: {
              planned_tasks: 20,
              scored_tasks: 2,
              invalid_tasks: 18,
              coverage_rate: 0.1,
              confidence_level: 'low',
              insufficient_sample: true,
            },
          }),
        ],
      },
    })

    expect(wrapper.text()).toContain('RADAR_INSUFFICIENT_SAMPLE')
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
