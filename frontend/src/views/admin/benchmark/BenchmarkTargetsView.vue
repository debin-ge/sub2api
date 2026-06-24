<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">Benchmark Targets</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">管理参与 benchmark 的模型和通道快照。</p>
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          刷新
        </button>
      </div>

      <section class="card">
        <form class="grid grid-cols-1 gap-4 p-6 lg:grid-cols-6 lg:items-end" @submit.prevent="createTarget">
          <label class="block lg:col-span-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Model name</span>
            <input v-model.trim="form.model_name" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Channel ID</span>
            <input v-model.number="form.channel_id" type="number" min="1" class="input mt-1" required />
          </label>
          <label class="block lg:col-span-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Display name</span>
            <input v-model.trim="form.display_name" class="input mt-1" />
          </label>
          <button type="submit" class="btn btn-primary" :disabled="saving">创建 Target</button>
        </form>
      </section>

      <section class="card">
        <DataTable :columns="columns" :data="targets" :loading="loading">
          <template #cell-model_name="{ row }">
            <div>
              <p class="font-medium text-gray-900 dark:text-white">{{ row.display_name || row.model_name }}</p>
              <p class="mt-1 font-mono text-xs text-gray-500 dark:text-gray-400">{{ row.model_name }}</p>
            </div>
          </template>
          <template #cell-supported_task_types="{ row }">{{ row.supported_task_types?.join(', ') || '-' }}</template>
          <template #cell-budget="{ row }">
            {{ row.per_run_budget ?? '-' }} / {{ row.daily_budget ?? '-' }}
          </template>
          <template #cell-enabled="{ row }">
            <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="row.enabled ? enabledClass : disabledClass">
              {{ row.enabled ? 'enabled' : 'disabled' }}
            </span>
          </template>
          <template #cell-public_visible="{ row }">
            <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="row.public_visible ? enabledClass : disabledClass">
              {{ row.public_visible ? 'public' : 'private' }}
            </span>
          </template>
          <template #empty>
            <EmptyState title="暂无 Target" description="添加模型 target 后会显示在这里。" />
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
import type { BenchmarkTarget, CreateBenchmarkTargetRequest } from '@/types/benchmark'

const appStore = useAppStore()
const enabledClass = 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
const disabledClass = 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'

const targets = ref<BenchmarkTarget[]>([])
const loading = ref(false)
const saving = ref(false)
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

const form = reactive<CreateBenchmarkTargetRequest>({
  model_name: '',
  channel_id: 0,
  display_name: '',
  supported_task_types: [],
  max_concurrency: 1,
  enabled: true,
  public_visible: true,
  sort_order: 0,
})

const columns: Column[] = [
  { key: 'model_name', label: 'Model' },
  { key: 'channel_id', label: 'Channel' },
  { key: 'provider_snapshot', label: 'Provider' },
  { key: 'supported_task_types', label: 'Task types' },
  { key: 'max_concurrency', label: 'Concurrency' },
  { key: 'budget', label: 'Budget' },
  { key: 'enabled', label: 'Status' },
  { key: 'public_visible', label: 'Visibility' },
]

async function load() {
  loading.value = true
  try {
    const response = await adminAPI.benchmark.listTargets({
      page: pagination.page,
      page_size: pagination.page_size,
    })
    targets.value = response.items || []
    pagination.total = response.total || 0
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : '加载 Target 失败')
  } finally {
    loading.value = false
  }
}

async function createTarget() {
  saving.value = true
  try {
    const created = await adminAPI.benchmark.createTarget({
      ...form,
      channel_id: Number(form.channel_id),
      display_name: form.display_name || undefined,
    })
    targets.value = [created, ...targets.value.filter((target) => target.id !== created.id)]
    pagination.total += 1
    appStore.showSuccess('Target 已创建')
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : '创建 Target 失败')
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
