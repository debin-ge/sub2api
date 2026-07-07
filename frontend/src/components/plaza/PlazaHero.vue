<template>
  <section class="border-b border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-950">
    <div class="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-8 sm:px-6 lg:flex-row lg:items-center lg:justify-between lg:px-8 lg:py-10">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-3">
          <h1 class="text-3xl font-bold text-gray-950 dark:text-white sm:text-4xl">
            {{ t('plaza.hero.title') }}
          </h1>
          <span class="inline-flex items-center rounded-md bg-amber-100 px-2 py-1 text-xs font-medium text-amber-800 dark:bg-amber-500/20 dark:text-amber-300">
            {{ t('plaza.hero.rateTag', { rate: rechargeRateLabel }) }}
          </span>
        </div>
        <p class="mt-2 max-w-2xl text-sm text-gray-500 dark:text-gray-400">
          {{ t('plaza.hero.subtitle', { boost: valueBoost }) }}
        </p>
      </div>

      <div class="grid shrink-0 grid-cols-3 gap-3 sm:gap-4">
        <div class="min-w-[88px] rounded-xl border border-gray-200 bg-white px-4 py-3 shadow-sm dark:border-dark-800 dark:bg-dark-900">
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('plaza.metrics.models') }}</div>
          <div class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ modelCount }}</div>
        </div>
        <div class="min-w-[88px] rounded-xl border border-gray-200 bg-white px-4 py-3 shadow-sm dark:border-dark-800 dark:bg-dark-900">
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('plaza.metrics.platforms') }}</div>
          <div class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ platformCount }}</div>
        </div>
        <div
          :class="[
            'min-w-[88px] rounded-xl border px-4 py-3 shadow-sm',
            hasBoost
              ? 'border-emerald-200 bg-emerald-50 dark:border-emerald-500/30 dark:bg-emerald-500/10'
              : 'border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-900'
          ]"
        >
          <div
            :class="[
              'text-xs',
              hasBoost ? 'text-emerald-700 dark:text-emerald-300' : 'text-gray-500 dark:text-gray-400'
            ]"
          >
            {{ t('plaza.metrics.boost') }}
          </div>
          <div
            :class="[
              'mt-1 text-xl font-semibold',
              hasBoost ? 'text-emerald-700 dark:text-emerald-300' : 'text-gray-900 dark:text-white'
            ]"
          >
            {{ valueBoostLabel }}
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { computeValueBoost, normalizePlazaMultiplier } from '@/utils/pricing'

const props = defineProps<{
  modelCount: number
  platformCount: number
  rate: number
  multiplier: number
}>()

const { t } = useI18n()

const valueBoost = computed(() => computeValueBoost(props.multiplier, props.rate))
const hasBoost = computed(() => valueBoost.value > 1)
const valueBoostLabel = computed(() => t('plaza.hero.boostValue', { boost: valueBoost.value }))

const rechargeRateLabel = computed(() => {
  const value = normalizePlazaMultiplier(props.multiplier)
  return Number(value.toFixed(3)).toString()
})
</script>
