<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.runs.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.runs.description') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button type="button" data-test="process-due-runs" class="btn btn-secondary inline-flex items-center gap-2" :disabled="processingDue" @click="processDue">
            <Icon name="play" size="sm" />
            {{ t('benchmark.admin.runs.processDue') }}
          </button>
          <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="reload">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
            {{ t('benchmark.admin.runs.refresh') }}
          </button>
        </div>
      </div>

      <section class="card">
        <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.runs.standardTitle') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.runs.standardDescription') }}</p>
        </div>
        <form data-test="standard-run-form" class="space-y-4 p-6" @submit.prevent="createStandardRun">
          <div class="flex flex-wrap items-center gap-3">
            <button type="submit" data-test="create-standard-run-button" class="btn btn-primary inline-flex items-center gap-2" :disabled="creatingStandardRun" @click.prevent="createStandardRun">
              <Icon name="play" size="sm" />
              {{ t('benchmark.admin.runs.createRun') }}
            </button>
            <button type="button" data-test="standard-run-advanced-toggle" class="btn btn-secondary" @click="showAdvanced = !showAdvanced">
              {{ t(showAdvanced ? 'benchmark.admin.runs.hideAdvancedOptions' : 'benchmark.admin.runs.advancedOptions') }}
            </button>
          </div>

          <div v-if="showAdvanced" class="space-y-4">
            <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
              <label class="block">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.runs.fields.taskCount') }}</span>
                <input v-model.number="taskCount" data-test="run-task-count" type="number" min="0" class="input mt-1" />
              </label>
              <label class="mt-7 inline-flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-300">
                <input v-model="processImmediately" data-test="run-process-immediately" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
                {{ t('benchmark.admin.runs.fields.processImmediately') }}
              </label>
            </div>

            <div>
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.runs.fields.targetIds') }}</p>
              <div class="mt-2 max-h-32 space-y-2 overflow-y-auto rounded-lg border border-gray-100 p-3 dark:border-dark-700">
                <label v-for="target in targets" :key="target.id" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
                  <input
                    v-model="targetIds"
                    :data-test="`run-target-${target.id}`"
                    type="checkbox"
                    :value="target.id"
                    class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                  />
                  <span>{{ targetLabel(target) }}</span>
                </label>
                <p v-if="targets.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.runs.noTargets') }}</p>
              </div>
            </div>
          </div>

          <p v-if="formError" data-test="run-form-error" class="text-sm text-red-600 dark:text-red-400">{{ formError }}</p>
        </form>
      </section>

      <section class="card">
        <DataTable :columns="columns" :data="runs" :loading="loading">
          <template #cell-id="{ row }">
            <router-link :to="`/admin/benchmark/runs/${row.id}`" class="font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400">
              {{ benchmarkRunFallback(row.id, t) }}
            </router-link>
          </template>
          <template #cell-status="{ row }">
            <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="statusClass(row.status)">
              {{ benchmarkRunStatusLabel(row.status, t) }}
            </span>
          </template>
          <template #cell-task_count="{ row }">{{ formatInteger(row.task_count) }}</template>
          <template #cell-planned="{ row }">
            {{ t('benchmark.admin.runs.planSummary', { targets: formatInteger(row.planned_target_count), tasks: formatInteger(row.planned_task_count), results: formatInteger(row.planned_result_count) }) }}
          </template>
          <template #cell-finished_at="{ row }">{{ formatDate(row.finished_at || row.updated_at || row.created_at) }}</template>
          <template #cell-actions="{ row }">
            <div class="flex flex-wrap items-center gap-2">
              <button
                v-if="canProcess(row.status)"
                type="button"
                class="btn btn-secondary btn-sm"
                :data-test="`run-process-${row.id}`"
                :disabled="processingRunId === row.id"
                @click="processOneRun(row)"
              >
                {{ t('benchmark.admin.runs.process') }}
              </button>
              <button
                v-if="canCancel(row.status)"
                type="button"
                class="btn btn-danger btn-sm"
                :data-test="`run-cancel-${row.id}`"
                :disabled="cancelingRunId === row.id"
                @click="cancelOneRun(row)"
              >
                {{ t('benchmark.admin.runs.cancel') }}
              </button>
            </div>
          </template>
          <template #empty>
            <EmptyState :title="t('benchmark.admin.runs.emptyTitle')" :description="t('benchmark.admin.runs.emptyDescription')" />
          </template>
        </DataTable>
      </section>

      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :page-size="pagination.page_size"
        :total="pagination.total"
        @update:page="onPageChange"
        @update:pageSize="onPageSizeChange"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { Column } from '@/components/common/types'
import type {
  BenchmarkRun,
  BenchmarkRunStatus,
  BenchmarkTarget,
  CreateBenchmarkStandardRunRequest,
} from '@/types/benchmark'
import {
  benchmarkRunFallback,
  benchmarkRunStatusLabel,
  benchmarkTargetFallback,
} from '@/components/radar/benchmarkI18n'

