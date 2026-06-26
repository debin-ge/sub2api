<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.profiles.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.profiles.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="reload">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          {{ t('benchmark.admin.profiles.refresh') }}
        </button>
      </div>

      <div class="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,420px)_1fr]">
        <section class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.profiles.createTitle') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.profiles.createDescription') }}</p>
          </div>
          <form class="space-y-4 p-6" @submit.prevent="createProfile">
            <label class="block">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.profiles.fields.name') }}</span>
              <input v-model.trim="form.name" data-test="profile-name-input" class="input mt-1" required />
            </label>

            <label class="block">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.profiles.fields.suiteId') }}</span>
              <input v-model.number="form.suite_id" data-test="profile-suite-input" type="number" min="1" class="input mt-1" required />
            </label>

            <label class="block">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.profiles.fields.description') }}</span>
              <textarea v-model.trim="form.description" rows="2" class="input mt-1" />
            </label>

            <div>
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.profiles.fields.targets') }}</p>
              <div class="mt-2 max-h-40 space-y-2 overflow-y-auto rounded-lg border border-gray-100 p-3 dark:border-dark-700">
                <label v-for="target in targets" :key="target.id" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
                  <input
                    v-model="form.target_ids"
                    :data-test="`profile-target-${target.id}`"
                    type="checkbox"
                    :value="target.id"
                    class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                  />
                  <span>{{ targetLabel(target) }}</span>
                </label>
                <p v-if="targets.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.profiles.noTargets') }}</p>
              </div>
            </div>

            <div>
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.profiles.fields.taskTypes') }}</p>
              <div class="mt-2 flex flex-wrap gap-2">
                <label v-for="type in taskTypes" :key="type" class="inline-flex items-center gap-2 rounded-md border border-gray-200 px-3 py-2 text-sm dark:border-dark-700">
                  <input
                    v-model="form.task_types"
                    :data-test="`profile-task-type-${type}`"
                    type="checkbox"
                    :value="type"
                    class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                  />
                  <span>{{ benchmarkTaskTypeLabel(type, t) }}</span>
                </label>
              </div>
            </div>

            <div>
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.profiles.fields.taskScale') }}</p>
              <div class="mt-2 grid grid-cols-2 gap-2">
                <label v-for="scale in scales" :key="scale" class="inline-flex items-center gap-2 rounded-md border border-gray-200 px-3 py-2 text-sm dark:border-dark-700">
                  <input
                    v-model="form.task_scale"
                    :data-test="`profile-scale-${scale}`"
                    type="radio"
                    :value="scale"
                    class="border-gray-300 text-primary-600 focus:ring-primary-500"
                  />
                  <span>{{ benchmarkTaskScaleLabel(scale, t) }}</span>
                </label>
              </div>
            </div>

            <button type="button" data-test="create-profile-button" class="btn btn-primary w-full" :disabled="saving" @click="createProfile">
              {{ t('benchmark.admin.profiles.createTitle') }}
            </button>
          </form>
        </section>

        <section class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.profiles.previewTitle') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.profiles.previewDescription') }}</p>
          </div>
          <div class="space-y-4 p-6">
            <label class="block">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.profiles.fields.profile') }}</span>
              <select v-model.number="previewProfileId" data-test="preview-profile-select" class="input mt-1" @change="syncPreviewFromProfile">
                <option :value="0">{{ t('benchmark.admin.profiles.chooseProfile') }}</option>
                <option v-for="profile in profiles" :key="profile.id" :value="profile.id">{{ profile.name }}</option>
              </select>
            </label>

            <div>
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.profiles.fields.taskTypes') }}</p>
              <div class="mt-2 flex flex-wrap gap-2">
                <label v-for="type in taskTypes" :key="type" class="inline-flex items-center gap-2 rounded-md border border-gray-200 px-3 py-2 text-sm dark:border-dark-700">
                  <input
                    v-model="preview.task_types"
                    :data-test="`preview-task-type-${type}`"
                    type="checkbox"
                    :value="type"
                    class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                  />
                  <span>{{ benchmarkTaskTypeLabel(type, t) }}</span>
                </label>
              </div>
            </div>

            <div>
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.profiles.fields.taskScale') }}</p>
              <div class="mt-2 grid grid-cols-2 gap-2">
                <label v-for="scale in scales" :key="scale" class="inline-flex items-center gap-2 rounded-md border border-gray-200 px-3 py-2 text-sm dark:border-dark-700">
                  <input
                    v-model="preview.task_scale"
                    :data-test="`preview-scale-${scale}`"
                    type="radio"
                    :value="scale"
                    class="border-gray-300 text-primary-600 focus:ring-primary-500"
                  />
                  <span>{{ benchmarkTaskScaleLabel(scale, t) }}</span>
                </label>
              </div>
            </div>

            <button type="button" data-test="preview-button" class="btn btn-secondary" :disabled="previewing || !previewProfileId" @click="previewSelectedProfile">
              {{ t('benchmark.admin.profiles.preview') }}
            </button>

            <div v-if="profilePreview" class="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div class="rounded-lg bg-gray-50 p-4 dark:bg-dark-700/50">
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.profiles.previewCards.target') }}</p>
                <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ formatInteger(profilePreview.target_count) }}</p>
              </div>
              <div class="rounded-lg bg-gray-50 p-4 dark:bg-dark-700/50">
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.profiles.previewCards.task') }}</p>
                <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ formatInteger(profilePreview.task_count) }}</p>
              </div>
              <div class="rounded-lg bg-gray-50 p-4 dark:bg-dark-700/50">
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.profiles.previewCards.result') }}</p>
                <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ formatInteger(profilePreview.result_count) }}</p>
              </div>
            </div>
          </div>
        </section>
      </div>

      <section class="card">
        <DataTable :columns="columns" :data="profiles" :loading="loading">
          <template #cell-name="{ row }">
            <div>
              <p class="font-medium text-gray-900 dark:text-white">{{ row.name }}</p>
              <p v-if="row.description" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ row.description }}</p>
            </div>
          </template>
          <template #cell-target_ids="{ row }">{{ row.target_ids.length }}</template>
          <template #cell-task_types="{ row }">{{ formatTaskTypes(row.task_types) }}</template>
          <template #cell-task_scale="{ row }">{{ benchmarkTaskScaleLabel(row.task_scale, t) }}</template>
          <template #cell-enabled="{ row }">
            <span :class="row.enabled ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'" class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium">
              {{ benchmarkEnabledLabel(row.enabled, t) }}
            </span>
          </template>
          <template #empty>
            <EmptyState :title="t('benchmark.admin.profiles.emptyTitle')" :description="t('benchmark.admin.profiles.emptyDescription')" />
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
  BenchmarkProfile,
  BenchmarkProfilePreview,
  BenchmarkProfilePreviewRequest,
  BenchmarkTarget,
  BenchmarkTask,
  BenchmarkTaskScale,
  CreateBenchmarkProfileRequest,
} from '@/types/benchmark'
import { benchmarkEnabledLabel, benchmarkTargetFallback, benchmarkTaskScaleLabel, benchmarkTaskTypeLabel } from '@/components/radar/benchmarkI18n'

