<template>
  <div class="zt-kpi">
    <div class="zt-kpi-main">
      <div class="zt-kpi-label"><slot name="label">{{ label }}</slot></div>
      <div class="zt-kpi-value" :style="valueColor ? { color: valueColor } : undefined" :title="valueTitle">
        <slot name="value">{{ value }}</slot>
      </div>
      <div v-if="$slots.sub || sub" class="zt-kpi-sub"><slot name="sub">{{ sub }}</slot></div>
    </div>
    <div v-if="spark && spark.length > 1" class="zt-kpi-spark"><Sparkline :data="spark" /></div>
    <slot name="aside" />
  </div>
</template>

<script setup lang="ts">
import Sparkline from './Sparkline.vue'

defineProps<{
  label?: string
  value?: string | number
  /** 例如 'var(--zt-good)'；不传沿用墨色 */
  valueColor?: string
  valueTitle?: string
  sub?: string
  spark?: number[]
}>()
</script>
