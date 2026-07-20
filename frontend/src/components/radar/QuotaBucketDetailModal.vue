<template>
  <Teleport to="body">
    <div
      v-if="show && bucket"
      data-testid="quota-modal-backdrop"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      @click.self="emit('close')"
    >
      <section
        ref="dialogRef"
        tabindex="-1"
        role="dialog"
        aria-modal="true"
        aria-labelledby="quota-bucket-modal-title"
        class="flex max-h-[90vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl bg-white shadow-2xl dark:bg-dark-900"
      >
        <header class="sticky top-0 z-10 flex items-start justify-between gap-4 border-b border-gray-200 bg-white px-5 py-4 dark:border-dark-800 dark:bg-dark-900 sm:px-6">
          <div class="min-w-0">
            <p class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ platformLabel(bucket.platform) }} · {{ bucket.plan_tier }}
            </p>
            <h2 id="quota-bucket-modal-title" class="mt-1 truncate text-xl font-bold text-gray-950 dark:text-white">
              {{ bucket.display_name }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('radar.quota.accounts', 'Accounts') }}: {{ formatNumber(bucket.accounts_count) }}
              · {{ t('radar.quota.capturedAt', 'Snapshot') }}:
              <time :datetime="bucket.captured_at">{{ formatDate(bucket.captured_at) }}</time>
            </p>
          </div>
          <button
            ref="closeButtonRef"
            data-testid="quota-modal-close"
            type="button"
            :aria-label="t('radar.quota.closeDetails', 'Close quota details')"
            class="shrink-0 rounded-lg p-2 text-gray-500 hover:bg-gray-100 hover:text-gray-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-gray-400 dark:hover:bg-dark-800 dark:hover:text-white"
            @click="emit('close')"
          >
            <Icon name="x" size="md" aria-hidden="true" />
          </button>
        </header>

        <div class="overflow-y-auto px-5 py-5 sm:px-6">
          <div role="tablist" :aria-label="t('radar.quota.windowTabs', 'Quota window')" class="mb-5 flex gap-2 border-b border-gray-200 dark:border-dark-800">
            <button
              v-for="windowOption in windowOptions"
              :key="windowOption.key"
              type="button"
              role="tab"
              :data-window="windowOption.key"
              :id="`quota-tab-${windowOption.key}`"
              :aria-selected="selectedWindow === windowOption.key"
              :aria-controls="`quota-panel-${windowOption.key}`"
              :tabindex="selectedWindow === windowOption.key ? 0 : -1"
              :disabled="!windowOption.available"
              class="-mb-px border-b-2 px-4 py-2 text-sm font-semibold focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-40"
              :class="selectedWindow === windowOption.key ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'"
              @click="selectWindow(windowOption.key)"
              @keydown="handleWindowKeydown"
            >
              {{ windowOption.label }}
            </button>
          </div>

          <div
            :id="`quota-panel-${selectedWindow}`"
            role="tabpanel"
            :aria-labelledby="`quota-tab-${selectedWindow}`"
          >
            <div v-if="selectedStats" class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <div v-for="stat in summaryStats" :key="stat.label" class="rounded-xl bg-gray-50 p-3 dark:bg-dark-800/70">
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ stat.label }}</p>
                <p class="mt-1 font-semibold text-gray-950 dark:text-white">{{ stat.value }}</p>
              </div>
            </div>
            <div v-else class="rounded-xl border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
              {{ t('radar.quota.noWindow', 'No data for this window') }}
            </div>

            <p
              v-if="selectedStats && bucket.accounts_count < sampleSizeWarnBelow"
              class="mt-3 inline-flex items-center gap-1.5 text-sm font-medium text-amber-700 dark:text-amber-300"
            >
              <Icon name="exclamationTriangle" size="sm" aria-hidden="true" />
              {{ t('radar.quota.smallSample', 'Small sample') }}: n={{ formatNumber(bucket.accounts_count) }}
            </p>

            <section
              v-if="selectedWindow === '7d' && bucket.platform === 'anthropic' && (bucket.seven_day_sonnet || bucket.seven_day_fable)"
              class="mt-6"
            >
              <h3 class="text-base font-semibold text-gray-950 dark:text-white">
                {{ t('radar.quota.modelUtilization', 'Anthropic model utilization') }}
              </h3>
              <div class="mt-3 grid gap-3 sm:grid-cols-2">
                <div v-if="bucket.seven_day_sonnet" class="rounded-xl border border-gray-200 p-4 dark:border-dark-800">
                  <p class="font-medium text-gray-900 dark:text-white">Sonnet</p>
                  <p class="mt-1 text-2xl font-bold text-gray-950 dark:text-white">{{ formatPercent(bucket.seven_day_sonnet.avg_utilization) }}</p>
                  <p class="text-xs text-gray-500 dark:text-gray-400">n={{ formatNumber(bucket.seven_day_sonnet.sample_size) }}</p>
                </div>
                <div v-if="bucket.seven_day_fable" class="rounded-xl border border-gray-200 p-4 dark:border-dark-800">
                  <p class="font-medium text-gray-900 dark:text-white">Fable</p>
                  <p class="mt-1 text-2xl font-bold text-gray-950 dark:text-white">{{ formatPercent(bucket.seven_day_fable.avg_utilization) }}</p>
                  <p class="text-xs text-gray-500 dark:text-gray-400">n={{ formatNumber(bucket.seven_day_fable.sample_size) }}</p>
                </div>
              </div>
            </section>

            <section class="mt-6">
              <h3 class="text-base font-semibold text-gray-950 dark:text-white">
                {{ t('radar.quota.breakdown', 'Model cost breakdown') }}
              </h3>
              <div v-if="selectedBreakdown.length > 0" class="mt-3 overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-800">
                <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-800">
                  <thead class="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800/70 dark:text-gray-400">
                    <tr>
                      <th class="px-4 py-3">{{ t('radar.quota.model', 'Model') }}</th>
                      <th class="px-4 py-3 text-right">{{ t('radar.quota.averageCost', 'Average cost') }}</th>
                      <th class="px-4 py-3 text-right">{{ t('radar.quota.averageRequests', 'Average requests') }}</th>
                      <th class="px-4 py-3 text-right">{{ t('radar.quota.share', 'Share') }}</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                    <tr v-for="row in selectedBreakdown" :key="row.model">
                      <td class="whitespace-nowrap px-4 py-3 font-medium text-gray-900 dark:text-white">{{ row.model }}</td>
                      <td class="whitespace-nowrap px-4 py-3 text-right text-gray-600 dark:text-gray-300">{{ formatUSD(row.avg_cost) }}</td>
                      <td class="whitespace-nowrap px-4 py-3 text-right text-gray-600 dark:text-gray-300">{{ formatNumber(row.avg_requests) }}</td>
                      <td class="whitespace-nowrap px-4 py-3 text-right text-gray-600 dark:text-gray-300">{{ formatPercent(row.percentage) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <p v-else class="mt-3 rounded-xl border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
                {{ t('radar.quota.noBreakdown', 'No model breakdown data') }}
              </p>
            </section>

            <section class="mt-6">
              <h3 class="text-base font-semibold text-gray-950 dark:text-white">{{ t('radar.quota.trend', 'Trend') }}</h3>
              <p v-if="trendLoading" role="status" aria-live="polite" class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ t('radar.trend.loading', 'Loading trend') }}</p>
              <p v-if="trendError" role="status" aria-live="polite" class="mt-3 text-sm text-red-700 dark:text-red-300">{{ t('radar.trend.error', 'Unable to load trend') }}</p>
              <div
                v-if="visualTrendData.length > 0"
                data-testid="quota-modal-trend-bars"
                class="mt-3 flex h-24 max-w-full items-end gap-1 overflow-hidden"
                aria-hidden="true"
              >
                <span
                  v-for="point in visualTrendData"
                  :key="point.timestamp"
                  data-trend-point
                  :data-trend-value="point.value"
                  :data-trend-timestamp="point.timestamp"
                  class="min-w-0 flex-1 rounded-t bg-primary-500"
                  :style="{ height: `${Math.max(4, Math.min(100, point.value))}%` }"
                />
              </div>
              <p v-if="trendSummary" data-testid="quota-modal-trend-summary" class="sr-only">
                {{ t('radar.quota.trend', 'Trend') }}. {{ selectedWindowLabel }}.
                {{ t('radar.quota.trendRange', 'Range') }}:
                {{ formatDate(trendSummary.startTimestamp) }} – {{ formatDate(trendSummary.endTimestamp) }}.
                {{ formatNumber(trendSummary.count) }} {{ t('radar.quota.trendPoints', 'points') }}.
                {{ t('radar.quota.trendLatest', 'Latest') }}: {{ formatPercent(trendSummary.latest) }}.
                {{ t('radar.quota.trendMinimum', 'Minimum') }}: {{ formatPercent(trendSummary.minimum) }}.
                {{ t('radar.quota.trendMaximum', 'Maximum') }}: {{ formatPercent(trendSummary.maximum) }}.
              </p>
              <p v-if="!trendLoading && !trendError && trendData.length === 0" class="mt-3 rounded-xl border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
                {{ t('radar.quota.noTrend', 'No trend data') }}
              </p>
            </section>
          </div>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type {
  BucketSnapshotDTO,
  ModelCostBreakdownDTO,
  QuotaTrendDTO,
  RadarPlatform,
  WindowStatsDTO,
} from '@/types/radar'

