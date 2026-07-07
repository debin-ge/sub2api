<template>
  <article
    :class="[
      'group relative flex h-full cursor-pointer flex-col rounded-2xl border bg-white p-5 shadow-sm transition-all duration-200 hover:-translate-y-0.5 hover:shadow-lg dark:bg-dark-900',
      cardBorderClass
    ]"
    tabindex="0"
    role="button"
    @click="$emit('open-detail', model)"
    @keydown.enter="$emit('open-detail', model)"
    @keydown.space.prevent="$emit('open-detail', model)"
  >
    <header class="mb-3 flex items-start gap-3">
      <div
        :class="[
          'flex h-9 w-9 shrink-0 items-center justify-center rounded-xl',
          platformBadgeLightClass(model.platform)
        ]"
        aria-hidden="true"
      >
        <PlatformIcon :platform="model.platform as GroupPlatform" size="lg" />
      </div>
      <div class="min-w-0 flex-1">
        <h3 class="truncate text-base font-semibold text-gray-900 dark:text-white">
          {{ model.displayName }}
        </h3>
      </div>
    </header>

    <div class="mb-4 flex flex-wrap gap-1.5">
      <span class="inline-flex items-center rounded-md bg-sky-50 px-2 py-0.5 text-xs font-medium text-sky-700 dark:bg-sky-900/30 dark:text-sky-300">
        {{ billingLabel }}
      </span>
      <span
        v-if="hasDiscount"
        class="inline-flex items-center rounded-md bg-emerald-50 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300"
      >
        {{ discountLabel }}
      </span>
    </div>

    <div class="grid flex-1 grid-cols-2 gap-x-4 gap-y-4">
      <PriceCell
        :label="t('plaza.card.input')"
        :value="model.minPricing.input"
        :scale="PER_MILLION_TOKEN_SCALE"
        :multiplier="multiplier"
        :rate="rate"
        :billing-rate-multiplier="model.bestRateMultiplier"
      />
      <PriceCell
        :label="t('plaza.card.output')"
        :value="model.minPricing.output"
        :scale="PER_MILLION_TOKEN_SCALE"
        :multiplier="multiplier"
        :rate="rate"
        :billing-rate-multiplier="model.bestRateMultiplier"
      />
      <PriceCell
        v-if="hasCacheWrite"
        :label="t('plaza.card.cacheWrite')"
        :value="model.minPricing.cacheWrite"
        :scale="PER_MILLION_TOKEN_SCALE"
        :multiplier="multiplier"
        :rate="rate"
        :billing-rate-multiplier="model.bestRateMultiplier"
      />
      <PriceCell
        v-if="hasCacheRead"
        :label="t('plaza.card.cacheRead')"
        :value="model.minPricing.cacheRead"
        :scale="PER_MILLION_TOKEN_SCALE"
        :multiplier="multiplier"
        :rate="rate"
        :billing-rate-multiplier="model.bestRateMultiplier"
      />
      <PriceCell
        v-if="hasImageOutput"
        :label="t('plaza.card.imageOutput')"
        :value="model.minPricing.imageOutput"
        :scale="PER_MILLION_TOKEN_SCALE"
        :multiplier="multiplier"
        :rate="rate"
        :billing-rate-multiplier="model.bestRateMultiplier"
      />
      <PriceCell
        v-if="hasPerRequest"
        :label="t('plaza.card.perRequest')"
        :value="model.minPricing.perRequest"
        :scale="PER_REQUEST_SCALE"
        :multiplier="multiplier"
        :rate="rate"
        :billing-rate-multiplier="model.bestRateMultiplier"
      />
    </div>

    <div class="mt-4 flex items-center justify-between gap-2 border-t border-gray-100 pt-3 text-xs dark:border-dark-800">
      <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-gray-500 dark:text-gray-400">
        <span class="truncate">
          {{ t('plaza.card.supportedChannels', { n: model.supportedGroups.length }) }}
        </span>
        <span
          v-if="model.recentCalls > 0"
          class="inline-flex items-center gap-1 text-orange-600 dark:text-orange-400"
        >
          <svg class="h-3 w-3 shrink-0" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
            <path fill-rule="evenodd" d="M12.395 2.553a1 1 0 00-1.45-.385c-.345.23-.614.558-.822.88-.214.33-.403.713-.57 1.116-.334.804-.614 1.768-.84 2.734a31.365 31.365 0 00-.613 3.58 2.64 2.64 0 01-.945-1.067c-.328-.68-.398-1.534-.398-2.654A1 1 0 005.05 6.05 6.981 6.981 0 003 11a7 7 0 1011.95-4.95c-.592-.591-.98-.985-1.348-1.467-.363-.476-.724-1.063-1.207-2.03zM12.12 15.12A3 3 0 017 13s.879.5 2.5.5c0-1 .5-4 1.25-4.5.5 1 .786 1.293 1.371 1.879A2.99 2.99 0 0113 13a2.99 2.99 0 01-.879 2.121z" clip-rule="evenodd" />
          </svg>
          {{ t('plaza.card.recentCalls', { count: formattedRecentCalls }) }}
        </span>
      </div>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import PriceCell from './PriceCell.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformBadgeLightClass, platformBorderClass } from '@/utils/platformColors'
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

const props = defineProps<{
  model: AggregatedModel
  multiplier: number
  rate: number
}>()

defineEmits<{
  'open-detail': [model: AggregatedModel]
}>()

const { t } = useI18n()

const discountFold = computed(() =>
  computeDiscountFold(props.multiplier, props.rate, props.model.bestRateMultiplier)
)
const discountPercent = computed(() =>
  computeDiscountPercent(props.multiplier, props.rate, props.model.bestRateMultiplier)
)
const hasDiscount = computed(() => discountPercent.value < 100)
const discountLabel = computed(() =>
  t('plaza.card.discountBadge', {
    discount: formatDiscountFold(discountFold.value),
    percent: discountPercent.value
  })
)

const formattedRecentCalls = computed(() => {
  const count = props.model.recentCalls
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K`
  return String(count)
})

const hasCacheWrite = computed(() => props.model.minPricing.cacheWrite != null)
const hasCacheRead = computed(() => props.model.minPricing.cacheRead != null)
const hasImageOutput = computed(() => props.model.minPricing.imageOutput != null)
const hasPerRequest = computed(() => props.model.minPricing.perRequest != null)

const isPerRequestOnly = computed(() =>
  hasPerRequest.value &&
  props.model.minPricing.input == null &&
  props.model.minPricing.output == null
)

const billingLabel = computed(() =>
  isPerRequestOnly.value ? t('plaza.card.billingPerRequest') : t('plaza.card.billingPerToken')
)

const cardBorderClass = computed(() =>
  `${platformBorderClass(props.model.platform)} hover:border-primary-300 dark:hover:border-primary-700`
)
</script>
