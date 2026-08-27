<template>
  <div :class="['grid items-center gap-x-3 px-3 py-2.5', gridClass]">
    <div :class="['min-w-0 truncate text-xs font-medium text-gray-500 dark:text-gray-400', labelClass]">
      {{ label }}
    </div>

    <div v-for="column in columns" :key="column.key" class="min-w-0 text-right">
      <!-- 分时价：高峰/空闲共用一个内联栅格，数值与单位各自对齐，避免撑高卡片。 -->
      <template v-if="showTimeSchedule && column.value != null">
        <span class="inline-grid max-w-full grid-cols-[auto_auto_auto] items-baseline gap-x-1.5 gap-y-0.5 text-left">
          <span :class="['text-[10px] font-medium leading-4', column.tagClass]">
            {{ t('plaza.card.peakPrice') }}
          </span>
          <span :class="['justify-self-end font-mono text-[15px] font-semibold leading-5 tabular-nums tracking-tight', column.valueClass]">
            {{ rechargedDisplay(column.peak, column.billingRateMultiplier) }}
          </span>
          <span :class="['font-mono text-[10px] leading-4', column.unitClass]">{{ unitLabel }}</span>

          <span :class="['text-[10px] font-medium leading-4', column.tagClass]">
            {{ t('plaza.card.offPeakPrice') }}
          </span>
          <span :class="['justify-self-end font-mono text-[13px] font-semibold leading-5 tabular-nums tracking-tight', column.secondaryClass]">
            {{ rechargedDisplay(column.offPeak, column.billingRateMultiplier) }}
          </span>
          <span aria-hidden="true"></span>
        </span>
        <div class="mt-0.5 truncate font-mono text-[10px] tabular-nums text-gray-400/80 line-through decoration-gray-400/70 dark:text-gray-500 dark:decoration-gray-500/70">
          {{ originalDisplay(column.peak) }} / {{ originalDisplay(column.offPeak) }}
        </div>
      </template>

      <template v-else>
        <div class="flex items-baseline justify-end gap-1">
          <span :class="['truncate font-mono text-[15px] font-semibold leading-5 tabular-nums tracking-tight', column.valueClass]">
            {{ rechargedDisplay(column.value, column.billingRateMultiplier) }}
          </span>
          <span v-if="column.value != null" :class="['shrink-0 font-mono text-[10px]', column.unitClass]">
            {{ unitLabel }}
          </span>
        </div>
        <div
          :class="[
            'mt-0.5 truncate font-mono text-[10px] tabular-nums text-gray-400/80 dark:text-gray-500',
            column.value != null && 'line-through decoration-gray-400/70 dark:decoration-gray-500/70'
          ]"
        >
          {{ column.value == null ? t('plaza.card.notAvailable') : originalDisplay(column.value) }}
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelPriceTimeSchedule } from '@/api/channels'
import {
  PER_REQUEST_SCALE,
  formatCNYEffective,
  formatScaled,
  scheduledScaledPrice
} from '@/utils/pricing'

const props = defineProps<{
  label: string
  scale: number
  rate: number
  standardAvailable: boolean
  standardValue: number | null
  standardBillingRateMultiplier?: number
  vipAvailable: boolean
  vipValue: number | null
  vipBillingRateMultiplier?: number
  timeSchedule?: ModelPriceTimeSchedule
}>()

const { t } = useI18n()

const hasBothPricingTypes = computed(() => props.standardAvailable && props.vipAvailable)
const gridClass = computed(() =>
  hasBothPricingTypes.value
    ? 'grid-cols-2 sm:grid-cols-[minmax(5rem,0.75fr)_repeat(2,minmax(0,1fr))]'
    : 'grid-cols-[minmax(0,1fr)_minmax(7rem,auto)]'
)
const labelClass = computed(() => hasBothPricingTypes.value ? 'col-span-2 sm:col-span-1' : '')

const showTimeSchedule = computed(() =>
  props.scale !== PER_REQUEST_SCALE &&
  !!props.timeSchedule &&
  typeof props.timeSchedule.peak_multiplier === 'number' &&
  typeof props.timeSchedule.off_peak_multiplier === 'number'
)
// 两档都由倍率换算：随行的那份价既可能是空闲价（目录价），也可能是高峰价（官方兜底表）。
const standardPeak = computed(() =>
  scheduledScaledPrice(props.standardValue, props.timeSchedule?.peak_multiplier)
)
const standardOffPeak = computed(() =>
  scheduledScaledPrice(props.standardValue, props.timeSchedule?.off_peak_multiplier)
)
const vipPeak = computed(() =>
  scheduledScaledPrice(props.vipValue, props.timeSchedule?.peak_multiplier)
)
const vipOffPeak = computed(() =>
  scheduledScaledPrice(props.vipValue, props.timeSchedule?.off_peak_multiplier)
)

// 普通价与 VIP 价的结构完全一致，只有配色不同，用一份模板渲染两列。
const columns = computed(() => {
  const list = []
  if (props.standardAvailable) {
    list.push({
      key: 'standard',
      value: props.standardValue,
      billingRateMultiplier: props.standardBillingRateMultiplier,
      peak: standardPeak.value,
      offPeak: standardOffPeak.value,
      valueClass: 'text-gray-900 dark:text-white',
      secondaryClass: 'text-gray-600 dark:text-gray-300',
      unitClass: 'text-gray-400 dark:text-gray-500',
      tagClass: 'text-gray-400 dark:text-gray-500'
    })
  }
  if (props.vipAvailable) {
    list.push({
      key: 'vip',
      value: props.vipValue,
      billingRateMultiplier: props.vipBillingRateMultiplier,
      peak: vipPeak.value,
      offPeak: vipOffPeak.value,
      valueClass: 'text-orange-600 dark:text-orange-400',
      secondaryClass: 'text-orange-600/85 dark:text-orange-300',
      unitClass: 'text-orange-400 dark:text-orange-500/80',
      tagClass: 'text-orange-400/90 dark:text-orange-500/80'
    })
  }
  return list
})

const originalDisplay = (value: number | null) => formatScaled(value, props.scale)
const rechargedDisplay = (value: number | null, billingRateMultiplier?: number) =>
  formatCNYEffective(value, props.rate, props.scale, billingRateMultiplier)

const unitLabel = computed(() =>
  props.scale === PER_REQUEST_SCALE
    ? t('plaza.price.unitPerRequest')
    : t('plaza.price.unitPerMillion')
)
</script>
