<template>
  <div class="flex min-h-screen flex-col bg-gray-50 dark:bg-dark-950">
    <RadarPageHeader />

    <div
      v-if="initialLoading"
      data-testid="radar-initial-loading"
      role="status"
      aria-live="polite"
      class="flex flex-1 flex-col items-center justify-center gap-3 px-4 py-24 text-gray-500 dark:text-gray-400"
    >
      <Icon name="refresh" size="md" class="animate-spin motion-reduce:animate-none" aria-hidden="true" />
      {{ t('radar.state.loading', 'Loading radar data') }}
    </div>

    <div
      v-else-if="radar.allInitialFailed.value"
      data-testid="radar-all-failed"
      class="flex flex-1 flex-col items-center justify-center gap-3 px-4 py-24 text-center"
    >
      <h1 class="text-xl font-semibold text-gray-900 dark:text-white">
        {{ t('radar.error.title', 'Unable to load radar data') }}
      </h1>
      <p class="max-w-md text-sm text-gray-500 dark:text-gray-400">
        {{
          t(
            'radar.error.safeReason',
            'The public data sources are temporarily unavailable. Please try again.',
          )
        }}
      </p>
    </div>

    <template v-else>
      <RadarHero :last-fetched-at="radar.lastFetchedAt.value" />

      <!-- Stats bar -->
      <div class="border-t border-white/10 bg-dark-900/80 backdrop-blur">
        <div class="mx-auto grid max-w-7xl grid-cols-2 gap-6 px-4 py-8 sm:px-6 lg:grid-cols-4 lg:px-8">
          <div
            v-for="stat in statsBar"
            :key="stat.value"
            class="flex flex-col items-center gap-1 text-center lg:items-start lg:text-left"
          >
            <p class="text-2xl font-bold text-white sm:text-3xl">{{ stat.value }}</p>
            <p class="text-xs text-gray-400 sm:text-sm">{{ stat.label }}</p>
          </div>
        </div>
      </div>

      <main class="mx-auto w-full max-w-7xl flex-1 space-y-20 px-4 py-16 sm:px-6 lg:px-8">
        <!-- Feature grid -->
        <section aria-labelledby="radar-features-heading">
          <div class="mx-auto max-w-2xl text-center">
            <h2 id="radar-features-heading" class="text-2xl font-bold text-gray-950 dark:text-white sm:text-3xl">
              {{ t('radar.home.features.title', { siteName }) }}
            </h2>
            <p class="mt-3 text-sm text-gray-500 dark:text-gray-400 sm:text-base">
              {{
                t(
                  'radar.home.features.subtitle',
                  'Everything you need to run AI workloads on a reliable, unified gateway.',
                )
              }}
            </p>
          </div>
          <div class="mt-10 grid gap-5 sm:grid-cols-2 lg:grid-cols-4">
            <div
              v-for="feature in featureCards"
              :key="feature.title"
              class="rounded-2xl border border-gray-200 bg-white p-5 transition-shadow hover:shadow-glow dark:border-dark-800 dark:bg-dark-800"
            >
              <span
                class="flex h-10 w-10 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-950/50 dark:text-primary-400"
              >
                <Icon :name="feature.icon" size="sm" />
              </span>
              <h3 class="mt-4 text-sm font-semibold text-gray-950 dark:text-white">{{ feature.title }}</h3>
              <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-gray-400">{{ feature.description }}</p>
            </div>
          </div>
        </section>

        <section id="health" class="scroll-mt-44 sm:scroll-mt-32" aria-labelledby="radar-health-heading">
          <h2 id="radar-health-heading" class="text-xl font-bold text-gray-950 dark:text-white sm:text-2xl">
            {{ t('radar.health.title', 'Service health') }}
          </h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('radar.health.subtitle', 'Current service status for added model platforms and vendors.') }}
          </p>
          <div class="mt-6">
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
          </div>
        </section>

        <section id="degradation" class="scroll-mt-44 sm:scroll-mt-32" aria-labelledby="radar-degradation-heading">
          <h2 id="radar-degradation-heading" class="text-xl font-bold text-gray-950 dark:text-white sm:text-2xl">
            {{ t('radar.degradation.title', 'Benchmark radar') }}
          </h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{
              t(
                'radar.degradation.subtitle',
                'Current Artificial Analysis indices intersected with Model Plaza, plus model leaderboard rankings.',
              )
            }}
          </p>
          <div class="mt-6">
            <DegradationRadarTabs
              :latest="degradationData"
              :latest-loading="radar.degradationLatest.loading.value"
              :latest-error="radar.degradationLatest.error.value"
              :lmarena="lmarenaData"
              :lmarena-loading="lmarenaLoading"
              :lmarena-error="lmarenaError"
            />
          </div>
        </section>

        <!-- Protocol docs preview -->
        <section id="docs" class="scroll-mt-32" aria-labelledby="radar-docs-heading">
          <div class="grid items-center gap-10 lg:grid-cols-[1fr_1.15fr]">
            <div>
              <h2 id="radar-docs-heading" class="text-2xl font-bold text-gray-950 dark:text-white sm:text-3xl">
                {{ t('radar.home.docs.title', 'One line of code, endless models') }}
              </h2>
              <p class="mt-3 text-sm leading-6 text-gray-500 dark:text-gray-400 sm:text-base">
                {{
                  t(
                    'radar.home.docs.description',
                    'Point your existing SDK at our endpoint and start calling any connected model in minutes.',
                  )
                }}
              </p>
              <ul class="mt-6 space-y-3">
                <li v-for="item in docsChecklist" :key="item" class="flex items-start gap-2 text-sm text-gray-600 dark:text-gray-300">
                  <Icon name="check" size="xs" class="mt-0.5 shrink-0 text-primary-600 dark:text-primary-400" />
                  {{ item }}
                </li>
              </ul>
            </div>
            <RadarHomeDocsPreview />
          </div>
        </section>

        <!-- CTA banner -->
        <section id="pricing" aria-labelledby="radar-cta-heading">
          <div
            class="relative overflow-hidden rounded-3xl bg-gradient-to-r from-primary-600 to-accent-500 px-8 py-12 text-center sm:py-16"
          >
            <div class="pointer-events-none absolute -left-16 -top-16 h-56 w-56 rounded-full bg-white/10 blur-3xl"></div>
            <div class="pointer-events-none absolute -bottom-16 -right-16 h-56 w-56 rounded-full bg-white/10 blur-3xl"></div>
            <h2 id="radar-cta-heading" class="relative text-2xl font-bold text-white sm:text-3xl">
              {{ t('radar.home.cta.title', 'Ready to get started?') }}
            </h2>
            <p class="relative mx-auto mt-3 max-w-xl text-sm text-white/85 sm:text-base">
              {{ t('radar.home.cta.description', { siteName }) }}
            </p>
            <router-link
              to="/register"
              class="relative mt-8 inline-flex items-center justify-center rounded-xl bg-white px-6 py-3 text-sm font-semibold text-primary-700 shadow-lg transition hover:bg-gray-50"
            >
              {{ t('radar.home.cta.button', 'Sign up free') }}
            </router-link>
          </div>
        </section>
      </main>
    </template>

    <footer class="border-t border-gray-200/50 dark:border-dark-800/50">
      <div class="mx-auto max-w-7xl px-4 py-12 text-center sm:px-6 lg:px-8">
        <div class="flex items-center justify-center gap-2">
          <div class="flex h-8 w-8 items-center justify-center overflow-hidden rounded-lg">
            <img :src="siteLogo || '/logo.svg'" alt="" class="h-full w-full object-contain" />
          </div>
          <span class="text-base font-bold text-gray-950 dark:text-white">{{ siteName }}</span>
        </div>
        <p class="mx-auto mt-3 max-w-xs text-sm leading-6 text-gray-500 dark:text-gray-400">
          {{ t('radar.home.footer.tagline', 'A standardized API gateway that unifies access to leading AI models, powering your applications and digital assets.') }}
        </p>
      </div>

      <div class="border-t border-gray-200/50 px-6 py-8 dark:border-dark-800/50">
        <div class="mx-auto max-w-6xl text-center">
          <p data-testid="radar-footer" class="text-sm text-gray-500 dark:text-dark-400">
            &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
          </p>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import DegradationRadarTabs from '@/components/radar/DegradationRadarTabs.vue'
