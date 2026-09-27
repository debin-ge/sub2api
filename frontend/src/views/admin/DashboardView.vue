<template>
  <AppLayout>
    <div class="zt-stack">
      <!-- Loading State -->
      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner />
      </div>

      <template v-else-if="stats">
        <!-- Row 1: Core Stats -->
        <KpiRow :cols="upstreamBalance?.enabled ? 5 : 4">
          <KpiItem :label="t('admin.dashboard.apiKeys')" :value="stats.total_api_keys">
            <template #sub><span class="zt-good-text">{{ stats.active_api_keys }} {{ t('common.active') }}</span></template>
          </KpiItem>
          <KpiItem :label="t('admin.dashboard.accounts')" :value="stats.total_accounts">
            <template #sub>
              <span class="zt-good-text">{{ stats.normal_accounts }} {{ t('common.active') }}</span>
              <span v-if="stats.error_accounts > 0" class="zt-bad-text">{{ stats.error_accounts }} {{ t('common.error') }}</span>
            </template>
          </KpiItem>
          <KpiItem :label="t('admin.dashboard.todayRequests')" :value="stats.today_requests" :sub="`${t('common.total')}: ${formatNumber(stats.total_requests)}`" />
          <KpiItem :label="t('admin.dashboard.users')" :value="`+${stats.today_new_users}`" value-color="var(--zt-good)" :sub="`${t('common.total')}: ${formatNumber(stats.total_users)}`" />
          <KpiItem v-if="upstreamBalance?.enabled" :label="t('admin.dashboard.upstreamBalance')" :sub="upstreamBalanceStatusText">
            <template #value>
              <span v-if="upstreamBalance.status === 'ok'">${{ formatCost(upstreamBalance.balance) }}</span>
              <span v-else>--</span>
            </template>
          </KpiItem>
        </KpiRow>

        <!-- Row 2: Token Stats -->
        <KpiRow :cols="4">
          <KpiItem :label="t('admin.dashboard.todayTokens')" :value="formatTokens(stats.today_tokens)">
            <template #sub>
              <span class="zt-good-text" :title="t('admin.dashboard.actual')">${{ formatCost(stats.today_actual_cost) }}</span>
              <span>/</span>
              <span class="zt-warn-text" :title="t('admin.dashboard.accountCost')">${{ formatCost(stats.today_account_cost) }}</span>
              <span>/</span>
              <span :title="t('admin.dashboard.standard')">${{ formatCost(stats.today_cost) }}</span>
            </template>
          </KpiItem>
          <KpiItem :label="t('admin.dashboard.totalTokens')" :value="formatTokens(stats.total_tokens)">
            <template #sub>
              <span class="zt-good-text" :title="t('admin.dashboard.actual')">${{ formatCost(stats.total_actual_cost) }}</span>
              <span>/</span>
              <span class="zt-warn-text" :title="t('admin.dashboard.accountCost')">${{ formatCost(stats.total_account_cost) }}</span>
              <span>/</span>
              <span :title="t('admin.dashboard.standard')">${{ formatCost(stats.total_cost) }}</span>
            </template>
          </KpiItem>
          <KpiItem :label="t('admin.dashboard.performance')">
            <template #value>{{ formatTokens(stats.rpm) }}<small>RPM</small></template>
            <template #sub><span class="zt-kpi-second">{{ formatTokens(stats.tpm) }}</span><span>TPM</span></template>
          </KpiItem>
          <KpiItem :label="t('admin.dashboard.avgResponse')" :value="formatDuration(stats.average_duration_ms)" :sub="`${stats.active_users} ${t('admin.dashboard.activeUsers')}`" />
        </KpiRow>

        <!-- Charts Section -->
        <div class="zt-stack">
          <!-- Date Range Filter -->
          <div class="zt-filters">
            <span class="zt-filters-label">{{ t('admin.dashboard.timeRange') }}</span>
            <DateRangePicker
              v-model:start-date="startDate"
              v-model:end-date="endDate"
              @change="onDateRangeChange"
            />
            <button @click="loadDashboardStats" :disabled="chartsLoading" class="btn btn-secondary">
              {{ t('common.refresh') }}
            </button>
            <span class="zt-filters-spacer"></span>
            <span class="zt-filters-label">{{ t('admin.dashboard.granularity') }}</span>
            <div class="w-28">
              <Select
                v-model="granularity"
                :options="granularityOptions"
                @change="loadChartData"
              />
            </div>
          </div>

          <!-- Charts Grid -->
          <div class="zt-two">
            <ModelDistributionChart
              :model-stats="modelStats"
              :enable-ranking-view="true"
              :ranking-items="rankingItems"
              :ranking-total-actual-cost="rankingTotalActualCost"
              :ranking-total-requests="rankingTotalRequests"
              :ranking-total-tokens="rankingTotalTokens"
              :loading="chartsLoading"
              :ranking-loading="rankingLoading"
              :ranking-error="rankingError"
              :start-date="startDate"
              :end-date="endDate"
              @ranking-click="goToUserUsage"
            />
            <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" />
          </div>

          <!-- User Usage Trend (Full Width) -->
          <div class="card p-4">
            <h3 class="mb-4 text-sm font-semibold zt-ink">
              {{ t('admin.dashboard.recentUsage') }} (Top 12)
            </h3>
            <div class="h-64">
              <div v-if="userTrendLoading" class="flex h-full items-center justify-center">
                <LoadingSpinner size="md" />
              </div>
              <Line v-else-if="userTrendChartData" :data="userTrendChartData" :options="lineOptions" />
              <div
                v-else
                class="flex h-full items-center justify-center text-sm zt-ink-3"
              >
                {{ t('admin.dashboard.noDataAvailable') }}
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
import { adminAPI } from '@/api/admin'
import type { ResellerUpstreamBalance } from '@/api/admin/reseller'
import type {
  DashboardStats,
  TrendDataPoint,
  ModelStat,
  UserUsageTrendPoint,
  UserSpendingRankingItem
} from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import { KpiRow, KpiItem } from '@/components/console'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select from '@/components/common/Select.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import { getLast24HoursRange } from '@/utils/dateRange'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'

