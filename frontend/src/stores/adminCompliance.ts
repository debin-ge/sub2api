import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import adminComplianceAPI, { type AdminComplianceStatus } from '@/api/admin/compliance'
import { getLocale } from '@/i18n'
import { useAppStore } from './app'

const DEFAULT_SITE_NAME = 'Sub2API'

function normalizeComplianceSiteName(siteName: string): string {
  return siteName.trim() || DEFAULT_SITE_NAME
}

function zhAckPhrase(siteName: string): string {
  return `我已阅读、理解并同意 ${normalizeComplianceSiteName(siteName)} 部署与运营合规承诺`
}

function enAckPhrase(siteName: string): string {
  return `I have read, understood, and agree to the ${normalizeComplianceSiteName(siteName)} Deployment and Operation Compliance Commitment`
}

export const useAdminComplianceStore = defineStore('adminCompliance', () => {
  const appStore = useAppStore()
  const status = ref<AdminComplianceStatus | null>(null)
  const loading = ref(false)
  const submitting = ref(false)
  const initialized = ref(false)
  const forceVisible = ref(false)

  const required = computed(() => status.value?.required === true)
  const shouldShow = computed(() => required.value || forceVisible.value)
  const currentLocale = computed(() => getLocale())
  const siteName = computed(() =>
    status.value?.site_name || appStore.cachedPublicSettings?.site_name || appStore.siteName || DEFAULT_SITE_NAME
  )
  const expectedPhrase = computed(() => {
    if (currentLocale.value === 'zh') {
      return status.value?.ack_phrase_zh || zhAckPhrase(siteName.value)
    }
    return status.value?.ack_phrase_en || enAckPhrase(siteName.value)
  })

  async function fetchStatus(): Promise<AdminComplianceStatus> {
    loading.value = true
    try {
      const nextStatus = await adminComplianceAPI.getStatus()
      status.value = nextStatus
      initialized.value = true
      forceVisible.value = nextStatus.required
      return nextStatus
    } finally {
      loading.value = false
    }
  }

  async function accept(phrase: string): Promise<AdminComplianceStatus> {
    submitting.value = true
    try {
      const nextStatus = await adminComplianceAPI.accept({
        phrase,
        language: currentLocale.value
      })
      status.value = nextStatus
      forceVisible.value = nextStatus.required
      return nextStatus
    } finally {
      submitting.value = false
    }
  }

  function requireAcknowledgement(partialStatus?: Partial<AdminComplianceStatus>): void {
    const nextSiteName = partialStatus?.site_name || status.value?.site_name || siteName.value
    status.value = {
      required: true,
      version: partialStatus?.version || status.value?.version || 'v2026.06.10',
      site_name: nextSiteName,
      document_path_zh: partialStatus?.document_path_zh || status.value?.document_path_zh || 'docs/legal/admin-compliance.zh.md',
      document_path_en: partialStatus?.document_path_en || status.value?.document_path_en || 'docs/legal/admin-compliance.en.md',
      document_url_zh: partialStatus?.document_url_zh || status.value?.document_url_zh || 'https://github.com/Wei-Shaw/sub2api/blob/main/docs/legal/admin-compliance.zh.md',
      document_url_en: partialStatus?.document_url_en || status.value?.document_url_en || 'https://github.com/Wei-Shaw/sub2api/blob/main/docs/legal/admin-compliance.en.md',
      ack_phrase_zh: partialStatus?.ack_phrase_zh || status.value?.ack_phrase_zh || zhAckPhrase(nextSiteName),
      ack_phrase_en: partialStatus?.ack_phrase_en || status.value?.ack_phrase_en || enAckPhrase(nextSiteName),
      acknowledgement: status.value?.acknowledgement
    }
    initialized.value = true
    forceVisible.value = true
  }

  function reset(): void {
    status.value = null
    loading.value = false
    submitting.value = false
    initialized.value = false
    forceVisible.value = false
  }

  return {
    status,
    loading,
    submitting,
    initialized,
    required,
    shouldShow,
    siteName,
    expectedPhrase,
    fetchStatus,
    accept,
    requireAcknowledgement,
    reset
  }
})
