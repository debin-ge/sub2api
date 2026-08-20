<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="space-y-3">
          <div class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900/40 dark:bg-amber-950/40 dark:text-amber-100">
            <p>{{ t('admin.modelPrices.noticeSalePrice') }}</p>
            <p class="mt-1">{{ t('admin.modelPrices.noticeCurrentPrice') }}</p>
            <p class="mt-1">{{ t('admin.modelPrices.noticeCallable') }}</p>
            <p class="mt-1">{{ t('admin.modelPrices.noticeDeepSeekTime') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <div class="text-sm text-gray-600 dark:text-gray-300">
              {{ t('admin.modelPrices.catalogCount') }}:
              <span class="font-medium text-gray-900 dark:text-white">{{ status.catalog_model_count ?? 0 }}</span>
            </div>
            <div class="text-sm text-gray-600 dark:text-gray-300">
              {{ t('admin.modelPrices.overrideCount') }}:
              <span class="font-medium text-gray-900 dark:text-white">{{ status.override_count ?? 0 }}</span>
            </div>
            <div class="text-sm text-gray-600 dark:text-gray-300">
              {{ t('admin.modelPrices.lastUpdated') }}:
              <span class="font-medium text-gray-900 dark:text-white">{{ lastUpdatedLabel }}</span>
            </div>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <div class="flex-1 sm:max-w-64">
              <input
                v-model="filters.q"
                type="text"
                class="input"
                :placeholder="t('admin.modelPrices.search')"
                @input="handleSearch"
              />
            </div>
            <Select v-model="filters.platform" :options="platformOptions" class="w-40" @change="reload" />
            <Select v-model="filters.status" :options="statusOptions" class="w-40" @change="reload" />
            <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
              <button class="btn btn-secondary" :disabled="loading || syncing" @click="handleSync">
                <Icon name="refresh" size="md" :class="syncing ? 'animate-spin' : ''" />
                <span class="ml-2">{{ t('admin.modelPrices.syncNow') }}</span>
              </button>
              <button class="btn btn-primary" @click="openCreate">
                {{ t('admin.modelPrices.addModel') }}
              </button>
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="items" :loading="loading">
          <template #cell-model="{ row }">
            <div class="flex flex-col gap-1">
              <span class="font-medium text-gray-900 dark:text-white">{{ row.model }}</span>
              <div class="flex flex-wrap gap-1">
                <span v-if="row.token_pricing_absent && !row.has_image_pricing" class="badge-warn">{{ t('admin.modelPrices.missing') }}</span>
                <span v-if="row.has_image_pricing && row.token_pricing_absent" class="badge-info">{{ t('admin.modelPrices.imageOnly') }}</span>
                <span v-if="row.sync_invalidated" class="badge-danger">{{ t('admin.modelPrices.syncInvalidated') }}</span>
                <span v-if="row.redundant" class="badge-muted">{{ t('admin.modelPrices.redundant') }}</span>
                <span v-if="row.enabled === false" class="badge-muted">{{ t('admin.modelPrices.disabled') }}</span>
              </div>
            </div>
          </template>
          <template #cell-source="{ row }">
            <span class="badge-muted">{{ sourceLabel(row.source) }}</span>
          </template>
          <template #cell-input="{ row }">
            <div class="flex flex-col gap-0.5 text-xs">
              <span>{{ scheduledPriceLabel(row.effective?.input_cost_per_token, row.time_schedule, 'peak') }}</span>
              <span v-if="row.time_schedule" class="text-gray-500 dark:text-gray-400">
                {{ scheduledPriceLabel(row.effective?.input_cost_per_token, row.time_schedule, 'offPeak') }}
              </span>
            </div>
          </template>
          <template #cell-output="{ row }">
            <div class="flex flex-col gap-0.5 text-xs">
              <span>{{ scheduledPriceLabel(row.effective?.output_cost_per_token, row.time_schedule, 'peak') }}</span>
              <span v-if="row.time_schedule" class="text-gray-500 dark:text-gray-400">
                {{ scheduledPriceLabel(row.effective?.output_cost_per_token, row.time_schedule, 'offPeak') }}
              </span>
            </div>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button class="action-btn" @click="openEdit(row)">
                <Icon name="edit" size="sm" />
                <span class="text-xs">{{ row.source === 'catalog' ? t('admin.modelPrices.fill') : t('admin.modelPrices.edit') }}</span>
              </button>
              <button
                v-if="row.override_platform"
                class="action-btn action-danger"
                @click="askDelete(row)"
              >
                <Icon name="trash" size="sm" />
                <span class="text-xs">{{ t('admin.modelPrices.delete') }}</span>
              </button>
            </div>
          </template>
          <template #empty>
            <EmptyState :message="t('admin.modelPrices.empty')" />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.pageSize"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <BaseDialog :show="showEditor" :title="editorTitle" width="wide" @close="showEditor = false">
      <div class="space-y-4">
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="block text-sm">
            <span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('admin.modelPrices.modelName') }}</span>
            <input v-model="form.model" class="input" :disabled="!creating" />
          </label>
          <label class="block text-sm">
            <span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('admin.modelPrices.platform') }}</span>
            <Select v-model="form.platform" :options="writePlatformOptions" :disabled="!creating" />
          </label>
        </div>
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
          <input v-model="form.enabled" type="checkbox" class="rounded border-gray-300" />
          {{ t('admin.modelPrices.enabled') }}
        </label>
        <div class="space-y-3">
          <div v-for="field in PRICE_FIELDS" :key="field" class="grid items-end gap-2 sm:grid-cols-[1fr,1fr,1fr,auto]">
            <div class="text-xs text-gray-500 dark:text-gray-400">
              <div class="font-medium text-gray-800 dark:text-gray-200">{{ fieldLabel(field) }}</div>
              <div>{{ t('admin.modelPrices.catalogValue') }}: {{ formatPrice(detail?.catalog?.[field], isImageField(field)) }}</div>
            </div>
            <input
              v-model="form.fields[field]"
              class="input"
              :placeholder="isImageField(field) ? t('admin.modelPrices.perImage') : t('admin.modelPrices.perMTok')"
            />
            <div class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.modelPrices.effectiveValue') }}:
              {{ formatPrice(detail?.effective?.[field], isImageField(field)) }}
              <span v-if="detail?.time_schedule && !isImageField(field)" class="ml-1">
                / {{ t('admin.modelPrices.peakPrice') }}
                {{ scheduledPrice(detail?.effective?.[field], detail.time_schedule, 'peak') }}
                / {{ t('admin.modelPrices.offPeakPrice') }}
                {{ scheduledPrice(detail?.effective?.[field], detail.time_schedule, 'offPeak') }}
              </span>
            </div>
            <button type="button" class="btn btn-secondary btn-sm" @click="form.fields[field] = ''">
              {{ t('admin.modelPrices.clearField') }}
            </button>
          </div>
        </div>
        <label class="block text-sm">
          <span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('admin.modelPrices.note') }}</span>
          <textarea v-model="form.note" class="input min-h-20" />
        </label>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" @click="showEditor = false">{{ t('admin.modelPrices.cancel') }}</button>
          <button class="btn btn-primary" :disabled="saving" @click="saveOverride(false)">{{ t('admin.modelPrices.save') }}</button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="showDelete"
      :title="t('admin.modelPrices.delete')"
      :message="t('admin.modelPrices.confirmDelete')"
      danger
      @confirm="confirmDelete"
      @cancel="showDelete = false"
    />
    <ConfirmDialog
      :show="showMagnitude"
      :title="t('admin.modelPrices.save')"
      :message="t('admin.modelPrices.confirmMagnitude')"
      @confirm="saveOverride(true)"
      @cancel="showMagnitude = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import {
  PRICE_FIELDS,
  deleteModelPrice,
  getModelPriceEntry,
  getModelPriceSyncStatus,
  isImageField,
  listModelPricePlatforms,
  listModelPrices,
  mTokToToken,
  syncModelPrices,
  tokenToMTok,
  upsertModelPrice,
  type ModelPriceDetail,
  type ModelPriceListItem,
  type ModelPriceSyncStatus,
  type ModelPriceTimeSchedule,
  type PriceField,
} from '@/api/admin/modelPrices'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const syncing = ref(false)
const saving = ref(false)
const items = ref<ModelPriceListItem[]>([])
const platforms = ref<string[]>(['*'])
const status = ref<ModelPriceSyncStatus>({})
const showEditor = ref(false)
const showDelete = ref(false)
const showMagnitude = ref(false)
const creating = ref(false)
const detail = ref<ModelPriceDetail | null>(null)
const pendingDelete = ref<ModelPriceListItem | null>(null)
let searchTimer: ReturnType<typeof setTimeout> | null = null

