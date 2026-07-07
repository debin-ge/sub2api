<template>
  <div class="rounded-2xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-800 dark:bg-dark-900">
    <div class="grid gap-3 lg:grid-cols-[minmax(0,1fr)_13rem_14rem_auto] lg:items-end">
      <label>
        <span class="sr-only">{{ t('plaza.filters.search') }}</span>
        <span class="relative block">
          <svg
            class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400"
            viewBox="0 0 20 20"
            fill="currentColor"
            aria-hidden="true"
          >
            <path fill-rule="evenodd" d="M8 4a4 4 0 100 8 4 4 0 000-8zM2 8a6 6 0 1110.89 3.476l4.817 4.817a1 1 0 01-1.414 1.414l-4.816-4.816A6 6 0 012 8z" clip-rule="evenodd" />
          </svg>
          <input
            :value="modelValue.query"
            type="search"
            class="h-11 w-full rounded-xl border border-gray-200 bg-white pl-10 pr-4 text-sm text-gray-900 outline-none transition placeholder:text-gray-400 focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-700 dark:bg-dark-950 dark:text-white"
            :placeholder="t('plaza.filters.searchPlaceholder')"
            @input="update({ query: ($event.target as HTMLInputElement).value })"
          />
        </span>
      </label>

      <label>
        <span class="mb-1.5 block text-xs font-medium text-gray-500 dark:text-gray-400">
          {{ t('plaza.filters.platform') }}
        </span>
        <div class="relative">
          <select
            :value="modelValue.platform"
            :aria-label="t('plaza.filters.platform')"
            class="h-11 w-full appearance-none rounded-xl border border-gray-200 bg-white px-3 pr-9 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-700 dark:bg-dark-950 dark:text-white"
            @change="update({ platform: ($event.target as HTMLSelectElement).value })"
          >
            <option value="">{{ t('plaza.filters.allPlatforms') }} ({{ totalCount }})</option>
            <option
              v-for="entry in platformEntries"
              :key="entry.platform"
              :value="entry.platform"
            >
              {{ formatPlatformLabel(entry.platform) }} ({{ entry.count }})
            </option>
          </select>
          <SelectChevron />
        </div>
      </label>

      <label>
        <span class="mb-1.5 block text-xs font-medium text-gray-500 dark:text-gray-400">
          {{ t('plaza.filters.sort') }}
        </span>
        <div class="relative">
          <select
            :value="modelValue.sort"
            :aria-label="t('plaza.filters.sort')"
            class="h-11 w-full appearance-none rounded-xl border border-gray-200 bg-white px-3 pr-9 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-700 dark:bg-dark-950 dark:text-white"
            @change="update({ sort: ($event.target as HTMLSelectElement).value as PlazaSort })"
          >
            <option value="popularity">{{ t('plaza.filters.sortPopularity') }}</option>
            <option value="default">{{ t('plaza.filters.sortDefault') }}</option>
            <option value="input_asc">{{ t('plaza.filters.sortInputAsc') }}</option>
            <option value="input_desc">{{ t('plaza.filters.sortInputDesc') }}</option>
          </select>
          <SelectChevron />
        </div>
      </label>

      <div class="flex h-11 items-center justify-start text-xs text-gray-500 dark:text-gray-400 lg:justify-end">
        <span v-if="hasActiveFilters" class="font-medium text-primary-600 dark:text-primary-400">
          {{ t('plaza.searchBar.filtered', { visible: visibleCount, total: totalCount }) }}
        </span>
        <span v-else>
          {{ t('plaza.searchBar.total', { total: totalCount }) }}
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { platformLabel } from '@/utils/platformColors'
import type { PlazaSort } from '@/composables/useModelAggregation'

export interface PlazaFilterState {
  platform: string
  query: string
  sort: PlazaSort
}

export interface PlazaPlatformEntry {
  platform: string
  count: number
}

const props = defineProps<{
  modelValue: PlazaFilterState
  platforms: PlazaPlatformEntry[]
  visibleCount: number
  totalCount: number
  hasActiveFilters: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: PlazaFilterState]
}>()

const { t } = useI18n()

const platformEntries = computed(() => props.platforms)

function update(patch: Partial<PlazaFilterState>) {
  emit('update:modelValue', {
    ...props.modelValue,
    ...patch,
  })
}

function formatPlatformLabel(platform: string) {
  return platformLabel(platform)
}

const SelectChevron = {
  template: `
    <svg
      class="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400"
      viewBox="0 0 20 20"
      fill="currentColor"
      aria-hidden="true"
    >
      <path fill-rule="evenodd" d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z" clip-rule="evenodd" />
    </svg>
  `
}
</script>
