<template>
  <Teleport to="body">
    <div v-if="open && model" class="fixed inset-0 z-50 flex items-end justify-center md:items-center">
      <button
        class="absolute inset-0 h-full w-full bg-black/60 backdrop-blur-sm"
        type="button"
        :aria-label="t('plaza.modal.close')"
        @click="$emit('close')"
      />
      <section
        ref="dialogRef"
        class="relative flex max-h-[92vh] w-full max-w-3xl flex-col overflow-hidden rounded-t-2xl bg-white shadow-2xl dark:bg-dark-900 md:m-6 md:rounded-2xl"
        role="dialog"
        aria-modal="true"
        aria-labelledby="plaza-modal-title"
        tabindex="-1"
      >
        <header class="relative border-b border-gray-100 px-6 pb-4 pt-6 dark:border-dark-800">
          <div class="flex items-start gap-4">
            <div
              :class="[
                'flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl',
                platformBadgeLightClass(model.platform)
              ]"
              aria-hidden="true"
            >
              <PlatformIcon :platform="model.platform as GroupPlatform" size="lg" />
            </div>
            <div class="min-w-0 flex-1">
              <h2 id="plaza-modal-title" class="truncate text-lg font-semibold text-gray-900 dark:text-white sm:text-xl">
                {{ model.displayName }}
              </h2>
              <p class="mt-0.5 flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                <span class="truncate">{{ platformLabel(model.platform) }}</span>
                <span class="text-gray-300 dark:text-gray-600">·</span>
                <span class="truncate">{{ t('plaza.modal.channelsCount', { n: model.supportedGroups.length }) }}</span>
              </p>
              <div class="mt-3 flex flex-wrap gap-1.5">
                <span class="inline-flex items-center rounded-md bg-sky-50 px-2 py-0.5 text-xs font-medium text-sky-700 dark:bg-sky-900/30 dark:text-sky-300">
                  {{ billingLabel }}
                </span>
                <span
                  v-if="hasPeakPricing"
                  class="inline-flex items-center rounded-md bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
                >
                  {{ t('plaza.card.peakPricing') }}
                </span>
                <span
                  v-if="model.recentCalls > 0"
                  class="inline-flex items-center gap-1 rounded-md bg-orange-50 px-2 py-0.5 text-xs font-medium text-orange-700 dark:bg-orange-900/30 dark:text-orange-300"
                >
                  <svg class="h-3 w-3" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
                    <path fill-rule="evenodd" d="M12.395 2.553a1 1 0 00-1.45-.385c-.345.23-.614.558-.822.88-.214.33-.403.713-.57 1.116-.334.804-.614 1.768-.84 2.734a31.365 31.365 0 00-.613 3.58 2.64 2.64 0 01-.945-1.067c-.328-.68-.398-1.534-.398-2.654A1 1 0 005.05 6.05 6.981 6.981 0 003 11a7 7 0 1011.95-4.95c-.592-.591-.98-.985-1.348-1.467-.363-.476-.724-1.063-1.207-2.03zM12.12 15.12A3 3 0 017 13s.879.5 2.5.5c0-1 .5-4 1.25-4.5.5 1 .786 1.293 1.371 1.879A2.99 2.99 0 0113 13a2.99 2.99 0 01-.879 2.121z" clip-rule="evenodd" />
                  </svg>
                  {{ t('plaza.card.recentCalls', { count: formatCallCount(model.recentCalls) }) }}
                </span>
              </div>
            </div>
            <button
              ref="closeButtonRef"
              data-testid="plaza-modal-close"
              class="shrink-0 rounded-full p-2 text-gray-400 transition hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-800 dark:hover:text-white"
              type="button"
              :aria-label="t('plaza.modal.close')"
              @click="$emit('close')"
            >
              <svg class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
                <path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd" />
              </svg>
              <span class="sr-only">{{ t('plaza.modal.close') }}</span>
            </button>
          </div>
        </header>

        <div class="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          <section>
            <h3 class="mb-3 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
              <span class="inline-block h-4 w-1 rounded-full bg-primary-500" aria-hidden="true"></span>
              {{ t('plaza.modal.fullPricing') }}
            </h3>
            <div class="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-900/40">
              <div :class="['grid items-center gap-x-3 border-b border-gray-100 bg-gray-50 px-3 py-2.5 dark:border-dark-800 dark:bg-dark-800/40', pricingGridClass]">
                <div :class="pricingHeaderSpacerClass"></div>
                <div v-if="model.standardPricing" class="min-w-0 text-right">
                  <div class="text-xs font-semibold text-gray-700 dark:text-gray-200">
                    {{ t('plaza.price.standardPricing') }}
                  </div>
                  <div v-if="hasStandardDiscount" class="mt-0.5 truncate text-[10px] text-emerald-600 dark:text-emerald-400">
                    {{ standardDiscountLabel }}
                  </div>
                </div>
                <div v-if="model.vipPricing" class="min-w-0 border-l border-orange-200 pl-3 text-right dark:border-orange-900/60">
                  <div class="text-xs font-semibold text-orange-700 dark:text-orange-400">
                    {{ t('plaza.price.vipPricing') }}
                  </div>
                  <div v-if="hasVipDiscount" class="mt-0.5 truncate text-[10px] text-orange-500 dark:text-orange-400/90">
                    {{ vipDiscountLabel }}
                  </div>
                </div>
              </div>
              <div class="divide-y divide-gray-100 dark:divide-dark-800">
                <PriceCell
                  :label="t('plaza.modal.input')"
                  :scale="PER_MILLION_TOKEN_SCALE"
                  :multiplier="multiplier"
                  :standard-available="model.standardPricing != null"
                  :standard-value="model.standardPricing?.minPricing.input ?? null"
                  :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers.input"
                  :vip-available="model.vipPricing != null"
                  :vip-value="model.vipPricing?.minPricing.input ?? null"
                  :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers.input"
                />
                <PriceCell
                  :label="t('plaza.modal.output')"
                  :scale="PER_MILLION_TOKEN_SCALE"
                  :multiplier="multiplier"
                  :standard-available="model.standardPricing != null"
                  :standard-value="model.standardPricing?.minPricing.output ?? null"
                  :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers.output"
                  :vip-available="model.vipPricing != null"
                  :vip-value="model.vipPricing?.minPricing.output ?? null"
                  :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers.output"
                />
                <PriceCell
                  :label="t('plaza.modal.cacheWrite')"
                  :scale="PER_MILLION_TOKEN_SCALE"
                  :multiplier="multiplier"
                  :standard-available="model.standardPricing != null"
                  :standard-value="model.standardPricing?.minPricing.cacheWrite ?? null"
                  :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers.cacheWrite"
                  :vip-available="model.vipPricing != null"
                  :vip-value="model.vipPricing?.minPricing.cacheWrite ?? null"
                  :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers.cacheWrite"
                />
                <PriceCell
                  :label="t('plaza.modal.cacheRead')"
                  :scale="PER_MILLION_TOKEN_SCALE"
                  :multiplier="multiplier"
                  :standard-available="model.standardPricing != null"
                  :standard-value="model.standardPricing?.minPricing.cacheRead ?? null"
                  :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers.cacheRead"
                  :vip-available="model.vipPricing != null"
                  :vip-value="model.vipPricing?.minPricing.cacheRead ?? null"
                  :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers.cacheRead"
                />
                <PriceCell
                  :label="t('plaza.modal.imageOutput')"
                  :scale="PER_MILLION_TOKEN_SCALE"
                  :multiplier="multiplier"
                  :standard-available="model.standardPricing != null"
                  :standard-value="model.standardPricing?.minPricing.imageOutput ?? null"
                  :standard-billing-rate-multiplier="model.standardPricing?.minPricingRateMultipliers.imageOutput"
                  :vip-available="model.vipPricing != null"
                  :vip-value="model.vipPricing?.minPricing.imageOutput ?? null"
                  :vip-billing-rate-multiplier="model.vipPricing?.minPricingRateMultipliers.imageOutput"
                />
                <PriceCell
                  :label="t('plaza.modal.perRequest')"
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
            <p v-if="hasPeakPricing" class="mt-3 text-xs text-amber-700 dark:text-amber-300">
              {{ t('plaza.modal.basePricePeakNote') }}
            </p>
          </section>

          <section v-if="tieredGroups.length > 0">
            <h3 class="mb-3 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
              <span class="inline-block h-4 w-1 rounded-full bg-amber-500" aria-hidden="true"></span>
              {{ t('plaza.modal.tieredPricing') }}
            </h3>
            <div class="space-y-3">
              <div
                v-for="item in tieredGroups"
                :key="`${item.channelName}-${item.group.id}-tiers`"
                class="rounded-xl border border-gray-200 bg-gray-50/50 p-4 dark:border-dark-800 dark:bg-dark-900/40"
              >
                <div class="mb-3 flex items-center gap-2">
                  <span class="text-sm font-medium text-gray-900 dark:text-white">{{ item.channelName }}</span>
                  <span
                    :class="[
                      'inline-flex items-center rounded-md border px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide',
                      platformBadgeClass(model.platform)
                    ]"
                  >
                    {{ item.group.name }}
                  </span>
                  <span
                    v-if="item.group.vip_only === true"
                    class="inline-flex items-center rounded-md bg-orange-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-orange-700 dark:bg-orange-900/50 dark:text-orange-300"
                  >
                    {{ t('plaza.price.vipLabel') }}
                  </span>
                </div>
                <div class="space-y-4">
                  <div
                    v-for="(interval, index) in item.pricing.intervals"
                    :key="`${item.channelName}-${item.group.id}-${index}-${interval.min_tokens}`"
                    class="space-y-2 rounded-lg border border-gray-100 bg-white p-3 dark:border-dark-800 dark:bg-dark-900"
                  >
                    <div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
                      <span v-if="interval.tier_label" class="rounded-md bg-primary-50 px-2 py-0.5 text-xs font-semibold text-primary-700 dark:bg-primary-500/15 dark:text-primary-300">
                        {{ interval.tier_label }}
                      </span>
                      <span class="text-xs text-gray-500 dark:text-gray-400">
                        {{ formatTierRange(interval.min_tokens, interval.max_tokens) }}
                      </span>
                    </div>
                    <div class="grid gap-2 md:grid-cols-2">
                      <PriceLine
                        :label="t('plaza.modal.input')"
                        :value="interval.input_price"
                        :scale="PER_MILLION_TOKEN_SCALE"
                        :multiplier="multiplier"
                        :rate="rate"
                        :billing-rate-multiplier="item.group.rate_multiplier"
                      />
                      <PriceLine
                        :label="t('plaza.modal.output')"
                        :value="interval.output_price"
                        :scale="PER_MILLION_TOKEN_SCALE"
                        :multiplier="multiplier"
                        :rate="rate"
                        :billing-rate-multiplier="item.group.rate_multiplier"
                      />
                      <PriceLine
                        :label="t('plaza.modal.cacheWrite')"
                        :value="interval.cache_write_price"
                        :scale="PER_MILLION_TOKEN_SCALE"
                        :multiplier="multiplier"
                        :rate="rate"
                        :billing-rate-multiplier="item.group.rate_multiplier"
                      />
                      <PriceLine
                        :label="t('plaza.modal.cacheRead')"
                        :value="interval.cache_read_price"
                        :scale="PER_MILLION_TOKEN_SCALE"
                        :multiplier="multiplier"
                        :rate="rate"
                        :billing-rate-multiplier="item.group.rate_multiplier"
                      />
                      <PriceLine
                        :label="t('plaza.modal.imageOutput')"
                        :value="item.pricing.image_output_price"
                        :scale="PER_MILLION_TOKEN_SCALE"
                        :multiplier="multiplier"
                        :rate="rate"
                        :billing-rate-multiplier="item.group.rate_multiplier"
                      />
                      <PriceLine
                        :label="t('plaza.modal.perRequest')"
                        :value="interval.per_request_price"
                        :scale="PER_REQUEST_SCALE"
                        :multiplier="multiplier"
                        :rate="rate"
                        :billing-rate-multiplier="requestRateMultiplier(item)"
                      />
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </section>

          <section>
            <h3 class="mb-3 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
              <span class="inline-block h-4 w-1 rounded-full bg-emerald-500" aria-hidden="true"></span>
              {{ t('plaza.modal.supportedChannels') }}
            </h3>
            <div class="grid gap-2 sm:grid-cols-2">
              <div
                v-for="item in sortedGroups"
                :key="`${item.channelName}-${item.group.id}`"
                class="flex items-start gap-3 rounded-xl border border-gray-200 bg-white p-3 dark:border-dark-800 dark:bg-dark-900/40"
              >
                <div
                  :class="[
                    'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                    platformBadgeLightClass(model.platform)
                  ]"
                  aria-hidden="true"
                >
                  <PlatformIcon :platform="model.platform as GroupPlatform" size="md" />
                </div>
                <div class="min-w-0 flex-1">
                  <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.channelName }}</div>
                  <div v-if="item.channelDescription" class="mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400">
                    {{ item.channelDescription }}
                  </div>
                  <div class="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
                    <span class="rounded-md bg-gray-100 px-1.5 py-0.5 dark:bg-dark-800">{{ item.group.name }}</span>
                    <span
                      v-if="item.group.vip_only === true"
                      class="rounded-md bg-orange-100 px-1.5 py-0.5 font-semibold text-orange-700 dark:bg-orange-900/50 dark:text-orange-300"
                    >
                      {{ t('plaza.price.vipLabel') }}
                    </span>
                    <span>×{{ item.group.rate_multiplier || 1 }}</span>
                    <span
                      v-if="hasPeakRate(item.group)"
                      class="rounded-md bg-amber-50 px-1.5 py-0.5 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
                    >
                      {{ formatPeakRateWindow(item.group, serverTimezone) }}
                    </span>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PriceCell from './PriceCell.vue'