import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'

// Register Chart.js components
ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
)

const appStore = useAppStore()
const router = useRouter()
const { refreshBatchImageAccess } = useBatchImageAccess()
const stats = ref<DashboardStats | null>(null)
const loading = ref(false)
const chartsLoading = ref(false)
const userTrendLoading = ref(false)
const rankingLoading = ref(false)
const rankingError = ref(false)
const upstreamBalance = ref<ResellerUpstreamBalance | null>(null)

// Chart data
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const userTrend = ref<UserUsageTrendPoint[]>([])
const rankingItems = ref<UserSpendingRankingItem[]>([])
const rankingTotalActualCost = ref(0)
const rankingTotalRequests = ref(0)
const rankingTotalTokens = ref(0)
let chartLoadSeq = 0
let usersTrendLoadSeq = 0
let rankingLoadSeq = 0
const rankingLimit = 12

// Date range
const granularity = ref<'day' | 'hour'>('hour')
const defaultRange = getLast24HoursRange()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)
// Exact bounds of the default "last 24 hours" window; without them the two
// dates above would be read as a 48-hour range.
const startTime = ref<string | undefined>(defaultRange.startTime)
const endTime = ref<string | undefined>(defaultRange.endTime)

// Granularity options for Select component
const granularityOptions = computed(() => [
  { value: 'day', label: t('admin.dashboard.day') },
  { value: 'hour', label: t('admin.dashboard.hour') }
])

const upstreamBalanceStatusText = computed(() => {
  switch (upstreamBalance.value?.status) {
    case 'ok':
      return t('admin.dashboard.upstreamBalanceConnected')
    case 'not_configured':
      return t('admin.dashboard.upstreamBalanceNotConfigured')
    case 'auth_failed':
      return t('admin.dashboard.upstreamBalanceAuthFailed')
    case 'upstream_unreachable':
      return t('admin.dashboard.upstreamBalanceUnreachable')
    case 'invalid_response':
      return t('admin.dashboard.upstreamBalanceInvalidResponse')
    case 'upstream_error':
      return t('admin.dashboard.upstreamBalanceError')
    default:
      return t('admin.dashboard.upstreamBalanceDisabled')
  }
})

// Dark mode detection
const isDarkMode = computed(() => {
  return document.documentElement.classList.contains('dark')
})

