<template>
  <div class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <PlazaHeader />
    <main>
      <PlazaHero
        :model-count="modelCount"
        :platform-count="platformCount"
        :rate="pricingConfig.rate"
        :multiplier="pricingConfig.multiplier"
      />

      <section class="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
        <div class="flex flex-col gap-4">
          <PlazaFilters
            v-model="filters"
            :platforms="platformEntries"
            :visible-count="visibleModelCount"
            :total-count="modelCount"
            :has-active-filters="hasActiveFilters"
          />

          <PlazaInfoBanner v-if="hasModels" />

          <PlazaLoading v-if="loading" />
          <PlazaEmpty
            v-else-if="error"
            :title="t('plaza.error.title')"
            :subtitle="error || t('plaza.error.subtitle')"
          />
          <PlazaEmpty
            v-else-if="visibleModels.length === 0"
            :title="emptyTitle"
            :subtitle="emptySubtitle"
          />
          <template v-else>
            <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
              <ModelCard
                v-for="model in paginatedModels"
                :key="`${model.platform}::${model.model}`"
                :model="model"
                :multiplier="pricingConfig.multiplier"
                :rate="pricingConfig.rate"
                @open-detail="modal.open"
              />
            </div>

            <div
              v-if="visibleModelCount > pageSize"
              class="overflow-hidden rounded-xl border border-gray-200 shadow-sm dark:border-dark-800"
            >
              <Pagination
                :page="currentPage"
                :page-size="pageSize"
                :total="visibleModelCount"
                :show-page-size-selector="false"
                @update:page="onPageChange"
                @update:pageSize="onPageSizeChange"
              />
            </div>
          </template>
        </div>
      </section>
    </main>

    <ModelDetailModal
      :open="modalIsOpen"
      :model="modalCurrentModel"
      :multiplier="pricingConfig.multiplier"
      :rate="pricingConfig.rate"
      @close="modal.close"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PlazaHeader from '@/components/plaza/PlazaHeader.vue'
import PlazaHero from '@/components/plaza/PlazaHero.vue'
import PlazaFilters, { type PlazaFilterState, type PlazaPlatformEntry } from '@/components/plaza/PlazaFilters.vue'
import PlazaInfoBanner from '@/components/plaza/PlazaInfoBanner.vue'
import PlazaLoading from '@/components/plaza/PlazaLoading.vue'
import PlazaEmpty from '@/components/plaza/PlazaEmpty.vue'
import ModelCard from '@/components/plaza/ModelCard.vue'
import ModelDetailModal from '@/components/plaza/ModelDetailModal.vue'
import Pagination from '@/components/common/Pagination.vue'
import { usePlazaData } from '@/composables/usePlazaData'
import {
  aggregateByPlatformModel,
  sortAggregatedModels,
  type AggregatedModel
} from '@/composables/useModelAggregation'
import { useModelDetailModal } from '@/composables/useModelDetailModal'

const { t } = useI18n()
const { channels, pricingConfig, loading, error, fetchAll } = usePlazaData()
const modal = useModelDetailModal()

const filters = ref<PlazaFilterState>({
  platform: '',
  query: '',
  sort: 'popularity',
})
const currentPage = ref(1)
const pageSize = 20

const allSections = computed(() =>
  aggregateByPlatformModel(channels.value)
)

const allModels = computed<AggregatedModel[]>(() =>
  sortAggregatedModels(
    allSections.value.flatMap((section) => section.models),
    filters.value.sort
  )
)

const platformEntries = computed<PlazaPlatformEntry[]>(() =>
  allSections.value.map((section) => ({
    platform: section.platform,
    count: section.models.length,
  }))
)

const normalizedQuery = computed(() => filters.value.query.trim().toLowerCase())

const visibleModels = computed<AggregatedModel[]>(() =>
  allModels.value.filter((model) => {
    if (filters.value.platform && model.platform !== filters.value.platform) return false
    if (!normalizedQuery.value) return true
    return (
      model.model.toLowerCase().includes(normalizedQuery.value) ||
      model.displayName.toLowerCase().includes(normalizedQuery.value)
    )
  })
)
const totalPages = computed(() => Math.max(1, Math.ceil(visibleModels.value.length / pageSize)))
const paginatedModels = computed<AggregatedModel[]>(() => {
  const start = (currentPage.value - 1) * pageSize
  return visibleModels.value.slice(start, start + pageSize)
})

const modelCount = computed(() => allModels.value.length)
const platformCount = computed(() => allSections.value.length)
const visibleModelCount = computed(() => visibleModels.value.length)
const hasModels = computed(() => modelCount.value > 0)
const modalIsOpen = computed(() => modal.isOpen.value)
const modalCurrentModel = computed(() => modal.currentModel.value)
const hasActiveFilters = computed(() => !!filters.value.platform || !!normalizedQuery.value)
const emptyTitle = computed(() =>
  hasActiveFilters.value ? t('plaza.empty.filteredTitle') : t('plaza.empty.title')
)
const emptySubtitle = computed(() =>
  hasActiveFilters.value ? t('plaza.empty.filteredSubtitle') : t('plaza.empty.subtitle')
)

function onPageChange(page: number) {
  currentPage.value = Math.max(1, Math.min(page, totalPages.value))
}

function onPageSizeChange() {
  currentPage.value = 1
}

watch(
  () => [filters.value.platform, filters.value.query, filters.value.sort] as const,
  () => {
    currentPage.value = 1
  }
)

watch(visibleModelCount, () => {
  if (currentPage.value > totalPages.value) {
    currentPage.value = totalPages.value
  }
})

onMounted(() => {
  void fetchAll()
})
</script>