import PriceLine from './PriceLine.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import {
  PER_MILLION_TOKEN_SCALE,
  PER_REQUEST_SCALE,
  computeDiscountFold,
  computeDiscountPercent,
  formatDiscountFold
} from '@/utils/pricing'
import {
  platformBadgeClass,
  platformBadgeLightClass,
  platformLabel
} from '@/utils/platformColors'
import type {
  AggregatedModel,
  PlazaPricingSummary,
  PlazaSupportedGroup
} from '@/composables/useModelAggregation'
import type { UserPricingInterval } from '@/api/channels'
import type { GroupPlatform } from '@/types'
import { formatPeakRateWindow, hasPeakRate, serverTimezoneLabel } from '@/utils/peak-rate'

const props = defineProps<{
  open: boolean
  model: AggregatedModel | null
  multiplier: number
  rate: number
  serverUtcOffset?: string
}>()

const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()

const dialogRef = ref<HTMLElement | null>(null)
const closeButtonRef = ref<HTMLButtonElement | null>(null)
const previousFocus = ref<HTMLElement | null>(null)

const sortedGroups = computed(() =>
  [...(props.model?.supportedGroups ?? [])].sort(
    (a, b) => {
      const typeDifference = Number(a.group.vip_only === true) - Number(b.group.vip_only === true)
      return typeDifference || (a.group.rate_multiplier || 1) - (b.group.rate_multiplier || 1)
    }
  )
)

