<template>
  <div class="zt-cell-meter">
    <span>
      <slot name="used">{{ fmt(used) }}</slot>
      <em><slot name="limit">/ {{ limit ? fmt(limit) : unlimitedLabel }}</slot></em>
    </span>
    <div class="zt-meter is-thin" :class="{ 'is-warn': ratio >= warnAt && ratio < 1, 'is-bad': ratio >= 1 }">
      <i :style="{ width: `${Math.min(100, ratio * 100).toFixed(1)}%` }"></i>
    </div>
  </div>
</template>

<script setup lang="ts">
/** 「已用 / 上限」+ 细进度条；无上限时进度为 0，≥ warnAt 变警告色，≥ 100% 变危险色。 */
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  used: number
  limit?: number | null
  format?: (value: number) => string
  unlimitedLabel?: string
  warnAt?: number
}>(), { limit: null, unlimitedLabel: '∞', warnAt: 0.8 })

const fmt = (v: number) => (props.format ? props.format(v) : String(v))
const ratio = computed(() => (props.limit && props.limit > 0 ? props.used / props.limit : 0))
</script>
