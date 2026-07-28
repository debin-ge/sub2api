import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PaymentMethodSelector from '@/components/payment/PaymentMethodSelector.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, fallback?: string) => ({
      'payment.methods.stripe': 'Stripe',
      'payment.methods.card': 'Card',
      'payment.methods.wxpay': 'WeChat Pay',
      'payment.methods.alipay': 'Alipay',
      'payment.methods.link': 'Link',
    }[key] ?? fallback ?? key),
  }),
}))

describe('PaymentMethodSelector', () => {
  it('shows the configured display name for custom EasyPay methods', () => {
    const wrapper = mount(PaymentMethodSelector, {
      props: {
        selected: 'ldc',
        methods: [{ type: 'ldc', display_name: 'LDC Pay', fee_rate: 0, available: true }],
      },
    })

    expect(wrapper.text()).toContain('LDC Pay')
    expect(wrapper.text()).not.toContain('ldc')
    expect(wrapper.text()).not.toContain('payment.methods.ldc')
  })

  it('uses the generic selected style for custom methods that contain built-in names', () => {
    const wrapper = mount(PaymentMethodSelector, {
      props: {
        selected: 'card_alipay',
        methods: [{ type: 'card_alipay', display_name: 'Card Pay', fee_rate: 0, available: true }],
      },
    })

    const button = wrapper.get('button')
    expect(button.classes()).toContain('border-primary-500')
    expect(button.classes()).not.toContain('border-[#02A9F1]')
  })

  it('shows only the Stripe sub-methods enabled by the provider', () => {
    const wrapper = mount(PaymentMethodSelector, {
      props: {
        selected: 'stripe',
        methods: [{
          type: 'stripe',
          supported_types: ['wxpay', 'card'],
          fee_rate: 0,
          available: true,
        }],
      },
    })

    expect(wrapper.get('[data-testid="stripe-sub-method-card"]').text()).toBe('Card')
    expect(wrapper.get('[data-testid="stripe-sub-method-wxpay"]').text()).toBe('WeChat Pay')
    expect(wrapper.find('[data-testid="stripe-sub-method-alipay"]').exists()).toBe(false)
  })

  it('automatically renders newly enabled Stripe sub-methods', () => {
    const wrapper = mount(PaymentMethodSelector, {
      props: {
        selected: 'stripe',
        methods: [{
          type: 'stripe',
          supported_types: ['card', 'wxpay', 'alipay', 'link'],
          fee_rate: 0,
          available: true,
        }],
      },
    })

    expect(wrapper.get('[data-testid="stripe-sub-method-alipay"]').text()).toBe('Alipay')
    expect(wrapper.get('[data-testid="stripe-sub-method-link"]').text()).toBe('Link')
  })
})