const tieredGroups = computed(() =>
  sortedGroups.value
    .filter((item) => item.pricing?.intervals?.length)
    .map((item) => ({
      ...item,
      pricing: {
        ...item.pricing!,
        intervals: [...item.pricing!.intervals].sort(
          (a, b) => a.min_tokens - b.min_tokens
        )
      }
    }))
)

const pricingSummaries = computed<PlazaPricingSummary[]>(() =>
  [props.model?.standardPricing, props.model?.vipPricing]
    .filter((pricing): pricing is PlazaPricingSummary => pricing != null)
)

const standardDiscountFold = computed(() =>
  computeDiscountFold(props.multiplier, props.rate, props.model?.standardPricing?.displayRateMultiplier)
)
const standardDiscountPercent = computed(() =>
  computeDiscountPercent(props.multiplier, props.rate, props.model?.standardPricing?.displayRateMultiplier)
)
const vipDiscountFold = computed(() =>
  computeDiscountFold(props.multiplier, props.rate, props.model?.vipPricing?.displayRateMultiplier)
)
const vipDiscountPercent = computed(() =>
  computeDiscountPercent(props.multiplier, props.rate, props.model?.vipPricing?.displayRateMultiplier)
)
const hasAnyPrice = (pricing: PlazaPricingSummary | null | undefined) =>
  pricing != null && Object.values(pricing.minPricing).some((value) => value != null)
