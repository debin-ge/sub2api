<template>
  <section class="zt-sec">
    <div class="zt-sec-head">
      <h2>
        <slot name="title">{{ title }}</slot>
        <span v-if="subtitle || $slots.subtitle" class="zt-sub"><slot name="subtitle">{{ subtitle }}</slot></span>
      </h2>
      <div v-if="$slots.actions" class="zt-sec-actions"><slot name="actions" /></div>
    </div>
    <slot />
  </section>
</template>

<script setup lang="ts">
/** 编辑式分区：标题 + 副标题 + 右侧操作 + 细线。 */
defineProps<{ title?: string; subtitle?: string }>()
</script>

<style scoped>
.zt-sec-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
