<template>
  <div class="zt-hbars">
    <div v-for="(item, i) in items" :key="item.key ?? item.label" class="zt-hbar">
      <div class="zt-hbar-name">
        <i :style="{ background: item.color || colorAt(i) }"></i>
        <span :title="item.label">{{ item.label }}</span>
      </div>
      <div class="zt-hbar-track">
        <i :style="{ width: `${max > 0 ? ((item.value / max) * 100).toFixed(1) : 0}%`, background: item.color || colorAt(i) }"></i>
      </div>
      <div class="zt-hbar-value">
        {{ format ? format(item.value) : item.value }}
        <b v-if="item.sub">{{ item.sub }}</b>
      </div>
    </div>
    <div v-if="!items.length" class="zt-empty">{{ emptyText }}</div>
  </div>
</template>

<script setup lang="ts">
/** 横条排行：类别色按固定顺序取自图表主题。 */
import { computed } from 'vue'
import { useChartTheme } from '@/composables/useChartTheme'

export interface HorizontalBarItem {
  key?: string
  label: string
  value: number
  color?: string
  sub?: string
}
const props = withDefaults(defineProps<{ items: HorizontalBarItem[]; format?: (v: number) => string; emptyText?: string }>(), { emptyText: '' })
const { colorAt } = useChartTheme()
const max = computed(() => Math.max(0, ...props.items.map((i) => i.value)))
</script>
