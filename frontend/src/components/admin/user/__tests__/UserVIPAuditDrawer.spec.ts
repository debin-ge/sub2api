import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UserVIPAuditDrawer from '../UserVIPAuditDrawer.vue'

const { getVIPAudit } = vi.hoisted(() => ({
  getVIPAudit: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: { getVIPAudit }
  }
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string) => `time:${value}`
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const event = {
  id: 1,
  user_id: 7,
  actor_type: 'admin',
  actor_user_id: 2,
  actor_snapshot: 'ops@example.com (#2)',
  action: 'SET_MANUAL_MODE',
  reason: 'compliance hold',
  order_id: null,
  request_id: 'req-1',
  old_paid_eligible: true,
  new_paid_eligible: true,
  old_manual_override: null,
  new_manual_override: false,
  old_is_vip: true,
  new_is_vip: false,
  source: 'ADMIN',
  created_at: '2026-07-29T00:00:00Z'
}

describe('UserVIPAuditDrawer', () => {
  beforeEach(() => {
    getVIPAudit.mockReset()
    getVIPAudit.mockResolvedValue({
      items: [event],
      total: 21,
      page: 1,
      page_size: 20,
      pages: 2
    })
  })

  it('loads the selected user audit and requests the next page', async () => {
    const wrapper = mount(UserVIPAuditDrawer, {
      props: {
        show: true,
        user: { id: 7, email: 'target@example.com' } as any
      },
      global: {
        stubs: {
          Teleport: { template: '<div><slot /></div>' },
          Icon: true,
          Pagination: {
            props: ['page'],
            emits: ['update:page'],
            template: '<button data-test="audit-next" @click="$emit(\'update:page\', 2)">next</button>'
          }
        }
      }
    })
    await flushPromises()

    expect(getVIPAudit).toHaveBeenCalledWith(7, 1, 20)
    expect(wrapper.get('[data-test="vip-audit-event"]').text()).toContain('compliance hold')
    expect(wrapper.text()).toContain('ops@example.com (#2)')
    expect(wrapper.text()).toContain('admin.users.vip.modes.FORCE_OFF')

    getVIPAudit.mockResolvedValueOnce({
      items: [],
      total: 21,
      page: 2,
      page_size: 20,
      pages: 2
    })
    await wrapper.get('[data-test="audit-next"]').trigger('click')
    await flushPromises()

    expect(getVIPAudit).toHaveBeenLastCalledWith(7, 2, 20)
  })
})
