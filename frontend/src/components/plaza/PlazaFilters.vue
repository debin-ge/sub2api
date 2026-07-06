<template>
  <section class="border-y border-gray-200 bg-gray-50 dark:border-dark-800 dark:bg-dark-900">
    <div class="mx-auto grid max-w-7xl gap-3 px-4 py-4 sm:px-6 md:grid-cols-[minmax(0,1fr)_180px_180px] lg:px-8">
      <label class="min-w-0">
        <span class="sr-only">{{ t('plaza.filters.search') }}</span>
        <input
          :value="modelValue.query"
          type="search"
          class="h-10 w-full rounded-lg border border-gray-300 bg-white px-3 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-700 dark:bg-dark-950 dark:text-white"
          :placeholder="t('plaza.filters.searchPlaceholder')"
          @input="update({ query: ($event.target as HTMLInputElement).value })"
        />
      </label>

      <label>
        <span class="sr-only">{{ t('plaza.filters.platform') }}</span>
        <select
          :value="modelValue.platform"
          class="h-10 w-full rounded-lg border border-gray-300 bg-white px-3 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-700 dark:bg-dark-950 dark:text-white"
          @change="update({ platform: ($event.target as HTMLSelectElement).value })"
        >
          <option value="">{{ t('plaza.filters.allPlatforms') }}</option>
          <option v-for="platform in platforms" :key="platform" :value="platform">{{ platform }}</option>
        </select>
      </label>

      <label>
        <span class="sr-only">{{ t('plaza.filters.sort') }}</span>
        <select
          :value="modelValue.sort"
          class="h-10 w-full rounded-lg border border-gray-300 bg-white px-3 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-700 dark:bg-dark-950 dark:text-white"
          @change="update({ sort: ($event.target as HTMLSelectElement).value as PlazaSort })"
        >
          <option value="default">{{ t('plaza.filters.sortDefault') }}</option>
          <option value="input_asc">{{ t('plaza.filters.sortInputAsc') }}</option>
          <option value="input_desc">{{ t('plaza.filters.sortInputDesc') }}</option>
        </select>
      </label>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PlazaSort } from '@/composables/useModelAggregation'

export interface PlazaFilterState {
  platform: string
  query: string
  sort: PlazaSort
}

const props = defineProps<{
  modelValue: PlazaFilterState
  platforms: string[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: PlazaFilterState]
}>()

const { t } = useI18n()

function update(patch: Partial<PlazaFilterState>) {
  emit('update:modelValue', {
    ...props.modelValue,
    ...patch,
  })
}
</script>
