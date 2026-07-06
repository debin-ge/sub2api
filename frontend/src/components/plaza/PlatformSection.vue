<template>
  <section class="space-y-4">
    <div class="flex items-center justify-between gap-3">
      <h2 class="flex min-w-0 items-center gap-2 text-lg font-semibold text-gray-900 dark:text-white">
        <PlatformIcon :platform="section.platform as GroupPlatform" size="md" />
        <span class="truncate">{{ section.platform }}</span>
      </h2>
      <span class="shrink-0 text-sm text-gray-500 dark:text-gray-400">
        {{ section.models.length }} 个模型
      </span>
    </div>
    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
      <ModelCard
        v-for="model in section.models"
        :key="model.model"
        :model="model"
        :multiplier="multiplier"
        :rate="rate"
        @open-detail="$emit('open-detail', $event)"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import ModelCard from './ModelCard.vue'
import type { AggregatedModel, PlazaPlatformSection } from '@/composables/useModelAggregation'
import type { GroupPlatform } from '@/types'

defineProps<{
  section: PlazaPlatformSection
  multiplier: number
  rate: number
}>()

defineEmits<{ 'open-detail': [model: AggregatedModel] }>()
</script>
