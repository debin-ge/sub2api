<template>
  <div class="overflow-hidden rounded-xl border border-gray-100 dark:border-dark-800">
    <div :class="['grid items-center gap-x-3 bg-gray-50 px-3 py-1.5 dark:bg-dark-800/50', gridClass]">
      <div :class="['min-w-0 truncate text-[11px] font-semibold text-gray-400 dark:text-gray-500', captionClass]">
        {{ caption }}
      </div>
      <div v-if="model.standardPricing" class="min-w-0 text-right">
        <div class="text-[11px] font-semibold leading-4 text-gray-600 dark:text-gray-300">
          {{ t('plaza.price.standardLabel') }}
        </div>
        <div v-if="standardDiscount" class="truncate text-[10px] leading-4 tabular-nums text-emerald-600 dark:text-emerald-400">
          {{ standardDiscount }}
        </div>
      </div>
      <div v-if="model.vipPricing" class="min-w-0 text-right">
        <div class="text-[11px] font-semibold leading-4 text-orange-600 dark:text-orange-400">
          {{ t('plaza.price.vipLabel') }}
        </div>
        <div v-if="vipDiscount" class="truncate text-[10px] leading-4 tabular-nums text-orange-500 dark:text-orange-400/90">
          {{ vipDiscount }}
        </div>
      </div>
    </div>
    <div class="divide-y divide-gray-100 dark:divide-dark-800/70">
      <PriceCell
        v-for="item in rows"
        :key="item.key"
        :label="rowLabel(t, item)"
        :scale="unitScale(item.unit)"
        :unit="item.unit"
        :standard-currency="model.standardPricing?.minPricingCurrencies?.[item.key]"
        :standard-available="model.standardPricing != null"
        :standard-value="model.standardPricing?.minPricing[item.key] ?? null"
        :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers[item.key]"
        :vip-available="model.vipPricing != null"
        :vip-value="model.vipPricing?.minPricing[item.key] ?? null"
        :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers[item.key]"
        :vip-currency="model.vipPricing?.minPricingCurrencies?.[item.key]"
        :time-schedule="item.unit === 'million' ? model.timeSchedule : undefined"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PriceCell from './PriceCell.vue'
import { rowLabel, rowsRateMultiplier, unitScale, type PlazaPricingRow } from './plazaPricingRows'
import { computeDiscountFold, computeDiscountPercent, formatDiscountFold } from '@/utils/pricing'
import type { AggregatedModel, PlazaPricingSummary } from '@/composables/useModelAggregation'

const props = defineProps<{
  model: AggregatedModel
  rows: PlazaPricingRow[]
  caption: string
}>()

const { t } = useI18n()

const hasBothPricingTypes = computed(() =>
  props.model.standardPricing != null && props.model.vipPricing != null
)
const gridClass = computed(() =>
  hasBothPricingTypes.value
    ? 'grid-cols-2 sm:grid-cols-[minmax(5rem,0.75fr)_repeat(2,minmax(0,1fr))]'
    : 'grid-cols-[minmax(0,1fr)_minmax(7rem,auto)]'
)
const captionClass = computed(() => hasBothPricingTypes.value ? 'hidden sm:block' : '')

function discountLabel(summary: PlazaPricingSummary | null): string {
  const rate = rowsRateMultiplier(summary, props.rows)
  if (rate == null || computeDiscountPercent(rate) >= 100) return ''
  return t('plaza.card.discountBadge', {
    discount: formatDiscountFold(computeDiscountFold(rate)),
    percent: computeDiscountPercent(rate)
  })
}

const standardDiscount = computed(() => discountLabel(props.model.standardPricing))
const vipDiscount = computed(() => discountLabel(props.model.vipPricing))
</script>