// Chart colors
const chartColors = computed(() => ({
  text: isDarkMode.value ? '#e5e7eb' : '#374151',
  grid: isDarkMode.value ? '#374151' : '#e5e7eb'
}))

// Line chart options (for user trend chart)
const lineOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    intersect: false,
    mode: 'index' as const
  },
  plugins: {
    legend: {
      position: 'top' as const,
      labels: {
        color: chartColors.value.text,
        usePointStyle: true,
        pointStyle: 'circle',
        padding: 15,
        font: {
          size: 11
        }
      }
    },
    tooltip: {
      itemSort: (a: any, b: any) => {
        const aValue = typeof a?.raw === 'number' ? a.raw : Number(a?.parsed?.y ?? 0)
        const bValue = typeof b?.raw === 'number' ? b.raw : Number(b?.parsed?.y ?? 0)
        return bValue - aValue
      },
      callbacks: {
        label: (context: any) => {
          return `${context.dataset.label}: ${formatTokens(context.raw)}`
        }
      }
    }
  },
  scales: {
    x: {
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        }
      }
    },
    y: {
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        },
        callback: (value: string | number) => formatTokens(Number(value))
      }
    }
  }
}))

// User trend chart data
const userTrendChartData = computed(() => {
  if (!userTrend.value?.length) return null

  const getDisplayName = (point: UserUsageTrendPoint): string => {
    const username = point.username?.trim()
    if (username) {
      return username
    }

    const email = point.email?.trim()
    if (email) {
      return email
    }

    return t('admin.redeem.userPrefix', { id: point.user_id })
  }

  // Group by user_id to avoid merging different users with the same display name
  const userGroups = new Map<number, { name: string; data: Map<string, number> }>()
  const allDates = new Set<string>()

  userTrend.value.forEach((point) => {
    allDates.add(point.date)
    const key = point.user_id
    if (!userGroups.has(key)) {
      userGroups.set(key, { name: getDisplayName(point), data: new Map() })
    }
    userGroups.get(key)!.data.set(point.date, point.tokens)
  })

  const sortedDates = Array.from(allDates).sort()
  const colors = [
    '#3b82f6',
    '#10b981',
    '#f59e0b',
    '#ef4444',
    '#8b5cf6',
    '#ec4899',
    '#14b8a6',
    '#f97316',
    '#6366f1',
    '#84cc16',
    '#06b6d4',
    '#a855f7'
  ]

  const datasets = Array.from(userGroups.values()).map((group, idx) => ({
    label: group.name,
    data: sortedDates.map((date) => group.data.get(date) || 0),
    borderColor: colors[idx % colors.length],
    backgroundColor: `${colors[idx % colors.length]}20`,
    fill: false,
    tension: 0.3
  }))

  return {
    labels: sortedDates,
    datasets
  }
})

