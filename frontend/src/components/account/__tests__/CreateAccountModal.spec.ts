import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

const {
  createAccountMock,
  checkMixedChannelRiskMock,
  probeUpstreamBillingMock,
  importCodexSessionMock,
  createOpenAICodexPATMock
} = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn(),
  probeUpstreamBillingMock: vi.fn(),
  importCodexSessionMock: vi.fn(),
  createOpenAICodexPATMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
    showWarning: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isSimpleMode: true
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      checkMixedChannelRisk: checkMixedChannelRiskMock,
      probeUpstreamBilling: probeUpstreamBillingMock,
      importCodexSession: importCodexSessionMock,
      createOpenAICodexPAT: createOpenAICodexPATMock
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue({})
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

import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: {
      type: Boolean,
      default: false
    }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => []
    },
    platform: {
      type: String,
      default: ''
    },
    syncCredentials: {
      type: Object,
      default: undefined
    }
  },
  emits: ['update:modelValue'],
  template: `
    <div data-testid="model-whitelist-selector">
      <span data-testid="model-whitelist-platform">{{ platform }}</span>
      <button
        type="button"
        data-testid="select-models"
        @click="$emit('update:modelValue', platform === 'glm' ? ['GLM-4.7'] : platform === 'kimi' ? ['kimi-for-coding'] : platform === 'deepseek' ? ['deepseek-v4-pro'] : platform === 'windsurf' ? ['claude-sonnet-4.6'] : platform === 'opencode' ? ['opencode/gpt5-nano'] : ['gpt-5.2'])"
      >
        select
      </button>
      <span data-testid="model-whitelist-value">
        {{ Array.isArray(modelValue) ? modelValue.join(',') : '' }}
      </span>
    </div>
  `
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: {
      type: [String, Number, Boolean, null],
      default: ''
    },
    options: {
      type: Array,
      default: () => []
    }
  },
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
})

const QuotaLimitCardStub = defineComponent({
  name: 'QuotaLimitCard',
  template: '<div data-testid="quota-limit-card" />'
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: {
    showManualOption: Boolean,
    showCodexSessionImportOption: Boolean,
    showAgentIdentityOption: Boolean,
    showCodexPatOption: Boolean,
    initialInputMethod: String
  },
  data: () => ({ inputMethod: 'manual' }),
  emits: ['import-codex-session', 'import-codex-pat'],
  template: `
    <div>
      <button data-testid="import-codex-session" @click="$emit('import-codex-session', 'session-json')">session</button>
      <button data-testid="import-codex-pat" @click="$emit('import-codex-pat', 'pat-token')">pat</button>
    </div>
  `
})

function mountModal() {
  return mount(CreateAccountModal, {
    props: {
      show: true,
      proxies: [],
      groups: []
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        Select: SelectStub,
        Icon: true,
        ProxySelector: true,
        GroupSelector: true,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        QuotaLimitCard: QuotaLimitCardStub,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub
      }
    }
  })
}

async function selectButtonByText(wrapper: ReturnType<typeof mountModal>, text: string) {
  const button = wrapper.findAll('button').find(candidate => candidate.text().includes(text))
  expect(button).toBeDefined()
  await button?.trigger('click')
}

async function submitApiKeyAccount(
  platform: 'openai' | 'anthropic',
  enableLongContextBilling = false,
  disableUpstreamBillingProbe = false
) {
  const wrapper = mountModal()
  await selectButtonByText(
    wrapper,
    platform === 'openai' ? 'OpenAI' : 'admin.accounts.claudeConsole'
  )
  if (platform === 'openai') {
    await selectButtonByText(wrapper, 'API Key')
  }
  await wrapper
    .get('form#create-account-form input[type="text"]')
    .setValue(`${platform} account`)
  await wrapper
    .get('form#create-account-form input[type="password"]')
    .setValue('test-api-key')
  if (enableLongContextBilling) {
    await wrapper.get('[data-testid="openai-long-context-billing-toggle"]').trigger('click')
  }
  if (disableUpstreamBillingProbe) {
    await wrapper.get('[data-testid="upstream-billing-auto-probe"]').trigger('click')
  }
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
  return wrapper
}

