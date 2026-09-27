<template>
  <span class="zt-sparkline" aria-hidden="true">
    <svg :viewBox="`0 0 ${width} ${height}`" preserveAspectRatio="none">
      <path :d="areaPath" fill="currentColor" opacity="0.12" />
      <path :d="linePath" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round" vector-effect="non-scaling-stroke" />
    </svg>
    <svg :viewBox="`0 0 ${width} ${height}`" class="zt-sparkline-dot">
      <circle :cx="last.x" :cy="last.y" r="3" fill="currentColor" stroke="var(--zt-surface)" stroke-width="2" />
    </svg>
  </span>
</template>

<script setup lang="ts">
/** 迷你面积折线：颜色跟随 currentColor，用于 KPI 项的趋势提示。 */
import { computed } from 'vue'

const props = withDefaults(defineProps<{ data: number[]; width?: number; height?: number }>(), { width: 96, height: 32 })

const points = computed(() => {
  const data = props.data.length > 1 ? props.data : [0, 0]
  const w = props.width, h = props.height, p = 3
  const max = Math.max(...data), min = Math.min(...data)
  const xs = (i: number) => p + (i * (w - 2 * p)) / (data.length - 1)
  const ys = (v: number) => h - p - ((v - min) / (max - min || 1)) * (h - 2 * p)
  return data.map((v, i) => ({ x: +xs(i).toFixed(1), y: +ys(v).toFixed(1) }))
})
const linePath = computed(() => 'M' + points.value.map((pt) => `${pt.x},${pt.y}`).join(' L'))
const areaPath = computed(() => `${linePath.value} L${points.value[points.value.length - 1].x},${props.height} L${points.value[0].x},${props.height} Z`)
const last = computed(() => points.value[points.value.length - 1])
</script>

<style scoped>
.zt-sparkline {
  position: relative;
  display: block;
  width: 100%;
  height: 100%;
}
.zt-sparkline svg {
  display: block;
  width: 100%;
  height: 100%;
  overflow: visible;
}
.zt-sparkline-dot {
  position: absolute;
  inset: 0;
}
</style>
