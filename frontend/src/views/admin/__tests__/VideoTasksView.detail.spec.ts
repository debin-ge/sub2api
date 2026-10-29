import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import VideoTasksView from '../VideoTasksView.vue'

const { api, app } = vi.hoisted(() => ({
  api: { overview: vi.fn(), listTasks: vi.fn(), listResources: vi.fn(), listCallbacks: vi.fn(), getTask: vi.fn(), listEvents: vi.fn() },
  app: { showError: vi.fn(), showSuccess: vi.fn() },
}))
vi.mock('@/api/admin/videos', () => ({ default: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const task = {
  id: 1, public_id: 'video_a', version: 7, lease_epoch: 3, source: 'managed', user_id: 42, provider: 'openai',
  operation: 'generate', endpoint: '/v1/videos', generation_state: 'completed', billing_state: 'captured', delete_state: 'none',
  requested_model: 'sora-2', public_model: 'sora-2', channel_model: 'sora-2', upstream_model: 'sora-2',
  currency: 'USD', input_manifest: [], request_attributes: {}, price_snapshot: {}, provider_cost_snapshot: {},
  usage_snapshot: {}, response_metadata: {}, content_variants: ['video'], provider_access: { configured: false },
  callback_configured: false, poll_attempts: 1, submit_attempts: 1, created_at: '2026-09-06T00:00:00Z', updated_at: '2026-09-06T00:01:00Z',
}

const healthySpool = {
  enabled: false, active_sessions: 0, current_bytes: 0, max_bytes: 0, utilization: 0,
  orphan_candidates: 0, last_sweep_result: 'disabled', cleanup_failure_count: 0,
}
const overview = {
  tasks_by_generation: { in_progress: 2 }, tasks_by_billing: { held: 3, capture_pending: 1 },
  tasks_by_delete: {}, callbacks_by_status: { failed: 4 }, held_amount: 12.5, unmatched_webhooks: 6,
  queue_status: 'available', queue: { ready: 0, delayed: 0, active: 0 }, spool: healthySpool,
}

type Field = { label: string; value: string }
type Section = { key: string; fields: Field[] }

async function mountView(overviewValue: unknown = overview, detail: unknown = task) {
  api.overview.mockResolvedValue(overviewValue)
  api.getTask.mockResolvedValue(detail)
  const wrapper = mount(VideoTasksView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' }, BaseDialog: { template: '<div><slot /></div>' }, Pagination: true, Icon: true,
  } } })
  await flushPromises()
  const state = (wrapper.vm.$ as unknown as { setupState: {
    detailSections: Section[]
    healthAlerts: string[]
    overviewMetrics: { key: string }[]
    openTask: (task: unknown) => Promise<void>
  } }).setupState
  return { wrapper, state }
}

beforeEach(() => {
  vi.resetAllMocks()
  api.listTasks.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
  api.listEvents.mockResolvedValue({ items: [] })
})

function fieldLabels(sections: Section[]) {
  return sections.flatMap(section => section.fields.map(field => field.label))
}

describe('video task detail trimming', () => {
  it('collapses the four-model chain into one row when every layer agrees', async () => {
    const { wrapper, state } = await mountView()
    try {
      await state.openTask(task)
      const labels = fieldLabels(state.detailSections)
      expect(labels).toContain('admin.videos.columns.model')
      expect(labels).not.toContain('admin.videos.detail.requestedModel')
      expect(labels).not.toContain('admin.videos.detail.upstreamModel')
    } finally { wrapper.unmount() }
  })

  it('keeps every layer when a mapping rewrote the model on the way upstream', async () => {
    const remapped = { ...task, channel_model: 'sora-2-pro', upstream_model: 'sora-2-pro' }
    const { wrapper, state } = await mountView(overview, remapped)
    try {
      await state.openTask(remapped)
      const labels = fieldLabels(state.detailSections)
      for (const label of ['requestedModel', 'publicModel', 'channelModel', 'upstreamModel']) {
        expect(labels).toContain(`admin.videos.detail.${label}`)
      }
      expect(labels).not.toContain('admin.videos.columns.model')
    } finally { wrapper.unmount() }
  })

  it('drops empty rows instead of rendering a wall of dashes', async () => {
    const { wrapper, state } = await mountView()
    try {
      await state.openTask(task)
      const fields = state.detailSections.flatMap(section => section.fields)
      expect(fields.length).toBeGreaterThan(0)
      expect(fields.every(field => field.value !== '-' && field.value !== '')).toBe(true)
      // 上游任务 ID 缺席时那一行整条不出现，而不是渲染成 '-'
      expect(fieldLabels(state.detailSections)).not.toContain('admin.videos.columns.upstreamTask')
    } finally { wrapper.unmount() }
  })

  it('never shows the optimistic-lock and lease machinery operators cannot act on', async () => {
    const { wrapper, state } = await mountView()
    try {
      await state.openTask(task)
      const labels = fieldLabels(state.detailSections)
      for (const label of ['version', 'leaseEpoch', 'leaseExpiresAt', 'endpoint', 'inputCount', 'currency']) {
        expect(labels).not.toContain(`admin.videos.detail.${label}`)
      }
    } finally { wrapper.unmount() }
  })
})

