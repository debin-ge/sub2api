<template>
  <div class="space-y-6">
    <!-- Row 1: Core Stats（指标集合与改版前一致） -->
    <KpiRow :cols="isSimple ? 3 : 4">
      <KpiItem
        v-if="!isSimple"
        :label="t('dashboard.balance')"
        :value="`$${formatBalanceAmount(balance)}`"
        value-color="var(--zt-good)"
        :sub="t('common.available')"
      />
      <KpiItem :label="t('dashboard.apiKeys')" :value="stats?.total_api_keys || 0">
        <template #sub><b>{{ stats?.active_api_keys || 0 }}</b> {{ t('common.active') }}</template>
      </KpiItem>
      <KpiItem :label="t('dashboard.todayRequests')" :value="stats?.today_requests || 0">
        <template #sub>{{ t('common.total') }}: {{ formatNumber(stats?.total_requests || 0) }}</template>
      </KpiItem>
      <KpiItem :label="t('dashboard.todayCost')">
        <template #value>
          <span class="zt-good-text" :title="t('dashboard.actual')">${{ formatCost(stats?.today_actual_cost || 0) }}</span>
          <small :title="t('dashboard.standard')">/ ${{ formatCost(stats?.today_cost || 0) }}</small>
        </template>
        <template #sub>
          <span>{{ t('common.total') }}:</span>
          <span class="zt-good-text" :title="t('dashboard.actual')">${{ formatCost(stats?.total_actual_cost || 0) }}</span>
          <span :title="t('dashboard.standard')">/ ${{ formatCost(stats?.total_cost || 0) }}</span>
        </template>
      </KpiItem>
    </KpiRow>

    <!-- Row 2: Token Stats -->
    <KpiRow>
      <KpiItem :label="t('dashboard.todayTokens')" :value="formatTokens(stats?.today_tokens || 0)">
        <template #sub>{{ t('dashboard.input') }}: {{ formatTokens(stats?.today_input_tokens || 0) }} / {{ t('dashboard.output') }}: {{ formatTokens(stats?.today_output_tokens || 0) }} / {{ t('dashboard.cache') }}: {{ formatTokens((stats?.today_cache_creation_tokens || 0) + (stats?.today_cache_read_tokens || 0)) }}</template>
      </KpiItem>
      <KpiItem :label="t('dashboard.totalTokens')" :value="formatTokens(stats?.total_tokens || 0)">
        <template #sub>{{ t('dashboard.input') }}: {{ formatTokens(stats?.total_input_tokens || 0) }} / {{ t('dashboard.output') }}: {{ formatTokens(stats?.total_output_tokens || 0) }} / {{ t('dashboard.cache') }}: {{ formatTokens((stats?.total_cache_creation_tokens || 0) + (stats?.total_cache_read_tokens || 0)) }}</template>
      </KpiItem>
      <KpiItem :label="t('dashboard.performance')">
        <template #value>
          {{ formatTokens(stats?.rpm || 0) }}<small>RPM</small>
          <span class="zt-kpi-second">{{ formatTokens(stats?.tpm || 0) }}</span><small>TPM</small>
        </template>
      </KpiItem>
      <KpiItem :label="t('dashboard.avgResponse')" :value="formatDuration(stats?.average_duration_ms || 0)" :sub="t('dashboard.averageTime')" />
    </KpiRow>

    <!-- Row 3: Per-platform breakdown（含配额条，数据与规则与改版前一致） -->
    <SectionBlock
      v-if="!isSimple && platformCards.length > 0"
      :title="t('dashboard.platformBreakdown')"
      :subtitle="t('dashboard.platformCount', { count: platformCount })"
    >
      <div class="zt-list">
        <div
          v-for="item in platformCards"
          :key="item.platform"
          data-testid="platform-card"
          :data-platform="item.platform"
          class="zt-list-row zt-plat-row"
          :class="{ 'is-other': item.isOther }"
        >
          <span class="zt-plat-logo" :style="{ background: platformGradient(item.platform) }" aria-hidden="true">
            {{ platformInitial(item) }}
          </span>
          <div class="min-w-0">
            <b>{{ item.isOther ? t('dashboard.platformOther') : platformLabel(item.platform) }}</b>
            <small>
              {{ t('dashboard.todayCost') }} ${{ formatCost(item.today_actual_cost) }}
              · {{ t('dashboard.requests') }} {{ item.total_requests > 0 ? formatNumber(item.total_requests) : '-' }}
              · {{ t('dashboard.tokens') }} {{ item.total_tokens > 0 ? formatTokens(item.total_tokens) : '-' }}
            </small>

            <!-- Quota 区：仅当 quota 配置存在、非 __other__ 且至少有一个窗口配了 limit 时显示 -->
            <div v-if="hasAnyLimit(item.quota) && !item.isOther" class="zt-plat-quota">
              <p class="zt-plat-quota-title">{{ t('dashboard.platformQuota.title') }}</p>
              <template v-for="w in (['daily', 'weekly', 'monthly'] as const)" :key="w">
                <div v-if="quotaVal(item.quota, `${w}_limit_usd`) != null" class="zt-quota-row">
                  <em>{{ t(`dashboard.platformQuota.${w}`) }}</em>
                  <!-- limit=0：完全禁用 -->
                  <template v-if="(quotaVal(item.quota, `${w}_limit_usd`) as number) === 0">
                    <div class="zt-meter is-thin is-bad"><i style="width: 100%"></i></div>
                    <span><span class="zt-quota-disabled">{{ t('dashboard.platformQuota.disabled') }}</span></span>
                  </template>
                  <!-- limit>0：正常用量进度条 -->
                  <template v-else>
                    <div
                      class="zt-meter is-thin"
                      :class="quotaMeterClass(calcPercent((quotaVal(item.quota, `${w}_usage_usd`) as number) ?? 0, quotaVal(item.quota, `${w}_limit_usd`) as number))"
                    >
                      <i :style="{ width: calcPercent((quotaVal(item.quota, `${w}_usage_usd`) as number) ?? 0, quotaVal(item.quota, `${w}_limit_usd`) as number) + '%' }" />
                    </div>
                    <span>
                      <span>${{ formatUsd((quotaVal(item.quota, `${w}_usage_usd`) as number) ?? 0) }} / ${{ formatUsd(quotaVal(item.quota, `${w}_limit_usd`) as number) }}</span>
                      <small v-if="quotaVal(item.quota, `${w}_window_resets_at`)">
                        {{ t('dashboard.platformQuota.resetsAt', { time: formatResetTime(quotaVal(item.quota, `${w}_window_resets_at`) as string) }) }}
                      </small>
                    </span>
                  </template>
                </div>
              </template>
            </div>
          </div>
          <div class="zt-plat-total" :title="t('dashboard.actual')">${{ formatCost(item.total_actual_cost) }}</div>
        </div>
      </div>
    </SectionBlock>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { KpiItem, KpiRow, SectionBlock } from '@/components/console'
