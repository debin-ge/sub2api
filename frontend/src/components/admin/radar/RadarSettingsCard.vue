<template>
  <section class="card" aria-labelledby="radar-settings-title">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 id="radar-settings-title" class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('admin.settings.features.radar.title') }}
          </h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.settings.features.radar.description') }}
          </p>
        </div>
        <button
          data-testid="radar-refresh"
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="loading || refreshPending || !status"
          :aria-busy="refreshPending"
          @click="refreshNow"
        >
          {{
            refreshPending
              ? t('admin.settings.features.radar.refresh.pending')
              : t('admin.settings.features.radar.refresh.action')
          }}
        </button>
      </div>
    </div>

    <div class="space-y-5 p-6">
      <div v-if="loading" role="status" class="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
        <span
          class="h-4 w-4 animate-spin rounded-full border-2 border-gray-300 border-t-primary-600"
          aria-hidden="true"
        ></span>
        {{ t('admin.settings.features.radar.loading') }}
      </div>

      <div
        v-else-if="loadError"
        role="alert"
        class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/30 dark:text-red-300"
      >
        <p>{{ t('admin.settings.features.radar.loadError') }}</p>
        <button
          data-testid="radar-retry"
          type="button"
          class="btn btn-secondary btn-sm mt-3"
          @click="loadStatus"
        >
          {{ t('admin.settings.features.radar.retry') }}
        </button>
      </div>

      <template v-else-if="status">
        <div class="flex flex-wrap items-center justify-between gap-4">
          <div>
            <p class="text-sm font-medium text-gray-900 dark:text-white">
              {{ t('admin.settings.features.radar.enabled') }}
            </p>
            <p id="radar-enabled-hint" class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.features.radar.enabledHint') }}
            </p>
          </div>
          <Toggle
            data-testid="radar-enabled-toggle"
            :model-value="status.enabled"
            :disabled="settingsPending"
            :aria-label="t('admin.settings.features.radar.enabled')"
            aria-describedby="radar-enabled-hint"
            @update:model-value="setEnabled"
          />
        </div>

        <p
          v-if="settingsError"
          data-testid="radar-settings-error"
          role="alert"
          class="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
        >
          {{ t('admin.settings.features.radar.updateError') }}
        </p>

        <div
          v-if="refreshError"
          role="alert"
          class="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
        >
          {{ t('admin.settings.features.radar.refresh.error') }}
        </div>
        <div
          v-else-if="refreshResult"
          data-testid="radar-refresh-result"
          aria-live="polite"
          class="rounded-md border border-primary-200 bg-primary-50 px-3 py-2 text-sm text-primary-800 dark:border-primary-800 dark:bg-primary-950/30 dark:text-primary-200"
        >
          <p class="font-medium">
            {{ t(`admin.settings.features.radar.refresh.${refreshResult.status}`) }}
          </p>
          <p class="mt-1 break-all text-xs opacity-80">
            {{ t('admin.settings.features.radar.refresh.id', { id: refreshResult.refresh_id }) }}
          </p>
          <p v-if="refreshResult.tasks.length" class="mt-1 text-xs opacity-80">
            {{
              t('admin.settings.features.radar.refresh.tasks', {
                tasks: refreshResult.tasks.map(sourceLabel).join(', '),
              })
            }}
          </p>
        </div>

        <div>
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.settings.features.radar.sourcesTitle') }}
          </h3>
          <div class="mt-3 grid gap-3 xl:grid-cols-2">
            <article
              v-for="source in allSources"
              :key="source.key"
              :data-testid="`radar-source-${source.key}`"
              class="rounded-lg border border-gray-200 p-4 dark:border-dark-700"
            >
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
                  {{ sourceLabel(source.key) }}
                </h4>
                <div class="flex flex-wrap items-center gap-1.5">
                  <span
                    class="rounded-full px-2 py-0.5 text-xs font-medium"
                    :class="statusClass(source.status)"
                  >
                    {{ t(`admin.settings.features.radar.status.${source.status}`) }}
                  </span>
                  <span
                    v-if="isStale(source)"
                    class="rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-800 dark:bg-amber-950/50 dark:text-amber-300"
                  >
                    {{ t('admin.settings.features.radar.status.stale') }}
                  </span>
                </div>
              </div>

              <dl class="mt-3 grid gap-x-4 gap-y-2 text-xs sm:grid-cols-2">
                <div>
                  <dt class="text-gray-500 dark:text-gray-400">
                    {{ t('admin.settings.features.radar.fields.lastSuccess') }}
                  </dt>
                  <dd class="mt-0.5 text-gray-800 dark:text-gray-200">
                    <time
                      v-if="source.last_success_at"
                      data-field="last-success"
                      :datetime="source.last_success_at"
                    >{{ formatDate(source.last_success_at) }}</time>
                    <span v-else>{{ t('admin.settings.features.radar.never') }}</span>
                  </dd>
                </div>
                <div>
                  <dt class="text-gray-500 dark:text-gray-400">
                    {{ t('admin.settings.features.radar.fields.lastFailure') }}
                  </dt>
                  <dd class="mt-0.5 text-gray-800 dark:text-gray-200">
                    <time
                      v-if="source.last_failure_at"
                      data-field="last-failure"
                      :datetime="source.last_failure_at"
                    >{{ formatDate(source.last_failure_at) }}</time>
                    <span v-else>{{ t('admin.settings.features.radar.never') }}</span>
                  </dd>
                </div>
                <div>
                  <dt class="text-gray-500 dark:text-gray-400">
                    {{ t('admin.settings.features.radar.fields.nextFire') }}
                  </dt>
                  <dd class="mt-0.5 text-gray-800 dark:text-gray-200">
                    <time
                      v-if="source.next_fire_at"
                      data-field="next-fire"
                      :datetime="source.next_fire_at"
                    >{{ formatDate(source.next_fire_at) }}</time>
                    <span v-else>{{ t('admin.settings.features.radar.unavailable') }}</span>
                  </dd>
                </div>
                <div>
                  <dt class="text-gray-500 dark:text-gray-400">
                    {{ t('admin.settings.features.radar.fields.error') }}
                  </dt>
                  <dd class="mt-0.5 text-gray-800 dark:text-gray-200">
                    <span v-if="source.error">
                      {{ t(`admin.settings.features.radar.errors.${source.error}`) }}
                      <span v-if="source.http_status"> (HTTP {{ source.http_status }})</span>
                    </span>
                    <span v-else>{{ t('admin.settings.features.radar.none') }}</span>
                  </dd>
                </div>
              </dl>
            </article>
          </div>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import {
  getRadarAdminStatus,
  triggerRadarAdminRefresh,
  updateRadarAdminSettings,
  type RadarAdminRefreshResult,
  type RadarAdminSourceStatus,
  type RadarAdminState,
  type RadarAdminStatus,
} from '@/api/admin/radar'

