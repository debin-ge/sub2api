import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import MonitorFormDialog from '../MonitorFormDialog.vue'
import { PROVIDERS } from '@/constants/channelMonitor'

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
  it('offers all supported monitor providers except glm when creating', () => {
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

    const labels = wrapper.findAll('button[data-testid^="monitor-provider-"]').map((button) => button.text())

    expect(labels).toEqual(expect.arrayContaining([
      'monitorCommon.providers.anthropic',
      'monitorCommon.providers.openai',
      'monitorCommon.providers.gemini',
      'monitorCommon.providers.grok',
      'monitorCommon.providers.antigravity',
      'monitorCommon.providers.kimi',
      'monitorCommon.providers.zhipu',
      'monitorCommon.providers.deepseek',
      'monitorCommon.providers.minimax',
      'monitorCommon.providers.windsurf',
      'monitorCommon.providers.opencode_go'
    ]))
    // glm is a legacy platform ID (historically aliased to zhipu): creation is blocked,
    // so it must not appear when the dialog is mounted without an existing `monitor`.
    expect(labels).not.toContain('monitorCommon.providers.glm')
    expect(labels).toHaveLength(PROVIDERS.length - 1)
  })

  it('still offers glm when editing an existing legacy monitor', () => {
    const wrapper = mount(MonitorFormDialog, {
      props: {
        show: true,
        monitor: {
          id: 1,
          name: 'legacy-glm-monitor',
          provider: 'glm',
          api_mode: 'chat_completions',
          endpoint: '',
          api_key_masked: '',
          primary_model: '',
          extra_models: [],
          group_name: '',
          enabled: true,
          interval_seconds: 60,
          jitter_seconds: 0,
          last_checked_at: null,
          created_by: 1,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
          primary_status: '',
          primary_latency_ms: null,
          availability_7d: 0,
          extra_models_status: [],
          template_id: null,
          extra_headers: {},
          body_override_mode: 'merge',
          body_override: null,
          check_mode: 'probe',
          account_id: null
        }
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

    const labels = wrapper.findAll('button[data-testid^="monitor-provider-"]').map((button) => button.text())

    expect(labels).toContain('monitorCommon.providers.glm')
    expect(labels).toHaveLength(PROVIDERS.length)
  })
})
