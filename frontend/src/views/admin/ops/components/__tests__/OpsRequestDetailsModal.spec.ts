import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { OpsDashboardOverview } from '@/api/admin/ops'
import { flushPromises, mount, shallowMount } from '@vue/test-utils'
import OpsDashboardHeader from '../OpsDashboardHeader.vue'
import OpsRequestDetailsModal from '../OpsRequestDetailsModal.vue'

const { listRequestDetails, viewport } = vi.hoisted(() => ({
  listRequestDetails: vi.fn(),
  viewport: { desktop: true },
}))

vi.mock('@vueuse/core', () => ({ useMediaQuery: () => ref(viewport.desktop) }))
vi.mock('@/api/admin/ops', () => ({ opsAPI: { listRequestDetails } }))
vi.mock('@/api', () => ({ adminAPI: { groups: { getAll: vi.fn().mockResolvedValue([]) } } }))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showWarning: vi.fn(),
  }),
  useAdminSettingsStore: () => ({ opsRealtimeMonitoringEnabled: false }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn(async () => true),
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, any>) => {
        if (key === 'admin.ops.requestDetails.rangeLabel') return `窗口：${params?.range ?? ''}`
        if (key === 'admin.ops.requestDetails.rangeHours') return `${params?.n ?? ''} 小时`
        if (key === 'admin.ops.requestDetails.rangeMinutes') return `${params?.n ?? ''} 分钟`
        if (key === 'admin.ops.timeRange.custom') return '自定义'
        return key
      },
    }),
  }
})

const BaseDialogStub = {
  props: ['show', 'title', 'width'],
  template: '<div v-if="show"><slot /></div>',
}

const PaginationStub = {
  template: '<div class="pagination-stub" />',
}

async function openDetails(sort: 'duration_desc' | 'ttft_desc') {
  const wrapper = mount(OpsRequestDetailsModal, {
    props: { modelValue: false, timeRange: '1h', preset: { title: 'Details', sort } },
    global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, Pagination: true } },
  })
  await wrapper.setProps({ modelValue: true })
  await flushPromises()
  return wrapper
}

describe('OpsRequestDetailsModal custom time range', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    viewport.desktop = true
    listRequestDetails.mockResolvedValue({ items: [], total: 0 })
  })

  it('uses explicit custom start/end times instead of recalculating a 1h window', async () => {
    const customStartTime = '2026-06-16T01:00:00.000Z'
    const customEndTime = '2026-06-16T03:30:00.000Z'
    const wrapper = mount(OpsRequestDetailsModal, {
      props: {
        modelValue: false,
        timeRange: 'custom',
        customStartTime,
        customEndTime,
        preset: {
          title: '请求明细',
          kind: 'all',
          sort: 'created_at_desc',
        },
        platform: 'openai',
        groupId: 7,
      } as any,
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Pagination: PaginationStub,
        },
      },
    })

    await wrapper.setProps({ modelValue: true })
    await flushPromises()

    expect(listRequestDetails).toHaveBeenCalledTimes(1)
    expect(listRequestDetails).toHaveBeenCalledWith(expect.objectContaining({
      start_time: customStartTime,
      end_time: customEndTime,
      platform: 'openai',
      group_id: 7,
    }))
  })

  it('does not display the fallback 1h label while custom times are unavailable', async () => {
    const wrapper = mount(OpsRequestDetailsModal, {
      props: {
        modelValue: true,
        timeRange: 'custom',
        preset: {
          title: '请求明细',
          kind: 'all',
          sort: 'created_at_desc',
        },
      } as any,
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Pagination: PaginationStub,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('窗口：自定义')
    expect(wrapper.text()).not.toContain('窗口：1 小时')
  })
})

describe('Ops request latency details', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    viewport.desktop = true
    listRequestDetails.mockResolvedValue({
      items: [
        { kind: 'success', created_at: '2026-09-10T00:00:00Z', duration_ms: 12000, first_token_ms: 800 },
        { kind: 'success', created_at: '2026-09-10T00:00:01Z', duration_ms: 9000, first_token_ms: 0 },
        { kind: 'success', created_at: '2026-09-10T00:00:02Z', duration_ms: 5000, first_token_ms: null },
      ],
      total: 3,
    })
  })

  it('opens the TTFT card with first-token sorting and successful requests', async () => {
    const wrapper = shallowMount(OpsDashboardHeader, {
      props: { overview: {} as OpsDashboardOverview, platform: '', groupId: null, timeRange: '1h', queryMode: 'auto', loading: false, lastUpdated: null },
    })
    await flushPromises()
    const button = wrapper.findAll('button').find((item) =>
      item.text() === 'admin.ops.requestDetails.details' &&
      item.element.parentElement?.textContent?.includes('TTFT'),
    )
    expect(button).toBeDefined()
    await button!.trigger('click')
    expect(wrapper.emitted('openRequestDetails')).toEqual([[
      { title: 'admin.ops.ttftLabel', kind: 'success', sort: 'ttft_desc' },
    ]])
    wrapper.unmount()
  })

  it.each([true, false])('shows TTFT rather than total duration (desktop: %s)', async (desktop) => {
    viewport.desktop = desktop
    const wrapper = await openDetails('ttft_desc')
    expect(listRequestDetails).toHaveBeenCalledWith(expect.objectContaining({ sort: 'ttft_desc' }))
    expect(wrapper.text()).toContain('admin.ops.ttftLabel')
    expect(wrapper.text()).toContain('800 ms')
    expect(wrapper.text()).toContain('0 ms')
    expect(wrapper.text()).not.toContain('12000 ms')
    expect(wrapper.text()).not.toContain('9000 ms')
    expect(wrapper.text()).not.toContain('5000 ms')
    if (desktop) expect(wrapper.findAll('tbody tr')[2].findAll('td')[4].text()).toBe('-')
    else expect(wrapper.text()).toContain('admin.ops.ttftLabel: -')
    wrapper.unmount()
  })

  it('keeps total duration for duration details', async () => {
    const wrapper = await openDetails('duration_desc')
    expect(listRequestDetails).toHaveBeenCalledWith(expect.objectContaining({ sort: 'duration_desc' }))
    expect(wrapper.text()).toContain('admin.ops.requestDetails.table.duration')
    expect(wrapper.text()).toContain('12000 ms')
    expect(wrapper.text()).not.toContain('800 ms')
    wrapper.unmount()
  })
})
