import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import BenchmarkRunsView from '../BenchmarkRunsView.vue'
import BenchmarkRunDetailView from '../BenchmarkRunDetailView.vue'
import { adminAPI } from '@/api/admin'

vi.mock('@/api/admin', () => ({
  adminAPI: {
    benchmark: {
      listRuns: vi.fn(),
      listProfiles: vi.fn(),
      listTargets: vi.fn(),
      createRun: vi.fn(),
      getRun: vi.fn(),
      listRunResults: vi.fn(),
      getRunScores: vi.fn(),
      publishRun: vi.fn(),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

function mountRunsView() {
  return mount(BenchmarkRunsView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: { template: '<span />' },
      },
    },
  })
}

function mountDetailView(runId = 9) {
  return mount(BenchmarkRunDetailView, {
    props: { runId },
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: { template: '<span />' },
      },
    },
  })
}

describe('BenchmarkRunsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
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

    vi.mocked(adminAPI.benchmark.listRuns).mockResolvedValue({
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
          finished_at: '2026-06-24T08:00:00Z',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    vi.mocked(adminAPI.benchmark.listProfiles).mockResolvedValue({
      items: [
        {
          id: 5,
          suite_id: 1,
          name: 'Daily profile',
          target_ids: [11, 12],
          task_types: ['reasoning'],
          task_scale: 'medium',
          enabled: true,
        },
      ],
      total: 1,
      page: 1,
      page_size: 100,
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
        {
          id: 12,
          model_name: 'tiny-model',
          channel_id: 102,
          display_name: 'Tiny Model',
          enabled: true,
          public_visible: true,
          sort_order: 2,
        },
      ],
      total: 2,
      page: 1,
      page_size: 100,
      pages: 1,
    })
    vi.mocked(adminAPI.benchmark.createRun).mockResolvedValue({
      id: 10,
      suite_id: 1,
      profile_id: 5,
      status: 'queued',
      trigger_type: 'manual',
      task_scale: 'medium',
      task_types: ['reasoning'],
      planned_target_count: 2,
      planned_task_count: 3,
      planned_result_count: 6,
    })
    vi.mocked(adminAPI.benchmark.getRun).mockResolvedValue({
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
      finished_at: '2026-06-24T08:00:00Z',
    })
    vi.mocked(adminAPI.benchmark.listRunResults).mockResolvedValue([
      {
        id: 100,
        run_id: 9,
        run_task_id: 21,
        run_target_id: 501,
        status: 'scored',
        score: 0.9,
        max_score: 1,
        normalized_score: 0.9,
        latency_ms: 880,
        prompt_tokens: 100,
        completion_tokens: 40,
        total_tokens: 140,
        estimated_cost: 0.02,
        attempt_count: 1,
        edges: {
          run_target: {
            id: 501,
            target_id: 11,
            model_name: 'gpt-4.1',
            channel_id: 101,
            display_name_snapshot: 'GPT-4.1 Snapshot',
          },
        },
      },
      {
        id: 101,
        run_id: 9,
        run_task_id: 22,
        run_target_id: 502,
        status: 'parse_error',
        score: null,
        max_score: 1,
        normalized_score: null,
        error_code: 'json_parse_failed',
        error_message: 'Invalid JSON',
        prompt_tokens: 100,
        completion_tokens: 10,
        total_tokens: 110,
        estimated_cost: 0.01,
        attempt_count: 1,
        edges: {
          run_target: {
            id: 502,
            target_id: 12,
            model_name: 'tiny-model',
            channel_id: 102,
            channel_name_snapshot: 'Relay B',
          },
        },
      },
    ])
    vi.mocked(adminAPI.benchmark.getRunScores).mockResolvedValue([
      {
        id: 201,
        run_id: 9,
        run_target_id: 501,
        overall_score: 91.5,
        dimension_scores: { reasoning: 91.5 },
        planned_tasks: 3,
        scored_tasks: 3,
        invalid_tasks: 0,
        coverage_rate: 1,
        confidence_level: 'high',
        insufficient_sample: false,
        success_rate: 1,
        latency_p50_ms: 880,
        latency_p95_ms: 1200,
        avg_total_tokens: 140,
        estimated_cost: 0.02,
        invalid_reason_breakdown: {},
        edges: {
          run_target: {
            id: 501,
            target_id: 11,
            model_name: 'gpt-4.1',
            channel_id: 101,
            display_name_snapshot: 'GPT-4.1 Snapshot',
          },
        },
      },
      {
        id: 202,
        run_id: 9,
        run_target_id: 502,
        overall_score: 37.2,
        dimension_scores: { reasoning: 37.2 },
        planned_tasks: 3,
        scored_tasks: 1,
        invalid_tasks: 2,
        coverage_rate: 0.33,
        confidence_level: 'low',
        insufficient_sample: true,
        success_rate: 0.33,
        latency_p50_ms: 1500,
        latency_p95_ms: 2200,
        avg_total_tokens: 110,
        estimated_cost: 0.01,
        invalid_reason_breakdown: {
          parse_error: 2,
          timeout: 1,
        },
        edges: {
          run_target: {
            id: 502,
            target_id: 12,
            model_name: 'tiny-model',
            channel_id: 102,
            channel_name_snapshot: 'Relay B',
          },
        },
      },
    ])
    vi.mocked(adminAPI.benchmark.publishRun).mockResolvedValue({ message: 'published' })
  })

  it('creates a run successfully', async () => {
    const wrapper = mountRunsView()
    await flushPromises()

    await wrapper.get('[data-test="run-profile-select"]').setValue('5')
    await wrapper.get('[data-test="create-run-button"]').trigger('click')
    await flushPromises()

    expect(adminAPI.benchmark.createRun).toHaveBeenCalledWith({
      profile_id: 5,
      trigger_type: 'manual',
    })
    expect(wrapper.text()).toContain('Run #10')
    expect(wrapper.text()).toContain('queued')
  })

  it('shows score snapshot, invalid reason breakdown, target details, and insufficient sample badge', async () => {
    const wrapper = mountDetailView()
    await flushPromises()

    expect(wrapper.text()).toContain('能力分')
    expect(wrapper.text()).toContain('91.5')
    expect(wrapper.text()).toContain('GPT-4.1 Snapshot')
    expect(wrapper.text()).toContain('tiny-model · Relay B')
    expect(wrapper.text()).toContain('样本不足')
    expect(wrapper.text()).toContain('Token')
    expect(wrapper.text()).toContain('Cost')
    expect(wrapper.text()).toContain('$0.0200')
    expect(wrapper.text()).toContain('parse_error')
    expect(wrapper.text()).toContain('timeout')
    expect(wrapper.text()).toContain('Result #101')
    expect(wrapper.text()).toContain('Invalid JSON')
  })

  it('publishes a completed run as a public snapshot', async () => {
    const wrapper = mountDetailView()
    await flushPromises()

    await wrapper.get('[data-test="publish-run-button"]').trigger('click')
    await flushPromises()

    expect(adminAPI.benchmark.publishRun).toHaveBeenCalledWith(9)
    expect(wrapper.text()).toContain('published')
  })
})
