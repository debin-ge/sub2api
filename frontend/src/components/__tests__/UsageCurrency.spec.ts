import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { useAppStore } from '@/stores'
import type { Account, AccountUsageStatsResponse, AdminUsageLog, ApiKey, PublicSettings, UsageLog } from '@/types'
import UsageStatsCards from '@/components/admin/usage/UsageStatsCards.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'
import UserTokenRanking from '@/components/admin/usage/UserTokenRanking.vue'
import ApiKeyTokenRanking from '@/components/admin/usage/ApiKeyTokenRanking.vue'
import type { UserDashboardStats as DashboardStats } from '@/api/usage'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'
import EndpointDistributionChart from '@/components/charts/EndpointDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import KeyDetail from '@/components/keys/KeyDetail.vue'
import AccountStatsModal from '@/components/account/AccountStatsModal.vue'
import AdminAccountStatsModal from '@/components/admin/account/AccountStatsModal.vue'
import AccountTodayStatsCell from '@/components/account/AccountTodayStatsCell.vue'
import AccountCapacityCell from '@/components/account/AccountCapacityCell.vue'
import UsageProgressBar from '@/components/account/UsageProgressBar.vue'
import QuotaBadge from '@/components/account/QuotaBadge.vue'
import UserBreakdownSubTable from '@/components/charts/UserBreakdownSubTable.vue'
import { formatCurrency } from '@/utils/format'
import { formatTokenPricePerMillion } from '@/utils/usagePricing'

const { getUserBreakdown, getApiKeyBreakdown, getAccountStats } = vi.hoisted(() => ({
  getUserBreakdown: vi.fn(),
  getApiKeyBreakdown: vi.fn(),
  getAccountStats: vi.fn(),
}))
vi.mock('@/api/admin/dashboard', () => ({ getUserBreakdown, getApiKeyBreakdown }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getStats: getAccountStats } } }))
vi.mock('@/stores', async () => {
  const { reactive } = await import('vue')
  const store = reactive({ cachedPublicSettings: null as PublicSettings | null })
  return { useAppStore: () => store }
})
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('@/utils/ipGeoLookup', () => ({ getEntry: vi.fn(), fetchBatch: vi.fn() }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) =>
    key === 'admin.accounts.usageWindow.estimatedTotalCost' ? `Est. total ${params?.symbol}${params?.cost}` : key }),
}))
vi.mock('vue-chartjs', () => ({
  Doughnut: { name: 'Doughnut', props: ['data', 'options'], template: '<div />' },
  Line: { name: 'Line', props: ['data', 'options'], template: '<div />' },
}))
const global = { stubs: { Icon: true, LoadingSpinner: true, RouterLink: true, Select: true, Teleport: true } }
enableAutoUnmount(afterEach)
beforeEach(() => { useAppStore().cachedPublicSettings = null })
async function setCurrency(currency: 'USD' | 'CNY') {
  useAppStore().cachedPublicSettings = { balance_currency: currency } as PublicSettings
  await nextTick()
}
const stats = {
  total_requests: 1, total_input_tokens: 10, total_output_tokens: 20, total_cache_tokens: 0,
  total_tokens: 30, total_cost: 2, total_actual_cost: 1.25, total_account_cost: 0.5, average_duration_ms: 10,
}