const appStore = useAppStore()
const { locale, t } = useI18n()

const runs = ref<BenchmarkRun[]>([])
const targets = ref<BenchmarkTarget[]>([])
const loading = ref(false)
const creatingStandardRun = ref(false)
const processingDue = ref(false)
const processingRunId = ref<number | null>(null)
const cancelingRunId = ref<number | null>(null)
const targetIds = ref<number[]>([])
const taskCount = ref<number | null>(null)
const processImmediately = ref(true)
const showAdvanced = ref(false)
const formError = ref('')
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

const columns = computed<Column[]>(() => [
  { key: 'id', label: t('benchmark.admin.runs.columns.run') },
  { key: 'status', label: t('benchmark.admin.runs.columns.status') },
  { key: 'task_count', label: t('benchmark.admin.runs.columns.taskCount') },
  { key: 'planned', label: t('benchmark.admin.runs.columns.plan') },
  { key: 'finished_at', label: t('benchmark.admin.runs.columns.finished') },
  { key: 'actions', label: t('benchmark.admin.runs.columns.actions'), sortable: false },
])

async function reload() {
  loading.value = true
  try {
    const [runRes, targetRes] = await Promise.all([
      adminAPI.benchmark.listRuns({ page: pagination.page, page_size: pagination.page_size }),
      adminAPI.benchmark.listTargets({ page: 1, page_size: 100 }),
    ])
    runs.value = runRes.items || []
    targets.value = targetRes.items || []
    pagination.total = runRes.total || 0
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.runs.loadError'))
  } finally {
    loading.value = false
  }
}

function normalizedTaskCount(): number | null {
  const rawTaskCount = taskCount.value as number | string | null | undefined
  if (rawTaskCount === null || rawTaskCount === undefined || rawTaskCount === '') {
    return null
  }

  const normalized = Number(rawTaskCount)
  return Number.isFinite(normalized) ? normalized : null
}

async function createStandardRun() {
  if (creatingStandardRun.value) return

  formError.value = ''
  const normalized = normalizedTaskCount()
  if (showAdvanced.value && normalized !== null && normalized < 0) {
    formError.value = t('benchmark.admin.runs.validation.taskCountLimit')
    return
  }

  let payload: CreateBenchmarkStandardRunRequest | undefined
  if (showAdvanced.value) {
    payload = {
      process_immediately: processImmediately.value,
    }
    if (targetIds.value.length > 0) {
      payload.target_ids = [...targetIds.value]
    }
    if (normalized !== null && normalized > 0) {
      payload.task_count = normalized
    }
  }

  creatingStandardRun.value = true
  try {
    const created = await adminAPI.benchmark.createStandardRun(payload)
    runs.value = [created, ...runs.value.filter((run) => run.id !== created.id)]
    pagination.total += 1
    appStore.showSuccess(t('benchmark.admin.runs.createSuccess', { id: created.id }))
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.runs.createError'))
  } finally {
    creatingStandardRun.value = false
  }
}

async function cancelOneRun(run: BenchmarkRun) {
  cancelingRunId.value = run.id
  try {
    await adminAPI.benchmark.cancelRun(run.id)
    appStore.showSuccess(t('benchmark.admin.runs.cancelSuccess'))
    await reload()
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.runs.cancelError'))
  } finally {
    cancelingRunId.value = null
  }
}

async function processOneRun(run: BenchmarkRun) {
  processingRunId.value = run.id
  try {
    const result = await adminAPI.benchmark.processRun(run.id)
    appStore.showSuccess(t('benchmark.admin.runs.processSuccess', { count: result.processed }))
    await reload()
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.runs.processError'))
  } finally {
    processingRunId.value = null
  }
}

async function processDue() {
  processingDue.value = true
  try {
    const result = await adminAPI.benchmark.processDueRuns()
    appStore.showSuccess(t('benchmark.admin.runs.processDueSuccess', { count: result.processed }))
    await reload()
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.runs.processDueError'))
  } finally {
    processingDue.value = false
  }
}

function targetLabel(target: BenchmarkTarget): string {
  return target.display_name || target.model_name || benchmarkTargetFallback(target.id, t)
}

function statusClass(status: BenchmarkRunStatus): string {
  if (status === 'completed') return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
  if (status === 'failed' || status === 'canceled') return 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300'
  if (status === 'running') return 'bg-sky-50 text-sky-700 dark:bg-sky-900/20 dark:text-sky-300'
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
}

function canCancel(status: BenchmarkRunStatus): boolean {
  return ['queued', 'running'].includes(status)
}

function canProcess(status: BenchmarkRunStatus): boolean {
  return ['queued', 'running'].includes(status)
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

function formatInteger(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return new Intl.NumberFormat(locale.value, {
    maximumFractionDigits: 0,
  }).format(Number(value))
}

function onPageChange(page: number) {
  pagination.page = page
  reload()
}

function onPageSizeChange(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  reload()
}

onMounted(reload)
</script>
