<template>
  <div class="flex items-start gap-3 rounded-lg border border-gray-200 bg-gray-50/60 px-4 py-3 text-sm dark:border-dark-800 dark:bg-dark-900/60">
    <svg
      class="mt-0.5 h-4 w-4 shrink-0 text-gray-400 dark:text-gray-500"
      viewBox="0 0 20 20"
      fill="currentColor"
      aria-hidden="true"
    >
      <path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2h-.05V10a1 1 0 00-.95-1h-1z" clip-rule="evenodd" />
    </svg>
    <div class="min-w-0 text-xs leading-relaxed text-gray-500 dark:text-gray-400">
      <p>{{ t('plaza.infoBanner.text') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>
