<template>
  <div
    data-testid="service-history-legend"
    class="mb-4 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-gray-500 dark:text-gray-400"
  >
    <span class="font-medium text-gray-700 dark:text-gray-200">
      {{ t('radar.health.statusLegend', 'Status key') }}
    </span>
    <span
      v-for="legend in historyLegend"
      :key="legend.status"
      :data-history-legend-status="legend.status"
      class="inline-flex items-center gap-1.5"
    >
      <span class="h-2.5 w-2.5 rounded-sm" :class="legend.barClass" aria-hidden="true" />
      {{ legend.label }}
    </span>
  </div>

  <div class="grid gap-5 lg:grid-cols-2">
    <article
      v-for="item in normalizedServices"
      :key="item.service_key"
      :data-service-key="item.service_key"
      :data-platform="item.service_key"
      class="min-w-0 rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-800 dark:bg-dark-900 sm:p-6"
    >
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <h3 class="truncate text-base font-semibold text-gray-950 dark:text-white">{{ item.name }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('radar.health.history30d', '30-day history') }} · {{ historyRange(item.history_30d) }}
          </p>
        </div>
        <span
          class="inline-flex shrink-0 items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-semibold"
          :class="statusMeta(item.status).classes"
        >
          <Icon :name="statusMeta(item.status).icon" size="sm" aria-hidden="true" />
          {{ statusMeta(item.status).label }}
        </span>
      </div>

      <dl class="mt-4 grid gap-2 text-xs text-gray-500 dark:text-gray-400 sm:grid-cols-2">
        <div v-if="item.last_updated_at" data-testid="service-updated">
          <dt class="font-medium">{{ t('radar.health.updated', 'Updated') }}</dt>
          <dd class="mt-0.5 text-gray-700 dark:text-gray-200">
            <time :datetime="item.last_updated_at">{{ formatDate(item.last_updated_at) }}</time>
          </dd>
        </div>
        <div v-if="item.uptime_90d !== null" data-testid="service-uptime">
          <dt class="font-medium">{{ t('radar.health.uptime', '90-day uptime') }}</dt>
          <dd class="mt-0.5 font-semibold text-gray-900 dark:text-white">{{ formatPercent(item.uptime_90d) }}</dd>
        </div>
      </dl>

      <p
        v-if="item.stale"
        data-testid="service-stale"
        class="mt-3 inline-flex items-center gap-1.5 text-xs font-medium text-amber-700 dark:text-amber-300"
      >
        <Icon name="exclamationTriangle" size="xs" aria-hidden="true" />
        {{ t('radar.common.stale', 'Data may be outdated') }}
      </p>

      <div
        v-if="item.last_incident"
        data-testid="service-incident"
        class="mt-4 rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800"
      >
        <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
          <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
            {{ t('radar.health.lastIncident', 'Latest incident') }}
          </p>
          <time
            data-testid="service-incident-time"
            :datetime="item.last_incident.created_at"
            class="text-xs text-gray-500 dark:text-gray-400"
          >
            {{ formatDate(item.last_incident.created_at) }}
          </time>
        </div>
        <p class="mt-1 text-gray-800 dark:text-gray-100">{{ item.last_incident.name }}</p>
      </div>

      <div class="mt-5 flex items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400">
        <span>{{ t('radar.health.dailyStatus', 'Daily status') }}</span>
        <span>
          {{ formatNumber(historyIncidentCount(item.history_30d)) }}
          {{ t('radar.health.incidents30d', 'incidents') }}
        </span>
      </div>

      <div
        data-testid="service-history-region"
        class="relative mt-2"
        @mouseleave="closeHoverHistory(item.service_key)"
      >
        <div
          data-testid="service-history-strip"
          class="grid gap-1"
          style="grid-template-columns: repeat(30, minmax(0, 1fr))"
        >
          <button
            v-for="day in item.history_30d"
            :key="day.date"
            type="button"
            :data-history-date="day.date"
            :data-history-status="day.status"
            :aria-label="historyDayAriaLabel(day)"
            class="h-6 min-w-0 rounded-[3px] outline-none ring-offset-2 transition hover:-translate-y-0.5 hover:shadow-sm focus-visible:ring-2 focus-visible:ring-primary-500 dark:ring-offset-dark-900 sm:h-5"
            :class="historyStatusMeta(day.status).barClass"
            @mouseenter="openHistory(item.service_key, day, false)"
            @focus="openHistory(item.service_key, day, true)"
            @click="openHistory(item.service_key, day, true)"
            @keydown.esc="activeHistory = null"
          />
        </div>

        <div
          v-if="activeHistory?.serviceKey === item.service_key"
          data-testid="service-history-tooltip"
          role="tooltip"
          class="absolute right-0 top-full z-20 mt-2 w-[min(20rem,calc(100vw-3rem))] rounded-xl border border-gray-200 bg-white p-4 text-sm shadow-xl dark:border-dark-700 dark:bg-dark-800"
        >
          <div class="flex items-start justify-between gap-3">
            <div>
              <p class="font-semibold text-gray-950 dark:text-white">{{ formatHistoryDate(activeHistory.day.date) }}</p>
              <p class="mt-1 inline-flex items-center gap-1.5 text-xs font-medium" :class="historyStatusMeta(activeHistory.day.status).textClass">
                <Icon :name="historyStatusMeta(activeHistory.day.status).icon" size="xs" aria-hidden="true" />
                {{ historyStatusMeta(activeHistory.day.status).label }}
              </p>
            </div>
            <button
              type="button"
              :aria-label="t('radar.health.closeHistory', 'Close history details')"
              class="rounded-md p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-dark-700 dark:hover:text-gray-200"
              @click="activeHistory = null"
            >
              <Icon name="x" size="xs" aria-hidden="true" />
            </button>
          </div>
          <div v-if="activeHistory.day.incidents.length > 0" class="mt-3 space-y-3 border-t border-gray-100 pt-3 dark:border-dark-700">
            <div v-for="incident in activeHistory.day.incidents.slice(0, 3)" :key="`${incident.created_at}-${incident.name}`">
              <p class="font-medium leading-5 text-gray-900 dark:text-white">{{ incident.name }}</p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{{ formatIncidentWindow(incident) }}</p>
            </div>
            <p v-if="activeHistory.day.incidents.length > 3" class="text-xs text-gray-500 dark:text-gray-400">
              +{{ formatNumber(activeHistory.day.incidents.length - 3) }} {{ t('radar.health.moreIncidents', 'more incidents') }}
            </p>
          </div>
          <p v-else class="mt-3 border-t border-gray-100 pt-3 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
            {{ activeHistory.day.status === 'unknown'
              ? t('radar.health.historyUnavailable', 'Historical coverage is unavailable for this day.')
              : t('radar.health.noIncidents', 'No incidents reported for this day.') }}
          </p>
        </div>
      </div>
    </article>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type {
  RadarIncidentDTO,
  ServiceHealthDTO,
  ServiceHealthHistoryDayDTO,
  ServiceKey,
  ServiceStatus,
} from '@/types/radar'
import { platformLabel } from '@/utils/platformColors'

