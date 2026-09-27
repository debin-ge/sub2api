<template>
  <div class="zt-tabstrip" :class="{ 'is-plain': plain }" role="tablist">
    <button
      v-for="tab in tabs"
      :key="tab.key"
      type="button"
      role="tab"
      class="zt-tab"
      :class="{ 'is-active': tab.key === modelValue }"
      :aria-selected="tab.key === modelValue"
      :data-testid="tab.testId"
      @click="$emit('update:modelValue', tab.key)"
    >
      <Icon v-if="tab.icon" :name="tab.icon" size="sm" />
      {{ tab.label }}
      <span v-if="tab.count !== undefined && tab.count !== null" class="zt-tab-count">{{ tab.count }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
/** 下划线页签，可带图标与计数；plain=true 时不带左右内边距（独立于面板使用）。 */
import Icon from '@/components/icons/Icon.vue'

type IconName = InstanceType<typeof Icon>['$props']['name']

export interface TabStripItem {
  key: string
  label: string
  count?: number | string | null
  icon?: IconName
  testId?: string
}
defineProps<{ tabs: TabStripItem[]; modelValue: string; plain?: boolean }>()
defineEmits<{ 'update:modelValue': [key: string] }>()
</script>

<style scoped>
.zt-tabstrip.is-plain {
  padding: 0;
}
</style>