import RadarHero from '@/components/radar/RadarHero.vue'
import RadarHomeDocsPreview from '@/components/radar/RadarHomeDocsPreview.vue'
import RadarPageHeader from '@/components/radar/RadarPageHeader.vue'
import RadarSectionState from '@/components/radar/RadarSectionState.vue'
import ServiceHealthGrid from '@/components/radar/ServiceHealthGrid.vue'
import { useAppStore } from '@/stores'
import userChannelsAPI, { type UserAvailableChannel } from '@/api/channels'
import { usePublicRadar } from '@/composables/usePublicRadar'
import { radarCatalogPlatforms } from '@/utils/radarCatalog'
import { sanitizeUrl } from '@/utils/url'

const { t } = useI18n()
const appStore = useAppStore()
const radar = usePublicRadar()
const catalogChannels = shallowRef<UserAvailableChannel[]>([])
const catalogLoading = ref(true)
const catalogError = ref<'load_failed' | null>(null)
let catalogController: AbortController | null = null

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'ZenTok')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const currentYear = computed(() => new Date().getFullYear())

const healthData = computed(() => radar.health.data.value)
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

const healthPlatforms = computed(() =>
  !catalogLoading.value && catalogError.value === null && healthSourcePlatforms.value.length > 0
    ? catalogPlatforms.value
    : responseHealthPlatforms.value,
)

