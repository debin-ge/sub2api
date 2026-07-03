<template>
  <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
    <div
      v-for="card in cards"
      :key="card.label"
      class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900"
    >
      <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">
        {{ card.label }}
      </div>
      <div class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ card.value }}</div>
      <div v-if="card.caption" class="mt-1 text-xs text-gray-500 dark:text-dark-400">
        {{ card.caption }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { BenchmarkPublicRadar } from '@/types/benchmark'
import { benchmarkRunFallback } from '@/components/radar/benchmarkI18n'

const props = defineProps<{
  radar: BenchmarkPublicRadar
}>()

const { locale, t } = useI18n()

const averageAbilityScore = computed(() => {
  if (props.radar.targets.length === 0) return '-'
  const total = props.radar.targets.reduce((sum, target) => sum + target.overall_score, 0)
  return new Intl.NumberFormat(locale.value, {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  }).format(total / props.radar.targets.length)
})

const passedSummary = computed(() => {
  const passed = props.radar.targets.reduce((sum, target) => sum + (target.passed_count || 0), 0)
  const total = props.radar.targets.reduce((sum, target) => sum + (target.total_count || 0), 0)
  const fmt = (v: number) => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(v)
  return `${fmt(passed)}/${fmt(total)}`
})

const cards = computed(() => [
  {
    label: t('benchmark.public.summary.targetsLabel'),
    value: new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(props.radar.targets.length),
    caption: t('benchmark.public.summary.targetsCaption'),
  },
  {
    label: t('benchmark.public.summary.averageScoreLabel'),
    value: averageAbilityScore.value,
    caption: t('benchmark.public.summary.averageScoreCaption'),
  },
  {
    label: t('benchmark.public.summary.passedLabel'),
    value: passedSummary.value,
    caption: t('benchmark.public.summary.passedCaption'),
  },
  {
    label: t('benchmark.public.summary.latestRunLabel'),
    value: props.radar.latest_run ? benchmarkRunFallback(props.radar.latest_run.id, t) : '-',
    caption: props.radar.latest_run?.completed_at
      ? new Intl.DateTimeFormat(locale.value, {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      }).format(new Date(props.radar.latest_run.completed_at))
      : t('benchmark.public.summary.latestRunEmptyCaption'),
  },
])
</script>
