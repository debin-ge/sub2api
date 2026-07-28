import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'

enableAutoUnmount(afterEach)

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))

const routerPush = vi.hoisted(() => vi.fn())
const pollOrderStatus = vi.hoisted(() => vi.fn())
const verifyOrder = vi.hoisted(() => vi.fn())
const verifyOrderPublic = vi.hoisted(() => vi.fn())
const resolveOrderPublicByResumeToken = vi.hoisted(() => vi.fn())

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
    }),
  }
})

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => ({
    pollOrderStatus,
  }),
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    verifyOrder,
    verifyOrderPublic,
    resolveOrderPublicByResumeToken,
  },
}))

import PaymentResultView from '../PaymentResultView.vue'
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'
import { formatPaymentAmount } from '@/components/payment/currency'

const orderFactory = (status: string) => ({
  id: 42,
  user_id: 9,
  amount: 88,
  pay_amount: 88,
  fee_rate: 0,
  payment_type: 'alipay',
  out_trade_no: 'sub2_20260420abcd1234',
  status,
  order_type: 'balance',
  created_at: '2026-04-20T12:00:00Z',
  expires_at: '2026-04-20T12:30:00Z',
  refund_amount: 0,
})

const recoverySnapshotFactory = (resumeToken: string) => ({
  orderId: 42,
  amount: 88,
  qrCode: '',
  expiresAt: '2099-01-01T00:10:00.000Z',
  paymentType: 'alipay',
  payUrl: 'https://pay.example.com/session/42',
  outTradeNo: 'sub2_20260420abcd1234',
  clientSecret: '',
  intentId: '',
  currency: '',
  countryCode: '',
  paymentEnv: '',
  payAmount: 88,
  orderType: 'balance',
  paymentMode: 'popup',
  resumeToken,
  createdAt: Date.UTC(2099, 0, 1, 0, 0, 0),
})

