<template>
  <footer class="border-t border-gray-200 bg-gray-50 dark:border-dark-800 dark:bg-dark-950/60">
    <div class="mx-auto max-w-7xl px-4 py-10 sm:px-6 lg:px-8">
      <h2 class="text-base font-semibold text-gray-950 dark:text-white">
        {{ t('radar.sources.title', 'Data sources') }}
      </h2>

      <div v-if="sources.length > 0" class="mt-4 grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
        <article
          v-for="source in sources"
          :key="source.key"
          class="min-w-0 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-800 dark:bg-dark-900"
        >
          <div class="flex min-w-0 flex-col items-start gap-3 sm:flex-row sm:justify-between">
            <div class="min-w-0 flex-1">
              <a
                v-if="sourceLink(source.key)"
                :href="sourceLink(source.key) ?? undefined"
                target="_blank"
                rel="noopener noreferrer"
                class="flex min-w-0 max-w-full items-start gap-1 font-semibold text-primary-600 hover:underline dark:text-primary-400"
              >
                <span class="min-w-0 break-words [overflow-wrap:anywhere]">{{ sourceName(source) }}</span>
                <Icon name="externalLink" size="xs" class="shrink-0" aria-hidden="true" />
              </a>
              <p v-else class="min-w-0 break-words font-semibold text-gray-900 [overflow-wrap:anywhere] dark:text-white">
                {{ sourceName(source) }}
              </p>
              <p class="mt-1 min-w-0 break-words text-xs text-gray-500 [overflow-wrap:anywhere] dark:text-gray-400">
                {{ t('radar.sources.every', 'Every') }} {{ formatInterval(source.interval) }}
              </p>
            </div>
            <span
              class="inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-1 text-xs font-semibold"
              :class="sourceState(source).classes"
            >
              <Icon :name="sourceState(source).icon" size="xs" aria-hidden="true" />
              {{ sourceState(source).label }}
            </span>
          </div>

          <dl class="mt-4 space-y-2 text-xs">
            <div class="grid min-w-0 grid-cols-1 gap-1 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] sm:gap-3">
              <dt class="min-w-0 break-words text-gray-500 [overflow-wrap:anywhere] dark:text-gray-400">{{ t('radar.sources.lastAttempt', 'Last attempt') }}</dt>
              <dd class="min-w-0 break-words text-gray-700 [overflow-wrap:anywhere] dark:text-gray-200 sm:text-right">{{ formatDate(source.last_attempt_at) }}</dd>
            </div>
            <div class="grid min-w-0 grid-cols-1 gap-1 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] sm:gap-3">
              <dt class="min-w-0 break-words text-gray-500 [overflow-wrap:anywhere] dark:text-gray-400">{{ t('radar.sources.lastSuccess', 'Last success') }}</dt>
              <dd class="min-w-0 break-words text-gray-700 [overflow-wrap:anywhere] dark:text-gray-200 sm:text-right">{{ formatDate(source.last_success_at) }}</dd>
            </div>
            <div v-if="source.next_fire_at" class="grid min-w-0 grid-cols-1 gap-1 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] sm:gap-3">
              <dt class="min-w-0 break-words text-gray-500 [overflow-wrap:anywhere] dark:text-gray-400">{{ t('radar.sources.nextRun', 'Next scheduled run') }}</dt>
              <dd class="min-w-0 break-words text-gray-700 [overflow-wrap:anywhere] dark:text-gray-200 sm:text-right">{{ formatDate(source.next_fire_at) }}</dd>
            </div>
          </dl>

          <p v-if="source.error" class="mt-3 text-xs text-red-700 dark:text-red-300">
            {{ sourceError(source.error) }}
          </p>
          <p v-if="source.stale" class="mt-3 inline-flex items-center gap-1 text-xs font-medium text-amber-700 dark:text-amber-300">
            <Icon name="exclamationTriangle" size="xs" aria-hidden="true" />
            {{ t('radar.common.stale', 'Data may be outdated') }}
          </p>
        </article>
      </div>
      <p v-else-if="!loading" class="mt-4 text-sm text-gray-500 dark:text-gray-400">
        {{ t('radar.sources.empty', 'No source metadata available') }}
      </p>

      <p class="mt-8 max-w-5xl text-sm leading-6 text-gray-600 dark:text-gray-400">
        {{ t('radar.sources.disclaimer', 'Data is aggregated from anonymous on-site statistics and public third-party sources. Model benchmark results depend on evaluation methodology; API-equivalent values are statistical estimates, not official vendor quota limits or commitments, and should not be the sole basis for critical business decisions.') }}
      </p>
    </div>
  </footer>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { formatGoDuration } from '@/components/radar/radarFormatters'
import type { DataSourceErrorCode, DataSourceMetaDTO } from '@/types/radar'

withDefaults(defineProps<{
  sources?: readonly DataSourceMetaDTO[]
  loading?: boolean
}>(), {
  sources: () => [],
  loading: false,
})

const { t, locale } = useI18n()

function formatInterval(value: string): string {
  return formatGoDuration(value, locale.value)
}

function sourceLink(key: string): string | null {
  if (key === 'aa') return 'https://artificialanalysis.ai'
  if (key === 'lmarena') return 'https://arena.ai/leaderboard/text'
  if (key === 'status_claude') return 'https://status.claude.com'
  if (key === 'status_openai') return 'https://status.openai.com'
  return null
}

function sourceName(source: DataSourceMetaDTO): string {
  if (source.key === 'quota_aggregator') {
    return t('radar.sources.aggregatedUsage', 'Sub2API Aggregated Usage')
  }
  return source.name
}

function sourceState(source: DataSourceMetaDTO): {
  label: string
  icon: 'checkCircle' | 'exclamationCircle' | 'infoCircle' | 'questionCircle'
  classes: string
} {
  if (source.state === 'healthy' && source.is_healthy) {
    return { label: t('radar.sources.healthy', 'Healthy'), icon: 'checkCircle', classes: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300' }
  }
  switch (source.state) {
    case 'failed':
      return { label: t('radar.sources.failed', 'Failed'), icon: 'exclamationCircle', classes: 'bg-red-100 text-red-800 dark:bg-red-950/40 dark:text-red-300' }
    case 'not_configured':
      return { label: t('radar.sources.notConfigured', 'Not configured'), icon: 'infoCircle', classes: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-200' }
    case 'never_attempted':
      return { label: t('radar.sources.neverAttempted', 'Not attempted'), icon: 'questionCircle', classes: 'bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-gray-300' }
    default:
      return { label: t('radar.sources.stale', 'Stale'), icon: 'exclamationCircle', classes: 'bg-amber-100 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300' }
  }
}

function sourceError(error: DataSourceErrorCode): string {
  switch (error) {
    case 'network_error': return t('radar.sources.error.network', 'Network error')
    case 'unauthorized': return t('radar.sources.error.unauthorized', 'Source authorization failed')
    case 'rate_limited': return t('radar.sources.error.rateLimited', 'Source rate limited')
    case 'invalid_response': return t('radar.sources.error.invalidResponse', 'Invalid source response')
    case 'upstream_error': return t('radar.sources.error.upstream', 'Upstream source error')
    case 'aggregation_error': return t('radar.sources.error.aggregation', 'Usage aggregation failed')
    default: return t('radar.sources.error.generic', 'Source unavailable')
  }
}

function formatDate(value: string | null): string {
  if (!value) return t('radar.common.never', 'Not yet')
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return t('radar.common.unknownTime', 'Unknown')
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}
</script>
