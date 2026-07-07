<template>
  <div class="grid grid-cols-[5rem_minmax(0,1fr)] items-center gap-3 rounded-lg border border-gray-100 bg-gray-50/60 px-3 py-2 text-sm dark:border-dark-800 dark:bg-dark-900/40">
    <span class="flex items-center gap-1.5 text-xs font-medium text-gray-500 dark:text-gray-400">
      <span class="inline-block h-3 w-0.5 rounded-full bg-gray-300 dark:bg-dark-600" aria-hidden="true"></span>
      <span class="truncate">{{ label }}</span>
    </span>
    <div class="min-w-0">
      <div class="flex items-baseline gap-1">
        <span class="text-base font-semibold text-gray-900 dark:text-white">
          {{ formatScaled(value, scale) }}
        </span>
        <span v-if="value != null" class="text-xs text-gray-400 dark:text-gray-500">
          {{ unitLabel }}
        </span>
      </div>
      <div v-if="value != null" class="mt-0.5 flex items-center gap-1 text-xs">
        <span class="text-gray-400 line-through dark:text-gray-500">
          {{ formatCNYMarket(value, rate, scale) }}
        </span>
        <svg
          class="h-3 w-3 shrink-0 text-gray-400 dark:text-gray-500"
          viewBox="0 0 20 20"
          fill="currentColor"
          aria-hidden="true"
        >
          <path fill-rule="evenodd" d="M12.293 5.293a1 1 0 011.414 0l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414-1.414L14.586 11H3a1 1 0 110-2h11.586l-2.293-2.293a1 1 0 010-1.414z" clip-rule="evenodd" />
        </svg>
        <span class="font-semibold text-emerald-600 dark:text-emerald-400">
          {{ formatCNYRecharged(value, multiplier, scale, billingRateMultiplier) }}
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  PER_REQUEST_SCALE,
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
  billingRateMultiplier?: number
}>()

const { t } = useI18n()

const unitLabel = computed(() =>
  props.scale === PER_REQUEST_SCALE
    ? t('plaza.price.unitPerRequest')
    : t('plaza.price.unitPerMillion')
)
</script>
