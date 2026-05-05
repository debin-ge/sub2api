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
  template: '<div data-testid="model-whitelist-selector">{{ platform }}</div>'
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

  it('submits GLM API key credentials without editable base URLs', async () => {
    createAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    createAccountMock.mockResolvedValue({ id: 2 })

    const wrapper = mountModal()

    expect(wrapper.find('[data-testid="create-platform-glm"]').exists()).toBe(true)

    await wrapper.get('[data-tour="account-form-name"]').setValue('GLM Coding')
    await wrapper.get('[data-testid="create-platform-glm"]').trigger('click')
    expect(wrapper.find('[data-testid="glm-anthropic-base-url"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="glm-openai-base-url"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-whitelist-selector"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-limit-card"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin.accounts.poolMode')
    expect(wrapper.text()).not.toContain('admin.accounts.customErrorCodes')
    expect(wrapper.text()).not.toContain('admin.accounts.tempUnschedulable.title')

    await wrapper.get('[data-testid="glm-api-key"]').setValue('sk-glm-test')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload).toEqual(expect.objectContaining({
      name: 'GLM Coding',
      platform: 'glm',
      type: 'apikey',
      credentials: {
        api_key: 'sk-glm-test'
      }
    }))
    expect(Object.keys(payload.credentials).sort()).toEqual(['api_key'])
    expect(payload.credentials.base_url).toBeUndefined()
    expect(payload.credentials.base_url_anthropic).toBeUndefined()
    expect(payload.credentials.base_url_openai).toBeUndefined()
    expect(payload.extra?.quota_limit).toBeUndefined()
    expect(payload.extra?.quota_daily_limit).toBeUndefined()
    expect(payload.extra?.quota_weekly_limit).toBeUndefined()
    expect(checkMixedChannelRiskMock).not.toHaveBeenCalled()
  })
})
