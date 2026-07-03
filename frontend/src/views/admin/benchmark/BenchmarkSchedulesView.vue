<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.schedules.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.schedules.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="reload">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          {{ t('benchmark.admin.schedules.refresh') }}
        </button>
      </div>

      <section class="card">
        <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ editingSchedule ? t('benchmark.admin.schedules.edit') : t('benchmark.admin.schedules.create') }}</h2>
        </div>
        <form class="space-y-4 p-6" @submit.prevent="saveSchedule">
          <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <label class="block">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.schedules.fields.name') }}</span>
              <input v-model.trim="form.name" data-test="schedule-name" class="input mt-1" required />
            </label>
            <label class="block">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.schedules.fields.cronExpr') }}</span>
              <input v-model.trim="form.cron_expr" data-test="schedule-cron" class="input mt-1" required placeholder="0 2 * * *" />
            </label>
          </div>

          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.schedules.fields.taskCount') }}</span>
            <input v-model.number="form.task_count" data-test="schedule-task-count" type="number" min="1" class="input mt-1" />
          </label>

          <div>
            <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.schedules.fields.targets') }}</p>
            <div class="mt-2 max-h-40 space-y-2 overflow-y-auto rounded-lg border border-gray-100 p-3 dark:border-dark-700">
              <label v-for="target in targets" :key="target.id" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
                <input
                  v-model="form.target_ids"
                  :data-test="`schedule-target-${target.id}`"
                  type="checkbox"
                  :value="target.id"
                  class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                />
                <span>{{ targetLabel(target) }}</span>
              </label>
              <p v-if="targets.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.schedules.noTargets') }}</p>
            </div>
          </div>

          <label class="inline-flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-300">
            <input v-model="form.enabled" data-test="schedule-enabled" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            {{ t('benchmark.admin.schedules.fields.enabled') }}
          </label>

          <p v-if="formError" data-test="schedule-form-error" class="text-sm text-red-600 dark:text-red-400">{{ formError }}</p>

          <div class="flex gap-2">
            <button type="submit" data-test="save-schedule-button" class="btn btn-primary" :disabled="saving">
              {{ editingSchedule ? t('benchmark.admin.schedules.update') : t('benchmark.admin.schedules.create') }}
            </button>
            <button v-if="editingSchedule" type="button" class="btn btn-secondary" @click="cancelEdit">
              {{ t('benchmark.admin.schedules.cancel') }}
            </button>
          </div>
        </form>
      </section>

      <section class="card">
        <DataTable :columns="columns" :data="schedules" :loading="loading">
          <template #cell-name="{ row }">
            <span class="font-medium text-gray-900 dark:text-white">{{ row.name }}</span>
          </template>
          <template #cell-cron_expr="{ row }">
            <code class="rounded bg-gray-100 px-2 py-0.5 text-xs dark:bg-dark-700">{{ row.cron_expr }}</code>
          </template>
          <template #cell-target_count="{ row }">{{ formatInteger(row.target_ids?.length ?? 0) }}</template>
          <template #cell-task_count="{ row }">{{ formatInteger(row.task_count) }}</template>
          <template #cell-enabled="{ row }">
            <span :class="row.enabled ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-400'">
              {{ benchmarkEnabledLabel(row.enabled, t) }}
            </span>
          </template>
          <template #cell-next_run_at="{ row }">{{ formatDate(row.next_run_at) }}</template>
          <template #cell-actions="{ row }">
            <div class="flex flex-wrap items-center gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :data-test="`schedule-trigger-${row.id}`" :disabled="triggering === row.id" @click="triggerSchedule(row)">
                {{ t('benchmark.admin.schedules.trigger') }}
              </button>
              <button type="button" class="btn btn-secondary btn-sm" :data-test="`schedule-edit-${row.id}`" @click="startEdit(row)">
                {{ t('benchmark.admin.schedules.edit') }}
              </button>
              <button type="button" class="btn btn-danger btn-sm" :data-test="`schedule-delete-${row.id}`" :disabled="deleting === row.id" @click="deleteSchedule(row)">
                {{ t('benchmark.admin.schedules.delete') }}
              </button>
            </div>
          </template>
          <template #empty>
            <EmptyState :title="t('benchmark.admin.schedules.emptyTitle')" :description="t('benchmark.admin.schedules.emptyDescription')" />
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
  BenchmarkSchedule,
  BenchmarkTarget,
  CreateBenchmarkScheduleRequest,
  UpdateBenchmarkScheduleRequest,
} from '@/types/benchmark'
import {
  benchmarkEnabledLabel,
  benchmarkTargetFallback,
} from '@/components/radar/benchmarkI18n'

