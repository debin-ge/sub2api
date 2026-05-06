import { describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

const { updateAccountMock, checkMixedChannelRiskMock } = vi.hoisted(() => ({
  updateAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
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
      update: updateAccountMock,
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

import EditAccountModal from '../EditAccountModal.vue'

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
    <div>
      <button
        type="button"
        data-testid="rewrite-to-snapshot"
        @click="$emit('update:modelValue', platform === 'glm' ? ['GLM-4.5-air'] : platform === 'kimi' ? ['kimi-for-coding'] : ['gpt-5.2-2025-12-11'])"
      >
        rewrite
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

function buildAccount() {
  return {
    id: 1,
    name: 'OpenAI Key',
    notes: '',
    platform: 'openai',
    type: 'apikey',
    credentials: {
      api_key: 'sk-test',
      base_url: 'https://api.openai.com',
      model_mapping: {
        'gpt-5.2': 'gpt-5.2'
      }
    },
    extra: {},
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false
  } as any
}

function buildMiniMaxAccount() {
  return {
    ...buildAccount(),
    name: 'MiniMax Token Plan',
    platform: 'minimax',
    type: 'apikey',
    credentials: {
      api_key: 'sk-cp-existing',
      auth_scheme: 'bearer',
      base_url_anthropic: 'https://old.example/anthropic',
      base_url_openai: 'https://old.example/v1',
      model_mapping: {
        'MiniMax-M2.7': 'MiniMax-M2.7'
      },
      future_unknown_key: 'keep-me'
    }
  } as any
}

function buildGLMAccount() {
  return {
    ...buildAccount(),
    name: 'GLM Coding',
    platform: 'glm',
    type: 'apikey',
    credentials: {
      api_key: 'sk-glm-existing',
      base_url: 'https://should-not-be-submitted.example',
      base_url_anthropic: 'https://should-not-be-submitted.example/anthropic',
      base_url_openai: 'https://should-not-be-submitted.example/v1',
      model_mapping: { 'GLM-4.7': 'GLM-4.7' },
      compact_model_mapping: { 'GLM-4.7': 'GLM-4.7-compact' },
      pool_mode: true,
      pool_mode_retry_count: 3,
      custom_error_codes_enabled: true,
      custom_error_codes: [429],
      intercept_warmup_requests: true,
      temp_unschedulable_enabled: true,
      temp_unschedulable_rules: [{ error_code: 429, keywords: ['quota'], duration_minutes: 30 }],
      future_unknown_key: 'keep-me'
    },
    extra: {
      quota_limit: 50,
      quota_daily_limit: 10,
      quota_weekly_limit: 20
    }
  } as any
}

function buildKimiAccount() {
  return {
    ...buildAccount(),
    name: 'Kimi Coding',
    platform: 'kimi',
    type: 'apikey',
    credentials: {
      api_key: 'sk-kimi-existing',
      base_url: 'https://should-not-be-submitted.example',
      base_url_anthropic: 'https://should-not-be-submitted.example/anthropic',
      base_url_openai: 'https://should-not-be-submitted.example/v1',
      model_mapping: { 'kimi-for-coding': 'kimi-for-coding' },
      compact_model_mapping: { 'kimi-for-coding': 'ignored' },
      pool_mode: true,
      pool_mode_retry_count: 3,
      custom_error_codes_enabled: true,
      custom_error_codes: [429],
      intercept_warmup_requests: true,
      temp_unschedulable_enabled: true,
      temp_unschedulable_rules: [{ error_code: 429, keywords: ['quota'], duration_minutes: 30 }],
      future_unknown_key: 'keep-me'
    },
    extra: {
      quota_limit: 50,
      quota_daily_limit: 10,
      quota_weekly_limit: 20
    }
  } as any
}

function mountModal(account = buildAccount()) {
  return mount(EditAccountModal, {
    props: {
      show: true,
      account,
      proxies: [],
      groups: []
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
        ProxySelector: true,
        GroupSelector: true,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        QuotaLimitCard: QuotaLimitCardStub
      }
    }
  })
}

describe('EditAccountModal', () => {
  it('reopening the same account rehydrates the OpenAI whitelist from props', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2')

    await wrapper.get('[data-testid="rewrite-to-snapshot"]').trigger('click')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2-2025-12-11')

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })

    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_mapping).toEqual({
      'gpt-5.2': 'gpt-5.2'
    })
  })

  it('submits OpenAI compact mode and compact-only model mapping', async () => {
    const account = buildAccount()
    account.extra = {
      openai_compact_mode: 'force_on'
    }
    account.credentials = {
      ...account.credentials,
      compact_model_mapping: {
        'gpt-5.4': 'gpt-5.4-openai-compact'
      }
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_compact_mode).toBe('force_on')
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.compact_model_mapping).toEqual({
      'gpt-5.4': 'gpt-5.4-openai-compact'
    })
  })

  it('submits MiniMax Token Plan base URLs while preserving unknown credential keys', async () => {
    const account = buildMiniMaxAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="minimax-anthropic-base-url"]').setValue('https://custom.example/anthropic')
    await wrapper.get('[data-testid="minimax-openai-base-url"]').setValue('https://custom.example/v1')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toEqual(expect.objectContaining({
      api_key: 'sk-cp-existing',
      auth_scheme: 'bearer',
      base_url_anthropic: 'https://custom.example/anthropic',
      base_url_openai: 'https://custom.example/v1',
      future_unknown_key: 'keep-me',
      model_mapping: {
        'MiniMax-M2.7': 'MiniMax-M2.7'
      }
    }))
  })

  it('submits GLM API key updates with model whitelist and without editable base URLs', async () => {
    const account = buildGLMAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.find('[data-testid="glm-base-url"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="glm-anthropic-base-url"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="glm-openai-base-url"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-whitelist-value"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('GLM-4.7')
    expect(wrapper.find('[data-testid="quota-limit-card"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin.accounts.poolMode')
    expect(wrapper.text()).not.toContain('admin.accounts.customErrorCodes')
    expect(wrapper.text()).not.toContain('admin.accounts.tempUnschedulable.title')

    await wrapper.get('[data-testid="glm-api-key"]').setValue('sk-glm-updated')
    await wrapper.get('[data-testid="rewrite-to-snapshot"]').trigger('click')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('GLM-4.5-air')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials).toEqual(expect.objectContaining({
      api_key: 'sk-glm-updated',
      future_unknown_key: 'keep-me'
    }))
    expect(credentials.base_url).toBeUndefined()
    expect(credentials.base_url_anthropic).toBeUndefined()
    expect(credentials.base_url_openai).toBeUndefined()
    expect(credentials.model_mapping).toEqual({
      'GLM-4.5-air': 'GLM-4.5-air'
    })
    expect(credentials.compact_model_mapping).toBeUndefined()
    expect(credentials.pool_mode).toBeUndefined()
    expect(credentials.pool_mode_retry_count).toBeUndefined()
    expect(credentials.custom_error_codes_enabled).toBeUndefined()
    expect(credentials.custom_error_codes).toBeUndefined()
    expect(credentials.intercept_warmup_requests).toBeUndefined()
    expect(credentials.temp_unschedulable_enabled).toBeUndefined()
    expect(credentials.temp_unschedulable_rules).toBeUndefined()
    const extra = updateAccountMock.mock.calls[0]?.[1]?.extra
    expect(extra?.quota_limit).toBeUndefined()
    expect(extra?.quota_daily_limit).toBeUndefined()
    expect(extra?.quota_weekly_limit).toBeUndefined()
  })

  it('submits Kimi API key updates with only kimi-for-coding mapping and no base URLs', async () => {
    const account = buildKimiAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.find('[data-testid="kimi-base-url"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="kimi-anthropic-base-url"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="kimi-openai-base-url"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-whitelist-value"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('kimi-for-coding')
    expect(wrapper.find('[data-testid="quota-limit-card"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin.accounts.poolMode')
    expect(wrapper.text()).not.toContain('admin.accounts.customErrorCodes')
    expect(wrapper.text()).not.toContain('admin.accounts.tempUnschedulable.title')

    await wrapper.get('[data-testid="kimi-api-key"]').setValue('sk-kimi-updated')
    await wrapper.get('[data-testid="rewrite-to-snapshot"]').trigger('click')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('kimi-for-coding')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials).toEqual(expect.objectContaining({
      api_key: 'sk-kimi-updated',
      future_unknown_key: 'keep-me'
    }))
    expect(credentials.base_url).toBeUndefined()
    expect(credentials.base_url_anthropic).toBeUndefined()
    expect(credentials.base_url_openai).toBeUndefined()
    expect(credentials.model_mapping).toEqual({
      'kimi-for-coding': 'kimi-for-coding'
    })
    expect(credentials.model_mapping['claude-sonnet-4-5']).toBeUndefined()
    expect(credentials.compact_model_mapping).toBeUndefined()
    expect(credentials.pool_mode).toBeUndefined()
    expect(credentials.pool_mode_retry_count).toBeUndefined()
    expect(credentials.custom_error_codes_enabled).toBeUndefined()
    expect(credentials.custom_error_codes).toBeUndefined()
    expect(credentials.intercept_warmup_requests).toBeUndefined()
    expect(credentials.temp_unschedulable_enabled).toBeUndefined()
    expect(credentials.temp_unschedulable_rules).toBeUndefined()
    const extra = updateAccountMock.mock.calls[0]?.[1]?.extra
    expect(extra?.quota_limit).toBeUndefined()
    expect(extra?.quota_daily_limit).toBeUndefined()
    expect(extra?.quota_weekly_limit).toBeUndefined()
  })
})
