import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ModelPricesView from '@/views/admin/ModelPricesView.vue'

const {
  listModelPrices,
  listModelPricePlatforms,
  getModelPriceSyncStatus,
  getModelPriceEntry,
  upsertModelPrice,
  showError,
} = vi.hoisted(() => ({
  listModelPrices: vi.fn(),
  listModelPricePlatforms: vi.fn(),
  getModelPriceSyncStatus: vi.fn(),
  getModelPriceEntry: vi.fn(),
  upsertModelPrice: vi.fn(),
  showError: vi.fn(),
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
    upsertModelPrice,
    deleteModelPrice: vi.fn(),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError }),
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
    showError.mockReset()
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
      payload: {},
    }))
  })
})
