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

function mountInline(googlePayEnabled = true) {
  return mount(StripePaymentInline, {
    props: {
      orderId: 42,
      amount: 100,
      clientSecret: 'pi_secret_42',
      publishableKey: 'pk_test',
      googlePayEnabled,
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
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('does not create or render Express Checkout or its divider when disabled', async () => {
    const wrapper = mountInline(false)
    await flushPromises()
    await nextTick()

    expect(elements.create).toHaveBeenCalledWith('payment', expect.any(Object))
    expect(elements.create).not.toHaveBeenCalledWith('expressCheckout', expect.anything())
    expect(wrapper.find('[data-testid="stripe-google-pay-state"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stripe-google-pay-divider"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('creates Payment and the real Express Checkout child from one Elements instance when enabled', async () => {
    const wrapper = mountInline()
    await flushPromises()
    await nextTick()

    expect(stripe.elements).toHaveBeenCalledTimes(1)
    expect(elements.create).toHaveBeenCalledWith('payment', expect.any(Object))
    expect(elements.create).toHaveBeenCalledWith('expressCheckout', {
      paymentMethods: {
        googlePay: 'auto',
        applePay: 'never',
        link: 'never',
        amazonPay: 'never',
        paypal: 'never',
        klarna: 'never',
      },
    })
    expect(paymentElement.mount).toHaveBeenCalledOnce()
    expect(expressElement.mount).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="stripe-google-pay-state"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="stripe-google-pay-divider"]').exists()).toBe(true)
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
    await wrapper.get('button.btn-stripe').trigger('click')
    expect(stripe.confirmPayment).toHaveBeenCalledTimes(1)

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

  it('keeps Google Pay locked while a popup method initializes through the real child', async () => {
    const popup = { closed: false, postMessage: vi.fn() } as unknown as Window
    vi.spyOn(window, 'open').mockReturnValue(popup)
    const wrapper = mountInline()
    await flushPromises()

    paymentElementHandlers.get('change')?.({ value: { type: 'alipay' } })
    await nextTick()
    await wrapper.get('button.btn-stripe').trigger('click')
    await nextTick()
    window.dispatchEvent(new MessageEvent('message', {
      data: { type: 'STRIPE_POPUP_READY' },
      source: popup,
    }))
    await nextTick()

    const paymentFailed = vi.fn()
    await expressHandlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed,
    })

    expect(popup.postMessage).toHaveBeenCalledWith({
      type: 'STRIPE_POPUP_INIT',
      clientSecret: 'pi_secret_42',
      publishableKey: 'pk_test',
    }, window.location.origin)
    expect(stripe.confirmPayment).not.toHaveBeenCalled()
    expect(paymentFailed).toHaveBeenCalledWith({
      reason: 'fail',
      message: 'common.processing',
    })
    wrapper.unmount()
  })

  it('releases the popup lock immediately when the browser blocks window.open', async () => {
    vi.spyOn(window, 'open').mockReturnValue(null)
    const addEventListener = vi.spyOn(window, 'addEventListener')
    const wrapper = mountInline()
    await flushPromises()

    paymentElementHandlers.get('change')?.({ value: { type: 'wechat_pay' } })
    await nextTick()
    await wrapper.get('button.btn-stripe').trigger('click')
    await nextTick()

    expect(wrapper.get('button.btn-stripe').attributes('disabled')).toBeUndefined()
    expect(addEventListener).not.toHaveBeenCalledWith('message', expect.any(Function))

    const paymentFailed = vi.fn()
    await expressHandlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed,
    })
    await flushPromises()
    expect(stripe.confirmPayment).toHaveBeenCalledOnce()
    expect(paymentFailed).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('releases the popup lock when the popup closes before confirmation', async () => {
    vi.useFakeTimers()
    const popup = { closed: false, postMessage: vi.fn() }
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    const wrapper = mountInline()
    await flushPromises()

    paymentElementHandlers.get('change')?.({ value: { type: 'alipay' } })
    await nextTick()
    await wrapper.get('button.btn-stripe').trigger('click')
    await nextTick()
    expect(wrapper.get('button.btn-stripe').attributes('disabled')).toBeDefined()

    popup.closed = true
    await vi.advanceTimersByTimeAsync(1000)
    await nextTick()
    expect(wrapper.get('button.btn-stripe').attributes('disabled')).toBeUndefined()

    const paymentFailed = vi.fn()
    await expressHandlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed,
    })
    await flushPromises()
    expect(stripe.confirmPayment).toHaveBeenCalledOnce()
    expect(paymentFailed).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('removes listeners before destroying both Stripe Elements on unmount', async () => {
    const wrapper = mountInline()
    await flushPromises()

    wrapper.unmount()

    expect(paymentElement.off).toHaveBeenCalledWith('ready', expect.any(Function))
    expect(paymentElement.off).toHaveBeenCalledWith('change', expect.any(Function))
    expect(Math.max(...paymentElement.off.mock.invocationCallOrder)).toBeLessThan(
      paymentElement.destroy.mock.invocationCallOrder[0],
    )
    expect(paymentElement.destroy).toHaveBeenCalledOnce()
    expect(expressElement.off).toHaveBeenCalledWith('ready', expect.any(Function))
    expect(expressElement.off).toHaveBeenCalledWith('availablepaymentmethodschange', expect.any(Function))
    expect(expressElement.off).toHaveBeenCalledWith('confirm', expect.any(Function))
    expect(expressElement.off).toHaveBeenCalledWith('cancel', expect.any(Function))
    expect(expressElement.off).toHaveBeenCalledWith('loaderror', expect.any(Function))
    expect(Math.max(...expressElement.off.mock.invocationCallOrder)).toBeLessThan(
      expressElement.destroy.mock.invocationCallOrder[0],
    )
    expect(expressElement.destroy).toHaveBeenCalledOnce()
  })

  it('removes a pending popup ready listener and close timer on unmount', async () => {
    const popup = { closed: false, postMessage: vi.fn() } as unknown as Window
    vi.spyOn(window, 'open').mockReturnValue(popup)
    const removeEventListener = vi.spyOn(window, 'removeEventListener')
    const clearInterval = vi.spyOn(window, 'clearInterval')
    const wrapper = mountInline()
    await flushPromises()

    paymentElementHandlers.get('change')?.({ value: { type: 'alipay' } })
    await nextTick()
    await wrapper.get('button.btn-stripe').trigger('click')
    wrapper.unmount()

    expect(removeEventListener).toHaveBeenCalledWith('message', expect.any(Function))
    expect(clearInterval).toHaveBeenCalledOnce()
  })
})
