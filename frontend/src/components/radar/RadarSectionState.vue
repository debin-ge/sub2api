<template>
  <div class="relative">
    <div
      role="status"
      aria-live="polite"
      aria-atomic="true"
      class="mb-3 flex flex-wrap items-center gap-2 text-sm"
    >
      <span v-if="loading" class="inline-flex items-center gap-2 text-gray-600 dark:text-gray-300">
        <Icon name="refresh" size="sm" class="animate-spin motion-reduce:animate-none" aria-hidden="true" />
        {{ t('radar.state.loading', 'Loading') }}
      </span>
      <span v-if="error" class="inline-flex items-center gap-2 text-red-700 dark:text-red-300">
        <Icon name="exclamationCircle" size="sm" aria-hidden="true" />
        {{ errorMessage }}
      </span>
    </div>

    <slot v-if="hasContent" />
    <div
      v-else-if="loading"
      class="rounded-xl border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400"
    >
      <slot name="loading">{{ t('radar.state.loading', 'Loading') }}</slot>
    </div>
    <div
      v-else-if="error"
      class="rounded-xl border border-red-200 bg-red-50 p-8 text-center text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/20 dark:text-red-300"
    >
      <slot name="error">{{ errorMessage }}</slot>
    </div>
    <div
      v-else-if="empty"
      class="rounded-xl border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400"
    >
      <slot name="empty">{{ t('radar.state.empty', 'No data available') }}</slot>
    </div>
    <slot v-else />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  loading?: boolean
  error?: string | null
  empty?: boolean
  hasContent?: boolean
}>(), {
  loading: false,
  error: null,
  empty: false,
  hasContent: false,
})

const { t } = useI18n()

const errorMessage = computed(() => {
  switch (props.error) {
    case 'load_failed':
      return t('radar.error.loadFailed', 'Unable to load this section')
    default:
      return t('radar.error.generic', 'Unable to load this section')
  }
})
</script>
