<template>
  <div
    v-if="!buckets || buckets.length === 0"
    class="rounded-2xl border border-dashed border-gray-300 p-10 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400"
  >
    {{ t('radar.quota.emptyNoPublishable', 'No publishable quota data. Supported plan buckets require recent passive quota snapshots and their configured minimum sample.') }}
  </div>

  <div v-else ref="gridRef" class="grid gap-5 md:grid-cols-2 xl:grid-cols-3">
    <article
      v-for="bucket in buckets"
      :key="bucket.bucket_key"
      :data-bucket-key="bucket.bucket_key"
      class="group relative rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-800 dark:bg-dark-900"
    >
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <p class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
            {{ platformLabel(bucket.platform) }}
          </p>
          <h3 class="mt-1 truncate font-semibold text-gray-950 dark:text-white">{{ bucket.display_name }}</h3>
        </div>
        <span class="shrink-0 rounded-full bg-gray-100 px-2.5 py-1 text-xs text-gray-600 dark:bg-dark-800 dark:text-gray-300">
          {{ t('radar.quota.accounts', 'Accounts') }}: {{ formatNumber(bucket.accounts_count) }}
        </span>
      </div>

      <p
        v-if="primaryWindowStats(bucket) && primaryWindowSampleSize(bucket) < sampleSizeWarnBelow"
        class="mt-3 text-xs font-semibold text-amber-700 dark:text-amber-300"
      >
        ⚠ {{ t('radar.quota.smallSample', 'Small sample') }}: n={{ formatNumber(primaryWindowSampleSize(bucket)) }}
      </p>

      <div v-if="primaryWindowStats(bucket)" class="mt-6">
        <div class="flex items-end justify-between gap-3">
          <div>
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ primaryWindowLabel(bucket) }}</p>
            <p class="mt-1 text-3xl font-bold tabular-nums text-gray-950 dark:text-white">
              {{ formatPercent(primaryWindowUtilization(bucket)) }}
            </p>
          </div>
        </div>
        <div class="mt-3 h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-800">
          <div
            :data-utilization-level="utilizationLevel(primaryWindowUtilization(bucket))"
            class="h-full rounded-full transition-all"
            :class="utilizationClass(primaryWindowUtilization(bucket))"
            :style="{ width: `${clampedPercent(primaryWindowUtilization(bucket))}%` }"
          />
        </div>

        <div class="mt-5 rounded-xl bg-gray-50 p-3 dark:bg-dark-800/70">
          <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
            {{ t('radar.quota.inferredLimit', 'Inferred limit') }}
          </p>
          <p v-if="primaryWindowLimit(bucket) !== null" class="mt-1 font-semibold text-gray-950 dark:text-white">
            {{ formatUSD(primaryWindowLimit(bucket) ?? 0) }}
            <span v-if="primaryWindowStdev(bucket) !== null" class="font-normal text-gray-500 dark:text-gray-400">
              ± {{ formatUSD(primaryWindowStdev(bucket) ?? 0) }}
            </span>
          </p>
          <p v-else class="mt-1 text-sm text-gray-600 dark:text-gray-300">
            {{ inferenceMessage(primaryWindowInferenceReason(bucket)) }}
          </p>
        </div>
      </div>
      <div v-else class="mt-6 rounded-xl border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
        {{ t('radar.quota.noWindow', 'No data for this window') }}
      </div>

      <div class="mt-5 border-t border-gray-100 pt-4 dark:border-dark-800">
        <div class="mb-2 flex items-center justify-between text-xs text-gray-500 dark:text-gray-400">
          <span>{{ trendTitle(bucket) }}</span>
        </div>
        <p v-if="trendLoading[bucket.bucket_key]" role="status" aria-live="polite" class="py-1 text-center text-xs text-gray-500 dark:text-gray-400">
          {{ t('radar.trend.loading', 'Loading trend') }}
        </p>
        <p v-if="trendErrors[bucket.bucket_key]" role="status" aria-live="polite" class="py-1 text-center text-xs text-red-700 dark:text-red-300">
          {{ t('radar.trend.error', 'Unable to load trend') }}
        </p>
        <svg
          v-if="trendSummary(bucket.bucket_key)?.points"
          :data-testid="`quota-sparkline-${bucket.bucket_key}`"
          viewBox="0 0 180 44"
          class="h-11 w-full overflow-visible"
          aria-hidden="true"
          focusable="false"
        >
          <polyline
            :points="trendSummary(bucket.bucket_key)?.points ?? ''"
            fill="none"
            stroke="currentColor"
            stroke-width="3"
            stroke-linecap="round"
            stroke-linejoin="round"
            class="text-primary-500"
          />
        </svg>
        <p
          v-if="trendSummary(bucket.bucket_key)"
          :data-testid="`quota-sparkline-data-${bucket.bucket_key}`"
          class="sr-only"
        >
          {{ t('radar.quota.sparklineLabel', 'Quota utilization trend') }}.
          {{ trendWindowLabel(trendSummary(bucket.bucket_key)!.window) }}.
          {{ t('radar.quota.trendRange', 'Range') }}:
          {{ formatDateTime(trendSummary(bucket.bucket_key)!.startTimestamp) }} –
          {{ formatDateTime(trendSummary(bucket.bucket_key)!.endTimestamp) }}.
          {{ formatNumber(trendSummary(bucket.bucket_key)!.count) }} {{ t('radar.quota.trendPoints', 'points') }}.
          {{ t('radar.quota.trendLatest', 'Latest') }}: {{ formatPercent(trendSummary(bucket.bucket_key)!.latest) }}.
          {{ t('radar.quota.trendMinimum', 'Minimum') }}: {{ formatPercent(trendSummary(bucket.bucket_key)!.minimum) }}.
          {{ t('radar.quota.trendMaximum', 'Maximum') }}: {{ formatPercent(trendSummary(bucket.bucket_key)!.maximum) }}.
        </p>
        <p
          v-if="!trendSummary(bucket.bucket_key)?.points && !trendLoading[bucket.bucket_key] && !trendErrors[bucket.bucket_key]"
          class="py-3 text-center text-xs text-gray-500 dark:text-gray-400"
        >
          {{ t('radar.quota.noTrend', 'No trend data') }}
        </p>
      </div>

      <p
        :data-testid="`quota-open-hint-${bucket.bucket_key}`"
        aria-hidden="true"
        class="mt-3 text-right text-sm font-semibold text-primary-600 group-hover:underline dark:text-primary-400"
      >
        {{ t('radar.quota.openDetails', 'View details') }} →
      </p>

      <button
        :data-testid="`quota-open-${bucket.bucket_key}`"
        type="button"
        :aria-label="`${t('radar.quota.openDetails', 'View details')}: ${bucket.display_name}`"
        class="absolute inset-0 z-10 cursor-pointer rounded-2xl bg-transparent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 focus-visible:ring-offset-white dark:focus-visible:ring-offset-dark-900"
        @click="emit('select', bucket)"
      />
    </article>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  BucketSnapshotDTO,
  InferenceRejectReason,
  QuotaTrendDTO,
  RadarPlatform,
  WindowStatsDTO,
} from '@/types/radar'