import type { PlatformDashboardStats, UserDashboardStats as UserStatsType } from '@/api/usage'
import { formatBalanceAmount } from '@/utils/formatters'
import type { PlatformQuotaItem } from '@/types'

interface FusedPlatformCard {
  platform: string
  total_actual_cost: number
  today_actual_cost: number
  total_requests: number
  total_tokens: number
  isOther?: boolean
  quota?: PlatformQuotaItem
}

const props = defineProps<{
  stats: UserStatsType
  balance: number
  isSimple: boolean
  platformQuotas?: PlatformQuotaItem[] | null
}>()
const { t } = useI18n()

const PLATFORM_LABELS: Record<string, string> = {
  anthropic: 'Claude',
  openai: 'OpenAI',
  gemini: 'Gemini',
  antigravity: 'Antigravity',
  grok: 'Grok',
  kimi: 'Kimi',
  zhipu: 'Zhipu GLM',
  deepseek: 'DeepSeek',
  minimax: 'MiniMax',
}

const platformLabel = (p: string) => PLATFORM_LABELS[p] ?? p

// 处理"各平台之和 < 总值"的差值：后端按平台聚合时过滤了无法归属平台的行
// （group 与 account 都缺 platform）。这里把差值作为"其他"卡片显式展示，
// 避免 Row 1 总值与 Row 3 平台拆分加总对不上、用户困惑。
const OTHER_THRESHOLD = 0.0001
const platformCards = computed<FusedPlatformCard[]>(() => {
  // 建立 by_platform Map
  const byPlat = new Map<string, PlatformDashboardStats>()
  for (const item of props.stats?.by_platform ?? []) byPlat.set(item.platform, item)

  // 建立 quota Map。三档全空的记录不产生卡片，挂到卡片上也不渲染配额区。
  const byQuota = new Map<string, PlatformQuotaItem>()
  for (const q of props.platformQuotas ?? []) byQuota.set(q.platform, q)

  // 卡片集合 = 有用量的平台 ∪ 至少配置了一档限额的平台。
  // 三档全空的限额记录等价于不限额，不单独产生卡片。
  // 后端 by_platform / quota 接口均不会返回 platform='__other__'，
  // 无需显式排除；__other__ 由下方差值补差逻辑单独追加。
  const platforms = new Set<string>(byPlat.keys())
  for (const [platform, q] of byQuota) {
    if (hasAnyLimit(q)) platforms.add(platform)
  }

  const PLATFORM_ORDER = ['anthropic', 'openai', 'gemini', 'antigravity', 'grok']
  const cards: FusedPlatformCard[] = []

  for (const p of platforms) {
    const stat = byPlat.get(p)
    cards.push({
      platform: p,
      total_actual_cost: stat?.total_actual_cost ?? 0,
      today_actual_cost: stat?.today_actual_cost ?? 0,
      total_requests: stat?.total_requests ?? 0,
      total_tokens: stat?.total_tokens ?? 0,
      quota: byQuota.get(p),
    })
  }

  // 排序：按 PLATFORM_ORDER，未知平台按名称排序
  cards.sort((a, b) => {
    const ai = PLATFORM_ORDER.indexOf(a.platform)
    const bi = PLATFORM_ORDER.indexOf(b.platform)
    if (ai === -1 && bi === -1) return a.platform.localeCompare(b.platform)
    if (ai === -1) return 1
    if (bi === -1) return -1
    return ai - bi
  })

  // __other__ 补差逻辑：只对 by_platform 有 usage 数据的总和计算
  const total = props.stats?.total_actual_cost ?? 0
  const today = props.stats?.today_actual_cost ?? 0
  const sumTotal = cards.reduce((s, c) => s + c.total_actual_cost, 0)
  const sumToday = cards.reduce((s, c) => s + c.today_actual_cost, 0)
  const diffTotal = Math.max(0, total - sumTotal)
  const diffToday = Math.max(0, today - sumToday)

  if (diffTotal > OTHER_THRESHOLD || diffToday > OTHER_THRESHOLD) {
    cards.push({
      platform: '__other__',
      total_actual_cost: diffTotal,
      today_actual_cost: diffToday,
      total_requests: 0,
      total_tokens: 0,
      isOther: true,
    })
  }

  return cards
})

