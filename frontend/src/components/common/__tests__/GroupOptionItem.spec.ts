import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupOptionItem from '../GroupOptionItem.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: null }),
}))

describe('GroupOptionItem description layout', () => {
  it('applies multiline and overflow-safe text styles', () => {
    const description = 'First section\nvery-long-unbroken-description-value-that-must-not-overflow'
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'Example group',
        platform: 'openai',
        description,
      },
      global: {
        stubs: {
          GroupBadge: true,
        },
      },
    })

    const descriptionElement = wrapper
      .findAll('span')
      .find((element) => element.text() === description)

    expect(descriptionElement).toBeDefined()
    expect(descriptionElement?.classes()).toContain('whitespace-pre-line')
    expect(descriptionElement?.classes()).toContain('[overflow-wrap:anywhere]')
    expect(descriptionElement?.classes()).toContain('line-clamp-3')
    expect(wrapper.find('[title]').attributes('title')).toBe(description)
  })

  it('shows a VIP badge, denial reason and an independent payment action for a known decision', async () => {
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'VIP group',
        platform: 'openai',
        vipOnly: true,
        canBind: false,
        denyReason: 'GROUP_VIP_ONLY',
        suggestedAction: 'PAYMENT',
        allowPaymentAction: true,
      },
      global: {
        stubs: {
          GroupBadge: true,
        },
      },
    })

    expect(wrapper.get('[data-testid="vip-only-badge"]').text()).toBe('vip.group.badge')
    expect(wrapper.get('[data-testid="group-access-denied"]').text()).toContain('vip.group.denied.GROUP_VIP_ONLY')

    await wrapper.get('[data-testid="group-payment-cta"]').trigger('click')
    expect(wrapper.emitted('action')).toEqual([['PAYMENT']])
  })

  it('fails safe for unknown denial or action values', () => {
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'Future group',
        platform: 'openai',
        vipOnly: true,
        canBind: false,
        denyReason: 'FUTURE_DENIAL',
        suggestedAction: 'PAYMENT',
        allowPaymentAction: true,
      },
      global: {
        stubs: {
          GroupBadge: true,
        },
      },
    })

    expect(wrapper.get('[data-testid="group-access-denied"]').text()).toContain('vip.group.denied.UNKNOWN')
    expect(wrapper.find('[data-testid="group-payment-cta"]').exists()).toBe(false)
  })
})
