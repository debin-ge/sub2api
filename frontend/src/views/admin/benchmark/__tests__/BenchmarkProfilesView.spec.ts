import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import BenchmarkProfilesView from '../BenchmarkProfilesView.vue'
import { adminAPI } from '@/api/admin'

const showError = vi.fn()
const showSuccess = vi.fn()

vi.mock('@/api/admin', () => ({
  adminAPI: {
    benchmark: {
      listProfiles: vi.fn(),
      listTargets: vi.fn(),
      listTasks: vi.fn(),
      createProfile: vi.fn(),
      previewProfile: vi.fn(),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  const locale = ref('en-GB')
  return {
    ...actual,
    useI18n: () => ({
      locale,
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'benchmark.fallback.target') return `PROFILE_TARGET_${params?.id as number}`
        if (key === 'benchmark.admin.profiles.previewCards.target') return 'PROFILE_PREVIEW_TARGETS'
        if (key === 'benchmark.admin.profiles.previewCards.task') return 'PROFILE_PREVIEW_TASKS'
        if (key === 'benchmark.admin.profiles.previewCards.result') return 'PROFILE_PREVIEW_RESULTS'
        if (key === 'benchmark.admin.profiles.createSuccess') return 'PROFILE_CREATED_TOAST'
        return key
      },
    }),
  }
})

function mountView() {
  return mount(BenchmarkProfilesView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: { template: '<span />' },
      },
    },
  })
}

describe('BenchmarkProfilesView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    showError.mockReset()
    showSuccess.mockReset()
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

    vi.mocked(adminAPI.benchmark.listProfiles).mockResolvedValue({
      items: [
        {
          id: 5,
          suite_id: 1,
          name: 'Daily profile',
          description: 'Core suite',
          target_ids: [11],
          task_types: ['reasoning'],
          task_scale: 'medium',
          enabled: true,
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    vi.mocked(adminAPI.benchmark.listTargets).mockResolvedValue({
      items: [
        {
          id: 11,
          model_name: '',
          channel_id: 101,
          display_name: '',
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
      items: [
        {
          id: 21,
          suite_id: 1,
          title: 'Reasoning sample',
          type: 'reasoning',
          prompt: 'Solve it',
          verifier_type: 'exact_match',
          weight: 1,
          min_scale: 'small',
          public_prompt: true,
          enabled: true,
        },
        {
          id: 22,
          suite_id: 1,
          title: 'Coding sample',
          type: 'coding',
          prompt: 'Write code',
          verifier_type: 'exact_match',
          weight: 1,
          min_scale: 'medium',
          public_prompt: false,
          enabled: true,
        },
      ],
      total: 2,
      page: 1,
      page_size: 100,
      pages: 1,
    })
    vi.mocked(adminAPI.benchmark.previewProfile).mockResolvedValue({
      target_count: 1,
      task_count: 2,
      result_count: 2,
      task_types: ['reasoning', 'coding'],
      task_scale: 'custom',
      ranking_basis: 'ability_score_only',
      estimated_cost: 0.12,
      selected_task_ids: [21, 22],
      selected_target_ids: [11],
    })
    vi.mocked(adminAPI.benchmark.createProfile).mockResolvedValue({
      id: 6,
      suite_id: 1,
      name: 'Created profile',
      target_ids: [11],
      task_types: ['reasoning', 'coding'],
      task_scale: 'custom',
      enabled: true,
    })
  })

  it('previews a profile with multiple task types and a custom task scale', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="preview-profile-select"]').setValue('5')
    await wrapper.get('[data-test="preview-task-type-reasoning"]').setValue(true)
    await wrapper.get('[data-test="preview-task-type-coding"]').setValue(true)
    await wrapper.get('[data-test="preview-scale-custom"]').setValue(true)
    await wrapper.get('[data-test="preview-button"]').trigger('click')
    await flushPromises()

    expect(adminAPI.benchmark.previewProfile).toHaveBeenCalledWith(5, {
      target_ids: [11],
      task_types: ['reasoning', 'coding'],
      task_scale: 'custom',
      task_count_limit: null,
      per_type_limit: {},
      difficulty_filter: [],
      tag_filter: [],
      selection_seed: null,
    })
    expect(wrapper.text()).toContain('PROFILE_TARGET_11')
    expect(wrapper.text()).toContain('PROFILE_PREVIEW_TARGETS')
    expect(wrapper.text()).toContain('PROFILE_PREVIEW_TASKS')
    expect(wrapper.text()).toContain('PROFILE_PREVIEW_RESULTS')
  })

  it('creates a profile with selected task types and scale', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="profile-name-input"]').setValue('Created profile')
    await wrapper.get('[data-test="profile-suite-input"]').setValue('1')
    await wrapper.get('[data-test="profile-target-11"]').setValue(true)
    await wrapper.get('[data-test="profile-task-type-reasoning"]').setValue(true)
    await wrapper.get('[data-test="profile-task-type-coding"]').setValue(true)
    await wrapper.get('[data-test="profile-scale-custom"]').setValue(true)
    await wrapper.get('[data-test="create-profile-button"]').trigger('click')
    await flushPromises()

    expect(adminAPI.benchmark.createProfile).toHaveBeenCalledWith({
      suite_id: 1,
      name: 'Created profile',
      description: '',
      target_ids: [11],
      task_types: ['reasoning', 'coding'],
      task_scale: 'custom',
      task_count_limit: null,
      per_type_limit: {},
      difficulty_filter: [],
      tag_filter: [],
      sampling_strategy: 'random',
      selection_seed: null,
      enabled: true,
    })
    expect(showSuccess).toHaveBeenCalledWith('PROFILE_CREATED_TOAST')
  })

  it('offers every task scale option', async () => {
    const wrapper = mountView()
    await flushPromises()

    for (const scale of ['small', 'medium', 'full', 'custom']) {
      expect(wrapper.get(`[data-test="profile-scale-${scale}"]`).exists()).toBe(true)
      expect(wrapper.get(`[data-test="preview-scale-${scale}"]`).exists()).toBe(true)
    }
  })
})
