<template>
  <section class="relative overflow-hidden bg-dark-950">
    <div class="tech-grid absolute inset-0" aria-hidden="true"></div>
    <div class="absolute left-1/2 top-0 h-[420px] w-[900px] -translate-x-1/2 -translate-y-1/3 rounded-full bg-primary-500/20 blur-[120px]" aria-hidden="true"></div>
    <div class="absolute -right-24 top-1/3 h-80 w-80 rounded-full bg-accent-400/10 blur-3xl" aria-hidden="true"></div>

    <div class="relative mx-auto max-w-7xl px-4 pb-10 pt-12 sm:px-6 lg:px-8 lg:pt-16">
      <div class="flex flex-col gap-10 lg:flex-row lg:items-end lg:justify-between">
        <div class="max-w-2xl">
          <p class="mb-5 inline-flex items-center gap-2 rounded-full border border-primary-400/30 bg-primary-500/10 px-3 py-1 font-mono text-xs font-medium text-primary-300">
            <span class="relative flex h-1.5 w-1.5" aria-hidden="true">
              <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75"></span>
              <span class="relative inline-flex h-1.5 w-1.5 rounded-full bg-emerald-400"></span>
            </span>
            {{ t('plaza.hero.eyebrow') }}
          </p>
          <h1 class="text-4xl font-extrabold leading-tight tracking-tight text-white sm:text-5xl">
            {{ t('plaza.hero.title') }}
            <span class="text-gradient">{{ t('plaza.hero.titleAccent') }}</span>
          </h1>
          <p class="mt-4 max-w-xl text-base leading-7 text-gray-300">
            {{ t('plaza.hero.subtitle') }}
          </p>
          <div class="mt-5 flex flex-wrap items-center gap-2 font-mono text-xs">
            <span class="inline-flex items-center rounded-lg border border-amber-400/30 bg-amber-500/10 px-3 py-1.5 text-amber-300">
              {{ t('plaza.hero.rateTag', { rate: rechargeRateLabel }) }}
            </span>
            <span
              v-if="hasBoost"
              class="inline-flex items-center rounded-lg border border-emerald-400/30 bg-emerald-500/10 px-3 py-1.5 text-emerald-300"
            >
              {{ t('plaza.hero.boostValue', { multiplier: rechargeRateLabel }) }}
            </span>
          </div>
        </div>

        <div class="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:w-[520px]">
          <div
            v-for="metric in metrics"
            :key="metric.key"
            class="rounded-xl border border-white/10 bg-white/5 px-4 py-3 backdrop-blur"
          >
            <p class="font-mono text-2xl font-bold tabular-nums text-white">{{ metric.value }}</p>
            <p class="mt-0.5 flex items-center gap-1.5 text-xs text-gray-400">
              <span v-if="metric.dot" :class="['h-1.5 w-1.5 rounded-full', metric.dot]" aria-hidden="true"></span>
              {{ metric.label }}
            </p>
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
  imageModelCount: number
  videoModelCount: number
  multiplier: number
}>()

const { t } = useI18n()

const hasBoost = computed(() => normalizePlazaMultiplier(props.multiplier) !== 1)

const rechargeRateLabel = computed(() => {
  const value = normalizePlazaMultiplier(props.multiplier)
  return Number(value.toFixed(3)).toString()
})

const metrics = computed(() => [
  { key: 'models', value: props.modelCount, label: t('plaza.metrics.models'), dot: '' },
  { key: 'platforms', value: props.platformCount, label: t('plaza.metrics.platforms'), dot: '' },
  { key: 'image', value: props.imageModelCount, label: t('plaza.metrics.imageModels'), dot: 'bg-fuchsia-400' },
  { key: 'video', value: props.videoModelCount, label: t('plaza.metrics.videoModels'), dot: 'bg-amber-400' }
])
</script>
