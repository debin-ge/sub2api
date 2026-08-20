import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ModelPricesView from '@/views/admin/ModelPricesView.vue'

const {
  listModelPrices,
  listModelPricePlatforms,
  getModelPriceSyncStatus,
  getModelPriceEntry,
  showError,
} = vi.hoisted(() => ({
  listModelPrices: vi.fn(),
  listModelPricePlatforms: vi.fn(),
  getModelPriceSyncStatus: vi.fn(),
  getModelPriceEntry: vi.fn(),
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
    upsertModelPrice: vi.fn(),
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

function mountView() {
  return mount(ModelPricesView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        Select: true,
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
    showError.mockReset()
    listModelPricePlatforms.mockResolvedValue(['*', 'anthropic'])
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
      platform: '*',
      model: 'openai/gpt-5.4',
      catalog: {},
      override: { input_cost_per_token: 3e-6 },
      effective: { input_cost_per_token: 3e-6 },
      enabled: true,
      token_pricing_absent: false,
      has_image_pricing: false,
      sync_invalidated: false,
      redundant: false,
    })
  })

  it('loads slash model names and opens them with query-style detail', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(listModelPrices).toHaveBeenCalled()
    expect(wrapper.text()).toContain('openai/gpt-5.4')

    await wrapper.find('button.action-btn').trigger('click')
    await flushPromises()
    expect(getModelPriceEntry).toHaveBeenCalledWith('*', 'openai/gpt-5.4')
    expect(wrapper.text()).toContain('admin.modelPrices.fields.input_cost_per_token')
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
})
