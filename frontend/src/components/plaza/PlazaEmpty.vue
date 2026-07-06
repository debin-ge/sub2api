<template>
  <div class="py-16 text-center">
    <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ title }}</h2>
    <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ subtitle }}</p>
  </div>
</template>

<script setup lang="ts">
defineProps<{ title: string; subtitle: string }>()
</script>
