import { describe, it, expect, beforeEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpsErrorDetailsModal from '../OpsErrorDetailsModal.vue'

const mockListRequestErrors = vi.fn()
const mockListUpstreamErrors = vi.fn()

vi.mock('@/api/admin/ops', () => ({
  opsAPI: {
    listRequestErrors: (...args: any[]) => mockListRequestErrors(...args),
    listUpstreamErrors: (...args: any[]) => mockListUpstreamErrors(...args),
  },
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const BaseDialogStub = {
  props: ['show', 'title', 'width'],
  template: '<div v-if="show"><slot /></div>',
}

const SelectStub = {
  props: ['modelValue', 'options'],
  template: '<div class="select-stub" />',
}

const OpsErrorLogTableStub = {
  props: ['rows', 'total', 'loading', 'page', 'pageSize'],
  template: '<div class="error-log-table-stub" />',
}

describe('OpsErrorDetailsModal custom time range', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListRequestErrors.mockResolvedValue({ items: [], total: 0 })
    mockListUpstreamErrors.mockResolvedValue({ items: [], total: 0 })
  })

  it('sends custom start/end times and does not send time_range=custom', async () => {
    const customStartTime = '2026-06-16T01:00:00.000Z'
    const customEndTime = '2026-06-16T03:30:00.000Z'
    const wrapper = mount(OpsErrorDetailsModal, {
      props: {
        show: false,
        timeRange: 'custom',
        customStartTime,
        customEndTime,
        platform: 'openai',
        groupId: 7,
        errorType: 'request',
      } as any,
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          OpsErrorLogTable: OpsErrorLogTableStub,
        },
      },
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(mockListRequestErrors).toHaveBeenCalledTimes(1)
    const params = mockListRequestErrors.mock.calls[0][0]
    expect(params).toEqual(expect.objectContaining({
      start_time: customStartTime,
      end_time: customEndTime,
      platform: 'openai',
      group_id: 7,
    }))
    expect(params.time_range).toBeUndefined()
  })
})
