import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import AmountInput from '../AmountInput.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      key === 'payment.minimumRechargeAmount'
        ? `minimum:${params?.amount}`
        : key,
  }),
}))
enableAutoUnmount(afterEach)

function mountInput(value: number | null = null) {
  return mount(AmountInput, { props: { modelValue: value } })
}

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

describe('recharge amount input', () => {
  it.each(['10abc', '10.555', '-10', '1e2'])('restores the accepted amount after rejecting %s', async (value) => {
    const wrapper = mountInput(10)
    const input = wrapper.get('input')
    await input.setValue(value)
    expect((input.element as HTMLInputElement).value).toBe('10')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('restores the last typed amount rather than a stale prop', async () => {
    const wrapper = mountInput()
    const input = wrapper.get('input')
    await input.setValue('12.50')
    await input.setValue('12.500')
    expect((input.element as HTMLInputElement).value).toBe('12.50')
    expect(wrapper.emitted('update:modelValue')).toEqual([[12.5]])
  })

  it('preserves decimal editing and allows clearing the amount', async () => {
    const wrapper = mountInput()
    const input = wrapper.get('input')
    for (const value of ['0', '0.', '0.5', '0.50', '']) await input.setValue(value)
    expect(wrapper.emitted('update:modelValue')).toEqual([[null], [null], [0.5], [0.5], [null]])
    expect((input.element as HTMLInputElement).value).toBe('')
  })
})
