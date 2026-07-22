<template>
  <div class="rounded-2xl border border-gray-200 bg-white shadow-sm dark:border-dark-800 dark:bg-dark-900">
    <div class="border-b border-gray-200 px-4 pt-4 dark:border-dark-800 sm:px-6">
      <div
        role="tablist"
        :aria-label="t('radar.degradation.tabs', 'Benchmark views')"
        class="flex gap-2 overflow-x-auto"
      >
        <button
          v-for="tab in tabs"
          :id="`degradation-tab-${tab.key}`"
          :key="tab.key"
          type="button"
          role="tab"
          :data-tab="tab.key"
          :aria-selected="activeTab === tab.key"
          :aria-controls="`degradation-panel-${tab.key}`"
          :tabindex="activeTab === tab.key ? 0 : -1"
          class="-mb-px shrink-0 border-b-2 px-3 py-3 text-sm font-semibold focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          :class="activeTab === tab.key
            ? 'border-primary-500 text-primary-600 dark:text-primary-400'
            : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'"
          @click="activateTab(tab.key)"
          @keydown="handleTabKeydown"
        >
          {{ tab.label }}
        </button>
      </div>
    </div>

    <section
      v-if="activeTab === 'overview'"
      id="degradation-panel-overview"
      role="tabpanel"
      aria-labelledby="degradation-tab-overview"
      class="space-y-6 p-4 sm:p-6"
    >
      <div class="flex flex-wrap items-center gap-3 text-sm" aria-live="polite">
        <p
          v-if="latestLoading"
          data-testid="degradation-latest-loading"
          role="status"
          class="inline-flex items-center gap-2 text-gray-600 dark:text-gray-300"
        >
          <Icon name="refresh" size="sm" class="animate-spin motion-reduce:animate-none" aria-hidden="true" />
          {{ t('radar.degradation.loading', 'Loading benchmark data') }}
        </p>
        <p
          v-if="latestError"
          data-testid="degradation-latest-error"
          role="status"
          class="inline-flex items-center gap-2 text-red-700 dark:text-red-300"
        >
          <Icon name="exclamationCircle" size="sm" aria-hidden="true" />
          {{ t('radar.degradation.error', 'Unable to load benchmark data') }}
        </p>
        <span
          v-if="latest?.stale"
          data-testid="degradation-stale"
          class="inline-flex rounded-full bg-amber-100 px-2.5 py-1 text-xs font-semibold text-amber-800 dark:bg-amber-950/50 dark:text-amber-200"
        >
          {{ t('radar.degradation.stale', 'Data may be outdated') }}
        </span>
      </div>

      <div
        v-if="latest"
        class="grid gap-3 rounded-xl border border-gray-200 bg-gray-50 p-4 text-sm dark:border-dark-800 dark:bg-dark-800/40 sm:grid-cols-3"
      >
        <div>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('radar.degradation.indexVersion', 'Intelligence Index version') }}
          </p>
          <p data-testid="aa-index-version" class="mt-1 font-semibold text-gray-900 dark:text-white">
            {{ latest.intelligence_index_version ?? '—' }}
          </p>
        </div>
        <div>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('radar.degradation.projectFetchedAt', 'Project fetched at') }}
          </p>
          <p data-testid="aa-fetched-at" class="mt-1 font-semibold text-gray-900 dark:text-white">
            {{ aaFetchedAt ? formatDate(aaFetchedAt) : '—' }}
          </p>
        </div>
        <div class="sm:text-right">
          <a
            href="https://artificialanalysis.ai"
            target="_blank"
            rel="noopener noreferrer"
            class="font-semibold text-primary-600 hover:underline dark:text-primary-400"
          >
            {{ t('radar.degradation.officialSource', 'Artificial Analysis official source') }}
          </a>
        </div>
      </div>

      <dl class="grid gap-3 sm:grid-cols-3">
        <div
          v-for="metric in metrics"
          :key="metric.key"
          class="rounded-xl border border-gray-200 p-3 dark:border-dark-800"
        >
          <dt class="text-sm font-semibold text-gray-900 dark:text-white">{{ metric.label }}</dt>
          <dd class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ metric.description }}</dd>
        </div>
      </dl>

      <div v-if="allModels.length > 0" class="space-y-6">
        <section class="rounded-xl border border-gray-200 p-4 dark:border-dark-800" aria-labelledby="aa-model-selector-heading">
          <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
            <label class="block min-w-0 flex-1" for="aa-model-search">
              <span id="aa-model-selector-heading" class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ t('radar.degradation.selectModels', 'Select models') }}
              </span>
              <span class="ml-2 text-xs text-gray-500 dark:text-gray-400">
                {{ selectedModelSlugs.length }}/{{ maxSelectedModels }}
              </span>
              <input
                id="aa-model-search"
                v-model="modelSearch"
                data-testid="model-search"
                type="search"
                :placeholder="t('radar.degradation.searchModels', 'Search AA or Model Plaza models')"
                class="mt-2 block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 placeholder:text-gray-400 focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500 dark:border-dark-700 dark:bg-dark-800 dark:text-white"
              />
            </label>
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('radar.degradation.selectionLimit', 'Compare up to 10 models') }}
            </p>
          </div>

          <div data-testid="selected-models" class="mt-3 flex flex-wrap gap-2">
            <span
              v-for="model in radarModels"
              :key="model.slug"
              class="inline-flex max-w-full items-center gap-1 rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-800 dark:bg-primary-950/40 dark:text-primary-200"
            >
              <span class="truncate">{{ model.name }}</span>
              <button
                type="button"
                :disabled="selectedModelSlugs.length <= 1"
                :aria-label="`${t('radar.degradation.removeModel', 'Remove model')}: ${model.name}`"
                class="rounded-full px-1 hover:bg-primary-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:bg-primary-900/60"
                @click="removeModel(model.slug)"
              >
                ×
              </button>
            </span>
          </div>

          <ul
            data-testid="model-options"
            class="mt-3 max-h-64 space-y-1 overflow-y-auto rounded-lg border border-gray-200 p-1 dark:border-dark-700"
          >
            <li v-for="model in filteredModels" :key="model.slug">
              <label class="flex cursor-pointer items-start gap-3 rounded-md px-3 py-2 hover:bg-gray-50 dark:hover:bg-dark-800/70">
                <input
                  type="checkbox"
                  :value="model.slug"
                  :checked="selectedModelSlugs.includes(model.slug)"
                  :disabled="modelOptionDisabled(model.slug)"
                  class="mt-1 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
                  @change="toggleModel(model.slug)"
                />
                <span class="min-w-0">
                  <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ model.name }}</span>
                  <span class="block break-all text-xs text-gray-500 dark:text-gray-400">{{ model.slug }}</span>
                  <span v-if="catalogMatches(model).length" class="mt-1 block text-xs text-gray-500 dark:text-gray-400">
                    {{ catalogMatchSummary(model) }}
                  </span>
                </span>
              </label>
            </li>
            <li v-if="filteredModels.length === 0" class="px-3 py-6 text-center text-sm text-gray-500 dark:text-gray-400">
              {{ t('radar.degradation.noSearchResults', 'No matching models') }}
            </li>
          </ul>
        </section>

        <div class="grid gap-8 xl:grid-cols-[minmax(0,1.25fr)_minmax(18rem,0.75fr)]">
          <div class="min-w-0">
            <div
              class="h-[26rem]"
              role="img"
              :aria-label="t('radar.degradation.radarLabel', 'Model benchmark comparison radar chart')"
            >
              <Radar :data="radarData" :options="radarOptions" />
            </div>
          </div>

          <div class="space-y-3">
            <article
              v-for="model in radarModels"
              :key="model.slug"
              class="rounded-xl border border-gray-200 p-4 dark:border-dark-800"
            >
              <h3 class="font-semibold text-gray-950 dark:text-white">{{ model.name }}</h3>
              <p class="break-all text-xs text-gray-500 dark:text-gray-400">
                {{ model.vendor || '—' }} · {{ model.slug }}
              </p>
              <dl class="mt-3 grid grid-cols-3 gap-2 text-center text-xs">
                <div
                  v-for="metric in metrics"
                  :key="metric.key"
                  class="rounded-lg bg-gray-50 p-2 dark:bg-dark-800/70"
                >
                  <dt class="truncate text-gray-500 dark:text-gray-400">{{ metric.shortLabel }}</dt>
                  <dd class="mt-1 font-semibold text-gray-900 dark:text-white">
                    {{ metricValue(model, metric.key) ?? '—' }}
                  </dd>
                </div>
              </dl>
              <div v-if="catalogMatches(model).length" class="mt-3 flex flex-wrap gap-1.5">
                <span
                  v-for="match in catalogMatches(model)"
                  :key="`${match.platform}:${match.model_id}`"
                  class="max-w-full break-all rounded-md bg-gray-100 px-2 py-1 text-[11px] text-gray-600 dark:bg-dark-800 dark:text-gray-300"
                >
                  {{ match.platform }} / {{ match.model_id }}
                </span>
              </div>
            </article>
          </div>
        </div>
      </div>
      <p
        v-else-if="!latestLoading && !latestError"
        class="rounded-xl border border-dashed border-gray-300 p-10 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400"
      >
        {{ t('radar.degradation.emptyIntersection', 'No complete Artificial Analysis models currently match the Model Plaza catalog.') }}
      </p>
    </section>

    <section
      v-else
      id="degradation-panel-lmarena"
      role="tabpanel"
      aria-labelledby="degradation-tab-lmarena"
      class="p-4 sm:p-6"
    >
      <div class="mb-4 flex flex-wrap items-center gap-3 text-sm" aria-live="polite">
        <p
          v-if="lmarenaLoading"
          data-testid="lmarena-loading"
          role="status"
          class="inline-flex items-center gap-2 text-gray-600 dark:text-gray-300"
        >
          <Icon name="refresh" size="sm" class="animate-spin motion-reduce:animate-none" aria-hidden="true" />
          {{ t('radar.lmarena.loading', 'Loading model leaderboard') }}
        </p>
        <p
          v-if="lmarenaError"
          data-testid="lmarena-error"
          role="status"
          class="inline-flex items-center gap-2 text-red-700 dark:text-red-300"
        >
          <Icon name="exclamationCircle" size="sm" aria-hidden="true" />
          {{ t('radar.lmarena.error', 'Unable to load model leaderboard') }}
        </p>
      </div>
      <div class="mb-4 flex flex-wrap items-center justify-between gap-3 text-sm text-gray-500 dark:text-gray-400">
        <p>
          {{ t('radar.lmarena.totalVotes', 'Leaderboard model vote sum') }}:
          <strong class="font-semibold text-gray-900 dark:text-white">
            {{ lmarena?.total_votes === null || lmarena?.total_votes === undefined ? '—' : formatNumber(lmarena.total_votes) }}
          </strong>
        </p>
        <p data-testid="lmarena-fetched-at">
          {{ t('radar.lmarena.fetchedAt', 'Fetched at') }}:
          {{ lmarena?.fetched_at ? formatDate(lmarena.fetched_at) : '—' }}
        </p>
      </div>
      <div
        v-if="leaderboard.length > 0"
        data-testid="lmarena-scroll"
        class="overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-800"
      >
        <table class="w-full min-w-[46rem] divide-y divide-gray-200 text-sm dark:divide-dark-800">
          <thead class="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800/70 dark:text-gray-400">
            <tr>
              <th class="px-4 py-3">{{ t('radar.lmarena.rank', 'Rank') }}</th>
              <th class="px-4 py-3">{{ t('radar.lmarena.model', 'Model') }}</th>
              <th class="px-4 py-3">{{ t('radar.lmarena.vendor', 'Vendor') }}</th>
              <th class="px-4 py-3 text-right">{{ t('radar.lmarena.elo', 'Elo') }}</th>
              <th class="px-4 py-3 text-right">{{ t('radar.lmarena.confidence', 'Confidence interval') }}</th>
              <th class="px-4 py-3 text-right">{{ t('radar.lmarena.votes', 'Votes') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
            <tr v-for="(entry, index) in leaderboard" :key="`${entry.rank}-${entry.model}`">
              <td class="whitespace-nowrap px-4 py-3 font-semibold text-gray-900 dark:text-white">{{ formatNumber(index + 1) }}</td>
              <td class="whitespace-nowrap px-4 py-3 text-gray-900 dark:text-white">{{ entry.model }}</td>
              <td class="whitespace-nowrap px-4 py-3 text-gray-600 dark:text-gray-300">{{ entry.vendor || '—' }}</td>
              <td class="whitespace-nowrap px-4 py-3 text-right text-gray-600 dark:text-gray-300">{{ entry.elo === null ? '—' : formatNumber(entry.elo) }}</td>
              <td class="whitespace-nowrap px-4 py-3 text-right text-gray-600 dark:text-gray-300">{{ confidenceInterval(entry.ci_lower, entry.ci_upper) }}</td>
              <td class="whitespace-nowrap px-4 py-3 text-right text-gray-600 dark:text-gray-300">{{ entry.votes === null ? '—' : formatNumber(entry.votes) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p
        v-else-if="!lmarenaLoading && !lmarenaError"
        class="rounded-xl border border-dashed border-gray-300 p-10 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400"
      >
        {{ t('radar.lmarena.empty', 'No leaderboard models match the current model catalog.') }}
      </p>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  Chart as ChartJS,
  Legend,
  LineElement,
  PointElement,
  RadarController,
  RadialLinearScale,
  Tooltip,
} from 'chart.js'
import { Radar } from 'vue-chartjs'
import Icon from '@/components/icons/Icon.vue'
import type { DegradationLatestDTO, DegradationMetric, DegradationModelDTO, LMArenaDTO } from '@/types/radar'

ChartJS.register(Legend, LineElement, PointElement, RadarController, RadialLinearScale, Tooltip)

type DegradationTab = 'overview' | 'lmarena'

const maxSelectedModels = 10

const props = withDefaults(defineProps<{
  latest?: DegradationLatestDTO | null
  latestLoading?: boolean
  latestError?: string | null
  lmarena?: LMArenaDTO | null
  lmarenaLoading?: boolean
  lmarenaError?: string | null
}>(), {
  latest: null,
  latestLoading: false,
  latestError: null,
  lmarena: null,
  lmarenaLoading: false,
  lmarenaError: null,
})

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const activeTab = ref<DegradationTab>('overview')
const selectedModelSlugs = ref<string[]>([])
const modelSearch = ref('')
const isDark = ref(document.documentElement.classList.contains('dark'))
const selectionInitialized = ref(false)
let themeObserver: MutationObserver | null = null

const tabs = computed(() => [
  { key: 'overview' as const, label: t('radar.degradation.overview', 'Index overview') },
  { key: 'lmarena' as const, label: t('radar.degradation.lmarena', 'Model leaderboard') },
])
const metrics = computed(() => [
  {
    key: 'intelligence_index' as const,
    label: t('radar.degradation.intelligence', 'Intelligence index'),
    shortLabel: t('radar.degradation.intelligenceShort', 'Intelligence'),
    description: t('radar.degradation.intelligenceDescription', 'Composite performance across broad reasoning and knowledge evaluations.'),
  },
  {
    key: 'coding_index' as const,
    label: t('radar.degradation.coding', 'Coding index'),
    shortLabel: t('radar.degradation.codingShort', 'Coding'),
    description: t('radar.degradation.codingDescription', 'Performance on software-development and code-generation evaluations.'),
  },
  {
    key: 'agentic_index' as const,
    label: t('radar.degradation.agentic', 'Agentic index'),
    shortLabel: t('radar.degradation.agenticShort', 'Agentic'),
    description: t('radar.degradation.agenticDescription', 'Performance on multi-step agent and tool-use evaluations.'),
  },
])
const allModels = computed<DegradationModelDTO[]>(() => {
  if (Array.isArray(props.latest?.available_models)) return props.latest.available_models
  return props.latest?.models ?? []
})
const modelsBySlug = computed(() => new Map(allModels.value.map((model) => [model.slug, model])))
const defaultModelSlugs = computed(() => {
  const configured = Array.isArray(props.latest?.default_model_slugs)
    ? props.latest.default_model_slugs
    : props.latest?.models?.map((model) => model.slug) ?? []
  return sanitizeModelSlugs(configured).slice(0, 6)
})
const radarModels = computed(() => selectedModelSlugs.value
  .map((slug) => modelsBySlug.value.get(slug))
  .filter((model): model is DegradationModelDTO => model !== undefined))
const filteredModels = computed(() => {
  const query = modelSearch.value.trim().toLocaleLowerCase()
  if (!query) return allModels.value
  return allModels.value.filter((model) => modelSearchText(model).includes(query))
})
const leaderboard = computed(() => [...(props.lmarena?.leaderboard ?? [])].sort((left, right) => left.rank - right.rank))
const aaFetchedAt = computed(() => props.latest?.sources_last_updated?.aa ?? null)

const palette = [
  { border: '#2563eb', fill: 'rgba(37, 99, 235, 0.12)' },
  { border: '#9333ea', fill: 'rgba(147, 51, 234, 0.12)' },
  { border: '#059669', fill: 'rgba(5, 150, 105, 0.12)' },
  { border: '#ea580c', fill: 'rgba(234, 88, 12, 0.12)' },
  { border: '#db2777', fill: 'rgba(219, 39, 119, 0.12)' },
  { border: '#0891b2', fill: 'rgba(8, 145, 178, 0.12)' },
  { border: '#4f46e5', fill: 'rgba(79, 70, 229, 0.12)' },
  { border: '#65a30d', fill: 'rgba(101, 163, 13, 0.12)' },
  { border: '#c026d3', fill: 'rgba(192, 38, 211, 0.12)' },
  { border: '#dc2626', fill: 'rgba(220, 38, 38, 0.12)' },
]

const radarData = computed(() => ({
  labels: metrics.value.map((metric) => metric.shortLabel),
  datasets: radarModels.value.map((model, index) => ({
    label: model.name,
    data: metrics.value.map((metric) => metricValue(model, metric.key)),
    borderColor: palette[index].border,
    backgroundColor: palette[index].fill,
    pointBackgroundColor: palette[index].border,
    spanGaps: false,
  })),
}))
const radarOptions = computed(() => {
  const textColor = isDark.value ? '#d1d5db' : '#374151'
  const gridColor = isDark.value ? 'rgba(148, 163, 184, 0.28)' : 'rgba(107, 114, 128, 0.22)'
  return {
    responsive: true,
    maintainAspectRatio: false,
    animation: false as const,
    scales: {
      r: {
        beginAtZero: true,
        suggestedMin: 0,
        suggestedMax: 100,
        grid: { color: gridColor },
        angleLines: { color: gridColor },
        pointLabels: { color: textColor },
        ticks: { color: textColor, backdropColor: 'transparent' },
      },
    },
    plugins: {
      legend: { labels: { color: textColor } },
      tooltip: {
        callbacks: {
          label: (context: { dataset: { label?: string }; raw: unknown }) => {
            const value = typeof context.raw === 'number' ? context.raw : '—'
            return `${context.dataset.label ?? ''}: ${value}`
          },
        },
      },
    },
  }
})

watch(
  () => [props.latest !== null && props.latest !== undefined, allModels.value.map((model) => model.slug).join('\u0000')] as const,
  () => reconcileSelection(),
  { immediate: true }
)
watch(selectedModelSlugs, () => {
  if (selectionInitialized.value) replaceModelsQuery(selectedModelSlugs.value)
}, { deep: true })
watch(() => route.query.models, () => reconcileSelection(true))

function sanitizeModelSlugs(slugs: readonly string[]): string[] {
  const result: string[] = []
  const seen = new Set<string>()
  for (const slug of slugs) {
    if (result.length >= maxSelectedModels || seen.has(slug) || !modelsBySlug.value.has(slug)) continue
    seen.add(slug)
    result.push(slug)
  }
  return result
}

function readModelsQuery(): string[] {
  const queryValue = route.query.models
  const raw = Array.isArray(queryValue) ? queryValue[0] : queryValue
  if (typeof raw !== 'string' || raw.length === 0) return []
  return sanitizeModelSlugs(raw.split(',').map((slug) => slug.trim()).filter(Boolean))
}

function fallbackModelSlugs(): string[] {
  const defaults = defaultModelSlugs.value
  return defaults.length > 0 ? defaults : allModels.value.slice(0, 6).map((model) => model.slug)
}

function reconcileSelection(fromLocation = !selectionInitialized.value): void {
  if (props.latest === null || props.latest === undefined) return
  if (allModels.value.length === 0) {
    selectedModelSlugs.value = []
    selectionInitialized.value = true
    replaceModelsQuery([])
    return
  }
  const candidate = fromLocation
    ? readModelsQuery()
    : sanitizeModelSlugs(selectedModelSlugs.value)
  selectedModelSlugs.value = candidate.length > 0 ? candidate : fallbackModelSlugs()
  selectionInitialized.value = true
  replaceModelsQuery(selectedModelSlugs.value)
}

function replaceModelsQuery(slugs: readonly string[]): void {
  const models = slugs.length > 0 ? slugs.join(',') : undefined
  const routeModels = route.query.models
  const alreadyCanonical = Array.isArray(routeModels)
    ? routeModels.length === 1 && routeModels[0] === models
    : routeModels === models
  if (alreadyCanonical) return

  const query = { ...route.query }
  if (models === undefined) delete query.models
  else query.models = models
  void router.replace({ path: route.path, query, hash: route.hash })
}

function modelSearchText(model: DegradationModelDTO): string {
  return [
    model.name,
    model.slug,
    model.vendor,
    ...catalogMatches(model).flatMap((match) => [match.platform, match.model_id]),
  ].join('\n').toLocaleLowerCase()
}

function modelOptionDisabled(slug: string): boolean {
  if (selectedModelSlugs.value.includes(slug)) return selectedModelSlugs.value.length <= 1
  return selectedModelSlugs.value.length >= maxSelectedModels
}

function toggleModel(slug: string): void {
  if (!modelsBySlug.value.has(slug)) return
  const selected = selectedModelSlugs.value
  if (selected.includes(slug)) {
    if (selected.length > 1) selectedModelSlugs.value = selected.filter((item) => item !== slug)
    return
  }
  if (selected.length < maxSelectedModels) selectedModelSlugs.value = [...selected, slug]
}

function removeModel(slug: string): void {
  if (selectedModelSlugs.value.length <= 1) return
  selectedModelSlugs.value = selectedModelSlugs.value.filter((item) => item !== slug)
}

function catalogMatchSummary(model: DegradationModelDTO): string {
  return catalogMatches(model).map((match) => `${match.platform} / ${match.model_id}`).join(' · ')
}

function catalogMatches(model: DegradationModelDTO): DegradationModelDTO['catalog_matches'] {
  return model.catalog_matches ?? []
}

function metricValue(model: DegradationModelDTO, metric: DegradationMetric): number | null {
  return model[metric]
}

function activateTab(tab: DegradationTab): void {
  activeTab.value = tab
}

function handleTabKeydown(event: KeyboardEvent): void {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  let index = tabs.value.findIndex((tab) => tab.key === activeTab.value)
  if (event.key === 'ArrowRight') index = (index + 1) % tabs.value.length
  else if (event.key === 'ArrowLeft') index = (index - 1 + tabs.value.length) % tabs.value.length
  else if (event.key === 'Home') index = 0
  else index = tabs.value.length - 1
  activeTab.value = tabs.value[index].key
  void nextTick(() => document.getElementById(`degradation-tab-${activeTab.value}`)?.focus())
}

function confidenceInterval(lower: number | null, upper: number | null): string {
  if (lower === null || upper === null) return '—'
  return `${formatNumber(lower)}–${formatNumber(upper)}`
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(value)
}

function formatDate(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return t('radar.common.unknownTime', 'Unknown')
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

onMounted(() => {
  themeObserver = new MutationObserver(() => {
    isDark.value = document.documentElement.classList.contains('dark')
  })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})

onBeforeUnmount(() => {
  themeObserver?.disconnect()
})
</script>
