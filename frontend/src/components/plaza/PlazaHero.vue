<template>
  <section class="border-b border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-950">
    <div class="mx-auto max-w-[90rem] px-4 py-8 sm:px-6 lg:px-8 lg:py-10">
      <div class="flex flex-col gap-8 lg:flex-row lg:items-end lg:justify-between">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-3">
            <span class="h-px w-8 bg-primary-500" aria-hidden="true"></span>
            <span class="text-xs font-medium tracking-wide text-gray-500 dark:text-gray-400">
              {{ t('plaza.hero.eyebrow') }}
            </span>
          </div>
          <div class="mt-4 flex flex-wrap items-center gap-3">
            <h1 class="text-3xl font-bold tracking-tight text-gray-950 dark:text-white sm:text-4xl">
              {{ t('plaza.hero.title') }}
            </h1>
            <span class="inline-flex items-center rounded border border-amber-300/70 bg-amber-50 px-2 py-1 font-mono text-[11px] font-medium tabular-nums text-amber-700 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
              {{ t('plaza.hero.rateTag', { rate: rechargeRateLabel }) }}
            </span>
          </div>
          <p class="mt-3 max-w-2xl text-sm leading-relaxed text-gray-500 dark:text-gray-400">
            {{ t('plaza.hero.subtitle') }}
          </p>
        </div>

        <div class="grid min-w-0 flex-1 grid-cols-2 overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-900 sm:grid-cols-3">
          <div class="min-w-0 px-5 py-4">
            <div class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('plaza.metrics.models') }}
            </div>
            <div class="mt-1.5 font-mono text-2xl font-semibold tabular-nums tracking-tight text-gray-900 dark:text-white">
              {{ modelCount }}
            </div>
          </div>
          <div class="min-w-0 border-l border-gray-200 px-5 py-4 dark:border-dark-800">
            <div class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('plaza.metrics.platforms') }}
            </div>
            <div class="mt-1.5 font-mono text-2xl font-semibold tabular-nums tracking-tight text-gray-900 dark:text-white">
              {{ platformCount }}
            </div>
          </div>
          <div class="min-w-0 col-span-2 border-t border-gray-200 px-5 py-4 dark:border-dark-800 sm:col-span-1 sm:border-l sm:border-t-0">
            <div class="flex items-center gap-1.5 text-xs font-medium text-gray-500 dark:text-gray-400">
              <span v-if="hasBoost" class="h-1.5 w-1.5 rounded-full bg-emerald-500" aria-hidden="true"></span>
              {{ t('plaza.metrics.boost') }}
            </div>
            <div
              :class="[
                'mt-1.5 break-words font-mono text-2xl font-semibold tabular-nums tracking-tight',
                hasBoost ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-900 dark:text-white'
              ]"
            >
              {{ valueBoostLabel }}
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { normalizePlazaMultiplier } from '@/utils/pricing'

const props = defineProps<{
  modelCount: number
  platformCount: number
  multiplier: number
}>()

const { t } = useI18n()

const hasBoost = computed(() => normalizePlazaMultiplier(props.multiplier) !== 1)
const valueBoostLabel = computed(() => t('plaza.hero.boostValue', { multiplier: rechargeRateLabel.value }))

const rechargeRateLabel = computed(() => {
  const value = normalizePlazaMultiplier(props.multiplier)
  return Number(value.toFixed(3)).toString()
})
</script>
