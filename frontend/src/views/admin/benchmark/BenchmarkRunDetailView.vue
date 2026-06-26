<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.runDetail.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ run ? t('benchmark.admin.runDetail.runStatus', { id: run.id, status: benchmarkRunStatusLabel(run.status, t) }) : t('benchmark.admin.runDetail.defaultDescription') }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <label v-if="!resolvedRunId" class="flex items-center gap-2">
            <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.runDetail.runId') }}</span>
            <input v-model.number="manualRunId" type="number" min="1" class="input w-32" />
          </label>
          <button v-if="!resolvedRunId" type="button" class="btn btn-secondary" :disabled="!manualRunId" @click="loadManualRun">{{ t('benchmark.admin.runDetail.loadManual') }}</button>
          <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading || !activeRunId" @click="load">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
            {{ t('benchmark.admin.runDetail.refresh') }}
          </button>
          <button
            v-if="run?.status === 'completed'"
            type="button"
            data-test="publish-run-button"
            class="btn btn-primary inline-flex items-center gap-2"
            :disabled="publishing"
            @click="publish"
          >
            <Icon name="globe" size="sm" />
            {{ t('benchmark.admin.runDetail.publish') }}
          </button>
        </div>
      </div>

      <p v-if="publishMessage" class="rounded-lg bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300">
        {{ publishMessage }}
      </p>

      <div v-if="loading" class="flex items-center justify-center py-16">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
      </div>

      <EmptyState v-else-if="!activeRunId" :title="t('benchmark.admin.runDetail.emptySelectTitle')" :description="t('benchmark.admin.runDetail.emptySelectDescription')" />

      <template v-else>
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
          <div v-for="item in overviewItems" :key="item.label" class="rounded-lg border border-gray-100 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800">
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ item.label }}</p>
            <p class="mt-2 truncate text-2xl font-semibold text-gray-900 dark:text-white">{{ item.value }}</p>
            <p v-if="item.meta" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ item.meta }}</p>
          </div>
        </div>

        <section class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.runDetail.rankingTitle') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.runDetail.rankingDescription') }}</p>
          </div>
          <DataTable :columns="scoreColumns" :data="rankedScores" :loading="false">
            <template #cell-rank="{ row }">#{{ row.rank }}</template>
            <template #cell-target="{ row }">
              <div class="flex items-center gap-2">
                <span class="font-medium text-gray-900 dark:text-white">{{ scoreTargetName(row) }}</span>
                <span v-if="row.insufficient_sample" class="inline-flex rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('benchmark.admin.runDetail.insufficientSample') }}</span>
              </div>
            </template>
            <template #cell-overall_score="{ row }">{{ formatNumber(row.overall_score) }}</template>
            <template #cell-coverage_rate="{ row }">{{ formatPercent(row.coverage_rate) }}</template>
            <template #cell-success_rate="{ row }">{{ formatPercent(row.success_rate) }}</template>
            <template #cell-latency="{ row }">{{ formatLatency(row.latency_p50_ms) }} / {{ formatLatency(row.latency_p95_ms) }}</template>
            <template #cell-avg_total_tokens="{ row }">{{ formatInteger(row.avg_total_tokens) }}</template>
            <template #cell-estimated_cost="{ row }">{{ formatCost(row.estimated_cost) }}</template>
            <template #empty>
              <EmptyState :title="t('benchmark.admin.runDetail.emptyScoreTitle')" :description="t('benchmark.admin.runDetail.emptyScoreDescription')" />
            </template>
          </DataTable>
        </section>

        <div class="grid grid-cols-1 gap-6 xl:grid-cols-2">
          <section class="card">
            <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.runDetail.resultStatusTitle') }}</h2>
            </div>
            <div class="space-y-3 p-6">
              <div v-for="item in resultStatusBreakdown" :key="item.key" class="flex items-center justify-between rounded-lg bg-gray-50 px-4 py-3 dark:bg-dark-700/50">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ item.key }}</span>
                <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ item.count }}</span>
              </div>
              <p v-if="resultStatusBreakdown.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.runDetail.noResults') }}</p>
            </div>
          </section>

          <section class="card">
            <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.runDetail.invalidBreakdownTitle') }}</h2>
            </div>
            <div class="space-y-3 p-6">
              <div v-for="item in invalidReasonBreakdown" :key="item.key" class="flex items-center justify-between rounded-lg bg-gray-50 px-4 py-3 dark:bg-dark-700/50">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ item.key }}</span>
                <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ item.count }}</span>
              </div>
              <p v-if="invalidReasonBreakdown.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.runDetail.noInvalidReasons') }}</p>
            </div>
          </section>
        </div>

        <section class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.runDetail.targetResultsTitle') }}</h2>
          </div>
          <div class="space-y-6 p-6">
            <div v-for="group in resultsByTarget" :key="group.targetId" class="rounded-lg border border-gray-100 dark:border-dark-700">
              <div class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                <div class="flex items-center gap-2">
                  <h3 class="font-medium text-gray-900 dark:text-white">{{ targetName(group.targetId) }}</h3>
                  <span v-if="scoreByTargetId.get(group.targetId)?.insufficient_sample" class="inline-flex rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('benchmark.admin.runDetail.insufficientSample') }}</span>
                </div>
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.runDetail.targetResultsCount', { count: formatInteger(group.items.length) }) }}</span>
              </div>
              <DataTable :columns="resultColumns" :data="group.items" :loading="false">
                <template #cell-id="{ row }">{{ benchmarkResultFallback(row.id, t) }}</template>
                <template #cell-status="{ row }">{{ localizeResultStatus(row.status) }}</template>
                <template #cell-score="{ row }">{{ row.normalized_score ?? row.score ?? '-' }}</template>
                <template #cell-latency_ms="{ row }">{{ formatLatency(row.latency_ms) }}</template>
                <template #cell-total_tokens="{ row }">{{ formatInteger(row.total_tokens) }}</template>
                <template #cell-error="{ row }">{{ row.error_message || row.error_code || '-' }}</template>
              </DataTable>
            </div>
            <p v-if="resultsByTarget.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.runDetail.noTargetResults') }}</p>
          </div>
        </section>
      </template>
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
import { useAppStore } from '@/stores/app'
import type { Column } from '@/components/common/types'
import type { BenchmarkResult, BenchmarkResultStatus, BenchmarkRun, BenchmarkScoreSnapshot } from '@/types/benchmark'
import {
  benchmarkChannelFallback,
  benchmarkResultFallback,
  benchmarkResultStatusLabel,
  benchmarkRunStatusLabel,
  benchmarkTargetFallback,
  benchmarkTaskTypeLabel,
} from '@/components/radar/benchmarkI18n'

