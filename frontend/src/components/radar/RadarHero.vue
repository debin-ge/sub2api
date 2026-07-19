<template>
  <section class="relative overflow-hidden border-b border-gray-200 bg-gradient-to-br from-primary-50 via-white to-indigo-50 dark:border-dark-800 dark:from-primary-950/30 dark:via-dark-950 dark:to-indigo-950/20">
    <div class="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-12 sm:px-6 md:flex-row md:items-end md:justify-between lg:px-8 lg:py-16">
      <div class="max-w-3xl">
        <slot name="eyebrow">
          <p class="mb-3 text-sm font-semibold uppercase tracking-[0.2em] text-primary-600 dark:text-primary-400">
            {{ t('radar.hero.eyebrow', 'Public data dashboard') }}
          </p>
        </slot>
        <h1 class="text-3xl font-bold tracking-tight text-gray-950 dark:text-white sm:text-5xl">
          {{ t('radar.hero.title', 'Model Radar') }}
        </h1>
        <p class="mt-4 max-w-2xl text-base leading-7 text-gray-600 dark:text-gray-300 sm:text-lg">
          {{ t('radar.hero.description', 'Track service health, anonymous quota aggregates, and public model benchmark changes.') }}
        </p>
      </div>

      <div class="flex flex-col items-start gap-3 md:items-end">
        <div class="text-sm text-gray-500 dark:text-gray-400" aria-live="polite">
          <span>{{ t('radar.hero.clientUpdated', 'Page data fetched') }}:</span>
          <time v-if="lastFetchedAt" :datetime="dateTimeValue">{{ formattedLastFetchedAt }}</time>
          <span v-else>{{ t('radar.common.never', 'Not yet') }}</span>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(defineProps<{
  lastFetchedAt?: Date | string | null
}>(), {
  lastFetchedAt: null,
})

const { t, locale } = useI18n()

const parsedDate = computed(() => {
  if (!props.lastFetchedAt) return null
  const value = props.lastFetchedAt instanceof Date ? props.lastFetchedAt : new Date(props.lastFetchedAt)
  return Number.isFinite(value.getTime()) ? value : null
})
const dateTimeValue = computed(() => parsedDate.value?.toISOString())
const formattedLastFetchedAt = computed(() => {
  if (!parsedDate.value) return t('radar.common.unknownTime', 'Unknown')
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(parsedDate.value)
})
</script>