const appStore = useAppStore()
const { locale, t } = useI18n()
const scales: BenchmarkTaskScale[] = ['small', 'medium', 'full', 'custom']

const profiles = ref<BenchmarkProfile[]>([])
const targets = ref<BenchmarkTarget[]>([])
const tasks = ref<BenchmarkTask[]>([])
const loading = ref(false)
const saving = ref(false)
const previewing = ref(false)
const profilePreview = ref<BenchmarkProfilePreview | null>(null)
const previewProfileId = ref(0)
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

const form = reactive<CreateBenchmarkProfileRequest>({
  suite_id: 1,
  name: '',
  description: '',
  target_ids: [],
  task_types: [],
  task_scale: 'medium',
  task_count_limit: null,
  per_type_limit: {},
  difficulty_filter: [],
  tag_filter: [],
  sampling_strategy: 'random',
  selection_seed: null,
  enabled: true,
})

const preview = reactive<BenchmarkProfilePreviewRequest>({
  target_ids: [],
  task_types: [],
  task_scale: 'medium',
  task_count_limit: null,
  per_type_limit: {},
  difficulty_filter: [],
  tag_filter: [],
  selection_seed: null,
})

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('benchmark.admin.profiles.columns.name') },
  { key: 'target_ids', label: t('benchmark.admin.profiles.columns.targets') },
  { key: 'task_types', label: t('benchmark.admin.profiles.columns.taskTypes') },
  { key: 'task_scale', label: t('benchmark.admin.profiles.columns.scale') },
  { key: 'enabled', label: t('benchmark.admin.profiles.columns.status') },
])

