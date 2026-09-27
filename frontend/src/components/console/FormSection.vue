<template>
  <section class="zt-fsec">
    <div class="zt-fsec-head">
      <h2><slot name="title">{{ title }}</slot></h2>
      <p v-if="description || $slots.description"><slot name="description">{{ description }}</slot></p>
    </div>
    <div class="zt-fsec-body"><slot /></div>
  </section>
</template>

<script setup lang="ts">
/** 设置类表单分区：左标题与说明，右字段。 */
defineProps<{ title?: string; description?: string }>()
</script>
