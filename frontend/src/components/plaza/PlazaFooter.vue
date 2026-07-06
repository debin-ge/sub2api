<template>
  <footer class="border-t border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-950">
    <div class="mx-auto flex max-w-7xl flex-col gap-4 px-4 py-8 text-sm text-gray-500 dark:text-gray-400 sm:px-6 lg:flex-row lg:items-center lg:justify-between lg:px-8">
      <div class="max-w-3xl space-y-1">
        <p>{{ t('plaza.footer.scaleNote') }}</p>
        <p>{{ t('plaza.footer.referenceRateDisclaimer') }}</p>
      </div>
      <div v-if="showRechargeCta" class="flex shrink-0 gap-2">
        <router-link
          to="/register"
          class="rounded-lg border border-gray-300 px-3 py-2 font-medium text-gray-700 transition hover:bg-gray-50 dark:border-dark-700 dark:text-gray-200 dark:hover:bg-dark-800"
        >
          {{ t('plaza.footer.registerCta') }}
        </router-link>
        <router-link
          to="/purchase"
          class="rounded-lg bg-primary-600 px-3 py-2 font-medium text-white transition hover:bg-primary-700"
        >
          {{ t('plaza.footer.rechargeCta') }}
        </router-link>
      </div>
    </div>
  </footer>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ showRechargeCta: boolean }>()

const { t } = useI18n()
</script>
