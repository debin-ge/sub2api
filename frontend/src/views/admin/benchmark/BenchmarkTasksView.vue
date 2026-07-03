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
        <form class="grid grid-cols-1 gap-4 p-6 xl:grid-cols-6 xl:items-end" @submit.prevent="submitTask">
          <div v-if="formError" class="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/40 dark:bg-red-900/20 dark:text-red-300 xl:col-span-6">
            {{ formError }}
          </div>
          <label class="block xl:col-span-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.title') }}</span>
            <input v-model.trim="form.title" data-test="task-title-input" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.type') }}</span>
            <select v-model="form.type_option" data-test="task-type-select" class="input mt-1" required>
              <option v-for="taskType in taskTypes" :key="taskType" :value="taskType">{{ benchmarkTaskTypeLabel(taskType, t) }}</option>
              <option value="__custom__">{{ t('benchmark.admin.tasks.customType') }}</option>
            </select>
          </label>
          <label v-if="form.type_option === customTypeValue" class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.customType') }}</span>
            <input v-model.trim="form.custom_type" data-test="task-custom-type-input" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.difficulty') }}</span>
            <input v-model.trim="form.difficulty" data-test="task-difficulty-input" class="input mt-1" />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.sortOrder') }}</span>
            <input v-model.number="form.sort_order" data-test="task-sort-order-input" type="number" class="input mt-1" />
          </label>

          <label class="block xl:col-span-3">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.prompt') }}</span>
            <textarea v-model.trim="form.prompt" data-test="task-prompt-input" rows="4" class="input mt-1" required />
          </label>
          <label class="block xl:col-span-3">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.inputPayload') }}</span>
            <textarea v-model="inputPayloadText" data-test="task-input-payload-input" rows="4" class="input mt-1 font-mono text-sm" />
          </label>
          <label class="block xl:col-span-3">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.expectedOutput') }}</span>
            <textarea v-model="expectedOutputText" data-test="task-expected-output-input" rows="4" class="input mt-1 font-mono text-sm" />
          </label>
          <label class="block xl:col-span-3">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.verifierConfig') }}</span>
            <textarea v-model="verifierConfigText" data-test="task-verifier-config-input" rows="4" class="input mt-1 font-mono text-sm" />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.verifier') }}</span>
            <input v-model.trim="form.verifier_type" data-test="task-verifier-type-input" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.tasks.fields.weight') }}</span>
            <input v-model.number="form.weight" data-test="task-weight-input" type="number" min="0" step="0.1" class="input mt-1" />
          </label>
          <label class="flex items-center gap-2 pt-6 text-sm text-gray-700 dark:text-gray-200">
            <input v-model="form.public_prompt" data-test="task-public-prompt-input" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            {{ t('benchmark.admin.tasks.fields.publicPrompt') }}
          </label>
          <label class="flex items-center gap-2 pt-6 text-sm text-gray-700 dark:text-gray-200">
            <input v-model="form.enabled" data-test="task-enabled-input" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            {{ t('benchmark.admin.tasks.fields.enabled') }}
          </label>
          <div class="flex flex-wrap gap-2 xl:col-span-6 xl:justify-end">
            <button type="submit" data-test="task-submit-button" class="btn btn-primary" :disabled="saving">
              {{ editingTask ? t('benchmark.admin.tasks.update') : t('benchmark.admin.tasks.create') }}
            </button>
            <button v-if="editingTask" type="button" class="btn btn-secondary" :disabled="saving" @click="resetForm">
              {{ t('common.cancel') }}
            </button>
          </div>
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
          <template #cell-difficulty="{ row }">{{ row.difficulty || '-' }}</template>
          <template #cell-weight="{ row }">{{ formatNumber(row.weight) }}</template>
          <template #cell-sort_order="{ row }">{{ formatInteger(row.sort_order) }}</template>
          <template #cell-public_prompt="{ row }">{{ benchmarkVisibilityLabel(row.public_prompt, t) }}</template>
          <template #cell-enabled="{ row }">
            <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="row.enabled ? enabledClass : disabledClass">
              {{ benchmarkEnabledLabel(row.enabled, t) }}
            </span>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex flex-wrap items-center gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :data-test="`task-edit-${row.id}`" @click="editTask(row)">
                {{ t('benchmark.admin.tasks.edit') }}
              </button>
              <button type="button" class="btn btn-danger btn-sm" :data-test="`task-delete-${row.id}`" :disabled="deletingId === row.id" @click="deleteTask(row)">
                {{ t('common.delete') }}
              </button>
            </div>
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
import type { BenchmarkTask, CreateBenchmarkTaskRequest, UpdateBenchmarkTaskRequest } from '@/types/benchmark'
import {
  benchmarkEnabledLabel,
  benchmarkTaskTypeLabel,
  benchmarkVisibilityLabel,
} from '@/components/radar/benchmarkI18n'

const appStore = useAppStore()
const { locale, t } = useI18n()
const taskTypes = ['reasoning', 'coding', 'writing']
const customTypeValue = '__custom__'
const enabledClass = 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
const disabledClass = 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'

const tasks = ref<BenchmarkTask[]>([])
const loading = ref(false)
const saving = ref(false)
const deletingId = ref<number | null>(null)
const editingTask = ref<BenchmarkTask | null>(null)
const formError = ref('')
const inputPayloadText = ref('')
const expectedOutputText = ref('')
const verifierConfigText = ref('')
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

type TaskFormState = {
  title: string
  type_option: string
  custom_type: string
  difficulty: string
  prompt: string
  verifier_type: string
  weight: number
  public_prompt: boolean
  enabled: boolean
  sort_order: number
}

