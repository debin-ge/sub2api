<template>
  <div class="min-h-screen bg-white text-gray-900 dark:bg-dark-950 dark:text-white">
    <PlazaHeader />
    <main>
      <PlazaHero :model-count="modelCount" :platform-count="platformCount" :boost="boost" />
      <PlazaFilters v-model="filters" :platforms="platformOptions" />

      <section class="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
        <PlazaLoading v-if="loading" />
        <PlazaEmpty
          v-else-if="error"
          :title="t('plaza.error.title')"
          :subtitle="error || t('plaza.error.subtitle')"
        />
        <PlazaEmpty
          v-else-if="!pricingConfig.availableChannelsEnabled"
          :title="t('plaza.unavailable.title')"
          :subtitle="t('plaza.unavailable.subtitle')"
        />
        <PlazaEmpty
          v-else-if="filteredSections.length === 0"
          :title="emptyTitle"
          :subtitle="emptySubtitle"
        />
        <div v-else class="space-y-10">
          <PlatformSection
            v-for="section in filteredSections"
            :key="section.platform"
            :section="section"
            :multiplier="pricingConfig.multiplier"
            :rate="pricingConfig.rate"
            @open-detail="modal.open"
          />
        </div>
      </section>
    </main>

    <PlazaFooter :show-recharge-cta="showRechargeCta" />
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
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import PlazaHeader from '@/components/plaza/PlazaHeader.vue'
import PlazaHero from '@/components/plaza/PlazaHero.vue'
import PlazaFilters, { type PlazaFilterState } from '@/components/plaza/PlazaFilters.vue'
import PlazaFooter from '@/components/plaza/PlazaFooter.vue'
import PlazaLoading from '@/components/plaza/PlazaLoading.vue'
import PlazaEmpty from '@/components/plaza/PlazaEmpty.vue'
import PlatformSection from '@/components/plaza/PlatformSection.vue'
import ModelDetailModal from '@/components/plaza/ModelDetailModal.vue'
import { usePlazaData } from '@/composables/usePlazaData'
import { aggregateByPlatformModel, type PlazaPlatformSection } from '@/composables/useModelAggregation'
import { useModelDetailModal } from '@/composables/useModelDetailModal'
import { computeValueBoost } from '@/utils/pricing'

const { t } = useI18n()
const { channels, pricingConfig, loading, error, fetchAll } = usePlazaData()
const modal = useModelDetailModal()

const filters = ref<PlazaFilterState>({
  platform: '',
  query: '',
  sort: 'default',
})

const allSections = computed(() => aggregateByPlatformModel(channels.value, { sort: filters.value.sort }))
const platformOptions = computed(() => allSections.value.map((section) => section.platform))
const normalizedQuery = computed(() => filters.value.query.trim().toLowerCase())

const filteredSections = computed<PlazaPlatformSection[]>(() => {
  return allSections.value
    .filter((section) => !filters.value.platform || section.platform === filters.value.platform)
    .map((section) => ({
      platform: section.platform,
      models: section.models.filter((model) => {
        if (!normalizedQuery.value) return true
        return (
          model.model.toLowerCase().includes(normalizedQuery.value) ||
          model.displayName.toLowerCase().includes(normalizedQuery.value)
        )
      }),
    }))
    .filter((section) => section.models.length > 0)
})

const modelCount = computed(() =>
  allSections.value.reduce((total, section) => total + section.models.length, 0)
)
const platformCount = computed(() => allSections.value.length)
const boost = computed(() => computeValueBoost(pricingConfig.value.multiplier, pricingConfig.value.rate))
const showRechargeCta = computed(() => pricingConfig.value.paymentEnabled && !pricingConfig.value.balanceDisabled)
const modalIsOpen = computed(() => modal.isOpen.value)
const modalCurrentModel = computed(() => modal.currentModel.value)
const hasActiveFilters = computed(() => !!filters.value.platform || !!normalizedQuery.value)
const emptyTitle = computed(() => hasActiveFilters.value ? t('plaza.empty.filteredTitle') : t('plaza.empty.title'))
const emptySubtitle = computed(() =>
  hasActiveFilters.value ? t('plaza.empty.filteredSubtitle') : t('plaza.empty.subtitle')
)

onMounted(() => {
  void fetchAll()
})
</script>
