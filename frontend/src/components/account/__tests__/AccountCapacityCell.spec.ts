import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AccountCapacityCell from '../AccountCapacityCell.vue'
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
    concurrency: 2,
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

describe('AccountCapacityCell', () => {
  it('renders MiniMax official 5h remains when present in account extra', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          extra: {
            minimax_text_5h_limit: 4500,
            minimax_text_5h_remaining: 3200,
            minimax_remains_synced_at: '2026-05-04T00:00:00Z'
          }
        })
      },
      global: {
        stubs: {
          QuotaBadge: true
        }
      }
    })

    expect(wrapper.text()).toContain('3200')
    expect(wrapper.text()).toContain('4500')
  })

  it('renders DeepSeek official balance when present in account extra', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          name: 'DeepSeek account',
          platform: 'deepseek',
          extra: {
            deepseek_balance_available: true,
            deepseek_balance_amount: '10.00',
            deepseek_balance_currency: 'CNY',
            deepseek_balance_status: 'ok',
            deepseek_balance_checked_at: '2026-05-12T00:00:00Z'
          }
        })
      },
      global: {
        stubs: {
          QuotaBadge: true
        }
      }
    })

    expect(wrapper.text()).toContain('10.00')
    expect(wrapper.text()).toContain('CNY')
  })

  it.each(['glm', 'kimi'] as const)('does not render fake remains for %s accounts', (platform) => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          name: `${platform} account`,
          platform,
          extra: {
            minimax_text_5h_limit: 4500,
            minimax_text_5h_remaining: 3200
          }
        })
      },
      global: {
        stubs: {
          QuotaBadge: true
        }
      }
    })

    expect(wrapper.text()).not.toContain('3200')
    expect(wrapper.text()).not.toContain('4500')
  })
})
