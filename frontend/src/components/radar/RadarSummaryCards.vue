<template>
  <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
    <div
      v-for="card in cards"
      :key="card.label"
      class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900"
    >
      <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">
        {{ card.label }}
      </div>
      <div class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ card.value }}</div>
      <div v-if="card.caption" class="mt-1 text-xs text-gray-500 dark:text-dark-400">
        {{ card.caption }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { BenchmarkPublicRadar } from '@/types/benchmark'

const props = defineProps<{
  radar: BenchmarkPublicRadar
}>()

const scoredTargets = computed(() => props.radar.targets.filter((target) => !target.score_basis.insufficient_sample))
const averageAbilityScore = computed(() => {
  if (props.radar.targets.length === 0) return '-'
  const total = props.radar.targets.reduce((sum, target) => sum + target.overall_score, 0)
  return (total / props.radar.targets.length).toFixed(1)
})

const cards = computed(() => [
  {
    label: '参评模型',
    value: props.radar.targets.length.toString(),
    caption: '公开可见目标',
  },
  {
    label: '平均能力分',
    value: averageAbilityScore.value,
    caption: '仅表示任务能力表现',
  },
  {
    label: '样本状态',
    value: `${scoredTargets.value.length}/${props.radar.targets.length}`,
    caption: '达到有效样本的模型',
  },
  {
    label: '最新运行',
    value: props.radar.latest_run ? `#${props.radar.latest_run.id}` : '-',
    caption: props.radar.latest_run?.completed_at ? formatDate(props.radar.latest_run.completed_at) : '暂无完成记录',
  },
])

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}
</script>
