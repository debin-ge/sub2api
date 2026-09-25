<template>
  <div class="flex flex-wrap items-center gap-3">
    <p class="text-sm tabular-nums text-gray-500 dark:text-gray-400">
      <template v-if="hasActiveFilters">
        {{ t('plaza.searchBar.filtered', { visible: visibleCount, total: totalCount }) }}
      </template>
      <template v-else>
        {{ t('plaza.searchBar.total', { total: totalCount }) }}
      </template>
    </p>
    <button
      type="button"
      class="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 bg-white px-3 py-1.5 text-xs font-medium text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-200 lg:hidden"
      :aria-expanded="filtersOpen"
      @click="$emit('toggle-filters')"
    >
      <svg class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
        <path fill-rule="evenodd" d="M3 3a1 1 0 011-1h12a1 1 0 01.8 1.6L12 10v5a1 1 0 01-.55.9l-2 1A1 1 0 018 16v-6L3.2 3.6A1 1 0 013 3z" clip-rule="evenodd" />
      </svg>
      {{ t('plaza.filters.title') }}<template v-if="activeFilterCount > 0"> · {{ activeFilterCount }}</template>
    </button>
    <div class="flex w-full items-center gap-2 sm:ml-auto sm:w-auto">
      <label class="relative min-w-0 flex-1 sm:flex-none">
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
          class="h-9 w-full rounded-lg border border-gray-200 bg-white pl-9 pr-3 text-sm text-gray-900 outline-none transition placeholder:text-gray-400 focus:border-primary-400 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-700 dark:bg-dark-900 dark:text-white sm:w-64"
          :placeholder="t('plaza.filters.searchPlaceholder')"
          @input="$emit('update:query', ($event.target as HTMLInputElement).value)"
        />
      </label>
      <select
        :value="sort"
        :aria-label="t('plaza.filters.sort')"
        class="h-9 shrink-0 rounded-lg border border-gray-200 bg-white px-3 pr-8 text-sm text-gray-900 outline-none transition focus:border-primary-400 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-700 dark:bg-dark-900 dark:text-white"
        @change="$emit('update:sort', ($event.target as HTMLSelectElement).value as PlazaSort)"
      >
        <option value="popularity">{{ t('plaza.filters.sortPopularity') }}</option>
        <option value="default">{{ t('plaza.filters.sortDefault') }}</option>
        <option value="input_asc">{{ t('plaza.filters.sortInputAsc') }}</option>
        <option value="input_desc">{{ t('plaza.filters.sortInputDesc') }}</option>
      </select>
      <div
        role="group"
        :aria-label="t('plaza.view.label')"
        class="inline-flex h-9 shrink-0 items-center rounded-lg border border-gray-200 bg-white p-0.5 dark:border-dark-700 dark:bg-dark-900"
      >
        <button
          v-for="option in viewOptions"
          :key="option.value"
          type="button"
          :data-testid="`plaza-view-${option.value}`"
          :aria-pressed="view === option.value"
          :aria-label="t(option.labelKey)"
          :title="t(option.labelKey)"
          :class="[
            'inline-flex h-full items-center justify-center rounded-md px-2 transition',
            view === option.value
              ? 'bg-primary-50 text-primary-600 dark:bg-primary-500/15 dark:text-primary-300'
              : 'text-gray-400 hover:text-gray-700 dark:hover:text-gray-200'
          ]"
          @click="$emit('update:view', option.value)"
        >
          <svg class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
            <path v-if="option.value === 'card'" d="M3 3h6v6H3V3zm8 0h6v6h-6V3zM3 11h6v6H3v-6zm8 0h6v6h-6v-6z" />
            <path v-else fill-rule="evenodd" d="M3 4.5A1.5 1.5 0 014.5 3h11a1.5 1.5 0 010 3h-11A1.5 1.5 0 013 4.5zm0 5.5A1.5 1.5 0 014.5 8.5h11a1.5 1.5 0 010 3h-11A1.5 1.5 0 013 10zm1.5 4a1.5 1.5 0 000 3h11a1.5 1.5 0 000-3h-11z" clip-rule="evenodd" />
          </svg>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PlazaSort } from '@/composables/useModelAggregation'

export type PlazaViewMode = 'card' | 'list'

defineProps<{
  query: string
  sort: PlazaSort
  view: PlazaViewMode
  visibleCount: number
  totalCount: number
  hasActiveFilters: boolean
  activeFilterCount: number
  filtersOpen: boolean
}>()

defineEmits<{
  'update:query': [value: string]
  'update:sort': [value: PlazaSort]
  'update:view': [value: PlazaViewMode]
  'toggle-filters': []
}>()

const { t } = useI18n()

const viewOptions: { value: PlazaViewMode; labelKey: string }[] = [
  { value: 'card', labelKey: 'plaza.view.card' },
  { value: 'list', labelKey: 'plaza.view.list' }
]
</script>
