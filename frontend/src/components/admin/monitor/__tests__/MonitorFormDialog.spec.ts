import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import MonitorFormDialog from '../MonitorFormDialog.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    showError: vi.fn(),
    showSuccess: vi.fn()
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitorTemplate: {
      list: vi.fn().mockResolvedValue({ items: [] })
    },
    channelMonitor: {
      create: vi.fn(),
      update: vi.fn()
    }
  }
}))

vi.mock('@/api/keys', () => ({
  keysAPI: {
    list: vi.fn()
  }
}))

vi.mock('@/api/groups', () => ({
  userGroupsAPI: {
    getUserGroupRates: vi.fn()
  }
}))

describe('MonitorFormDialog', () => {
  it('offers all supported monitor providers', () => {
    const wrapper = mount(MonitorFormDialog, {
      props: {
        show: true,
        monitor: null
      },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
          Toggle: { template: '<input type="checkbox" />' },
          Select: { template: '<div />' },
          ModelTagInput: { template: '<div />' },
          MonitorKeyPickerDialog: true,
          MonitorAdvancedRequestConfig: true,
          ProviderIcon: true
        }
      }
    })

    const labels = wrapper.findAll('button[aria-pressed]').map((button) => button.text())

    expect(labels).toEqual(expect.arrayContaining([
      'monitorCommon.providers.anthropic',
      'monitorCommon.providers.openai',
      'monitorCommon.providers.gemini',
      'monitorCommon.providers.minimax',
      'monitorCommon.providers.glm',
      'monitorCommon.providers.kimi',
      'monitorCommon.providers.deepseek'
    ]))
    expect(labels).toHaveLength(7)
  })
})
