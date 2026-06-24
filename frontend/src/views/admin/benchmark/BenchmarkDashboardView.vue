<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">Benchmark Dashboard</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">后台 benchmark 运行概览与最新排行榜。</p>
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          刷新
        </button>
      </div>

      <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-5">
        <div v-for="item in cards" :key="item.label" class="rounded-lg border border-gray-100 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ item.label }}</p>
          <p class="mt-2 truncate text-2xl font-semibold text-gray-900 dark:text-white">{{ item.value }}</p>
          <p v-if="item.meta" class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400">{{ item.meta }}</p>
        </div>
      </div>

      <section class="card">
        <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">Top 5 排名</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">基于最新 completed run 的 score snapshot，按能力分排序。</p>
        </div>
        <DataTable :columns="columns" :data="topScores" :loading="loading">
          <template #cell-rank="{ row }">#{{ row.rank }}</template>
          <template #cell-target="{ row }">
            <div class="flex items-center gap-2">
              <span class="font-medium text-gray-900 dark:text-white">{{ scoreTargetName(row) }}</span>
              <span v-if="row.insufficient_sample" class="inline-flex rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">样本不足</span>
            </div>
          </template>
          <template #cell-overall_score="{ row }">{{ formatNumber(row.overall_score) }}</template>
          <template #cell-success_rate="{ row }">{{ formatPercent(row.success_rate) }}</template>
          <template #cell-latency_p50_ms="{ row }">{{ formatLatency(row.latency_p50_ms) }}</template>
          <template #cell-avg_total_tokens="{ row }">{{ formatInteger(row.avg_total_tokens) }}</template>
          <template #cell-estimated_cost="{ row }">{{ formatCost(row.estimated_cost) }}</template>
          <template #empty>
            <EmptyState title="暂无排名" description="等待 completed run 生成 score snapshot。" />
          </template>
        </DataTable>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { radarAPI } from '@/api/radar'
import { useAppStore } from '@/stores/app'
import type { Column } from '@/components/common/types'
import type { BenchmarkPublicRadar, BenchmarkRun, BenchmarkScoreSnapshot } from '@/types/benchmark'

type RankedScore = BenchmarkScoreSnapshot & { rank: number }

const appStore = useAppStore()
const loading = ref(false)
const latestRun = ref<BenchmarkRun | null>(null)
const latestCompletedRun = ref<BenchmarkRun | null>(null)
const targetCount = ref(0)
const taskCount = ref(0)
const profileCount = ref(0)
const scores = ref<BenchmarkScoreSnapshot[]>([])
const publicRadar = ref<BenchmarkPublicRadar | null>(null)

const columns: Column[] = [
  { key: 'rank', label: 'Rank' },
  { key: 'target', label: 'Target' },
  { key: 'overall_score', label: '能力分' },
  { key: 'success_rate', label: 'Success' },
  { key: 'latency_p50_ms', label: 'P50 latency' },
  { key: 'avg_total_tokens', label: 'Token' },
  { key: 'estimated_cost', label: 'Cost' },
]

const cards = computed(() => [
  { label: 'Latest run', value: latestRun.value?.status || '暂无 run', meta: latestRun.value ? `Run #${latestRun.value.id}` : undefined },
  {
    label: 'Public snapshot',
    value: publicRadar.value?.published_at ? formatDate(publicRadar.value.published_at) : '等待发布',
    meta: publicRadar.value?.latest_run ? `Run #${publicRadar.value.latest_run.id}` : '暂无公开快照',
  },
  { label: 'Targets', value: String(targetCount.value) },
  { label: 'Tasks', value: String(taskCount.value) },
  { label: 'Profiles', value: String(profileCount.value) },
])

const topScores = computed<RankedScore[]>(() =>
  [...scores.value]
    .sort((a, b) => b.overall_score - a.overall_score)
    .slice(0, 5)
    .map((score, index) => ({ ...score, rank: index + 1 }))
)

async function load() {
  loading.value = true
  try {
    const [runRes, completedRunRes, targetRes, taskRes, profileRes] = await Promise.all([
      adminAPI.benchmark.listRuns({ page: 1, page_size: 5 }),
      adminAPI.benchmark.listRuns({ status: 'completed', page: 1, page_size: 1 }),
      adminAPI.benchmark.listTargets({ page: 1, page_size: 100 }),
      adminAPI.benchmark.listTasks({ page: 1, page_size: 1 }),
      adminAPI.benchmark.listProfiles({ page: 1, page_size: 1 }),
    ])

    latestRun.value = runRes.items?.[0] || null
    latestCompletedRun.value = completedRunRes.items?.[0] || null
    targetCount.value = targetRes.total || 0
    taskCount.value = taskRes.total || 0
    profileCount.value = profileRes.total || 0
    scores.value = latestCompletedRun.value
      ? await adminAPI.benchmark.getRunScores(latestCompletedRun.value.id)
      : []
    try {
      publicRadar.value = await radarAPI.getCurrent()
    } catch {
      publicRadar.value = null
    }
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : '加载 Benchmark Dashboard 失败')
  } finally {
    loading.value = false
  }
}

function scoreTargetName(score: BenchmarkScoreSnapshot): string {
  const runTarget = score.edges?.run_target
  if (!runTarget) return `Target #${score.run_target_id}`
  return runTargetLabel(runTarget, score.run_target_id)
}

function runTargetLabel(
  runTarget: {
    display_name_snapshot?: string | null
    model_name?: string
    channel_name_snapshot?: string | null
    channel_id?: number
  },
  id: number
): string {
  if (runTarget.display_name_snapshot) return runTarget.display_name_snapshot
  if (runTarget.model_name) {
    const channelLabel = runTarget.channel_name_snapshot || (runTarget.channel_id ? `Channel #${runTarget.channel_id}` : null)
    return channelLabel ? `${runTarget.model_name} · ${channelLabel}` : runTarget.model_name
  }
  return `Target #${id}`
}

function formatNumber(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return Number(value).toFixed(1).replace(/\.0$/, '')
}

function formatPercent(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return `${Math.round(Number(value) * 100)}%`
}

function formatLatency(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return `${Math.round(Number(value))} ms`
}

function formatInteger(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(Number(value))
}

function formatCost(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return `$${Number(value).toFixed(4)}`
}

function formatDate(value?: string | null): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

onMounted(load)
</script>
