import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AmountInput from '../AmountInput.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      key === 'payment.minimumRechargeAmount'
        ? `minimum:${params?.amount}`
        : key,
  }),
}))

describe('AmountInput', () => {
  it('shows eight default quick amounts in a four-column, two-row grid without 10', () => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: null,
      },
    })

    const buttons = wrapper.findAll('button')
    expect(buttons.map(button => button.text())).toEqual([
      '20', '50', '100', '200', '500', '1000', '2000', '5000',
    ])
    expect(wrapper.find('.grid').classes()).toContain('grid-cols-4')
  })

  it('shows the configured minimum recharge amount only when it is positive', async () => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: null,
        configuredMin: 5.5,
      },
    })

    expect(wrapper.get('[data-test="minimum-recharge-amount"]').text()).toBe('minimum:5.5')

    await wrapper.setProps({ configuredMin: 0 })
    expect(wrapper.find('[data-test="minimum-recharge-amount"]').exists()).toBe(false)
  })
})