const appStore = useAppStore()
const { locale, t } = useI18n()

const schedules = ref<BenchmarkSchedule[]>([])
const targets = ref<BenchmarkTarget[]>([])
const loading = ref(false)
const saving = ref(false)
const deleting = ref<number | null>(null)
const triggering = ref<number | null>(null)
const editingSchedule = ref<BenchmarkSchedule | null>(null)
const formError = ref('')
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

const form = reactive<{
  name: string
  cron_expr: string
  enabled: boolean
  target_ids: number[]
  task_count: number
}>({
  name: '',
  cron_expr: '',
  enabled: true,
  target_ids: [],
  task_count: 10,
})

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('benchmark.admin.schedules.columns.name') },
  { key: 'cron_expr', label: t('benchmark.admin.schedules.columns.cronExpr') },
  { key: 'target_count', label: t('benchmark.admin.schedules.columns.targetCount') },
  { key: 'task_count', label: t('benchmark.admin.schedules.columns.taskCount') },
  { key: 'enabled', label: t('benchmark.admin.schedules.columns.enabled') },
  { key: 'next_run_at', label: t('benchmark.admin.schedules.columns.nextRunAt') },
  { key: 'actions', label: t('benchmark.admin.schedules.columns.actions'), sortable: false },
])

async function reload() {
  loading.value = true
  try {
    const [scheduleRes, targetRes] = await Promise.all([
      adminAPI.benchmark.listSchedules({ page: pagination.page, page_size: pagination.page_size }),
      adminAPI.benchmark.listTargets({ page: 1, page_size: 100 }),
    ])
    schedules.value = scheduleRes.items || []
    targets.value = targetRes.items || []
    pagination.total = scheduleRes.total || 0
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.schedules.loadError'))
  } finally {
    loading.value = false
  }
}

async function saveSchedule() {
  formError.value = ''
  if (!form.name.trim() || !form.cron_expr.trim()) {
    formError.value = t('benchmark.admin.schedules.validation.required')
    return
  }

  saving.value = true
  try {
    if (editingSchedule.value) {
      const payload: UpdateBenchmarkScheduleRequest = {
        name: form.name,
        cron_expr: form.cron_expr,
        enabled: form.enabled,
        target_ids: form.target_ids,
        task_count: form.task_count,
      }
      await adminAPI.benchmark.updateSchedule(editingSchedule.value.id, payload)
      appStore.showSuccess(t('benchmark.admin.schedules.updateSuccess'))
    } else {
      const payload: CreateBenchmarkScheduleRequest = {
        name: form.name,
        cron_expr: form.cron_expr,
        enabled: form.enabled,
        target_ids: form.target_ids.length > 0 ? form.target_ids : undefined,
        task_count: form.task_count > 0 ? form.task_count : undefined,
      }
      await adminAPI.benchmark.createSchedule(payload)
      appStore.showSuccess(t('benchmark.admin.schedules.createSuccess'))
    }
    resetForm()
    await reload()
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.schedules.saveError'))
  } finally {
    saving.value = false
  }
}

async function deleteSchedule(schedule: BenchmarkSchedule) {
  if (!confirm(t('benchmark.admin.schedules.deleteConfirm'))) return
  deleting.value = schedule.id
  try {
    await adminAPI.benchmark.deleteSchedule(schedule.id)
    appStore.showSuccess(t('benchmark.admin.schedules.deleteSuccess'))
    await reload()
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.schedules.deleteError'))
  } finally {
    deleting.value = null
  }
}

async function triggerSchedule(schedule: BenchmarkSchedule) {
  triggering.value = schedule.id
  try {
    const run = await adminAPI.benchmark.triggerSchedule(schedule.id)
    appStore.showSuccess(t('benchmark.admin.schedules.triggerSuccess', { id: run.id }))
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.schedules.triggerError'))
  } finally {
    triggering.value = null
  }
}

function startEdit(schedule: BenchmarkSchedule) {
  editingSchedule.value = schedule
  form.name = schedule.name
  form.cron_expr = schedule.cron_expr
  form.enabled = schedule.enabled
  form.target_ids = [...(schedule.target_ids || [])]
  form.task_count = schedule.task_count || 10
}

function cancelEdit() {
  resetForm()
}

function resetForm() {
  editingSchedule.value = null
  form.name = ''
  form.cron_expr = ''
  form.enabled = true
  form.target_ids = []
  form.task_count = 10
  formError.value = ''
}

function targetLabel(target: BenchmarkTarget): string {
  return target.display_name || target.model_name || benchmarkTargetFallback(target.id, t)
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
