<template>
  <RadarEmptyState v-if="targets.length === 0" />

  <div v-else class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
    <div class="overflow-x-auto">
      <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
        <thead class="bg-gray-50 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-dark-400">
          <tr>
            <th class="w-16 px-4 py-3 text-left">排名</th>
            <th class="min-w-64 px-4 py-3 text-left">模型</th>
            <th class="px-4 py-3 text-right">能力分</th>
            <th class="px-4 py-3 text-right">延迟</th>
            <th class="px-4 py-3 text-right">成功率</th>
            <th class="px-4 py-3 text-right">Token</th>
            <th class="px-4 py-3 text-right">成本</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
          <tr
            v-for="target in targets"
            :key="`${target.channel_id}:${target.model}`"
            class="transition-colors hover:bg-gray-50 dark:hover:bg-dark-800/60"
          >
            <td class="px-4 py-4 align-top font-semibold text-gray-500 dark:text-dark-400">
              #{{ target.rank }}
            </td>
            <td class="px-4 py-4 align-top">
              <div class="flex flex-col gap-1">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="font-semibold text-gray-900 dark:text-white">
                    {{ displayName(target) }}
                  </span>
                  <span
                    v-if="target.score_basis.insufficient_sample"
                    class="rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700 dark:border-amber-700/50 dark:bg-amber-900/30 dark:text-amber-300"
                  >
                    样本不足
                  </span>
                </div>
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
              <div>{{ formatLatency(target.metrics.latency_p50_ms) }}</div>
              <div v-if="target.metrics.latency_p95_ms != null" class="text-xs text-gray-400 dark:text-dark-500">
                P95 {{ formatLatency(target.metrics.latency_p95_ms) }}
              </div>
            </td>
            <td class="px-4 py-4 text-right align-top text-gray-700 dark:text-dark-200">
              {{ formatPercent(target.metrics.success_rate) }}
            </td>
            <td class="px-4 py-4 text-right align-top text-gray-700 dark:text-dark-200">
              {{ formatInteger(target.metrics.avg_total_tokens) }}
            </td>
            <td class="px-4 py-4 text-right align-top text-gray-700 dark:text-dark-200">
              {{ formatCost(target.metrics.estimated_cost) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import RadarEmptyState from './RadarEmptyState.vue'
import type { BenchmarkRadarTarget } from '@/types/benchmark'

defineProps<{
  targets: BenchmarkRadarTarget[]
}>()

function displayName(target: BenchmarkRadarTarget): string {
  return target.display_name?.trim() || target.model
}

function channelName(target: BenchmarkRadarTarget): string {
  return target.channel_name?.trim() || `Channel ${target.channel_id}`
}

function formatScore(value: number): string {
  return value.toFixed(1)
}

function formatLatency(value?: number | null): string {
  if (value == null) return '-'
  if (value >= 1000) return `${(value / 1000).toFixed(2)}s`
  return `${Math.round(value)}ms`
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`
}

function formatInteger(value?: number | null): string {
  if (value == null) return '-'
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(value)
}

function formatCost(value: number): string {
  return `$${value.toFixed(4)}`
}
</script>
