import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import StripePaymentInline from '../StripePaymentInline.vue'

const loadStripe = vi.hoisted(() => vi.fn())
const routerResolve = vi.hoisted(() => vi.fn(() => ({ href: '/payment/stripe-popup?mock=1' })))
const paymentElementHandlers = vi.hoisted(() => new Map<string, (event?: any) => void>())
const paymentElement = vi.hoisted(() => ({
  mount: vi.fn(),
  destroy: vi.fn(),
  on: vi.fn((event: string, callback: (event?: any) => void) => {
    paymentElementHandlers.set(event, callback)
    if (event === 'ready') callback()
  }),
  off: vi.fn((event: string) => {
    paymentElementHandlers.delete(event)
  }),
}))
const expressHandlers = vi.hoisted(() => new Map<string, (event: any) => unknown>())
const expressElement = vi.hoisted(() => ({
  mount: vi.fn(),
  destroy: vi.fn(),
  on: vi.fn((event: string, handler: (event: any) => unknown) => {
    expressHandlers.set(event, handler)
    return expressElement
  }),
  off: vi.fn((event: string) => {
    expressHandlers.delete(event)
    return expressElement
  }),
}))
const elements = vi.hoisted(() => ({
  create: vi.fn((type: string) => (
    type === 'expressCheckout' ? expressElement : paymentElement
  )),
}))
const stripe = vi.hoisted(() => ({
  elements: vi.fn(() => elements),
  confirmPayment: vi.fn(),
}))

vi.mock('@stripe/stripe-js', () => ({ loadStripe }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('vue-router', () => ({
  useRouter: () => ({ resolve: routerResolve, push: vi.fn() }),
}))
vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: vi.fn() }),
}))
vi.mock('@/api/payment', () => ({
  paymentAPI: { cancelOrder: vi.fn() },
}))

function mountInline() {
  return mount(StripePaymentInline, {
    props: {
      orderId: 42,
      amount: 100,
      clientSecret: 'pi_secret_42',
      publishableKey: 'pk_test',
      googlePayEnabled: false,
      payAmount: 103,
      currency: 'USD',
    },
    global: { stubs: { Icon: true } },
  })
}

describe('StripePaymentInline Google Pay integration', () => {
  beforeEach(() => {
    expressHandlers.clear()
    paymentElementHandlers.clear()
    vi.clearAllMocks()
    loadStripe.mockResolvedValue(stripe)
    stripe.confirmPayment.mockResolvedValue({})
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('creates Payment and Express Checkout Elements from one Elements instance', async () => {
    const wrapper = mountInline()
    await flushPromises()
    await nextTick()

    expect(stripe.elements).toHaveBeenCalledTimes(1)
    expect(elements.create).toHaveBeenCalledWith('payment', expect.any(Object))
    expect(elements.create).toHaveBeenCalledWith('expressCheckout', expect.objectContaining({
      paymentMethods: expect.objectContaining({ googlePay: 'auto' }),
    }))
    expect(paymentElement.mount).toHaveBeenCalledOnce()
    expect(expressElement.mount).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('shares the submitting lock and handles a real child confirmation', async () => {
    let resolveConfirmation!: (value: object) => void
    stripe.confirmPayment.mockReturnValue(new Promise(resolve => {
      resolveConfirmation = resolve
    }))
    const wrapper = mountInline()
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
    expect(wrapper.emitted('confirmed')).toEqual([[]])
    expect(wrapper.text()).not.toContain('payment.result.success')
    wrapper.unmount()
  })

  it('does not let Google Pay confirm while the Payment Element owns the lock', async () => {
    let resolveCardConfirmation!: (value: object) => void
    stripe.confirmPayment.mockReturnValue(new Promise(resolve => {
      resolveCardConfirmation = resolve
    }))
    const wrapper = mountInline()
    await flushPromises()

    const cardPromise = wrapper.get('button.btn-stripe').trigger('click')
    await nextTick()
    const paymentFailed = vi.fn()
    await expressHandlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed,
    })

    expect(stripe.confirmPayment).toHaveBeenCalledTimes(1)
    expect(paymentFailed).toHaveBeenCalledWith({
      reason: 'fail',
      message: 'common.processing',
    })

    resolveCardConfirmation({})
    await cardPromise
    await flushPromises()
    wrapper.unmount()
  })

  it('destroys the Payment Element and removes its listeners on unmount', async () => {
    const wrapper = mountInline()
    await flushPromises()

    wrapper.unmount()

    expect(paymentElement.off).toHaveBeenCalledWith('ready', expect.any(Function))
    expect(paymentElement.off).toHaveBeenCalledWith('change', expect.any(Function))
    expect(paymentElement.destroy).toHaveBeenCalledOnce()
  })

  it('removes a pending popup ready listener on unmount', async () => {
    const popup = { closed: false, postMessage: vi.fn() } as unknown as Window
    vi.spyOn(window, 'open').mockReturnValue(popup)
    const removeEventListener = vi.spyOn(window, 'removeEventListener')
    const wrapper = mountInline()
    await flushPromises()

    paymentElementHandlers.get('change')?.({ value: { type: 'alipay' } })
    await nextTick()
    await wrapper.get('button.btn-stripe').trigger('click')
    wrapper.unmount()

    expect(removeEventListener).toHaveBeenCalledWith('message', expect.any(Function))
  })
})
