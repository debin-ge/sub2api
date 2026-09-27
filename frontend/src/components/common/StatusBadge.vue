<template>
  <div class="flex items-center gap-1.5">
    <span
      :class="[
        'inline-block h-2 w-2 rounded-full',
        variantClass
      ]"
    ></span>
    <span class="text-sm zt-ink-2">
      {{ label }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  status: string
  label: string
}>()

const variantClass = computed(() => {
  switch (props.status) {
    case 'active':
    case 'success':
      return 'zt-bg-good'
    case 'disabled':
    case 'inactive':
    case 'warning':
      return 'zt-bg-warn'
    case 'error':
    case 'danger':
      return 'zt-bg-bad'
    default:
      return 'zt-bg-muted'
  }
})
</script>