// 标题右侧的平台计数 = 实际渲染的平台卡片数，不含"其他"差额卡。
const platformCount = computed(() => platformCards.value.filter((c) => !c.isOther).length)

// Quota helpers

type QuotaWindow = 'daily' | 'weekly' | 'monthly'
type QuotaField = `${QuotaWindow}_limit_usd` | `${QuotaWindow}_usage_usd` | `${QuotaWindow}_window_resets_at`

function quotaVal(q: PlatformQuotaItem | undefined, key: QuotaField): PlatformQuotaItem[QuotaField] {
  return q?.[key]
}

function hasAnyLimit(q: PlatformQuotaItem | undefined): boolean {
  if (!q) return false
  return q.daily_limit_usd != null || q.weekly_limit_usd != null || q.monthly_limit_usd != null
}

function calcPercent(usage: number, limit: number): number {
  if (!limit || limit <= 0) return 0
  return Math.min(100, Math.max(0, Math.round((usage / limit) * 100)))
}

function quotaMeterClass(p: number): string {
  if (p >= 95) return 'is-bad'
  if (p >= 75) return 'is-warn'
  return ''
}

const PLATFORM_GRADIENTS: Record<string, string> = {
  anthropic: 'linear-gradient(135deg, #f97316, #ea580c)',
  openai: 'linear-gradient(135deg, #10b981, #059669)',
  gemini: 'linear-gradient(135deg, #3b82f6, #2563eb)',
  antigravity: 'linear-gradient(135deg, #f43f5e, #db2777)',
  grok: 'linear-gradient(135deg, #475569, #0f172a)',
  kimi: 'linear-gradient(135deg, #f59e0b, #d97706)',
  zhipu: 'linear-gradient(135deg, #6366f1, #7c3aed)',
  deepseek: 'linear-gradient(135deg, #6366f1, #4338ca)',
  minimax: 'linear-gradient(135deg, #06b6d4, #0284c7)',
}
const platformGradient = (p: string) => PLATFORM_GRADIENTS[p] ?? 'linear-gradient(135deg, #94a3b8, #64748b)'
const platformInitial = (item: FusedPlatformCard) => (item.isOther ? '…' : platformLabel(item.platform).charAt(0).toUpperCase())

// 使用 Intl.NumberFormat 避免 toFixed 在不同 JS 引擎下偶发截断而非四舍五入。
const usdFormatter = new Intl.NumberFormat('en-US', {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})
function formatUsd(n: number): string {
  if (!Number.isFinite(n)) return '0.00'
  return usdFormatter.format(n)
}

function formatResetTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString(undefined, {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

const formatNumber = (n: number) => n.toLocaleString()
const formatCost = (c: number) => c.toFixed(4)
const formatTokens = (t: number) => {
  if (t >= 1_000_000) return `${(t / 1_000_000).toFixed(1)}M`
  if (t >= 1000) return `${(t / 1000).toFixed(1)}K`
  return t.toString()
}
const formatDuration = (ms: number) => ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${ms.toFixed(0)}ms`
</script>
