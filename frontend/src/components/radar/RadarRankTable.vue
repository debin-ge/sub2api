<template>
  <RadarEmptyState v-if="targets.length === 0" />

  <div v-else class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
    <div class="overflow-x-auto">
      <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
        <thead class="bg-gray-50 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-dark-400">
          <tr>
            <th class="w-16 px-4 py-3 text-left">{{ t('benchmark.public.table.rank') }}</th>
            <th class="min-w-64 px-4 py-3 text-left">{{ t('benchmark.public.table.model') }}</th>
            <th class="px-4 py-3 text-right">{{ t('benchmark.public.table.abilityScore') }}</th>
            <th class="px-4 py-3 text-right">{{ t('benchmark.public.table.passed') }}</th>
            <th class="px-4 py-3 text-right">{{ t('benchmark.public.table.latency') }}</th>
            <th class="px-4 py-3 text-right">{{ t('benchmark.public.table.token') }}</th>
            <th class="px-4 py-3 text-right">{{ t('benchmark.public.table.cost') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
          <tr
            v-for="target in targets"
            :key="`${target.channel_id}:${target.model}`"
            class="transition-colors hover:bg-gray-50 dark:hover:bg-dark-800/60"
          >
            <td class="px-4 py-4 align-top font-semibold text-gray-500 dark:text-dark-400">
              #{{ formatInteger(target.rank) }}
            </td>
            <td class="px-4 py-4 align-top">
              <div class="flex flex-col gap-1">
                <span class="font-semibold text-gray-900 dark:text-white">
                  {{ displayName(target) }}
                </span>
                <span class="text-xs text-gray-500 dark:text-dark-400">
                  {{ target.model }} · {{ channelName(target) }}
                </span>
              </div>
            </td>
            <td class="px-4 py-4 text-right align-top">
              <span class="font-semibold text-gray-900 dark:text-white">
                {{ formatScore(target.overall_score) }}
              </span>
            </td>
            <td class="px-4 py-4 text-right align-top text-gray-700 dark:text-dark-200">
              {{ formatInteger(target.passed_count) }} / {{ formatInteger(target.total_count) }}
            </td>
            <td class="px-4 py-4 text-right align-top text-gray-700 dark:text-dark-200">
              {{ formatLatency(target.metrics.avg_latency_ms) }}
            </td>
            <td class="px-4 py-4 text-right align-top text-gray-700 dark:text-dark-200">
              {{ formatInteger(target.metrics.avg_total_tokens) }}
            </td>
            <td class="px-4 py-4 text-right align-top text-gray-700 dark:text-dark-200">
              {{ formatCost(target.metrics.total_cost) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import RadarEmptyState from './RadarEmptyState.vue'
import type { BenchmarkRadarTarget } from '@/types/benchmark'
import { benchmarkChannelFallback } from '@/components/radar/benchmarkI18n'

defineProps<{
  targets: BenchmarkRadarTarget[]
}>()

const { locale, t } = useI18n()

function displayName(target: BenchmarkRadarTarget): string {
  return target.display_name?.trim() || target.model
}

function channelName(target: BenchmarkRadarTarget): string {
  return target.channel_name?.trim() || benchmarkChannelFallback(target.channel_id, t)
}

function formatScore(value: number): string {
  return new Intl.NumberFormat(locale.value, {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  }).format(value)
}

function formatLatency(value?: number | null): string {
  if (value == null) return '-'
  if (value >= 1000) {
    return `${new Intl.NumberFormat(locale.value, {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(value / 1000)}s`
  }
  return `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(value)}ms`
}

function formatInteger(value?: number | null): string {
  if (value == null) return '-'
  return new Intl.NumberFormat(locale.value, {
    maximumFractionDigits: 0,
  }).format(value)
}

function formatCost(value: number): string {
  return new Intl.NumberFormat(locale.value, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 4,
    maximumFractionDigits: 4,
  }).format(value)
}
</script>
