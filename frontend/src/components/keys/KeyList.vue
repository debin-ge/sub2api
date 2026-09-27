<template>
  <div class="zt-split-list-items" data-testid="key-list">
    <div v-if="loading && keys.length === 0" class="zt-empty">
      <LoadingSpinner size="md" />
    </div>
    <div v-else-if="keys.length === 0" class="zt-empty">
      <slot name="empty" />
    </div>
    <template v-else>
      <div
        v-for="item in keys"
        :key="item.id"
        class="zt-split-item zt-key-item"
        :class="{ 'is-active': item.id === activeId, 'is-off': item.status !== 'active' }"
        role="button"
        tabindex="0"
        :aria-pressed="item.id === activeId"
        @click="$emit('select', item.id)"
        @keydown.enter.prevent="$emit('select', item.id)"
        @keydown.space.prevent="$emit('select', item.id)"
      >
        <div class="zt-split-item-title">
          <input
            v-if="selectable"
            type="checkbox"
            class="cb"
            :checked="selectedIds.includes(item.id)"
            :aria-label="t('keys.bulkEdit.selectKey', { name: item.name })"
            @click.stop
            @change="toggleSelected(item.id, ($event.target as HTMLInputElement).checked)"
          />
          <i :class="statusDotClass(item.status)"></i>
          <span>{{ item.name }}</span>
          <Icon
            v-if="item.ip_whitelist?.length > 0 || item.ip_blacklist?.length > 0"
            name="shield"
            size="xs"
            class="zt-key-item-shield"
            :title="t('keys.ipRestrictionEnabled')"
          />
        </div>
        <div class="zt-split-item-meta">
          <span>{{ maskApiKey(item.key) }}</span>
          <b>${{ (usageStats[item.id]?.today_actual_cost ?? 0).toFixed(4) }}</b>
        </div>
        <div v-if="item.quota > 0" class="zt-meter is-thin" :class="quotaMeterClass(item)">
          <i :style="{ width: Math.min((item.quota_used / item.quota) * 100, 100) + '%' }"></i>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
/**
 * 密钥列表列（分栏视图左侧）：与表格视图共用同一份数据、筛选、分页与勾选状态，只换排布。
 */
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { maskApiKey } from '@/utils/maskApiKey'
import type { ApiKey } from '@/types'
import type { BatchApiKeyUsageStats } from '@/api/usage'

const props = withDefaults(defineProps<{
  keys: ApiKey[]
  activeId: number | null
  selectedIds?: number[]
  selectable?: boolean
  usageStats: Record<string, BatchApiKeyUsageStats>
  loading?: boolean
}>(), { selectedIds: () => [], selectable: true, loading: false })

const emit = defineEmits<{
  select: [id: number]
  'update:selectedIds': [ids: number[]]
}>()

const { t } = useI18n()

function toggleSelected(id: number, checked: boolean) {
  const next = checked ? Array.from(new Set([...props.selectedIds, id])) : props.selectedIds.filter((x) => x !== id)
  emit('update:selectedIds', next)
}

function statusDotClass(status: string) {
  return status === 'active' ? '' : status === 'quota_exhausted' ? 'is-warn' : status === 'expired' ? 'is-bad' : 'is-muted'
}

function quotaMeterClass(item: ApiKey) {
  if (item.quota_used >= item.quota) return 'is-bad'
  if (item.quota_used >= item.quota * 0.8) return 'is-warn'
  return ''
}
</script>

<style scoped>
.zt-key-item .zt-split-item-title i.is-warn {
  background: var(--zt-warn);
}
.zt-key-item .zt-split-item-title i.is-bad {
  background: var(--zt-bad);
}
.zt-key-item .zt-split-item-title i.is-muted {
  background: var(--zt-ink-3);
}
.zt-key-item-shield {
  color: var(--zt-accent);
  flex: none;
}
.cb {
  width: 14px;
  height: 14px;
  accent-color: var(--zt-accent-500);
  margin: 0;
  flex: none;
}
</style>
