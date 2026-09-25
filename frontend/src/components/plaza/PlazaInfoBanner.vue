<template>
  <div class="flex items-start gap-3 rounded-xl border border-primary-200/70 bg-gradient-to-r from-primary-50 to-accent-50/60 px-4 py-3 text-sm text-primary-900 dark:border-primary-500/20 dark:from-primary-500/10 dark:to-accent-500/5 dark:text-primary-100">
    <svg class="mt-0.5 h-4 w-4 shrink-0 text-primary-500" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
      <path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clip-rule="evenodd" />
    </svg>
    <p class="min-w-0 text-xs leading-relaxed">{{ t('plaza.infoBanner.text') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>
