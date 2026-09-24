import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PRICE_FIELDS } from '@/api/admin/modelPrices'
import ModelPricesView from '@/views/admin/ModelPricesView.vue'

const {
  listModelPrices,
  listModelPricePlatforms,
  getModelPriceSyncStatus,
  getModelPriceEntry,
  upsertModelPrice,
  previewVideoPrice,
  showError,
  showWarning,
} = vi.hoisted(() => ({
  listModelPrices: vi.fn(),
  listModelPricePlatforms: vi.fn(),
  getModelPriceSyncStatus: vi.fn(),
  getModelPriceEntry: vi.fn(),
  upsertModelPrice: vi.fn(),
  previewVideoPrice: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
}))

vi.mock('@/api/admin/modelPrices', async () => {
  const actual = await vi.importActual<typeof import('@/api/admin/modelPrices')>('@/api/admin/modelPrices')
  return {
    ...actual,
    listModelPrices,
    listModelPricePlatforms,
    getModelPriceSyncStatus,
    getModelPriceEntry,
    syncModelPrices: vi.fn(),
    previewVideoPrice,
    upsertModelPrice,
    deleteModelPrice: vi.fn(),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError, showWarning }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const AppLayoutStub = defineComponent({
  template: '<main><slot /></main>',
})
const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>',
})
const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
  },
  template: '<div data-test="table"><div v-for="row in data" :key="row.model"><span>{{ row.model }}</span><slot name="cell-input" :row="row" /><slot name="cell-output" :row="row" /><slot name="cell-actions" :row="row" /></div></div>',
})
const SelectStub = defineComponent({
  props: {
    modelValue: { type: [String, Number, Boolean], default: '' },
    options: { type: Array, default: () => [] },
    disabled: { type: Boolean, default: false },
  },
  emits: ['update:modelValue', 'change'],
  template: `
    <select
      data-test="select"
      :value="modelValue"
      :disabled="disabled"
      @change="$emit('update:modelValue', $event.target.value); $emit('change', $event.target.value)"
    >
      <option v-for="option in options" :key="String(option.value)" :value="option.value">
        {{ option.label }}
      </option>
    </select>
  `,
})

function mountView() {
  return mount(ModelPricesView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        Select: SelectStub,
        Icon: true,
        EmptyState: true,
        BaseDialog: {
          props: ['show', 'title'],
          template: '<div v-if="show" data-test="editor"><slot /><slot name="footer" /></div>',
        },
        ConfirmDialog: true,
      },
    },
  })
}

