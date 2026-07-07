<template>
  <div class="min-w-0">
    <div class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
      <span class="inline-block h-3 w-0.5 rounded-full bg-gray-300 dark:bg-dark-600" aria-hidden="true"></span>
      <span class="truncate">{{ label }}</span>
    </div>
    <div class="mt-1 flex items-baseline gap-1">
      <span class="text-2xl font-extrabold tracking-tight text-emerald-600 dark:text-emerald-400">
        {{ rechargedDisplay }}
      </span>
      <span v-if="hasValue" class="text-xs font-medium text-gray-400 dark:text-gray-500">
        {{ unitLabel }}
      </span>
    </div>
    <div v-if="hasValue" class="mt-0.5 flex items-center gap-1 text-xs">
      <span class="text-gray-400 line-through decoration-gray-400 dark:text-gray-500 dark:decoration-gray-500">
        {{ originalDisplay }}
      </span>
    </div>
    <div v-else class="mt-1 text-xs text-gray-400 dark:text-gray-600">
      {{ t('plaza.card.notAvailable') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  PER_REQUEST_SCALE,
  formatCNYRecharged,
  formatScaled
} from '@/utils/pricing'

const props = defineProps<{
  label: string
  value: number | null
  scale: number
  multiplier: number
  rate: number
  billingRateMultiplier?: number
}>()

const { t } = useI18n()

const hasValue = computed(() => props.value != null)
const originalDisplay = computed(() => formatScaled(props.value, props.scale))
const rechargedDisplay = computed(() =>
  formatCNYRecharged(props.value, props.multiplier, props.scale, props.billingRateMultiplier)
)

const unitLabel = computed(() =>
  props.scale === PER_REQUEST_SCALE
    ? t('plaza.price.unitPerRequest')
    : t('plaza.price.unitPerMillion')
)
</script>