const props = withDefaults(defineProps<{
  buckets?: readonly BucketSnapshotDTO[] | null
  sampleSizeWarnBelow?: number
  trends?: Readonly<Record<string, QuotaTrendDTO | null | undefined>>
  trendLoading?: Readonly<Record<string, boolean | undefined>>
  trendErrors?: Readonly<Record<string, string | null | undefined>>
}>(), {
  buckets: () => [],
  sampleSizeWarnBelow: 3,
  trends: () => ({}),
  trendLoading: () => ({}),
  trendErrors: () => ({}),
})

const emit = defineEmits<{
  select: [bucket: BucketSnapshotDTO]
  requestTrend: [bucketKey: string]
}>()
const { t, locale } = useI18n()
const gridRef = ref<HTMLElement | null>(null)
const observedCards = new Set<Element>()
let trendObserver: IntersectionObserver | null = null

interface SparklineSummary {
  points: string | null
  window: '5h' | '7d'
  count: number
  startTimestamp: string
  endTimestamp: string
  latest: number
  minimum: number
  maximum: number
}

const numberFormatter = computed(() => new Intl.NumberFormat(locale.value))
const percentFormatter = computed(() => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }))
const dateTimeFormatter = computed(() => new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }))
const usdFormatter = computed(() => new Intl.NumberFormat(locale.value, {
  style: 'currency',
  currency: 'USD',
  maximumFractionDigits: 0,
}))
const trendSummaries = computed(() => {
  const summaries = new Map<string, SparklineSummary>()
  for (const [bucketKey, trend] of Object.entries(props.trends)) {
    if (!trend) continue
    const hasFiveHourData = trend.data_points.some((point) => {
      const value = point.five_hour?.avg_utilization
      return value !== undefined && Number.isFinite(value)
    })
    const window = hasFiveHourData ? '5h' : '7d'
    const data = trend.data_points.flatMap((point) => {
      const value = window === '5h'
        ? point.five_hour?.avg_utilization
        : point.seven_day?.avg_utilization
      return value !== undefined && Number.isFinite(value)
        ? [{ timestamp: point.timestamp, value }]
        : []
    })
    if (data.length === 0) continue
    const values = data.map((point) => point.value)
    const minimum = Math.min(...values)
    const maximum = Math.max(...values)
    const range = maximum - minimum || 1
    const points = values.length < 2
      ? null
      : values.map((value, index) => {
          const x = (index / (values.length - 1)) * 180
          const y = 40 - ((value - minimum) / range) * 36
          return `${x.toFixed(1)},${y.toFixed(1)}`
        }).join(' ')
    summaries.set(bucketKey, {
      points,
      window,
      count: data.length,
      startTimestamp: data[0].timestamp,
      endTimestamp: data[data.length - 1].timestamp,
      latest: data[data.length - 1].value,
      minimum,
      maximum,
    })
  }
  return summaries
})