const pagination = reactive({ page: 1, pageSize: 50, total: 0 })
const filters = reactive({ q: '', platform: '', status: '' })
const form = reactive({
  platform: '*',
  model: '',
  enabled: true,
  note: '',
  fields: Object.fromEntries(PRICE_FIELDS.map((field) => [field, ''])) as Record<PriceField, string>,
})

const columns = computed<Column[]>(() => [
  { key: 'model', label: t('admin.modelPrices.columns.model') },
  { key: 'platform', label: t('admin.modelPrices.columns.platform') },
  { key: 'source', label: t('admin.modelPrices.columns.source') },
  { key: 'input', label: t('admin.modelPrices.columns.input') },
  { key: 'output', label: t('admin.modelPrices.columns.output') },
  { key: 'actions', label: t('admin.modelPrices.columns.actions') },
])

const platformOptions = computed(() => [
  { value: '', label: t('admin.modelPrices.allPlatforms') },
  ...platforms.value.map((platform) => ({
    value: platform,
    label: platform === '*' ? t('admin.modelPrices.wildcard') : platform,
  })),
])

const writePlatformOptions = computed(() =>
  platforms.value.map((platform) => ({
    value: platform,
    label: platform === '*' ? t('admin.modelPrices.wildcard') : platform,
  })),
)

