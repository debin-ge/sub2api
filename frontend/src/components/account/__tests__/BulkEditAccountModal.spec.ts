import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BulkEditAccountModal from '../BulkEditAccountModal.vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import { adminAPI } from '@/api/admin'

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      bulkUpdate: vi.fn(),
      checkMixedChannelRisk: vi.fn()
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

function mountModal(extraProps: Record<string, unknown> = {}) {
  return mount(BulkEditAccountModal, {
    props: {
      show: true,
      accountIds: [1, 2],
      selectedPlatforms: ['antigravity'],
      selectedTypes: ['apikey'],
      proxies: [],
      groups: [],
      ...extraProps
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        ConfirmDialog: true,
        Select: {
          props: ['modelValue', 'options'],
          emits: ['update:modelValue'],
          template: `
            <select
              v-bind="$attrs"
              :value="modelValue"
              @change="$emit('update:modelValue', $event.target.value)"
            >
              <option v-for="option in options" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          `
        },
        ProxySelector: true,
        GroupSelector: true,
        Icon: true
      }
    }
  })
}

describe('BulkEditAccountModal', () => {
  beforeEach(() => {
    vi.mocked(adminAPI.accounts.bulkUpdate).mockReset()
    vi.mocked(adminAPI.accounts.checkMixedChannelRisk).mockReset()

    vi.mocked(adminAPI.accounts.bulkUpdate).mockResolvedValue({
      success: 2,
      failed: 0,
      results: []
    } as any)
    vi.mocked(adminAPI.accounts.checkMixedChannelRisk).mockResolvedValue({
      has_risk: false
    } as any)
  })

  it('antigravity 白名单包含 Gemini 图片模型且过滤掉普通 GPT 模型', async () => {
    const wrapper = mountModal()
    const selector = wrapper.findComponent(ModelWhitelistSelector)
    expect(selector.exists()).toBe(true)

    await selector.find('div.cursor-pointer').trigger('click')

    expect(wrapper.text()).toContain('gemini-3.1-flash-image')
    expect(wrapper.text()).toContain('gemini-2.5-flash-image')
    expect(wrapper.text()).not.toContain('gpt-5.3-codex')
  })

  it('antigravity 映射预设包含图片映射并过滤 OpenAI 预设', async () => {
    const wrapper = mountModal()

    const mappingTab = wrapper.findAll('button').find((btn) => btn.text().includes('admin.accounts.modelMapping'))
    expect(mappingTab).toBeTruthy()
    await mappingTab!.trigger('click')

    expect(wrapper.text()).toContain('3.1-Flash-Image透传')
    expect(wrapper.text()).toContain('3-Pro-Image→3.1')
    expect(wrapper.text()).not.toContain('GPT-5.3 Codex Spark')
  })

	  it('仅勾选模型限制且白名单留空时，应提交空 model_mapping 以支持所有模型', async () => {
	    const wrapper = mountModal({
	      selectedPlatforms: ['anthropic'],
	      selectedTypes: ['apikey']
	    })

    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        model_mapping: {}
      }
	    })
	  })

  it('GLM API Key 账号批量编辑应支持模型白名单和自定义模型输入', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['glm'],
      selectedTypes: ['apikey']
    })

    expect(wrapper.find('#bulk-edit-model-restriction-enabled').exists()).toBe(true)
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    expect(wrapper.text()).toContain('admin.accounts.customModelName')

    const selector = wrapper.findComponent(ModelWhitelistSelector)
    await selector.find('input[placeholder="admin.accounts.enterCustomModelName"]').setValue('custom-glm-model')
    const addButton = wrapper.findAll('button').find((btn) => btn.text().includes('admin.accounts.addModel'))
    expect(addButton).toBeTruthy()
    await addButton!.trigger('click')

    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        model_mapping: {
          'custom-glm-model': 'custom-glm-model'
        }
      }
    })
  })

	  it('OpenAI 账号批量编辑可开启自动透传', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-passthrough-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: true
      }
    })
  })

  it('OpenAI OAuth 批量编辑应提交 OAuth 专属 WS mode 字段（含 http_bridge）', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-ws-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-ws-mode-select"]').setValue('http_bridge')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_oauth_responses_websockets_v2_mode: 'http_bridge',
        openai_oauth_responses_websockets_v2_enabled: true
      }
    })
  })

  it('OpenAI API Key 批量编辑不显示 WS mode 入口', () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    expect(wrapper.find('#bulk-edit-openai-ws-mode-enabled').exists()).toBe(false)
  })

  it('OpenAI OAuth 批量编辑应提交 codex_cli_only 字段', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-codex-cli-only-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-cli-only-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        codex_cli_only: true
      }
    })
  })

  it('OpenAI OAuth 批量编辑应提交 codex_cli_only_allow_app_server 字段（需同时开启父开关）', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    // 子开关从属于 codex_cli_only：必须同时批量开启父开关才写入
    await wrapper.get('#bulk-edit-openai-codex-cli-only-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-cli-only-toggle').trigger('click')
    await wrapper.get('#bulk-edit-openai-codex-app-server-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-app-server-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        codex_cli_only: true,
        codex_cli_only_allow_app_server: true
      }
    })
  })

  it('OpenAI OAuth 批量编辑应提交 codex_cli_only_allowed_clients 字段（需同时开启父开关）', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-codex-cli-only-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-cli-only-toggle').trigger('click')
    await wrapper.get('#bulk-edit-openai-codex-allow-claude-code-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-allow-claude-code-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        codex_cli_only: true,
        codex_cli_only_allowed_clients: ['claude_code']
      }
    })
  })

  it('OpenAI API Key 批量编辑应提交请求头覆写配置', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-header-override-enabled').setValue(true)
    await wrapper.get('#bulk-edit-header-override-toggle').trigger('click')
    await wrapper.get('#bulk-edit-header-override-add-row').trigger('click')
    const inputs = wrapper.findAll('input')
    await inputs.find((input) => input.attributes('placeholder') === 'admin.accounts.headerOverride.namePlaceholder')!.setValue('User-Agent')
    await inputs.find((input) => input.attributes('placeholder') === 'admin.accounts.headerOverride.valuePlaceholder')!.setValue('sub2api-test')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        header_override_enabled: true,
        header_overrides: {
          'user-agent': 'sub2api-test'
        }
      }
    })
  })

  it('未同时开启父开关时不应写入 codex_cli_only_allow_app_server', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    // 仅开启子开关、不批量设置父开关 codex_cli_only：不应写入孤立字段，也不应调用接口
    await wrapper.get('#bulk-edit-openai-codex-app-server-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-app-server-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).not.toHaveBeenCalled()
  })

  it('OpenAI API Key 批量编辑应提交 API Key 专属 WS mode 字段', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-apikey-ws-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-apikey-ws-mode-select"]').setValue('ctx_pool')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_apikey_responses_websockets_v2_mode: 'ctx_pool',
        openai_apikey_responses_websockets_v2_enabled: true
      }
    })
  })

  it('包含 GLM 的批量编辑目标不显示通用凭据控件', () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey'],
      target: {
        mode: 'selected',
        selectedPlatforms: ['openai', 'glm'],
        selectedTypes: ['apikey']
      }
    })

    expect(wrapper.find('#bulk-edit-base-url-enabled').exists()).toBe(false)
    expect(wrapper.find('#bulk-edit-model-restriction-enabled').exists()).toBe(false)
    expect(wrapper.find('#bulk-edit-custom-error-codes-enabled').exists()).toBe(false)
    expect(wrapper.find('#bulk-edit-intercept-warmup-enabled').exists()).toBe(false)
  })

  it('GLM 批量编辑只提交模型限制，不提交其他陈旧通用凭据开关', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['glm'],
      selectedTypes: ['apikey']
    })

    Object.assign(wrapper.vm as any, {
      enableBaseUrl: true,
      baseUrl: 'https://example.invalid',
      enableModelRestriction: true,
      allowedModels: ['GLM-5.1'],
      enableCustomErrorCodes: true,
      selectedErrorCodes: [429],
      enableInterceptWarmup: true,
      interceptWarmupRequests: true,
      enableStatus: true,
      status: 'inactive'
    })

    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        model_mapping: {
          'GLM-5.1': 'GLM-5.1'
        }
      },
      status: 'inactive'
    })
  })

  it('filtered-results 未指定平台时按可能包含 GLM 处理', async () => {
    const wrapper = mountModal({
      accountIds: [],
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey'],
      target: {
        mode: 'filtered',
        filters: {
          platform: '',
          type: 'apikey'
        },
        previewCount: 150,
        selectedPlatforms: ['openai'],
        selectedTypes: ['apikey']
      }
    })

    expect(wrapper.find('#bulk-edit-base-url-enabled').exists()).toBe(false)
    expect(wrapper.find('#bulk-edit-model-restriction-enabled').exists()).toBe(false)
    expect(wrapper.find('#bulk-edit-custom-error-codes-enabled').exists()).toBe(false)
    expect(wrapper.find('#bulk-edit-intercept-warmup-enabled').exists()).toBe(false)

    Object.assign(wrapper.vm as any, {
      enableBaseUrl: true,
      baseUrl: 'https://example.invalid',
      enableModelRestriction: true,
      allowedModels: ['gpt-4'],
      enableCustomErrorCodes: true,
      selectedErrorCodes: [429],
      enableInterceptWarmup: true,
      interceptWarmupRequests: true,
      enableOpenAIPassthrough: true,
      openaiPassthroughEnabled: true,
      enableOpenAIWSMode: true,
      enableOpenAIAPIKeyWSMode: true,
      enableCodexCLIOnly: true,
      codexCLIOnlyEnabled: true,
      enableRpmLimit: true,
      rpmLimitEnabled: true,
      bulkBaseRpm: 30,
      userMsgQueueMode: 'throttle',
      enableStatus: true,
      status: 'inactive'
    })

    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith({
      filters: {
        platform: '',
        type: 'apikey'
      },
      status: 'inactive'
    })
  })

  it('筛选 OpenAI 账号批量编辑应提交 Compact 模式和专属模型映射', async () => {
    const wrapper = mountModal({
      accountIds: [],
      selectedPlatforms: [],
      selectedTypes: [],
      target: {
        mode: 'filtered',
        filters: { platform: 'openai' },
        previewCount: 12,
        selectedPlatforms: ['openai'],
        selectedTypes: ['oauth', 'apikey']
      }
    })

    await wrapper.get('#bulk-edit-openai-compact-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-compact-mode-select"]').setValue('force_on')
    await wrapper.get('#bulk-edit-openai-compact-model-mapping-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-compact-model-mapping-add"]').trigger('click')
    const inputs = wrapper.findAll('[data-testid="bulk-edit-openai-compact-model-mapping-input"]')
    await inputs[0].setValue('gpt-5.4')
    await inputs[1].setValue('gpt-5.4-openai-compact')

    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith({
      filters: { platform: 'openai' },
      extra: {
        openai_compact_mode: 'force_on'
      },
      credentials: {
        compact_model_mapping: {
          'gpt-5.4': 'gpt-5.4-openai-compact'
        }
      }
    })
  })

  it('OpenAI 账号批量编辑可关闭自动透传', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: false,
        openai_oauth_passthrough: false
      }
    })
  })

  it('开启 OpenAI 自动透传时不再同时提交模型限制', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-passthrough-toggle').trigger('click')
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: true
      }
    })
    expect(wrapper.text()).toContain('admin.accounts.openai.modelRestrictionDisabledByPassthrough')
  })

  it('filtered-results 模式下应提交 filters 而不是 account_ids', async () => {
    const wrapper = mountModal({
      accountIds: [],
      target: {
        mode: 'filtered',
        filters: {
          platform: 'openai',
          type: 'oauth',
          status: 'active',
          group: '12',
          search: 'bulk-target',
          privacy_mode: 'training_set_cf_blocked'
        },
        previewCount: 5,
        selectedPlatforms: ['openai'],
        selectedTypes: ['oauth']
      }
    })

    await wrapper.get('#bulk-edit-status-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith({
      filters: {
        platform: 'openai',
        type: 'oauth',
        status: 'active',
        group: '12',
        search: 'bulk-target',
        privacy_mode: 'training_set_cf_blocked'
      },
      status: 'active'
    })
  })
})
