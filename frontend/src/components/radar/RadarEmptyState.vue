<template>
  <div
    class="rounded-lg border border-dashed border-gray-300 bg-white px-6 py-10 text-center dark:border-dark-700 dark:bg-dark-900"
  >
    <div class="mx-auto mb-3 flex h-10 w-10 items-center justify-center rounded-lg bg-gray-100 dark:bg-dark-800">
      <span class="text-lg font-semibold text-gray-500 dark:text-dark-300">R</span>
    </div>
    <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ title }}</h3>
    <p v-if="description" class="mt-2 text-sm text-gray-500 dark:text-dark-400">
      {{ description }}
    </p>
  </div>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    title?: string
    description?: string
  }>(),
  {
    title: '暂无 Radar 数据',
    description: '公开基准完成后会在这里展示模型能力排行。',
  },
)
</script>
