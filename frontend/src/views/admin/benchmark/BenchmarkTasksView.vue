<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.tasks.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.tasks.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          {{ t('benchmark.admin.tasks.refresh') }}
        </button>
      </div>

      <section class="card">
        <form class="grid grid-cols-1 gap-4 p-6 xl:grid-cols-6 xl:items-end" @submit.prevent="createTask">
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.suiteId') }}</span>
            <input v-model.number="form.suite_id" type="number" min="1" class="input mt-1" required />
          </label>
          <label class="block xl:col-span-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.title') }}</span>
            <input v-model.trim="form.title" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.type') }}</span>
            <input v-model.trim="form.type" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.minScale') }}</span>
            <select v-model="form.min_scale" class="input mt-1">
              <option v-for="scale in scales" :key="scale" :value="scale">{{ benchmarkTaskScaleLabel(scale, t) }}</option>
            </select>
          </label>
          <button type="submit" class="btn btn-primary" :disabled="saving">{{ t('benchmark.admin.tasks.create') }}</button>

          <label class="block xl:col-span-3">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.prompt') }}</span>
            <textarea v-model.trim="form.prompt" rows="3" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.verifier') }}</span>
            <input v-model.trim="form.verifier_type" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.weight') }}</span>
            <input v-model.number="form.weight" type="number" min="0" step="0.1" class="input mt-1" />
          </label>
          <label class="flex items-center gap-2 pt-6 text-sm text-gray-700 dark:text-gray-200">
            <input v-model="form.public_prompt" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            {{ t('benchmark.admin.tasks.publicPrompt') }}
          </label>
        </form>
      </section>

      <section class="card">
        <DataTable :columns="columns" :data="tasks" :loading="loading">
          <template #cell-title="{ row }">
            <div>
              <p class="font-medium text-gray-900 dark:text-white">{{ row.title }}</p>
              <p class="mt-1 line-clamp-1 text-xs text-gray-500 dark:text-gray-400">{{ row.prompt }}</p>
            </div>
          </template>
          <template #cell-type="{ row }">{{ benchmarkTaskTypeLabel(row.type, t) }}</template>
          <template #cell-tags="{ row }">{{ row.tags?.join(', ') || '-' }}</template>
          <template #cell-min_scale="{ row }">{{ benchmarkTaskScaleLabel(row.min_scale, t) }}</template>
          <template #cell-public_prompt="{ row }">{{ benchmarkVisibilityLabel(row.public_prompt, t) }}</template>
          <template #cell-enabled="{ row }">
            <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="row.enabled ? enabledClass : disabledClass">
              {{ benchmarkEnabledLabel(row.enabled, t) }}
            </span>
          </template>
          <template #empty>
            <EmptyState :title="t('benchmark.admin.tasks.emptyTitle')" :description="t('benchmark.admin.tasks.emptyDescription')" />
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
import type { BenchmarkTask, BenchmarkTaskScale, CreateBenchmarkTaskRequest } from '@/types/benchmark'
import {
  benchmarkEnabledLabel,
  benchmarkTaskScaleLabel,
  benchmarkTaskTypeLabel,
  benchmarkVisibilityLabel,
} from '@/components/radar/benchmarkI18n'

const appStore = useAppStore()
const { t } = useI18n()
const scales: BenchmarkTaskScale[] = ['small', 'medium', 'full', 'custom']
const enabledClass = 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
const disabledClass = 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'

const tasks = ref<BenchmarkTask[]>([])
const loading = ref(false)
const saving = ref(false)
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

const form = reactive<CreateBenchmarkTaskRequest>({
  suite_id: 1,
  title: '',
  type: '',
  prompt: '',
  verifier_type: 'exact_match',
  weight: 1,
  min_scale: 'small',
  public_prompt: false,
  enabled: true,
})

const columns = computed<Column[]>(() => [
  { key: 'title', label: t('benchmark.admin.tasks.columns.title') },
  { key: 'suite_id', label: t('benchmark.admin.tasks.columns.suite') },
  { key: 'type', label: t('benchmark.admin.tasks.columns.type') },
  { key: 'category', label: t('benchmark.admin.tasks.columns.category') },
  { key: 'difficulty', label: t('benchmark.admin.tasks.columns.difficulty') },
  { key: 'tags', label: t('benchmark.admin.tasks.columns.tags') },
  { key: 'min_scale', label: t('benchmark.admin.tasks.columns.minScale') },
  { key: 'weight', label: t('benchmark.admin.tasks.columns.weight') },
  { key: 'public_prompt', label: t('benchmark.admin.tasks.columns.prompt') },
  { key: 'enabled', label: t('benchmark.admin.tasks.columns.status') },
])

async function load() {
  loading.value = true
  try {
    const response = await adminAPI.benchmark.listTasks({
      page: pagination.page,
      page_size: pagination.page_size,
    })
    tasks.value = response.items || []
    pagination.total = response.total || 0
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.tasks.loadError'))
  } finally {
    loading.value = false
  }
}

async function createTask() {
  saving.value = true
  try {
    const created = await adminAPI.benchmark.createTask({
      ...form,
      suite_id: Number(form.suite_id),
      weight: Number(form.weight ?? 1),
    })
    tasks.value = [created, ...tasks.value.filter((task) => task.id !== created.id)]
    pagination.total += 1
    appStore.showSuccess(t('benchmark.admin.tasks.createSuccess'))
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.tasks.createError'))
  } finally {
    saving.value = false
  }
}

function onPageChange(page: number) {
  pagination.page = page
  load()
}

function onPageSizeChange(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  load()
}

onMounted(load)
</script>
