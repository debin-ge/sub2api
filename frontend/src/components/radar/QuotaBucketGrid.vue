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

      <div class="mt-5 grid grid-cols-2 gap-3">
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
                {{ t('radar.quota.inferredLimit', 'Quota limit') }}
              </dt>
              <dd class="mt-0.5 text-lg font-bold tabular-nums text-gray-950 dark:text-white">
                {{ formatLimit(window.stats) }}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('radar.quota.sampleSize', 'Sample size') }}
              </dt>
              <dd class="mt-0.5 font-semibold tabular-nums text-gray-900 dark:text-gray-100">
                {{ formatSampleSize(window.stats) }}
              </dd>
            </div>
          </dl>
        </section>
      </div>
    </article>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { BucketSnapshotDTO, RadarPlatform, WindowStatsDTO } from '@/types/radar'

withDefaults(defineProps<{
  buckets?: readonly BucketSnapshotDTO[] | null
}>(), {
  buckets: () => [],
})

const { t, locale } = useI18n()
const numberFormatter = computed(() => new Intl.NumberFormat(locale.value))
const usdFormatter = computed(() => new Intl.NumberFormat(locale.value, {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 0,
  maximumFractionDigits: 2,
}))

function quotaWindows(bucket: BucketSnapshotDTO) {
  return [
    { key: '5h', label: '5H', stats: bucket.five_hour },
    { key: '7d', label: '7D', stats: bucket.seven_day },
  ] as const
}

function platformLabel(platform: RadarPlatform): string {
  switch (platform) {
    case 'anthropic': return 'Anthropic'
    case 'openai': return 'OpenAI'
    case 'antigravity': return 'Antigravity'
  }
}

function formatLimit(stats: WindowStatsDTO | null): string {
  if (!stats || stats.inferred_limit_usd === null) return '—'
  return usdFormatter.value.format(stats.inferred_limit_usd)
}

function formatSampleSize(stats: WindowStatsDTO | null): string {
  return stats === null ? '—' : numberFormatter.value.format(stats.sample_size)
}
</script>
