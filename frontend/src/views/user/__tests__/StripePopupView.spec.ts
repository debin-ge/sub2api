import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))
const getOrder = vi.hoisted(() => vi.fn())
const verifyOrder = vi.hoisted(() => vi.fn())
const loadStripe = vi.hoisted(() => vi.fn())
const confirmWechatPayPayment = vi.hoisted(() => vi.fn())

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState,
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    getOrder,
    verifyOrder,
  },
}))

vi.mock('@stripe/stripe-js/pure', () => ({
  loadStripe,
}))

import StripePopupView from '../StripePopupView.vue'
import type { PaymentOrder } from '@/types/payment'

function orderFactory(status: PaymentOrder['status']): PaymentOrder {
  return {
    id: 42,
    user_id: 7,
    amount: 100,
    pay_amount: 100,
    currency: 'CNY',
    fee_rate: 0,
    payment_type: 'stripe',
    out_trade_no: 'sub2_stripe_popup_42',
    status,
    order_type: 'balance',
    created_at: '2026-07-27T19:30:00Z',
    expires_at: '2026-07-27T19:35:00Z',
    refund_amount: 0,
  }
}

describe('StripePopupView', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    routeState.query = {
      order_id: '42',
      method: 'wechat_pay',
      amount: '100',
    }
    getOrder.mockReset().mockResolvedValue({
      data: orderFactory('PENDING'),
    })
    verifyOrder.mockReset().mockResolvedValue({
      data: orderFactory('RECHARGING'),
    })
    confirmWechatPayPayment.mockReset().mockResolvedValue({
      paymentIntent: {
        status: 'requires_action',
      },
    })
    loadStripe.mockReset().mockResolvedValue({
      confirmWechatPayPayment,
    })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('treats RECHARGING as success after actively verifying a pending Stripe popup order', async () => {
    const closeSpy = vi.spyOn(window, 'close').mockImplementation(() => {})
    const wrapper = mount(StripePopupView)

    window.dispatchEvent(new MessageEvent('message', {
      origin: window.location.origin,
      data: {
        type: 'STRIPE_POPUP_INIT',
        clientSecret: 'pi_secret_popup_42',
        publishableKey: 'pk_test',
      },
    }))
    await flushPromises()
    await flushPromises()

    expect(confirmWechatPayPayment).toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(getOrder).toHaveBeenCalledWith(42)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_stripe_popup_42')
    expect(verifyOrder).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('payment.result.success')
    expect(closeSpy).not.toHaveBeenCalled()

    wrapper.unmount()
  })
})
