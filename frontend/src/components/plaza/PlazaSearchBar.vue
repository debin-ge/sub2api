<template>
  <div class="flex flex-col gap-3 sm:flex-row sm:items-center">
    <label class="relative flex-1">
      <span class="sr-only">{{ t('plaza.filters.search') }}</span>
      <svg
        class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400"
        viewBox="0 0 20 20"
        fill="currentColor"
        aria-hidden="true"
      >
        <path fill-rule="evenodd" d="M8 4a4 4 0 100 8 4 4 0 000-8zM2 8a6 6 0 1110.89 3.476l4.817 4.817a1 1 0 01-1.414 1.414l-4.816-4.816A6 6 0 012 8z" clip-rule="evenodd" />
      </svg>
      <input
        :value="query"
        type="search"
        class="h-11 w-full rounded-xl border border-gray-200 bg-white pl-10 pr-4 text-sm text-gray-900 outline-none transition placeholder:text-gray-400 focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-800 dark:bg-dark-900 dark:text-white"
        :placeholder="t('plaza.filters.searchPlaceholder')"
        @input="$emit('update:query', ($event.target as HTMLInputElement).value)"
      />
    </label>
    <div class="flex items-center justify-end gap-3 text-xs text-gray-500 dark:text-gray-400">
      <span v-if="hasActiveFilters" class="text-primary-600 dark:text-primary-400">
        {{ t('plaza.searchBar.filtered', { visible: visibleCount, total: totalCount }) }}
      </span>
      <span v-else>
        {{ t('plaza.searchBar.total', { total: totalCount }) }}
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  query: string
  visibleCount: number
  totalCount: number
  hasActiveFilters: boolean
}>()

defineEmits<{
  'update:query': [value: string]
}>()

const { t } = useI18n()
</script>
