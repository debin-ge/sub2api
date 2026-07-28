import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))
const routerPush = vi.hoisted(() => vi.fn())
const getOrder = vi.hoisted(() => vi.fn())
const verifyOrder = vi.hoisted(() => vi.fn())
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
    verifyOrder,
  },
}))

vi.mock('@stripe/stripe-js/pure', () => ({
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
  return shallowMount(StripePaymentView, {
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
    routeState.query = {
      order_id: '42',
      client_secret: 'pi_secret_42',
    }
    routerPush.mockReset()
    getOrder.mockReset()
    verifyOrder.mockReset()
    paymentStore.config = { stripe_publishable_key: 'pk_test' }
    paymentStore.fetchConfig.mockReset().mockResolvedValue(undefined)
    paymentStore.pollOrderStatus.mockReset()
    loadStripe.mockReset().mockResolvedValue(stripeInstance)
    stripeElements.create.mockReset().mockReturnValue(stripePaymentElement)
    stripePaymentElement.mount.mockReset()
    stripePaymentElement.on.mockReset().mockImplementation((event: string, callback: () => void) => {
      if (event === 'ready') callback()
    })
    stripeInstance.elements.mockReset().mockReturnValue(stripeElements)
    stripeInstance.confirmPayment.mockReset()
    stripeInstance.confirmAlipayPayment.mockReset()
    stripeInstance.confirmWechatPayPayment.mockReset()
    window.localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
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
  })

  it('Stripe 跳转返回链接保留签名恢复令牌和订单号', async () => {
    routeState.query = {
      order_id: '42',
      client_secret: 'pi_secret_42',
      method: 'alipay',
      resume_token: 'resume-stripe-42',
    }
    getOrder.mockResolvedValue({
      data: orderFactory({
        out_trade_no: 'sub2_stripe_return_42',
      }),
    })
    stripeInstance.confirmAlipayPayment.mockResolvedValue({})

    mountView()
    await flushPromises()
    await flushPromises()

    expect(stripeInstance.confirmAlipayPayment).toHaveBeenCalledTimes(1)
    const [, options] = stripeInstance.confirmAlipayPayment.mock.calls[0] as [
      string,
      { return_url: string },
    ]
    const returnUrl = new URL(options.return_url)
    expect(returnUrl.pathname).toBe('/payment/result')
    expect(returnUrl.searchParams.get('order_id')).toBe('42')
    expect(returnUrl.searchParams.get('resume_token')).toBe('resume-stripe-42')
    expect(returnUrl.searchParams.get('out_trade_no')).toBe('sub2_stripe_return_42')
  })

  it('微信扫码后主动核验返回 RECHARGING 时及时显示成功', async () => {
    vi.useFakeTimers()
    routeState.query = {
      order_id: '42',
      client_secret: 'pi_secret_42',
      method: 'wechat_pay',
    }
    getOrder.mockResolvedValue({
      data: orderFactory(),
    })
    stripeInstance.confirmWechatPayPayment.mockResolvedValue({
      paymentIntent: {
        status: 'requires_action',
        next_action: {
          wechat_pay_display_qr_code: {
            image_data_url: 'data:image/png;base64,wechat-qr',
          },
        },
      },
    })
    paymentStore.pollOrderStatus.mockResolvedValue(orderFactory())
    verifyOrder.mockResolvedValue({
      data: orderFactory({ status: 'RECHARGING' }),
    })

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('img[alt="WeChat Pay QR"]').exists()).toBe(true)

    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(paymentStore.pollOrderStatus).toHaveBeenCalledWith(42)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_stripe_42')
    expect(verifyOrder).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('payment.result.success')
  })
})
