import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AccountActionMenu from '../AccountActionMenu.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'MiniMax account',
    platform: 'minimax',
    type: 'apikey',
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-05-01T00:00:00Z',
    updated_at: '2026-05-01T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides
  } as Account
}

describe('AccountActionMenu', () => {
  it('shows and emits MiniMax official remains sync for MiniMax API key accounts', async () => {
    const account = makeAccount({})
    const wrapper = mount(AccountActionMenu, {
      props: {
        show: true,
        account,
        position: { top: 10, left: 20 }
      },
      global: {
        stubs: {
          Teleport: { template: '<div><slot /></div>' },
          Icon: { template: '<span />' }
        }
      }
    })

    expect(wrapper.text()).toContain('admin.accounts.syncMiniMaxRemains')

    const button = wrapper.findAll('button').find(item => item.text().includes('admin.accounts.syncMiniMaxRemains'))
    expect(button).toBeTruthy()
    await button!.trigger('click')

    expect(wrapper.emitted('sync-minimax-remains')?.[0]).toEqual([account])
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})
