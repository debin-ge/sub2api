<template>
  <div class="py-16 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('plaza.loading') }}</div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>