function observeTrendCards(): void {
  void nextTick(() => {
    if (!trendObserver || !gridRef.value) return
    for (const card of gridRef.value.querySelectorAll<HTMLElement>('[data-bucket-key]')) {
      if (observedCards.has(card)) continue
      observedCards.add(card)
      trendObserver.observe(card)
    }
    for (const card of observedCards) {
      if (gridRef.value.contains(card)) continue
      trendObserver.unobserve(card)
      observedCards.delete(card)
    }
  })
}

onMounted(() => {
  if (typeof IntersectionObserver === 'undefined') return
  trendObserver = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting || !observedCards.has(entry.target)) continue
      const card = entry.target as HTMLElement
      const bucketKey = card.dataset.bucketKey
      trendObserver?.unobserve(card)
      observedCards.delete(card)
      if (bucketKey) emit('requestTrend', bucketKey)
    }
  }, { rootMargin: '200px 0px', threshold: 0.01 })
  observeTrendCards()
})

watch(
  () => props.buckets?.map((bucket) => bucket.bucket_key).join('\u0000') ?? '',
  observeTrendCards,
)

onBeforeUnmount(() => {
  trendObserver?.disconnect()
  trendObserver = null
  observedCards.clear()
})

function utilizationLevel(value: number): 'low' | 'moderate' | 'high' | 'critical' {
  if (value < 40) return 'low'
  if (value < 60) return 'moderate'
  if (value < 80) return 'high'
  return 'critical'
}

function primaryWindowStats(bucket: BucketSnapshotDTO): WindowStatsDTO | null {
  return bucket.five_hour ?? bucket.seven_day
}

function primaryWindowUtilization(bucket: BucketSnapshotDTO): number {
  return primaryWindowStats(bucket)?.avg_utilization ?? 0
}

function primaryWindowLimit(bucket: BucketSnapshotDTO): number | null {
  return primaryWindowStats(bucket)?.inferred_limit_usd ?? null
}

function primaryWindowSampleSize(bucket: BucketSnapshotDTO): number {
  return primaryWindowStats(bucket)?.sample_size ?? 0
}

function primaryWindowStdev(bucket: BucketSnapshotDTO): number | null {
  return primaryWindowStats(bucket)?.inferred_stdev ?? null
}

function primaryWindowInferenceReason(bucket: BucketSnapshotDTO): InferenceRejectReason | undefined {
  return primaryWindowStats(bucket)?.inference_reject_reason
}

function primaryWindowLabel(bucket: BucketSnapshotDTO): string {
  return bucket.five_hour
    ? t('radar.quota.fiveHourUtilization', '5-hour utilization')
    : t('radar.quota.sevenDayUtilization', '7-day utilization')
}

function trendWindowLabel(window: '5h' | '7d'): string {
  return window === '5h'
    ? t('radar.quota.fiveHourUtilization', '5-hour utilization')
    : t('radar.quota.sevenDayUtilization', '7-day utilization')
}

function trendTitle(bucket: BucketSnapshotDTO): string {
  const window = trendSummary(bucket.bucket_key)?.window ?? (bucket.five_hour ? '5h' : '7d')
  return window === '5h'
    ? t('radar.quota.sevenDayTrend', '7-day 5-hour utilization trend')
    : t('radar.quota.sevenDayQuotaTrend', '7-day quota utilization trend')
}

function utilizationClass(value: number): string {
  switch (utilizationLevel(value)) {
    case 'low': return 'bg-emerald-500'
    case 'moderate': return 'bg-yellow-500'
    case 'high': return 'bg-orange-500'
    case 'critical': return 'bg-red-500'
  }
}

function clampedPercent(value: number): number {
  return Math.min(100, Math.max(0, value))
}

function inferenceMessage(reason?: InferenceRejectReason): string {
  switch (reason) {
    case 'insufficient_samples': return t('radar.quota.inference.insufficient', 'Insufficient samples')
    case 'high_dispersion': return t('radar.quota.inference.dispersed', 'Data is too dispersed')
    case 'invalid_mean': return t('radar.quota.inference.invalid', 'No trusted result')
    default: return t('radar.quota.inference.unavailable', 'No trusted result')
  }
}

function trendSummary(bucketKey: string): SparklineSummary | null {
  return trendSummaries.value.get(bucketKey) ?? null
}

function platformLabel(platform: RadarPlatform): string {
  switch (platform) {
    case 'anthropic': return 'Anthropic'
    case 'openai': return 'OpenAI'
    case 'antigravity': return 'Antigravity'
  }
}

function formatNumber(value: number): string {
  return numberFormatter.value.format(value)
}

function formatPercent(value: number): string {
  return `${percentFormatter.value.format(value)}%`
}

function formatDateTime(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return t('radar.common.unknownTime', 'Unknown')
  return dateTimeFormatter.value.format(date)
}

function formatUSD(value: number): string {
  return usdFormatter.value.format(value)
}
</script>
