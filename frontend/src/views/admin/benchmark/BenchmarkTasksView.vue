<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">Benchmark Tasks</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">管理任务类型、难度、prompt 与 verifier。</p>
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          刷新
        </button>
      </div>

      <section class="card">
        <form class="grid grid-cols-1 gap-4 p-6 xl:grid-cols-6 xl:items-end" @submit.prevent="createTask">
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Suite ID</span>
            <input v-model.number="form.suite_id" type="number" min="1" class="input mt-1" required />
          </label>
          <label class="block xl:col-span-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Title</span>
            <input v-model.trim="form.title" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Type</span>
            <input v-model.trim="form.type" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Min scale</span>
            <select v-model="form.min_scale" class="input mt-1">
              <option v-for="scale in scales" :key="scale" :value="scale">{{ scale }}</option>
            </select>
          </label>
          <button type="submit" class="btn btn-primary" :disabled="saving">创建 Task</button>

          <label class="block xl:col-span-3">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Prompt</span>
            <textarea v-model.trim="form.prompt" rows="3" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Verifier</span>
            <input v-model.trim="form.verifier_type" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Weight</span>
            <input v-model.number="form.weight" type="number" min="0" step="0.1" class="input mt-1" />
          </label>
          <label class="flex items-center gap-2 pt-6 text-sm text-gray-700 dark:text-gray-200">
            <input v-model="form.public_prompt" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            Public prompt
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
          <template #cell-tags="{ row }">{{ row.tags?.join(', ') || '-' }}</template>
          <template #cell-public_prompt="{ row }">{{ row.public_prompt ? 'public' : 'private' }}</template>
          <template #cell-enabled="{ row }">
            <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="row.enabled ? enabledClass : disabledClass">
              {{ row.enabled ? 'enabled' : 'disabled' }}
            </span>
          </template>
          <template #empty>
            <EmptyState title="暂无 Task" description="创建 benchmark task 后会显示在这里。" />
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
import { onMounted, reactive, ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { Column } from '@/components/common/types'
import type { BenchmarkTask, BenchmarkTaskScale, CreateBenchmarkTaskRequest } from '@/types/benchmark'

const appStore = useAppStore()
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

const columns: Column[] = [
  { key: 'title', label: 'Title' },
  { key: 'suite_id', label: 'Suite' },
  { key: 'type', label: 'Type' },
  { key: 'category', label: 'Category' },
  { key: 'difficulty', label: 'Difficulty' },
  { key: 'tags', label: 'Tags' },
  { key: 'min_scale', label: 'Min scale' },
  { key: 'weight', label: 'Weight' },
  { key: 'public_prompt', label: 'Prompt' },
  { key: 'enabled', label: 'Status' },
]

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
    appStore.showError(error instanceof Error ? error.message : '加载 Task 失败')
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
    appStore.showSuccess('Task 已创建')
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : '创建 Task 失败')
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