const { t, locale } = useI18n()
const requestController = new AbortController()
let disposed = false

const loading = ref(true)
const loadError = ref(false)
const status = ref<RadarAdminStatus | null>(null)
const settingsPending = ref(false)
const settingsError = ref(false)
const refreshPending = ref(false)
const refreshError = ref(false)
const refreshResult = ref<RadarAdminRefreshResult | null>(null)

const sourceLabelKeys: Readonly<Record<string, string>> = {
  aa: 'admin.settings.features.radar.sources.aa',
  lmarena: 'admin.settings.features.radar.sources.lmarena',
  status_claude: 'admin.settings.features.radar.sources.status_claude',
  status_openai: 'admin.settings.features.radar.sources.status_openai',
  quota_aggregator: 'admin.settings.features.radar.sources.quota_aggregator',
}
const aaPerformanceSourcePrefix = 'aa_perf:'

const allSources = computed(() => {
  if (!status.value) return []
  return [...status.value.sources, status.value.aggregator]
})

function sourceLabel(key: string): string {
  if (key.startsWith(aaPerformanceSourcePrefix)) {
    const slug = key.slice(aaPerformanceSourcePrefix.length)
    if (slug) {
      return `${t('admin.settings.features.radar.sources.aa_performance')} · ${slug}`
    }
  }
  const labelKey = sourceLabelKeys[key]
  return labelKey ? t(labelKey) : key
}

function statusClass(value: RadarAdminState): string {
  if (value === 'healthy') {
    return 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/50 dark:text-emerald-300'
  }
  if (value === 'failed') {
    return 'bg-red-100 text-red-800 dark:bg-red-950/50 dark:text-red-300'
  }
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
}

function isStale(source: RadarAdminSourceStatus): boolean {
  return source.stale
}

function formatDate(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return t('admin.settings.features.radar.unavailable')
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

async function loadStatus(): Promise<void> {
  if (disposed) return
  loading.value = true
  loadError.value = false
  try {
    const result = await getRadarAdminStatus({ signal: requestController.signal })
    if (disposed) return
    status.value = result
  } catch {
    if (disposed) return
    loadError.value = true
  } finally {
    if (!disposed) loading.value = false
  }
}

async function setEnabled(enabled: boolean): Promise<void> {
  if (!status.value || settingsPending.value || disposed) return
  const previous = status.value.enabled
  settingsError.value = false
  settingsPending.value = true
  status.value = { ...status.value, enabled }
  try {
    const result = await updateRadarAdminSettings(enabled, { signal: requestController.signal })
    if (disposed || !status.value) return
    status.value = { ...status.value, enabled: result.enabled }
  } catch {
    if (disposed || !status.value) return
    status.value = { ...status.value, enabled: previous }
    settingsError.value = true
  } finally {
    if (!disposed) settingsPending.value = false
  }
}

async function refreshNow(): Promise<void> {
  if (!status.value || refreshPending.value || disposed) return
  refreshPending.value = true
  refreshError.value = false
  refreshResult.value = null
  try {
    const result = await triggerRadarAdminRefresh({ signal: requestController.signal })
    if (disposed) return
    refreshResult.value = result
  } catch {
    if (disposed) return
    refreshError.value = true
  } finally {
    if (!disposed) refreshPending.value = false
  }
}

onMounted(loadStatus)
onUnmounted(() => {
  disposed = true
  requestController.abort()
})
</script>
