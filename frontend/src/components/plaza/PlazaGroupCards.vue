<template>
  <ul class="grid gap-2.5 sm:grid-cols-2" data-testid="plaza-group-cards">
    <li
      v-for="group in cards"
      :key="group.id"
      :data-vip="group.vip || undefined"
      :class="[
        'relative overflow-hidden rounded-xl border py-3 pl-4 pr-3 transition',
        group.vip
          ? 'border-orange-200 bg-gradient-to-br from-orange-50 to-white dark:border-orange-500/30 dark:from-orange-500/10 dark:to-dark-900'
          : 'border-gray-200 bg-gradient-to-br from-primary-50/60 to-white dark:border-dark-700 dark:from-primary-500/10 dark:to-dark-900'
      ]"
    >
      <span
        :class="[
          'absolute inset-y-0 left-0 w-1',
          group.vip ? 'bg-gradient-to-b from-orange-400 to-rose-500' : 'bg-gradient-to-b from-primary-500 to-accent-400'
        ]"
        aria-hidden="true"
      ></span>

      <div class="flex items-start gap-3">
        <div class="min-w-0 flex-1">
          <div class="flex min-w-0 items-center gap-1.5">
            <span class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ group.name }}</span>
            <span
              v-if="group.vip"
              class="shrink-0 rounded bg-orange-500 px-1 text-[9px] font-bold uppercase leading-4 text-white"
            >
              {{ t('plaza.price.vipLabel') }}
            </span>
            <span
              v-if="group.best"
              data-testid="plaza-group-best"
              class="shrink-0 rounded bg-emerald-500/10 px-1 text-[10px] font-semibold leading-4 text-emerald-600 dark:text-emerald-400"
            >
              {{ t('plaza.modal.bestRate') }}
            </span>
          </div>
        </div>

        <div class="shrink-0 text-right">
          <div
            :class="[
              'font-mono text-lg font-bold leading-6 tabular-nums',
              group.vip ? 'text-orange-600 dark:text-orange-400' : 'text-primary-600 dark:text-primary-300'
            ]"
          >
            ×{{ group.rate }}
          </div>
          <div class="text-[10px] leading-3 text-gray-400">{{ t('plaza.modal.groupRate') }}</div>
        </div>
      </div>

      <div v-if="group.mediaRate || group.peakWindow" class="mt-2 flex flex-wrap gap-1.5">
        <span
          v-if="group.mediaRate"
          :class="[
            'inline-flex items-center rounded-md px-1.5 py-0.5 text-[11px] font-medium',
            group.mediaRate.kind === 'image'
              ? 'bg-fuchsia-50 text-fuchsia-700 dark:bg-fuchsia-500/10 dark:text-fuchsia-300'
              : 'bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-300'
          ]"
        >
          {{ t(group.mediaRate.kind === 'image' ? 'plaza.modal.imageRate' : 'plaza.modal.videoRate', { rate: group.mediaRate.rate }) }}
        </span>
        <span
          v-if="group.peakWindow"
          class="inline-flex items-center gap-1 rounded-md bg-amber-50 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-500/10 dark:text-amber-300"
        >
          <svg class="h-3 w-3" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm.75-12.5a.75.75 0 00-1.5 0V10c0 .2.08.39.22.53l2.5 2.5a.75.75 0 101.06-1.06l-2.28-2.28V5.5z" clip-rule="evenodd" />
          </svg>
          {{ t('plaza.card.peakPrice') }} {{ group.peakWindow }}
        </span>
      </div>
    </li>
  </ul>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { summarizePlazaGroups } from './plazaGroups'
import type { PlazaBillingKind, PlazaSupportedGroup } from '@/composables/useModelAggregation'

const props = defineProps<{
  groups: PlazaSupportedGroup[]
  billingKind: PlazaBillingKind
  serverUtcOffset?: string
}>()

const { t } = useI18n()

// 普通与 VIP 各自比较：同类有多个分组时，倍率最低的标「最优」。
const cards = computed(() => {
  const groups = summarizePlazaGroups(props.groups, props.billingKind, props.serverUtcOffset)
  const lowest = (vip: boolean) => {
    const rates = groups.filter((group) => group.vip === vip).map((group) => group.rate)
    return rates.length > 1 && new Set(rates).size > 1 ? Math.min(...rates) : null
  }
  const best = { standard: lowest(false), vip: lowest(true) }
  return groups.map((group) => ({
    ...group,
    best: group.rate === (group.vip ? best.vip : best.standard)
  }))
})
</script>
