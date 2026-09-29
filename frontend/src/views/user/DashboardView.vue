<template>
  <AppLayout>
    <div class="zt-stack">
      <div v-if="loading" class="flex items-center justify-center py-12"><LoadingSpinner /></div>
      <template v-else-if="stats">
        <UserDashboardStats :stats="stats" :balance="user?.balance || 0" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" />
        <UserDashboardCharts v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :trend="trendData" :models="modelStats" :start-time="startTime" :end-time="endTime" @dateRangeChange="onDateRangeChange" @granularityChange="loadCharts" @refresh="refreshAll" />
        <UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" />
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'; import { useAuthStore } from '@/stores/auth'; import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'; import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'; import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'
import { getMyPlatformQuotas } from '@/api/user'
import { formatDateLocalInput } from '@/utils/format'
import { getLast24HoursRange } from '@/utils/dateRange'

const authStore = useAuthStore(); const user = computed(() => authStore.user)
const stats = ref<UserStatsType | null>(null); const loading = ref(false); const loadingUsage = ref(false); const loadingCharts = ref(false)
const trendData = ref<TrendDataPoint[]>([]); const modelStats = ref<ModelStat[]>([]); const recentUsage = ref<UsageLog[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)

const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000))); const endDate = ref(formatDateLocalInput(new Date())); const granularity = ref('day')
// Exact bounds, set only while a rolling preset (last 24 hours) is active.
const startTime = ref<string | undefined>(); const endTime = ref<string | undefined>(); const activePreset = ref<string | null>(null)
const windowParams = () => ({ start_date: startDate.value, end_date: endDate.value, start_time: startTime.value, end_time: endTime.value })

const loadStats = async () => { loading.value = true; try { await authStore.refreshUser(); stats.value = await usageAPI.getDashboardStats() } catch (error) { console.error('Failed to load dashboard stats:', error) } finally { loading.value = false } }
const loadCharts = async () => { loadingCharts.value = true; try { const res = await Promise.all([usageAPI.getDashboardTrend({ ...windowParams(), granularity: granularity.value as any }), usageAPI.getDashboardModels(windowParams())]); trendData.value = res[0].trend || []; modelStats.value = res[1].models || [] } catch (error) { console.error('Failed to load charts:', error) } finally { loadingCharts.value = false } }
const loadRecent = async () => { loadingUsage.value = true; try { const res = await usageAPI.getByDateRange(startDate.value, endDate.value, undefined, { startTime: startTime.value, endTime: endTime.value }); recentUsage.value = res.items.slice(0, 5) } catch (error) { console.error('Failed to load recent usage:', error) } finally { loadingUsage.value = false } }
const loadPlatformQuotas = async () => { try { const data = await getMyPlatformQuotas(); platformQuotas.value = data.platform_quotas ?? [] } catch (error) { console.warn('Failed to load platform quotas:', error); platformQuotas.value = [] } }
const onDateRangeChange = (range: { startDate: string; endDate: string; startTime?: string; endTime?: string; preset: string | null }) => {
  startDate.value = range.startDate; endDate.value = range.endDate; startTime.value = range.startTime; endTime.value = range.endTime; activePreset.value = range.preset
  loadCharts(); loadRecent()
}
// A rolling window moves with the clock, so refresh re-anchors it to now.
const refreshAll = () => {
  if (activePreset.value === 'last24Hours') { const r = getLast24HoursRange(); startDate.value = r.start; endDate.value = r.end; startTime.value = r.startTime; endTime.value = r.endTime }
  loadStats(); loadCharts(); loadRecent(); loadPlatformQuotas()
}

onMounted(() => { refreshAll() })
</script>
