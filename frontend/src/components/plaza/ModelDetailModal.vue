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
        class="relative flex max-h-[92vh] w-full max-w-3xl flex-col overflow-hidden rounded-t-2xl border border-gray-200 bg-white shadow-2xl dark:border-dark-800 dark:bg-dark-900 md:m-6 md:rounded-2xl"
        role="dialog"
        aria-modal="true"
        aria-labelledby="plaza-modal-title"
        tabindex="-1"
      >
        <header class="relative shrink-0 overflow-hidden bg-dark-950 px-6 py-5">
          <div class="tech-grid absolute inset-0 opacity-60" aria-hidden="true"></div>
          <div :class="['absolute -top-20 left-1/3 h-48 w-96 rounded-full blur-3xl', headerGlowClass]" aria-hidden="true"></div>
          <div class="relative flex items-start gap-4">
            <div
              :class="['flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl text-white shadow-glow', headerIconClass]"
              aria-hidden="true"
            >
              <PlatformIcon :platform="model.platform as GroupPlatform" size="lg" />
            </div>
            <div class="min-w-0 flex-1">
              <h2 id="plaza-modal-title" class="truncate text-lg font-bold text-white sm:text-xl">
                {{ model.displayName }}
              </h2>
              <p class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-400">
                <span>{{ platformLabel(model.platform) }}</span>
                <span :class="['rounded-full px-2 py-px', headerKindBadgeClass]">{{ t(kindStyle.labelKey) }}</span>
                <span v-if="hasDeepSeekTimePricing" class="rounded-full bg-sky-500/20 px-2 py-px text-sky-200">
                  {{ t('plaza.card.deepSeekTimePricing') }}
                </span>
                <span v-if="hasPeakPricing" class="rounded-full bg-amber-500/20 px-2 py-px text-amber-200">
                  {{ t('plaza.card.peakPricing') }}
                </span>
                <span v-if="model.recentCalls > 0" class="tabular-nums">
                  {{ t('plaza.card.recentCalls', { count: formatCallCount(model.recentCalls) }) }}
                </span>
              </p>
              <p class="mt-2 flex items-center gap-2 text-xs text-gray-400">
                <code class="truncate font-mono text-gray-300">{{ model.model }}</code>
                <button
                  type="button"
                  data-testid="plaza-modal-copy"
                  class="shrink-0 rounded border border-white/15 px-1.5 text-[10px] text-gray-300 transition hover:bg-white/10"
                  @click="copyModelId"
                >
                  {{ copied ? t('plaza.modal.copied') : t('plaza.modal.copy') }}
                </button>
              </p>
            </div>
            <button
              ref="closeButtonRef"
              data-testid="plaza-modal-close"
              class="shrink-0 rounded-lg p-2 text-gray-400 transition hover:bg-white/10 hover:text-white"
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

        <div class="flex-1 overflow-y-auto px-6 py-6">
          <div class="space-y-6">
            <!-- 视频 -->
            <section v-if="model.billingKind === 'video'" class="space-y-3">
              <h3 class="text-xs font-semibold text-gray-500 dark:text-gray-400">{{ t('plaza.modal.videoPricing') }}</h3>
              <PricingTable v-if="videoRows.length" :model="model" :rows="videoRows" :caption="t('plaza.card.videoCaption')" />
              <PricingTable v-else-if="perRequestRows.length" :model="model" :rows="perRequestRows" :caption="t('plaza.price.unitPerRequest')" />
              <p class="rounded-lg bg-gray-50 px-3 py-2 text-[11px] leading-5 text-gray-500 dark:bg-dark-800/60 dark:text-gray-400">
                {{ t('plaza.modal.videoNote') }}
              </p>
            </section>

            <!-- 图片 -->
            <section v-else-if="model.billingKind === 'image'" :class="['grid gap-5', imageTokenRows.length && imageTierRows.length ? 'md:grid-cols-2' : '']">
              <div v-if="imageTokenRows.length">
                <p class="mb-2 flex items-center gap-2 text-xs font-semibold text-gray-500 dark:text-gray-400">
                  <span v-if="imageTierRows.length" class="rounded bg-primary-600 px-1.5 py-px text-[10px] text-white">{{ t('plaza.modal.preferred') }}</span>
                  {{ t('plaza.modal.imageTokenPricing') }}
                </p>
                <PricingTable :model="model" :rows="imageTokenRows" :caption="t('plaza.card.tokenCaption')" />
              </div>
              <div v-if="imageTierRows.length || perRequestRows.length">
                <p class="mb-2 text-xs font-semibold text-gray-500 dark:text-gray-400">
                  {{ imageTokenRows.length ? t('plaza.modal.perImageFallback') : t('plaza.modal.perImagePricing') }}
                </p>
                <PricingTable v-if="imageTierRows.length" :model="model" :rows="imageTierRows" :caption="t('plaza.card.perImageCaption')" />
                <PricingTable v-else :model="model" :rows="perRequestRows" :caption="t('plaza.price.unitPerRequest')" />
                <p class="mt-3 rounded-lg bg-gray-50 px-3 py-2 text-[11px] leading-5 text-gray-500 dark:bg-dark-800/60 dark:text-gray-400">
                  {{ imageTokenRows.length ? t('plaza.modal.imageNote') : t('plaza.modal.imagePerImageNote') }}
                </p>
              </div>
            </section>

            <!-- 文本 / 按次 -->
            <section v-else class="space-y-3">
              <PricingTable v-if="textRows.length" :model="model" :rows="textRows" :caption="t('plaza.card.tokenCaption')" />
              <PricingTable v-if="perRequestRows.length" :model="model" :rows="perRequestRows" :caption="t('plaza.price.unitPerRequest')" />
              <p v-if="!textRows.length && !perRequestRows.length" class="text-sm text-gray-500 dark:text-gray-400">
                {{ t('plaza.card.notAvailable') }}
              </p>
            </section>

            <div v-if="hasDeepSeekTimePricing || hasPeakPricing" class="space-y-2">
              <p v-if="hasDeepSeekTimePricing" class="text-xs text-sky-700 dark:text-sky-300">
                {{ t('plaza.modal.deepSeekTimeNote') }}
              </p>
              <p v-if="hasPeakPricing" class="text-xs text-amber-700 dark:text-amber-300">
                {{ t('plaza.modal.basePricePeakNote') }}
              </p>
            </div>

            <section data-testid="plaza-modal-groups">
              <div class="mb-3 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <h3 class="flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
                  <span class="inline-block h-4 w-1 rounded-full bg-gradient-to-b from-primary-500 to-accent-400" aria-hidden="true"></span>
                  {{ t('plaza.modal.availableGroups') }}
                  <span class="rounded-full bg-primary-50 px-1.5 text-[11px] font-semibold tabular-nums text-primary-600 dark:bg-primary-500/15 dark:text-primary-300">
                    {{ groupCount }}
                  </span>
                </h3>
                <p class="text-[11px] text-gray-400">{{ t('plaza.modal.groupsHint') }}</p>
              </div>
              <PlazaGroupCards
                :groups="model.supportedGroups"
                :billing-kind="model.billingKind"
                :server-utc-offset="serverUtcOffset"
              />
            </section>

            <section v-if="showTiered">
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
                          :currency="item.pricing.currency"
                          :billing-rate-multiplier="item.group.rate_multiplier"
                        />
                        <PriceLine
                          :label="t('plaza.modal.output')"
                          :value="interval.output_price"
                          :scale="PER_MILLION_TOKEN_SCALE"
                          :currency="item.pricing.currency"
                          :billing-rate-multiplier="item.group.rate_multiplier"
                        />
                        <PriceLine
                          :label="t('plaza.modal.cacheWrite')"
                          :value="interval.cache_write_price"
                          :scale="PER_MILLION_TOKEN_SCALE"
                          :currency="item.pricing.currency"
                          :billing-rate-multiplier="item.group.rate_multiplier"
                        />
                        <PriceLine
                          :label="t('plaza.modal.cacheRead')"
                          :value="interval.cache_read_price"
                          :scale="PER_MILLION_TOKEN_SCALE"
                          :currency="item.pricing.currency"
                          :billing-rate-multiplier="item.group.rate_multiplier"
                        />
                        <PriceLine
                          :label="t('plaza.modal.imageOutput')"
                          :value="item.pricing.image_output_price"
                          :scale="PER_MILLION_TOKEN_SCALE"
                          :currency="item.pricing.currency"
                          :billing-rate-multiplier="item.group.rate_multiplier"
                        />
                        <PriceLine
                          :label="t('plaza.modal.perRequest')"
                          :value="interval.per_request_price"
                          :scale="PER_REQUEST_SCALE"
                          :currency="item.pricing.currency"
                          :billing-rate-multiplier="requestRateMultiplier(item)"
                        />
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </section>
          </div>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PriceLine from './PriceLine.vue'
