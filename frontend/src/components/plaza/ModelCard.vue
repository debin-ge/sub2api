<template>
  <article
    :class="[
      'group relative flex h-full cursor-pointer flex-col overflow-hidden rounded-2xl border border-gray-200 bg-white transition-all duration-200 hover:-translate-y-0.5 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:border-dark-800 dark:bg-dark-900',
      kindStyle.hover
    ]"
    :data-billing-kind="model.billingKind"
    tabindex="0"
    role="button"
    @click="$emit('open-detail', model)"
    @keydown.enter="$emit('open-detail', model)"
    @keydown.space.prevent="$emit('open-detail', model)"
  >
    <div :class="['h-0.5 w-full shrink-0', kindStyle.accentBar]" aria-hidden="true"></div>

    <header class="flex items-start gap-3 px-5 pt-4">
      <div
        :class="['flex h-10 w-10 shrink-0 items-center justify-center rounded-xl', platformBadgeLightClass(model.platform)]"
        aria-hidden="true"
      >
        <PlatformIcon :platform="model.platform as GroupPlatform" size="lg" />
      </div>
      <div class="min-w-0 flex-1">
        <h3 class="truncate text-[15px] font-semibold leading-5 tracking-tight text-gray-900 dark:text-white">
          {{ model.displayName }}
        </h3>
        <div class="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] leading-4 text-gray-500 dark:text-gray-400">
          <span class="font-medium text-gray-600 dark:text-gray-300">{{ platformLabel(model.platform) }}</span>
          <span :class="['inline-flex items-center gap-1 rounded-full px-1.5 py-px font-medium', kindStyle.badge]">
            <span :class="['h-1 w-1 rounded-full', kindStyle.dot]" aria-hidden="true"></span>
            {{ t(kindStyle.labelKey) }}
          </span>
          <span v-if="billingHint" class="text-gray-400">{{ billingHint }}</span>
          <span
            v-if="hasDeepSeekTimePricing"
            class="inline-flex items-center gap-1 rounded-full bg-sky-50 px-1.5 py-px font-medium text-sky-700 dark:bg-sky-500/10 dark:text-sky-300"
          >
            {{ t('plaza.card.deepSeekTimePricing') }}
          </span>
          <span
            v-if="hasPeakPricing"
            class="inline-flex items-center gap-1 rounded-full bg-amber-50 px-1.5 py-px font-medium text-amber-700 dark:bg-amber-500/10 dark:text-amber-300"
          >
            {{ t('plaza.card.peakPricing') }}
          </span>
        </div>
      </div>
    </header>

    <!-- 视频：分辨率 × 每秒 -->
    <template v-if="model.billingKind === 'video'">
      <div v-if="videoRows.length" class="mx-5 mt-4">
        <PricingTable :model="model" :rows="videoRows" :caption="t('plaza.card.videoCaption')" />
      </div>
      <div
        v-if="videoExample"
        data-testid="plaza-video-example"
        class="mx-5 mt-3 flex items-center justify-between gap-3 rounded-lg bg-amber-50/70 px-3 py-2 text-[12px] dark:bg-amber-500/10"
      >
        <span class="text-amber-800 dark:text-amber-200">
          {{ t('plaza.card.videoExample', { seconds: VIDEO_EXAMPLE_SECONDS, tier: videoExample.tier }) }}
        </span>
        <span class="font-mono font-semibold tabular-nums text-amber-900 dark:text-amber-100">≈ {{ videoExample.total }}</span>
      </div>
      <div v-if="perRequestRows.length && !videoRows.length" class="mx-5 mt-4">
        <PricingTable :model="model" :rows="perRequestRows" :caption="t('plaza.price.unitPerRequest')" />
      </div>
    </template>

    <!-- 图片：图片 token 价 + 按张档位 -->
    <template v-else-if="model.billingKind === 'image'">
      <div
        v-if="imageTokenRows.length && imageTierRows.length"
        class="mx-5 mt-3 flex items-center gap-1.5 rounded-lg bg-fuchsia-50/70 px-2.5 py-1.5 text-[11px] text-fuchsia-800 dark:bg-fuchsia-500/10 dark:text-fuchsia-200"
      >
        <svg class="h-3.5 w-3.5 shrink-0" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path d="M10 2a8 8 0 100 16 8 8 0 000-16zm1 12H9v-2h2v2zm0-4H9V6h2v4z" />
        </svg>
        {{ t('plaza.card.imageTokenFirst') }}
      </div>
      <div v-if="imageTokenRows.length" class="mx-5 mt-3">
        <PricingTable :model="model" :rows="imageTokenRows" :caption="t('plaza.card.imageTokenCaption')" />
      </div>
      <div v-if="imageTierRows.length" class="mx-5 mt-3">
        <p class="mb-1.5 text-[11px] font-semibold text-gray-400">{{ t('plaza.card.perImageCaption') }}</p>
        <MediaTierTiles :model="model" :rows="imageTierRows" />
      </div>
      <div v-else-if="perRequestRows.length" class="mx-5 mt-3">
        <PricingTable :model="model" :rows="perRequestRows" :caption="t('plaza.price.unitPerRequest')" />
      </div>
    </template>

    <!-- 按次 -->
    <div
      v-else-if="model.billingKind === 'per_request'"
      class="mx-5 mt-4 flex items-end justify-between gap-3 rounded-xl bg-gradient-to-br from-emerald-50 to-accent-50/40 px-4 py-4 dark:from-emerald-500/10 dark:to-accent-500/5"
    >
      <div v-if="model.standardPricing" class="min-w-0">
        <p class="text-[11px] font-semibold text-gray-400">{{ t('plaza.card.perRequestLabel') }}</p>
        <p class="mt-1 truncate font-mono text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
          {{ perRequestPrice(model.standardPricing) }}
        </p>
      </div>
      <div v-if="model.vipPricing" :class="['min-w-0', model.standardPricing ? 'text-right' : '']">
        <p class="text-[11px] font-semibold text-orange-600">{{ t('plaza.price.vipLabel') }}</p>
        <p
          :class="[
            'mt-1 truncate font-mono font-semibold tabular-nums text-orange-600 dark:text-orange-400',
            model.standardPricing ? 'text-lg' : 'text-2xl font-bold'
          ]"
        >
          {{ perRequestPrice(model.vipPricing) }}
        </p>
      </div>
    </div>

    <!-- 文本 token -->
    <div v-else class="mx-5 mt-4 space-y-3">
      <PricingTable v-if="textRows.length" :model="model" :rows="textRows" :caption="t('plaza.card.tokenCaption')" />
      <PricingTable v-if="perRequestRows.length" :model="model" :rows="perRequestRows" :caption="t('plaza.price.unitPerRequest')" />
    </div>

    <div class="mt-auto flex items-center justify-between gap-2 px-5 pb-3 pt-4 text-[11px] text-gray-400">
      <span class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 tabular-nums">
        <span>{{ t('plaza.card.supportedChannels', { n: model.supportedGroups.length }) }}</span>
        <span v-if="model.recentCalls > 0" class="inline-flex items-center gap-1.5 text-gray-500 dark:text-gray-400">
          <span class="relative flex h-1.5 w-1.5 shrink-0" aria-hidden="true">
            <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary-400 opacity-60"></span>
            <span class="relative inline-flex h-1.5 w-1.5 rounded-full bg-primary-500"></span>
          </span>
          {{ t('plaza.card.recentCalls', { count: formatCallCount(model.recentCalls) }) }}
        </span>
      </span>
      <span :class="['shrink-0 font-medium opacity-0 transition group-hover:opacity-100 group-focus-visible:opacity-100', kindStyle.link]">
        {{ t('plaza.card.details') }} →
      </span>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PricingTable from './PricingTable.vue'
