<template>
  <div class="rounded-2xl border border-gray-200 bg-white shadow-sm dark:border-dark-800 dark:bg-dark-900">
    <div class="border-b border-gray-200 px-4 pt-4 dark:border-dark-800 sm:px-6">
      <div role="tablist" :aria-label="t('radar.degradation.tabs', 'Benchmark views')" class="flex gap-2 overflow-x-auto">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          type="button"
          role="tab"
          :data-tab="tab.key"
          :id="`degradation-tab-${tab.key}`"
          :aria-selected="activeTab === tab.key"
          :aria-controls="`degradation-panel-${tab.key}`"
          :tabindex="activeTab === tab.key ? 0 : -1"
          class="-mb-px shrink-0 border-b-2 px-3 py-3 text-sm font-semibold focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          :class="activeTab === tab.key ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'"
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
      class="p-4 sm:p-6"
    >
      <div class="mb-4 flex flex-wrap items-center gap-3 text-sm" aria-live="polite">
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
      </div>
      <div v-if="allModels.length > 0" class="grid gap-8 xl:grid-cols-[minmax(0,1.25fr)_minmax(18rem,0.75fr)]">
        <div class="min-w-0">
          <div class="h-[22rem]" role="img" :aria-label="t('radar.degradation.radarLabel', 'Model benchmark comparison radar chart')">
            <Radar :data="radarData" :options="radarOptions" />
          </div>
        </div>

        <div class="space-y-3">
          <article v-for="model in radarModels" :key="model.slug" class="rounded-xl border border-gray-200 p-4 dark:border-dark-800">
            <div class="flex items-start justify-between gap-3">
              <div>
                <h3 class="font-semibold text-gray-950 dark:text-white">{{ model.name }}</h3>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ model.vendor }}</p>
              </div>
            </div>
            <dl class="mt-3 grid grid-cols-3 gap-2 text-center text-xs">
              <div v-for="metric in metrics" :key="metric.key" class="rounded-lg bg-gray-50 p-2 dark:bg-dark-800/70">
                <dt class="truncate text-gray-500 dark:text-gray-400">{{ metric.shortLabel }}</dt>
                <dd class="mt-1 font-semibold text-gray-900 dark:text-white">{{ metricValue(model, metric.key) ?? '—' }}</dd>
              </div>
            </dl>
          </article>
        </div>
      </div>
      <p v-else-if="!latestLoading && !latestError" class="rounded-xl border border-dashed border-gray-300 p-10 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
        {{ t('radar.degradation.empty', 'Waiting for benchmark data') }}
      </p>

      <section v-if="trendModels.length > 0" class="mt-8 border-t border-gray-200 pt-6 dark:border-dark-800">
        <div class="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <h3 class="text-base font-semibold text-gray-950 dark:text-white">
              {{ t('radar.degradation.trendTitle', '90-day model trend') }}
            </h3>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <label class="text-xs font-medium text-gray-600 dark:text-gray-300">
              {{ t('radar.degradation.model', 'Model') }}
              <select
                v-model="selectedModel"
                data-testid="model-select"
                class="mt-1 block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500 dark:border-dark-700 dark:bg-dark-800 dark:text-white"
              >
                <option v-for="model in trendModels" :key="model.slug" :value="model.slug">{{ model.name }}</option>
              </select>
            </label>
            <label class="text-xs font-medium text-gray-600 dark:text-gray-300">
              {{ t('radar.degradation.metric', 'Metric') }}
              <select
                v-model="selectedMetric"
                data-testid="metric-select"
                class="mt-1 block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500 dark:border-dark-700 dark:bg-dark-800 dark:text-white"
              >
                <option v-for="metric in metrics" :key="metric.key" :value="metric.key">{{ metric.label }}</option>
              </select>
            </label>
          </div>
        </div>

        <div class="mt-5 min-h-56">
          <p v-if="trendLoading" role="status" aria-live="polite" class="py-16 text-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('radar.trend.loading', 'Loading trend') }}
          </p>
          <p v-if="trendError" role="status" aria-live="polite" class="py-4 text-center text-sm text-red-700 dark:text-red-300">
            {{ t('radar.trend.error', 'Unable to load trend') }}
          </p>
          <div v-if="matchingTrend && matchingTrend.data_points.length > 0" class="h-64" aria-hidden="true">
            <Line :data="lineData" :options="lineOptions" />
          </div>
          <table
            v-if="matchingTrend && matchingTrend.data_points.length > 0"
            data-testid="degradation-trend-data"
            class="sr-only"
          >
            <caption>{{ trendChartLabel }}</caption>
            <thead>
              <tr>
                <th scope="col">{{ t('radar.common.date', 'Date') }}</th>
                <th scope="col">{{ selectedMetricLabel }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="point in matchingTrend.data_points" :key="point.date">
                <td><time :datetime="point.date">{{ formatDay(point.date) }}</time></td>
                <td>{{ formatNumber(point.value) }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="!trendLoading && !trendError && (!matchingTrend || matchingTrend.data_points.length === 0)" class="rounded-xl border border-dashed border-gray-300 py-16 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
            {{ t('radar.degradation.noTrend', 'No trend data') }}
          </p>
        </div>
      </section>
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
          <strong class="font-semibold text-gray-900 dark:text-white">{{ lmarena?.total_votes === null || lmarena?.total_votes === undefined ? '—' : formatNumber(lmarena.total_votes) }}</strong>
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
        <table class="min-w-[46rem] w-full divide-y divide-gray-200 text-sm dark:divide-dark-800">
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
      <p v-else-if="!lmarenaLoading && !lmarenaError" class="rounded-xl border border-dashed border-gray-300 p-10 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
        {{ t('radar.lmarena.empty', 'No leaderboard models match the current model catalog.') }}
      </p>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  CategoryScale,
  Legend,
  LineController,
  LineElement,
  LinearScale,
  PointElement,
  RadarController,
  RadialLinearScale,
  Title,
  Tooltip,
} from 'chart.js'
import { Line, Radar } from 'vue-chartjs'
import Icon from '@/components/icons/Icon.vue'
import type {
  DegradationLatestDTO,
  DegradationMetric,
  DegradationModelDTO,
  DegradationTrendDTO,
  LMArenaDTO,
} from '@/types/radar'

ChartJS.register(
  CategoryScale,
  Legend,
  LineController,
  LineElement,
  LinearScale,
  PointElement,
  RadarController,
  RadialLinearScale,
  Title,
  Tooltip
)

type DegradationTab = 'overview' | 'lmarena'

const props = withDefaults(defineProps<{
  latest?: DegradationLatestDTO | null
  latestLoading?: boolean
  latestError?: string | null
  lmarena?: LMArenaDTO | null
  lmarenaLoading?: boolean
  lmarenaError?: string | null
  trend?: DegradationTrendDTO | null
  trendLoading?: boolean
  trendError?: string | null
}>(), {
  latest: null,
  latestLoading: false,
  latestError: null,
  lmarena: null,
  lmarenaLoading: false,
  lmarenaError: null,
  trend: null,
  trendLoading: false,
  trendError: null,
})

const emit = defineEmits<{
  'request-trend': [model: string, metric: DegradationMetric, days: 90]
}>()
const { t, locale } = useI18n()
const activeTab = ref<DegradationTab>('overview')
const selectedModel = ref('')
const selectedMetric = ref<DegradationMetric>('intelligence_index')
const isDark = ref(document.documentElement.classList.contains('dark'))
let themeObserver: MutationObserver | null = null

const tabs = computed(() => [
  { key: 'overview' as const, label: t('radar.degradation.overview', 'Index overview') },
  { key: 'lmarena' as const, label: t('radar.degradation.lmarena', 'Model leaderboard') },
])
const metrics = computed(() => [
  { key: 'intelligence_index' as const, label: t('radar.degradation.intelligence', 'Intelligence index'), shortLabel: t('radar.degradation.intelligenceShort', 'Intelligence') },
  { key: 'coding_index' as const, label: t('radar.degradation.coding', 'Coding index'), shortLabel: t('radar.degradation.codingShort', 'Coding') },
  { key: 'agentic_index' as const, label: t('radar.degradation.agentic', 'Agentic index'), shortLabel: t('radar.degradation.agenticShort', 'Agentic') },
])
const allModels = computed(() => props.latest?.models ?? [])
const radarModels = computed(() => allModels.value.slice(0, 6))
const trendModels = computed(() => props.latest?.trend_available ? allModels.value : [])
const leaderboard = computed(() => [...(props.lmarena?.leaderboard ?? [])].sort((left, right) => left.rank - right.rank))
const matchingTrend = computed(() => {
  if (!props.trend || props.trend.model_slug !== selectedModel.value || props.trend.metric !== selectedMetric.value) return null
  return props.trend
})
const trendChartLabel = computed(() => {
  const model = allModels.value.find((item) => item.slug === selectedModel.value)?.name ?? selectedModel.value
  const metric = metrics.value.find((item) => item.key === selectedMetric.value)?.label ?? selectedMetric.value
  return `${model}: ${metric}`
})
const selectedMetricLabel = computed(() => metrics.value.find((item) => item.key === selectedMetric.value)?.label ?? selectedMetric.value)

const palette = [
  { border: '#2563eb', fill: 'rgba(37, 99, 235, 0.12)' },
  { border: '#9333ea', fill: 'rgba(147, 51, 234, 0.12)' },
  { border: '#059669', fill: 'rgba(5, 150, 105, 0.12)' },
  { border: '#ea580c', fill: 'rgba(234, 88, 12, 0.12)' },
  { border: '#db2777', fill: 'rgba(219, 39, 119, 0.12)' },
  { border: '#0891b2', fill: 'rgba(8, 145, 178, 0.12)' },
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
const lineData = computed(() => ({
  labels: matchingTrend.value?.data_points.map((point) => formatDay(point.date)) ?? [],
  datasets: [{
    label: trendChartLabel.value,
    data: matchingTrend.value?.data_points.map((point) => point.value) ?? [],
    borderColor: palette[0].border,
    backgroundColor: palette[0].fill,
    pointRadius: 2,
    tension: 0.25,
  }],
}))
const lineOptions = computed(() => {
  const textColor = isDark.value ? '#d1d5db' : '#374151'
  const gridColor = isDark.value ? 'rgba(148, 163, 184, 0.22)' : 'rgba(107, 114, 128, 0.16)'
  return {
    responsive: true,
    maintainAspectRatio: false,
    animation: false as const,
    scales: {
      x: { ticks: { color: textColor }, grid: { color: gridColor } },
      y: {
        suggestedMin: 0,
        suggestedMax: 100,
        title: { display: true, text: selectedMetricLabel.value, color: textColor },
        ticks: { color: textColor },
        grid: { color: gridColor },
      },
    },
    plugins: { legend: { labels: { color: textColor } } },
  }
})

watch(trendModels, (models) => {
  if (!models.some((model) => model.slug === selectedModel.value)) selectedModel.value = models[0]?.slug ?? ''
}, { immediate: true })
watch([selectedModel, selectedMetric], ([model, metric]) => {
  if (model) emit('request-trend', model, metric, 90)
}, { immediate: true })

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

function formatDay(value: string): string {
  const date = new Date(`${value}T00:00:00Z`)
  if (!Number.isFinite(date.getTime())) return t('radar.common.unknownTime', 'Unknown')
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeZone: 'UTC' }).format(date)
}

onMounted(() => {
  themeObserver = new MutationObserver(() => {
    isDark.value = document.documentElement.classList.contains('dark')
  })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})

onBeforeUnmount(() => themeObserver?.disconnect())
</script>