const hasStandardDiscount = computed(() =>
  hasAnyPrice(props.model?.standardPricing) && standardDiscountPercent.value < 100
)
const hasVipDiscount = computed(() =>
  hasAnyPrice(props.model?.vipPricing) && vipDiscountPercent.value < 100
)
const hasPeakPricing = computed(() =>
  props.model?.supportedGroups.some((item) => hasPeakRate(item.group)) === true
)
const serverTimezone = computed(() => serverTimezoneLabel(props.serverUtcOffset))
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
const isPerRequestOnly = computed(() =>
  pricingSummaries.value.some((pricing) => pricing.minPricing.perRequest != null) &&
  pricingSummaries.value.every((pricing) =>
    pricing.minPricing.input == null && pricing.minPricing.output == null
  )
)
const billingLabel = computed(() =>
  isPerRequestOnly.value ? t('plaza.card.billingPerRequest') : t('plaza.card.billingPerToken')
)

const hasBothPricingTypes = computed(() =>
  props.model?.standardPricing != null && props.model.vipPricing != null
)
const pricingGridClass = computed(() =>
  hasBothPricingTypes.value
    ? 'grid-cols-2 sm:grid-cols-[minmax(5rem,0.75fr)_repeat(2,minmax(0,1fr))]'
    : 'grid-cols-[minmax(0,1fr)_minmax(7rem,auto)]'
)
const pricingHeaderSpacerClass = computed(() => hasBothPricingTypes.value ? 'hidden sm:block' : '')