const props = defineProps<{
  runId?: number | string
}>()

type RankedScore = BenchmarkScoreSnapshot & { rank: number }

const appStore = useAppStore()
const { locale, t } = useI18n()
const run = ref<BenchmarkRun | null>(null)
const results = ref<BenchmarkResult[]>([])
const scores = ref<BenchmarkScoreSnapshot[]>([])
const loading = ref(false)
const publishing = ref(false)
const publishMessage = ref('')
const manualRunId = ref<number | null>(null)
const manualLoadedRunId = ref<number | null>(null)

const resolvedRunId = computed(() => {
  if (props.runId === undefined || props.runId === null || props.runId === '') return null
  const value = Number(props.runId)
  return Number.isFinite(value) && value > 0 ? value : null
})

const activeRunId = computed(() => resolvedRunId.value || manualLoadedRunId.value)

const scoreColumns = computed<Column[]>(() => [
  { key: 'rank', label: t('benchmark.admin.runDetail.columns.rank') },
  { key: 'target', label: t('benchmark.admin.runDetail.columns.target') },
  { key: 'overall_score', label: t('benchmark.admin.runDetail.columns.abilityScore') },
  { key: 'coverage_rate', label: t('benchmark.admin.runDetail.columns.coverage') },
  { key: 'success_rate', label: t('benchmark.admin.runDetail.columns.successRate') },
  { key: 'latency', label: t('benchmark.admin.runDetail.columns.p50p95') },
  { key: 'avg_total_tokens', label: t('benchmark.admin.runDetail.columns.token') },
  { key: 'estimated_cost', label: t('benchmark.admin.runDetail.columns.cost') },
])

const resultColumns = computed<Column[]>(() => [
  { key: 'id', label: t('benchmark.admin.runDetail.columns.result') },
  { key: 'run_task_id', label: t('benchmark.admin.runDetail.columns.task') },
  { key: 'status', label: t('benchmark.admin.runDetail.columns.status') },
  { key: 'score', label: t('benchmark.admin.runDetail.columns.score') },
  { key: 'latency_ms', label: t('benchmark.admin.runDetail.columns.latency') },
  { key: 'total_tokens', label: t('benchmark.admin.runDetail.columns.tokens') },
  { key: 'error', label: t('benchmark.admin.runDetail.columns.error') },
])

