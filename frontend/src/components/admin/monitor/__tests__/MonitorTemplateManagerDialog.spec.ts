import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import MonitorTemplateManagerDialog from '../MonitorTemplateManagerDialog.vue'

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
    showError: vi.fn(),
    showSuccess: vi.fn()
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitorTemplate: {
      list: vi.fn().mockResolvedValue({ items: [] }),
      create: vi.fn(),
      update: vi.fn(),
      del: vi.fn()
    }
  }
}))

describe('MonitorTemplateManagerDialog', () => {
  it('renders a template tab for every supported monitor provider', () => {
    const wrapper = mount(MonitorTemplateManagerDialog, {
      props: {
        show: true
      },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
          ConfirmDialog: true,
          Icon: true,
          MonitorAdvancedRequestConfig: true,
          MonitorTemplateApplyPickerDialog: true
        }
      }
    })

    const labels = wrapper.findAll('button[role="tab"]').map((button) => button.text())

    expect(labels).toEqual(expect.arrayContaining([
      'monitorCommon.providers.anthropic',
      'monitorCommon.providers.openai',
      'monitorCommon.providers.gemini',
      'monitorCommon.providers.minimax',
      'monitorCommon.providers.glm',
      'monitorCommon.providers.kimi',
      'monitorCommon.providers.deepseek',
      'monitorCommon.providers.windsurf',
      'monitorCommon.providers.opencode'
    ]))
    expect(labels).toHaveLength(9)
  })
})
