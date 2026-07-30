import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { AdminGroupCatalogEntry, AdminUser, ApiKey } from '@/types'
import UserApiKeysModal from '../UserApiKeysModal.vue'
import GroupReplaceModal from '../GroupReplaceModal.vue'

const {
  getUserApiKeys,
  getGroupCatalog,
  updateApiKeyGroup,
  replaceGroup,
  showError,
  showSuccess
} = vi.hoisted(() => ({
  getUserApiKeys: vi.fn(),
  getGroupCatalog: vi.fn(),
  updateApiKeyGroup: vi.fn(),
  replaceGroup: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      getUserApiKeys,
      getGroupCatalog,
      replaceGroup
    },
    apiKeys: {
      updateApiKeyGroup
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    cachedPublicSettings: null
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key
    })
  }
})

const user = {
  id: 7,
  email: 'catalog-target@example.com',
  username: 'catalog-target',
  role: 'user',
  balance: 0,
  concurrency: 1,
  status: 'active',
  is_vip: false,
  vip_mode: 'AUTO',
  allowed_groups: [],
  balance_notify_enabled: false,
  balance_notify_threshold: null,
  balance_notify_extra_emails: [],
  created_at: '2026-07-29T00:00:00Z',
  updated_at: '2026-07-29T00:00:00Z',
  notes: ''
} satisfies AdminUser

const makeCatalogGroup = (
  overrides: Partial<AdminGroupCatalogEntry>
): AdminGroupCatalogEntry => ({
  id: 10,
  name: 'VIP exclusive',
  description: null,
  platform: 'anthropic',
  rate_multiplier: 1,
  is_exclusive: true,
  vip_only: true,
  can_bind: false,
  deny_reason: 'GROUP_VIP_ONLY',
  suggested_action: 'PAYMENT',
  will_grant_exclusive: true,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_live: false,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-29T00:00:00Z',
  updated_at: '2026-07-29T00:00:00Z',
  ...overrides
})

const apiKey: ApiKey = {
  id: 33,
  user_id: user.id,
  key: 'sk-test-admin-catalog-abcdefghijklmnopqrstuvwxyz',
  name: 'catalog key',
  group_id: null,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-07-29T00:00:00Z',
  updated_at: '2026-07-29T00:00:00Z',
  current_concurrency: 0,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null
}

const global = {
  stubs: {
    BaseDialog: {
      props: ['show'],
      template: '<div v-if="show"><slot /><slot name="footer" /></div>'
    },
    Teleport: {
      template: '<div><slot /></div>'
    },
    GroupBadge: true,
    Icon: true,
    GroupOptionItem: {
      props: [
        'name',
        'canBind',
        'denyReason',
        'suggestedAction',
        'allowPaymentAction'
      ],
      template: `
        <div
          data-test="catalog-option-content"
          :data-can-bind="String(canBind)"
          :data-allow-payment-action="String(allowPaymentAction)"
        >
          {{ name }} {{ denyReason }}
        </div>
      `
    }
  }
}

describe('administrator target-user group catalog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getUserApiKeys.mockResolvedValue({ items: [apiKey], total: 1 })
    getGroupCatalog.mockResolvedValue([
      makeCatalogGroup({ id: 10 }),
      makeCatalogGroup({
        id: 11,
        name: 'Allowed exclusive',
        vip_only: false,
        can_bind: true,
        deny_reason: null,
        suggested_action: null
      })
    ])
    updateApiKeyGroup.mockResolvedValue({
      api_key: { ...apiKey, group_id: 11 },
      auto_granted_group_access: true,
      granted_group_name: 'Allowed exclusive'
    })
    replaceGroup.mockResolvedValue({ migrated_keys: 2 })
  })

  it('uses the target-user catalog for admin key binding and disables denied options without a payment CTA', async () => {
    const wrapper = mount(UserApiKeysModal, {
      props: { show: false, user },
      global
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(getGroupCatalog).toHaveBeenCalledWith(user.id)
    await wrapper.get('[data-test="api-key-group-trigger-33"]').trigger('click')

    const denied = wrapper.get('[data-test="admin-group-option-10"]')
    expect(denied.attributes('disabled')).toBeDefined()
    expect(denied.text()).toContain('GROUP_VIP_ONLY')
    expect(denied.text()).toContain('admin.users.vip.willGrantExclusive')
    expect(denied.get('[data-test="catalog-option-content"]').attributes('data-allow-payment-action')).toBe('false')

    await wrapper.get('[data-test="admin-group-option-11"]').trigger('click')
    await flushPromises()
    expect(updateApiKeyGroup).toHaveBeenCalledWith(apiKey.id, 11)
  })

  it('uses the same catalog for GroupReplace and never submits a denied target', async () => {
    const wrapper = mount(GroupReplaceModal, {
      props: {
        show: false,
        user,
        oldGroup: { id: 4, name: 'Old exclusive' }
      },
      global
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(getGroupCatalog).toHaveBeenCalledWith(user.id)
    expect(wrapper.get('[data-test="replace-group-option-10"] input').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-test="replace-group-option-11"] input').setValue()
    const confirm = wrapper
      .findAll('button')
      .find((button) => button.text().includes('admin.users.replaceGroupConfirm'))
    expect(confirm).toBeTruthy()
    await confirm!.trigger('click')
    await flushPromises()

    expect(replaceGroup).toHaveBeenCalledWith(user.id, 4, 11)
  })
})
