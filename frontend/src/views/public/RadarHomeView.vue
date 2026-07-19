<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <RadarPageHeader />

    <div
      v-if="initialLoading"
      data-testid="radar-initial-loading"
      role="status"
      aria-live="polite"
      class="mx-auto flex max-w-7xl items-center justify-center gap-3 px-4 py-24 text-gray-600 dark:text-gray-300"
    >
      <Icon name="refresh" size="md" class="animate-spin motion-reduce:animate-none" aria-hidden="true" />
      {{ t('radar.state.loading', 'Loading radar data') }}
    </div>

    <div
      v-else-if="radar.allInitialFailed.value"
      data-testid="radar-all-failed"
      class="mx-auto max-w-2xl px-4 py-20 text-center"
    >
      <h1 class="text-2xl font-bold text-gray-950 dark:text-white">
        {{ t('radar.error.title', 'Unable to load radar data') }}
      </h1>
      <p class="mt-3 text-sm text-gray-600 dark:text-gray-400">
        {{ t('radar.error.safeReason', 'The public data sources are temporarily unavailable. Please try again.') }}
      </p>
    </div>

    <template v-else>
      <RadarHero
        :last-fetched-at="radar.lastFetchedAt.value"
      />

      <main class="mx-auto max-w-7xl space-y-12 px-4 py-8 sm:px-6 lg:px-8">
        <section id="health" class="scroll-mt-44 sm:scroll-mt-32" aria-labelledby="radar-health-heading">
          <div class="mb-5">
            <h2 id="radar-health-heading" class="text-2xl font-bold text-gray-950 dark:text-white">
              {{ t('radar.health.title', 'Service health') }}
            </h2>
            <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
              {{ t('radar.health.subtitle', 'Current status for supported model services.') }}
            </p>
          </div>
          <RadarSectionState
            :loading="healthLoading"
            :error="healthError"
            :empty="healthEmpty"
            :has-content="healthHasContent"
          >
            <template #empty>
              {{ t('radar.health.empty', 'No added model platforms are currently available.') }}
            </template>
            <ServiceHealthGrid :services="healthData" :platforms="healthPlatforms" />
          </RadarSectionState>
        </section>

        <section id="quota" class="scroll-mt-44 sm:scroll-mt-32" aria-labelledby="radar-quota-heading">
          <div class="mb-5">
            <h2 id="radar-quota-heading" class="text-2xl font-bold text-gray-950 dark:text-white">
              {{ t('radar.quota.title', 'Quota radar') }}
            </h2>
            <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
              {{ t('radar.quota.subtitle', 'Anonymous quota aggregates by platform and plan.') }}
            </p>
          </div>
          <RadarSectionState
            :loading="radar.quotaLatest.loading.value"
            :error="radar.quotaLatest.error.value"
            :empty="quotaEmpty"
            :has-content="quotaHasContent"
          >
            <template #empty>
              {{ quotaEmptyMessage }}
            </template>
            <QuotaBucketGrid
              v-if="quotaData"
              :buckets="quotaData.buckets"
              :sample-size-warn-below="quotaData.sample_size_warn_below"
              :trends="quotaTrends"
              :trend-loading="quotaTrendLoading"
              :trend-errors="quotaTrendErrors"
              @select="openBucketDetail"
              @request-trend="ensureQuotaTrend"
            />
          </RadarSectionState>
        </section>

        <section id="degradation" class="scroll-mt-44 sm:scroll-mt-32" aria-labelledby="radar-degradation-heading">
          <div class="mb-5">
            <h2 id="radar-degradation-heading" class="text-2xl font-bold text-gray-950 dark:text-white">
              {{ t('radar.degradation.title', 'Benchmark radar') }}
            </h2>
            <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
              {{ t('radar.degradation.subtitle', 'Public model indices, trends, and model leaderboard rankings.') }}
            </p>
          </div>
          <DegradationRadarTabs
            :latest="degradationData"
            :latest-loading="radar.degradationLatest.loading.value"
            :latest-error="radar.degradationLatest.error.value"
            :lmarena="lmarenaData"
            :lmarena-loading="lmarenaLoading"
            :lmarena-error="lmarenaError"
            :trend="activeDegradationTrend"
            :trend-loading="activeDegradationTrendState?.loading.value ?? false"
            :trend-error="activeDegradationTrendState?.error.value ?? null"
            @request-trend="requestDegradationTrend"
          />
        </section>
      </main>
    </template>

    <QuotaBucketDetailModal
      :show="detailModalOpen"
      :bucket="selectedBucket"
      :trend="activeQuotaTrend"
      :trend-loading="activeQuotaTrendState?.loading.value ?? false"
      :trend-error="activeQuotaTrendState?.error.value ?? null"
      :sample-size-warn-below="quotaData?.sample_size_warn_below ?? 3"
      @close="closeBucketDetail"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowReactive, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import DegradationRadarTabs from '@/components/radar/DegradationRadarTabs.vue'
