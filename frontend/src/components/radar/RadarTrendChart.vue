<template>
  <section v-if="trends && trends.length > 0" class="rounded-2xl border border-gray-200 bg-white p-6 shadow-sm dark:border-dark-800 dark:bg-dark-900">
    <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('benchmark.public.trend.title') }}</h2>
    <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('benchmark.public.trend.description') }}</p>

    <div class="mt-6 space-y-8">
      <div v-for="trend in trends" :key="`${trend.model}-${trend.channel_id}`" class="space-y-2">
        <h3 class="text-sm font-medium text-gray-700 dark:text-gray-200">
          {{ trend.display_name || trend.model }}
          <span v-if="trend.channel_name" class="ml-1 text-xs text-gray-500">· {{ trend.channel_name }}</span>
        </h3>

        <div class="relative h-48 rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800">
          <svg class="h-full w-full" viewBox="0 0 800 200" preserveAspectRatio="none">
            <!-- Grid lines -->
            <line v-for="i in 5" :key="`grid-${i}`" x1="0" :y1="i * 40" x2="800" :y2="i * 40" stroke="currentColor" class="text-gray-200 dark:text-dark-600" stroke-width="0.5" />

            <!-- Score line -->
            <polyline
              :points="scorePolyline(trend.points)"
              fill="none"
              stroke="currentColor"
              class="text-primary-600 dark:text-primary-400"
              stroke-width="2"
            />

            <!-- Data points -->
            <circle
              v-for="(point, idx) in trend.points"
              :key="`point-${idx}`"
              :cx="pointX(idx, trend.points.length)"
              :cy="pointY(point.overall_score)"
              r="4"
              fill="currentColor"
              class="text-primary-600 dark:text-primary-400"
            >
              <title>{{ formatPointTooltip(point) }}</title>
            </circle>
          </svg>

          <!-- Y-axis labels -->
          <div class="absolute left-0 top-0 flex h-full flex-col justify-between py-4 text-xs text-gray-500">
            <span>100</span>
            <span>75</span>
            <span>50</span>
            <span>25</span>
            <span>0</span>
          </div>
        </div>

        <div class="flex items-center justify-between text-xs text-gray-500 dark:text-gray-400">
          <span v-if="trend.points.length > 0">{{ formatDate(trend.points[0].finished_at) }}</span>
          <span v-if="trend.points.length > 1">{{ formatDate(trend.points[trend.points.length - 1].finished_at) }}</span>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { BenchmarkPublicTrend, BenchmarkTrendPoint } from '@/types/benchmark'

defineProps<{
  trends?: BenchmarkPublicTrend[]
}>()

const { locale, t } = useI18n()

function scorePolyline(points: BenchmarkTrendPoint[]): string {
  if (points.length === 0) return ''
  return points.map((point, idx) => `${pointX(idx, points.length)},${pointY(point.overall_score)}`).join(' ')
}

function pointX(index: number, total: number): number {
  if (total === 1) return 400
  return (index / (total - 1)) * 800
}

function pointY(score: number): number {
  return 200 - (score / 100) * 200
}

function formatPointTooltip(point: BenchmarkTrendPoint): string {
  return `${formatDate(point.finished_at)}: ${point.overall_score.toFixed(1)}`
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    month: '2-digit',
    day: '2-digit',
  }).format(new Date(value))
}
</script>
