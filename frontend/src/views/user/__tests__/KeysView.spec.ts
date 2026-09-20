import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import type { ApiKey } from '@/types'
import { keysAPI } from '@/api'
import KeysView from '../KeysView.vue'

const {
  listKeys,
  updateKey,
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
  sendNotificationEmailCode,
  verifyNotificationEmail,
} = vi.hoisted(() => ({
  listKeys: vi.fn(),
  updateKey: vi.fn(),
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
  'keys.notificationEmailVerificationOptional': 'Email verification is optional',
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
  props: { columns: Array, data: Array, selectedKeys: Array, selectable: Boolean },
  emits: ['sort', 'update:selectedKeys'],
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

const mountView = async () => {
  const wrapper = mount(KeysView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: PaginationStub,
        BaseDialog: {
          props: ['show', 'title'],
          emits: ['close'],
          template: '<div v-if="show" role="dialog"><button data-test="close-dialog" @click="$emit(\'close\')">Close</button><slot /><slot name="footer" /></div>',
        },
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        SearchInput: SearchInputStub,
        Icon: IconStub,
        UseKeyModal: true,
        BulkEditKeysModal: true,
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
    updateKey.mockReset()
    vi.mocked(keysAPI.create).mockReset()
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

  it.each([
    { initialStatus: 'quota_exhausted', status: 'active', formStatus: 'active' },
    { initialStatus: 'inactive', status: 'inactive', formStatus: 'inactive' },
    { initialStatus: 'active', status: 'active', formStatus: 'inactive' },
  ] as const)('syncs quota reset from $initialStatus to $status with form status $formStatus', async ({ initialStatus, status, formStatus }) => {
    const key: ApiKey = {
      ...createApiKey(), group_id: 1, quota: 10, quota_used: 10,
      status: initialStatus,
    }
    listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
    updateKey.mockResolvedValue({ ...key, status, quota_used: 0 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('[data-tour="key-form-name"]').setValue('Unsaved name')
    const statusSelect = wrapper.findAllComponents({ name: 'Select' })
      .find((select) => select.props('options').length === 2 &&
        select.props('options')[0].value === 'active')!
    statusSelect.vm.$emit('update:modelValue', 'inactive')
    await wrapper.get('button[title="keys.resetQuotaUsed"]').trigger('click')
    const confirmation = wrapper.findAllComponents({ name: 'ConfirmDialog' })
      .find((dialog) => dialog.props('title') === 'keys.resetQuotaTitle')!
    confirmation.vm.$emit('confirm')
    await flushPromises()

    expect(updateKey).toHaveBeenNthCalledWith(1, key.id, { reset_quota: true })
    expect(wrapper.findComponent({ name: 'DataTable' }).props('data')[0])
      .toMatchObject({ status, quota_used: 0 })
    expect(statusSelect.props('modelValue')).toBe(formStatus)
    expect((wrapper.get('[data-tour="key-form-name"]').element as HTMLInputElement).value)
      .toBe('Unsaved name')

    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenNthCalledWith(2, key.id, expect.objectContaining({ name: 'Unsaved name', status: formStatus }))
    wrapper.unmount()
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

  it('opens bulk editing with only selected visible keys', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    expect(table.props('selectable')).toBe(true)
    table.vm.$emit('update:selectedKeys', [1, 99])
    await nextTick()
    await wrapper.get('[data-test="bulk-edit-keys"]').trigger('click')
    const modal = wrapper.findComponent({ name: 'BulkEditKeysModal' })
    expect(modal.props('show')).toBe(true)
    expect(modal.props('selectedKeys').map((key: ApiKey) => key.id)).toEqual([1])
    wrapper.unmount()
  })

  it.each(['filter', 'page size', 'sort'])('clears selection on %s changes', async (change) => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    if (change === 'filter') {
      wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    } else if (change === 'page size') {
      await wrapper.get('[data-test="page-size-50"]').trigger('click')
    } else {
      table.vm.$emit('sort', 'created_at', 'asc')
    }
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])
    expect(wrapper.find('[data-test="bulk-edit-keys"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('removes successful keys from the selection and refreshes the table', async () => {
    listKeys.mockResolvedValue({
      items: [createApiKey(), { ...createApiKey(), id: 2, name: 'Second' }],
      total: 2, pages: 1
    })
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1, 2])
    await nextTick()
    await wrapper.get('[data-test="bulk-edit-keys"]').trigger('click')
    wrapper.findComponent({ name: 'BulkEditKeysModal' }).vm.$emit('updated', [1])
    await flushPromises()
    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(table.props('selectedKeys')).toEqual([2])
    wrapper.unmount()
  })

  it('drops keys that are no longer visible after a refresh', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    listKeys.mockResolvedValue({ items: [], total: 0, pages: 0 })
    await wrapper.get('button[title="Refresh"]').trigger('click')
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])
    wrapper.unmount()
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

  it('allows an unverified changed email to enable notifications and rotation', async () => {
    const expiresAt = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString()
    listKeys.mockResolvedValueOnce({
      items: [{
        ...createApiKey(),
        group_id: 42,
        expires_at: expiresAt,
        notification_email: 'old@example.com',
        notification_email_verified: true,
      }],
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
    updateKey.mockResolvedValueOnce({})
    const wrapper = await mountView()

    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await nextTick()
    await wrapper.get('input[type="email"]').setValue('new@example.com')
    expect(wrapper.text()).toContain('Email verification is optional')

    const toggles = wrapper.findAll('button[aria-pressed]')
    expect(toggles).toHaveLength(2)
    await toggles[0]!.trigger('click')
    await toggles[1]!.trigger('click')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(sendNotificationEmailCode).not.toHaveBeenCalled()
    expect(verifyNotificationEmail).not.toHaveBeenCalled()
    expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({
      notification_email: 'new@example.com',
      change_notify_enabled: true,
      rotate_on_expiry: true,
    }))
    expect(updateKey.mock.calls[0]![1]).not.toHaveProperty('notification_email_verification_token')
  })

  it('creates a key with notifications without verifying the email', async () => {
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
    createKey.mockResolvedValueOnce({})
    const wrapper = await mountView()

    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await nextTick()
    await wrapper.get('input[data-tour="key-form-name"]').setValue('notification-key')
    const matchingSelects = wrapper.findAllComponents({ name: 'Select' }).filter(
      (select) => select.props('options')?.some((option: { value?: unknown }) => option.value === 42),
    )
    await matchingSelects[matchingSelects.length - 1]!.vm.$emit('update:modelValue', 42)
    await wrapper.get('input[type="email"]').setValue('owner@example.com')
    await wrapper.findAll('button[aria-pressed]')[0]!.trigger('click')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(createKey).toHaveBeenCalledWith(expect.objectContaining({
      name: 'notification-key',
      group_id: 42,
      notification_email: 'owner@example.com',
      change_notify_enabled: true,
      rotate_on_expiry: false,
    }))
    expect(createKey.mock.calls[0]![0]).not.toHaveProperty('notification_email_verification_token')
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

  describe('create provider selection', () => {
    const platforms = ['anthropic', 'openai', 'kimi', 'zhipu', 'deepseek', 'minimax', 'gemini', 'grok', 'antigravity', 'composite', 'opencode_go']
    const availableGroups = platforms.map((platform, index) => ({
      id: index + 1,
      // Deliberately ambiguous names: classification must follow the platform.
      name: `Shared group ${index + 1}`,
      platform,
      rate_multiplier: 1,
      subscription_type: 'standard',
      vip_only: false,
      can_bind: true,
      deny_reason: null,
      suggested_action: null,
    }))
    const groupSelect = (wrapper: VueWrapper) => wrapper.findComponent('[data-tour="key-form-group"]')
    const optionIds = (wrapper: VueWrapper) => groupSelect(wrapper).props('options').map((option: { value: number }) => option.value)
    const chooseProvider = (wrapper: VueWrapper, value: string) => wrapper.get(`input[name="key-provider"][value="${value}"]`).setValue()
    const openCreate = async () => {
      const wrapper = await mountView()
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      return wrapper
    }

    beforeEach(() => {
      getAvailableGroups.mockResolvedValue(availableGroups)
    })

    it('classifies all configured platforms and retains the complete table filter', async () => {
      const wrapper = await openCreate()
      expect(wrapper.findAll('input[name="key-provider"]')).toHaveLength(4)
      expect(optionIds(wrapper)).toEqual([1])
      await chooseProvider(wrapper, 'openai')
      expect(optionIds(wrapper)).toEqual([2])
      await chooseProvider(wrapper, 'domestic')
      expect(optionIds(wrapper)).toEqual([3, 4, 5, 6])
      await chooseProvider(wrapper, 'other')
      expect(optionIds(wrapper)).toEqual([7, 8, 9, 10, 11])
      expect(wrapper.findAllComponents({ name: 'Select' })[0].props('options')).toHaveLength(13)
    })

    it('clears the previous group on provider change and submits only the newly selected group', async () => {
      const wrapper = await openCreate()
      await wrapper.get('[data-tour="key-form-name"]').setValue('My key')
      await groupSelect(wrapper).vm.$emit('update:modelValue', 1)
      await chooseProvider(wrapper, 'domestic')
      expect(groupSelect(wrapper).props('modelValue')).toBeNull()
      await wrapper.get('#key-form').trigger('submit')
      expect(keysAPI.create).not.toHaveBeenCalled()
      expect(showError).toHaveBeenCalledWith('keys.groupRequired')

      await groupSelect(wrapper).vm.$emit('update:modelValue', 5)
      vi.mocked(keysAPI.create).mockResolvedValue({ ...createApiKey(), group_id: 5 })
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(keysAPI.create).toHaveBeenCalledOnce()
      expect(keysAPI.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'My key', group_id: 5 }))
    })

    it('defaults to a provider with available groups and disables empty categories', async () => {
      getAvailableGroups.mockResolvedValue([availableGroups[5]])
      const wrapper = await openCreate()
      expect(wrapper.get<HTMLInputElement>('input[value="domestic"]').element.checked).toBe(true)
      expect(wrapper.get<HTMLInputElement>('input[value="anthropic"]').element.disabled).toBe(true)
      expect(optionIds(wrapper)).toEqual([6])
    })

    it('shows the empty state when no groups are available', async () => {
      getAvailableGroups.mockResolvedValue([])
      const wrapper = await openCreate()
      expect(wrapper.get('[data-tour="key-form-provider"]').text()).toContain('common.noGroupsAvailable')
      expect(optionIds(wrapper)).toEqual([])
      expect(wrapper.findAll<HTMLInputElement>('input[name="key-provider"]').every((input) => input.element.disabled)).toBe(true)
    })

    it('selects an available provider when groups arrive after opening', async () => {
      let resolveGroups!: (value: typeof availableGroups) => void
      getAvailableGroups.mockReturnValue(new Promise((resolve) => { resolveGroups = resolve }))
      const wrapper = await openCreate()
      resolveGroups([availableGroups[1]])
      await flushPromises()
      expect(wrapper.get<HTMLInputElement>('input[value="openai"]').element.checked).toBe(true)
      expect(optionIds(wrapper)).toEqual([2])
    })

    it('resets provider and group when reopening create, and preserves edit options', async () => {
      const wrapper = await openCreate()
      await chooseProvider(wrapper, 'domestic')
      await groupSelect(wrapper).vm.$emit('update:modelValue', 5)
      await wrapper.get('[data-test="close-dialog"]').trigger('click')
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      expect(optionIds(wrapper)).toEqual([1])
      expect(groupSelect(wrapper).props('modelValue')).toBeNull()
      await wrapper.get('[data-test="close-dialog"]').trigger('click')
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      expect(wrapper.find('[data-tour="key-form-provider"]').exists()).toBe(false)
      expect(optionIds(wrapper)).toHaveLength(11)
    })
  })
})
