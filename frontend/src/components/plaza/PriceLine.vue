<template>
  <div class="grid grid-cols-[4.5rem_minmax(0,1fr)_auto] items-center gap-2 text-sm">
    <span class="text-gray-500 dark:text-gray-400">{{ label }}</span>
    <div class="min-w-0">
      <div class="text-xs text-gray-500 dark:text-gray-400">
        {{ formatScaled(value, scale) }} {{ unitLabel }}
      </div>
      <div class="truncate">
        <span class="text-gray-400 line-through dark:text-gray-500">
          {{ formatCNYMarket(value, rate, scale) }}
        </span>
        <span class="mx-1 text-gray-400">-&gt;</span>
        <span class="font-semibold text-emerald-600 dark:text-emerald-400">
          {{ formatCNYRecharged(value, multiplier, scale) }}
        </span>
      </div>
    </div>
    <span class="rounded-full bg-amber-100 px-2 py-0.5 text-xs font-semibold text-amber-700 dark:bg-amber-900/40 dark:text-amber-300">
      {{ computeValueBoost(multiplier, rate) }}x
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import {
  PER_REQUEST_SCALE,
  computeValueBoost,
  formatCNYMarket,
  formatCNYRecharged,
  formatScaled
} from '@/utils/pricing'

const props = defineProps<{
  label: string
  value: number | null
  scale: number
  multiplier: number
  rate: number
}>()

const unitLabel = computed(() => (props.scale === PER_REQUEST_SCALE ? '/次' : '/1M'))
</script>
