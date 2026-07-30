<template>
  <div class="flex min-h-screen flex-col bg-gray-50 dark:bg-dark-950">
    <RadarPageHeader />

    <div
      v-if="initialLoading"
      data-testid="radar-initial-loading"
      role="status"
      aria-live="polite"
      class="mx-auto flex w-full max-w-7xl flex-1 items-center justify-center gap-3 px-4 py-24 text-gray-600 dark:text-gray-300"
    >
      <Icon name="refresh" size="md" class="animate-spin motion-reduce:animate-none" aria-hidden="true" />
      {{ t('radar.state.loading', 'Loading radar data') }}
    </div>

    <div
      v-else-if="radar.allInitialFailed.value"
      data-testid="radar-all-failed"
      class="mx-auto w-full max-w-2xl flex-1 px-4 py-20 text-center"
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

      <main class="mx-auto w-full max-w-7xl flex-1 space-y-12 px-4 py-8 sm:px-6 lg:px-8">
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
              {{ t('radar.quota.subtitle', 'API-equivalent value estimates with sample sizes for each available quota window.') }}
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
              @select="openQuotaDetails"
            />
          </RadarSectionState>
        </section>

        <section id="degradation" class="scroll-mt-44 sm:scroll-mt-32" aria-labelledby="radar-degradation-heading">
          <div class="mb-5">
            <h2 id="radar-degradation-heading" class="text-2xl font-bold text-gray-950 dark:text-white">
              {{ t('radar.degradation.title', 'Benchmark radar') }}
            </h2>
            <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
              {{ t('radar.degradation.subtitle', 'Current Artificial Analysis indices and model leaderboard rankings.') }}
            </p>
          </div>
          <DegradationRadarTabs
            :latest="degradationData"
            :latest-loading="radar.degradationLatest.loading.value"
            :latest-error="radar.degradationLatest.error.value"
            :lmarena="lmarenaData"
            :lmarena-loading="lmarenaLoading"
            :lmarena-error="lmarenaError"
          />
        </section>
      </main>
    </template>

    <QuotaBucketDetailModal
      :show="selectedQuotaBucket !== null"
      :bucket="selectedQuotaBucket"
      :trend="selectedQuotaTrend"
      :trend-loading="selectedQuotaTrendLoading"
      :trend-error="selectedQuotaTrendError"
      :sample-size-warn-below="quotaData?.sample_size_warn_below"
      @close="selectedQuotaBucket = null"
    />

    <DataSourceFooter
      v-if="!radar.allInitialFailed.value"
      :sources="sourcesData ?? []"
      :loading="radar.sources.loading.value"
    />

    <footer
      data-testid="radar-footer"
      class="border-t border-gray-200/50 px-6 py-8 dark:border-dark-800/50"
    >
      <div
        class="mx-auto flex max-w-6xl flex-col items-center justify-center gap-4 text-center sm:flex-row sm:text-left"
      >
        <p class="text-sm text-gray-500 dark:text-dark-400">
          &copy; {{ currentYear }} {{ siteName }}.{{ t('home.footer.allRightsReserved') }}
        </p>
      </div>
    </footer>

  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import DegradationRadarTabs from '@/components/radar/DegradationRadarTabs.vue'
import DataSourceFooter from '@/components/radar/DataSourceFooter.vue'
import QuotaBucketDetailModal from '@/components/radar/QuotaBucketDetailModal.vue'
import QuotaBucketGrid from '@/components/radar/QuotaBucketGrid.vue'
import RadarHero from '@/components/radar/RadarHero.vue'
import RadarPageHeader from '@/components/radar/RadarPageHeader.vue'
import RadarSectionState from '@/components/radar/RadarSectionState.vue'
import ServiceHealthGrid from '@/components/radar/ServiceHealthGrid.vue'
import { useAppStore } from '@/stores'
import userChannelsAPI, { type UserAvailableChannel } from '@/api/channels'
import {
  usePublicRadar,
} from '@/composables/usePublicRadar'
import type { BucketSnapshotDTO } from '@/types/radar'
import { radarCatalogPlatforms } from '@/utils/radarCatalog'

const { t } = useI18n()
const appStore = useAppStore()
const radar = usePublicRadar()
const catalogChannels = shallowRef<UserAvailableChannel[]>([])
const catalogLoading = ref(true)
const catalogError = ref<'load_failed' | null>(null)
const selectedQuotaBucket = ref<BucketSnapshotDTO | null>(null)
let catalogController: AbortController | null = null

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const currentYear = computed(() => new Date().getFullYear())
const healthData = computed(() => radar.health.data.value)
const quotaData = computed(() => radar.quotaLatest.data.value)
const selectedQuotaTrendState = computed(() => (
  selectedQuotaBucket.value
    ? radar.getQuotaTrendState(selectedQuotaBucket.value.bucket_key, 7)
    : null
))
const selectedQuotaTrend = computed(() => selectedQuotaTrendState.value?.data.value ?? null)
const selectedQuotaTrendLoading = computed(() => selectedQuotaTrendState.value?.loading.value ?? false)
const selectedQuotaTrendError = computed(() => selectedQuotaTrendState.value?.error.value ?? null)
const degradationData = computed(() => radar.degradationLatest.data.value)
const lmarenaData = computed(() => radar.lmarena.data.value)
const sourcesData = computed(() => radar.sources.data.value)
const healthSourcePlatforms = computed(() => {
  const byPlatform = new Map<string, number>()
  for (const source of sourcesData.value ?? []) {
    const platform = source.platform?.trim().toLowerCase()
    if (!platform || source.platform_order === null) continue
    const current = byPlatform.get(platform)
    if (current === undefined || source.platform_order < current) {
      byPlatform.set(platform, source.platform_order)
    }
  }
  return [...byPlatform.entries()]
    .sort(([leftPlatform, leftOrder], [rightPlatform, rightOrder]) => (
      leftOrder - rightOrder || leftPlatform.localeCompare(rightPlatform)
    ))
    .map(([platform]) => platform)
})
const catalogPlatforms = computed(() => {
  const available = new Set(radarCatalogPlatforms(catalogChannels.value))
  return healthSourcePlatforms.value.filter((platform) => available.has(platform))
})
const responseHealthPlatforms = computed(() => {
  const byPlatform = new Map<string, number>()
  for (const service of healthData.value ?? []) {
    const platform = service.platform.trim().toLowerCase()
    if (!platform) continue
    const current = byPlatform.get(platform)
    if (current === undefined || service.platform_order < current) {
      byPlatform.set(platform, service.platform_order)
    }
  }
  return [...byPlatform.entries()]
    .sort(([leftPlatform, leftOrder], [rightPlatform, rightOrder]) => (
      leftOrder - rightOrder || leftPlatform.localeCompare(rightPlatform)
    ))
    .map(([platform]) => platform)
})
const healthPlatforms = computed(() => (
  !catalogLoading.value && catalogError.value === null && healthSourcePlatforms.value.length > 0
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
      'No publishable quota data. Supported plan buckets require recent passive quota snapshots and their configured minimum sample.'
    )
  }
  return t(
    'radar.quota.emptyPending',
    'No quota data yet. Aggregation runs after service startup; try again shortly.'
  )
})

function openQuotaDetails(bucket: BucketSnapshotDTO): void {
  selectedQuotaBucket.value = bucket
  void radar.loadQuotaTrend(bucket.bucket_key, 7).catch(() => undefined)
}

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
