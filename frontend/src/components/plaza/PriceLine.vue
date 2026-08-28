<template>
  <div class="grid grid-cols-[5rem_minmax(0,1fr)] items-center gap-3 rounded-lg border border-gray-100 bg-gray-50/60 px-3 py-2 text-sm dark:border-dark-800 dark:bg-dark-900/40">
    <span class="flex items-center gap-1.5 text-xs font-medium text-gray-500 dark:text-gray-400">
      <span class="inline-block h-3 w-0.5 rounded-full bg-gray-300 dark:bg-dark-600" aria-hidden="true"></span>
      <span class="truncate">{{ label }}</span>
    </span>
    <div class="min-w-0">
      <div class="flex items-baseline gap-1">
        <span class="text-base font-semibold text-gray-900 dark:text-white">
          {{ formatCNYEffective(value, scale, billingRateMultiplier) }}
        </span>
        <span v-if="value != null" class="text-xs text-gray-400 dark:text-gray-500">
          {{ unitLabel }}
        </span>
      </div>
      <div v-if="value != null" class="mt-0.5 flex items-center gap-1 text-xs">
        <span class="text-gray-400 line-through dark:text-gray-500">
          {{ formatMoney(value, currency, scale) }}
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
  formatCNYEffective,
  formatMoney
} from '@/utils/pricing'

const props = defineProps<{
  label: string
  value: number | null
  scale: number
  currency?: string
  billingRateMultiplier?: number
}>()

const { t } = useI18n()

const unitLabel = computed(() =>
  props.scale === PER_REQUEST_SCALE
    ? t('plaza.price.unitPerRequest')
    : t('plaza.price.unitPerMillion')
)
</script>