type PlatformHealthCard = Omit<ServiceHealthDTO, 'service_key'> & { service_key: string }
type HistoryIcon = 'checkCircle' | 'exclamationTriangle' | 'xCircle' | 'infoCircle' | 'questionCircle'

const props = withDefaults(defineProps<{
  services?: readonly ServiceHealthDTO[] | null
  platforms?: readonly string[] | null
}>(), {
  services: () => [],
  platforms: () => [],
})

const { t, locale } = useI18n()
const activeHistory = ref<{
  serviceKey: string
  day: ServiceHealthHistoryDayDTO
  pinned: boolean
} | null>(null)
const statusSeverity: Readonly<Record<ServiceStatus, number>> = {
  operational: 0,
  under_maintenance: 1,
  degraded_performance: 2,
  partial_outage: 3,
  major_outage: 4,
  unknown: 5,
}
const historySeverity: Readonly<Record<ServiceStatus, number>> = {
  operational: 0,
  unknown: 1,
  under_maintenance: 2,
  degraded_performance: 3,
  partial_outage: 4,
  major_outage: 5,
}
const supportedPlatforms = new Set(['anthropic', 'openai', 'windsurf', 'deepseek', 'kimi', 'minimax'])

function platformServiceKeys(platform: string): ServiceKey[] {
  if (platform === 'anthropic') return ['claude_api', 'claude_code']
  if (platform === 'openai') return ['openai_api', 'codex_web']
  if (platform === 'windsurf') return ['windsurf']
  if (platform === 'deepseek') return ['deepseek']
  if (platform === 'kimi') return ['kimi']
  if (platform === 'minimax') return ['minimax']
  return []
}

