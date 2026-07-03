import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { listTargets, createTarget, listTasks, createTask, listRuns, createRun, listSchedules, createSchedule } = vi.hoisted(() => ({
  listTargets: vi.fn(),
  createTarget: vi.fn(),
  listTasks: vi.fn(),
  createTask: vi.fn(),
  listRuns: vi.fn(),
  createRun: vi.fn(),
  listSchedules: vi.fn(),
  createSchedule: vi.fn(),
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
      updateTask: vi.fn(),
      deleteTask: vi.fn(),
      listRuns,
      createRun,
      cancelRun: vi.fn(),
      processRun: vi.fn(),
      processDueRuns: vi.fn(),
      listSchedules,
      createSchedule,
      updateSchedule: vi.fn(),
      deleteSchedule: vi.fn(),
      triggerSchedule: vi.fn(),
    },
  },
}))

const showSuccess = vi.fn()
const showError = vi.fn()
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError }),
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

const emptyPage = { items: [], total: 0, page: 1, page_size: 20, pages: 1 }

beforeEach(() => {
  vi.clearAllMocks()
  listTargets.mockResolvedValue(emptyPage)
  listTasks.mockResolvedValue(emptyPage)
  listRuns.mockResolvedValue(emptyPage)
  listSchedules.mockResolvedValue(emptyPage)
})

describe('BenchmarkTargetsView', () => {
  it('creates a target with the simplified field set', async () => {
    const { default: View } = await import('../BenchmarkTargetsView.vue')
    createTarget.mockResolvedValue({ id: 1, model_name: 'gpt-4.1', channel_id: 3 })
    const wrapper = mount(View, { global: { stubs } })
    await flushPromises()

    await wrapper.find('[data-test="target-model-name-input"]').setValue('gpt-4.1')
    await wrapper.find('[data-test="target-channel-id-input"]').setValue('3')
    await wrapper.find('[data-test="target-submit-button"]').trigger('submit')
    await flushPromises()

    expect(createTarget).toHaveBeenCalledTimes(1)
    const payload = createTarget.mock.calls[0][0]
    expect(payload.model_name).toBe('gpt-4.1')
    expect(payload.channel_id).toBe(3)
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

describe('BenchmarkRunsView', () => {
  it('creates a run with target_ids and task_count', async () => {
    const { default: View } = await import('../BenchmarkRunsView.vue')
    listTargets.mockResolvedValue({ ...emptyPage, items: [{ id: 7, model_name: 'gpt-4.1', channel_id: 3, display_name: 'GPT', enabled: true }], total: 1 })
    createRun.mockResolvedValue({ id: 9, status: 'queued' })
    const wrapper = mount(View, { global: { stubs } })
    await flushPromises()

    await wrapper.find('[data-test="run-task-count"]').setValue('5')
    await wrapper.find('[data-test="run-target-7"]').setValue(true)
    await wrapper.find('[data-test="create-run-button"]').trigger('click')
    await flushPromises()

    expect(createRun).toHaveBeenCalledTimes(1)
    const payload = createRun.mock.calls[0][0]
    expect(payload.task_count).toBe(5)
    expect(payload.target_ids).toEqual([7])
    // simplified: no profile_id / override
    expect(payload).not.toHaveProperty('profile_id')
    expect(payload).not.toHaveProperty('override')
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
