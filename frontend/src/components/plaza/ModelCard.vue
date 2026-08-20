<template>
  <article
    :class="[
      'group relative flex h-full cursor-pointer flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm transition-all duration-200 hover:-translate-y-0.5 hover:border-gray-300 hover:shadow-md focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:border-dark-800 dark:bg-dark-900 dark:hover:border-dark-700',
    ]"
    tabindex="0"
    role="button"
    @click="$emit('open-detail', model)"
    @keydown.enter="$emit('open-detail', model)"
    @keydown.space.prevent="$emit('open-detail', model)"
  >
    <!-- platform accent bar -->
    <div :class="['h-0.5 w-full shrink-0', platformAccentBarClass(model.platform)]" aria-hidden="true"></div>

    <header class="flex items-start gap-3 px-4 pb-3 pt-3.5">
      <div
        :class="[
          'flex h-9 w-9 shrink-0 items-center justify-center rounded-lg',
          platformBadgeLightClass(model.platform)
        ]"
        aria-hidden="true"
      >
        <PlatformIcon :platform="model.platform as GroupPlatform" size="lg" />
      </div>
      <div class="min-w-0 flex-1">
        <h3 class="truncate text-[15px] font-semibold leading-5 tracking-tight text-gray-900 dark:text-white">
          {{ model.displayName }}
        </h3>
        <div class="mt-1 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-[11px] leading-4 text-gray-500 dark:text-gray-400">
          <span class="font-medium text-gray-600 dark:text-gray-300">{{ platformLabel(model.platform) }}</span>
          <span class="text-gray-300 dark:text-dark-600" aria-hidden="true">·</span>
          <span>{{ billingLabel }}</span>
          <span
            v-if="hasDeepSeekTimePricing"
            class="inline-flex items-center gap-1 rounded-full bg-sky-50 px-1.5 py-px font-medium text-sky-700 dark:bg-sky-500/10 dark:text-sky-300"
          >
            <span class="h-1 w-1 rounded-full bg-sky-500" aria-hidden="true"></span>
            {{ t('plaza.card.deepSeekTimePricing') }}
          </span>
          <span
            v-if="hasPeakPricing"
            class="inline-flex items-center gap-1 rounded-full bg-amber-50 px-1.5 py-px font-medium text-amber-700 dark:bg-amber-500/10 dark:text-amber-300"
          >
            <span class="h-1 w-1 rounded-full bg-amber-500" aria-hidden="true"></span>
            {{ t('plaza.card.peakPricing') }}
          </span>
        </div>
      </div>
    </header>

    <div>
      <div :class="['grid items-center gap-x-3 border-y border-gray-100 bg-gray-50/70 px-3 py-1.5 dark:border-dark-800 dark:bg-dark-800/30', pricingGridClass]">
        <div :class="pricingHeaderSpacerClass"></div>
        <div v-if="model.standardPricing" class="min-w-0 text-right">
          <div class="text-[11px] font-semibold leading-4 text-gray-600 dark:text-gray-300">
            {{ t('plaza.price.standardLabel') }}
          </div>
          <div v-if="hasStandardDiscount" class="truncate text-[10px] leading-4 tabular-nums text-emerald-600 dark:text-emerald-400">
            {{ standardDiscountLabel }}
          </div>
        </div>
        <div v-if="model.vipPricing" class="min-w-0 text-right">
          <div class="text-[11px] font-semibold leading-4 text-orange-600 dark:text-orange-400">
            {{ t('plaza.price.vipLabel') }}
          </div>
          <div v-if="hasVipDiscount" class="truncate text-[10px] leading-4 tabular-nums text-orange-500 dark:text-orange-400/90">
            {{ vipDiscountLabel }}
          </div>
        </div>
      </div>
      <div class="divide-y divide-gray-100 dark:divide-dark-800/70">
        <PriceCell
          :label="t('plaza.card.input')"
          :scale="PER_MILLION_TOKEN_SCALE"
          :multiplier="multiplier"
          :standard-available="model.standardPricing != null"
          :standard-value="model.standardPricing?.minPricing.input ?? null"
          :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers.input"
          :vip-available="model.vipPricing != null"
          :vip-value="model.vipPricing?.minPricing.input ?? null"
          :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers.input"
          :time-schedule="model.timeSchedule"
        />
        <PriceCell
          :label="t('plaza.card.output')"
          :scale="PER_MILLION_TOKEN_SCALE"
          :multiplier="multiplier"
          :standard-available="model.standardPricing != null"
          :standard-value="model.standardPricing?.minPricing.output ?? null"
          :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers.output"
          :vip-available="model.vipPricing != null"
          :vip-value="model.vipPricing?.minPricing.output ?? null"
          :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers.output"
          :time-schedule="model.timeSchedule"
        />
        <PriceCell
          v-if="hasCacheWrite"
          :label="t('plaza.card.cacheWrite')"
          :scale="PER_MILLION_TOKEN_SCALE"
          :multiplier="multiplier"
          :standard-available="model.standardPricing != null"
          :standard-value="model.standardPricing?.minPricing.cacheWrite ?? null"
          :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers.cacheWrite"
          :vip-available="model.vipPricing != null"
          :vip-value="model.vipPricing?.minPricing.cacheWrite ?? null"
          :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers.cacheWrite"
          :time-schedule="model.timeSchedule"
        />
        <PriceCell
          v-if="hasCacheRead"
          :label="t('plaza.card.cacheRead')"
          :scale="PER_MILLION_TOKEN_SCALE"
          :multiplier="multiplier"
          :standard-available="model.standardPricing != null"
          :standard-value="model.standardPricing?.minPricing.cacheRead ?? null"
          :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers.cacheRead"
          :vip-available="model.vipPricing != null"
          :vip-value="model.vipPricing?.minPricing.cacheRead ?? null"
          :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers.cacheRead"
          :time-schedule="model.timeSchedule"
        />
        <PriceCell
          v-if="hasImageOutput"
          :label="t('plaza.card.imageOutput')"
          :scale="PER_MILLION_TOKEN_SCALE"
          :multiplier="multiplier"
          :standard-available="model.standardPricing != null"
          :standard-value="model.standardPricing?.minPricing.imageOutput ?? null"
          :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers.imageOutput"
          :vip-available="model.vipPricing != null"
          :vip-value="model.vipPricing?.minPricing.imageOutput ?? null"
          :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers.imageOutput"
          :time-schedule="model.timeSchedule"
        />
        <PriceCell
          v-if="hasPerRequest"
          :label="t('plaza.card.perRequest')"
          :scale="PER_REQUEST_SCALE"
          :multiplier="multiplier"
          :standard-available="model.standardPricing != null"
          :standard-value="model.standardPricing?.minPricing.perRequest ?? null"
          :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers.perRequest"
          :vip-available="model.vipPricing != null"
          :vip-value="model.vipPricing?.minPricing.perRequest ?? null"
          :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers.perRequest"
        />
      </div>
    </div>

    <div class="mt-auto flex items-center justify-between gap-2 border-t border-gray-100 px-4 py-2.5 dark:border-dark-800">
      <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
        <span class="text-[11px] leading-4 tabular-nums text-gray-400 dark:text-gray-500">
          {{ t('plaza.card.supportedChannels', { n: model.supportedGroups.length }) }}
        </span>
        <span
          v-if="model.recentCalls > 0"
          class="inline-flex items-center gap-1.5 text-[11px] leading-4 tabular-nums text-gray-500 dark:text-gray-400"
        >
          <span class="relative flex h-1.5 w-1.5 shrink-0" aria-hidden="true">
            <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary-400 opacity-60"></span>
            <span class="relative inline-flex h-1.5 w-1.5 rounded-full bg-primary-500"></span>
          </span>
          {{ t('plaza.card.recentCalls', { count: formattedRecentCalls }) }}
        </span>
      </div>
      <span class="inline-flex shrink-0 items-center text-gray-300 transition-all duration-200 group-hover:translate-x-0.5 group-hover:text-primary-500 dark:text-dark-600 dark:group-hover:text-primary-400" aria-hidden="true">
        <svg class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
          <path fill-rule="evenodd" d="M3 10a1 1 0 011-1h9.586L10.293 5.707a1 1 0 011.414-1.414l5 5a1 1 0 010 1.414l-5 5a1 1 0 01-1.414-1.414L13.586 11H4a1 1 0 01-1-1z" clip-rule="evenodd" />
        </svg>
      </span>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import PriceCell from './PriceCell.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformAccentBarClass, platformBadgeLightClass, platformLabel } from '@/utils/platformColors'
