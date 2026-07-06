import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { listTargets, createTarget, listTasks, createTask, applyStandardTasks, listRuns, createRun, createStandardRun, getRun, listRunResults, getRunScores, listSchedules, createSchedule, getAllGroups, getModelsListCandidates, routerPush } = vi.hoisted(() => ({
  listTargets: vi.fn(),
  createTarget: vi.fn(),
  listTasks: vi.fn(),
  createTask: vi.fn(),
  applyStandardTasks: vi.fn(),
  listRuns: vi.fn(),
  createRun: vi.fn(),
  createStandardRun: vi.fn(),
  getRun: vi.fn(),
  listRunResults: vi.fn(),
  getRunScores: vi.fn(),
  listSchedules: vi.fn(),
  createSchedule: vi.fn(),
  getAllGroups: vi.fn(),
  getModelsListCandidates: vi.fn(),
  routerPush: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    benchmark: {
      listTargets,
      createTarget,
      updateTarget: vi.fn(),
      deleteTarget: vi.fn(),
      listTasks,
      createTask,
      applyStandardTasks,
      updateTask: vi.fn(),
      deleteTask: vi.fn(),
      listRuns,
      createRun,
      createStandardRun,
      getRun,
      listRunResults,
      getRunScores,
      cancelRun: vi.fn(),
      processRun: vi.fn(),
      processDueRuns: vi.fn(),
      listSchedules,
      createSchedule,
      updateSchedule: vi.fn(),
      deleteSchedule: vi.fn(),
      triggerSchedule: vi.fn(),
    },
    groups: {
      getAll: getAllGroups,
      getModelsListCandidates,
    },
  },
}))

const showSuccess = vi.fn()
const showError = vi.fn()
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }),
  }
})

const stubs = {
  AppLayout: { template: '<div><slot /></div>' },
  DataTable: { template: '<div><slot name="empty" /></div>' },
  EmptyState: true,
  Pagination: true,
  Icon: true,
  'router-link': { template: '<a><slot /></a>' },
}

const rowTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id" :data-test="'table-row-' + row.id">
        <slot name="cell-id" :row="row" />
        <slot name="cell-status" :row="row" />
        <slot name="cell-actions" :row="row" />
      </div>
      <slot v-if="!data || data.length === 0" name="empty" />
    </div>
  `,
}

const emptyPage = { items: [], total: 0, page: 1, page_size: 20, pages: 1 }

beforeEach(() => {
  vi.clearAllMocks()
  listTargets.mockResolvedValue(emptyPage)
  listTasks.mockResolvedValue(emptyPage)
  listRuns.mockResolvedValue(emptyPage)
  getRun.mockResolvedValue({ id: 1, status: 'completed', planned_target_count: 0, planned_task_count: 0, planned_result_count: 0 })
  listRunResults.mockResolvedValue([])
  getRunScores.mockResolvedValue([])
  listSchedules.mockResolvedValue(emptyPage)
  getAllGroups.mockResolvedValue([])
  getModelsListCandidates.mockResolvedValue([])
})

describe('BenchmarkTargetsView', () => {
  it('creates a target from a source group and linked model option', async () => {
    const { default: View } = await import('../BenchmarkTargetsView.vue')
    createTarget.mockResolvedValue({ id: 1, model_name: 'gpt-4.1', channel_id: 3 })
    getAllGroups.mockResolvedValue([
      { id: 20, name: 'Anthropic subscription', platform: 'anthropic', status: 'active', subscription_type: 'subscription' },
      { id: 30, name: 'OpenAI subscription', platform: 'openai', status: 'active', subscription_type: 'subscription' },
      { id: 40, name: 'OpenAI standard', platform: 'openai', status: 'active', subscription_type: 'standard' },
    ])
    getModelsListCandidates.mockImplementation((groupId: number) => {
      if (groupId === 20) return Promise.resolve(['claude-sonnet-4', 'claude-opus-4'])
      if (groupId === 30) return Promise.resolve(['gpt-4.1', 'gpt-4.1-mini'])
      return Promise.resolve(['fallback-model'])
    })
    const wrapper = mount(View, { global: { stubs } })
    await flushPromises()

    expect(wrapper.find('[data-test="target-source-subscription"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="target-source-group"]').exists()).toBe(true)

    const groupSelect = wrapper.find('[data-test="target-group-select"]')
    expect(groupSelect.exists()).toBe(true)
    expect(wrapper.find('option[value="20"]').exists()).toBe(true)
    expect(wrapper.find('option[value="30"]').exists()).toBe(true)
    expect(wrapper.find('option[value="40"]').exists()).toBe(true)

    await groupSelect.setValue('30')
    await flushPromises()

    expect(getModelsListCandidates).toHaveBeenCalledWith(30, 'openai')
    const modelSelect = wrapper.find('[data-test="target-model-select"]')
    expect(modelSelect.exists()).toBe(true)
    expect(wrapper.find('option[value="gpt-4.1"]').exists()).toBe(true)
    await modelSelect.setValue('gpt-4.1')

    await wrapper.find('[data-test="target-submit-button"]').trigger('submit')
    await flushPromises()

    expect(getAllGroups).toHaveBeenCalledTimes(1)
    expect(createTarget).toHaveBeenCalledTimes(1)
    const payload = createTarget.mock.calls[0][0]
    expect(payload.model_name).toBe('gpt-4.1')
    expect(payload.group_id).toBe(30)
    expect(payload.channel_id).toBeUndefined()
    // simplified schema: no supported_task_types / budgets / metadata
    expect(payload).not.toHaveProperty('supported_task_types')
    expect(payload).not.toHaveProperty('per_run_budget')
  })
})

describe('BenchmarkTasksView', () => {
  it('creates a task without suite/min_scale fields', async () => {
    const { default: View } = await import('../BenchmarkTasksView.vue')
    createTask.mockResolvedValue({ id: 1, title: 'T', type: 'reasoning' })
    const wrapper = mount(View, { global: { stubs } })
    await flushPromises()

    await wrapper.find('[data-test="task-title-input"]').setValue('Reasoning sample')
    await wrapper.find('[data-test="task-prompt-input"]').setValue('Solve it')
    await wrapper.find('[data-test="task-verifier-type-input"]').setValue('exact_match')
    await wrapper.find('[data-test="task-submit-button"]').trigger('submit')
    await flushPromises()

    expect(createTask).toHaveBeenCalledTimes(1)
    const payload = createTask.mock.calls[0][0]
    expect(payload.title).toBe('Reasoning sample')
    expect(payload.type).toBe('reasoning')
    expect(payload).not.toHaveProperty('suite_id')
    expect(payload).not.toHaveProperty('min_scale')
    expect(payload).toHaveProperty('sort_order')
  })
})

describe('BenchmarkTasksView standard task set', () => {
  it('applies the standard task set and reloads tasks', async () => {
    const { default: View } = await import('../BenchmarkTasksView.vue')
    applyStandardTasks.mockResolvedValue({ created_count: 6, existing_count: 0, enabled_count: 6, tasks: [] })
    const wrapper = mount(View, { global: { stubs } })
    await flushPromises()

    await wrapper.find('[data-test="apply-standard-tasks-button"]').trigger('click')
    await flushPromises()

    expect(applyStandardTasks).toHaveBeenCalledTimes(1)
    expect(listTasks).toHaveBeenCalledTimes(2)
    expect(showSuccess).toHaveBeenCalled()
  })

  it('does not apply the standard task set twice while pending', async () => {
    const { default: View } = await import('../BenchmarkTasksView.vue')
    applyStandardTasks.mockReturnValue(new Promise(() => {}))
    const wrapper = mount(View, { global: { stubs } })
    await flushPromises()

    const button = wrapper.find('[data-test="apply-standard-tasks-button"]')
    await Promise.all([button.trigger('click'), button.trigger('click')])
    await flushPromises()

    expect(applyStandardTasks).toHaveBeenCalledTimes(1)
  })
})

describe('BenchmarkRunsView', () => {
  it('creates a standard run by default and uses advanced target options when selected', async () => {
    const { default: View } = await import('../BenchmarkRunsView.vue')
    listTargets.mockResolvedValue({ ...emptyPage, items: [{ id: 7, model_name: 'gpt-4.1', channel_id: 3, display_name: 'GPT', enabled: true }], total: 1 })
    createStandardRun.mockResolvedValue({ id: 9, status: 'queued' })
    const wrapper = mount(View, { global: { stubs } })
    await flushPromises()

    await wrapper.find('[data-test="create-standard-run-button"]').trigger('click')
    await flushPromises()

    expect(createStandardRun).toHaveBeenCalledWith(undefined)
    expect(routerPush).toHaveBeenCalledWith('/admin/benchmark/runs/9')

    await wrapper.find('[data-test="standard-run-advanced-toggle"]').trigger('click')
    await wrapper.find('[data-test="run-task-count"]').setValue('5')
    await wrapper.find('[data-test="run-target-7"]').setValue(true)
    await wrapper.find('[data-test="run-process-immediately"]').setValue(false)
    await wrapper.find('[data-test="create-standard-run-button"]').trigger('click')
    await flushPromises()

    expect(createStandardRun).toHaveBeenCalledTimes(2)
    expect(createStandardRun.mock.calls.at(-1)?.[0]).toEqual({
      target_ids: [7],
      task_count: 5,
      process_immediately: false,
    })
    expect(routerPush).toHaveBeenLastCalledWith('/admin/benchmark/runs/9')
  })

  it('shows an explicit detail action for each run', async () => {
    const { default: View } = await import('../BenchmarkRunsView.vue')
    listRuns.mockResolvedValue({
      ...emptyPage,
      items: [{
        id: 41,
        status: 'completed',
        trigger_type: 'manual',
        task_count: 6,
        planned_target_count: 1,
        planned_task_count: 6,
        planned_result_count: 6,
      }],
      total: 1,
    })
    const wrapper = mount(View, {
      global: {
        stubs: {
          ...stubs,
          DataTable: rowTableStub,
        },
      },
    })
    await flushPromises()

    const detailLink = wrapper.find('[data-test="run-detail-41"]')
    expect(detailLink.exists()).toBe(true)
    expect(detailLink.attributes('to')).toBe('/admin/benchmark/runs/41')
  })

  it('does not submit duplicate standard runs while creation is pending', async () => {
    const { default: View } = await import('../BenchmarkRunsView.vue')
    createStandardRun.mockReturnValue(new Promise(() => {}))
    const wrapper = mount(View, { global: { stubs } })
    await flushPromises()

    await wrapper.find('[data-test="standard-run-form"]').trigger('submit')
    await wrapper.find('[data-test="standard-run-form"]').trigger('submit')
    await flushPromises()

    expect(createStandardRun).toHaveBeenCalledTimes(1)
  })
})

describe('BenchmarkSchedulesView', () => {
  it('creates a schedule with cron, target_ids and task_count', async () => {
    const { default: View } = await import('../BenchmarkSchedulesView.vue')
    createSchedule.mockResolvedValue({ id: 1, name: 'Nightly' })
    const wrapper = mount(View, { global: { stubs } })
    await flushPromises()

    await wrapper.find('[data-test="schedule-name"]').setValue('Nightly')
    await wrapper.find('[data-test="schedule-cron"]').setValue('0 3 * * *')
    await wrapper.find('[data-test="schedule-task-count"]').setValue('12')
    await wrapper.find('[data-test="save-schedule-button"]').trigger('submit')
    await flushPromises()

    expect(createSchedule).toHaveBeenCalledTimes(1)
    const payload = createSchedule.mock.calls[0][0]
    expect(payload.name).toBe('Nightly')
    expect(payload.cron_expr).toBe('0 3 * * *')
    expect(payload.task_count).toBe(12)
    expect(payload).not.toHaveProperty('profile_id')
  })
})

describe('BenchmarkRunDetailView', () => {
  it('auto-refreshes active runs until they reach a terminal status', async () => {
    vi.useFakeTimers()
    try {
      const { default: View } = await import('../BenchmarkRunDetailView.vue')
      getRun
        .mockResolvedValueOnce({ id: 41, status: 'running', planned_target_count: 1, planned_task_count: 6, planned_result_count: 6 })
        .mockResolvedValueOnce({ id: 41, status: 'completed', planned_target_count: 1, planned_task_count: 6, planned_result_count: 6 })
      const wrapper = mount(View, { props: { runId: 41 }, global: { stubs } })
      await flushPromises()

      expect(getRun).toHaveBeenCalledTimes(1)

      await vi.advanceTimersByTimeAsync(2500)
      await flushPromises()

      expect(getRun).toHaveBeenCalledTimes(2)

      await vi.advanceTimersByTimeAsync(2500)
      await flushPromises()

      expect(getRun).toHaveBeenCalledTimes(2)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