describe('video overview band', () => {
  it('keeps only signals the tab badges do not already carry', async () => {
    const { wrapper, state } = await mountView()
    try {
      expect(state.overviewMetrics.map(metric => metric.key)).toEqual(['active', 'billing', 'held'])
    } finally { wrapper.unmount() }
  })

  it('stays silent while the spool and queue are healthy', async () => {
    const { wrapper, state } = await mountView()
    try {
      expect(state.healthAlerts).toEqual([])
    } finally { wrapper.unmount() }
  })

  it('speaks up for every condition that needs a human', async () => {
    const degraded = { ...overview, queue_status: 'unavailable', spool: {
      ...healthySpool, enabled: true, current_bytes: 9, max_bytes: 10, utilization: 0.9,
      orphan_candidates: 2, cleanup_failure_count: 3, last_sweep_result: 'error',
    } }
    const { wrapper, state } = await mountView(degraded)
    try {
      expect(state.healthAlerts).toEqual([
        'admin.videos.health.queueUnavailable',
        'admin.videos.health.diskTight',
        'admin.videos.health.cleanupFailing',
        'admin.videos.health.sweepFailed',
      ])
    } finally { wrapper.unmount() }
  })
})

describe('video task upstream cost', () => {
  const vendorTask = {
    ...task,
    provider: 'wavespeed',
    usage_snapshot: { upstream_cost_usd: 0.42, billable_units: 3, inference_ms: 1234, inference_seconds: 1.5 },
    provider_cost_snapshot: {
      upstream_quote_usd: 0.5, configured_base_usd: 0.4, upstream_quote_exceeds_price: true,
      upstream_quote_error: 'quote timeout', account_rate_multiplier: 1.25,
    },
  }

  it('renders an upstream cost section from the usage and provider cost snapshots', async () => {
    const { wrapper, state } = await mountView(overview, vendorTask)
    try {
      await state.openTask(vendorTask)
      const section = state.detailSections.find(item => item.key === 'upstream-cost')
      expect(section).toBeDefined()
      const labels = section!.fields.map(field => field.label)
      for (const label of [
        'upstreamActualCost', 'billableUnits', 'inferenceMs', 'inferenceSeconds',
        'upstreamQuote', 'configuredBase', 'upstreamQuoteError', 'accountRateMultiplier',
      ]) {
        expect(labels).toContain(`admin.videos.detail.${label}`)
      }
      expect(section!.fields.find(field => field.label === 'admin.videos.detail.accountRateMultiplier')?.value).toBe('1.2500x')
      expect(section!.fields.find(field => field.label === 'admin.videos.detail.upstreamQuoteError')?.value).toBe('quote timeout')
      expect(wrapper.find('[data-test="video-detail-upstream-cost"]').exists()).toBe(true)
      // 上游报价高于本地配置单价时，整段顶部挂一条醒目的警示
      const warning = wrapper.find('[data-test="video-detail-upstream-cost-warning"]')
      expect(warning.exists()).toBe(true)
      expect(warning.text()).toContain('admin.videos.detail.upstreamQuoteExceeds')
    } finally { wrapper.unmount() }
  })

  it('omits the upstream cost section when no provider reported cost data', async () => {
    const { wrapper, state } = await mountView()
    try {
      await state.openTask(task)
      expect(state.detailSections.some(section => section.key === 'upstream-cost')).toBe(false)
      expect(wrapper.find('[data-test="video-detail-upstream-cost"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="video-detail-upstream-cost-warning"]').exists()).toBe(false)
    } finally { wrapper.unmount() }
  })
})

it('shows Grok settlement, sync, legacy attribution and upstream costs separately', async () => {
  const grok = { ...task, public_id: 'grok_video_a', provider: 'grok', source: 'grok_relay',
    provider_task_id: 'upstream-uuid', generation_state: 'completed', billing_state: 'none',
    settlement_status: 'settled', sync_status: 'finished', imported_from_legacy: true,
    last_polled_at: '2026-10-28T00:00:00Z', allowed_actions: [],
    provider_cost_snapshot: { cost_in_usd_ticks: 6900000000 }, actual_cost: 1.25,
  }
  const { wrapper, state } = await mountView(overview, grok)
  try {
    await state.openTask(grok)
    const labels = fieldLabels(state.detailSections)
    expect(labels).toContain('admin.videos.grok.settlement')
    expect(labels).toContain('admin.videos.grok.lastPolled')
    expect(labels).toContain('admin.videos.grok.imported')
    const fields = state.detailSections.flatMap(section => section.fields)
    expect(fields.find(field => field.label === 'admin.videos.columns.billing')?.value).toBe('admin.videos.grok.settled')
    expect(wrapper.find('button[title="admin.videos.actions.retryDelete"]').exists()).toBe(false)
  } finally { wrapper.unmount() }
})
