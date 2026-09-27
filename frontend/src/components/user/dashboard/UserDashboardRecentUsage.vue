<template>
  <Panel :title="t('dashboard.recentUsage')" flush>
    <template #actions>
      <span class="badge badge-gray">{{ t('dashboard.last7Days') }}</span>
    </template>
    <div v-if="loading" class="flex items-center justify-center py-12">
      <LoadingSpinner size="lg" />
    </div>
    <div v-else-if="data.length === 0" class="py-6">
      <EmptyState :title="t('dashboard.noUsageRecords')" :description="t('dashboard.startUsingApi')" />
    </div>
    <div v-else class="zt-list zt-list-padded">
      <div v-for="log in data" :key="log.id" class="zt-list-row">
        <span class="zt-recent-icon"><Icon name="beaker" size="sm" /></span>
        <div class="min-w-0">
          <b class="truncate">{{ log.model }}</b>
          <small>{{ formatDateTime(log.created_at) }}</small>
        </div>
        <div class="zt-recent-cost">
          <b>
            <span class="zt-good-text" :title="t('dashboard.actual')">${{ formatCost(log.actual_cost) }}</span>
            <span class="zt-strike" :title="t('dashboard.standard')">${{ formatCost(log.total_cost) }}</span>
          </b>
          <small>{{ (log.input_tokens + log.output_tokens).toLocaleString() }} tokens</small>
        </div>
      </div>
      <router-link to="/usage" class="zt-list-more">
        {{ t('dashboard.viewAllUsage') }}
        <Icon name="arrowRight" size="sm" />
      </router-link>
    </div>
  </Panel>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import { Panel } from '@/components/console'
import { formatDateTime } from '@/utils/format'
import type { UsageLog } from '@/types'

defineProps<{
  data: UsageLog[]
  loading: boolean
}>()
const { t } = useI18n()
const formatCost = (c: number) => c.toFixed(4)
</script>

<style scoped>
.zt-list-padded {
  padding: 0 16px;
}
.zt-recent-icon {
  width: 28px;
  height: 28px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  background: var(--zt-accent-50);
  color: var(--zt-accent);
}
.zt-recent-cost {
  text-align: right;
}
.zt-recent-cost b {
  display: flex;
  gap: 6px;
  justify-content: flex-end;
  font: 600 13px/1.3 var(--zt-mono);
  font-variant-numeric: tabular-nums;
}
.zt-recent-cost small {
  display: block;
  font: 11.5px/1.4 var(--zt-mono);
  color: var(--zt-ink-3);
}
.zt-list-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 12px;
  font-size: 13px;
  font-weight: 500;
  color: var(--zt-accent);
  border-top: 1px solid var(--zt-border);
}
</style>
