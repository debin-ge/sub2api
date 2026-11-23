<template>
  <div class="card" data-test="build-version-card">
    <div class="border-b zt-border-c px-6 py-4">
      <h2 class="text-lg font-semibold zt-ink">
        {{ t('admin.settings.buildInfo.title') }}
      </h2>
      <p class="mt-1 text-sm zt-ink-3">
        {{ t('admin.settings.buildInfo.description') }}
      </p>
    </div>
    <div class="p-6">
      <p v-if="error" role="alert" class="text-sm text-red-600">
        {{ t('admin.settings.buildInfo.loadFailed') }}
      </p>
      <dl v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div v-for="item in items" :key="item.key" :data-test="`build-${item.key}`">
          <dt class="text-xs zt-ink-3">{{ item.label }}</dt>
          <dd class="mt-1 break-all font-mono text-sm font-medium zt-ink">
            {{ loading ? '…' : item.value || '—' }}
          </dd>
        </div>
      </dl>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getBuildInfo, type SystemBuildInfo } from '@/api/admin/system'
import { formatDateTimeToMinute } from '@/utils/format'

const { t } = useI18n()

const info = ref<SystemBuildInfo | null>(null)
const loading = ref(true)
const error = ref(false)

function withV(version: string | undefined): string {
  const v = (version || '').trim()
  if (!v) return ''
  return /^\d/.test(v) ? `v${v}` : v
}

function formatBuildDate(raw: string | undefined): string {
  const value = (raw || '').trim()
  if (!value || value === 'unknown') return ''
  return Number.isNaN(Date.parse(value)) ? value : formatDateTimeToMinute(value)
}

function shortCommit(raw: string | undefined): string {
  const value = (raw || '').trim()
  if (!value || value === 'unknown') return ''
  return /^[0-9a-f]{12,}$/i.test(value) ? value.slice(0, 9) : value
}

const items = computed(() => [
  {
    key: 'version',
    label: t('admin.settings.buildInfo.projectVersion'),
    value: withV(info.value?.version)
  },
  {
    key: 'upstream',
    label: t('admin.settings.buildInfo.upstreamVersion'),
    value: withV(info.value?.upstream_version)
  },
  {
    key: 'commit',
    label: t('admin.settings.buildInfo.commit'),
    value: shortCommit(info.value?.commit)
  },
  {
    key: 'date',
    label: t('admin.settings.buildInfo.buildDate'),
    value: formatBuildDate(info.value?.build_date)
  }
])

onMounted(async () => {
  try {
    info.value = await getBuildInfo()
  } catch {
    error.value = true
  } finally {
    loading.value = false
  }
})
</script>
