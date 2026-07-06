<template>
  <article
    class="group flex h-full cursor-pointer flex-col rounded-lg border border-gray-200 bg-white p-4 shadow-sm transition hover:-translate-y-0.5 hover:shadow-md dark:border-dark-700 dark:bg-dark-800"
    tabindex="0"
    role="button"
    @click="$emit('open-detail', model)"
    @keydown.enter="$emit('open-detail', model)"
    @keydown.space.prevent="$emit('open-detail', model)"
  >
    <header class="mb-4 flex items-start justify-between gap-3">
      <div class="min-w-0">
        <h3 class="truncate text-base font-semibold text-gray-900 dark:text-white">
          {{ model.displayName }}
        </h3>
        <p class="truncate text-xs text-gray-500 dark:text-gray-400">{{ model.model }}</p>
      </div>
      <span
        :class="[
          'inline-flex shrink-0 items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium uppercase',
          platformBadgeClass(model.platform)
        ]"
      >
        <PlatformIcon :platform="model.platform as GroupPlatform" size="xs" />
        {{ model.platform }}
      </span>
    </header>

    <div class="flex flex-1 flex-col gap-3">
      <PriceLine
        label="输入"
        :value="model.minPricing.input"
        :scale="PER_MILLION_TOKEN_SCALE"
        :multiplier="multiplier"
        :rate="rate"
      />
      <PriceLine
        label="输出"
        :value="model.minPricing.output"
        :scale="PER_MILLION_TOKEN_SCALE"
        :multiplier="multiplier"
        :rate="rate"
      />
      <PriceLine
        label="缓存写"
        :value="model.minPricing.cacheWrite"
        :scale="PER_MILLION_TOKEN_SCALE"
        :multiplier="multiplier"
        :rate="rate"
      />
      <PriceLine
        label="缓存读"
        :value="model.minPricing.cacheRead"
        :scale="PER_MILLION_TOKEN_SCALE"
        :multiplier="multiplier"
        :rate="rate"
      />
    </div>

    <footer class="mt-4 flex items-center justify-between border-t border-gray-100 pt-3 text-sm dark:border-dark-700">
      <span class="text-gray-500 dark:text-gray-400">支持渠道 · {{ model.supportedGroups.length }}</span>
      <span class="font-medium text-primary-600 dark:text-primary-400">查看详情</span>
    </footer>
  </article>
</template>

<script setup lang="ts">
import PriceLine from './PriceLine.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformBadgeClass } from '@/utils/platformColors'
import { PER_MILLION_TOKEN_SCALE } from '@/utils/pricing'
import type { AggregatedModel } from '@/composables/useModelAggregation'
import type { GroupPlatform } from '@/types'

defineProps<{
  model: AggregatedModel
  multiplier: number
  rate: number
}>()

defineEmits<{
  'open-detail': [model: AggregatedModel]
}>()
</script>
