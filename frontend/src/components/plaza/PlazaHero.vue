<template>
  <section class="bg-white dark:bg-dark-950">
    <div class="mx-auto grid max-w-7xl gap-8 px-4 py-10 sm:px-6 lg:grid-cols-[1fr_auto] lg:px-8 lg:py-14">
      <div class="max-w-3xl">
        <p class="text-sm font-semibold uppercase tracking-wide text-primary-600 dark:text-primary-400">
          {{ t('plaza.hero.eyebrow') }}
        </p>
        <h1 class="mt-3 text-4xl font-bold text-gray-950 dark:text-white sm:text-5xl">
          {{ t('plaza.hero.title') }}
        </h1>
        <p class="mt-4 text-base leading-7 text-gray-600 dark:text-gray-300 sm:text-lg">
          {{ t('plaza.hero.subtitle') }}
        </p>
      </div>

      <div class="grid min-w-[280px] gap-3 sm:grid-cols-3 lg:grid-cols-1">
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-800">
          <div class="text-2xl font-semibold text-gray-950 dark:text-white">{{ modelCount }}</div>
          <div class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('plaza.metrics.models') }}</div>
        </div>
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-800">
          <div class="text-2xl font-semibold text-gray-950 dark:text-white">{{ platformCount }}</div>
          <div class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('plaza.metrics.platforms') }}</div>
        </div>
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-800">
          <div class="text-2xl font-semibold text-primary-600 dark:text-primary-400">{{ boostLabel }}</div>
          <div class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('plaza.metrics.boost') }}</div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  modelCount: number
  platformCount: number
  boost: number
}>()

const { t } = useI18n()

const boostLabel = computed(() => {
  const percent = Math.max(0, Math.round((props.boost - 1) * 100))
  return percent > 0 ? `+${percent}%` : '0%'
})
</script>
