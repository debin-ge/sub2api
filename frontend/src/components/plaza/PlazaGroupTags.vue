<template>
  <ul class="flex flex-wrap gap-1.5" data-testid="plaza-group-tags">
    <li
      v-for="tag in tags"
      :key="tag.id"
      :title="tag.title"
      :data-vip="tag.vip || undefined"
      :class="[
        'inline-flex max-w-full items-center gap-1 rounded-md border font-medium',
        size === 'sm' ? 'px-1.5 py-px text-[11px]' : 'px-2 py-0.5 text-xs',
        tag.vip
          ? 'border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-500/30 dark:bg-orange-500/10 dark:text-orange-300'
          : 'border-gray-200 bg-gray-50 text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-200'
      ]"
    >
      <span class="truncate">{{ tag.name }}</span>
      <span
        v-if="tag.vip"
        class="shrink-0 rounded bg-orange-500 px-1 text-[9px] font-bold uppercase leading-4 text-white"
      >
        {{ t('plaza.price.vipLabel') }}
      </span>
    </li>
  </ul>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { summarizePlazaGroups } from './plazaGroups'
import type { PlazaBillingKind, PlazaSupportedGroup } from '@/composables/useModelAggregation'

const props = withDefaults(defineProps<{
  groups: PlazaSupportedGroup[]
  billingKind: PlazaBillingKind
  serverUtcOffset?: string
  size?: 'sm' | 'md'
}>(), {
  serverUtcOffset: undefined,
  size: 'md'
})

const { t } = useI18n()

// 标签只显示分组名；渠道、倍率等细节放进悬浮提示。
const tags = computed(() =>
  summarizePlazaGroups(props.groups, props.billingKind, props.serverUtcOffset).map((group) => {
    const details = [`×${group.rate}`]
    if (group.mediaRate) {
      details.push(t(group.mediaRate.kind === 'image' ? 'plaza.modal.imageRate' : 'plaza.modal.videoRate', { rate: group.mediaRate.rate }))
    }
    if (group.peakWindow) details.push(group.peakWindow)
    details.push(...group.channels)
    return { ...group, title: details.join(' · ') }
  })
)
</script>
