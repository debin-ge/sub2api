import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))
const routerPush = vi.hoisted(() => vi.fn())
const getOrder = vi.hoisted(() => vi.fn())
const paymentStore = vi.hoisted(() => ({
  config: { stripe_publishable_key: 'pk_test' } as {
    stripe_publishable_key?: string
    stripe_google_pay_enabled?: boolean
  },
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
  off: vi.fn(),
  destroy: vi.fn(),
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
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'
import type { PaymentRecoverySnapshot } from '@/components/payment/paymentFlow'
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

function recoverySnapshot(
  overrides: Partial<PaymentRecoverySnapshot> = {},
): PaymentRecoverySnapshot {
  return {
    orderId: 42,
    amount: 100,
    qrCode: '',
    expiresAt: '2099-01-01T00:10:00.000Z',
    paymentType: 'stripe',
    payUrl: '',
    outTradeNo: 'sub2_stripe_42',
    clientSecret: 'pi_secret_42',
    intentId: '',
    currency: 'USD',
    countryCode: 'US',
    paymentEnv: 'test',
    payAmount: 103,
    orderType: 'balance',
    paymentMode: '',
    resumeToken: '',
    createdAt: Date.now(),
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
    paymentStore.config = {
      stripe_publishable_key: 'pk_test',
      stripe_google_pay_enabled: false,
    }
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
    stripePaymentElement.off.mockReset()
    stripePaymentElement.destroy.mockReset()
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
    stripeInstance.confirmPayment.mockReset().mockResolvedValue({})
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

  it('does not create or render Express Checkout or its divider when disabled', async () => {
    getOrder.mockResolvedValue({ data: orderFactory({ currency: 'USD' }) })

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(stripeElements.create).toHaveBeenCalledWith('payment', expect.any(Object))
    expect(stripeElements.create).not.toHaveBeenCalledWith('expressCheckout', expect.anything())
    expect(wrapper.find('[data-testid="stripe-google-pay-state"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stripe-google-pay-divider"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('creates Payment and the real Express Checkout child from one Elements instance when enabled', async () => {
    paymentStore.config = {
      stripe_publishable_key: 'pk_test',
      stripe_google_pay_enabled: true,
    }
    getOrder.mockResolvedValue({ data: orderFactory({ currency: 'USD' }) })

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(stripeInstance.elements).toHaveBeenCalledTimes(1)
    expect(stripeElements.create).toHaveBeenCalledWith('payment', expect.any(Object))
    expect(stripeElements.create).toHaveBeenCalledWith(
      'expressCheckout',
      {
        paymentMethods: {
          googlePay: 'auto',
          applePay: 'never',
          link: 'never',
          amazonPay: 'never',
          paypal: 'never',
          klarna: 'never',
        },
      },
    )
    expect(wrapper.get('[data-testid="stripe-google-pay-state"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="stripe-google-pay-divider"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps a restored false capability authoritative over a true Checkout fallback', async () => {
    paymentStore.config = {
      stripe_publishable_key: 'pk_checkout',
      stripe_google_pay_enabled: true,
    }
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify(recoverySnapshot({
      stripePublishableKey: 'pk_snapshot',
      googlePayEnabled: false,
    })))
    getOrder.mockResolvedValue({ data: orderFactory({ currency: 'USD' }) })

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(loadStripe).toHaveBeenCalledWith('pk_snapshot')
    expect(stripeElements.create).toHaveBeenCalledWith('payment', expect.any(Object))
    expect(stripeElements.create).not.toHaveBeenCalledWith('expressCheckout', expect.anything())
    expect(wrapper.find('[data-testid="stripe-google-pay-state"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stripe-google-pay-divider"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('removes listeners before destroying both Stripe Elements on unmount', async () => {
    paymentStore.config = {
      stripe_publishable_key: 'pk_test',
      stripe_google_pay_enabled: true,
    }
    getOrder.mockResolvedValue({ data: orderFactory() })
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    wrapper.unmount()

    expect(stripePaymentElement.off).toHaveBeenCalledWith('ready', expect.any(Function))
    expect(Math.max(...stripePaymentElement.off.mock.invocationCallOrder)).toBeLessThan(
      stripePaymentElement.destroy.mock.invocationCallOrder[0],
    )
    expect(stripePaymentElement.destroy).toHaveBeenCalledOnce()
    expect(stripeExpressElement.off).toHaveBeenCalledWith('ready', expect.any(Function))
    expect(stripeExpressElement.off).toHaveBeenCalledWith('availablepaymentmethodschange', expect.any(Function))
    expect(stripeExpressElement.off).toHaveBeenCalledWith('confirm', expect.any(Function))
    expect(stripeExpressElement.off).toHaveBeenCalledWith('cancel', expect.any(Function))
    expect(stripeExpressElement.off).toHaveBeenCalledWith('loaderror', expect.any(Function))
    expect(Math.max(...stripeExpressElement.off.mock.invocationCallOrder)).toBeLessThan(
      stripeExpressElement.destroy.mock.invocationCallOrder[0],
    )
    expect(stripeExpressElement.destroy).toHaveBeenCalledOnce()
  })

  it('does not let Google Pay confirm while the generic form owns the lock', async () => {
    paymentStore.config = {
      stripe_publishable_key: 'pk_test',
      stripe_google_pay_enabled: true,
    }
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

  it('does not let the generic form confirm while Google Pay owns the lock', async () => {
    let resolveConfirmation!: (value: object) => void
    stripeInstance.confirmPayment.mockReturnValue(new Promise(resolve => {
      resolveConfirmation = resolve
    }))
    getOrder.mockResolvedValue({ data: orderFactory() })
    paymentStore.config = {
      stripe_publishable_key: 'pk_test',
      stripe_google_pay_enabled: true,
    }
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    const confirmPromise = expressHandlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed: vi.fn(),
    })
    await nextTick()
    expect(wrapper.get('button.btn-stripe').attributes('disabled')).toBeDefined()
    await wrapper.get('button.btn-stripe').trigger('click')
    expect(stripeInstance.confirmPayment).toHaveBeenCalledTimes(1)

    resolveConfirmation({})
    await confirmPromise
    await flushPromises()
    wrapper.unmount()
  })

  it('routes Google Pay confirmation to result polling without local success or clearing recovery', async () => {
    paymentStore.config = {
      stripe_publishable_key: 'pk_test',
      stripe_google_pay_enabled: true,
    }
    routeState.query = {
      order_id: '42',
      client_secret: 'pi_secret_42',
      resume_token: 'resume-stripe-42',
    }
    const restored = recoverySnapshot({
      googlePayEnabled: true,
      resumeToken: 'resume-stripe-42',
    })
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify(restored))
    getOrder.mockResolvedValue({ data: orderFactory() })

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    await expressHandlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed: vi.fn(),
    })
    await flushPromises()

    expect(routerPush).toHaveBeenCalledWith({
      path: '/payment/result',
      query: {
        order_id: '42',
        resume_token: 'resume-stripe-42',
      },
    })
    expect(wrapper.text()).not.toContain('payment.stripeSuccessProcessing')
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY))
      .toBe(JSON.stringify(restored))
    wrapper.unmount()
  })

  it('does not render Google Pay in the direct Alipay flow', async () => {
    paymentStore.config = {
      stripe_publishable_key: 'pk_test',
      stripe_google_pay_enabled: true,
    }
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
    expect(wrapper.find('[data-testid="stripe-google-pay-state"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stripe-google-pay-divider"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not render Google Pay in the direct WeChat Pay flow', async () => {
    paymentStore.config = {
      stripe_publishable_key: 'pk_test',
      stripe_google_pay_enabled: true,
    }
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
    expect(wrapper.find('[data-testid="stripe-google-pay-state"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stripe-google-pay-divider"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