type QuotaWindow = '5h' | '7d'

const props = withDefaults(defineProps<{
  show: boolean
  bucket?: BucketSnapshotDTO | null
  trend?: QuotaTrendDTO | null
  trendLoading?: boolean
  trendError?: string | null
  sampleSizeWarnBelow?: number
}>(), {
  bucket: null,
  trend: null,
  trendLoading: false,
  trendError: null,
  sampleSizeWarnBelow: 3,
})

const emit = defineEmits<{
  close: []
}>()
const { t, locale } = useI18n()
const dialogRef = ref<HTMLElement | null>(null)
const closeButtonRef = ref<HTMLButtonElement | null>(null)
const selectedWindow = ref<QuotaWindow>('5h')
let returnFocusTo: HTMLElement | null = null
let previousBodyOverflow = ''
let bodyLocked = false

const windowOptions = computed(() => [
  { key: '5h' as const, label: t('radar.quota.window5h', '5 hours'), available: Boolean(props.bucket?.five_hour) },
  { key: '7d' as const, label: t('radar.quota.window7d', '7 days'), available: Boolean(props.bucket?.seven_day) },
])
const selectedStats = computed<WindowStatsDTO | null>(() => {
  if (!props.bucket) return null
  return selectedWindow.value === '5h' ? props.bucket.five_hour : props.bucket.seven_day
})
const selectedBreakdown = computed<readonly ModelCostBreakdownDTO[]>(() => {
  if (!props.bucket) return []
  // Older cached Radar snapshots may contain JSON null for an empty Go slice.
  // Keep the public dialog compatible with those snapshots while the backend
  // normalizes newly served responses to arrays.
  return (selectedWindow.value === '5h' ? props.bucket.model_breakdown_5h : props.bucket.model_breakdown_7d) ?? []
})
const summaryStats = computed(() => {
  const stats = selectedStats.value
  if (!stats) return []
  return [
    { label: t('radar.quota.averageUtilization', 'Average utilization'), value: formatPercent(stats.avg_utilization) },
    { label: t('radar.quota.range', 'Utilization range'), value: `${formatPercent(stats.min_utilization)} – ${formatPercent(stats.max_utilization)}` },
    { label: t('radar.quota.averageCost', 'Average cost'), value: formatUSD(stats.avg_cost) },
    { label: t('radar.quota.inferredLimit', 'Inferred limit'), value: formatInference(stats) },
    { label: t('radar.quota.sampleSize', 'Sample size'), value: formatNumber(stats.sample_size) },
  ]
})
const selectedWindowLabel = computed(() => selectedWindow.value === '5h'
  ? t('radar.quota.fiveHourUtilization', '5-hour utilization')
  : t('radar.quota.sevenDayUtilization', '7-day utilization'))
