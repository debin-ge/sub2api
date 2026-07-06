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
          <div class="block xl:col-span-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.targets.fields.sourceType') }}</span>
            <div class="mt-1 inline-flex rounded-md border border-gray-200 bg-white p-1 dark:border-dark-600 dark:bg-dark-800">
              <button
                type="button"
                data-test="target-source-subscription"
                class="rounded px-3 py-1.5 text-sm font-medium"
                :class="form.source_type === 'subscription' ? 'bg-primary-600 text-white' : 'text-gray-600 dark:text-gray-300'"
                @click="setSourceType('subscription')"
              >
                {{ t('benchmark.admin.targets.source.subscription') }}
              </button>
              <button
                type="button"
                data-test="target-source-group"
                class="rounded px-3 py-1.5 text-sm font-medium"
                :class="form.source_type === 'standard' ? 'bg-primary-600 text-white' : 'text-gray-600 dark:text-gray-300'"
                @click="setSourceType('standard')"
              >
                {{ t('benchmark.admin.targets.source.group') }}
              </button>
            </div>
          </div>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.targets.fields.group') }}</span>
            <select
              v-model.number="form.group_id"
              data-test="target-group-select"
              class="input mt-1"
              required
              :disabled="groupsLoading || groupOptions.length === 0"
              @change="onGroupChange"
            >
              <option :value="0" disabled>{{ groupSelectPlaceholder }}</option>
              <option
                v-for="group in groupOptions"
                :key="group.id"
                :value="group.id"
              >
                {{ groupOptionLabel(group) }}
              </option>
            </select>
            <p v-if="groupSelectionHint" data-test="target-group-hint" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ groupSelectionHint }}
            </p>
          </label>
          <label class="block">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.targets.fields.modelName') }}</span>
            <select
              v-model="form.model_name"
              data-test="target-model-select"
              class="input mt-1"
              required
              :disabled="modelsLoading || modelOptions.length === 0"
            >
              <option value="" disabled>{{ modelSelectPlaceholder }}</option>
              <option v-for="model in modelOptions" :key="model" :value="model">{{ model }}</option>
            </select>
          </label>
          <label class="block xl:col-span-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.targets.fields.displayName') }}</span>
            <input v-model.trim="form.display_name" data-test="target-display-name-input" class="input mt-1" />
          </label>
          <div class="block xl:col-span-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('benchmark.admin.targets.fields.channelSnapshot') }}</span>
            <div data-test="target-channel-snapshot" class="mt-1 min-h-[42px] rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-700 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200">
              {{ selectedChannelSnapshot || t('benchmark.admin.targets.fields.channelSnapshotHint') }}
            </div>
          </div>
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
            <button type="submit" data-test="target-submit-button" class="btn btn-primary" :disabled="saving || !canSubmitTarget">
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
import type { AdminGroup, SubscriptionType } from '@/types'
import { benchmarkChannelFallback, benchmarkEnabledLabel, benchmarkVisibilityLabel } from '@/components/radar/benchmarkI18n'

const appStore = useAppStore()
const { t } = useI18n()
const enabledClass = 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
const disabledClass = 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'

const targets = ref<BenchmarkTarget[]>([])
const groups = ref<AdminGroup[]>([])
const modelOptions = ref<string[]>([])
const loading = ref(false)
const groupsLoading = ref(false)
const modelsLoading = ref(false)
const saving = ref(false)
const deletingId = ref<number | null>(null)
const editingTarget = ref<BenchmarkTarget | null>(null)
const formError = ref('')
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

type TargetFormState = {
  model_name: string
  source_type: SubscriptionType
  group_id: number
  channel_id: number
  display_name: string
  channel_name_snapshot: string
  enabled: boolean
  public_visible: boolean
  sort_order: number
}

const defaultForm = (): TargetFormState => ({
  model_name: '',
  source_type: 'subscription',
  group_id: 0,
  channel_id: 0,
  display_name: '',
  channel_name_snapshot: '',
  enabled: true,
  public_visible: true,
  sort_order: 0,
})

const form = reactive<TargetFormState>(defaultForm())

const groupOptions = computed(() => groups.value)
const selectedGroup = computed(() => groups.value.find((group) => group.id === Number(form.group_id)) || null)
const selectedChannelSnapshot = computed(() => selectedGroup.value?.name || form.channel_name_snapshot || '')
const canSubmitTarget = computed(() => {
  if (saving.value || groupsLoading.value || modelsLoading.value || stringsEmpty(form.model_name)) return false
  if (Number(form.group_id) > 0) return selectedGroup.value?.platform === 'openai'
  return Boolean(editingTarget.value && Number(form.channel_id) > 0)
})
const groupSelectPlaceholder = computed(() => {
  if (groupsLoading.value) return t('benchmark.admin.targets.groupLoading')
  if (groupOptions.value.length === 0) return t('benchmark.admin.targets.noGroups')
  return t('benchmark.admin.targets.selectGroup')
})
const modelSelectPlaceholder = computed(() => {
  if (modelsLoading.value) return t('benchmark.admin.targets.modelLoading')
  if (!selectedGroup.value) return t('benchmark.admin.targets.selectGroupFirst')
  if (modelOptions.value.length === 0) return t('benchmark.admin.targets.noModels')
  return t('benchmark.admin.targets.selectModel')
})
const groupSelectionHint = computed(() => {
  if (groupsLoading.value) return ''
  if (!selectedGroup.value) return t('benchmark.admin.targets.groupHint')
  if (selectedGroup.value.platform !== 'openai') {
    return t('benchmark.admin.targets.groupUnsupportedPlatform', { platform: selectedGroup.value.platform })
  }
  return t('benchmark.admin.targets.groupReady')
})

