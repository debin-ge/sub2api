import { describe, it, expect, beforeEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpsRequestDetailsModal from '../OpsRequestDetailsModal.vue'

const mockListRequestDetails = vi.fn()

vi.mock('@/api/admin/ops', () => ({
  opsAPI: {
    listRequestDetails: (...args: any[]) => mockListRequestDetails(...args),
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showWarning: vi.fn(),
  }),
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

describe('OpsRequestDetailsModal custom time range', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListRequestDetails.mockResolvedValue({ items: [], total: 0 })
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

    expect(mockListRequestDetails).toHaveBeenCalledTimes(1)
    expect(mockListRequestDetails).toHaveBeenCalledWith(expect.objectContaining({
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