function emptyHistory(anchor = new Date()): ServiceHealthHistoryDayDTO[] {
  const end = new Date(Date.UTC(anchor.getUTCFullYear(), anchor.getUTCMonth(), anchor.getUTCDate()))
  return Array.from({ length: 30 }, (_, index) => {
    const date = new Date(end)
    date.setUTCDate(end.getUTCDate() - (29 - index))
    return { date: date.toISOString().slice(0, 10), status: 'unknown', incidents: [] }
  })
}

function unknownPlatformCard(platform: string): PlatformHealthCard {
  return {
    service_key: platform,
    name: platformLabel(platform),
    status: 'unknown',
    status_indicator: 'unknown',
    uptime_90d: null,
    last_incident: null,
    last_updated_at: null,
    history_30d: emptyHistory(),
    source_url: '',
    stale: false,
  }
}

function mergeHistory(candidates: readonly ServiceHealthDTO[]): ServiceHealthHistoryDayDTO[] {
  const latestDate = candidates
    .flatMap((candidate) => candidate.history_30d ?? [])
    .map((day) => day.date)
    .filter((date) => parseHistoryDate(date) !== null)
    .sort()
    .at(-1)
  const history = emptyHistory(latestDate ? parseHistoryDate(latestDate)! : new Date())
  const byDate = new Map(history.map((day) => [day.date, day]))
  const coveredDates = new Set<string>()
  for (const candidate of candidates) {
    for (const day of candidate.history_30d ?? []) {
      const current = byDate.get(day.date)
      if (!current) continue
      if (!coveredDates.has(day.date)) {
        current.status = day.status
        coveredDates.add(day.date)
      } else if (historySeverity[day.status] > historySeverity[current.status]) {
        current.status = day.status
      }
      const seen = new Set(current.incidents.map((incident) => `${incident.created_at}\u0000${incident.name}`))
      for (const incident of day.incidents) {
        const key = `${incident.created_at}\u0000${incident.name}`
        if (!seen.has(key)) {
          current.incidents.push({ ...incident })
          seen.add(key)
        }
      }
      current.incidents.sort((left, right) => right.created_at.localeCompare(left.created_at))
    }
  }
  return history
}

function latestIncidentFromRecentHistory(
  history: readonly ServiceHealthHistoryDayDTO[],
  days = 7
): RadarIncidentDTO | null {
  const uniqueIncidents = new Map<string, RadarIncidentDTO>()
  const recentDays = [...history]
    .filter((day) => parseHistoryDate(day.date) !== null)
    .sort((left, right) => right.date.localeCompare(left.date))
    .slice(0, days)

  for (const day of recentDays) {
    for (const incident of day.incidents) {
      if (!Number.isFinite(new Date(incident.created_at).getTime())) continue
      const key = `${incident.created_at}\u0000${incident.name}`
      if (!uniqueIncidents.has(key)) uniqueIncidents.set(key, incident)
    }
  }

  return [...uniqueIncidents.values()]
    .sort((left, right) => new Date(right.created_at).getTime() - new Date(left.created_at).getTime())[0]
    ?? null
}

const normalizedServices = computed(() => {
  const byKey = new Map((props.services ?? []).map((item) => [item.service_key, item]))
  const seen = new Set<string>()
  const platforms = (props.platforms ?? [])
    .map((platform) => platform.trim().toLowerCase())
    .filter((platform) => {
      if (!supportedPlatforms.has(platform) || seen.has(platform)) return false
      seen.add(platform)
      return true
    })

  return platforms.map((platform): PlatformHealthCard => {
    const candidates = platformServiceKeys(platform)
      .map((key) => byKey.get(key))
      .filter((item): item is ServiceHealthDTO => item !== undefined)
    if (candidates.length === 0) return unknownPlatformCard(platform)

    const worst = [...candidates].sort((left, right) => statusSeverity[right.status] - statusSeverity[left.status])[0]
    const latestUpdated = candidates
      .map((item) => item.last_updated_at)
      .filter((value): value is string => value !== null)
      .sort()
      .at(-1) ?? null
    const history = mergeHistory(candidates)
    return {
      ...worst,
      service_key: platform,
      name: platformLabel(platform),
      last_incident: latestIncidentFromRecentHistory(history),
      last_updated_at: latestUpdated,
      source_url: worst.source_url || candidates.find((item) => item.source_url)?.source_url || '',
      history_30d: history,
      stale: candidates.some((item) => item.stale),
    }
  })
})

