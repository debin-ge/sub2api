import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VIPReconcileJob, VIPReconcilePreview } from '@/types'
import VIPReconcileView from '../VIPReconcileView.vue'

const {
  getVIPReconcilePreview,
  createVIPReconcileJob,
  getVIPReconcileJob,
  showError,
  showSuccess,
  stepUpRun,
} = vi.hoisted(() => ({
  getVIPReconcilePreview: vi.fn(),
  createVIPReconcileJob: vi.fn(),
  getVIPReconcileJob: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  stepUpRun: vi.fn(),
}))

vi.mock('@/api', () => ({
  adminAPI: {
    users: {
      getVIPReconcilePreview,
      createVIPReconcileJob,
      getVIPReconcileJob,
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({
    visible: { value: false },
    blockedReason: { value: '' },
    run: stepUpRun,
  }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        key.replace(/\{(\w+)\}/g, (_, token) => String(params?.[token] ?? `{${token}}`)),
    }),
  }
})

const previewResponse = (
  overrides: Partial<VIPReconcilePreview> = {}
): VIPReconcilePreview => ({
  as_of: '2026-07-29T12:00:00Z',
  total: 1,
  stats: {
    eligibility_repair: 1,
    effective_change: 1,
    force_off_unchanged: 0,
    invalid_order: 0,
    deleted_user: 0,
  },
  items: [{
    category: 'EFFECTIVE_CHANGE',
    user_id: 8,
    order_id: 81,
    completed_at: '2026-07-01T00:00:00Z',
    current_vip_mode: 'AUTO',
    current_is_vip: false,
    will_effective_change: true,
  }],
  next_cursor: '',
  ...overrides,
})

const reconcileJob = (
  overrides: Partial<VIPReconcileJob> = {}
): VIPReconcileJob => ({
  id: 17,
  request_id: 'request-17',
  actor_user_id: 1,
  actor_snapshot: 'admin@example.com',
  reason: 'repair historical drift',
  status: 'queued',
  as_of: '2026-07-29T12:00:00Z',
  cursor_completed_at: '0001-01-01T00:00:00Z',
  cursor_order_id: 0,
  scanned: 0,
  eligibility_repaired: 0,
  effective_changed: 0,
  force_off_unchanged: 0,
  already_correct: 0,
  deleted: 0,
  invalid_order: 0,
  failed: 0,
  attempts: 0,
  last_error: '',
  started_at: null,
  finished_at: null,
  created_at: '2026-07-29T12:00:00Z',
  updated_at: '2026-07-29T12:00:00Z',
  ...overrides,
})

const mountView = async () => {
  const wrapper = mount(VIPReconcileView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TotpStepUpDialog: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('admin VIPReconcileView', () => {
  beforeEach(() => {
    vi.useRealTimers()
    localStorage.clear()
    getVIPReconcilePreview.mockReset()
    createVIPReconcileJob.mockReset()
    getVIPReconcileJob.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    stepUpRun.mockReset()
    stepUpRun.mockImplementation((action: () => Promise<unknown>) => action())
    getVIPReconcilePreview.mockResolvedValue(previewResponse())
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
    localStorage.clear()
  })

  it('passes opaque cursors through unchanged and keeps pagination on the fixed as-of snapshot', async () => {
    getVIPReconcilePreview
      .mockResolvedValueOnce(previewResponse({ next_cursor: 'opaque-page-2' }))
      .mockResolvedValueOnce(previewResponse({
        items: [{
          category: 'FORCE_OFF_UNCHANGED',
          user_id: 9,
          order_id: 92,
          completed_at: '2026-07-02T00:00:00Z',
          current_vip_mode: 'FORCE_OFF',
          current_is_vip: false,
          will_effective_change: false,
        }],
        next_cursor: '',
      }))
      .mockResolvedValueOnce(previewResponse({ next_cursor: 'opaque-page-2' }))

    const wrapper = await mountView()

    expect(getVIPReconcilePreview).toHaveBeenNthCalledWith(1, '', 50)
    expect(wrapper.get('[data-testid="preview-as-of"]').exists()).toBe(true)

    await wrapper.get('[data-testid="preview-next"]').trigger('click')
    await flushPromises()

    expect(getVIPReconcilePreview).toHaveBeenNthCalledWith(2, 'opaque-page-2', 50)
    expect(wrapper.get('[data-testid="preview-previous"]').attributes('disabled')).toBeUndefined()

    await wrapper.get('[data-testid="preview-previous"]').trigger('click')
    await flushPromises()

    expect(getVIPReconcilePreview).toHaveBeenNthCalledWith(3, '', 50)
    expect(getVIPReconcilePreview.mock.results.every((result) => result.type === 'return')).toBe(true)
    wrapper.unmount()
  })

  it('requires a reason and wraps idempotent job creation in step-up', async () => {
    const succeeded = reconcileJob({ status: 'succeeded', finished_at: '2026-07-29T12:01:00Z' })
    createVIPReconcileJob.mockResolvedValue({ job_id: succeeded.id, job: succeeded })
    const wrapper = await mountView()
    const generatedRequestId = (wrapper.get('[data-testid="request-id"]').element as HTMLInputElement).value

    await wrapper.get('[data-testid="create-job"]').trigger('click')
    expect(createVIPReconcileJob).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="reconcile-reason"]').attributes('aria-invalid')).toBe('true')

    await wrapper.get('[data-testid="reconcile-reason"]').setValue('  approved historical repair  ')
    await wrapper.get('[data-testid="create-job"]').trigger('click')
    await flushPromises()

    expect(generatedRequestId).not.toBe('')
    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(createVIPReconcileJob).toHaveBeenCalledWith({
      request_id: generatedRequestId,
      reason: 'approved historical repair',
    })
    expect(localStorage.getItem('admin-vip-reconcile-last-job-id')).toBe(String(succeeded.id))
    wrapper.unmount()
  })

  it('polls active jobs and stops after a terminal status', async () => {
    vi.useFakeTimers()
    const queued = reconcileJob({ status: 'queued' })
    createVIPReconcileJob.mockResolvedValue({ job_id: queued.id, job: queued })
    getVIPReconcileJob.mockResolvedValue(reconcileJob({
      status: 'succeeded',
      scanned: 12,
      finished_at: '2026-07-29T12:02:00Z',
    }))
    const wrapper = await mountView()

    await wrapper.get('[data-testid="reconcile-reason"]').setValue('scheduled repair')
    await wrapper.get('[data-testid="create-job"]').trigger('click')
    await flushPromises()

    await vi.advanceTimersByTimeAsync(2500)
    await flushPromises()
    expect(getVIPReconcileJob).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(15000)
    await flushPromises()
    expect(getVIPReconcileJob).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('resumes a failed job with exactly the original request ID and reason', async () => {
    const failed = reconcileJob({
      status: 'failed',
      request_id: 'stable-retry-id',
      reason: 'same audited reason',
      last_error: 'database timeout',
    })
    const queued = reconcileJob({
      status: 'queued',
      request_id: failed.request_id,
      reason: failed.reason,
      attempts: 1,
    })
    createVIPReconcileJob
      .mockResolvedValueOnce({ job_id: failed.id, job: failed })
      .mockResolvedValueOnce({ job_id: queued.id, job: queued })
    const wrapper = await mountView()

    await wrapper.get('[data-testid="reconcile-reason"]').setValue(failed.reason)
    await wrapper.get('[data-testid="create-job"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="resume-job"]').trigger('click')
    await flushPromises()

    expect(createVIPReconcileJob).toHaveBeenCalledTimes(2)
    expect(createVIPReconcileJob.mock.calls[1]).toEqual([{
      request_id: failed.request_id,
      reason: failed.reason,
    }])
    expect(stepUpRun).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('stops automatic polling for an unknown job status', async () => {
    vi.useFakeTimers()
    const unknown = reconcileJob({ status: 'awaiting_manual_review' })
    createVIPReconcileJob.mockResolvedValue({ job_id: unknown.id, job: unknown })
    const wrapper = await mountView()

    await wrapper.get('[data-testid="reconcile-reason"]').setValue('manual review run')
    await wrapper.get('[data-testid="create-job"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="unknown-job-status"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(15000)
    await flushPromises()
    expect(getVIPReconcileJob).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
