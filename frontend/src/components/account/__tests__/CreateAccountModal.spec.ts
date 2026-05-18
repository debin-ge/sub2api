import { describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

const { createAccountMock, checkMixedChannelRiskMock } = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn()
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
      checkMixedChannelRisk: checkMixedChannelRiskMock
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
        OAuthAuthorizationFlow: true
      }
    }
  })
}

describe('CreateAccountModal', () => {
  it('submits MiniMax Token Plan API key credentials with both upstream base URLs', async () => {
    createAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    createAccountMock.mockResolvedValue({ id: 1 })

    const wrapper = mountModal()

    await wrapper.get('[data-tour="account-form-name"]').setValue('MiniMax Token Plan')
    await wrapper.get('[data-testid="create-platform-minimax"]').trigger('click')
    await wrapper.get('[data-testid="minimax-api-key"]').setValue('sk-cp-test')
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
    expect(checkMixedChannelRiskMock).not.toHaveBeenCalled()
  })

  it('submits Kimi API key credentials with editable base URLs and no Claude aliases', async () => {
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
    await wrapper.get('[data-testid="select-models"]').trigger('click')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('kimi-for-coding')
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
          'kimi-for-coding': 'kimi-for-coding'
        }
      })
    }))
    expect(Object.keys(payload.credentials).sort()).toEqual(['api_key', 'base_url_anthropic', 'base_url_openai', 'model_mapping'])
    expect(payload.credentials.base_url).toBeUndefined()
    expect(payload.credentials.model_mapping['claude-sonnet-4-5']).toBeUndefined()
    expect(checkMixedChannelRiskMock).not.toHaveBeenCalled()
  })

  it('submits DeepSeek API key credentials with editable base URLs and no legacy aliases', async () => {
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
    await wrapper.get('[data-testid="select-models"]').trigger('click')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('deepseek-v4-pro')
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
          'deepseek-v4-pro': 'deepseek-v4-pro'
        }
      })
    }))
    expect(Object.keys(payload.credentials).sort()).toEqual(['api_key', 'base_url_anthropic', 'base_url_openai', 'model_mapping'])
    expect(payload.credentials.base_url).toBeUndefined()
    expect(payload.credentials.model_mapping['deepseek-chat']).toBeUndefined()
    expect(payload.credentials.model_mapping['deepseek-reasoner']).toBeUndefined()
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
})