function statusMeta(status: ServiceStatus): { label: string; icon: HistoryIcon; classes: string } {
  switch (status) {
    case 'operational':
      return { label: t('radar.health.status.operational', 'Operational'), icon: 'checkCircle', classes: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300' }
    case 'degraded_performance':
      return { label: t('radar.health.status.degraded', 'Degraded performance'), icon: 'exclamationTriangle', classes: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-950/40 dark:text-yellow-300' }
    case 'partial_outage':
      return { label: t('radar.health.status.partialOutage', 'Partial outage'), icon: 'exclamationTriangle', classes: 'bg-orange-100 text-orange-800 dark:bg-orange-950/40 dark:text-orange-300' }
    case 'major_outage':
      return { label: t('radar.health.status.majorOutage', 'Major outage'), icon: 'xCircle', classes: 'bg-red-100 text-red-800 dark:bg-red-950/40 dark:text-red-300' }
    case 'under_maintenance':
      return { label: t('radar.health.status.maintenance', 'Under maintenance'), icon: 'infoCircle', classes: 'bg-sky-100 text-sky-800 dark:bg-sky-950/40 dark:text-sky-300' }
    default:
      return { label: t('radar.health.status.unknown', 'Status unknown'), icon: 'questionCircle', classes: 'bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-gray-300' }
  }
}

function historyStatusMeta(status: ServiceStatus): { label: string; icon: HistoryIcon; barClass: string; textClass: string } {
  const current = statusMeta(status)
  const colors: Record<ServiceStatus, { barClass: string; textClass: string }> = {
    operational: { barClass: 'bg-emerald-500', textClass: 'text-emerald-700 dark:text-emerald-300' },
    under_maintenance: { barClass: 'bg-sky-500', textClass: 'text-sky-700 dark:text-sky-300' },
    degraded_performance: { barClass: 'bg-amber-400', textClass: 'text-amber-700 dark:text-amber-300' },
    partial_outage: { barClass: 'bg-orange-500', textClass: 'text-orange-700 dark:text-orange-300' },
    major_outage: { barClass: 'bg-rose-500', textClass: 'text-rose-700 dark:text-rose-300' },
    unknown: { barClass: 'bg-gray-300 dark:bg-dark-600', textClass: 'text-gray-600 dark:text-gray-300' },
  }
  return { label: current.label, icon: current.icon, ...colors[status] }
}

const historyLegend = computed(() => (['operational', 'degraded_performance', 'partial_outage', 'major_outage', 'unknown'] as const)
  .map((status) => ({ status, ...historyStatusMeta(status) })))

function openHistory(serviceKey: string, day: ServiceHealthHistoryDayDTO, pinned: boolean): void {
  activeHistory.value = { serviceKey, day, pinned }
}

function closeHoverHistory(serviceKey: string): void {
  if (
    activeHistory.value?.serviceKey === serviceKey
    && !activeHistory.value.pinned
    && !document.activeElement?.hasAttribute('data-history-date')
  ) {
    activeHistory.value = null
  }
}

function historyIncidentCount(history: readonly ServiceHealthHistoryDayDTO[]): number {
  const incidents = new Set<string>()
  for (const day of history) {
    for (const incident of day.incidents) incidents.add(`${incident.created_at}\u0000${incident.name}`)
  }
  return incidents.size
}

function parseHistoryDate(value: string): Date | null {
  const date = new Date(`${value}T00:00:00.000Z`)
  return Number.isFinite(date.getTime()) ? date : null
}

function formatHistoryDate(value: string): string {
  const date = parseHistoryDate(value)
  return date ? new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeZone: 'UTC' }).format(date) : value
}

function historyRange(history: readonly ServiceHealthHistoryDayDTO[]): string {
  if (history.length === 0) return '—'
  return `${formatHistoryDate(history[0].date)} – ${formatHistoryDate(history.at(-1)!.date)}`
}

function historyDayAriaLabel(day: ServiceHealthHistoryDayDTO): string {
  return `${formatHistoryDate(day.date)}: ${historyStatusMeta(day.status).label}; ${formatNumber(day.incidents.length)} ${t('radar.health.incidents', 'incidents')}`
}

function formatIncidentWindow(incident: RadarIncidentDTO): string {
  const start = new Date(incident.created_at)
  const end = incident.resolved_at ? new Date(incident.resolved_at) : null
  const formatter = new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
    timeZone: 'UTC',
  })
  if (!Number.isFinite(start.getTime())) return '—'
  if (!end || !Number.isFinite(end.getTime())) return formatter.format(start)
  return `${formatter.format(start)} – ${formatter.format(end)}`
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat(locale.value).format(value)
}

function formatDate(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return t('radar.common.unknownTime', 'Unknown time')
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function formatPercent(value: number): string {
  return `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }).format(value)}%`
}
</script>