const trendData = computed(() => {
  if (!props.trend) return []
  return (props.trend.data_points ?? []).flatMap((point) => {
    const value = selectedWindow.value === '5h'
      ? point.five_hour?.avg_utilization
      : point.seven_day?.avg_utilization
    return value !== undefined && Number.isFinite(value)
      ? [{ timestamp: point.timestamp, value }]
      : []
  })
})
const trendSummary = computed(() => {
  const data = trendData.value
  if (data.length === 0) return null
  const values = data.map((point) => point.value)
  return {
    count: data.length,
    startTimestamp: data[0].timestamp,
    endTimestamp: data[data.length - 1].timestamp,
    latest: data[data.length - 1].value,
    minimum: Math.min(...values),
    maximum: Math.max(...values),
  }
})
const visualTrendData = computed(() => downsampleTrendData(trendData.value, 96))
const numberFormatter = computed(() => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }))
const percentFormatter = computed(() => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }))
const dateTimeFormatter = computed(() => new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }))
const usdFormatter = computed(() => new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 2 }))

watch(() => props.bucket, (bucket) => {
  chooseAvailableWindow()
  if (props.show && bucket && !bodyLocked) openDialog()
  else if (!bucket && bodyLocked) closeDialog()
})
watch(() => props.show, (show) => {
  if (show && props.bucket) {
    openDialog()
  }
  else closeDialog()
}, { immediate: true })