describe('ModelPricesView', () => {
  beforeEach(() => {
    listModelPrices.mockReset()
    listModelPricePlatforms.mockReset()
    getModelPriceSyncStatus.mockReset()
    getModelPriceEntry.mockReset()
    upsertModelPrice.mockReset()
    previewVideoPrice.mockReset()
    showError.mockReset()
    showWarning.mockReset()
    listModelPricePlatforms.mockResolvedValue(['*', 'anthropic', 'deepseek'])
    getModelPriceSyncStatus.mockResolvedValue({ catalog_model_count: 2, override_count: 1 })
    listModelPrices.mockResolvedValue({
      items: [
        {
          platform: 'anthropic',
          model: 'openai/gpt-5.4',
          source: 'merged',
          token_pricing_absent: false,
          has_image_pricing: false,
          sync_invalidated: false,
          redundant: false,
          effective: { input_cost_per_token: 3e-6 },
          overridden_fields: ['input_cost_per_token'],
          override_platform: '*',
          enabled: true,
        },
      ],
      total: 1,
    })
    getModelPriceEntry.mockResolvedValue({
      platform: 'anthropic',
      model: 'openai/gpt-5.4',
      catalog: {},
      override: { input_cost_per_token: 3e-6 },
      effective: { input_cost_per_token: 3e-6 },
      enabled: true,
      token_pricing_absent: false,
      has_image_pricing: false,
      sync_invalidated: false,
      redundant: false,
      override_platform: '*',
    })
    upsertModelPrice.mockResolvedValue({})
    previewVideoPrice.mockResolvedValue({
      matched: true,
      rule_key: 'default',
      billing_unit: 'second',
      estimated_units: 5,
      maximum_units: 5,
      estimated_cost: 0,
      normalized_attributes: { input_has_video: false },
      rejected_rules: [],
    })
  })

  it('keeps wildcard out of the platform filter and edits the row platform', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(listModelPrices).toHaveBeenCalled()
    expect(wrapper.text()).toContain('openai/gpt-5.4')

    const filterOptions = wrapper.findAll('[data-test="select"]')[0].findAll('option')
    expect(filterOptions.map((option) => option.element.value)).toEqual(['', 'anthropic', 'deepseek'])

    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()
    expect(getModelPriceEntry).toHaveBeenCalledWith('anthropic', 'openai/gpt-5.4')
    expect(wrapper.text()).toContain('admin.modelPrices.fields.input_cost_per_token')

    const editorPlatform = wrapper.find('[data-test="editor"] [data-test="select"]')
    expect(editorPlatform.element.value).toBe('anthropic')
    expect(editorPlatform.attributes('disabled')).toBeDefined()

    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.save')
    expect(saveButton).toBeDefined()
    await saveButton!.trigger('click')
    await flushPromises()
    expect(upsertModelPrice).toHaveBeenCalledWith(expect.objectContaining({
      platform: 'anthropic',
      model: 'openai/gpt-5.4',
    }))
  })

  it('edits structured video pricing in the existing model price dialog', async () => {
    const videoPricing = {
      version: 1 as const,
      enabled: true,
      currency: 'USD' as const,
      defaults: { resolution: '480p', request_mode: 'standard' as const, inference_mode: 'online' as const },
      resolutions: { '480p': { sizes: ['864x480', '480x864'] } },
      estimators: {
        output: { type: 'pixel_frame' as const, token_scope: 'output_only' as const, fps: 24, divisor: 1024 },
      },
      rules: [{
        key: '480p-text', billing_unit: 'video_token' as const, unit_price_usd: 1e-6,
        estimator: 'output', conditions: { resolutions: ['480p'], input_has_video: false },
      }],
    }
    listModelPrices.mockResolvedValue({
      items: [{
        platform: 'openai', model: 'doubao-seedance-2.0-mini-480p', source: 'override', currency: 'USD',
        token_pricing_absent: true, has_image_pricing: false, has_video_pricing: true,
        video_pricing_valid: true, video_rule_count: 1, video_billing_units: ['video_token'], video_resolutions: ['480p'],
        sync_invalidated: false, redundant: false, effective: { video_pricing: videoPricing },
        overridden_fields: ['video_pricing'], override_platform: 'openai', enabled: true,
      }],
      total: 1,
    })
    getModelPriceEntry.mockResolvedValue({
      platform: 'openai', model: 'doubao-seedance-2.0-mini-480p', currency: 'USD',
      catalog: {}, override: { video_pricing: videoPricing }, effective: { video_pricing: videoPricing },
      billing_mode: 'video', enabled: true, token_pricing_absent: true, has_image_pricing: false, has_video_pricing: true,
      video_pricing_valid: true, video_rule_count: 1, video_billing_units: ['video_token'], video_resolutions: ['480p'],
      sync_invalidated: false, redundant: false, override_platform: 'openai',
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="video-pricing-editor"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('admin.modelPrices.video.resolutions')
    expect(wrapper.findAll('input').some((input) => input.element.value === '480p-text')).toBe(true)

    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.save')
    await saveButton!.trigger('click')
    await flushPromises()

    expect(upsertModelPrice).toHaveBeenCalledWith(expect.objectContaining({
      platform: 'openai',
      model: 'doubao-seedance-2.0-mini-480p',
      currency: 'USD',
      payload: expect.objectContaining({ video_pricing: videoPricing }),
    }))
  })

  it('creates a valid simple per-second video price', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()

    const videoTab = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.tabs.video')
    await videoTab!.trigger('click')
    await flushPromises()
    await wrapper.find('[data-test="video-mode-enabled"]').trigger('click')
    await flushPromises()

    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.save')
    await saveButton!.trigger('click')
    await flushPromises()
    expect(upsertModelPrice).toHaveBeenLastCalledWith(expect.objectContaining({
      payload: expect.objectContaining({
        video_pricing: expect.objectContaining({
          enabled: true,
          rules: [{ key: 'default', billing_unit: 'second', unit_price_usd: 0 }],
        }),
      }),
    }))
  })

  it('reopens a canonical simple video profile in simple mode and preserves it on save', async () => {
    const videoPricing = {
      version: 1 as const,
      enabled: true,
      currency: 'USD' as const,
      defaults: { request_mode: 'standard' as const, inference_mode: 'online' as const, generate_audio: false },
      estimators: { default: { type: 'fixed_tokens_per_second' as const, tokens_per_second: 48_600 } },
      rules: [{
        key: 'reference-video', billing_unit: 'video_token' as const, unit_price_usd: 23.25e-6,
        estimator: 'default', conditions: {
          operations: ['generate'], input_has_video: true,
          request_modes: ['standard' as const], inference_modes: ['online' as const],
        },
      }],
    }
    listModelPrices.mockResolvedValue({
      items: [{
        platform: 'openai', model: 'doubao-seedance-2.0-pro-1080p', source: 'override', currency: 'USD',
        token_pricing_absent: true, has_image_pricing: false, has_video_pricing: true,
        video_pricing_valid: true, video_rule_count: 1, video_billing_units: ['video_token'], video_resolutions: [],
        sync_invalidated: false, redundant: false, effective: { video_pricing: videoPricing },
        overridden_fields: ['video_pricing'], override_platform: 'openai', enabled: true,
      }],
      total: 1,
    })
    getModelPriceEntry.mockResolvedValue({
      platform: 'openai', model: 'doubao-seedance-2.0-pro-1080p', currency: 'USD', catalog: {},
      override: { video_pricing: videoPricing }, effective: { video_pricing: videoPricing }, billing_mode: 'video', enabled: true,
      token_pricing_absent: true, has_image_pricing: false, has_video_pricing: true,
      video_pricing_valid: true, video_rule_count: 1, video_billing_units: ['video_token'], video_resolutions: [],
      sync_invalidated: false, redundant: false, override_platform: 'openai',
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="video-simple-editor"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="video-editor-simple"]').classes()).toContain('experience-tab-active')

    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.save')
    await saveButton!.trigger('click')
    await flushPromises()

    expect(upsertModelPrice).toHaveBeenCalledWith(expect.objectContaining({
      payload: { video_pricing: videoPricing },
    }))
  })

  it('blocks an incomplete token estimator and saves after the required value is entered', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()

    const videoTab = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.tabs.video')
    await videoTab!.trigger('click')
    await flushPromises()
    await wrapper.find('[data-test="video-mode-enabled"]').trigger('click')
    await flushPromises()

    await wrapper.find('[data-test="video-simple-billing-unit"]').setValue('video_token')
    await flushPromises()
    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.save')
    await saveButton!.trigger('click')
    await flushPromises()
    expect(upsertModelPrice).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.modelPrices.video.validationEstimatorConfig')

    await wrapper.find('[data-test="video-simple-tokens-per-second"]').setValue('9720')
    await flushPromises()
    await saveButton!.trigger('click')
    await flushPromises()
    expect(upsertModelPrice).toHaveBeenCalledWith(expect.objectContaining({
      payload: expect.objectContaining({
        video_pricing: expect.objectContaining({
          estimators: expect.objectContaining({
            default: { type: 'fixed_tokens_per_second', tokens_per_second: 9720 },
          }),
          rules: expect.arrayContaining([expect.objectContaining({ billing_unit: 'video_token', estimator: 'default' })]),
        }),
      }),
    }))
  })

  it('shows DeepSeek peak and off-peak prices when a time schedule is present', async () => {
    listModelPrices.mockResolvedValue({
      items: [
        {
          platform: 'deepseek',
          model: 'deepseek-v4-flash',
          source: 'official',
          token_pricing_absent: false,
          has_image_pricing: false,
          sync_invalidated: false,
          redundant: false,
          effective: { input_cost_per_token: 3e-6, output_cost_per_token: 9e-6 },
          overridden_fields: [],
          enabled: true,
          time_schedule: {
            kind: 'deepseek_official',
            timezone: 'Asia/Shanghai',
            peak_windows: ['09:00-12:00', '14:00-18:00'],
            // source=official：生效价来自代码内官方兜底表，存的是高峰价。
            peak_multiplier: 1,
            off_peak_multiplier: 0.5,
          },
        },
      ],
      total: 1,
    })

    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('admin.modelPrices.noticeDeepSeekTime')
    expect(wrapper.text()).toContain('admin.modelPrices.peakPrice')
    expect(wrapper.text()).toContain('admin.modelPrices.offPeakPrice')
    expect(wrapper.text()).toContain('1.5')
    expect(wrapper.text()).toContain('4.5')
  })

  it('does not expose floating-point tails in DeepSeek prices', async () => {
    listModelPrices.mockResolvedValue({
      items: [
        {
          platform: 'deepseek',
          model: 'deepseek-v4-pro',
          source: 'override',
          token_pricing_absent: false,
          has_image_pricing: false,
          sync_invalidated: false,
          redundant: false,
          effective: { input_cost_per_token: 0.66e-6, output_cost_per_token: 1.98e-6 },
          overridden_fields: ['input_cost_per_token', 'output_cost_per_token'],
          enabled: true,
          time_schedule: {
            kind: 'deepseek_official',
            timezone: 'Asia/Shanghai',
            peak_windows: ['09:00-12:00', '14:00-18:00'],
            peak_multiplier: 2,
            off_peak_multiplier: 1,
          },
        },
      ],
      total: 1,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('3.96')
    expect(wrapper.text()).toContain('1.98')
    expect(wrapper.text()).not.toContain('3.9600000000000004')
    expect(wrapper.text()).not.toContain('1.9800000000000002')
  })

  it('submits the selected currency and warns when it replaces another catalog currency', async () => {
    getModelPriceEntry.mockResolvedValue({
      platform: 'zhipu',
      model: 'glm-5.1',
      currency: 'CNY',
      catalog_currency: 'USD',
      override_currency: 'CNY',
      catalog: { input_cost_per_token: 1e-6, output_cost_per_token: 4e-6 },
      override: { input_cost_per_token: 1.4e-6, output_cost_per_token: 4.4e-6 },
      effective: { input_cost_per_token: 1.4e-6, output_cost_per_token: 4.4e-6 },
      enabled: true,
      token_pricing_absent: false,
      has_image_pricing: false,
      sync_invalidated: false,
      redundant: false,
      override_platform: 'zhipu',
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('admin.modelPrices.crossCurrencyWarning')
    const editorSelects = wrapper.findAll('[data-test="editor"] [data-test="select"]')
    expect(editorSelects[1].element.value).toBe('CNY')

    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.save')
    expect(saveButton).toBeDefined()
    await saveButton!.trigger('click')
    await flushPromises()
    expect(upsertModelPrice).toHaveBeenCalledWith(expect.objectContaining({
      platform: 'zhipu',
      model: 'glm-5.1',
      currency: 'CNY',
      payload: expect.objectContaining({
        input_cost_per_token: 1.4e-6,
        output_cost_per_token: 4.4e-6,
      }),
    }))
  })

  it('allows saving with every price field blank to keep inherited pricing', async () => {
    getModelPriceEntry.mockResolvedValue({
      platform: 'anthropic',
      model: 'claude-sonnet-4',
      currency: 'USD',
      catalog_currency: 'USD',
      catalog: { input_cost_per_token: 3e-6, output_cost_per_token: 15e-6 },
      effective: { input_cost_per_token: 3e-6, output_cost_per_token: 15e-6 },
      enabled: true,
      token_pricing_absent: false,
      has_image_pricing: false,
      sync_invalidated: false,
      redundant: false,
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()

    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.save')
    expect(saveButton).toBeDefined()
    await saveButton!.trigger('click')
    await flushPromises()

    expect(upsertModelPrice).toHaveBeenCalledWith(expect.objectContaining({
      platform: 'anthropic',
      model: 'claude-sonnet-4',
      currency: 'USD',
      billing_mode: 'token',
      payload: {},
    }))
  })

  it('submits only the fields of the selected billing mode', async () => {
    upsertModelPrice.mockResolvedValue({
      override: {},
      warnings: [{ code: 'BILLING_MODE_SUPPRESSES_TOKEN', field: 'billing_mode' }],
    })
    getModelPriceEntry.mockResolvedValue({
      platform: 'anthropic',
      model: 'gemini-2.5-flash-image',
      currency: 'USD',
      catalog: { input_cost_per_token: 3e-7, output_cost_per_token: 2.5e-6, output_cost_per_image_token: 3e-5 },
      override: { input_cost_per_token: 3e-7 },
      effective: { input_cost_per_token: 3e-7, output_cost_per_token: 2.5e-6, output_cost_per_image_token: 3e-5 },
      billing_mode: 'token',
      enabled: true,
      token_pricing_absent: false,
      has_image_pricing: true,
      sync_invalidated: false,
      redundant: false,
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('admin.modelPrices.billingMode.discardNotice')

    const imageTab = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.tabs.image')
    await imageTab!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.modelPrices.billingMode.hint.image')
    // image 档接受 token 与图片价，编辑器里的值都不会被丢弃
    expect(wrapper.text()).not.toContain('admin.modelPrices.billingMode.discardNotice')

    // input[0] 是模型名，价格输入框按 PRICE_FIELDS 顺序排在其后
    const priceInput = (field: (typeof PRICE_FIELDS)[number]) =>
      wrapper.findAll('[data-test="editor"] input.input')[1 + PRICE_FIELDS.indexOf(field)]
    await priceInput('output_cost_per_image').setValue('0.04')
    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.save')
    await saveButton!.trigger('click')
    await flushPromises()

    // 图片按 token 计费是正常配置：已有的 token 覆盖随 image 档一起提交
    expect(upsertModelPrice).toHaveBeenCalledWith(expect.objectContaining({
      billing_mode: 'image',
      payload: { input_cost_per_token: 3e-7, output_cost_per_image: 0.04 },
    }))
    expect(showWarning).toHaveBeenCalledWith('admin.modelPrices.billingMode.suppressesTokenWarning', 8000)
  })

  it('warns about discarded values when switching to the video billing mode', async () => {
    getModelPriceEntry.mockResolvedValue({
      platform: 'anthropic',
      model: 'doubao-seedance-1-0-lite-t2v-250428',
      currency: 'USD',
      catalog: { input_cost_per_token: 3e-7, output_cost_per_token: 2.5e-6 },
      override: null,
      effective: { input_cost_per_token: 3e-7, output_cost_per_token: 2.5e-6 },
      billing_mode: 'token',
      enabled: true,
      token_pricing_absent: false,
      has_image_pricing: false,
      sync_invalidated: false,
      redundant: false,
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()

    await wrapper.findAll('[data-test="editor"] input.input')[1].setValue('0.5')
    const videoTab = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.tabs.video')
    await videoTab!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.modelPrices.billingMode.hint.video')
    expect(wrapper.text()).toContain('admin.modelPrices.billingMode.discardNotice')
  })

  it('keeps legacy image and video pricing when resaving a token-mode row', async () => {
    upsertModelPrice.mockResolvedValue({ override: {}, warnings: [] })
    const videoPricing = { enabled: true, rules: [{ key: 'default', billing_unit: 'second', unit_price_usd: 0.1 }] }
    getModelPriceEntry.mockResolvedValue({
      platform: 'anthropic',
      model: 'legacy-media-model',
      currency: 'USD',
      catalog: { input_cost_per_token: 3e-7, output_cost_per_token: 2.5e-6 },
      override: { input_cost_per_token: 3e-7, output_cost_per_image: 0.04, video_pricing: videoPricing },
      effective: { input_cost_per_token: 3e-7, output_cost_per_token: 2.5e-6, output_cost_per_image: 0.04 },
      billing_mode: 'token',
      enabled: true,
      token_pricing_absent: false,
      has_image_pricing: true,
      sync_invalidated: false,
      redundant: false,
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('admin.modelPrices.billingMode.discardNotice')
    expect(wrapper.text()).toContain('admin.modelPrices.billingMode.legacyVideoNotice')

    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.save')
    await saveButton!.trigger('click')
    await flushPromises()

    expect(upsertModelPrice).toHaveBeenCalledTimes(1)
    const request = upsertModelPrice.mock.calls[0][0]
    expect(request.billing_mode).toBe('token')
    expect(request.payload.input_cost_per_token).toBeCloseTo(3e-7, 15)
    expect(request.payload.output_cost_per_image).toBe(0.04)
    expect(request.payload.video_pricing).toBeTruthy()
  })

  it('does not scale long-context threshold and multipliers as per-MTok prices', async () => {
    upsertModelPrice.mockResolvedValue({ override: {}, warnings: [] })
    getModelPriceEntry.mockResolvedValue({
      platform: 'anthropic',
      model: 'claude-long',
      currency: 'USD',
      catalog: { input_cost_per_token: 3e-6, output_cost_per_token: 1.5e-5 },
      override: {
        input_cost_per_token: 3e-6,
        long_context_input_token_threshold: 200000,
        long_context_input_cost_multiplier: 2,
        long_context_output_cost_multiplier: 1.5,
      },
      effective: { input_cost_per_token: 3e-6, output_cost_per_token: 1.5e-5 },
      billing_mode: 'token',
      enabled: true,
      token_pricing_absent: false,
      has_image_pricing: false,
      sync_invalidated: false,
      redundant: false,
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()

    const priceInput = (field: (typeof PRICE_FIELDS)[number]) =>
      wrapper.findAll('[data-test="editor"] input.input')[1 + PRICE_FIELDS.indexOf(field)]
    expect((priceInput('long_context_input_token_threshold').element as HTMLInputElement).value).toBe('200000')
    expect((priceInput('long_context_input_cost_multiplier').element as HTMLInputElement).value).toBe('2')

    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.save')
    await saveButton!.trigger('click')
    await flushPromises()

    // 倍率 > 1 不应触发"价格量级"确认，也不应被除以 1e6
    const request = upsertModelPrice.mock.calls[0][0]
    expect(request.payload).toMatchObject({
      long_context_input_token_threshold: 200000,
      long_context_input_cost_multiplier: 2,
      long_context_output_cost_multiplier: 1.5,
    })
  })

  it('rejects an invalid price instead of silently inheriting the catalog value', async () => {
    getModelPriceEntry.mockResolvedValue({
      platform: 'anthropic',
      model: 'claude-typo',
      currency: 'USD',
      catalog: { input_cost_per_token: 3e-6, output_cost_per_token: 1.5e-5 },
      override: null,
      effective: { input_cost_per_token: 3e-6, output_cost_per_token: 1.5e-5 },
      billing_mode: 'token',
      enabled: true,
      token_pricing_absent: false,
      has_image_pricing: false,
      sync_invalidated: false,
      redundant: false,
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()
    await wrapper.findAll('[data-test="editor"] input.input')[1].setValue('3,5')
    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.modelPrices.save')
    await saveButton!.trigger('click')
    await flushPromises()

    expect(upsertModelPrice).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.modelPrices.invalidNumber')
  })
})
