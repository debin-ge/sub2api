<template>
  <div :class="['grid items-center gap-x-3 px-3 py-2', gridClass]">
    <div :class="['min-w-0 font-mono text-[10px] font-medium uppercase tracking-[0.12em] text-gray-400 dark:text-gray-500', labelClass]">
      <span class="truncate">{{ label }}</span>
    </div>

    <div v-if="standardAvailable" class="min-w-0 text-right">
      <div class="flex items-baseline justify-end gap-1">
        <span class="truncate font-mono text-base font-semibold tabular-nums tracking-tight text-gray-900 dark:text-white">
          {{ rechargedDisplay(standardValue, standardBillingRateMultiplier) }}
        </span>
        <span v-if="standardValue != null" class="shrink-0 font-mono text-[10px] text-gray-400 dark:text-gray-500">
          {{ unitLabel }}
        </span>
      </div>
      <div
        :class="[
          'mt-0.5 truncate font-mono text-[10px] tabular-nums text-gray-400/80 dark:text-gray-500',
          standardValue != null && 'line-through decoration-gray-400/70 dark:decoration-gray-500/70'
        ]"
      >
        {{ standardValue == null ? t('plaza.card.notAvailable') : originalDisplay(standardValue) }}
      </div>
    </div>

    <div v-if="vipAvailable" class="min-w-0 text-right">
      <div class="flex items-baseline justify-end gap-1">
        <span class="truncate font-mono text-base font-semibold tabular-nums tracking-tight text-orange-600 dark:text-orange-400">
          {{ rechargedDisplay(vipValue, vipBillingRateMultiplier) }}
        </span>
        <span v-if="vipValue != null" class="shrink-0 font-mono text-[10px] text-orange-400 dark:text-orange-500/80">
          {{ unitLabel }}
        </span>
      </div>
      <div
        :class="[
          'mt-0.5 truncate font-mono text-[10px] tabular-nums text-gray-400/80 dark:text-gray-500',
          vipValue != null && 'line-through decoration-gray-400/70 dark:decoration-gray-500/70'
        ]"
      >
        {{ vipValue == null ? t('plaza.card.notAvailable') : originalDisplay(vipValue) }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  PER_REQUEST_SCALE,
  formatCNYRecharged,
  formatScaled
} from '@/utils/pricing'

const props = defineProps<{
  label: string
  scale: number
  multiplier: number
  standardAvailable: boolean
  standardValue: number | null
  standardBillingRateMultiplier?: number
  vipAvailable: boolean
  vipValue: number | null
  vipBillingRateMultiplier?: number
}>()

const { t } = useI18n()

const hasBothPricingTypes = computed(() => props.standardAvailable && props.vipAvailable)
const gridClass = computed(() =>
  hasBothPricingTypes.value
    ? 'grid-cols-2 sm:grid-cols-[minmax(5rem,0.75fr)_repeat(2,minmax(0,1fr))]'
    : 'grid-cols-[minmax(0,1fr)_minmax(7rem,auto)]'
)
const labelClass = computed(() => hasBothPricingTypes.value ? 'col-span-2 sm:col-span-1' : '')

const originalDisplay = (value: number | null) => formatScaled(value, props.scale)
const rechargedDisplay = (value: number | null, billingRateMultiplier?: number) =>
  formatCNYRecharged(value, props.multiplier, props.scale, billingRateMultiplier)

const unitLabel = computed(() =>
  props.scale === PER_REQUEST_SCALE
    ? t('plaza.price.unitPerRequest')
    : t('plaza.price.unitPerMillion')
)
</script>
