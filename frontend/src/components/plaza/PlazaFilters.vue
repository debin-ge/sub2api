<template>
  <div class="space-y-7">
    <section>
      <p class="mb-2 px-3 text-[11px] font-semibold uppercase tracking-wider text-gray-400">
        {{ t('plaza.filters.billingType') }}
      </p>
      <div class="space-y-0.5 text-sm" role="group" :aria-label="t('plaza.filters.billingType')">
        <button
          v-for="entry in billingOptions"
          :key="entry.value || 'all'"
          type="button"
          :aria-pressed="modelValue.billingType === entry.value"
          :data-testid="`plaza-billing-${entry.value || 'all'}`"
          :class="[
            'relative flex w-full items-center justify-between rounded-lg px-3 py-2 text-left transition-colors',
            modelValue.billingType === entry.value
              ? 'bg-gradient-to-r from-primary-50 to-transparent font-semibold text-primary-700 dark:from-primary-500/15 dark:text-primary-300'
              : 'text-gray-600 hover:bg-white dark:text-gray-400 dark:hover:bg-dark-900'
          ]"
          @click="update({ billingType: entry.value })"
        >
          <span
            v-if="modelValue.billingType === entry.value"
            class="absolute bottom-2 left-0 top-2 w-0.5 rounded-full bg-gradient-to-b from-primary-500 to-accent-400"
            aria-hidden="true"
          ></span>
          <span class="flex items-center gap-2">
            <span v-if="entry.dot" :class="['h-2 w-2 rounded-full', entry.dot]" aria-hidden="true"></span>
            {{ entry.label }}
          </span>
          <span class="text-xs font-normal tabular-nums text-gray-400">{{ entry.count }}</span>
        </button>
      </div>
    </section>

    <section v-if="platforms.length">
      <p class="mb-2 px-3 text-[11px] font-semibold uppercase tracking-wider text-gray-400">
        {{ t('plaza.filters.platform') }}
      </p>
      <div class="space-y-0.5 text-sm" role="group" :aria-label="t('plaza.filters.platform')">
        <button
          type="button"
          :aria-pressed="modelValue.platform === ''"
          :class="platformButtonClass('')"
          @click="update({ platform: '' })"
        >
          {{ t('plaza.filters.allPlatforms') }}
          <span class="text-xs font-normal tabular-nums text-gray-400">{{ platformTotal }}</span>
        </button>
        <button
          v-for="entry in shownPlatforms"
          :key="entry.platform"
          type="button"
          :aria-pressed="modelValue.platform === entry.platform"
          :class="platformButtonClass(entry.platform)"
          @click="update({ platform: entry.platform })"
        >
          <span class="flex min-w-0 items-center gap-2">
            <span
              :class="['flex h-5 w-5 shrink-0 items-center justify-center rounded', platformBadgeLightClass(entry.platform)]"
              aria-hidden="true"
            >
              <PlatformIcon :platform="entry.platform as GroupPlatform" size="xs" />
            </span>
            <span class="truncate">{{ platformLabel(entry.platform) }}</span>
          </span>
          <span class="text-xs font-normal tabular-nums text-gray-400">{{ entry.count }}</span>
        </button>
        <button
          v-if="hiddenPlatformCount > 0 || expanded"
          type="button"
          class="px-3 py-1.5 text-xs text-primary-600 hover:text-primary-700 dark:text-primary-400"
          @click="expanded = !expanded"
        >
          {{ expanded ? t('plaza.filters.showLess') : t('plaza.filters.showMore', { n: hiddenPlatformCount }) }}
        </button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformBadgeLightClass, platformLabel } from '@/utils/platformColors'
import { PLAZA_BILLING_KINDS, PLAZA_BILLING_KIND_STYLES } from './plazaBillingKind'
import type { PlazaBillingKind, PlazaSort } from '@/composables/useModelAggregation'
import type { GroupPlatform } from '@/types'

export interface PlazaFilterState {
  platform: string
  billingType: PlazaBillingKind | ''
  query: string
  sort: PlazaSort
}

export interface PlazaPlatformEntry {
  platform: string
  count: number
}

const COLLAPSED_PLATFORM_COUNT = 6

const props = defineProps<{
  modelValue: PlazaFilterState
  platforms: PlazaPlatformEntry[]
  /** 各计费类型的模型数（已按当前服务商筛选）。 */
  billingCounts: Record<PlazaBillingKind, number>
}>()

const emit = defineEmits<{
  'update:modelValue': [value: PlazaFilterState]
}>()

const { t } = useI18n()

const expanded = ref(false)

const billingOptions = computed(() => {
  const total = PLAZA_BILLING_KINDS.reduce((sum, kind) => sum + props.billingCounts[kind], 0)
  return [
    { value: '' as const, label: t('plaza.filters.allModels'), count: total, dot: '' },
    ...PLAZA_BILLING_KINDS
      .filter((kind) => props.billingCounts[kind] > 0 || props.modelValue.billingType === kind)
      .map((kind) => ({
        value: kind,
        label: t(PLAZA_BILLING_KIND_STYLES[kind].labelKey),
        count: props.billingCounts[kind],
        dot: PLAZA_BILLING_KIND_STYLES[kind].dot
      }))
  ]
})

const platformTotal = computed(() => props.platforms.reduce((sum, entry) => sum + entry.count, 0))

const shownPlatforms = computed(() => {
  if (expanded.value || props.platforms.length <= COLLAPSED_PLATFORM_COUNT) return props.platforms
  const head = props.platforms.slice(0, COLLAPSED_PLATFORM_COUNT)
  // 选中的服务商即使排在折叠区也要可见。
  const selected = props.platforms.find((entry) => entry.platform === props.modelValue.platform)
  return selected && !head.includes(selected) ? [...head, selected] : head
})

const hiddenPlatformCount = computed(() => props.platforms.length - shownPlatforms.value.length)

function platformButtonClass(platform: string) {
  return [
    'flex w-full items-center justify-between gap-2 rounded-lg px-3 py-2 text-left transition-colors',
    props.modelValue.platform === platform
      ? 'bg-white font-medium text-gray-900 shadow-sm ring-1 ring-gray-200 dark:bg-dark-900 dark:text-white dark:ring-dark-800'
      : 'text-gray-600 hover:bg-white dark:text-gray-400 dark:hover:bg-dark-900'
  ]
}

function update(patch: Partial<PlazaFilterState>) {
  emit('update:modelValue', { ...props.modelValue, ...patch })
}
</script>