describe('PaymentResultView', () => {
  beforeEach(() => {
    routeState.query = {}
    routerPush.mockReset()
    pollOrderStatus.mockReset()
    verifyOrder.mockReset()
    verifyOrderPublic.mockReset()
    resolveOrderPublicByResumeToken.mockReset()
    window.localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('renders a pending state instead of a failure state when the restored order is still pending', async () => {
    routeState.query = {
      resume_token: 'resume-42',
      order_id: '999',
      status: 'success',
    }
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
      orderId: 42,
      amount: 88,
      qrCode: '',
      expiresAt: '2099-01-01T00:10:00.000Z',
      paymentType: 'alipay',
      payUrl: 'https://pay.example.com/session/42',
      outTradeNo: 'sub2_20260420abcd1234',
      clientSecret: '',
      intentId: '',
      currency: '',
      countryCode: '',
      paymentEnv: '',
      payAmount: 88,
      orderType: 'balance',
      paymentMode: 'redirect',
      resumeToken: 'resume-42',
      createdAt: Date.UTC(2099, 0, 1, 0, 0, 0),
    }))
    resolveOrderPublicByResumeToken.mockResolvedValue({
      data: orderFactory('PENDING'),
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledWith('resume-42')
    expect(pollOrderStatus).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.processing')
    expect(wrapper.text()).not.toContain('payment.result.success')
    expect(wrapper.text()).not.toContain('payment.result.failed')
  })

  it('prefers the public resume-token result over a stale restored DB snapshot', async () => {
    routeState.query = {
      resume_token: 'resume-authoritative',
      order_id: '42',
      status: 'success',
    }
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
      orderId: 42,
      amount: 88,
      qrCode: '',
      expiresAt: '2099-01-01T00:10:00.000Z',
      paymentType: 'alipay',
      payUrl: 'https://pay.example.com/session/42',
      outTradeNo: 'sub2_20260420abcd1234',
      clientSecret: '',
      intentId: '',
      currency: '',
      countryCode: '',
      paymentEnv: '',
      payAmount: 88,
      orderType: 'balance',
      paymentMode: 'popup',
      resumeToken: 'resume-authoritative',
      createdAt: Date.UTC(2099, 0, 1, 0, 0, 0),
    }))
    resolveOrderPublicByResumeToken.mockResolvedValue({
      data: {
        ...orderFactory('PAID'),
        amount: 100,
        pay_amount: 103,
        fee_rate: 3,
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(pollOrderStatus).not.toHaveBeenCalled()
    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledWith('resume-authoritative')
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.text()).toContain('103.00')
    expect(wrapper.text()).toContain('100.00')
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('uses authenticated local polling after the initial resume-token recovery', async () => {
    vi.useFakeTimers()
    routeState.query = {
      resume_token: 'resume-77',
    }
    window.localStorage.setItem(
      PAYMENT_RECOVERY_STORAGE_KEY,
      JSON.stringify(recoverySnapshotFactory('resume-77')),
    )
    resolveOrderPublicByResumeToken.mockResolvedValueOnce({
      data: orderFactory('PENDING'),
    })
    pollOrderStatus.mockResolvedValueOnce(orderFactory('PAID'))

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('payment.result.processing')
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).not.toBeNull()

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledTimes(1)
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.text()).not.toContain('payment.result.failed')
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('keeps two-second resume-token fallback for a non-Stripe order when local polling is unavailable', async () => {
    vi.useFakeTimers()
    routeState.query = {
      resume_token: 'resume-non-stripe-fallback',
    }
    window.localStorage.setItem(
      PAYMENT_RECOVERY_STORAGE_KEY,
      JSON.stringify(recoverySnapshotFactory('resume-non-stripe-fallback')),
    )
    resolveOrderPublicByResumeToken
      .mockResolvedValueOnce({
        data: orderFactory('PENDING'),
      })
      .mockResolvedValueOnce({
        data: orderFactory('PAID'),
      })
    pollOrderStatus.mockRejectedValue(new Error('auth unavailable'))

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()
    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('payment.result.success')
    wrapper.unmount()
  })

  it('falls back to order_id polling when resume-token recovery fails', async () => {
    routeState.query = {
      resume_token: 'resume-fail',
      order_id: '77',
    }
    window.localStorage.setItem(
      PAYMENT_RECOVERY_STORAGE_KEY,
      JSON.stringify({
        ...recoverySnapshotFactory('resume-fail'),
        orderId: 42,
      }),
    )
    resolveOrderPublicByResumeToken.mockRejectedValueOnce(new Error('resume failed'))
    pollOrderStatus.mockResolvedValueOnce({
      ...orderFactory('PAID'),
      id: 77,
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledWith('resume-fail')
    expect(pollOrderStatus).toHaveBeenCalledWith(77)
    expect(verifyOrderPublic).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.success')
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('restores the signed resume token from local recovery state after a Stripe return', async () => {
    routeState.query = {
      order_id: '42',
      status: 'success',
    }
    window.localStorage.setItem(
      PAYMENT_RECOVERY_STORAGE_KEY,
      JSON.stringify({
        ...recoverySnapshotFactory('resume-from-snapshot'),
        paymentType: 'stripe',
        outTradeNo: 'sub2_stripe_snapshot_42',
      }),
    )
    resolveOrderPublicByResumeToken.mockResolvedValueOnce({
      data: {
        ...orderFactory('COMPLETED'),
        payment_type: 'stripe',
        out_trade_no: 'sub2_stripe_snapshot_42',
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledWith('resume-from-snapshot')
    expect(pollOrderStatus).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.success')
  })

  it('actively verifies a pending Stripe order resolved through order_id', async () => {
    routeState.query = {
      order_id: '42',
      status: 'success',
    }
    pollOrderStatus.mockResolvedValueOnce({
      ...orderFactory('PENDING'),
      payment_type: 'stripe',
      out_trade_no: 'sub2_stripe_result_42',
    })
    verifyOrder.mockResolvedValueOnce({
      data: {
        ...orderFactory('COMPLETED'),
        payment_type: 'stripe',
        out_trade_no: 'sub2_stripe_result_42',
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(verifyOrder).toHaveBeenCalledTimes(1)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_stripe_result_42')
    expect(wrapper.text()).toContain('payment.result.success')
  })

  it('uses provider_key to verify a Stripe-backed wxpay order resolved through order_id', async () => {
    routeState.query = {
      order_id: '42',
      status: 'success',
    }
    pollOrderStatus.mockResolvedValueOnce({
      ...orderFactory('PENDING'),
      payment_type: 'wxpay',
      provider_key: 'stripe',
      out_trade_no: 'sub2_stripe_wxpay_result_42',
    })
    verifyOrder.mockResolvedValueOnce({
      data: {
        ...orderFactory('COMPLETED'),
        payment_type: 'wxpay',
        provider_key: 'stripe',
        out_trade_no: 'sub2_stripe_wxpay_result_42',
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(verifyOrder).toHaveBeenCalledTimes(1)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_stripe_wxpay_result_42')
    expect(wrapper.text()).toContain('payment.result.success')
  })

  it('shows success when Stripe completes after the old five-minute polling cutoff', async () => {
    vi.useFakeTimers()
    routeState.query = {
      order_id: '42',
      status: 'success',
    }
    const pendingStripeOrder = {
      ...orderFactory('PENDING'),
      payment_type: 'stripe',
      out_trade_no: 'sub2_stripe_long_refresh_42',
    }
    let localStatusReads = 0
    pollOrderStatus.mockImplementation(async () => {
      localStatusReads += 1
      return localStatusReads >= 152
        ? { ...pendingStripeOrder, status: 'COMPLETED' }
        : pendingStripeOrder
    })
    verifyOrder.mockResolvedValue({
      data: pendingStripeOrder,
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(298000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledTimes(150)
    expect(verifyOrder.mock.calls.length).toBeLessThanOrEqual(20)

    await vi.advanceTimersByTimeAsync(14000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledTimes(152)
    expect(wrapper.text()).toContain('payment.result.success')
    wrapper.unmount()
  })

  it('keeps two-second local polling while limiting resume and verify upstream recovery calls', async () => {
    vi.useFakeTimers()
    routeState.query = {
      resume_token: 'resume-stripe-long',
      order_id: '42',
      status: 'success',
    }
    window.localStorage.setItem(
      PAYMENT_RECOVERY_STORAGE_KEY,
      JSON.stringify({
        ...recoverySnapshotFactory('resume-stripe-long'),
        paymentType: 'stripe',
        outTradeNo: 'sub2_stripe_resume_long_42',
      }),
    )
    const pendingStripeOrder = {
      ...orderFactory('PENDING'),
      payment_type: 'stripe',
      out_trade_no: 'sub2_stripe_resume_long_42',
    }
    resolveOrderPublicByResumeToken.mockResolvedValue({
      data: pendingStripeOrder,
    })
    pollOrderStatus.mockResolvedValue(pendingStripeOrder)
    verifyOrder.mockResolvedValue({
      data: pendingStripeOrder,
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledTimes(1)
    expect(verifyOrder).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(298000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledTimes(149)
    expect(resolveOrderPublicByResumeToken.mock.calls.length).toBeGreaterThan(1)
    expect(resolveOrderPublicByResumeToken.mock.calls.length + verifyOrder.mock.calls.length)
      .toBeLessThanOrEqual(20)

    await vi.advanceTimersByTimeAsync(16000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledTimes(151)
    wrapper.unmount()
  })

  it('uses signed provider_key to throttle unauthenticated Stripe-backed wxpay recovery', async () => {
    vi.useFakeTimers()
    routeState.query = {
      resume_token: 'resume-stripe-fallback',
      order_id: '42',
      status: 'success',
    }
    window.localStorage.setItem(
      PAYMENT_RECOVERY_STORAGE_KEY,
      JSON.stringify({
        ...recoverySnapshotFactory('resume-stripe-fallback'),
        paymentType: 'wxpay',
        outTradeNo: 'sub2_stripe_resume_fallback_42',
      }),
    )
    const pendingStripeOrder = {
      ...orderFactory('PENDING'),
      payment_type: 'wxpay',
      provider_key: 'stripe',
      out_trade_no: 'sub2_stripe_resume_fallback_42',
    }
    resolveOrderPublicByResumeToken.mockResolvedValue({
      data: pendingStripeOrder,
    })
    pollOrderStatus.mockRejectedValue(new Error('auth unavailable'))

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(14000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledTimes(7)
    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledTimes(1)
    expect(verifyOrder).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledTimes(8)
    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledTimes(2)
    expect(verifyOrder).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('backs off instead of permanently stopping a non-Stripe pending refresh', async () => {
    vi.useFakeTimers()
    routeState.query = {
      order_id: '42',
      status: 'success',
    }
    let lookupCount = 0
    pollOrderStatus.mockImplementation(async () => {
      lookupCount += 1
      return orderFactory(lookupCount >= 17 ? 'COMPLETED' : 'PENDING')
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(32000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledTimes(16)
    expect(verifyOrder).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.processing')

    await vi.advanceTimersByTimeAsync(8000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledTimes(17)
    expect(wrapper.text()).toContain('payment.result.success')
    wrapper.unmount()
  })

  it('pauses while hidden and refreshes immediately when the result page becomes visible', async () => {
    vi.useFakeTimers()
    routeState.query = {
      order_id: '42',
      status: 'success',
    }
    let visibilityState: DocumentVisibilityState = 'visible'
    vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibilityState)

    const pendingStripeOrder = {
      ...orderFactory('PENDING'),
      payment_type: 'stripe',
      out_trade_no: 'sub2_stripe_visibility_42',
    }
    let upstreamStatus = 'PENDING'
    pollOrderStatus.mockResolvedValue(pendingStripeOrder)
    verifyOrder
      .mockRejectedValueOnce(new Error('temporary network failure'))
      .mockImplementation(async () => ({
        data: {
          ...pendingStripeOrder,
          status: upstreamStatus,
        },
      }))

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()
    expect(wrapper.text()).toContain('payment.result.processing')
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    expect(verifyOrder).toHaveBeenCalledTimes(1)

    visibilityState = 'hidden'
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(360000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    expect(verifyOrder).toHaveBeenCalledTimes(1)

    upstreamStatus = 'COMPLETED'
    visibilityState = 'visible'
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledTimes(2)
    expect(verifyOrder).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('payment.result.success')
    wrapper.unmount()
  })

  it('falls back to public out_trade_no verification when resume_token recovery fails in legacy return flows', async () => {
    routeState.query = {
      resume_token: 'resume-fail',
      out_trade_no: 'legacy-should-not-run',
      trade_status: 'TRADE_SUCCESS',
    }
    resolveOrderPublicByResumeToken.mockRejectedValueOnce(new Error('resume failed'))
    verifyOrderPublic.mockResolvedValueOnce({
      data: {
        ...orderFactory('PAID'),
        out_trade_no: 'legacy-should-not-run',
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledWith('resume-fail')
    expect(verifyOrderPublic).toHaveBeenCalledWith('legacy-should-not-run')
    expect(pollOrderStatus).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.success')
  })

  it('ignores a stale global recovery snapshot when legacy return markers do not identify the order', async () => {
    routeState.query = {
      trade_status: 'TRADE_SUCCESS',
    }
    window.localStorage.setItem(
      PAYMENT_RECOVERY_STORAGE_KEY,
      JSON.stringify(recoverySnapshotFactory('resume-stale')),
    )

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).not.toHaveBeenCalled()
    expect(verifyOrderPublic).not.toHaveBeenCalled()
    expect(pollOrderStatus).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.failed')
    expect(wrapper.text()).not.toContain('sub2_20260420abcd1234')
  })

  it('uses public out_trade_no verification when no signed resume context is available', async () => {
    routeState.query = {
      out_trade_no: 'legacy-123',
      trade_status: 'TRADE_SUCCESS',
    }
    verifyOrder.mockRejectedValue(new Error('auth required'))
    verifyOrderPublic.mockResolvedValue({
      data: orderFactory('PAID'),
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('legacy-123')
    expect(verifyOrderPublic).toHaveBeenCalledWith('legacy-123')
    expect(pollOrderStatus).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.success')
  })

  it('renders the minimal public out_trade_no verification result without payment_type', async () => {
    routeState.query = {
      out_trade_no: 'legacy-minimal',
      trade_status: 'TRADE_SUCCESS',
    }
    verifyOrder.mockRejectedValue(new Error('auth required'))
    verifyOrderPublic.mockResolvedValue({
      data: {
        out_trade_no: 'legacy-minimal',
        status: 'PAID',
        paid: true,
        created_at: '2026-04-20T12:00:00Z',
        expires_at: '2026-04-20T12:30:00Z',
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.text()).toContain('legacy-minimal')
    expect(wrapper.text()).not.toContain('payment.orders.paymentMethod')
  })

  it('prefers authenticated order verification before falling back to public lookup', async () => {
    routeState.query = {
      out_trade_no: 'auth-verify-123',
      trade_status: 'TRADE_SUCCESS',
    }
    verifyOrder.mockResolvedValue({
      data: orderFactory('COMPLETED'),
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('auth-verify-123')
    expect(verifyOrderPublic).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.success')
  })

  it('does not use public out_trade_no verification for bare order numbers without legacy return markers', async () => {
    routeState.query = {
      out_trade_no: 'legacy-bare',
    }

    mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(verifyOrderPublic).not.toHaveBeenCalled()
  })

  it('resolves order by resume token when local recovery snapshot is missing', async () => {
    routeState.query = {
      resume_token: 'resume-77',
    }
    resolveOrderPublicByResumeToken.mockResolvedValue({
      data: orderFactory('PAID'),
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledWith('resume-77')
    expect(wrapper.text()).toContain('payment.result.success')
  })

  it('uses the currency returned by the order API when rendering amounts', async () => {
    routeState.query = {
      resume_token: 'resume-hkd',
    }
    resolveOrderPublicByResumeToken.mockResolvedValue({
      data: {
        ...orderFactory('PAID'),
        currency: 'HKD',
        amount: 100,
        pay_amount: 103,
        fee_rate: 3,
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain(formatPaymentAmount(103, 'HKD'))
  })

  it('normalizes aliased payment methods before rendering the label', async () => {
    routeState.query = {
      resume_token: 'resume-88',
    }
    resolveOrderPublicByResumeToken.mockResolvedValueOnce({
      data: {
        ...orderFactory('PAID'),
        payment_type: 'alipay_direct',
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('payment.methods.alipay')
    expect(wrapper.text()).not.toContain('payment.methods.alipay_direct')
  })
})