const initialLoading = computed(() => !radar.hasCompletedRefresh.value && !radar.hasAnySuccess.value)
const healthHasContent = computed(() => Boolean(healthData.value?.length && healthPlatforms.value.length))
const healthEmpty = computed(() => radar.health.hasSucceeded.value && !healthHasContent.value)
const healthLoading = computed(() => radar.health.loading.value)
const healthError = computed(() => radar.health.error.value)
const lmarenaLoading = computed(() => radar.lmarena.loading.value)
const lmarenaError = computed(() => radar.lmarena.error.value)

const statsBar = computed(() => [
  { value: t('radar.home.stats.platforms', '13+'), label: t('radar.home.stats.platformsLabel', 'Connected platforms') },
  { value: t('radar.home.stats.uptime', '99.9%'), label: t('radar.home.stats.uptimeLabel', 'Average uptime') },
  { value: t('radar.home.stats.monitoring', '24/7'), label: t('radar.home.stats.monitoringLabel', 'Continuous monitoring') },
  { value: t('radar.home.stats.latency', '<100ms'), label: t('radar.home.stats.latencyLabel', 'Average latency') },
])

const featureCards = computed(() => [
  {
    icon: 'link' as const,
    title: t('radar.home.features.unifiedApi.title', 'Unified API access'),
    description: t(
      'radar.home.features.unifiedApi.description',
      'One key calls 13+ leading model providers. Native compatibility with Chat Completions, Responses, and Messages — switch protocols with a one-line change.',
    ),
  },
  {
    icon: 'bolt' as const,
    title: t('radar.home.features.smartRouting.title', 'Smart routing & failover'),
    description: t(
      'radar.home.features.smartRouting.description',
      'Multi-node scheduling with millisecond-level health probes automatically routes around unhealthy nodes, so your service stays online.',
    ),
  },
  {
    icon: 'creditCard' as const,
    title: t('radar.home.features.billing.title', 'Unified billing & usage'),
    description: t(
      'radar.home.features.billing.description',
      'Unified metering and billing across every model, with pay-as-you-go or subscription pricing and full visibility into cost.',
    ),
  },
  {
    icon: 'trophy' as const,
    title: t('radar.home.features.benchmarks.title', 'Transparent benchmarks'),
    description: t(
      'radar.home.features.benchmarks.description',
      'Integrated Artificial Analysis and LMArena leaderboards help you pick the right model for every workload.',
    ),
  },
])

const docsChecklist = computed(() => [
  t('radar.home.docs.checklist.protocols', 'Compatible with Chat Completions, Responses, and Messages protocols'),
  t('radar.home.docs.checklist.quota', 'Fine-grained key usage and quota controls'),
  t('radar.home.docs.checklist.sdk', 'Ready to use with SDKs or cURL out of the box'),
])

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