import PricingTable from './PricingTable.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PlazaGroupCards from './PlazaGroupCards.vue'
import {
  IMAGE_TIER_ROWS,
  IMAGE_TOKEN_ROWS,
  PER_REQUEST_ROWS,
  TEXT_TOKEN_ROWS,
  VIDEO_TIER_ROWS,
  visibleRows
} from './plazaPricingRows'
import { PLAZA_BILLING_KIND_STYLES, formatCallCount } from './plazaBillingKind'
import { PER_MILLION_TOKEN_SCALE, PER_REQUEST_SCALE } from '@/utils/pricing'
import { platformBadgeClass, platformLabel } from '@/utils/platformColors'
import {
  imageTierRateMultiplier,
  videoTierRateMultiplier,
  type AggregatedModel,
  type PlazaBillingKind,
  type PlazaSupportedGroup
} from '@/composables/useModelAggregation'
import type { UserPricingInterval } from '@/api/channels'
import type { GroupPlatform } from '@/types'
import { hasPeakRate } from '@/utils/peak-rate'

const props = defineProps<{
  open: boolean
  model: AggregatedModel | null
  serverUtcOffset?: string
}>()

const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()

const dialogRef = ref<HTMLElement | null>(null)
const closeButtonRef = ref<HTMLButtonElement | null>(null)
const previousFocus = ref<HTMLElement | null>(null)
const copied = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | null = null

