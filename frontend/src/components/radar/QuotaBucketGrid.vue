<template>
  <div
    v-if="!buckets || buckets.length === 0"
    class="rounded-2xl border border-dashed border-gray-300 p-10 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400"
  >
    {{ t('radar.quota.emptyNoPublishable', 'No publishable quota data. Supported plan buckets require recent passive quota snapshots and their configured minimum sample.') }}
  </div>

  <div v-else class="grid gap-5 md:grid-cols-2 xl:grid-cols-3">
    <article
      v-for="bucket in buckets"
      :key="bucket.bucket_key"
      :data-bucket-key="bucket.bucket_key"
      class="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-800 dark:bg-dark-900"
    >
      <p class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
        {{ platformLabel(bucket.platform) }}
      </p>
      <h3 class="mt-1 truncate font-semibold text-gray-950 dark:text-white">
        {{ bucket.display_name }}
      </h3>

      <div
        class="mt-5 grid gap-3"
        style="grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr))"
      >
        <section
          v-for="window in quotaWindows(bucket)"
          :key="window.label"
          :data-testid="`quota-window-${bucket.bucket_key}-${window.key}`"
          class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800/70"
        >
          <h4 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
            {{ window.label }}
          </h4>
          <dl class="mt-3 space-y-3">
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('radar.quota.inferredLimit', 'Estimated API-equivalent value') }}
              </dt>
              <dd class="mt-0.5 text-lg font-bold tabular-nums text-gray-950 dark:text-white">
                {{ formatLimit(window.stats, window.currency) }}
                <span
                  v-if="window.stats?.inference_confidence === 'low'"
                  class="mt-1 block text-xs font-medium text-amber-700 dark:text-amber-300"
                >
                  {{ t('radar.quota.singleSampleLowConfidence', 'Single-sample estimate · Low confidence') }}
                </span>
                <span
                  v-else-if="window.stats?.inference_reject_reason === 'unknown_plan'"
                  class="mt-1 block text-xs font-medium text-gray-500 dark:text-gray-400"
                >
                  {{ t('radar.quota.inference.unknownPlan', 'Plan is unknown; estimation is unavailable') }}
                </span>
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('radar.quota.sampleSize', 'Sample size') }}
              </dt>
              <dd class="mt-0.5 font-semibold tabular-nums text-gray-900 dark:text-gray-100">
                {{ formatSampleSize(window.stats) }}
                <span
                  v-if="isSmallSample(window.stats)"
                  data-testid="quota-small-sample"
                  class="mt-1 block text-xs font-medium text-amber-700 dark:text-amber-300"
                >
                  {{ t('radar.quota.smallSample', 'Small sample') }}
                </span>
              </dd>
            </div>
          </dl>
        </section>
      </div>
      <button
        type="button"
        data-testid="quota-view-details"
        class="mt-4 inline-flex w-full items-center justify-center rounded-lg border border-gray-200 px-3 py-2 text-sm font-semibold text-gray-700 transition hover:border-primary-300 hover:text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-700 dark:text-gray-200 dark:hover:border-primary-700 dark:hover:text-primary-300"
        @click="emit('select', bucket)"
      >
        {{ t('radar.quota.openDetails', 'View details') }}
      </button>
    </article>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { BucketSnapshotDTO, WindowStatsDTO } from '@/types/radar'
import { platformLabel } from '@/utils/platformColors'
import { quotaWindowsForBucket } from '@/utils/radarQuotaWindows'

const props = withDefaults(defineProps<{
  buckets?: readonly BucketSnapshotDTO[] | null
  sampleSizeWarnBelow?: number
}>(), {
  buckets: () => [],
  sampleSizeWarnBelow: 3,
})

const { t, locale } = useI18n()
const emit = defineEmits<{
  select: [bucket: BucketSnapshotDTO]
}>()
const numberFormatter = computed(() => new Intl.NumberFormat(locale.value))

function quotaWindows(bucket: BucketSnapshotDTO) {
  return quotaWindowsForBucket(bucket)
}

function formatLimit(stats: WindowStatsDTO | null, currency: string): string {
  if (!stats || stats.inferred_limit_usd === null) return '—'
  try {
    return new Intl.NumberFormat(locale.value, {
      style: 'currency',
      currency,
      minimumFractionDigits: 0,
      maximumFractionDigits: 2,
    }).format(stats.inferred_limit_usd)
  } catch {
    return numberFormatter.value.format(stats.inferred_limit_usd)
  }
}

function formatSampleSize(stats: WindowStatsDTO | null): string {
  return stats === null ? '—' : numberFormatter.value.format(stats.sample_size)
}

function isSmallSample(stats: WindowStatsDTO | null): boolean {
  return Boolean(
    stats
    && stats.inference_confidence !== 'low'
    && stats.sample_size < (props.sampleSizeWarnBelow ?? 3)
  )
}
</script>
