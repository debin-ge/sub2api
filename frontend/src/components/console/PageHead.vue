<template>
  <div class="zt-page-head">
    <div class="min-w-0">
      <h1><slot name="title">{{ title }}</slot></h1>
      <p v-if="description || $slots.description"><slot name="description">{{ description }}</slot></p>
    </div>
    <div v-if="$slots.actions" class="zt-page-actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 页头：标题 / 描述 / 右侧操作插槽。
 * AppLayout 默认按路由 meta 渲染一份；视图需要自定义描述时传 :page-head="false" 后自行使用。
 */
defineProps<{
  title?: string
  description?: string
}>()
</script>