import QuotaBucketDetailModal from '@/components/radar/QuotaBucketDetailModal.vue'
import QuotaBucketGrid from '@/components/radar/QuotaBucketGrid.vue'
import RadarHero from '@/components/radar/RadarHero.vue'
import RadarPageHeader from '@/components/radar/RadarPageHeader.vue'
import RadarSectionState from '@/components/radar/RadarSectionState.vue'
import ServiceHealthGrid from '@/components/radar/ServiceHealthGrid.vue'
import userChannelsAPI, { type UserAvailableChannel } from '@/api/channels'
import {
  usePublicRadar,
  type RadarResourceState,
} from '@/composables/usePublicRadar'
import type {
  BucketSnapshotDTO,
  DegradationMetric,
  DegradationTrendDTO,
  QuotaTrendDTO,
  ServiceHealthDTO,
} from '@/types/radar'
import { radarCatalogPlatforms } from '@/utils/radarCatalog'

const { t } = useI18n()
const radar = usePublicRadar()
const detailModalOpen = ref(false)
const selectedBucket = shallowRef<BucketSnapshotDTO | null>(null)
const quotaTrendStates = shallowReactive(new Map<string, RadarResourceState<QuotaTrendDTO>>())
const activeDegradationTrendState = shallowRef<RadarResourceState<DegradationTrendDTO> | null>(null)
const catalogChannels = shallowRef<UserAvailableChannel[]>([])
const catalogLoading = ref(true)
const catalogError = ref<'load_failed' | null>(null)
let catalogController: AbortController | null = null

const supportedHealthPlatforms = new Set(['anthropic', 'openai', 'windsurf', 'deepseek', 'kimi', 'minimax'])
const healthPlatformOrder = ['anthropic', 'deepseek', 'kimi', 'minimax', 'openai', 'windsurf'] as const

const healthData = computed(() => radar.health.data.value)
const quotaData = computed(() => radar.quotaLatest.data.value)
const degradationData = computed(() => radar.degradationLatest.data.value)
const lmarenaData = computed(() => radar.lmarena.data.value)
const sourcesData = computed(() => radar.sources.data.value)
const catalogPlatforms = computed(() => radarCatalogPlatforms(catalogChannels.value)
  .filter((platform) => supportedHealthPlatforms.has(platform)))
const responseHealthPlatforms = computed(() => {
  const platforms = new Set((healthData.value ?? [])
    .map((service) => healthPlatformForService(service))
    .filter((platform): platform is string => platform !== null))
  return healthPlatformOrder.filter((platform) => platforms.has(platform))
})
const healthPlatforms = computed(() => (
  !catalogLoading.value && catalogError.value === null
    ? catalogPlatforms.value
    : responseHealthPlatforms.value
))

const initialLoading = computed(() => (
  !radar.hasCompletedRefresh.value && !radar.hasAnySuccess.value
))
const healthHasContent = computed(() => Boolean(healthData.value?.length && healthPlatforms.value.length))
const healthEmpty = computed(() => radar.health.hasSucceeded.value && !healthHasContent.value)
const healthLoading = computed(() => radar.health.loading.value)
const healthError = computed(() => radar.health.error.value)
const lmarenaLoading = computed(() => radar.lmarena.loading.value)
const lmarenaError = computed(() => radar.lmarena.error.value)
const quotaHasContent = computed(() => Boolean(quotaData.value?.buckets.length))
const quotaEmpty = computed(() => (
  radar.quotaLatest.hasSucceeded.value && quotaData.value?.buckets.length === 0
))
const quotaAggregatorSource = computed(() => (
  sourcesData.value?.find((item) => item.key === 'quota_aggregator') ?? null
))
const quotaEmptyMessage = computed(() => {
  const aggregator = quotaAggregatorSource.value
  if (aggregator?.state === 'failed') {
    return t(
      'radar.quota.emptyFailed',
      'Quota aggregation is temporarily unavailable. Please try again later.'
    )
  }
  if (aggregator?.state === 'not_configured') {
    return t('radar.quota.emptyDisabled', 'Quota aggregation is currently disabled.')
  }
  if (
    (aggregator?.state === 'healthy' && aggregator.last_success_at !== null)
    || quotaData.value?.last_aggregated_at !== null
  ) {
    return t(
      'radar.quota.emptyNoPublishable',
      'No publishable quota data. At least 2 supported subscription accounts on the same plan with recent passive quota snapshots are required.'
    )
  }
  return t(
    'radar.quota.emptyPending',
    'No quota data yet. Aggregation runs after service startup; try again shortly.'
  )
})

