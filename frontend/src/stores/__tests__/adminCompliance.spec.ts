import { describe, expect, it, beforeEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { i18n } from '@/i18n'
import { useAdminComplianceStore } from '@/stores/adminCompliance'

describe('useAdminComplianceStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    i18n.global.locale.value = 'zh'
  })

  it('uses the compliance status site name for fallback acknowledgement copy', () => {
    const store = useAdminComplianceStore()

    store.requireAcknowledgement({
      version: 'v2026.06.10',
      site_name: 'Acme Gateway',
    })

    expect(store.siteName).toBe('Acme Gateway')
    expect(store.expectedPhrase).toBe('我已阅读、理解并同意 Acme Gateway 部署与运营合规承诺')
  })
})
