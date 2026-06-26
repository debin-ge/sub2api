<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.runs.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.runs.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="reload">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          {{ t('benchmark.admin.runs.refresh') }}
        </button>
      </div>

      <section class="card">
        <div class="grid grid-cols-1 gap-4 p-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.runs.fields.profile') }}</span>
            <select v-model.number="createProfileId" data-test="run-profile-select" class="input mt-1">
              <option :value="0">{{ t('benchmark.admin.runs.chooseProfile') }}</option>
              <option v-for="profile in profiles" :key="profile.id" :value="profile.id">{{ profile.name }}</option>
            </select>
          </label>
          <button type="button" data-test="create-run-button" class="btn btn-primary inline-flex items-center gap-2" :disabled="creating || !createProfileId" @click="createRun">
            <Icon name="play" size="sm" />
            {{ t('benchmark.admin.runs.createRun') }}
          </button>
        </div>
      </section>

      <section class="card">
        <DataTable :columns="columns" :data="runs" :loading="loading">
          <template #cell-id="{ row }">
            <span class="font-medium text-gray-900 dark:text-white">{{ benchmarkRunFallback(row.id, t) }}</span>
          </template>
          <template #cell-profile_id="{ row }">{{ profileName(row.profile_id) }}</template>
          <template #cell-status="{ row }">
            <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="statusClass(row.status)">
              {{ benchmarkRunStatusLabel(row.status, t) }}
            </span>
          </template>
          <template #cell-task_scale="{ row }">{{ benchmarkTaskScaleLabel(row.task_scale, t) }}</template>
          <template #cell-task_types="{ row }">{{ formatTaskTypes(row.task_types) }}</template>
          <template #cell-planned="{ row }">
            {{ t('benchmark.admin.runs.planSummary', { targets: row.planned_target_count, tasks: row.planned_task_count, results: row.planned_result_count }) }}
          </template>
          <template #cell-finished_at="{ row }">{{ formatDate(row.finished_at || row.updated_at || row.created_at) }}</template>
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
import type { BenchmarkProfile, BenchmarkRun, BenchmarkRunStatus } from '@/types/benchmark'
import {
  benchmarkProfileFallback,
  benchmarkRunFallback,
  benchmarkRunStatusLabel,
  benchmarkTaskScaleLabel,
  benchmarkTaskTypeLabel,
} from '@/components/radar/benchmarkI18n'

const appStore = useAppStore()
const { locale, t } = useI18n()

const runs = ref<BenchmarkRun[]>([])
const profiles = ref<BenchmarkProfile[]>([])
const loading = ref(false)
const creating = ref(false)
const createProfileId = ref(0)
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

const columns = computed<Column[]>(() => [
  { key: 'id', label: t('benchmark.admin.runs.columns.run') },
  { key: 'profile_id', label: t('benchmark.admin.runs.columns.profile') },
  { key: 'status', label: t('benchmark.admin.runs.columns.status') },
  { key: 'task_scale', label: t('benchmark.admin.runs.columns.scale') },
  { key: 'task_types', label: t('benchmark.admin.runs.columns.taskTypes') },
  { key: 'planned', label: t('benchmark.admin.runs.columns.plan') },
  { key: 'finished_at', label: t('benchmark.admin.runs.columns.finished') },
])

async function reload() {
  loading.value = true
  try {
    const [runRes, profileRes] = await Promise.all([
      adminAPI.benchmark.listRuns({ page: pagination.page, page_size: pagination.page_size }),
      adminAPI.benchmark.listProfiles({ page: 1, page_size: 100 }),
    ])
    runs.value = runRes.items || []
    profiles.value = profileRes.items || []
    pagination.total = runRes.total || 0
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.runs.loadError'))
  } finally {
    loading.value = false
  }
}

async function createRun() {
  if (!createProfileId.value) return
  creating.value = true
  try {
    const created = await adminAPI.benchmark.createRun({
      profile_id: createProfileId.value,
      trigger_type: 'manual',
    })
    runs.value = [created, ...runs.value.filter((run) => run.id !== created.id)]
    pagination.total += 1
    appStore.showSuccess(t('benchmark.admin.runs.createSuccess', { id: created.id }))
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.runs.createError'))
  } finally {
    creating.value = false
  }
}

function profileName(id: number): string {
  return profiles.value.find((profile) => profile.id === id)?.name || benchmarkProfileFallback(id, t)
}

function statusClass(status: BenchmarkRunStatus): string {
  if (status === 'completed') return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
  if (status === 'failed' || status === 'canceled') return 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300'
  if (status === 'running' || status === 'scoring' || status === 'snapshotting') return 'bg-sky-50 text-sky-700 dark:bg-sky-900/20 dark:text-sky-300'
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
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

function formatTaskTypes(taskTypes: string[]): string {
  if (taskTypes.length === 0) return '-'
  return taskTypes.map((taskType) => benchmarkTaskTypeLabel(taskType, t)).join(', ')
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
