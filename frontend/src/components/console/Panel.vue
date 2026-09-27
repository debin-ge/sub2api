<template>
  <div class="zt-panel">
    <div v-if="title || $slots.title || $slots.actions" class="zt-panel-head">
      <h3>
        <slot name="title">{{ title }}</slot>
        <span v-if="subtitle || $slots.subtitle" class="zt-sub"><slot name="subtitle">{{ subtitle }}</slot></span>
      </h3>
      <div v-if="$slots.actions" class="zt-panel-actions"><slot name="actions" /></div>
    </div>
    <div v-if="!flush" class="zt-panel-body"><slot /></div>
    <slot v-else />
    <slot name="footer" />
  </div>
</template>

<script setup lang="ts">
/** 细边框面板；flush=true 时内容不加内边距（表格、页签等自带布局的内容）。 */
defineProps<{ title?: string; subtitle?: string; flush?: boolean }>()
</script>

<style scoped>
.zt-panel-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
