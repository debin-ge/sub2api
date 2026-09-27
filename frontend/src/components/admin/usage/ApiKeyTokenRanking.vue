<template>
  <!-- 用量页"密钥排行"tab 内容：无卡片外观，依赖父级统一卡片；筛选/时间范围复用页面级筛选栏 -->
  <div>
    <!-- Toolbar -->
    <div class="flex flex-wrap items-center justify-between gap-3 border-b zt-border-c px-4 py-3 sm:px-6">
      <p class="text-xs zt-ink-3">{{ t(`${i18nPrefix}.subtitle`) }}</p>
      <div class="flex items-center gap-3">
        <span v-if="!loading && items.length > 0" class="text-xs zt-ink-3">
          {{ t(`${i18nPrefix}.keyCount`, { count: items.length }) }}
        </span>
        <div class="w-28">
          <Select v-model="limit" :options="limitOptions" @change="load" />
        </div>
      </div>
    </div>

    <!-- Table -->
    <div class="overflow-x-auto">
      <table class="w-full min-w-max divide-y zt-divide">
        <thead class="zt-surface-2">
          <tr>
            <th class="w-16 px-4 py-3 text-left text-xs font-medium uppercase tracking-wider zt-ink-3 sm:px-6">#</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider zt-ink-3">
              {{ t(`${i18nPrefix}.columns.key`) }}
            </th>
            <th v-if="mode === 'admin'" class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider zt-ink-3">
              {{ t(`${i18nPrefix}.columns.user`) }}
            </th>
            <th
              v-for="col in sortableColumns"
              :key="col.key"
              class="cursor-pointer select-none whitespace-nowrap px-4 py-3 text-right text-xs font-medium uppercase tracking-wider transition-colors zt-hover-2"
              :class="sortBy === col.key ? 'zt-accent-text' : 'zt-ink-3'"
              @click="setSort(col.key)"
            >
              {{ t(`${i18nPrefix}.columns.${col.label}`) }}
              <span v-if="sortBy === col.key" aria-hidden="true">↓</span>
            </th>
          </tr>
        </thead>
        <tbody class="divide-y zt-divide zt-surface">
          <tr v-if="loading">
            <td :colspan="sortableColumns.length + (mode === 'admin' ? 3 : 2)" class="py-12 text-center">
              <LoadingSpinner />
            </td>
          </tr>
          <tr v-else-if="items.length === 0">
            <td :colspan="sortableColumns.length + (mode === 'admin' ? 3 : 2)" class="py-12 text-center text-sm zt-ink-3">
              {{ t('admin.dashboard.noDataAvailable') }}
            </td>
          </tr>
          <tr
            v-for="(item, index) in items"
            v-else
            :key="item.api_key_id"
            class="cursor-pointer transition-colors zt-hover-2"
            :title="t(`${i18nPrefix}.rowHint`)"
            @click="$emit('select-key', item.api_key_id, item.key_name)"
          >
            <td class="px-4 py-3 sm:px-6">
              <span
                v-if="index < 3"
                class="inline-flex h-6 w-6 items-center justify-center rounded-full text-xs font-semibold"
                :class="RANK_BADGE_CLASSES[index]"
              >{{ index + 1 }}</span>
              <span v-else class="inline-block w-6 text-center text-sm tabular-nums zt-ink-3">{{ index + 1 }}</span>
            </td>
            <td class="max-w-[260px] truncate px-4 py-3 text-sm font-medium zt-ink-2" :title="item.key_name">
              {{ item.key_name || `Key #${item.api_key_id}` }}
              <span class="ml-1 font-normal zt-ink-3">#{{ item.api_key_id }}</span>
            </td>
            <td v-if="mode === 'admin'" class="max-w-[220px] truncate px-4 py-3 text-sm zt-ink-3" :title="item.email">
              {{ item.email || `User #${item.user_id}` }}
            </td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums zt-ink-3">{{ item.requests.toLocaleString() }}</td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums zt-ink-3">{{ fmtTokens(item.input_tokens) }}</td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums zt-ink-3">{{ fmtTokens(item.output_tokens) }}</td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums zt-ink-3">{{ fmtTokens(item.cache_tokens) }}</td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm font-medium tabular-nums zt-ink">{{ fmtTokens(item.total_tokens) }}</td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm font-medium tabular-nums zt-good-text">${{ fmtCost(item.actual_cost) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getApiKeyBreakdown, type ApiKeyBreakdownParams } from '@/api/admin/dashboard'
import { getMyApiKeyBreakdown, type MyApiKeyBreakdownParams } from '@/api/usage'
import { formatCompactNumber, formatCostFixed } from '@/utils/format'
import type { ApiKeyBreakdownItem } from '@/types'
import Select from '@/components/common/Select.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const props = defineProps<{
  mode: 'admin' | 'user'
  startDate: string
  endDate: string
  startTime?: string
  endTime?: string
  filters: Record<string, unknown>
  model?: string
}>()