import {
  PER_MILLION_TOKEN_SCALE,
  PER_REQUEST_SCALE,
  computeDiscountFold,
  computeDiscountPercent,
  formatDiscountFold
} from '@/utils/pricing'
import type { AggregatedModel } from '@/composables/useModelAggregation'
import type { GroupPlatform } from '@/types'
import { useI18n } from 'vue-i18n'
import { hasPeakRate } from '@/utils/peak-rate'

const props = defineProps<{
  model: AggregatedModel
  multiplier: number
  rate: number
}>()

defineEmits<{
  'open-detail': [model: AggregatedModel]
}>()

const { t } = useI18n()

const standardDiscountFold = computed(() =>
  computeDiscountFold(props.multiplier, props.rate, props.model.standardPricing?.displayRateMultiplier)
)
const standardDiscountPercent = computed(() =>
  computeDiscountPercent(props.multiplier, props.rate, props.model.standardPricing?.displayRateMultiplier)
)
const vipDiscountFold = computed(() =>
  computeDiscountFold(props.multiplier, props.rate, props.model.vipPricing?.displayRateMultiplier)
)
const vipDiscountPercent = computed(() =>
  computeDiscountPercent(props.multiplier, props.rate, props.model.vipPricing?.displayRateMultiplier)
)
const hasAnyPrice = (pricing: AggregatedModel['standardPricing']) =>
  pricing != null && Object.values(pricing.minPricing).some((value) => value != null)