const activeQuotaTrendState = computed(() => (
  selectedBucket.value
    ? quotaTrendStates.get(selectedBucket.value.bucket_key) ?? null
    : null
))
const activeQuotaTrend = computed(() => activeQuotaTrendState.value?.data.value ?? null)
const activeDegradationTrend = computed(() => (
  activeDegradationTrendState.value?.data.value ?? null
))
const quotaTrends = computed<Readonly<Record<string, QuotaTrendDTO | null>>>(() => {
  const trends: Record<string, QuotaTrendDTO | null> = {}
  for (const [bucketKey, state] of quotaTrendStates) trends[bucketKey] = state.data.value
  return trends
})
const quotaTrendLoading = computed<Readonly<Record<string, boolean>>>(() => {
  const loading: Record<string, boolean> = {}
  for (const [bucketKey, state] of quotaTrendStates) loading[bucketKey] = state.loading.value
  return loading
})
const quotaTrendErrors = computed<Readonly<Record<string, string | null>>>(() => {
  const errors: Record<string, string | null> = {}
  for (const [bucketKey, state] of quotaTrendStates) errors[bucketKey] = state.error.value
  return errors
})

function openBucketDetail(bucket: BucketSnapshotDTO): void {
  selectedBucket.value = bucket
  detailModalOpen.value = true
  ensureQuotaTrend(bucket.bucket_key)
}

function closeBucketDetail(): void {
  detailModalOpen.value = false
}

function healthPlatformForService(service: ServiceHealthDTO): string | null {
  switch (service.service_key) {
    case 'claude_api':
    case 'claude_code':
      return 'anthropic'
    case 'openai_api':
    case 'codex_web':
      return 'openai'
    case 'windsurf':
    case 'deepseek':
    case 'kimi':
    case 'minimax':
      return service.service_key
    default:
      return null
  }
}

function requestDegradationTrend(
  model: string,
  metric: DegradationMetric,
  days: 90
): void {
  const state = radar.getDegradationTrendState(model, metric, days)
  activeDegradationTrendState.value = state
  void radar.loadDegradationTrend(model, metric, days).catch(() => undefined)
}

function ensureQuotaTrend(bucketKey: string): void {
  let state = quotaTrendStates.get(bucketKey)
  if (!state) {
    state = radar.getQuotaTrendState(bucketKey, 7)
    quotaTrendStates.set(bucketKey, state)
  }
  if (state.loading.value || (state.hasSucceeded.value && state.data.value !== null)) return
  void radar.loadQuotaTrend(bucketKey, 7).catch(() => undefined)
}

watch(quotaData, (latest) => {
  const buckets = latest?.buckets ?? []
  const bucketsByKey = new Map(buckets.map((bucket) => [bucket.bucket_key, bucket]))

  for (const bucketKey of quotaTrendStates.keys()) {
    if (!bucketsByKey.has(bucketKey)) quotaTrendStates.delete(bucketKey)
  }

  if (selectedBucket.value) {
    const refreshedBucket = bucketsByKey.get(selectedBucket.value.bucket_key)
    if (refreshedBucket) {
      selectedBucket.value = refreshedBucket
    } else {
      detailModalOpen.value = false
      selectedBucket.value = null
    }
  }

}, { immediate: true })

onMounted(() => {
  const controller = new AbortController()
  catalogController = controller
  void userChannelsAPI.getPublic({ signal: controller.signal })
    .then((channels) => {
      catalogChannels.value = channels
      catalogError.value = null
    })
    .catch(() => {
      if (!controller.signal.aborted) catalogError.value = 'load_failed'
    })
    .finally(() => {
      if (!controller.signal.aborted) catalogLoading.value = false
    })
  void radar.refresh()
})

onBeforeUnmount(() => {
  catalogController?.abort()
  catalogController = null
  radar.dispose()
})
</script>
