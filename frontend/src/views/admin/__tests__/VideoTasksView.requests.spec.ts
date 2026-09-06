import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import VideoTasksView from '../VideoTasksView.vue'

const { api, app } = vi.hoisted(() => ({
  api: { overview: vi.fn(), listTasks: vi.fn(), listResources: vi.fn(), listCallbacks: vi.fn(), getTask: vi.fn(), listEvents: vi.fn(), listBillingReviews: vi.fn(), listSubmissionReviews: vi.fn(), retryGet: vi.fn(), retryCallback: vi.fn() },
  app: { showError: vi.fn(), showSuccess: vi.fn() },
}))
vi.mock('@/api/admin/videos', () => ({ default: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 99 } }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const task = { id: 1, public_id: 'video_a', version: 1, source: 'managed', user_id: 42,
  provider: 'openai', operation: 'generate', generation_state: 'failed', billing_state: 'manual_review', delete_state: 'none',
  currency: 'USD', request_attributes: {}, price_snapshot: {}, provider_cost_snapshot: {}, usage_snapshot: {},
  response_metadata: {}, provider_access: { configured: false }, callback_configured: false }
const second = { ...task, id: 2, public_id: 'video_b' }
const callback = { id: 7, task_id: 1, event_id: 'video_evt_1', event_type: 'video.failed', payload: {},
  target_configured: true, status: 'quarantined', attempts: 2, next_attempt_at: '2026-09-06T00:00:00Z',
  expires_at: '2099-01-01T00:00:00Z', created_at: '2026-09-06T00:00:00Z', updated_at: '2026-09-06T00:00:00Z' }
const page = (number: number) => ({ items: [task, second], total: 100, page: number, page_size: 20 })

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

async function mountView() {
  const wrapper = mount(VideoTasksView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' }, BaseDialog: { name: 'BaseDialog', template: '<div><slot /></div>' }, Pagination: true, Icon: true,
  } } })
  await flushPromises()
  const state = (wrapper.vm.$ as unknown as { setupState: {
    selectedTask: typeof task | null
    pagination: { page: number }
    loading: boolean
    error: string
    openTask: (task: typeof second) => Promise<void>
    closeTask: () => void
    changePage: (page: number) => void
    selectTab: (tab: string) => void
    runTaskAction: (task: typeof second, action: string) => Promise<void>
    retryCallback: (callback: typeof callback) => Promise<void>
  } }).setupState
  return { wrapper, state }
}

describe('video task request ordering', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.overview.mockResolvedValue({})
    api.listTasks.mockResolvedValue(page(1))
    api.listResources.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    api.listCallbacks.mockResolvedValue({ items: [callback], total: 1, page: 1, page_size: 20 })
    api.getTask.mockImplementation((id: string) => Promise.resolve(id === task.public_id ? task : second))
    api.listEvents.mockResolvedValue({ items: [] })
    api.listBillingReviews.mockResolvedValue([])
    api.listSubmissionReviews.mockResolvedValue([])
  })

  it('coalesces repeated task actions while a mutation is pending', async () => {
    const pending = deferred<typeof task>()
    api.retryGet.mockReturnValue(pending.promise)
    const { wrapper, state } = await mountView()
    try {
      const first = state.runTaskAction(task, 'get')
      await state.runTaskAction(task, 'get')
      expect(api.retryGet).toHaveBeenCalledTimes(1)
      pending.resolve(task)
      await first
    } finally { wrapper.unmount() }
  })

  it('coalesces repeated callback retries while a mutation is pending', async () => {
    const pending = deferred<typeof callback>()
    api.retryCallback.mockReturnValue(pending.promise)
    const { wrapper, state } = await mountView()
    try {
      const first = state.retryCallback(callback)
      await state.retryCallback(callback)
      expect(api.retryCallback).toHaveBeenCalledTimes(1)
      pending.resolve({ ...callback, status: 'pending' })
      await first
    } finally { wrapper.unmount() }
  })

  it('ignores details from a closed task after another task is opened', async () => {
    const first = deferred<typeof task>()
    api.getTask.mockImplementation((id: string) => id === task.public_id ? first.promise : Promise.resolve(second))
    const { wrapper, state } = await mountView()
    try {
      const pending = state.openTask(task)
      wrapper.findComponent({ name: 'BaseDialog' }).vm.$emit('close')
      await state.openTask(second)
      first.resolve(task)
      await pending
      expect(state.selectedTask?.public_id).toBe(second.public_id)
    } finally { wrapper.unmount() }
  })

  it('does not reopen a closed dialog or show stale request errors', async () => {
    const first = deferred<typeof task>()
    api.getTask.mockReturnValue(first.promise)
    const { wrapper, state } = await mountView()
    try {
      const pending = state.openTask(task)
      state.closeTask()
      first.reject(new Error('late failure'))
      await pending
      expect(state.selectedTask).toBeNull()
      expect(app.showError).not.toHaveBeenCalled()
    } finally { wrapper.unmount() }
  })

  it('keeps the latest page and loading state when responses finish out of order', async () => {
    const { wrapper, state } = await mountView()
    const old = deferred<ReturnType<typeof page>>()
    const latest = deferred<ReturnType<typeof page>>()
    api.listTasks.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise)
    try {
      state.changePage(2)
      state.changePage(3)
      old.resolve(page(2))
      await flushPromises()
      expect(state.loading).toBe(true)
      expect(state.pagination.page).toBe(3)
      latest.resolve(page(3))
      await flushPromises()
      expect(state.loading).toBe(false)
      expect(state.pagination.page).toBe(3)
    } finally { wrapper.unmount() }
  })

  it('does not let an old tab response change the new tab pagination', async () => {
    const { wrapper, state } = await mountView()
    const old = deferred<ReturnType<typeof page>>()
    api.listTasks.mockReturnValueOnce(old.promise)
    try {
      state.changePage(2)
      state.selectTab('resources')
      await flushPromises()
      old.resolve(page(2))
      await flushPromises()
      expect(state.pagination.page).toBe(1)
    } finally { wrapper.unmount() }
  })

  it('does not reopen a previous task when its management action completes', async () => {
    const { wrapper, state } = await mountView()
    const action = deferred<typeof task>()
    api.retryGet.mockReturnValueOnce(action.promise)
    try {
      await state.openTask(task)
      const pending = state.runTaskAction(task, 'get')
      state.closeTask()
      await state.openTask(second)
      action.resolve(task)
      await pending
      expect(state.selectedTask?.public_id).toBe(second.public_id)
    } finally { wrapper.unmount() }
  })

  it('ignores in-flight detail errors after unmount', async () => {
    const { wrapper, state } = await mountView()
    const old = deferred<typeof task>()
    api.getTask.mockReturnValueOnce(old.promise)
    const pending = state.openTask(task)
    wrapper.unmount()
    old.reject(new Error('unmounted request'))
    await pending
    expect(app.showError).not.toHaveBeenCalled()
  })
})
