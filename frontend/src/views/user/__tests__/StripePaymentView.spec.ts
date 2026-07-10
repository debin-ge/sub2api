import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))
const routerPush = vi.hoisted(() => vi.fn())
const getOrder = vi.hoisted(() => vi.fn())
const paymentStore = vi.hoisted(() => ({
  config: { stripe_publishable_key: 'pk_test' } as { stripe_publishable_key?: string },
  fetchConfig: vi.fn(),
  pollOrderStatus: vi.fn(),
}))
const loadStripe = vi.hoisted(() => vi.fn())
const stripeElements = vi.hoisted(() => ({
  create: vi.fn(),
}))
const stripePaymentElement = vi.hoisted(() => ({
  mount: vi.fn(),
  on: vi.fn(),
}))
const expressHandlers = vi.hoisted(() => new Map<string, (event: any) => unknown>())
const stripeExpressElement = vi.hoisted(() => ({
  mount: vi.fn(),
  destroy: vi.fn(),
  on: vi.fn(),
  off: vi.fn(),
}))
const stripeInstance = vi.hoisted(() => ({
  elements: vi.fn(),
  confirmPayment: vi.fn(),
  confirmAlipayPayment: vi.fn(),
  confirmWechatPayPayment: vi.fn(),
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState,
    useRouter: () => ({ push: routerPush }),
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'zh-CN' },
    }),
  }
})

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => paymentStore,
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    getOrder,
  },
}))

vi.mock('@stripe/stripe-js', () => ({
  loadStripe,
}))

import StripePaymentView from '../StripePaymentView.vue'
import { formatPaymentAmount } from '@/components/payment/currency'
import type { PaymentOrder } from '@/types/payment'

function orderFactory(overrides: Partial<PaymentOrder> = {}): PaymentOrder {
  return {
    id: 42,
    user_id: 7,
    amount: 100,
    pay_amount: 103,
    currency: 'CNY',
    fee_rate: 0.03,
    payment_type: 'stripe',
    out_trade_no: 'sub2_stripe_42',
    status: 'PENDING',
    order_type: 'balance',
    created_at: '2026-04-20T12:00:00Z',
    expires_at: '2026-04-20T12:30:00Z',
    refund_amount: 0,
    ...overrides,
  }
}

function mountView() {
  return mount(StripePaymentView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: true,
      },
    },
  })
}

