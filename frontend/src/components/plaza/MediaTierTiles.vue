<template>
  <div :class="['grid gap-2', rows.length >= 3 ? 'grid-cols-3' : rows.length === 2 ? 'grid-cols-2' : 'grid-cols-1']">
    <div
      v-for="item in rows"
      :key="item.key"
      class="rounded-xl bg-gradient-to-b from-gray-50 to-white px-2 py-2.5 text-center ring-1 ring-gray-100 dark:from-dark-800/60 dark:to-dark-900 dark:ring-dark-800"
    >
      <p class="text-[10px] font-semibold tracking-wide text-gray-400">{{ rowLabel(t, item) }}</p>
      <p
        v-if="model.standardPricing"
        class="mt-1 truncate font-mono text-sm font-semibold tabular-nums text-gray-900 dark:text-white"
        :title="originalTitle(model.standardPricing, item)"
      >
        {{ effective(model.standardPricing, item) }}
      </p>
      <p
        v-if="model.vipPricing"
        :class="[
          'truncate font-mono tabular-nums text-orange-600 dark:text-orange-400',
          model.standardPricing ? 'text-[11px]' : 'mt-1 text-sm font-semibold'
        ]"
        :title="originalTitle(model.vipPricing, item)"
      >
        <template v-if="model.standardPricing">{{ t('plaza.price.vipLabel') }} </template>{{ effective(model.vipPricing, item) }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { rowLabel, unitScale, type PlazaPricingRow } from './plazaPricingRows'
import { formatCNYEffective, formatMoney } from '@/utils/pricing'
import type { AggregatedModel, PlazaPricingSummary } from '@/composables/useModelAggregation'

defineProps<{
  model: AggregatedModel
  rows: PlazaPricingRow[]
}>()

const { t } = useI18n()

function effective(summary: PlazaPricingSummary, item: PlazaPricingRow): string {
  return formatCNYEffective(
    summary.minPricing[item.key],
    unitScale(item.unit),
    summary.minPricingRateMultipliers[item.key]
  )
}

function originalTitle(summary: PlazaPricingSummary, item: PlazaPricingRow): string {
  return formatMoney(summary.minPricing[item.key], summary.minPricingCurrencies?.[item.key], unitScale(item.unit))
}
</script>
