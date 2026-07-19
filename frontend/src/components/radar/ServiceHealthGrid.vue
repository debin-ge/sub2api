<template>
  <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
    <article
      v-for="item in normalizedServices"
      :key="item.service_key"
      :data-service-key="item.service_key"
      :data-platform="item.service_key"
      class="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-800 dark:bg-dark-900"
    >
      <div class="flex items-start justify-between gap-3">
        <h3 class="font-semibold text-gray-950 dark:text-white">{{ item.name }}</h3>
        <span
          class="inline-flex shrink-0 items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-semibold"
          :class="statusMeta(item.status).classes"
        >
          <Icon :name="statusMeta(item.status).icon" size="sm" aria-hidden="true" />
          {{ statusMeta(item.status).label }}
        </span>
      </div>

      <dl class="mt-5 space-y-3 text-sm">
        <div v-if="item.uptime_90d !== null" data-testid="service-uptime" class="flex justify-between gap-4">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('radar.health.uptime', '90-day uptime') }}</dt>
          <dd class="font-medium text-gray-900 dark:text-white">{{ formatPercent(item.uptime_90d) }}</dd>
        </div>
      </dl>

      <div
        v-if="item.last_incident"
        data-testid="service-incident"
        class="mt-4 rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800"
      >
        <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
          {{ t('radar.health.lastIncident', 'Latest incident') }}
        </p>
        <p class="mt-1 text-gray-800 dark:text-gray-100">{{ item.last_incident.name }}</p>
      </div>
    </article>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { ServiceHealthDTO, ServiceKey, ServiceStatus } from '@/types/radar'
import { platformLabel } from '@/utils/platformColors'

type PlatformHealthCard = Omit<ServiceHealthDTO, 'service_key'> & { service_key: string }

const props = withDefaults(defineProps<{
  services?: readonly ServiceHealthDTO[] | null
  platforms?: readonly string[] | null
}>(), {
  services: () => [],
  platforms: () => [],
})

const { t, locale } = useI18n()
const statusSeverity: Readonly<Record<ServiceStatus, number>> = {
  operational: 0,
  under_maintenance: 1,
  degraded_performance: 2,
  partial_outage: 3,
  major_outage: 4,
  unknown: 5,
}

function platformServiceKeys(platform: string): ServiceKey[] {
  if (platform === 'anthropic') return ['claude_api', 'claude_code']
  if (platform === 'openai') return ['openai_api', 'codex_web']
  return []
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
    source_url: '',
    stale: false,
  }
}

const normalizedServices = computed(() => {
  const byKey = new Map((props.services ?? []).map((item) => [item.service_key, item]))
  const seen = new Set<string>()
  const platforms = (props.platforms ?? [])
    .map((platform) => platform.trim().toLowerCase())
    .filter((platform) => {
      if (platform === '' || seen.has(platform)) return false
      seen.add(platform)
      return true
    })

  return platforms.map((platform): PlatformHealthCard => {
    const candidates = platformServiceKeys(platform)
      .map((key) => byKey.get(key))
      .filter((item): item is ServiceHealthDTO => item !== undefined)
    if (candidates.length === 0) return unknownPlatformCard(platform)

    const worst = [...candidates].sort((left, right) => (
      statusSeverity[right.status] - statusSeverity[left.status]
    ))[0]
    const latestUpdated = candidates
      .map((item) => item.last_updated_at)
      .filter((value): value is string => value !== null)
      .sort()
      .at(-1) ?? null
    return {
      ...worst,
      service_key: platform,
      name: platformLabel(platform),
      last_updated_at: latestUpdated,
      stale: candidates.some((item) => item.stale),
    }
  })
})

function statusMeta(status: ServiceStatus): {
  label: string
  icon: 'checkCircle' | 'exclamationTriangle' | 'xCircle' | 'infoCircle' | 'questionCircle'
  classes: string
} {
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
      return { label: t('radar.health.status.maintenance', 'Under maintenance'), icon: 'infoCircle', classes: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-200' }
    default:
      return { label: t('radar.health.status.unknown', 'Status unknown'), icon: 'questionCircle', classes: 'bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-gray-300' }
  }
}

function formatPercent(value: number): string {
  return `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }).format(value)}%`
}
</script>