function chooseAvailableWindow(): void {
  const selectedIsAvailable = selectedWindow.value === '5h'
    ? Boolean(props.bucket?.five_hour)
    : Boolean(props.bucket?.seven_day)
  if (selectedIsAvailable) return
  if (props.bucket?.five_hour) selectedWindow.value = '5h'
  else if (props.bucket?.seven_day) selectedWindow.value = '7d'
}

function openDialog(): void {
  chooseAvailableWindow()
  if (bodyLocked) {
    void nextTick(() => closeButtonRef.value?.focus())
    return
  }
  returnFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
  previousBodyOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  bodyLocked = true
  document.addEventListener('keydown', handleDocumentKeydown)
  void nextTick(() => closeButtonRef.value?.focus())
}

function closeDialog(): void {
  document.removeEventListener('keydown', handleDocumentKeydown)
  if (bodyLocked) {
    document.body.style.overflow = previousBodyOverflow
    bodyLocked = false
  }
  returnFocusTo?.focus()
  returnFocusTo = null
}

function handleDocumentKeydown(event: KeyboardEvent): void {
  if (!props.show) return
  if (event.key === 'Escape') {
    event.preventDefault()
    emit('close')
    return
  }
  if (event.key !== 'Tab' || !dialogRef.value) return
  const focusable = Array.from(dialogRef.value.querySelectorAll<HTMLElement>(
    'button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
  )).filter((element) => element.tabIndex >= 0 && !element.hasAttribute('disabled') && element.getAttribute('aria-hidden') !== 'true')
  if (focusable.length === 0) {
    event.preventDefault()
    dialogRef.value.focus()
    return
  }
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (!dialogRef.value.contains(document.activeElement)) {
    event.preventDefault()
    ;(event.shiftKey ? last : first).focus()
  } else if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

function handleWindowKeydown(event: KeyboardEvent): void {
  if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
  event.preventDefault()
  const available = windowOptions.value.filter((option) => option.available)
  if (available.length < 2) return
  const index = available.findIndex((option) => option.key === selectedWindow.value)
  const offset = event.key === 'ArrowRight' ? 1 : -1
  const next = available[(index + offset + available.length) % available.length]
  selectWindow(next.key)
  void nextTick(() => dialogRef.value?.querySelector<HTMLButtonElement>(`[data-window="${next.key}"]`)?.focus())
}

function selectWindow(value: QuotaWindow): void {
  const available = value === '5h' ? props.bucket?.five_hour : props.bucket?.seven_day
  if (available) selectedWindow.value = value
}

function downsampleTrendData<T extends { value: number }>(data: readonly T[], limit: number): T[] {
  if (data.length <= limit) return [...data]
  let minimumIndex = 0
  let maximumIndex = 0
  for (let index = 1; index < data.length; index += 1) {
    if (data[index].value < data[minimumIndex].value) minimumIndex = index
    if (data[index].value > data[maximumIndex].value) maximumIndex = index
  }
  const selected = new Set([0, data.length - 1, minimumIndex, maximumIndex])
  const remaining = Math.max(0, limit - selected.size)
  for (let slot = 1; slot <= remaining; slot += 1) {
    selected.add(Math.round((slot * (data.length - 1)) / (remaining + 1)))
  }
  return [...selected]
    .sort((left, right) => left - right)
    .slice(0, limit)
    .map((index) => data[index])
}

function formatInference(stats: WindowStatsDTO): string {
  if (stats.inferred_limit_usd === null) {
    switch (stats.inference_reject_reason) {
      case 'insufficient_samples': return t('radar.quota.inference.insufficient', 'Insufficient samples')
      case 'high_dispersion': return t('radar.quota.inference.dispersed', 'Data is too dispersed')
      default: return t('radar.quota.inference.unavailable', 'No trusted result')
    }
  }
  const stdev = stats.inferred_stdev === null ? '' : ` ± ${formatUSD(stats.inferred_stdev)}`
  return `${formatUSD(stats.inferred_limit_usd)}${stdev}`
}

function platformLabel(platform: RadarPlatform): string {
  switch (platform) {
    case 'anthropic': return 'Anthropic'
    case 'openai': return 'OpenAI'
    case 'antigravity': return 'Antigravity'
  }
}

function formatDate(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return t('radar.common.unknownTime', 'Unknown')
  return dateTimeFormatter.value.format(date)
}

function formatNumber(value: number): string {
  return numberFormatter.value.format(value)
}

function formatPercent(value: number): string {
  return `${percentFormatter.value.format(value)}%`
}

function formatUSD(value: number): string {
  return usdFormatter.value.format(value)
}

onBeforeUnmount(closeDialog)
</script>