const statusOptions = computed(() => [
  { value: '', label: t('admin.modelPrices.statusAll') },
  { value: 'overridden', label: t('admin.modelPrices.statusOverridden') },
  { value: 'missing', label: t('admin.modelPrices.statusMissing') },
  { value: 'sync_invalidated', label: t('admin.modelPrices.statusInvalidated') },
])

const lastUpdatedLabel = computed(() => {
  if (!status.value.last_updated) return '-'
  return formatDateTime(status.value.last_updated)
})

const editorTitle = computed(() =>
  creating.value ? t('admin.modelPrices.addModel') : t('admin.modelPrices.edit'),
)

function fieldLabel(field: PriceField): string {
  return t(`admin.modelPrices.fields.${field}`)
}

function sourceLabel(source: string): string {
  if (source === 'override') return t('admin.modelPrices.sourceOverride')
  if (source === 'merged') return t('admin.modelPrices.sourceMerged')
  if (source === 'official') return t('admin.modelPrices.sourceOfficial')
  return t('admin.modelPrices.sourceCatalog')
}

function formatPrice(value: unknown, image: boolean): string {
  if (value == null || value === '') return t('admin.modelPrices.inherit')
  const n = Number(value)
  if (Number.isNaN(n)) return String(value)
  if (image) return n.toString()
  const shown = tokenToMTok(n)
  return shown === '' ? t('admin.modelPrices.inherit') : String(shown)
}

// 生效价可能是空闲价（目录 / 手动覆盖）也可能是高峰价（官方兜底表），
// 两档一律按后端给的倍率换算，不要假设生效价本身是哪一档。
function scheduledPrice(
  value: unknown,
  schedule: ModelPriceTimeSchedule | undefined,
  kind: 'peak' | 'offPeak',
): string {
  if (value == null || value === '' || !schedule) return t('admin.modelPrices.inherit')
  const n = Number(value)
  if (Number.isNaN(n)) return String(value)
  const multiplier = kind === 'peak' ? schedule.peak_multiplier : schedule.off_peak_multiplier
  if (typeof multiplier !== 'number' || !Number.isFinite(multiplier)) return formatPrice(n, false)
  return formatPrice(n * multiplier, false)
}

function scheduledPriceLabel(
  value: unknown,
  schedule: ModelPriceTimeSchedule | undefined,
  kind: 'peak' | 'offPeak',
): string {
  if (!schedule) return formatPrice(value, false)
  const label = kind === 'peak' ? t('admin.modelPrices.peakPrice') : t('admin.modelPrices.offPeakPrice')
  return `${label} ${scheduledPrice(value, schedule, kind)}`
}

function handleSearch() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    pagination.page = 1
    void reload()
  }, 300)
}

function handlePageChange(page: number) {
  pagination.page = page
  void reload()
}

function handlePageSizeChange(pageSize: number) {
  pagination.pageSize = pageSize
  pagination.page = 1
  void reload()
}

async function reload() {
  loading.value = true
  try {
    const [list, syncStatus] = await Promise.all([
      listModelPrices({
        platform: filters.platform || undefined,
        q: filters.q || undefined,
        status: filters.status || undefined,
        page: pagination.page,
        page_size: pagination.pageSize,
      }),
      getModelPriceSyncStatus(),
    ])
    items.value = list.items || []
    pagination.total = list.total || 0
    status.value = syncStatus
  } catch (error) {
    appStore.showError(extractI18nErrorMessage(error, t, 'admin.modelPrices.errors', t('common.error')))
  } finally {
    loading.value = false
  }
}