async function openCodexImportStep(toggleClicks = 0) {
  const wrapper = mountModal()
  await selectButtonByText(wrapper, 'OpenAI')
  for (let click = 0; click < toggleClicks; click += 1) {
    await wrapper.get('[data-testid="openai-long-context-billing-toggle"]').trigger('click')
  }
  await wrapper.get('form#create-account-form input[type="text"]').setValue('Codex import')
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  return wrapper
}

describe('CreateAccountModal', () => {
  beforeEach(() => {
    probeUpstreamBillingMock.mockReset().mockResolvedValue({})
    importCodexSessionMock.mockReset().mockResolvedValue({
      created: 1,
      updated: 0,
      skipped: 0,
      failed: 0,
      errors: [],
      warnings: []
    })
    createOpenAICodexPATMock.mockReset().mockResolvedValue({})
  })

  it('submits MiniMax Token Plan API key credentials with both upstream base URLs', async () => {
    createAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    createAccountMock.mockResolvedValue({ id: 1 })

    const wrapper = mountModal()

    await wrapper.get('[data-tour="account-form-name"]').setValue('MiniMax Token Plan')
    await wrapper.get('[data-testid="create-platform-minimax"]').trigger('click')
    await wrapper.get('[data-testid="minimax-api-key"]').setValue('sk-cp-test')
    expect(wrapper.findComponent(ModelWhitelistSelectorStub).props('syncCredentials')).toEqual({
      platform: 'minimax',
      type: 'apikey',
      base_url: 'https://api.minimax.io/v1',
      api_key: 'sk-cp-test'
    })
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload).toEqual(expect.objectContaining({
      name: 'MiniMax Token Plan',
      platform: 'minimax',
      type: 'apikey',
      credentials: expect.objectContaining({
        api_key: 'sk-cp-test',
        auth_scheme: 'bearer',
        base_url_anthropic: 'https://api.minimax.io/anthropic',
        base_url_openai: 'https://api.minimax.io/v1'
      })
    }))
    expect(payload.credentials.base_url).toBeUndefined()
    expect(payload.credentials.model_mapping).toBeUndefined()
    expect(checkMixedChannelRiskMock).not.toHaveBeenCalled()
  })

  it('submits GLM API key credentials with editable base URLs', async () => {
    createAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    createAccountMock.mockResolvedValue({ id: 2 })

    const wrapper = mountModal()

    expect(wrapper.find('[data-testid="create-platform-glm"]').exists()).toBe(true)

	    await wrapper.get('[data-tour="account-form-name"]').setValue('GLM Coding')
	    await wrapper.get('[data-testid="create-platform-glm"]').trigger('click')
	    expect(wrapper.text()).toContain('admin.accounts.glm.apiKeyHint')
	    expect(wrapper.text()).not.toContain('admin.accounts.apiKeyHint')
	    expect(wrapper.find('[data-testid="glm-anthropic-base-url"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="glm-openai-base-url"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="model-whitelist-selector"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="model-whitelist-platform"]').text()).toBe('glm')
    expect(wrapper.find('[data-testid="quota-limit-card"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin.accounts.poolMode')
    expect(wrapper.text()).not.toContain('admin.accounts.customErrorCodes')
    expect(wrapper.text()).not.toContain('admin.accounts.tempUnschedulable.title')

    await wrapper.get('[data-testid="glm-api-key"]').setValue('sk-glm-test')
    await wrapper.get('[data-testid="glm-anthropic-base-url"]').setValue('https://custom.example/glm/anthropic')
    await wrapper.get('[data-testid="glm-openai-base-url"]').setValue('https://custom.example/glm/openai')
    await wrapper.get('[data-testid="select-models"]').trigger('click')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('GLM-4.7')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload).toEqual(expect.objectContaining({
      name: 'GLM Coding',
      platform: 'glm',
      type: 'apikey',
      credentials: expect.objectContaining({
        api_key: 'sk-glm-test',
        base_url_anthropic: 'https://custom.example/glm/anthropic',
        base_url_openai: 'https://custom.example/glm/openai',
        model_mapping: {
          'GLM-4.7': 'GLM-4.7'
        }
      })
    }))
    expect(Object.keys(payload.credentials).sort()).toEqual(['api_key', 'base_url_anthropic', 'base_url_openai', 'model_mapping'])
    expect(payload.credentials.base_url).toBeUndefined()
    expect(payload.extra?.quota_limit).toBeUndefined()
    expect(payload.extra?.quota_daily_limit).toBeUndefined()
    expect(payload.extra?.quota_weekly_limit).toBeUndefined()
    expect(payload.extra?.openai_long_context_billing_enabled).toBeUndefined()
    expect(payload.upstream_billing_probe_enabled).toBe(true)
    expect(checkMixedChannelRiskMock).not.toHaveBeenCalled()
  })

  it('submits Kimi API key credentials with an explicit custom model mapping', async () => {
    createAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    createAccountMock.mockResolvedValue({ id: 3 })

    const wrapper = mountModal()

    expect(wrapper.find('[data-testid="create-platform-kimi"]').exists()).toBe(true)

    await wrapper.get('[data-tour="account-form-name"]').setValue('Kimi Coding')
    await wrapper.get('[data-testid="create-platform-kimi"]').trigger('click')
    expect(wrapper.text()).toContain('admin.accounts.kimi.apiKeyHint')
    expect(wrapper.text()).not.toContain('admin.accounts.apiKeyHint')
    expect(wrapper.find('[data-testid="kimi-anthropic-base-url"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="kimi-openai-base-url"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="model-whitelist-selector"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="model-whitelist-platform"]').text()).toBe('kimi')
    expect(wrapper.find('[data-testid="quota-limit-card"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin.accounts.poolMode')
    expect(wrapper.text()).not.toContain('admin.accounts.customErrorCodes')
    expect(wrapper.text()).not.toContain('admin.accounts.tempUnschedulable.title')

    await wrapper.get('[data-testid="kimi-api-key"]').setValue('sk-kimi-test')
    await wrapper.get('[data-testid="kimi-anthropic-base-url"]').setValue('https://custom.example/kimi/anthropic')
    await wrapper.get('[data-testid="kimi-openai-base-url"]').setValue('https://custom.example/kimi/openai')
    const mappingModeButton = wrapper.findAll('button').find((button) => button.text().includes('admin.accounts.modelMapping'))
    expect(mappingModeButton).toBeDefined()
    await mappingModeButton!.trigger('click')
    const addMappingButton = wrapper.findAll('button').find((button) => button.text().includes('admin.accounts.addMapping'))
    expect(addMappingButton).toBeDefined()
    await addMappingButton!.trigger('click')
    await wrapper.get('input[placeholder="admin.accounts.requestModel"]').setValue('kimi-latest')
    await wrapper.get('input[placeholder="admin.accounts.actualModel"]').setValue('kimi-future-model')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload).toEqual(expect.objectContaining({
      name: 'Kimi Coding',
      platform: 'kimi',
      type: 'apikey',
      credentials: expect.objectContaining({
        api_key: 'sk-kimi-test',
        base_url_anthropic: 'https://custom.example/kimi/anthropic',
        base_url_openai: 'https://custom.example/kimi/openai',
        model_mapping: {
          'kimi-latest': 'kimi-future-model'
        }
      })
    }))
    expect(Object.keys(payload.credentials).sort()).toEqual(['api_key', 'base_url_anthropic', 'base_url_openai', 'model_mapping'])
    expect(payload.credentials.base_url).toBeUndefined()
    expect(checkMixedChannelRiskMock).not.toHaveBeenCalled()
  })

  it('submits DeepSeek API key credentials with an explicit custom model mapping', async () => {
    createAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    createAccountMock.mockResolvedValue({ id: 4 })

    const wrapper = mountModal()

    expect(wrapper.find('[data-testid="create-platform-deepseek"]').exists()).toBe(true)

    await wrapper.get('[data-tour="account-form-name"]').setValue('DeepSeek Gateway')
    await wrapper.get('[data-testid="create-platform-deepseek"]').trigger('click')
    expect(wrapper.text()).toContain('admin.accounts.deepseek.apiKeyHint')
    expect(wrapper.text()).not.toContain('admin.accounts.apiKeyHint')
    expect(wrapper.find('[data-testid="deepseek-anthropic-base-url"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="deepseek-openai-base-url"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="model-whitelist-selector"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="model-whitelist-platform"]').text()).toBe('deepseek')
    expect(wrapper.find('[data-testid="quota-limit-card"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin.accounts.poolMode')
    expect(wrapper.text()).not.toContain('admin.accounts.customErrorCodes')
    expect(wrapper.text()).not.toContain('admin.accounts.tempUnschedulable.title')

    await wrapper.get('[data-testid="deepseek-api-key"]').setValue('sk-deepseek-test')
    await wrapper.get('[data-testid="deepseek-anthropic-base-url"]').setValue('https://custom.example/deepseek/anthropic')
    await wrapper.get('[data-testid="deepseek-openai-base-url"]').setValue('https://custom.example/deepseek/openai')
    const mappingModeButton = wrapper.findAll('button').find((button) => button.text().includes('admin.accounts.modelMapping'))
    expect(mappingModeButton).toBeDefined()
    await mappingModeButton!.trigger('click')
    const addMappingButton = wrapper.findAll('button').find((button) => button.text().includes('admin.accounts.addMapping'))
    expect(addMappingButton).toBeDefined()
    await addMappingButton!.trigger('click')
    await wrapper.get('input[placeholder="admin.accounts.requestModel"]').setValue('deepseek-latest')
    await wrapper.get('input[placeholder="admin.accounts.actualModel"]').setValue('deepseek-future-model')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload).toEqual(expect.objectContaining({
      name: 'DeepSeek Gateway',
      platform: 'deepseek',
      type: 'apikey',
      credentials: expect.objectContaining({
        api_key: 'sk-deepseek-test',
        base_url_anthropic: 'https://custom.example/deepseek/anthropic',
        base_url_openai: 'https://custom.example/deepseek/openai',
        model_mapping: {
          'deepseek-latest': 'deepseek-future-model'
        }
      })
    }))
    expect(Object.keys(payload.credentials).sort()).toEqual(['api_key', 'base_url_anthropic', 'base_url_openai', 'model_mapping'])
    expect(payload.credentials.base_url).toBeUndefined()
    expect(checkMixedChannelRiskMock).not.toHaveBeenCalled()
  })

  it('submits Windsurf API key credentials with a single reverse-proxy base URL', async () => {
    createAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    createAccountMock.mockResolvedValue({ id: 5 })

    const wrapper = mountModal()

    expect(wrapper.find('[data-testid="create-platform-windsurf"]').exists()).toBe(true)

    await wrapper.get('[data-tour="account-form-name"]').setValue('Windsurf Gateway')
    await wrapper.get('[data-testid="create-platform-windsurf"]').trigger('click')
    expect(wrapper.text()).toContain('admin.accounts.windsurf.apiKeyHint')
    expect(wrapper.find('[data-testid="windsurf-base-url"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="model-whitelist-selector"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="model-whitelist-platform"]').text()).toBe('windsurf')
    expect(wrapper.find('[data-testid="quota-limit-card"]').exists()).toBe(false)

    await wrapper.get('[data-testid="windsurf-api-key"]').setValue('sk-windsurf-test')
    await wrapper.get('[data-testid="windsurf-base-url"]').setValue('https://custom.example/windsurf')
    await wrapper.get('[data-testid="select-models"]').trigger('click')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('claude-sonnet-4.6')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload).toEqual(expect.objectContaining({
      name: 'Windsurf Gateway',
      platform: 'windsurf',
      type: 'apikey',
      credentials: expect.objectContaining({
        api_key: 'sk-windsurf-test',
        base_url: 'https://custom.example/windsurf',
        model_mapping: {
          'claude-sonnet-4.6': 'claude-sonnet-4.6'
        }
      })
    }))
    expect(Object.keys(payload.credentials).sort()).toEqual(['api_key', 'base_url', 'model_mapping'])
    expect(payload.credentials.base_url_anthropic).toBeUndefined()
    expect(payload.credentials.base_url_openai).toBeUndefined()
    expect(checkMixedChannelRiskMock).not.toHaveBeenCalled()
  })

  it('submits OpenCode API key credentials with a single OpenCode2API base URL', async () => {
    createAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    createAccountMock.mockResolvedValue({ id: 6 })

    const wrapper = mountModal()

    expect(wrapper.find('[data-testid="create-platform-opencode"]').exists()).toBe(true)

    await wrapper.get('[data-tour="account-form-name"]').setValue('OpenCode Gateway')
    await wrapper.get('[data-testid="create-platform-opencode"]').trigger('click')
    expect(wrapper.text()).toContain('admin.accounts.opencode.apiKeyHint')
    expect(wrapper.find('[data-testid="opencode-base-url"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="model-whitelist-selector"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="model-whitelist-platform"]').text()).toBe('opencode')
    expect(wrapper.find('[data-testid="quota-limit-card"]').exists()).toBe(false)

    await wrapper.get('[data-testid="opencode-api-key"]').setValue('sk-opencode-test')
    await wrapper.get('[data-testid="opencode-base-url"]').setValue('https://custom.example/opencode')
    await wrapper.get('[data-testid="select-models"]').trigger('click')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('opencode/gpt5-nano')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload).toEqual(expect.objectContaining({
      name: 'OpenCode Gateway',
      platform: 'opencode',
      type: 'apikey',
      credentials: expect.objectContaining({
        api_key: 'sk-opencode-test',
        base_url: 'https://custom.example/opencode',
        model_mapping: {
          'opencode/gpt5-nano': 'opencode/gpt5-nano'
        }
      })
    }))
    expect(Object.keys(payload.credentials).sort()).toEqual(['api_key', 'base_url', 'model_mapping'])
    expect(payload.credentials.base_url_anthropic).toBeUndefined()
    expect(payload.credentials.base_url_openai).toBeUndefined()
    expect(checkMixedChannelRiskMock).not.toHaveBeenCalled()
  })

  describe('OpenAI billing compatibility and upstream probes', () => {
    beforeEach(() => {
      createAccountMock
        .mockReset()
        .mockResolvedValue({ id: 42, platform: 'openai', type: 'apikey' })
      checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
      probeUpstreamBillingMock.mockReset().mockResolvedValue({})
      importCodexSessionMock.mockReset().mockResolvedValue({
        created: 1,
        updated: 0,
        skipped: 0,
        failed: 0,
        errors: [],
        warnings: []
      })
      createOpenAICodexPATMock.mockReset().mockResolvedValue({})
    })

    it('sends false explicitly for normal OpenAI account creation by default', async () => {
      await submitApiKeyAccount('openai')

      expect(createAccountMock).toHaveBeenCalledTimes(1)
      expect(
        createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled
      ).toBe(false)
    })

    it('omits InternalRelay from a normal OpenAI API key account', async () => {
      await submitApiKeyAccount('openai')

      expect(createAccountMock).toHaveBeenCalledTimes(1)
      expect(createAccountMock.mock.calls[0]?.[0]?.extra).not.toHaveProperty('internal_relay')
    })

    it('submits InternalRelay for an OpenAI API key account using a loopback base URL', async () => {
      const wrapper = mountModal()
      await selectButtonByText(wrapper, 'OpenAI')
      await selectButtonByText(wrapper, 'API Key')
      await wrapper
        .get('form#create-account-form input[type="text"]')
        .setValue('OpenAI internal relay')
      await wrapper
        .get('input[placeholder="https://api.openai.com"]')
        .setValue('http://127.0.0.1:8080')
      await wrapper
        .get('form#create-account-form input[type="password"]')
        .setValue('test-api-key')
      const toggle = wrapper.get('[data-testid="openai-internal-relay-toggle"]')
      expect(toggle.attributes('aria-checked')).toBe('false')

      await toggle.trigger('click')
      await wrapper.get('form#create-account-form').trigger('submit.prevent')
      await flushPromises()

      expect(createAccountMock).toHaveBeenCalledTimes(1)
      expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({
        platform: 'openai',
        type: 'apikey',
        credentials: {
          base_url: 'http://127.0.0.1:8080'
        },
        extra: {
          internal_relay: true
        }
      })
    })

    it('shows InternalRelay only for OpenAI API key accounts', async () => {
      const wrapper = mountModal()
      await selectButtonByText(wrapper, 'OpenAI')

      expect(wrapper.find('[data-testid="openai-internal-relay-toggle"]').exists()).toBe(false)

      await selectButtonByText(wrapper, 'API Key')
      expect(wrapper.find('[data-testid="openai-internal-relay-toggle"]').exists()).toBe(true)
    })

    it('shows the Codex namespace flatten toggle only for OpenAI OAuth accounts', async () => {
      const wrapper = mountModal()
      await selectButtonByText(wrapper, 'OpenAI')

      expect(
        wrapper.find('[data-testid="create-openai-flatten-namespaces-toggle"]').exists()
      ).toBe(true)

      await selectButtonByText(wrapper, 'API Key')
      expect(
        wrapper.find('[data-testid="create-openai-flatten-namespaces-toggle"]').exists()
      ).toBe(false)
    })

    it('enables upstream billing probes by default for new OpenAI API key accounts', async () => {
      await submitApiKeyAccount('openai')

      expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(true)
    })

    it('waits for the initial upstream billing probe before refreshing the account list', async () => {
      let resolveProbe: (() => void) | undefined
      probeUpstreamBillingMock.mockImplementationOnce(
        () =>
          new Promise<void>(resolve => {
            resolveProbe = resolve
          })
      )

      const wrapper = await submitApiKeyAccount('openai')

      expect(probeUpstreamBillingMock).toHaveBeenCalledWith(42)
      expect(wrapper.emitted('created')).toBeUndefined()

      resolveProbe?.()
      await flushPromises()

      expect(wrapper.emitted('created')).toHaveLength(1)
    })

    it('sends an explicit disabled state when the create toggle is turned off', async () => {
      await submitApiKeyAccount('openai', false, true)

      expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(false)
      expect(probeUpstreamBillingMock).not.toHaveBeenCalled()
    })

    it('exposes Agent Identity in the OpenAI authorization methods', async () => {
      const wrapper = mountModal()
      await selectButtonByText(wrapper, 'OpenAI')
      await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenAI account')
      await wrapper.get('form#create-account-form').trigger('submit.prevent')

      const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
      expect(flow.props('showManualOption')).toBe(true)
      expect(flow.props('showCodexSessionImportOption')).toBe(true)
      expect(flow.props('showAgentIdentityOption')).toBe(true)
      expect(flow.props('showCodexPatOption')).toBe(true)
      expect(flow.props('initialInputMethod')).toBe('manual')
    })

    it.each([
      ['camelCase', { authMode: 'agentIdentity', agentIdentity: { agentRuntimeId: 'runtime' } }],
      ['nested identity without auth_mode', { agent_identity: { agent_runtime_id: 'runtime' } }]
    ])('accepts backend-compatible %s Agent Identity imports', async (_name, content) => {
      const wrapper = await openCodexImportStep()
      const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
      flow.vm.inputMethod = 'agent_identity'

      flow.vm.$emit('import-codex-session', JSON.stringify(content))
      await flushPromises()

      expect(importCodexSessionMock).toHaveBeenCalledTimes(1)
    })

    it('sends true explicitly when OpenAI long-context billing is enabled', async () => {
      await submitApiKeyAccount('openai', true)

      expect(createAccountMock).toHaveBeenCalledTimes(1)
      expect(
        createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled
      ).toBe(true)
    })

    it('omits the OpenAI setting and enables probes for non-OpenAI API keys', async () => {
      await submitApiKeyAccount('anthropic')

      expect(createAccountMock).toHaveBeenCalledTimes(1)
      expect(
        createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled
      ).toBeUndefined()
      expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(true)
    })

    it('can disable probes for a non-OpenAI API key account', async () => {
      await submitApiKeyAccount('anthropic', false, true)

      expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(false)
    })

    it('leaves Codex session import billing ownership to the backend', async () => {
      const wrapper = await openCodexImportStep()
      await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
      await flushPromises()

      expect(importCodexSessionMock).toHaveBeenCalledTimes(1)
      expect(
        importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled
      ).toBeUndefined()
    })

    it('leaves Codex PAT import billing ownership to the backend', async () => {
      const wrapper = await openCodexImportStep()
      await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
      await flushPromises()

      expect(createOpenAICodexPATMock).toHaveBeenCalledTimes(1)
      expect(
        createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled
      ).toBeUndefined()
    })

    it.each([
      ['session', 'import-codex-session'],
      ['PAT', 'import-codex-pat']
    ])('passes explicit long-context choices to Codex %s imports', async (_name, testId) => {
      const enabledWrapper = await openCodexImportStep(1)
      await enabledWrapper.get(`[data-testid="${testId}"]`).trigger('click')
      await flushPromises()

      const targetMock = testId === 'import-codex-session'
        ? importCodexSessionMock
        : createOpenAICodexPATMock
      expect(targetMock.mock.calls.at(-1)?.[0]?.extra?.openai_long_context_billing_enabled).toBe(
        true
      )

      targetMock.mockClear()
      const disabledWrapper = await openCodexImportStep(2)
      await disabledWrapper.get(`[data-testid="${testId}"]`).trigger('click')
      await flushPromises()

      expect(targetMock.mock.calls.at(-1)?.[0]?.extra?.openai_long_context_billing_enabled).toBe(
        false
      )
    })
  })
})
