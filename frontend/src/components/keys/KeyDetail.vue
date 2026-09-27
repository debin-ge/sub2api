<template>
  <div class="zt-split-detail" data-testid="key-detail">
    <!-- 头部：名称 / 状态 / 元信息 / 与表格操作列一一对应的按钮 -->
    <div class="zt-detail-head">
      <h2>
        {{ apiKey.name }}
        <Icon
          v-if="apiKey.ip_whitelist?.length > 0 || apiKey.ip_blacklist?.length > 0"
          name="shield"
          size="sm"
          class="zt-detail-shield"
          :title="t('keys.ipRestrictionEnabled')"
        />
      </h2>
      <span :class="['badge', statusBadgeClass]">{{ t('keys.status.' + apiKey.status) }}</span>
      <span class="zt-meta">#{{ apiKey.id }} · {{ t('keys.created') }} {{ formatDateTime(apiKey.created_at) }}</span>
      <div class="zt-detail-actions">
        <button type="button" class="btn btn-secondary btn-sm" @click="$emit('use')">
          <Icon name="terminal" size="xs" />
          {{ t('keys.useKey') }}
        </button>
        <button v-if="!hideCcsImport" type="button" class="btn btn-secondary btn-sm" @click="$emit('import-ccs')">
          <Icon name="upload" size="xs" />
          {{ t('keys.importToCcSwitch') }}
        </button>
        <button type="button" class="btn btn-secondary btn-sm" @click="$emit('toggle-status')">
          <Icon :name="apiKey.status === 'active' ? 'ban' : 'checkCircle'" size="xs" />
          {{ apiKey.status === 'active' ? t('keys.disable') : t('keys.enable') }}
        </button>
        <button type="button" class="btn btn-secondary btn-sm" @click="$emit('edit')">
          <Icon name="edit" size="xs" />
          {{ t('common.edit') }}
        </button>
        <button type="button" class="btn btn-secondary btn-sm zt-detail-delete" @click="$emit('delete')">
          <Icon name="trash" size="xs" />
          {{ t('common.delete') }}
        </button>
      </div>
    </div>

    <!-- Key 值 + 复制（与表格「密钥」列一致，展示脱敏值） -->
    <div class="zt-keybox">
      <span class="zt-lab">Key</span>
      <code>{{ maskApiKey(apiKey.key) }}</code>
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        :class="copied ? 'zt-good-text' : ''"
        :title="copied ? t('keys.copied') : t('keys.copyToClipboard')"
        @click="$emit('copy')"
      >
        <Icon :name="copied ? 'check' : 'clipboard'" size="xs" />
        {{ copied ? t('keys.copied') : t('keys.copyToClipboard') }}
      </button>
    </div>

    <div class="zt-two">
      <!-- 用量与配额（表格「用量」列） -->
      <Panel :title="t('keys.usage')">
        <template #actions>
          <span class="zt-detail-sub">{{ t('keys.today') }} ${{ (stats?.today_actual_cost ?? 0).toFixed(4) }}</span>
        </template>
        <dl class="zt-dl">
          <dt>{{ t('keys.today') }}</dt>
          <dd>${{ (stats?.today_actual_cost ?? 0).toFixed(4) }}</dd>
          <dt>{{ t('keys.total') }}</dt>
          <dd>${{ (stats?.total_actual_cost ?? 0).toFixed(4) }}</dd>
          <dt>{{ t('keys.currentConcurrency') }}</dt>
          <dd>{{ apiKey.current_concurrency ?? 0 }}</dd>
        </dl>
        <div v-if="apiKey.quota > 0" class="zt-detail-quota">
          <div class="zt-quota-row">
            <em>{{ t('keys.quota') }}</em>
            <div class="zt-meter" :class="quotaClass">
              <i :style="{ width: Math.min((apiKey.quota_used / apiKey.quota) * 100, 100) + '%' }"></i>
            </div>
            <span :class="quotaTextClass">${{ apiKey.quota_used?.toFixed(2) || '0.00' }} / ${{ apiKey.quota?.toFixed(2) }}</span>
          </div>
        </div>
      </Panel>

      <!-- 限额窗口（表格「限额」列，含重置按钮） -->
      <Panel :title="t('keys.rateLimitColumn')">
        <template v-if="hasRateLimit && hasRateUsage" #actions>
          <button type="button" class="btn btn-ghost btn-sm" :title="t('keys.resetRateLimitUsage')" @click="$emit('reset-rate-limit')">
            <Icon name="refresh" size="xs" />
            {{ t('keys.resetUsage') }}
          </button>
        </template>
        <div v-if="hasRateLimit" class="zt-quota-rows">
          <template v-for="w in windows" :key="w.key">
            <div v-if="w.limit > 0" class="zt-quota-row">
              <em>{{ w.label }}</em>
              <div class="zt-meter" :class="windowClass(w.usage, w.limit)">
                <i :style="{ width: Math.min((w.usage / w.limit) * 100, 100) + '%' }"></i>
              </div>
              <span :class="windowTextClass(w.usage, w.limit)">
                ${{ w.usage?.toFixed(2) || '0.00' }}/${{ w.limit?.toFixed(2) }}
                <small v-if="w.resetAt && formatResetTime(w.resetAt)">⟳ {{ formatResetTime(w.resetAt) }}</small>
              </span>
            </div>
          </template>
        </div>
        <div v-else class="zt-detail-none">-</div>
      </Panel>
    </div>

    <!-- 设置（分组 / 过期 / 通知邮箱 / 轮换 / 最近使用 / IP） -->
    <Panel :title="t('keys.detailSettings')">
      <template #actions>
        <button type="button" class="btn btn-ghost btn-sm" @click="$emit('edit')">
          <Icon name="edit" size="xs" />
          {{ t('common.edit') }}
        </button>
      </template>
      <dl class="zt-dl zt-detail-settings">
        <dt>{{ t('keys.group') }}</dt>
        <dd>
          <button
            ref="groupButtonRef"
            type="button"
            class="zt-detail-group"
            :title="t('keys.clickToChangeGroup')"
            @click="$emit('open-group', groupButtonRef)"
          >
            <GroupBadge
              v-if="apiKey.group"
              :name="apiKey.group.name"
              :platform="apiKey.group.platform"
              :subscription-type="apiKey.group.subscription_type"
              :rate-multiplier="apiKey.group.rate_multiplier"
              :user-rate-multiplier="userRate"
              :peak-rate-enabled="apiKey.group.peak_rate_enabled"
              :peak-start="apiKey.group.peak_start"
              :peak-end="apiKey.group.peak_end"
              :peak-rate-multiplier="apiKey.group.peak_rate_multiplier"
            />
            <span v-else class="zt-ink-3">{{ t('keys.noGroup') }}</span>
            <span class="zt-detail-group-hint">{{ t('keys.selectGroup') }}</span>
            <Icon name="arrowsUpDown" size="xs" class="zt-ink-3" />
          </button>
        </dd>
        <dt>{{ t('keys.expiresAt') }}</dt>
        <dd>
          <span v-if="apiKey.expires_at" :class="new Date(apiKey.expires_at) < new Date() ? 'zt-bad-text' : ''">{{ formatDateTime(apiKey.expires_at) }}</span>
          <span v-else class="zt-ink-3">{{ t('keys.noExpiration') }}</span>
        </dd>
        <dt>{{ t('keys.notificationEmail') }}</dt>
        <dd>{{ apiKey.notification_email || '-' }}</dd>
        <dt>{{ t('keys.rotateOnExpiryColumn') }}</dt>
        <dd><span :class="['badge', apiKey.rotate_on_expiry ? 'badge-success' : 'badge-gray']">{{ apiKey.rotate_on_expiry ? t('common.enabled') : t('common.disabled') }}</span></dd>
        <dt>{{ t('keys.lastUsedAt') }}</dt>
        <dd>{{ apiKey.last_used_at ? formatDateTime(apiKey.last_used_at) : '-' }}</dd>
        <dt>{{ t('keys.lastUsedIP') }}</dt>
        <dd>{{ apiKey.last_used_ip || '-' }}</dd>
      </dl>
    </Panel>
  </div>
