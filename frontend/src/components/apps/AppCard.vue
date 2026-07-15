<template>
  <RouterLink
    :to="`/apps/${app.slug}`"
    class="group flex flex-col rounded-2xl border border-gray-200 bg-white p-5 shadow-sm transition hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-md dark:border-dark-700 dark:bg-dark-900 dark:hover:border-primary-500/40"
  >
    <div class="flex items-start justify-between gap-3">
      <div class="flex h-11 w-11 items-center justify-center rounded-xl bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-dark-200">
        <AppIcon :icon="app.icon" size="lg" />
      </div>
      <span class="rounded-full bg-primary-50 px-2 py-0.5 text-[11px] font-semibold text-primary-700 ring-1 ring-inset ring-primary-600/15 dark:bg-primary-500/10 dark:text-primary-300 dark:ring-primary-400/25">
        {{ stepsLabel }}
      </span>
    </div>
    <h3 class="mt-4 text-base font-semibold text-gray-950 transition group-hover:text-primary-700 dark:text-white dark:group-hover:text-primary-300">
      {{ app.name }}
    </h3>
    <p class="mt-1 line-clamp-2 text-sm text-gray-500 dark:text-dark-300">
      {{ tagline }}
    </p>
    <div class="mt-3 flex flex-wrap gap-1.5">
      <span
        v-for="p in app.protocols"
        :key="p"
        :class="platformBadgeClass(p)"
        class="inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium"
      >
        {{ platformLabel(p) }}
      </span>
    </div>
  </RouterLink>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import AppIcon from '@/components/apps/AppIcon.vue'
import { platformBadgeClass, platformLabel } from '@/utils/platformColors'
import type { AppEntry } from '@/apps/registry'

interface Props {
  app: AppEntry
  locale: 'zh' | 'en'
  siteName: string
}

const props = defineProps<Props>()

const stepsLabel = computed(() =>
  props.locale === 'zh' ? `${props.app.steps} 步` : `${props.app.steps} steps`,
)

const tagline = computed(() =>
  props.app.tagline.replace(/\{\{SITE_NAME\}\}/g, props.siteName),
)
</script>