import MediaTierTiles from './MediaTierTiles.vue'
import {
  IMAGE_TIER_ROWS,
  IMAGE_TOKEN_ROWS,
  PER_REQUEST_ROWS,
  TEXT_TOKEN_ROWS,
  VIDEO_TIER_ROWS,
  visibleRows
} from './plazaPricingRows'
import { PLAZA_BILLING_KIND_STYLES, formatCallCount } from './plazaBillingKind'
import { platformBadgeLightClass, platformLabel } from '@/utils/platformColors'
import { PER_REQUEST_SCALE, formatCNYEffective } from '@/utils/pricing'
import { hasPeakRate } from '@/utils/peak-rate'
import type { AggregatedModel, PlazaPricingSummary } from '@/composables/useModelAggregation'
import type { GroupPlatform } from '@/types'

const VIDEO_EXAMPLE_SECONDS = 6

const props = defineProps<{
  model: AggregatedModel
}>()

defineEmits<{
  'open-detail': [model: AggregatedModel]
}>()

const { t } = useI18n()

const kindStyle = computed(() => PLAZA_BILLING_KIND_STYLES[props.model.billingKind])

const textRows = computed(() => visibleRows(props.model, TEXT_TOKEN_ROWS))
const imageTokenRows = computed(() => visibleRows(props.model, IMAGE_TOKEN_ROWS))
const imageTierRows = computed(() => visibleRows(props.model, IMAGE_TIER_ROWS))
const videoRows = computed(() => visibleRows(props.model, VIDEO_TIER_ROWS))
const perRequestRows = computed(() => visibleRows(props.model, PER_REQUEST_ROWS))

// 高峰倍率只作用于 token 计费，按张 / 按秒 / 按次价不受影响。
const hasPeakPricing = computed(() =>
  (textRows.value.length > 0 || imageTokenRows.value.length > 0) &&
  props.model.supportedGroups.some((item) => hasPeakRate(item.group))
)
const hasDeepSeekTimePricing = computed(() => props.model.timeSchedule?.kind === 'deepseek_official')

const billingHint = computed(() => {
  if (props.model.billingKind === 'video') return t('plaza.card.billingPerSecond')
  if (props.model.billingKind === 'image' && imageTierRows.value.length && !imageTokenRows.value.length) {
    return t('plaza.card.billingPerImage')
  }
  return ''
})

/** 示例费用：优先 720p，缺省取第一个可结算档位；用标准价（没有则 VIP 价）。 */
const videoExample = computed(() => {
  const summary = props.model.standardPricing ?? props.model.vipPricing
  if (!summary) return null
  const rows = videoRows.value.filter((item) => summary.minPricing[item.key] != null)
  const picked = rows.find((item) => item.key === 'video720p') ?? rows[0]
  if (!picked) return null
  const price = summary.minPricing[picked.key]
  return {
    tier: picked.label,
    total: formatCNYEffective(
      price == null ? null : price * VIDEO_EXAMPLE_SECONDS,
      PER_REQUEST_SCALE,
      summary.minPricingRateMultipliers[picked.key]
    )
  }
})

function perRequestPrice(summary: PlazaPricingSummary): string {
  return formatCNYEffective(
    summary.minPricing.perRequest,
    PER_REQUEST_SCALE,
    summary.minPricingRateMultipliers.perRequest
  )
}
</script>
