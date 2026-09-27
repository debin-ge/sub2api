<template>
  <div class="zt-statrow">
    <span v-for="(item, i) in items" :key="i" class="zt-statpill">
      <i v-if="item.tone" :style="{ background: toneColor(item.tone) }"></i>
      {{ item.label }}
      <b>{{ item.value }}</b>
    </span>
    <slot />
  </div>
</template>

<script setup lang="ts">
/** 状态摘要胶囊行：标签 + 等宽数值 + 可选语义色点。 */
export type StatPillTone = 'good' | 'warn' | 'bad' | 'accent' | 'muted'
export interface StatPillItem {
  label: string
  value: string | number
  tone?: StatPillTone
}
defineProps<{ items: StatPillItem[] }>()

const toneColor = (tone: StatPillTone) =>
  ({ good: 'var(--zt-good)', warn: 'var(--zt-warn)', bad: 'var(--zt-bad)', accent: 'var(--zt-accent-500)', muted: 'var(--zt-ink-3)' })[tone]
</script>