defineEmits<{ (e: 'select-key', apiKeyId: number, keyName: string): void }>()

const { t } = useI18n()

const i18nPrefix = computed(() => (props.mode === 'admin' ? 'admin.usage.apiKeyTokenRanking' : 'usage.keyRanking'))

type SortKey = NonNullable<ApiKeyBreakdownParams['sort_by']>
const sortableColumns: { key: SortKey; label: string }[] = [
  { key: 'requests', label: 'requests' },
  { key: 'input_tokens', label: 'inputTokens' },
  { key: 'output_tokens', label: 'outputTokens' },
  { key: 'cache_tokens', label: 'cacheTokens' },
  { key: 'total_tokens', label: 'totalTokens' },
  { key: 'actual_cost', label: 'cost' },
]

const limitOptions = [
  { value: 20, label: 'Top 20' },
  { value: 50, label: 'Top 50' },
  { value: 100, label: 'Top 100' },
  { value: 200, label: 'Top 200' },
]

// 前三名金/银/铜徽章
const RANK_BADGE_CLASSES = [
  'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-400',
  'zt-surface-3 zt-ink-2 dark:bg-gray-500/20',
  'bg-orange-100 text-orange-700 dark:bg-orange-500/20 dark:text-orange-400',
]

const items = ref<ApiKeyBreakdownItem[]>([])
const loading = ref(false)
const sortBy = ref<SortKey>('total_tokens')
const limit = ref(50)
let reqSeq = 0

const fmtTokens = (v: number) => formatCompactNumber(v)
const fmtCost = (v: number) => formatCostFixed(v, 4)

const setSort = (key: SortKey) => {
  if (sortBy.value === key) return
  sortBy.value = key
  load()
}

const load = async () => {
  const seq = ++reqSeq
  loading.value = true
  try {
    const params: ApiKeyBreakdownParams | MyApiKeyBreakdownParams = {
      ...props.filters,
      start_date: props.startDate,
      end_date: props.endDate,
      start_time: props.startTime,
      end_time: props.endTime,
      sort_by: sortBy.value,
      limit: limit.value,
    }
    if (props.model) {
      (params as ApiKeyBreakdownParams | MyApiKeyBreakdownParams).model = props.model
    }
    if (props.mode === 'admin') {
      const res = await getApiKeyBreakdown(params as ApiKeyBreakdownParams)
      if (seq !== reqSeq) return
      items.value = res.api_keys || []
    } else {
      const res = await getMyApiKeyBreakdown(params as MyApiKeyBreakdownParams)
      if (seq !== reqSeq) return
      items.value = res.api_keys || []
    }
  } catch {
    if (seq !== reqSeq) return
    items.value = []
  } finally {
    if (seq === reqSeq) loading.value = false
  }
}

// Reload when the shared filters / date range / model change.
watch(
  () => [props.startDate, props.endDate, props.model, JSON.stringify(props.filters)],
  () => load(),
  { immediate: true }
)

defineExpose({ reload: load })
</script>
