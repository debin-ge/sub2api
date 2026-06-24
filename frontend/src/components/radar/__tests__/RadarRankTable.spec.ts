import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import RadarRankTable from '../RadarRankTable.vue'
import type { BenchmarkRadarTarget } from '@/types/benchmark'

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

    expect(wrapper.text()).toContain('能力分')
    expect(wrapper.text()).not.toContain('综合分')
  })

  it('shows latency, success rate, token, and cost as separate runtime columns', () => {
    const wrapper = mount(RadarRankTable, {
      props: {
        targets: [target()],
      },
    })

    expect(wrapper.text()).toContain('延迟')
    expect(wrapper.text()).toContain('成功率')
    expect(wrapper.text()).toContain('Token')
    expect(wrapper.text()).toContain('成本')
    expect(wrapper.text()).toContain('1.23s')
    expect(wrapper.text()).toContain('96.0%')
    expect(wrapper.text()).toContain('1,820')
    expect(wrapper.text()).toContain('$0.0370')
  })

  it('marks targets with insufficient samples', () => {
    const wrapper = mount(RadarRankTable, {
      props: {
        targets: [
          target({
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

    expect(wrapper.text()).toContain('样本不足')
  })

  it('renders an empty state for empty data without an error stack', () => {
    const wrapper = mount(RadarRankTable, {
      props: {
        targets: [],
      },
    })

    expect(wrapper.text()).toContain('暂无 Radar 数据')
    expect(wrapper.text()).not.toContain('TypeError')
    expect(wrapper.text()).not.toContain('Error:')
  })
})