const taskTypes = computed(() => {
  const values = new Set<string>()
  tasks.value.forEach((task) => values.add(task.type))
  profiles.value.forEach((profile) => profile.task_types.forEach((type) => values.add(type)))
  return Array.from(values).sort()
})

function targetLabel(target: BenchmarkTarget): string {
  return target.display_name || target.model_name || benchmarkTargetFallback(target.id, t)
}

function profileById(id: number): BenchmarkProfile | undefined {
  return profiles.value.find((profile) => profile.id === id)
}

async function reload() {
  loading.value = true
  try {
    const [profileRes, targetRes, taskRes] = await Promise.all([
      adminAPI.benchmark.listProfiles({ page: pagination.page, page_size: pagination.page_size }),
      adminAPI.benchmark.listTargets({ page: 1, page_size: 100 }),
      adminAPI.benchmark.listTasks({ page: 1, page_size: 100 }),
    ])
    profiles.value = profileRes.items || []
    targets.value = targetRes.items || []
    tasks.value = taskRes.items || []
    pagination.total = profileRes.total || 0
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.profiles.loadError'))
  } finally {
    loading.value = false
  }
}

function syncPreviewFromProfile() {
  const selected = profileById(previewProfileId.value)
  if (!selected) return
  preview.target_ids = [...selected.target_ids]
  preview.task_types = [...selected.task_types]
  preview.task_scale = selected.task_scale
  preview.task_count_limit = selected.task_count_limit ?? null
  preview.per_type_limit = selected.per_type_limit ?? {}
  preview.difficulty_filter = selected.difficulty_filter ?? []
  preview.tag_filter = selected.tag_filter ?? []
  preview.selection_seed = selected.selection_seed ?? null
}

async function createProfile() {
  saving.value = true
  try {
    const created = await adminAPI.benchmark.createProfile({
      suite_id: Number(form.suite_id),
      name: form.name,
      description: form.description || '',
      target_ids: [...form.target_ids],
      task_types: [...form.task_types],
      task_scale: form.task_scale,
      task_count_limit: form.task_count_limit ?? null,
      per_type_limit: form.per_type_limit ?? {},
      difficulty_filter: form.difficulty_filter ?? [],
      tag_filter: form.tag_filter ?? [],
      sampling_strategy: form.sampling_strategy || 'random',
      selection_seed: form.selection_seed ?? null,
      enabled: form.enabled ?? true,
    })
    profiles.value = [created, ...profiles.value.filter((profile) => profile.id !== created.id)]
    pagination.total += 1
    appStore.showSuccess(t('benchmark.admin.profiles.createSuccess'))
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.profiles.createError'))
  } finally {
    saving.value = false
  }
}

async function previewSelectedProfile() {
  if (!previewProfileId.value) return
  previewing.value = true
  try {
    profilePreview.value = await adminAPI.benchmark.previewProfile(previewProfileId.value, {
      target_ids: [...(preview.target_ids || [])],
      task_types: [...(preview.task_types || [])],
      task_scale: preview.task_scale,
      task_count_limit: preview.task_count_limit ?? null,
      per_type_limit: preview.per_type_limit ?? {},
      difficulty_filter: preview.difficulty_filter ?? [],
      tag_filter: preview.tag_filter ?? [],
      selection_seed: preview.selection_seed ?? null,
    })
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.profiles.previewError'))
  } finally {
    previewing.value = false
  }
}

function formatTaskTypes(taskTypes: string[]): string {
  if (taskTypes.length === 0) return '-'
  return taskTypes.map((taskType) => benchmarkTaskTypeLabel(taskType, t)).join(', ')
}

function formatInteger(value: number): string {
  return new Intl.NumberFormat(locale.value, {
    maximumFractionDigits: 0,
  }).format(value)
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
