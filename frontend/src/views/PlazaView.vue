<template>
  <div class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <SiteHeader current="plaza" />
    <main>
      <PlazaHero
        :model-count="modelCount"
        :platform-count="platformCount"
        :image-model-count="kindTotals.image"
        :video-model-count="kindTotals.video"
        :multiplier="pricingConfig.multiplier"
      />

      <section class="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
        <div class="grid gap-8 lg:grid-cols-[220px_minmax(0,1fr)]">
          <aside class="hidden lg:block" data-testid="plaza-sidebar">
            <div class="sticky top-24">
              <PlazaFilters
                v-model="filters"
                :platforms="platformEntries"
                :billing-counts="billingCounts"
              />
            </div>
          </aside>

          <div class="min-w-0 space-y-5">
            <div
              v-if="descriptionHtml"
              data-testid="plaza-description"
              class="rounded-xl border border-gray-200 bg-white px-5 py-4 text-sm leading-6 text-gray-600 dark:border-dark-800 dark:bg-dark-900 dark:text-gray-300"
            >
              <div class="plaza-description" v-html="descriptionHtml"></div>
            </div>

            <PlazaToolbar
              v-model:query="filters.query"
              v-model:sort="filters.sort"
              v-model:view="viewMode"
              :visible-count="visibleModelCount"
              :total-count="modelCount"
              :has-active-filters="hasActiveFilters"
              :active-filter-count="activeFilterCount"
              :filters-open="mobileFiltersOpen"
              @toggle-filters="mobileFiltersOpen = !mobileFiltersOpen"
            />

            <div
              v-if="mobileFiltersOpen"
              class="rounded-xl border border-gray-200 bg-gray-50 p-3 dark:border-dark-800 dark:bg-dark-950 lg:hidden"
            >
              <PlazaFilters
                v-model="filters"
                :platforms="platformEntries"
                :billing-counts="billingCounts"
              />
            </div>

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
              <ModelPriceList
                v-if="viewMode === 'list'"
                :models="paginatedModels"
                :server-utc-offset="pricingConfig.serverUtcOffset"
                @open-detail="modal.open"
              />
              <div v-else class="grid grid-cols-1 gap-5 md:grid-cols-2 2xl:grid-cols-3">
                <ModelCard
                  v-for="model in paginatedModels"
                  :key="`${model.platform}::${model.model}`"
                  :model="model"
                  @open-detail="modal.open"
                />
              </div>

              <div
                v-if="visibleModelCount > pageSize"
                class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-800"
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
        </div>
      </section>
    </main>

    <ModelDetailModal
      :open="modalIsOpen"
      :model="modalCurrentModel"
      :server-utc-offset="pricingConfig.serverUtcOffset"
      @close="modal.close"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import SiteHeader from '@/components/common/SiteHeader.vue'
import PlazaHero from '@/components/plaza/PlazaHero.vue'
import PlazaFilters, { type PlazaFilterState, type PlazaPlatformEntry } from '@/components/plaza/PlazaFilters.vue'
import PlazaToolbar, { type PlazaViewMode } from '@/components/plaza/PlazaToolbar.vue'
import PlazaInfoBanner from '@/components/plaza/PlazaInfoBanner.vue'
import PlazaLoading from '@/components/plaza/PlazaLoading.vue'
import PlazaEmpty from '@/components/plaza/PlazaEmpty.vue'
import ModelCard from '@/components/plaza/ModelCard.vue'
import ModelPriceList from '@/components/plaza/ModelPriceList.vue'
import ModelDetailModal from '@/components/plaza/ModelDetailModal.vue'
import Pagination from '@/components/common/Pagination.vue'
import { usePlazaData } from '@/composables/usePlazaData'
import {
  aggregateByPlatformModel,
  sortAggregatedModels,
  type AggregatedModel,
  type PlazaBillingKind
} from '@/composables/useModelAggregation'
import { useModelDetailModal } from '@/composables/useModelDetailModal'

const { t } = useI18n()
const { channels, pricingConfig, loading, error, fetchAll } = usePlazaData()
const modal = useModelDetailModal()
const descriptionHtml = computed(() => {
  const markdown = pricingConfig.value.description.trim()
  if (!markdown) return ''
  return DOMPurify.sanitize(marked.parse(markdown) as string)
})

const filters = ref<PlazaFilterState>({
  platform: '',
  billingType: '',
  query: '',
  sort: 'popularity',
})
const currentPage = ref(1)
// 卡片最多三列，15 条正好铺满五行；列表模式用于快速浏览大量价格，每页放宽到 50 条。
const PAGE_SIZE: Record<PlazaViewMode, number> = { card: 15, list: 50 }
const VIEW_MODE_STORAGE_KEY = 'plaza.viewMode'