// Format helpers
const formatTokens = (value: number | undefined): string => {
  if (value === undefined || value === null) return '0'
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const toFiniteNumber = (value: unknown): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

const formatNumber = (value: number | null | undefined): string => {
  return toFiniteNumber(value).toLocaleString()
}

const formatCost = (value: number | null | undefined): string => {
  const safeValue = toFiniteNumber(value)
  if (safeValue >= 1000) {
    return (safeValue / 1000).toFixed(2) + 'K'
  } else if (safeValue >= 1) {
    return safeValue.toFixed(2)
  } else if (safeValue >= 0.01) {
    return safeValue.toFixed(3)
  }
  return safeValue.toFixed(4)
}

const formatDuration = (ms: number): string => {
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(2)}s`
  }
  return `${Math.round(ms)}ms`
}

const goToUserUsage = (item: UserSpendingRankingItem) => {
  void router.push({
    path: '/admin/usage',
    query: {
      user_id: String(item.user_id),
      start_date: startDate.value,
      end_date: endDate.value
    }
  })
}

// Date range change handler
const onDateRangeChange = (range: {
  startDate: string
  endDate: string
  startTime?: string
  endTime?: string
  preset: string | null
}) => {
  // undefined for every non-rolling preset and for hand-typed dates, which is
  // what makes the window fall back to whole calendar days.
  startTime.value = range.startTime
  endTime.value = range.endTime

  // Auto-select granularity based on date range
  const start = new Date(range.startDate)
  const end = new Date(range.endDate)
  const daysDiff = Math.ceil((end.getTime() - start.getTime()) / (1000 * 60 * 60 * 24))

  // If range is 1 day, use hourly granularity
  if (daysDiff <= 1) {
    granularity.value = 'hour'
  } else {
    granularity.value = 'day'
  }

  loadChartData()
}

// Load data
const loadDashboardSnapshot = async (includeStats: boolean) => {
  const currentSeq = ++chartLoadSeq
  if (includeStats && !stats.value) {
    loading.value = true
  }
  chartsLoading.value = true
  try {
    const response = await adminAPI.dashboard.getSnapshotV2({
      start_date: startDate.value,
      end_date: endDate.value,
      start_time: startTime.value,
      end_time: endTime.value,
      granularity: granularity.value,
      include_stats: includeStats,
      include_trend: true,
      include_model_stats: true,
      include_group_stats: false,
      include_users_trend: false
    })
    if (currentSeq !== chartLoadSeq) return
    if (includeStats && response.stats) {
      stats.value = response.stats
    }
    trendData.value = response.trend || []
    modelStats.value = response.models || []
  } catch (error) {
    if (currentSeq !== chartLoadSeq) return
    appStore.showError(t('admin.dashboard.failedToLoad'))
    console.error('Error loading dashboard snapshot:', error)
  } finally {
    if (currentSeq === chartLoadSeq) {
      loading.value = false
      chartsLoading.value = false
    }
  }
}

const loadUsersTrend = async () => {
  const currentSeq = ++usersTrendLoadSeq
  userTrendLoading.value = true
  try {
    const response = await adminAPI.dashboard.getUserUsageTrend({
      start_date: startDate.value,
      end_date: endDate.value,
      start_time: startTime.value,
      end_time: endTime.value,
      granularity: granularity.value,
      limit: 12
    })
    if (currentSeq !== usersTrendLoadSeq) return
    userTrend.value = response.trend || []
  } catch (error) {
    if (currentSeq !== usersTrendLoadSeq) return
    console.error('Error loading users trend:', error)
    userTrend.value = []
  } finally {
    if (currentSeq === usersTrendLoadSeq) {
      userTrendLoading.value = false
    }
  }
}

const loadUserSpendingRanking = async () => {
  const currentSeq = ++rankingLoadSeq
  rankingLoading.value = true
  rankingError.value = false
  try {
    const response = await adminAPI.dashboard.getUserSpendingRanking({
      start_date: startDate.value,
      end_date: endDate.value,
      start_time: startTime.value,
      end_time: endTime.value,
      limit: rankingLimit
    })
    if (currentSeq !== rankingLoadSeq) return
    rankingItems.value = response.ranking || []
    rankingTotalActualCost.value = response.total_actual_cost || 0
    rankingTotalRequests.value = response.total_requests || 0
    rankingTotalTokens.value = response.total_tokens || 0
  } catch (error) {
    if (currentSeq !== rankingLoadSeq) return
    console.error('Error loading user spending ranking:', error)
    rankingItems.value = []
    rankingTotalActualCost.value = 0
    rankingTotalRequests.value = 0
    rankingTotalTokens.value = 0
    rankingError.value = true
  } finally {
    if (currentSeq === rankingLoadSeq) {
      rankingLoading.value = false
    }
  }
}

const loadUpstreamBalance = async () => {
  try {
    upstreamBalance.value = await adminAPI.reseller.getUpstreamBalance()
  } catch (error) {
    upstreamBalance.value = null
    console.error('Error loading upstream balance:', error)
  }
}

const loadDashboardStats = async () => {
  await Promise.all([
    loadDashboardSnapshot(true),
    loadUsersTrend(),
    loadUserSpendingRanking(),
    loadUpstreamBalance()
  ])
}

const loadChartData = async () => {
  await Promise.all([
    loadDashboardSnapshot(false),
    loadUsersTrend(),
    loadUserSpendingRanking()
  ])
}

onMounted(() => {
  void refreshBatchImageAccess()
  loadDashboardStats()
})
</script>

<style scoped>
</style>
