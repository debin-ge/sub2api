<template>
  <section class="space-y-5" data-test="video-simple-editor">
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="field-label">
        <span>{{ t('admin.modelPrices.video.billingUnit') }}</span>
        <select data-test="video-simple-billing-unit" class="input" :value="profile.billingUnit" @change="setBillingUnit(inputValue($event) as VideoBillingUnit)">
          <option value="request">{{ t('admin.modelPrices.video.perRequestOption') }}</option>
          <option value="second">{{ t('admin.modelPrices.video.perSecondOption') }}</option>
          <option value="video_token">{{ t('admin.modelPrices.video.perVideoTokenOption') }}</option>
        </select>
      </label>
      <label v-if="profile.billingUnit === 'video_token'" class="field-label">
        <span>{{ t('admin.modelPrices.video.simpleTokensPerSecond') }}</span>
        <input
          class="input"
          data-test="video-simple-tokens-per-second"
          type="number"
          min="0"
          step="any"
          :value="profile.tokensPerSecond ?? ''"
          @input="update({ tokensPerSecond: optionalNumber($event) })"
        />
      </label>
    </div>

    <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
      <label class="flex items-center gap-2 text-sm font-medium text-gray-800 dark:text-gray-200">
        <input type="checkbox" class="rounded border-gray-300" :checked="profile.standardEnabled" @change="update({ standardEnabled: checkedValue($event) })" />
        {{ t('admin.modelPrices.video.simpleStandardEnabled') }}
      </label>
      <label v-if="profile.standardEnabled" class="field-label mt-3 block max-w-md">
        <span>{{ priceUnitLabel }}</span>
        <input class="input" type="number" min="0" step="any" :value="displayPrice(profile.standardUnitPriceUsd)" @input="setStandardPrice(inputValue($event))" />
      </label>
    </div>

    <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
      <label class="flex items-center gap-2 text-sm font-medium text-gray-800 dark:text-gray-200">
        <input type="checkbox" class="rounded border-gray-300" :checked="profile.referenceEnabled" @change="update({ referenceEnabled: checkedValue($event) })" />
        {{ t('admin.modelPrices.video.simpleReferenceEnabled') }}
      </label>
      <label v-if="profile.referenceEnabled && profile.standardEnabled" class="mt-3 flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
        <input type="checkbox" class="rounded border-gray-300" :checked="profile.referenceSameAsStandard" @change="update({ referenceSameAsStandard: checkedValue($event) })" />
        {{ t('admin.modelPrices.video.simpleReferenceSamePrice') }}
      </label>
      <label v-if="profile.referenceEnabled && (!profile.referenceSameAsStandard || !profile.standardEnabled)" class="field-label mt-3 block max-w-md">
        <span>{{ t('admin.modelPrices.video.simpleReferencePrice') }} · {{ priceUnitLabel }}</span>
        <input class="input" type="number" min="0" step="any" :value="displayPrice(profile.referenceUnitPriceUsd)" @input="setReferencePrice(inputValue($event))" />
      </label>
    </div>

    <div class="grid gap-3 border-t border-gray-200 pt-4 text-sm sm:grid-cols-2 dark:border-dark-600">
      <div>
        <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.modelPrices.video.simpleEstimateStandard') }}</div>
        <div class="mt-1 font-medium text-gray-900 dark:text-white">{{ estimate(profile.standardEnabled, profile.standardUnitPriceUsd) }}</div>
      </div>
      <div>
        <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.modelPrices.video.simpleEstimateReference') }}</div>
        <div class="mt-1 font-medium text-gray-900 dark:text-white">{{ estimate(profile.referenceEnabled, referencePrice) }}</div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { mTokToToken, tokenToMTok, type VideoBillingUnit, type VideoPricingConfig } from '@/api/admin/modelPrices'
import { buildSimpleVideoPricingConfig, parseSimpleVideoPricingConfig, type SimpleVideoPricingProfile } from './simpleVideoPricing'

const props = defineProps<{ modelValue: VideoPricingConfig }>()
const emit = defineEmits<{ 'update:modelValue': [value: VideoPricingConfig] }>()
const { t } = useI18n()

const profile = computed(() => parseSimpleVideoPricingConfig(props.modelValue) || {
  billingUnit: 'video_token',
  standardEnabled: true,
  standardUnitPriceUsd: 0,
  referenceEnabled: false,
  referenceSameAsStandard: false,
  referenceUnitPriceUsd: 0,
  tokensPerSecond: undefined,
} satisfies SimpleVideoPricingProfile)

const priceUnitLabel = computed(() => {
  if (profile.value.billingUnit === 'video_token') return t('admin.modelPrices.video.perMillionVideoTokens')
  if (profile.value.billingUnit === 'second') return t('admin.modelPrices.video.perSecond')
  return t('admin.modelPrices.video.perRequest')
})

const referencePrice = computed(() => profile.value.referenceSameAsStandard && profile.value.standardEnabled
  ? profile.value.standardUnitPriceUsd
  : profile.value.referenceUnitPriceUsd)

function update(patch: Partial<SimpleVideoPricingProfile>) {
  emit('update:modelValue', buildSimpleVideoPricingConfig({ ...profile.value, ...patch }))
}

function setBillingUnit(billingUnit: VideoBillingUnit) {
  update({ billingUnit, standardUnitPriceUsd: 0, referenceUnitPriceUsd: 0 })
}

function displayPrice(value: number): number | '' {
  return profile.value.billingUnit === 'video_token' ? tokenToMTok(value) : value
}

function parsePrice(raw: string): number | undefined {
  return profile.value.billingUnit === 'video_token' ? mTokToToken(raw) : optionalNumberValue(raw)
}

function setStandardPrice(raw: string) {
  const value = parsePrice(raw)
  if (value != null) update({ standardUnitPriceUsd: value })
}

function setReferencePrice(raw: string) {
  const value = parsePrice(raw)
  if (value != null) update({ referenceUnitPriceUsd: value })
}

function estimate(enabled: boolean, price: number): string {
  if (!enabled) return t('admin.modelPrices.video.simpleDisabled')
  const units = profile.value.billingUnit === 'request'
    ? 1
    : profile.value.billingUnit === 'second'
      ? 10
      : 10 * (profile.value.tokensPerSecond || 0)
  if (!units) return t('admin.modelPrices.video.simpleEstimateUnavailable')
  return `$${Number((units * price).toPrecision(12))}`
}

function optionalNumberValue(raw: string): number | undefined {
  if (raw === '') return undefined
  const value = Number(raw)
  return Number.isFinite(value) ? value : undefined
}

function optionalNumber(event: Event): number | undefined { return optionalNumberValue(inputValue(event)) }
function inputValue(event: Event): string { return (event.target as HTMLInputElement).value }
function checkedValue(event: Event): boolean { return (event.target as HTMLInputElement).checked }
</script>

<style scoped>
.field-label {
  @apply text-xs font-medium text-gray-600 dark:text-gray-300;
}
.field-label > span {
  @apply mb-1 block;
}
</style>