function readStoredViewMode(): PlazaViewMode {
  try {
    return localStorage.getItem(VIEW_MODE_STORAGE_KEY) === 'list' ? 'list' : 'card'
  } catch {
    return 'card'
  }
}

const viewMode = ref<PlazaViewMode>(readStoredViewMode())
const pageSize = computed(() => PAGE_SIZE[viewMode.value])

const allSections = computed(() =>
  aggregateByPlatformModel(channels.value)
)

const allModels = computed<AggregatedModel[]>(() =>
  sortAggregatedModels(
    allSections.value.flatMap((section) => section.models),
    filters.value.sort
  )
)

const normalizedQuery = computed(() => filters.value.query.trim().toLowerCase())

const matchesQuery = (model: AggregatedModel) =>
  !normalizedQuery.value ||
  model.model.toLowerCase().includes(normalizedQuery.value) ||
  model.displayName.toLowerCase().includes(normalizedQuery.value)
const matchesPlatform = (model: AggregatedModel) =>
  !filters.value.platform || model.platform === filters.value.platform
const matchesBillingType = (model: AggregatedModel) =>
  !filters.value.billingType || model.billingKind === filters.value.billingType

const emptyKindCounts = (): Record<PlazaBillingKind, number> => ({ text: 0, image: 0, video: 0, per_request: 0 })

function countKinds(models: AggregatedModel[]): Record<PlazaBillingKind, number> {
  const counts = emptyKindCounts()
  for (const model of models) counts[model.billingKind] += 1
  return counts
}

// 分面计数：计费类型的数量随服务商筛选变化，服务商的数量随计费类型筛选变化。
const billingCounts = computed(() =>
  countKinds(allModels.value.filter((model) => matchesPlatform(model) && matchesQuery(model)))
)
const kindTotals = computed(() => countKinds(allModels.value))

const platformEntries = computed<PlazaPlatformEntry[]>(() =>
  allSections.value
    .map((section) => ({
      platform: section.platform,
      count: section.models.filter((model) => matchesBillingType(model) && matchesQuery(model)).length,
    }))
    .filter((entry) => entry.count > 0 || entry.platform === filters.value.platform)
)

const visibleModels = computed<AggregatedModel[]>(() =>
  allModels.value.filter((model) => matchesPlatform(model) && matchesBillingType(model) && matchesQuery(model))
)
const totalPages = computed(() => Math.max(1, Math.ceil(visibleModels.value.length / pageSize.value)))
const paginatedModels = computed<AggregatedModel[]>(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return visibleModels.value.slice(start, start + pageSize.value)
})

const modelCount = computed(() => allModels.value.length)
const platformCount = computed(() => allSections.value.length)
const visibleModelCount = computed(() => visibleModels.value.length)
const hasModels = computed(() => modelCount.value > 0)
const modalIsOpen = computed(() => modal.isOpen.value)
const modalCurrentModel = computed(() => modal.currentModel.value)
const activeFilterCount = computed(() => Number(!!filters.value.platform) + Number(!!filters.value.billingType))
const hasActiveFilters = computed(() => activeFilterCount.value > 0 || !!normalizedQuery.value)
const mobileFiltersOpen = ref(false)
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
  () => [filters.value.platform, filters.value.billingType, filters.value.query, filters.value.sort] as const,
  () => {
    currentPage.value = 1
  }
)

watch(viewMode, (mode) => {
  currentPage.value = 1
  try {
    localStorage.setItem(VIEW_MODE_STORAGE_KEY, mode)
  } catch {
    // 无痕模式等场景下存储不可用，只影响下次打开时的默认视图。
  }
})

watch(visibleModelCount, () => {
  if (currentPage.value > totalPages.value) {
    currentPage.value = totalPages.value
  }
})

onMounted(() => {
  void fetchAll()
})
</script>

<style scoped>
.plaza-description :deep(p) {
  margin: 0.35rem 0;
}

.plaza-description :deep(a) {
  color: #4f46e5;
  text-decoration: underline;
  text-underline-offset: 2px;
}

:global(.dark) .plaza-description :deep(a) {
  color: #818cf8;
}

.plaza-description :deep(ul),
.plaza-description :deep(ol) {
  margin: 0.35rem 0;
  padding-left: 1.25rem;
}

.plaza-description :deep(ul) {
  list-style: disc;
}

.plaza-description :deep(ol) {
  list-style: decimal;
}
</style>
