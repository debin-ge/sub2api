<template>
  <div class="zt-toolbar">
    <slot />
    <span class="zt-toolbar-spacer"></span>
    <slot name="actions" />
  </div>
</template>

<script setup lang="ts">
/** 面板工具栏：默认插槽在左（筛选），actions 在右。 */
</script>