async function handleSync() {
  syncing.value = true
  try {
    const result = await syncModelPrices()
    status.value = result
    appStore.showSuccess(t('admin.modelPrices.syncDone', { count: result.overrides_reapplied ?? 0 }))
    await reload()
  } catch (error) {
    appStore.showError(extractI18nErrorMessage(error, t, 'admin.modelPrices.errors', t('common.error')))
  } finally {
    syncing.value = false
  }
}

function resetForm() {
  form.platform = '*'
  form.model = ''
  form.enabled = true
  form.note = ''
  for (const field of PRICE_FIELDS) {
    form.fields[field] = ''
  }
}

function fillFormFromDetail(entry: ModelPriceDetail) {
  form.platform = entry.override_platform || entry.platform || '*'
  form.model = entry.model
  form.enabled = entry.enabled
  form.note = entry.note || ''
  for (const field of PRICE_FIELDS) {
    const raw = entry.override ? entry.override[field] : undefined
    if (raw == null) {
      form.fields[field] = ''
      continue
    }
    form.fields[field] = isImageField(field) ? String(raw) : String(tokenToMTok(Number(raw)))
  }
}

function openCreate() {
  creating.value = true
  detail.value = null
  resetForm()
  showEditor.value = true
}

async function openEdit(row: ModelPriceListItem) {
  creating.value = false
  try {
    const entry = await getModelPriceEntry(row.override_platform || '*', row.model)
    detail.value = entry
    fillFormFromDetail(entry)
    showEditor.value = true
  } catch (error) {
    appStore.showError(extractI18nErrorMessage(error, t, 'admin.modelPrices.errors', t('common.error')))
  }
}

function buildPayload(): Record<string, number> {
  const payload: Record<string, number> = {}
  for (const field of PRICE_FIELDS) {
    const raw = form.fields[field].trim()
    if (raw === '') continue
    const value = isImageField(field) ? Number(raw) : mTokToToken(raw)
    if (value == null || Number.isNaN(value)) continue
    payload[field] = value
  }
  return payload
}

function hasMagnitudeRisk(payload: Record<string, number>): boolean {
  return PRICE_FIELDS.some((field) => !isImageField(field) && payload[field] != null && payload[field] > 1)
}

async function saveOverride(confirmed: boolean) {
  const payload = buildPayload()
  if (!confirmed && hasMagnitudeRisk(payload)) {
    showMagnitude.value = true
    return
  }
  showMagnitude.value = false
  saving.value = true
  try {
    await upsertModelPrice({
      platform: form.platform,
      model: form.model,
      payload,
      enabled: form.enabled,
      note: form.note || undefined,
    })
    showEditor.value = false
    await reload()
  } catch (error) {
    appStore.showError(extractI18nErrorMessage(error, t, 'admin.modelPrices.errors', t('common.error')))
  } finally {
    saving.value = false
  }
}

function askDelete(row: ModelPriceListItem) {
  pendingDelete.value = row
  showDelete.value = true
}

async function confirmDelete() {
  const row = pendingDelete.value
  showDelete.value = false
  if (!row?.override_platform) return
  try {
    await deleteModelPrice(row.override_platform, row.model)
    await reload()
  } catch (error) {
    appStore.showError(extractI18nErrorMessage(error, t, 'admin.modelPrices.errors', t('common.error')))
  }
}

onMounted(async () => {
  try {
    platforms.value = await listModelPricePlatforms()
  } catch {
    platforms.value = ['*']
  }
  await reload()
})
</script>

<style scoped>
.badge-warn {
  @apply inline-flex rounded bg-amber-100 px-2 py-0.5 text-xs text-amber-800 dark:bg-amber-900/40 dark:text-amber-100;
}
.badge-info {
  @apply inline-flex rounded bg-sky-100 px-2 py-0.5 text-xs text-sky-800 dark:bg-sky-900/40 dark:text-sky-100;
}
.badge-danger {
  @apply inline-flex rounded bg-red-100 px-2 py-0.5 text-xs text-red-800 dark:bg-red-900/40 dark:text-red-100;
}
.badge-muted {
  @apply inline-flex rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-700 dark:bg-dark-600 dark:text-gray-200;
}
.action-btn {
  @apply flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400;
}
.action-danger {
  @apply hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400;
}
</style>
