<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('benchmark.admin.targets.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.admin.targets.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          {{ t('benchmark.admin.targets.refresh') }}
        </button>
      </div>

      <section class="card">
        <form class="grid grid-cols-1 gap-4 p-6 xl:grid-cols-4 xl:items-end" @submit.prevent="submitTarget">
          <div v-if="formError" class="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/40 dark:bg-red-900/20 dark:text-red-300 xl:col-span-4">
            {{ formError }}
          </div>
          <label class="block xl:col-span-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.targets.fields.modelName') }}</span>
            <input v-model.trim="form.model_name" data-test="target-model-name-input" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.targets.fields.channelId') }}</span>
            <input v-model.number="form.channel_id" data-test="target-channel-id-input" type="number" min="1" class="input mt-1" required />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.targets.fields.displayName') }}</span>
            <input v-model.trim="form.display_name" data-test="target-display-name-input" class="input mt-1" />
          </label>
          <label class="block xl:col-span-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.targets.fields.channelSnapshot') }}</span>
            <input v-model.trim="form.channel_name_snapshot" data-test="target-channel-snapshot-input" class="input mt-1" :placeholder="t('benchmark.admin.targets.fields.channelSnapshotHint')" />
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.targets.fields.sortOrder') }}</span>
            <input v-model.number="form.sort_order" data-test="target-sort-order-input" type="number" class="input mt-1" />
          </label>
          <label class="flex items-center gap-2 pt-6 text-sm text-gray-700 dark:text-gray-200">
            <input v-model="form.enabled" data-test="target-enabled-input" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            {{ t('benchmark.admin.targets.fields.enabled') }}
          </label>
          <label class="flex items-center gap-2 pt-6 text-sm text-gray-700 dark:text-gray-200">
            <input v-model="form.public_visible" data-test="target-public-visible-input" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            {{ t('benchmark.admin.targets.fields.publicVisible') }}
          </label>
          <div class="flex flex-wrap gap-2 xl:col-span-2 xl:justify-end">
            <button type="submit" data-test="target-submit-button" class="btn btn-primary" :disabled="saving">
              {{ editingTarget ? t('benchmark.admin.targets.update') : t('benchmark.admin.targets.create') }}
            </button>
            <button v-if="editingTarget" type="button" class="btn btn-secondary" :disabled="saving" @click="resetForm">
              {{ t('common.cancel') }}
            </button>
          </div>
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
          <template #cell-channel="{ row }">
            {{ row.channel_name_snapshot || benchmarkChannelFallback(row.channel_id, t) }}
          </template>
          <template #cell-enabled="{ row }">
            <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="row.enabled ? enabledClass : disabledClass">
              {{ benchmarkEnabledLabel(row.enabled, t) }}
            </span>
          </template>
          <template #cell-public_visible="{ row }">
            <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="row.public_visible ? enabledClass : disabledClass">
              {{ benchmarkVisibilityLabel(row.public_visible, t) }}
            </span>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex flex-wrap items-center gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :data-test="`target-edit-${row.id}`" @click="editTarget(row)">
                {{ t('benchmark.admin.targets.edit') }}
              </button>
              <button type="button" class="btn btn-danger btn-sm" :data-test="`target-delete-${row.id}`" :disabled="deletingId === row.id" @click="deleteTarget(row)">
                {{ t('common.delete') }}
              </button>
            </div>
          </template>
          <template #empty>
            <EmptyState :title="t('benchmark.admin.targets.emptyTitle')" :description="t('benchmark.admin.targets.emptyDescription')" />
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
import type { BenchmarkTarget, CreateBenchmarkTargetRequest, UpdateBenchmarkTargetRequest } from '@/types/benchmark'
import { benchmarkChannelFallback, benchmarkEnabledLabel, benchmarkVisibilityLabel } from '@/components/radar/benchmarkI18n'

const appStore = useAppStore()
const { t } = useI18n()
const enabledClass = 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
const disabledClass = 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'

const targets = ref<BenchmarkTarget[]>([])
const loading = ref(false)
const saving = ref(false)
const deletingId = ref<number | null>(null)
const editingTarget = ref<BenchmarkTarget | null>(null)
const formError = ref('')
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

type TargetFormState = {
  model_name: string
  channel_id: number
  display_name: string
  channel_name_snapshot: string
  enabled: boolean
  public_visible: boolean
  sort_order: number
}

const defaultForm = (): TargetFormState => ({
  model_name: '',
  channel_id: 0,
  display_name: '',
  channel_name_snapshot: '',
  enabled: true,
  public_visible: true,
  sort_order: 0,
})

const form = reactive<TargetFormState>(defaultForm())

const columns = computed<Column[]>(() => [
  { key: 'model_name', label: t('benchmark.admin.targets.columns.model') },
  { key: 'channel', label: t('benchmark.admin.targets.columns.channel') },
  { key: 'enabled', label: t('benchmark.admin.targets.columns.status') },
  { key: 'public_visible', label: t('benchmark.admin.targets.columns.visibility') },
  { key: 'actions', label: t('benchmark.admin.targets.columns.actions'), sortable: false },
])

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
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.targets.loadError'))
  } finally {
    loading.value = false
  }
}

function resetForm() {
  Object.assign(form, defaultForm())
  editingTarget.value = null
  formError.value = ''
}

function editTarget(target: BenchmarkTarget) {
  editingTarget.value = target
  formError.value = ''
  Object.assign(form, {
    model_name: target.model_name,
    channel_id: target.channel_id,
    display_name: target.display_name || '',
    channel_name_snapshot: target.channel_name_snapshot || '',
    enabled: target.enabled,
    public_visible: target.public_visible,
    sort_order: target.sort_order,
  })
}

function buildPayload(): UpdateBenchmarkTargetRequest {
  return {
    model_name: form.model_name,
    channel_id: Number(form.channel_id),
    display_name: form.display_name || undefined,
    channel_name_snapshot: form.channel_name_snapshot || undefined,
    enabled: form.enabled,
    public_visible: form.public_visible,
    sort_order: Number(form.sort_order || 0),
  }
}

async function submitTarget() {
  formError.value = ''
  const payload = buildPayload()

  saving.value = true
  try {
    if (editingTarget.value) {
      const updated = await adminAPI.benchmark.updateTarget(editingTarget.value.id, payload)
      targets.value = targets.value.map((target) => target.id === updated.id ? updated : target)
      appStore.showSuccess(t('benchmark.admin.targets.updateSuccess'))
      resetForm()
    } else {
      const created = await adminAPI.benchmark.createTarget(payload as CreateBenchmarkTargetRequest)
      targets.value = [created, ...targets.value.filter((target) => target.id !== created.id)]
      pagination.total += 1
      appStore.showSuccess(t('benchmark.admin.targets.createSuccess'))
      resetForm()
    }
  } catch (error) {
    const fallback = editingTarget.value ? t('benchmark.admin.targets.updateError') : t('benchmark.admin.targets.createError')
    appStore.showError(error instanceof Error ? error.message : fallback)
  } finally {
    saving.value = false
  }
}

async function deleteTarget(target: BenchmarkTarget) {
  if (!window.confirm(t('benchmark.admin.targets.deleteConfirm'))) return

  deletingId.value = target.id
  try {
    await adminAPI.benchmark.deleteTarget(target.id)
    appStore.showSuccess(t('benchmark.admin.targets.deleteSuccess'))
    await load()
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.targets.deleteError'))
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
