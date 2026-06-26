import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import BenchmarkDashboardView from '../BenchmarkDashboardView.vue'
import { adminAPI } from '@/api/admin'
import { radarAPI } from '@/api/radar'

const showError = vi.fn()

vi.mock('@/api/admin', () => ({
  adminAPI: {
    benchmark: {
      listRuns: vi.fn(),
      listTargets: vi.fn(),
      listTasks: vi.fn(),
      listProfiles: vi.fn(),
      getRunScores: vi.fn(),
    },
  },
}))

vi.mock('@/api/radar', () => ({
  radarAPI: {
    getCurrent: vi.fn(),
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  const messages: Record<string, string> = {
    'benchmark.admin.dashboard.publicSnapshot': 'DASH_PUBLIC_SNAPSHOT',
    'benchmark.admin.dashboard.noPublicSnapshot': 'DASH_PUBLIC_SNAPSHOT_UNAVAILABLE',
    'benchmark.admin.dashboard.columns.token': 'DASH_TOKEN_LABEL',
    'benchmark.admin.dashboard.columns.cost': 'DASH_COST_LABEL',
    'benchmark.admin.dashboard.loadError': 'DASH_LOAD_FAILED',
  }
  const locale = ref('de-DE')
  return {
    ...actual,
    useI18n: () => ({
      locale,
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'benchmark.fallback.run') return `RUN_LABEL_${params?.id as number}`
        if (key === 'benchmark.fallback.channel') return `CHANNEL_LABEL_${params?.id as number}`
        if (key === 'benchmark.fallback.target') return `TARGET_LABEL_${params?.id as number}`
        return messages[key] ?? key
      },
    }),
  }
})

function mountView() {
  return mount(BenchmarkDashboardView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: { template: '<span />' },
      },
    },
  })
}

describe('BenchmarkDashboardView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    showError.mockReset()
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: true,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })

    vi.mocked(adminAPI.benchmark.listRuns)
      .mockResolvedValueOnce({
        items: [
          {
            id: 10,
            suite_id: 1,
            profile_id: 5,
            status: 'running',
            trigger_type: 'manual',
            task_scale: 'medium',
            task_types: ['reasoning'],
            planned_target_count: 2,
            planned_task_count: 3,
            planned_result_count: 6,
          },
        ],
        total: 10,
        page: 1,
        page_size: 5,
        pages: 2,
      })
      .mockResolvedValueOnce({
        items: [
          {
            id: 9,
            suite_id: 1,
            profile_id: 5,
            status: 'completed',
            trigger_type: 'manual',
            task_scale: 'medium',
            task_types: ['reasoning'],
            planned_target_count: 2,
            planned_task_count: 3,
            planned_result_count: 6,
          },
        ],
        total: 1,
        page: 1,
        page_size: 1,
        pages: 1,
      })
    vi.mocked(adminAPI.benchmark.listTargets).mockResolvedValue({
      items: [
        {
          id: 11,
          model_name: 'gpt-4.1',
          channel_id: 101,
          display_name: 'GPT-4.1',
          enabled: true,
          public_visible: true,
          sort_order: 1,
        },
      ],
      total: 1,
      page: 1,
      page_size: 100,
      pages: 1,
    })
    vi.mocked(adminAPI.benchmark.listTasks).mockResolvedValue({
      items: [],
      total: 3,
      page: 1,
      page_size: 1,
      pages: 3,
    })
    vi.mocked(adminAPI.benchmark.listProfiles).mockResolvedValue({
      items: [],
      total: 2,
      page: 1,
      page_size: 1,
      pages: 2,
    })
    vi.mocked(adminAPI.benchmark.getRunScores).mockResolvedValue([
      {
        id: 201,
        run_id: 9,
        run_target_id: 501,
        overall_score: 91.5,
        planned_tasks: 3,
        scored_tasks: 3,
        invalid_tasks: 0,
        coverage_rate: 1,
        confidence_level: 'high',
        insufficient_sample: false,
        success_rate: 1,
        latency_p50_ms: 880,
        avg_total_tokens: 140,
        estimated_cost: 0.02,
        edges: {
          run_target: {
            id: 501,
            target_id: 11,
            model_name: 'gpt-4.1',
            channel_id: 101,
            channel_name_snapshot: 'OpenAI',
          },
        },
      },
    ])
    vi.mocked(radarAPI.getCurrent).mockResolvedValue({
      ranking_basis: 'ability_score_only',
      published_at: '2026-06-24T09:15:00Z',
      latest_run: {
        id: 9,
        suite_id: 1,
        profile_id: 5,
        status: 'completed',
        completed_at: '2026-06-24T08:30:00Z',
      },
      targets: [],
    })
  })

  it('shows the latest public snapshot publish time from public radar', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(radarAPI.getCurrent).toHaveBeenCalled()
    expect(wrapper.text()).toContain('DASH_PUBLIC_SNAPSHOT')
    expect(wrapper.text()).toContain('2026')
    expect(wrapper.text()).toContain('RUN_LABEL_9')
    expect(wrapper.text()).not.toContain('DASH_PUBLIC_SNAPSHOT_UNAVAILABLE')
  })

  it('uses the latest completed run for ranking and shows independent token and cost metrics', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(adminAPI.benchmark.listRuns).toHaveBeenNthCalledWith(1, { page: 1, page_size: 5 })
    expect(adminAPI.benchmark.listRuns).toHaveBeenNthCalledWith(2, {
      status: 'completed',
      page: 1,
      page_size: 1,
    })
    expect(adminAPI.benchmark.getRunScores).toHaveBeenCalledWith(9)
    expect(wrapper.text()).toContain('gpt-4.1 · OpenAI')
    expect(wrapper.text()).toContain('DASH_TOKEN_LABEL')
    expect(wrapper.text()).toContain('140')
    expect(wrapper.text()).toContain('DASH_COST_LABEL')
    expect(wrapper.text()).toContain(new Intl.NumberFormat('de-DE', {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 4,
      maximumFractionDigits: 4,
    }).format(0.02))
  })

  it('uses translated error fallback when benchmark dashboard loading throws a non-Error value', async () => {
    vi.mocked(adminAPI.benchmark.listRuns).mockReset().mockRejectedValue('boom')

    mountView()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('DASH_LOAD_FAILED')
  })
})