const defaultForm = (): TaskFormState => ({
  title: '',
  type_option: 'reasoning',
  custom_type: '',
  difficulty: '',
  prompt: '',
  verifier_type: 'exact_match',
  weight: 1,
  public_prompt: false,
  enabled: true,
  sort_order: 0,
})

const form = reactive<TaskFormState>(defaultForm())

const columns = computed<Column[]>(() => [
  { key: 'title', label: t('benchmark.admin.tasks.columns.title') },
  { key: 'type', label: t('benchmark.admin.tasks.columns.type') },
  { key: 'difficulty', label: t('benchmark.admin.tasks.columns.difficulty') },
  { key: 'weight', label: t('benchmark.admin.tasks.columns.weight') },
  { key: 'sort_order', label: t('benchmark.admin.tasks.columns.sortOrder') },
  { key: 'public_prompt', label: t('benchmark.admin.tasks.columns.prompt') },
  { key: 'enabled', label: t('benchmark.admin.tasks.columns.status') },
  { key: 'actions', label: t('benchmark.admin.tasks.columns.actions'), sortable: false },
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

function resetForm() {
  Object.assign(form, defaultForm())
  inputPayloadText.value = ''
  expectedOutputText.value = ''
  verifierConfigText.value = ''
  editingTask.value = null
  formError.value = ''
}

function stringifyJson(value: Record<string, unknown> | undefined): string {
  return value ? JSON.stringify(value, null, 2) : ''
}

function editTask(task: BenchmarkTask) {
  editingTask.value = task
  formError.value = ''
  const isKnownType = taskTypes.includes(task.type)
  Object.assign(form, {
    title: task.title,
    type_option: isKnownType ? task.type : customTypeValue,
    custom_type: isKnownType ? '' : task.type,
    difficulty: task.difficulty || '',
    prompt: task.prompt,
    verifier_type: task.verifier_type,
    weight: task.weight,
    public_prompt: task.public_prompt,
    enabled: task.enabled,
    sort_order: task.sort_order,
  })
  inputPayloadText.value = stringifyJson(task.input_payload)
  expectedOutputText.value = stringifyJson(task.expected_output)
  verifierConfigText.value = stringifyJson(task.verifier_config)
}

function parseJsonField(rawValue: string, fieldKey: string): Record<string, unknown> | undefined | null {
  const raw = rawValue.trim()
  if (!raw) return undefined
  try {
    const parsed = JSON.parse(raw) as unknown
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>
    }
  } catch {
    // handled below
  }
  const message = t('benchmark.admin.tasks.jsonError', { field: t(fieldKey) })
  formError.value = message
  appStore.showError(message)
  return null
}

function formatInteger(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return new Intl.NumberFormat(locale.value, {
    maximumFractionDigits: 0,
  }).format(Number(value))
}

function formatNumber(value?: number | null): string {
  if (value === undefined || value === null) return '-'
  return new Intl.NumberFormat(locale.value, {
    maximumFractionDigits: 2,
  }).format(Number(value))
}

function buildPayload(): UpdateBenchmarkTaskRequest | null {
  const inputPayload = parseJsonField(inputPayloadText.value, 'benchmark.admin.tasks.fields.inputPayload')
  if (inputPayload === null) return null
  const expectedOutput = parseJsonField(expectedOutputText.value, 'benchmark.admin.tasks.fields.expectedOutput')
  if (expectedOutput === null) return null
  const verifierConfig = parseJsonField(verifierConfigText.value, 'benchmark.admin.tasks.fields.verifierConfig')
  if (verifierConfig === null) return null

  return {
    title: form.title,
    type: form.type_option === customTypeValue ? form.custom_type : form.type_option,
    difficulty: form.difficulty || undefined,
    prompt: form.prompt,
    input_payload: inputPayload,
    expected_output: expectedOutput,
    verifier_type: form.verifier_type,
    verifier_config: verifierConfig,
    weight: Number(form.weight ?? 1),
    public_prompt: form.public_prompt,
    enabled: form.enabled,
    sort_order: Number(form.sort_order || 0),
  }
}

async function submitTask() {
  formError.value = ''
  const payload = buildPayload()
  if (!payload) return

  saving.value = true
  try {
    if (editingTask.value) {
      const updated = await adminAPI.benchmark.updateTask(editingTask.value.id, payload)
      tasks.value = tasks.value.map((task) => task.id === updated.id ? updated : task)
      appStore.showSuccess(t('benchmark.admin.tasks.updateSuccess'))
      resetForm()
    } else {
      const created = await adminAPI.benchmark.createTask(payload as CreateBenchmarkTaskRequest)
      tasks.value = [created, ...tasks.value.filter((task) => task.id !== created.id)]
      pagination.total += 1
      appStore.showSuccess(t('benchmark.admin.tasks.createSuccess'))
      resetForm()
    }
  } catch (error) {
    const fallback = editingTask.value ? t('benchmark.admin.tasks.updateError') : t('benchmark.admin.tasks.createError')
    appStore.showError(error instanceof Error ? error.message : fallback)
  } finally {
    saving.value = false
  }
}

async function deleteTask(task: BenchmarkTask) {
  if (!window.confirm(t('benchmark.admin.tasks.deleteConfirm'))) return

  deletingId.value = task.id
  try {
    await adminAPI.benchmark.deleteTask(task.id)
    appStore.showSuccess(t('benchmark.admin.tasks.deleteSuccess'))
    await load()
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.tasks.deleteError'))
  } finally {
    deletingId.value = null
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