const columns = computed<Column[]>(() => [
  { key: 'model_name', label: t('benchmark.admin.targets.columns.model') },
  { key: 'channel', label: t('benchmark.admin.targets.columns.channel') },
  { key: 'enabled', label: t('benchmark.admin.targets.columns.status') },
  { key: 'public_visible', label: t('benchmark.admin.targets.columns.visibility') },
  { key: 'actions', label: t('benchmark.admin.targets.columns.actions'), sortable: false },
])

async function loadTargets() {
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

async function loadGroups() {
  groupsLoading.value = true
  try {
    groups.value = await adminAPI.groups.getAll()
    await ensureSelectedGroup()
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.targets.groupLoadError'))
  } finally {
    groupsLoading.value = false
  }
}

async function load() {
  await Promise.all([loadTargets(), loadGroups()])
}

function resetForm() {
  Object.assign(form, defaultForm())
  editingTarget.value = null
  formError.value = ''
  ensureSelectedGroup()
}

function editTarget(target: BenchmarkTarget) {
  editingTarget.value = target
  formError.value = ''
  Object.assign(form, {
    model_name: target.model_name,
    source_type: 'subscription',
    group_id: 0,
    channel_id: target.channel_id,
    display_name: target.display_name || '',
    channel_name_snapshot: target.channel_name_snapshot || '',
    enabled: target.enabled,
    public_visible: target.public_visible,
    sort_order: target.sort_order,
  })
  modelOptions.value = target.model_name ? [target.model_name] : []
}

async function ensureSelectedGroup() {
  if (editingTarget.value || Number(form.group_id) > 0) {
    return
  }
  form.group_id = preferredGroups(form.source_type)[0]?.id || groupOptions.value[0]?.id || 0
  await loadModelsForSelectedGroup()
}

async function setSourceType(sourceType: SubscriptionType) {
  if (form.source_type === sourceType) return
  form.source_type = sourceType
  form.group_id = preferredGroups(sourceType)[0]?.id || groupOptions.value[0]?.id || 0
  form.model_name = ''
  modelOptions.value = []
  await loadModelsForSelectedGroup()
}

async function onGroupChange() {
  if (selectedGroup.value?.subscription_type) {
    form.source_type = selectedGroup.value.subscription_type
  }
  form.model_name = ''
  modelOptions.value = []
  await loadModelsForSelectedGroup()
}

async function loadModelsForSelectedGroup() {
  const group = selectedGroup.value
  if (!group) return
  modelsLoading.value = true
  try {
    const models = await adminAPI.groups.getModelsListCandidates(group.id, group.platform)
    modelOptions.value = models || []
    if (!form.model_name && modelOptions.value.length > 0) {
      form.model_name = modelOptions.value[0]
    }
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.targets.modelLoadError'))
  } finally {
    modelsLoading.value = false
  }
}

function groupOptionLabel(group: AdminGroup) {
  const sourceLabel = group.subscription_type === 'subscription'
    ? t('benchmark.admin.targets.source.subscription')
    : t('benchmark.admin.targets.source.group')
  return `${group.name} · ${sourceLabel} (${group.platform})`
}

function preferredGroups(sourceType: SubscriptionType) {
  return groupOptions.value.filter((group) => (group.subscription_type || 'standard') === sourceType)
}

function stringsEmpty(value: string) {
  return value.trim() === ''
}

function buildPayload(): UpdateBenchmarkTargetRequest {
  const payload: UpdateBenchmarkTargetRequest = {
    model_name: form.model_name,
    display_name: form.display_name || undefined,
    channel_name_snapshot: selectedChannelSnapshot.value || undefined,
    enabled: form.enabled,
    public_visible: form.public_visible,
    sort_order: Number(form.sort_order || 0),
  }
  if (Number(form.group_id) > 0) {
    payload.group_id = Number(form.group_id)
  } else if (Number(form.channel_id) > 0) {
    payload.channel_id = Number(form.channel_id)
  }
  return payload
}

async function submitTarget() {
  formError.value = ''
  if (!canSubmitTarget.value) {
    formError.value = t('benchmark.admin.targets.groupRequired')
    return
  }
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
  loadTargets()
}

function onPageSizeChange(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  loadTargets()
}

onMounted(load)
</script>