const tokenFormatter = new Intl.NumberFormat()

function formatTierRange(minTokens: UserPricingInterval['min_tokens'], maxTokens: UserPricingInterval['max_tokens']) {
  const min = tokenFormatter.format(minTokens)
  if (maxTokens == null) {
    return t('plaza.modal.tierRangeOpenEnded', { min })
  }
  return t('plaza.modal.tierRange', { min, max: tokenFormatter.format(maxTokens) })
}

function requestRateMultiplier(item: PlazaSupportedGroup): number {
  if (item.pricing?.billing_mode !== 'image' || item.group.image_rate_independent !== true) {
    return item.group.rate_multiplier
  }
  const rate = item.group.image_rate_multiplier
  return typeof rate === 'number' && Number.isFinite(rate) && rate >= 0 ? rate : 1
}

function formatCallCount(count: number): string {
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K`
  return String(count)
}

function restorePreviousFocus() {
  if (previousFocus.value?.isConnected) {
    previousFocus.value.focus()
  }
  previousFocus.value = null
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    emit('close')
  }
}

watch(
  () => props.open && !!props.model,
  async (isOpen, wasOpen) => {
    if (isOpen) {
      previousFocus.value = document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null
      document.addEventListener('keydown', handleKeydown)
      await nextTick()
      ;(closeButtonRef.value ?? dialogRef.value)?.focus()
      return
    }

    if (wasOpen) {
      document.removeEventListener('keydown', handleKeydown)
      restorePreviousFocus()
    }
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleKeydown)
  restorePreviousFocus()
})
</script>