describe('account currency in usage and statistics', () => {
  it('defaults to USD and reacts to CNY for user and upstream costs without changing amounts', async () => {
    const wrapper = mount(UsageStatsCards, { props: { stats }, global })
    expect(wrapper.text()).toContain('$1.2500')
    expect(wrapper.text()).toContain('$0.5000')
    await setCurrency('CNY')
    expect(wrapper.text()).toContain('¥1.2500')
    expect(wrapper.text()).toContain('¥2.0000')
    expect(wrapper.text()).toContain('¥0.5000')
    expect(wrapper.text()).not.toContain('$')
    await setCurrency('USD')
    expect(wrapper.text()).toContain('$1.2500')
    expect(wrapper.text()).toContain('$0.5000')
    expect(wrapper.text()).not.toContain('¥')
  })

  it('updates usage rows, breakdowns and token unit prices, including account billed cost', async () => {
    const row = {
      request_id: 'currency-test', model: 'test-model', billing_mode: 'token',
      input_tokens: 1000, output_tokens: 0, input_cost: 0.002, output_cost: 0,
      cache_creation_cost: 0, cache_read_cost: 0, total_cost: 0.002, actual_cost: 0.001,
      rate_multiplier: 0.5, account_rate_multiplier: 1,
    } as AdminUsageLog
    const wrapper = mount(UsageTable, {
      props: { data: [row], columns: [{ key: 'cost', label: 'Cost' }] },
      global: { stubs: { ...global.stubs, DataTable: {
        props: ['data'], template: '<div><slot name="cell-cost" :row="data[0]" /></div>',
      } } },
    })
    expect(wrapper.text()).toContain('$0.001000')
    await setCurrency('CNY')
    expect(wrapper.text()).toContain('¥0.001000')
    expect(wrapper.text()).toContain('A ¥0.002000')
    await wrapper.get('.group.relative').trigger('mouseenter')
    await flushPromises()
    expect(wrapper.text()).toContain('¥0.00200000')
    expect(wrapper.text()).toContain('¥2.0000')
    expect(wrapper.text()).toContain('¥0.00100000')
    expect(wrapper.text()).not.toContain('$')
    expect(formatTokenPricePerMillion(0.002, 1000, { currencySymbol: '¥' })).toBe('¥2.0000')
  })

  it('updates model, group and endpoint statistics together with chart tooltip callbacks', async () => {
    const amounts = { requests: 1, input_tokens: 10, output_tokens: 20, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 30, cost: 2, actual_cost: 1.25, account_cost: 0.5 }
    const model = mount(ModelDistributionChart, { props: { modelStats: [{ ...amounts, model: 'test-model' }], metric: 'actual_cost' }, global })
    const group = mount(GroupDistributionChart, { props: { groupStats: [{ ...amounts, group_id: 1, group_name: 'test-group' }], metric: 'actual_cost' }, global })
    const endpoint = mount(EndpointDistributionChart, { props: { endpointStats: [{ ...amounts, endpoint: '/v1/responses' }], metric: 'actual_cost' }, global })
    for (const wrapper of [model, group, endpoint]) {
      expect(wrapper.text()).toContain('$1.25')
    }
    await setCurrency('CNY')
    for (const wrapper of [model, group, endpoint]) {
      expect(wrapper.text()).toContain('¥1.25')
      expect(wrapper.text()).toContain('¥2.00')
      const options = wrapper.getComponent({ name: 'Doughnut' }).props('options')
      expect(options.plugins.tooltip.callbacks.label({ label: 'Test', raw: 1.25, dataset: { data: [1.25] } })).toContain('¥1.25')
    }
    expect(model.text()).toContain('¥0.500')
    expect(group.text()).toContain('¥0.500')
    expect(model.text()).not.toContain('$')
    expect(group.text()).not.toContain('$')
  })

  it('updates upstream cost cells, window estimates, quotas and user breakdowns without converting amounts', async () => {
    const windowStats = { requests: 7, tokens: 1234, cost: 1.25, user_cost: 2.5 }
    const today = mount(AccountTodayStatsCell, { props: { stats: windowStats }, global })
    const progress = mount(UsageProgressBar, { props: { label: '5h', color: 'indigo', utilization: 25, windowStats, estimatedTotalCost: 5 }, global })
    const quota = mount(QuotaBadge, { props: { used: 1.25, limit: 10 }, global })
    const capacity = mount(AccountCapacityCell, { props: { account: { platform: 'anthropic', type: 'oauth', concurrency: 2, current_window_cost: 1.25, window_cost_limit: 10 } as Account }, global })
    const users = mount(UserBreakdownSubTable, { props: { items: [{ user_id: 1, email: 'test@example.com', requests: 7, total_tokens: 1234, actual_cost: 2.5, account_cost: 1.25, cost: 5 }] }, global })
    for (const currency of ['USD', 'CNY', 'USD'] as const) {
      await setCurrency(currency)
      const symbol = currency === 'CNY' ? '¥' : '$'
      expect(today.text()).toContain(formatCurrency(1.25, currency))
      expect(today.text()).toContain(formatCurrency(2.5, currency))
      expect(progress.text()).toContain(`A ${symbol}1.25`)
      expect(progress.text()).toContain(`U ${symbol}2.50`)
      expect(progress.get('[data-test="estimated-total-cost"]').text()).toBe(`Est. total ${symbol}5.00`)
      for (const wrapper of [quota, capacity]) {
        expect(wrapper.text()).toContain(`${symbol}1.25`)
        expect(wrapper.text()).toContain(`${symbol}10.00`)
      }
      expect(users.text()).toContain(`${symbol}1.25`)
      for (const wrapper of [today, progress, quota, capacity, users]) {
        expect(wrapper.text()).not.toContain(currency === 'CNY' ? '$' : '¥')
      }
    }
    expect(windowStats).toEqual({ requests: 7, tokens: 1234, cost: 1.25, user_cost: 2.5 })
  })

  it.each([['account', AccountStatsModal], ['admin account', AdminAccountStatsModal]] as const)(
    'updates %s statistics totals, chart labels, tooltips and axes together', async (_name, component) => {
      const day = { date: '2026-11-15', label: '11/15', cost: 1.25, user_cost: 2.5, requests: 7, tokens: 1234 }
      const response: AccountUsageStatsResponse = {
        history: [{ ...day, cost: 5, actual_cost: 1.25 }],
        summary: {
          days: 30, actual_days_used: 1, total_cost: 30.5, total_user_cost: 61, total_standard_cost: 100,
          total_requests: 7, total_tokens: 1234, avg_daily_cost: 1.25, avg_daily_user_cost: 2.5,
          avg_daily_requests: 7, avg_daily_tokens: 1234, avg_duration_ms: 10,
          today: day, highest_cost_day: day, highest_request_day: day,
        },
        models: [], endpoints: [], upstream_endpoints: [],
      }
      getAccountStats.mockResolvedValue(response)
      const wrapper = mount(component, {
        props: { show: false, account: { id: 1, name: 'upstream-account', status: 'active' } as Account },
        global: { stubs: { ...global.stubs, BaseDialog: { template: '<div><slot /></div>' }, ModelDistributionChart: true, EndpointDistributionChart: true } },
      })
      await wrapper.setProps({ show: true })
      await flushPromises()
      expect(wrapper.text()).toContain('$30.50')
      for (const currency of ['USD', 'CNY', 'USD'] as const) {
        await setCurrency(currency)
        const symbol = currency === 'CNY' ? '¥' : '$'
        expect(wrapper.text()).toContain(`${symbol}30.50`)
        expect(wrapper.text()).toContain(`${symbol}1.25`)
        expect(wrapper.text()).not.toContain(currency === 'CNY' ? '$' : '¥')
        const chart = wrapper.getComponent({ name: 'Line' })
        const data = chart.props('data')
        expect(data.datasets[0].label).toBe(`usage.accountBilled (${currency})`)
        expect(data.datasets[1].label).toBe(`usage.userBilled (${currency})`)
        expect(data.datasets.map((dataset: { data: number[] }) => dataset.data)).toEqual([[1.25], [2.5], [7]])
        const options = chart.props('options')
        expect(options.scales.y.ticks.callback(1.25)).toBe(`${symbol}1.25`)
        expect(options.scales.y.title.text).toBe(`usage.accountBilled (${currency})`)
        const tooltip = options.plugins.tooltip.callbacks.label
        expect(tooltip({ datasetIndex: 0, dataset: data.datasets[0], raw: 1.25 })).toBe(`usage.accountBilled (${currency}): ${symbol}1.25`)
        expect(tooltip({ datasetIndex: 1, dataset: data.datasets[1], raw: 2.5 })).toBe(`usage.userBilled (${currency}): ${symbol}2.50`)
        expect(tooltip({ datasetIndex: 2, dataset: data.datasets[2], raw: 7 })).toBe('admin.accounts.stats.requests: 7')
      }
    },
  )

  it('updates trend tooltip totals and dashboard recent usage', async () => {
    const trend = mount(TokenUsageTrend, { props: { trendData: [{ date: '2026-11-15', requests: 1, input_tokens: 10, output_tokens: 20, cache_creation_tokens: 0, cache_read_tokens: 0, cost: 2, actual_cost: 1.25 }] }, global })
    const recent = mount(UserDashboardRecentUsage, { props: { loading: false, data: [{ id: 1, model: 'test-model', created_at: '2026-11-15T10:00:00Z', input_tokens: 10, output_tokens: 20, actual_cost: 1.25, total_cost: 2 } as UsageLog] }, global })
    await setCurrency('CNY')
    expect(recent.text()).toContain('¥1.2500')
    expect(recent.text()).toContain('¥2.0000')
    const options = trend.getComponent({ name: 'Line' }).props('options')
    expect(options.plugins.tooltip.callbacks.footer([{ dataIndex: 0 }])).toBe('Actual: ¥1.25 | Standard: ¥2.00')
  })

  it('updates user and API key spending rankings without changing token totals', async () => {
    const item = { user_id: 1, email: 'test@example.com', api_key_id: 1, key_name: 'test-key', requests: 1, input_tokens: 10, output_tokens: 20, cache_tokens: 0, total_tokens: 30, actual_cost: 1.25 }
    getUserBreakdown.mockResolvedValue({ users: [item] })
    getApiKeyBreakdown.mockResolvedValue({ api_keys: [item] })
    const props = { startDate: '2026-11-15', endDate: '2026-11-15', filters: {} }
    const users = mount(UserTokenRanking, { props, global })
    const keys = mount(ApiKeyTokenRanking, { props: { ...props, mode: 'admin' }, global })
    await flushPromises()
    for (const wrapper of [users, keys]) expect(wrapper.text()).toContain('$1.2500')
    await setCurrency('CNY')
    for (const wrapper of [users, keys]) {
      expect(wrapper.text()).toContain('¥1.2500')
      expect(wrapper.text()).toContain('30')
      expect(wrapper.text()).not.toContain('$1.2500')
    }
  })

  it('updates dashboard totals and API key quotas alongside their billed usage', async () => {
    const dashboard = mount(UserDashboardStats, { props: { balance: 10, stats: { today_actual_cost: 1.25, total_actual_cost: 2.5, today_cost: 2, total_cost: 4 } as DashboardStats, isSimple: false, platformQuotas: [] }, global })
    const key = mount(KeyDetail, { props: { apiKey: { id: 1, name: 'test-key', key: 'sk-test', status: 'active', quota: 10, quota_used: 1.25 } as ApiKey, stats: { api_key_id: 1, today_actual_cost: 1.25, total_actual_cost: 2.5 }, formatResetTime: () => '' }, global })
    await setCurrency('CNY')
    expect(dashboard.text()).toContain('¥1.2500')
    expect(dashboard.text()).toContain('¥2.5000')
    expect(key.text()).toContain('¥1.2500')
    expect(key.text()).toContain('¥1.25 / ¥10.00')
  })
})
