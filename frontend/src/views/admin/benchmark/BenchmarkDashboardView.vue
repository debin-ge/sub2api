<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.dashboard.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.dashboard.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          {{ t('benchmark.admin.dashboard.refresh') }}
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
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.dashboard.topTitle') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.dashboard.topDescription') }}</p>
        </div>
        <DataTable :columns="columns" :data="topScores" :loading="loading">
          <template #cell-rank="{ row }">#{{ row.rank }}</template>
          <template #cell-target="{ row }">
            <div class="flex items-center gap-2">
              <span class="font-medium text-gray-900 dark:text-white">{{ scoreTargetName(row) }}</span>
              <span v-if="row.insufficient_sample" class="inline-flex rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('benchmark.admin.runDetail.insufficientSample') }}</span>
            </div>
          </template>
          <template #cell-overall_score="{ row }">{{ formatNumber(row.overall_score) }}</template>
          <template #cell-success_rate="{ row }">{{ formatPercent(row.success_rate) }}</template>
          <template #cell-latency_p50_ms="{ row }">{{ formatLatency(row.latency_p50_ms) }}</template>
          <template #cell-avg_total_tokens="{ row }">{{ formatInteger(row.avg_total_tokens) }}</template>
          <template #cell-estimated_cost="{ row }">{{ formatCost(row.estimated_cost) }}</template>
          <template #empty>
            <EmptyState :title="t('benchmark.admin.dashboard.emptyTitle')" :description="t('benchmark.admin.dashboard.emptyDescription')" />
          </template>
        </DataTable>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { radarAPI } from '@/api/radar'
import { useAppStore } from '@/stores/app'
import type { Column } from '@/components/common/types'
import type { BenchmarkPublicRadar, BenchmarkRun, BenchmarkScoreSnapshot } from '@/types/benchmark'
import {
  benchmarkChannelFallback,
  benchmarkRunFallback,
  benchmarkRunStatusLabel,
  benchmarkTargetFallback,
} from '@/components/radar/benchmarkI18n'

type RankedScore = BenchmarkScoreSnapshot & { rank: number }

const appStore = useAppStore()
const { locale, t } = useI18n()
const loading = ref(false)
const latestRun = ref<BenchmarkRun | null>(null)
const latestCompletedRun = ref<BenchmarkRun | null>(null)
const targetCount = ref(0)
const taskCount = ref(0)
const profileCount = ref(0)
const scores = ref<BenchmarkScoreSnapshot[]>([])
const publicRadar = ref<BenchmarkPublicRadar | null>(null)

const columns = computed<Column[]>(() => [
  { key: 'rank', label: t('benchmark.admin.dashboard.columns.rank') },
  { key: 'target', label: t('benchmark.admin.dashboard.columns.target') },
  { key: 'overall_score', label: t('benchmark.admin.dashboard.columns.abilityScore') },
  { key: 'success_rate', label: t('benchmark.admin.dashboard.columns.successRate') },
  { key: 'latency_p50_ms', label: t('benchmark.admin.dashboard.columns.p50Latency') },
  { key: 'avg_total_tokens', label: t('benchmark.admin.dashboard.columns.token') },
  { key: 'estimated_cost', label: t('benchmark.admin.dashboard.columns.cost') },
])

const cards = computed(() => [
  {
    label: t('benchmark.admin.dashboard.latestRun'),
    value: latestRun.value ? benchmarkRunStatusLabel(latestRun.value.status, t) : t('benchmark.admin.dashboard.noRun'),
    meta: latestRun.value ? benchmarkRunFallback(latestRun.value.id, t) : undefined,
  },
  {
    label: t('benchmark.admin.dashboard.publicSnapshot'),
    value: publicRadar.value?.published_at ? formatDate(publicRadar.value.published_at) : t('benchmark.admin.dashboard.waitingPublish'),
    meta: publicRadar.value?.latest_run ? benchmarkRunFallback(publicRadar.value.latest_run.id, t) : t('benchmark.admin.dashboard.noPublicSnapshot'),
  },
  { label: t('benchmark.admin.dashboard.targets'), value: formatInteger(targetCount.value) },
  { label: t('benchmark.admin.dashboard.tasks'), value: formatInteger(taskCount.value) },
  { label: t('benchmark.admin.dashboard.profiles'), value: formatInteger(profileCount.value) },
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
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.dashboard.loadError'))
  } finally {
    loading.value = false
  }
}

function scoreTargetName(score: BenchmarkScoreSnapshot): string {
  const runTarget = score.edges?.run_target
  if (!runTarget) return benchmarkTargetFallback(score.run_target_id, t)
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
    const channelLabel = runTarget.channel_name_snapshot || (runTarget.channel_id ? benchmarkChannelFallback(runTarget.channel_id, t) : null)
    return channelLabel ? `${runTarget.model_name} · ${channelLabel}` : runTarget.model_name
  }
  return benchmarkTargetFallback(id, t)
}

function formatNumber(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return new Intl.NumberFormat(locale.value, {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  }).format(Number(value))
}

function formatPercent(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return new Intl.NumberFormat(locale.value, {
    style: 'percent',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  }).format(Number(value))
}

function formatLatency(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(Number(value))} ms`
}

function formatInteger(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return new Intl.NumberFormat(locale.value, {
    maximumFractionDigits: 0,
  }).format(Number(value))
}

function formatCost(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return new Intl.NumberFormat(locale.value, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 4,
    maximumFractionDigits: 4,
  }).format(Number(value))
}

function formatDate(value?: string | null): string {
  if (!value) return '-'
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

onMounted(load)
</script>