const rankedScores = computed<RankedScore[]>(() =>
  [...scores.value]
    .sort((a, b) => b.overall_score - a.overall_score)
    .map((score, index) => ({ ...score, rank: index + 1 }))
)

const scoreByTargetId = computed(() => {
  const map = new Map<number, BenchmarkScoreSnapshot>()
  scores.value.forEach((score) => map.set(score.run_target_id, score))
  return map
})

const overviewItems = computed(() => [
  { label: t('benchmark.admin.runDetail.overview.status'), value: run.value ? benchmarkRunStatusLabel(run.value.status, t) : '-' },
  { label: t('benchmark.admin.runDetail.overview.plannedTargets'), value: formatInteger(run.value?.planned_target_count ?? 0) },
  { label: t('benchmark.admin.runDetail.overview.plannedTasks'), value: formatInteger(run.value?.planned_task_count ?? 0) },
  {
    label: t('benchmark.admin.runDetail.overview.plannedResults'),
    value: formatInteger(run.value?.planned_result_count ?? 0),
    meta: run.value?.task_types.map((taskType) => benchmarkTaskTypeLabel(taskType, t)).join(', ') || undefined,
  },
])

const resultStatusBreakdown = computed(() => countBy(results.value.map((result) => benchmarkResultStatusLabel(result.status, t))))

const invalidReasonBreakdown = computed(() => {
  const counts = new Map<string, number>()
  for (const score of scores.value) {
    for (const [reason, count] of Object.entries(score.invalid_reason_breakdown || {})) {
      const localizedReason = localizeResultStatus(reason)
      counts.set(localizedReason, (counts.get(localizedReason) || 0) + Number(count || 0))
    }
  }
  return Array.from(counts, ([key, count]) => ({ key, count })).sort((a, b) => b.count - a.count)
})

const resultsByTarget = computed(() => {
  const groups = new Map<number, BenchmarkResult[]>()
  for (const result of results.value) {
    const items = groups.get(result.run_target_id) || []
    items.push(result)
    groups.set(result.run_target_id, items)
  }
  return Array.from(groups, ([targetId, items]) => ({ targetId, items }))
})

async function load() {
  if (!activeRunId.value) return
  loading.value = true
  publishMessage.value = ''
  try {
    const [runRes, resultRes, scoreRes] = await Promise.all([
      adminAPI.benchmark.getRun(activeRunId.value),
      adminAPI.benchmark.listRunResults(activeRunId.value),
      adminAPI.benchmark.getRunScores(activeRunId.value),
    ])
    run.value = runRes
    results.value = resultRes || []
    scores.value = scoreRes || []
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.runDetail.loadError'))
  } finally {
    loading.value = false
  }
}

function loadManualRun() {
  if (!manualRunId.value) return
  manualLoadedRunId.value = manualRunId.value
  load()
}

async function publish() {
  if (!activeRunId.value) return
  publishing.value = true
  try {
    await adminAPI.benchmark.publishRun(activeRunId.value)
    publishMessage.value = t('benchmark.admin.runDetail.publishSuccess')
    appStore.showSuccess(publishMessage.value)
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.runDetail.publishError'))
  } finally {
    publishing.value = false
  }
}

function targetName(id: number): string {
  const scoreTarget = scoreByTargetId.value.get(id)?.edges?.run_target
  if (scoreTarget) return runTargetName(scoreTarget, id)
  const resultTarget = results.value.find((item) => item.run_target_id === id)?.edges?.run_target
  if (resultTarget) return runTargetName(resultTarget, id)
  return benchmarkTargetFallback(id, t)
}

function scoreTargetName(score: BenchmarkScoreSnapshot): string {
  return score.edges?.run_target ? runTargetName(score.edges.run_target, score.run_target_id) : targetName(score.run_target_id)
}

function runTargetName(
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

function countBy(values: string[]) {
  const counts = new Map<string, number>()
  values.forEach((value) => counts.set(value, (counts.get(value) || 0) + 1))
  return Array.from(counts, ([key, count]) => ({ key, count })).sort((a, b) => b.count - a.count)
}

function localizeResultStatus(status: string): string {
  const knownStatuses = new Set<BenchmarkResultStatus>([
    'pending',
    'running',
    'scored',
    'failed',
    'timeout',
    'channel_error',
    'parse_error',
    'rate_limited',
    'verifier_error',
    'skipped',
  ])

  return knownStatuses.has(status as BenchmarkResultStatus)
    ? benchmarkResultStatusLabel(status as BenchmarkResultStatus, t)
    : status
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

onMounted(() => {
  if (activeRunId.value) {
    load()
  }
})
</script>