</template>

<script setup lang="ts">
/**
 * 密钥详情（分栏视图右侧）：字段与操作与表格列一一对应，事件全部交给 KeysView 现有函数处理。
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import { Panel } from '@/components/console'
import { formatDateTime } from '@/utils/format'
import { maskApiKey } from '@/utils/maskApiKey'
import type { ApiKey } from '@/types'
import type { BatchApiKeyUsageStats } from '@/api/usage'

const props = defineProps<{
  apiKey: ApiKey
  stats?: BatchApiKeyUsageStats
  userRate?: number
  hideCcsImport?: boolean
  copied?: boolean
  formatResetTime: (resetAt: string | null) => string
}>()

defineEmits<{
  use: []
  'import-ccs': []
  'toggle-status': []
  edit: []
  delete: []
  copy: []
  'open-group': [el: HTMLElement | null]
  'reset-rate-limit': []
}>()

const { t } = useI18n()
const groupButtonRef = ref<HTMLElement | null>(null)

const statusBadgeClass = computed(() =>
  props.apiKey.status === 'active' ? 'badge-success'
    : props.apiKey.status === 'quota_exhausted' ? 'badge-warning'
      : props.apiKey.status === 'expired' ? 'badge-danger'
        : 'badge-gray')

const quotaClass = computed(() => {
  const k = props.apiKey
  if (k.quota_used >= k.quota) return 'is-bad'
  if (k.quota_used >= k.quota * 0.8) return 'is-warn'
  return ''
})
const quotaTextClass = computed(() => (quotaClass.value === 'is-bad' ? 'zt-bad-text' : quotaClass.value === 'is-warn' ? 'zt-warn-text' : ''))

const windows = computed(() => [
  { key: '5h', label: '5h', usage: props.apiKey.usage_5h, limit: props.apiKey.rate_limit_5h, resetAt: props.apiKey.reset_5h_at },
  { key: '1d', label: '1d', usage: props.apiKey.usage_1d, limit: props.apiKey.rate_limit_1d, resetAt: props.apiKey.reset_1d_at },
  { key: '7d', label: '7d', usage: props.apiKey.usage_7d, limit: props.apiKey.rate_limit_7d, resetAt: props.apiKey.reset_7d_at },
])
const hasRateLimit = computed(() => windows.value.some((w) => w.limit > 0))
const hasRateUsage = computed(() => windows.value.some((w) => w.usage > 0))

function windowClass(usage: number, limit: number) {
  if (usage >= limit) return 'is-bad'
  if (usage >= limit * 0.8) return 'is-warn'
  return ''
}
function windowTextClass(usage: number, limit: number) {
  const c = windowClass(usage, limit)
  return c === 'is-bad' ? 'zt-bad-text' : c === 'is-warn' ? 'zt-warn-text' : ''
}
</script>

<style scoped>
.zt-detail-shield {
  color: var(--zt-accent);
}
.zt-detail-delete {
  color: var(--zt-bad);
}
.zt-detail-sub {
  font: 500 11.5px/1 var(--zt-mono);
  color: var(--zt-ink-3);
}
.zt-detail-quota {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--zt-border);
}
.zt-detail-quota .zt-quota-row,
.zt-quota-rows .zt-quota-row {
  grid-template-columns: 44px 1fr auto;
}
.zt-quota-rows .zt-quota-row span {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  min-width: 120px;
}
.zt-quota-rows .zt-quota-row small {
  font-size: 10px;
  color: var(--zt-ink-3);
}
.zt-detail-none {
  color: var(--zt-ink-3);
  font-size: 13px;
}
.zt-detail-settings {
  grid-template-columns: 120px minmax(0, 1fr);
}
.zt-detail-settings dd {
  font-family: inherit;
  min-width: 0;
  overflow-wrap: anywhere;
}
.zt-detail-group {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 2px 6px;
  margin: -2px -6px;
  border-radius: 8px;
  text-align: left;
}
.zt-detail-group:hover {
  background: var(--zt-surface-2);
}
.zt-detail-group-hint {
  font-size: 12px;
  color: var(--zt-ink-3);
}
</style>
