import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ApiKeyTokenRanking from '../ApiKeyTokenRanking.vue'

const getApiKeyBreakdown = vi.hoisted(() => vi.fn())
const getMyApiKeyBreakdown = vi.hoisted(() => vi.fn())

vi.mock('@/api/admin/dashboard', () => ({
  getApiKeyBreakdown,
}))

vi.mock('@/api/usage', () => ({
  getMyApiKeyBreakdown,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const mountRanking = (props: Record<string, unknown>) => mount(ApiKeyTokenRanking, {
  props: {
    mode: 'admin',
    startDate: '2026-09-13',
    endDate: '2026-09-13',
    filters: {},
    ...props,
  },
  global: { stubs: { Select: true, LoadingSpinner: true } },
})

describe('ApiKeyTokenRanking', () => {
  beforeEach(() => {
    getApiKeyBreakdown.mockReset().mockResolvedValue({ api_keys: [] })
    getMyApiKeyBreakdown.mockReset().mockResolvedValue({ api_keys: [] })
  })

  it('preserves the admin model and exact rolling bounds', async () => {
    const wrapper = mountRanking({
      filters: { group_id: 3 },
      model: 'gpt-5.4',
      startTime: '2026-09-12T19:00:00.000Z',
      endTime: '2026-09-13T19:00:00.000Z',
    })
    await flushPromises()

    expect(getApiKeyBreakdown).toHaveBeenCalledWith(expect.objectContaining({
      group_id: 3,
      model: 'gpt-5.4',
      start_date: '2026-09-13',
      end_date: '2026-09-13',
      start_time: '2026-09-12T19:00:00.000Z',
      end_time: '2026-09-13T19:00:00.000Z',
    }))
    wrapper.unmount()
  })

  it('preserves the user model and exact rolling bounds', async () => {
    const wrapper = mountRanking({
      mode: 'user',
      model: 'claude-sonnet-5',
      startTime: '2026-09-12T19:00:00.000Z',
      endTime: '2026-09-13T19:00:00.000Z',
    })
    await flushPromises()

    expect(getMyApiKeyBreakdown).toHaveBeenCalledWith(expect.objectContaining({
      model: 'claude-sonnet-5',
      start_time: '2026-09-12T19:00:00.000Z',
      end_time: '2026-09-13T19:00:00.000Z',
    }))
    wrapper.unmount()
  })
})
