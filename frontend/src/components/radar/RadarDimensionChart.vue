<template>
  <div class="rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
    <div class="mb-4 flex items-center justify-between gap-3">
      <div>
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">能力维度</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">按公开结果汇总各维度能力分。</p>
      </div>
    </div>

    <div v-if="dimensionRows.length === 0" class="text-sm text-gray-500 dark:text-dark-400">
      暂无维度数据
    </div>

    <div v-else class="space-y-3">
      <div v-for="row in dimensionRows" :key="row.name" class="grid gap-2 sm:grid-cols-[8rem_1fr_3rem] sm:items-center">
        <div class="truncate text-sm font-medium text-gray-700 dark:text-dark-200">{{ row.name }}</div>
        <div class="h-2 rounded-full bg-gray-100 dark:bg-dark-800">
          <div
            class="h-2 rounded-full bg-primary-500"
            :style="{ width: `${Math.max(0, Math.min(row.value, 100))}%` }"
          ></div>
        </div>
        <div class="text-sm tabular-nums text-gray-500 dark:text-dark-400">{{ row.value.toFixed(1) }}</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { BenchmarkRadarTarget } from '@/types/benchmark'

const props = defineProps<{
  targets: BenchmarkRadarTarget[]
}>()

const dimensionRows = computed(() => {
  const totals = new Map<string, { total: number; count: number }>()

  for (const target of props.targets) {
    for (const [name, value] of Object.entries(target.dimensions ?? {})) {
      if (typeof value !== 'number' || Number.isNaN(value)) continue
      const current = totals.get(name) ?? { total: 0, count: 0 }
      current.total += value
      current.count += 1
      totals.set(name, current)
    }
  }

  return Array.from(totals.entries())
    .map(([name, item]) => ({ name, value: item.total / item.count }))
    .sort((a, b) => b.value - a.value)
})
</script>
