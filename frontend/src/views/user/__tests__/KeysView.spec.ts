import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import type { ApiKey } from '@/types'
import KeysView from '../KeysView.vue'

const {
  listKeys,
  getPublicSettings,
  getDashboardApiKeysUsage,
  getAvailableGroups,
  getUserGroupRates,
  showError,
  showSuccess,
  copyToClipboard,
  isCurrentStep,
  nextStep,
  createKey,
  updateKey,
  sendNotificationEmailCode,
  verifyNotificationEmail,
} = vi.hoisted(() => ({
  listKeys: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
  createKey: vi.fn(),
  updateKey: vi.fn(),
  sendNotificationEmailCode: vi.fn(),
  verifyNotificationEmail: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.actions': 'Actions',
  'common.name': 'Name',
  'common.refresh': 'Refresh',
  'common.status': 'Status',
  'keys.apiKey': 'API Key',
  'keys.allGroups': 'All Groups',
  'keys.allStatus': 'All Status',
  'keys.columnSettings': 'Column Settings',
  'keys.createKey': 'Create API Key',
  'keys.created': 'Created',
  'keys.expiresAt': 'Expires',
  'keys.group': 'Group',
  'keys.id': 'ID',
  'keys.currentConcurrency': 'Current Concurrency',
  'keys.lastUsedAt': 'Last Used',
  'keys.lastUsedIP': 'Last Used IP',
  'keys.rateLimitColumn': 'Rate Limit',
  'keys.notificationEmail': 'Notification Email',
  'keys.rotateOnExpiryColumn': 'Expiry Rotation',
  'keys.sendVerificationCode': 'Send Code',
  'keys.verifyEmail': 'Verify Email',
  'keys.notificationEmailVerified': 'Email verified',
  'keys.changeNotification': 'Change Notifications',
  'keys.rotateOnExpiry': 'Rotate After Expiry',
  'keys.searchPlaceholder': 'Search name or key...',
  'keys.status.active': 'Active',
  'keys.status.expired': 'Expired',
  'keys.status.inactive': 'Inactive',
  'keys.status.quota_exhausted': 'Quota exhausted',
  'keys.usage': 'Usage',
  'vip.group.denied.GROUP_VIP_ONLY': 'VIP group unavailable',
  'vip.group.errors.GROUP_VIP_ONLY': 'VIP group unavailable',
}

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    create: createKey,
    update: updateKey,
    delete: vi.fn(),
    toggleStatus: vi.fn(),
    sendNotificationEmailCode,
    verifyNotificationEmail,
  },
  authAPI: {
    getPublicSettings,
  },
  usageAPI: {
    getDashboardApiKeysUsage,
  },
  userGroupsAPI: {
    getAvailable: getAvailableGroups,
    getUserGroupRates,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const createApiKey = (): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test-key',
  name: 'test-key',
  group_id: null,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  notification_email: null,
  notification_email_verified: false,
  change_notify_enabled: false,
  rotate_on_expiry: false,
  last_rotated_at: null,
  created_at: '2026-06-27T00:00:00Z',
  updated_at: '2026-06-27T00:00:00Z',
  current_concurrency: 3,
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
  reset_7d_at: null,
})

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="actions" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  name: 'DataTable',
  props: ['columns', 'data'],
  emits: ['sort'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((col) => col.key).join(',') }}</div>
      <div data-test="columns-meta">{{ JSON.stringify(columns.map((col) => ({ key: col.key, sortable: !!col.sortable }))) }}</div>
      <button data-test="sort-current-concurrency" @click="$emit('sort', 'current_concurrency', 'asc')">
        Sort Current Concurrency
      </button>
      <div v-for="row in data" :key="row.id">
        <div
          v-if="columns.some((col) => col.key === 'id')"
          data-test="key-id"
        >
          <slot name="cell-id" :value="row.id" :row="row" />
        </div>
        <slot name="cell-name" :value="row.name" :row="row" />
        <div data-test="group-cell">
          <slot name="cell-group" :value="row.group" :row="row" />
        </div>
        <div data-test="current-concurrency">
          <slot name="cell-current_concurrency" :value="row.current_concurrency" :row="row" />
        </div>
        <div
          v-if="columns.some((col) => col.key === 'last_used_ip')"
          data-test="last-used-ip"
        >
          <slot name="cell-last_used_ip" :value="row.last_used_ip" :row="row" />
        </div>
        <div data-test="actions-cell">
          <slot name="cell-actions" :row="row" />
        </div>
      </div>
      <slot name="empty" />
    </div>
  `,
}

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"></select>',
}

const SearchInputStub = {
  name: 'SearchInput',
  props: ['modelValue'],
  emits: ['update:modelValue', 'search'],
  template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
}

const PaginationStub = {
  name: 'Pagination',
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: `
    <div>
      <button data-test="page-size-50" @click="$emit('update:pageSize', 50)">50</button>
    </div>
  `,
}

const IconStub = {
  props: ['name'],
  template: '<span data-test="icon">{{ name }}</span>',
}

const BaseDialogStub = {
  name: 'BaseDialog',
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
}

const mountView = async () => {
  const wrapper = mount(KeysView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: PaginationStub,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        SearchInput: SearchInputStub,
        Icon: IconStub,
        UseKeyModal: true,
        EndpointPopover: true,
        GroupBadge: true,
        GroupOptionItem: true,
        Teleport: true,
      },
    },
  })
  await flushPromises()
  await nextTick()
  return wrapper
}

const visibleColumnKeys = (wrapper: VueWrapper) =>
  wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const visibleColumnMeta = (wrapper: VueWrapper): Array<{ key: string; sortable: boolean }> =>
  JSON.parse(wrapper.get('[data-test="columns-meta"]').text())

const getButtonByText = (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

describe('user KeysView column settings', () => {
  beforeEach(() => {
    localStorage.clear()

    listKeys.mockReset()
    getPublicSettings.mockReset()
    getDashboardApiKeysUsage.mockReset()
    getAvailableGroups.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()
    createKey.mockReset()
    updateKey.mockReset()
    sendNotificationEmailCode.mockReset()
    verifyNotificationEmail.mockReset()

    listKeys.mockResolvedValue({
      items: [createApiKey()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getPublicSettings.mockResolvedValue({})
    getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    getAvailableGroups.mockResolvedValue([])
    getUserGroupRates.mockResolvedValue({})
    isCurrentStep.mockReturnValue(false)
  })

  it('uses the default API key columns with low-frequency columns hidden', async () => {
    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'group',
      'current_concurrency',
      'usage',
      'expires_at',
      'status',
      'created_at',
      'actions',
    ])
    expect(visibleColumnKeys(wrapper)).not.toContain('rate_limit')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_at')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_ip')
    expect(visibleColumnKeys(wrapper)).not.toContain('id')
  })

  it('shows a hidden column when toggled and persists the preference', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Rate Limit').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('rate_limit')
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['id', 'notification_email', 'rotate_on_expiry', 'last_used_at', 'last_used_ip'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('4')
  })

  it('shows the API key ID column when toggled', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'ID').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('id')
    expect(wrapper.get('[data-test="key-id"]').text()).toBe('#1')
    expect(visibleColumnMeta(wrapper).find((column) => column.key === 'id')?.sortable).toBe(true)
  })

  it('shows the last used IP column when toggled', async () => {
    listKeys.mockResolvedValueOnce({
      items: [{ ...createApiKey(), last_used_ip: '203.0.113.10' }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Last Used IP').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('last_used_ip')
    expect(wrapper.get('[data-test="last-used-ip"]').text()).toBe('203.0.113.10')
  })

  it('restores column preferences from localStorage on mount', async () => {
    localStorage.setItem('api-key-hidden-columns', JSON.stringify(['group', 'created_at']))
    localStorage.setItem('api-key-column-settings-version', '1')

    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'current_concurrency',
      'usage',
      'rate_limit',
      'expires_at',
      'status',
      'last_used_at',
      'actions',
    ])
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['group', 'created_at', 'last_used_ip', 'id', 'notification_email', 'rotate_on_expiry'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('4')
  })

  it('does not include always-visible columns in the toggleable menu', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await nextTick()

    const columnMenuText = wrapper.text()
    expect(columnMenuText).toContain('API Key')
    expect(columnMenuText).toContain('ID')
    expect(columnMenuText).toContain('Current Concurrency')
    expect(columnMenuText).toContain('Rate Limit')
    expect(columnMenuText).toContain('Last Used IP')
    expect(columnMenuText).not.toContain('Name')
    expect(columnMenuText).not.toContain('Actions')
  })

  it('renders the current concurrency value', async () => {
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="current-concurrency"]').text()).toBe('3')
  })

  it('marks current concurrency as sortable', async () => {
    const wrapper = await mountView()

    const currentConcurrencyColumn = visibleColumnMeta(wrapper).find(
      (column) => column.key === 'current_concurrency'
    )
    expect(currentConcurrencyColumn?.sortable).toBe(true)
  })

  it('keeps filters and selected page size when sorting by current concurrency', async () => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'OpenAI' }])
    const wrapper = await mountView()

    await wrapper.get('[data-test="page-size-50"]').trigger('click')
    await flushPromises()

    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('update:modelValue', 'target')
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()

    const selects = wrapper.findAllComponents({ name: 'Select' })
    await selects[0].vm.$emit('update:modelValue', 42)
    await flushPromises()
    await selects[1].vm.$emit('update:modelValue', 'active')
    await flushPromises()

    listKeys.mockClear()

    await wrapper.get('[data-test="sort-current-concurrency"]').trigger('click')
    await flushPromises()

    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      50,
      {
        search: 'target',
        status: 'active',
        group_id: 42,
        sort_by: 'current_concurrency',
        sort_order: 'asc',
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('does not call the update API from the quick selector for a denied catalog entry', async () => {
    getAvailableGroups.mockResolvedValueOnce([{
      id: 7,
      name: 'VIP only',
      description: null,
      platform: 'openai',
      rate_multiplier: 1,
      peak_rate_enabled: false,
      peak_start: '',
      peak_end: '',
      peak_rate_multiplier: 1,
      subscription_type: 'standard',
      vip_only: true,
      can_bind: false,
      deny_reason: 'GROUP_VIP_ONLY',
      suggested_action: 'PAYMENT',
    }])
    const wrapper = await mountView()

    await wrapper.get('button[title="keys.clickToChangeGroup"]').trigger('click')
    await nextTick()
    const deniedOption = wrapper.get('[role="option"][aria-disabled="true"]')
    await deniedOption.trigger('click')

    expect(updateKey).not.toHaveBeenCalled()
  })

  it('blocks a denied group again when the create form is submitted', async () => {
    getAvailableGroups.mockResolvedValueOnce([{
      id: 7,
      name: 'VIP only',
      description: null,
      platform: 'openai',
      rate_multiplier: 1,
      peak_rate_enabled: false,
      peak_start: '',
      peak_end: '',
      peak_rate_multiplier: 1,
      subscription_type: 'standard',
      vip_only: true,
      can_bind: false,
      deny_reason: 'GROUP_VIP_ONLY',
      suggested_action: 'PAYMENT',
    }])
    const wrapper = await mountView()

    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await nextTick()
    await wrapper.get('input[data-tour="key-form-name"]').setValue('blocked-key')
    const matchingSelects = wrapper.findAllComponents({ name: 'Select' }).filter(
      (select) => select.props('options')?.some((option: { value?: unknown }) => option.value === 7),
    )
    const groupSelect = matchingSelects[matchingSelects.length - 1]
    await groupSelect!.vm.$emit('update:modelValue', 7)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(createKey).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('VIP group unavailable')
  })

  it('blocks a denied group again when the edit form is submitted', async () => {
    getAvailableGroups.mockResolvedValueOnce([{
      id: 7,
      name: 'VIP only',
      description: null,
      platform: 'openai',
      rate_multiplier: 1,
      peak_rate_enabled: false,
      peak_start: '',
      peak_end: '',
      peak_rate_multiplier: 1,
      subscription_type: 'standard',
      vip_only: true,
      can_bind: false,
      deny_reason: 'GROUP_VIP_ONLY',
      suggested_action: 'PAYMENT',
    }])
    const wrapper = await mountView()

    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await nextTick()
    const matchingSelects = wrapper.findAllComponents({ name: 'Select' }).filter(
      (select) => select.props('options')?.some((option: { value?: unknown }) => option.value === 7),
    )
    const groupSelect = matchingSelects[matchingSelects.length - 1]
    await groupSelect!.vm.$emit('update:modelValue', 7)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(updateKey).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('VIP group unavailable')
  })

  it('allows editing other fields when the original group is no longer bindable', async () => {
    listKeys.mockResolvedValueOnce({
      items: [{ ...createApiKey(), group_id: 7 }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getAvailableGroups.mockResolvedValueOnce([{
      id: 7,
      name: 'VIP only',
      description: null,
      platform: 'openai',
      rate_multiplier: 1,
      peak_rate_enabled: false,
      peak_start: '',
      peak_end: '',
      peak_rate_multiplier: 1,
      subscription_type: 'standard',
      vip_only: true,
      can_bind: false,
      deny_reason: 'GROUP_VIP_ONLY',
      suggested_action: 'PAYMENT',
    }])
    updateKey.mockResolvedValueOnce({})
    const wrapper = await mountView()

    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await nextTick()
    await wrapper.get('input[data-tour="key-form-name"]').setValue('renamed-key')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(showError).not.toHaveBeenCalledWith('VIP group unavailable')
    expect(updateKey).toHaveBeenCalledTimes(1)
    expect(updateKey.mock.calls[0]![1]).toMatchObject({ name: 'renamed-key' })
    expect(updateKey.mock.calls[0]![1]).not.toHaveProperty('group_id')
  })

  it('verifies a changed email and submits notification and rotation settings', async () => {
    const expiresAt = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString()
    listKeys.mockResolvedValueOnce({
      items: [{ ...createApiKey(), group_id: 42, expires_at: expiresAt }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getAvailableGroups.mockResolvedValueOnce([{
      id: 42,
      name: 'OpenAI',
      description: null,
      platform: 'openai',
      rate_multiplier: 1,
      peak_rate_enabled: false,
      peak_start: '',
      peak_end: '',
      peak_rate_multiplier: 1,
      subscription_type: 'standard',
      vip_only: false,
      can_bind: true,
      deny_reason: null,
      suggested_action: null,
    }])
    sendNotificationEmailCode.mockResolvedValueOnce({ message: 'ok' })
    verifyNotificationEmail.mockResolvedValueOnce({
      verification_token: 'verified-token',
      email: 'owner@example.com',
      expires_at: new Date(Date.now() + 15 * 60 * 1000).toISOString(),
    })
    updateKey.mockResolvedValueOnce({})
    const wrapper = await mountView()

    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await nextTick()
    await wrapper.get('input[type="email"]').setValue('OWNER@example.com')
    await getButtonByText(wrapper, 'Send Code').trigger('click')
    await flushPromises()
    expect(sendNotificationEmailCode).toHaveBeenCalledWith('owner@example.com')

    await wrapper.get('input[maxlength="6"]').setValue('123456')
    await getButtonByText(wrapper, 'Verify Email').trigger('click')
    await flushPromises()
    const toggles = wrapper.findAll('button[aria-pressed]')
    expect(toggles).toHaveLength(2)
    await toggles[0]!.trigger('click')
    await toggles[1]!.trigger('click')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({
      notification_email: 'owner@example.com',
      notification_email_verification_token: 'verified-token',
      change_notify_enabled: true,
      rotate_on_expiry: true,
    }))
  })

  it('keeps the original quick binding when the backend rejects a raced catalog decision', async () => {
    getAvailableGroups.mockResolvedValueOnce([{
      id: 8,
      name: 'Initially allowed',
      description: null,
      platform: 'openai',
      rate_multiplier: 1,
      peak_rate_enabled: false,
      peak_start: '',
      peak_end: '',
      peak_rate_multiplier: 1,
      subscription_type: 'standard',
      vip_only: true,
      can_bind: true,
      deny_reason: null,
      suggested_action: null,
    }])
    updateKey.mockRejectedValueOnce({ status: 403, reason: 'GROUP_VIP_ONLY' })
    const wrapper = await mountView()

    await wrapper.get('button[title="keys.clickToChangeGroup"]').trigger('click')
    await nextTick()
    await wrapper.get('[role="option"][aria-disabled="false"]').trigger('click')
    await flushPromises()

    expect(updateKey).toHaveBeenCalledWith(1, { group_id: 8 })
    expect(listKeys).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-test="group-cell"]').text()).toContain('keys.noGroup')
    expect(showError).toHaveBeenCalledWith('VIP group unavailable')
  })
})
