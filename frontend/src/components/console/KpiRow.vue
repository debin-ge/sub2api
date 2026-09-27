<template>
  <div class="zt-kpis" :style="{ '--zt-kpi-cols': cols }">
    <slot />
  </div>
</template>

<script setup lang="ts">
/** 细线分隔的 KPI 行；cols 为一行的列数，指标多于设计稿时用多行承载而不是裁剪。 */
withDefaults(defineProps<{ cols?: number }>(), { cols: 4 })
</script>