describe('StripePaymentView', () => {
  beforeEach(() => {
    expressHandlers.clear()
    routeState.query = {
      order_id: '42',
      client_secret: 'pi_secret_42',
    }
    routerPush.mockReset()
    getOrder.mockReset()
    paymentStore.config = { stripe_publishable_key: 'pk_test' }
    paymentStore.fetchConfig.mockReset().mockResolvedValue(undefined)
    paymentStore.pollOrderStatus.mockReset()
    loadStripe.mockReset().mockResolvedValue(stripeInstance)
    stripeElements.create.mockReset().mockImplementation((type: string) => (
      type === 'expressCheckout' ? stripeExpressElement : stripePaymentElement
    ))
    stripePaymentElement.mount.mockReset()
    stripePaymentElement.on.mockReset().mockImplementation((event: string, callback: () => void) => {
      if (event === 'ready') callback()
    })
    stripeExpressElement.mount.mockReset()
    stripeExpressElement.destroy.mockReset()
    stripeExpressElement.on.mockReset().mockImplementation((event: string, handler: (event: any) => unknown) => {
      expressHandlers.set(event, handler)
      return stripeExpressElement
    })
    stripeExpressElement.off.mockReset().mockImplementation((event: string) => {
      expressHandlers.delete(event)
      return stripeExpressElement
    })
    stripeInstance.elements.mockReset().mockReturnValue(stripeElements)
    stripeInstance.confirmPayment.mockReset()
    stripeInstance.confirmAlipayPayment.mockReset()
    stripeInstance.confirmWechatPayPayment.mockReset()
    window.localStorage.clear()
  })

  it('本地恢复快照缺失时使用订单接口返回的 Stripe 币种展示金额', async () => {
    getOrder.mockResolvedValue({
      data: orderFactory({ currency: 'HKD', pay_amount: 103 }),
    })

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(getOrder).toHaveBeenCalledWith(42)
    expect(loadStripe).toHaveBeenCalledWith('pk_test')
    expect(wrapper.text()).toContain(formatPaymentAmount(103, 'HKD', 'zh-CN'))
    wrapper.unmount()
  })

  it('creates Payment and Express Checkout Elements from one Elements instance', async () => {
    getOrder.mockResolvedValue({ data: orderFactory({ currency: 'USD' }) })

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(stripeInstance.elements).toHaveBeenCalledTimes(1)
    expect(stripeElements.create).toHaveBeenCalledWith('payment', expect.any(Object))
    expect(stripeElements.create).toHaveBeenCalledWith(
      'expressCheckout',
      expect.objectContaining({
        paymentMethods: {
          googlePay: 'auto',
          applePay: 'never',
          link: 'never',
          amazonPay: 'never',
          paypal: 'never',
          klarna: 'never',
        },
      }),
    )
    wrapper.unmount()
  })

  it('does not let Google Pay confirm while the generic form owns the lock', async () => {
    let resolveCardConfirmation!: (value: object) => void
    stripeInstance.confirmPayment.mockReturnValue(new Promise(resolve => {
      resolveCardConfirmation = resolve
    }))
    getOrder.mockResolvedValue({ data: orderFactory() })

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    const cardPromise = wrapper.get('button.btn-stripe').trigger('click')
    await nextTick()
    const paymentFailed = vi.fn()
    await expressHandlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed,
    })

    expect(stripeInstance.confirmPayment).toHaveBeenCalledTimes(1)
    expect(paymentFailed).toHaveBeenCalledWith({
      reason: 'fail',
      message: 'common.processing',
    })

    resolveCardConfirmation({})
    await cardPromise
    await flushPromises()
    wrapper.unmount()
  })

  it('shares submitting state and completes the existing success flow through the real child', async () => {
    let resolveConfirmation!: (value: object) => void
    stripeInstance.confirmPayment.mockReturnValue(new Promise(resolve => {
      resolveConfirmation = resolve
    }))
    getOrder.mockResolvedValue({ data: orderFactory() })
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    const confirmPromise = expressHandlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed: vi.fn(),
    })
    await nextTick()
    expect(wrapper.get('button.btn-stripe').attributes('disabled')).toBeDefined()

    resolveConfirmation({})
    await confirmPromise
    await flushPromises()
    expect(wrapper.text()).toContain('payment.stripeSuccessProcessing')
    wrapper.unmount()
  })

  it('does not render Google Pay in the direct Alipay flow', async () => {
    routeState.query = {
      order_id: '42',
      client_secret: 'pi_secret_42',
      method: 'alipay',
    }
    getOrder.mockResolvedValue({ data: orderFactory() })
    stripeInstance.confirmAlipayPayment.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()

    expect(stripeElements.create).not.toHaveBeenCalledWith('expressCheckout', expect.anything())
    expect(wrapper.find('[data-testid="stripe-google-pay-express"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not render Google Pay in the direct WeChat Pay flow', async () => {
    routeState.query = {
      order_id: '42',
      client_secret: 'pi_secret_42',
      method: 'wechat_pay',
    }
    getOrder.mockResolvedValue({ data: orderFactory() })
    stripeInstance.confirmWechatPayPayment.mockResolvedValue({
      error: { message: 'stop after direct branch selection' },
    })

    const wrapper = mountView()
    await flushPromises()

    expect(stripeElements.create).not.toHaveBeenCalledWith('expressCheckout', expect.anything())
    expect(wrapper.find('[data-testid="stripe-google-pay-express"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