const billingKind = computed<PlazaBillingKind>(() => props.model?.billingKind ?? 'text')
const kindStyle = computed(() => PLAZA_BILLING_KIND_STYLES[billingKind.value])

const HEADER_STYLES: Record<PlazaBillingKind, { icon: string; glow: string; badge: string }> = {
  text: { icon: 'bg-gradient-to-br from-primary-500 to-accent-400', glow: 'bg-primary-600/30', badge: 'bg-primary-500/20 text-primary-200' },
  image: { icon: 'bg-gradient-to-br from-fuchsia-500 to-primary-500', glow: 'bg-fuchsia-600/25', badge: 'bg-fuchsia-500/20 text-fuchsia-200' },
  video: { icon: 'bg-gradient-to-br from-amber-500 to-rose-500', glow: 'bg-amber-500/20', badge: 'bg-amber-500/20 text-amber-200' },
  per_request: { icon: 'bg-gradient-to-br from-emerald-500 to-accent-400', glow: 'bg-emerald-500/20', badge: 'bg-emerald-500/20 text-emerald-200' }
}
const headerIconClass = computed(() => HEADER_STYLES[billingKind.value].icon)
const headerGlowClass = computed(() => HEADER_STYLES[billingKind.value].glow)
const headerKindBadgeClass = computed(() => HEADER_STYLES[billingKind.value].badge)

const sortedGroups = computed(() =>
  [...(props.model?.supportedGroups ?? [])].sort(
    (a, b) => {
      const typeDifference = Number(a.group.vip_only === true) - Number(b.group.vip_only === true)
      return typeDifference || (a.group.rate_multiplier || 1) - (b.group.rate_multiplier || 1)
    }
  )
)

const groupCount = computed(() => new Set(sortedGroups.value.map((item) => item.group.id)).size)

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
// 图片/视频的档位区间已经折算进上方的档位表，这里只展示 token 阶梯。
const showTiered = computed(() =>
  tieredGroups.value.length > 0 && billingKind.value !== 'image' && billingKind.value !== 'video'
)

const rowsFor = (rows: typeof TEXT_TOKEN_ROWS) => computed(() => props.model ? visibleRows(props.model, rows) : [])
const textRows = rowsFor(TEXT_TOKEN_ROWS)
const imageTokenRows = rowsFor(IMAGE_TOKEN_ROWS)
const imageTierRows = rowsFor(IMAGE_TIER_ROWS)
const videoRows = rowsFor(VIDEO_TIER_ROWS)
const perRequestRows = rowsFor(PER_REQUEST_ROWS)

const hasPeakPricing = computed(() =>
  (textRows.value.length > 0 || imageTokenRows.value.length > 0) &&
  props.model?.supportedGroups.some((item) => hasPeakRate(item.group)) === true
)
const hasDeepSeekTimePricing = computed(() =>
  props.model?.timeSchedule?.kind === 'deepseek_official'
)

const tokenFormatter = new Intl.NumberFormat()

function formatTierRange(minTokens: UserPricingInterval['min_tokens'], maxTokens: UserPricingInterval['max_tokens']) {
  const min = tokenFormatter.format(minTokens)
  if (maxTokens == null) {
    return t('plaza.modal.tierRangeOpenEnded', { min })
  }
  return t('plaza.modal.tierRange', { min, max: tokenFormatter.format(maxTokens) })
}

function requestRateMultiplier(item: PlazaSupportedGroup): number {
  if (item.pricing?.billing_mode === 'image') return imageTierRateMultiplier(item.group)
  if (item.pricing?.billing_mode === 'video') return videoTierRateMultiplier(item.group)
  return item.group.rate_multiplier
}

async function copyModelId() {
  if (!props.model) return
  try {
    await navigator.clipboard?.writeText(props.model.model)
    copied.value = true
    if (copiedTimer) clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => { copied.value = false }, 1500)
  } catch {
    copied.value = false
  }
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
  () => props.model ? `${props.model.platform}:${props.model.model}` : null,
  () => {
    copied.value = false
  }
)

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
  if (copiedTimer) clearTimeout(copiedTimer)
  restorePreviousFocus()
})
</script>