const hasStandardDiscount = computed(() =>
  hasAnyPrice(props.model.standardPricing) && standardDiscountPercent.value < 100
)
const hasVipDiscount = computed(() =>
  hasAnyPrice(props.model.vipPricing) && vipDiscountPercent.value < 100
)
const hasPeakPricing = computed(() =>
  props.model.supportedGroups.some((item) => hasPeakRate(item.group))
)
const hasDeepSeekTimePricing = computed(() =>
  props.model.timeSchedule?.kind === 'deepseek_official'
)
const standardDiscountLabel = computed(() =>
  t('plaza.card.discountBadge', {
    discount: formatDiscountFold(standardDiscountFold.value),
    percent: standardDiscountPercent.value
  })
)
const vipDiscountLabel = computed(() =>
  t('plaza.card.discountBadge', {
    discount: formatDiscountFold(vipDiscountFold.value),
    percent: vipDiscountPercent.value
  })
)

const formattedRecentCalls = computed(() => {
  const count = props.model.recentCalls
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K`
  return String(count)
})

const hasDimension = (key: keyof NonNullable<AggregatedModel['standardPricing']>['minPricing']) =>
  props.model.standardPricing?.minPricing[key] != null ||
  props.model.vipPricing?.minPricing[key] != null

const hasCacheWrite = computed(() => hasDimension('cacheWrite'))
const hasCacheRead = computed(() => hasDimension('cacheRead'))
const hasImageOutput = computed(() => hasDimension('imageOutput'))
const hasPerRequest = computed(() => hasDimension('perRequest'))

const isPerRequestOnly = computed(() =>
  hasPerRequest.value &&
  !hasDimension('input') &&
  !hasDimension('output')
)

const billingLabel = computed(() =>
  isPerRequestOnly.value ? t('plaza.card.billingPerRequest') : t('plaza.card.billingPerToken')
)

const hasBothPricingTypes = computed(() =>
  props.model.standardPricing != null && props.model.vipPricing != null
)
const pricingGridClass = computed(() =>
  hasBothPricingTypes.value
    ? 'grid-cols-2 sm:grid-cols-[minmax(5rem,0.75fr)_repeat(2,minmax(0,1fr))]'
    : 'grid-cols-[minmax(0,1fr)_minmax(7rem,auto)]'
)
const pricingHeaderSpacerClass = computed(() => hasBothPricingTypes.value ? 'hidden sm:block' : '')
</script>
