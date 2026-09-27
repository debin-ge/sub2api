<template>
  <div class="zt-frow">
    <div class="zt-frow-label">
      <b><slot name="label">{{ label }}</slot></b>
      <small v-if="hint || $slots.hint"><slot name="hint">{{ hint }}</slot></small>
    </div>
    <div class="zt-frow-control"><slot /></div>
  </div>
</template>

<script setup lang="ts">
defineProps<{ label?: string; hint?: string }>()
</script>
