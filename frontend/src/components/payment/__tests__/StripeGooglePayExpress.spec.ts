import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import type { Stripe, StripeElements } from '@stripe/stripe-js'
import StripeGooglePayExpress from '../StripeGooglePayExpress.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

type Handler = (event: any) => unknown

const handlers = new Map<string, Handler>()
const expressElement = {
  mount: vi.fn(),
  destroy: vi.fn(),
  on: vi.fn((event: string, handler: Handler) => {
    handlers.set(event, handler)
    return expressElement
  }),
  off: vi.fn((event: string) => {
    handlers.delete(event)
    return expressElement
  }),
}
const elements = {
  create: vi.fn(() => expressElement),
} as unknown as StripeElements
const confirmPayment = vi.fn()
const stripe = { confirmPayment } as unknown as Stripe
const mountedWrappers: Array<{ exists: () => boolean; unmount: () => void }> = []

function mountComponent(disabled = false) {
  const wrapper = mount(StripeGooglePayExpress, {
    attachTo: document.body,
    props: {
      stripe,
      elements,
      returnUrl: 'https://example.com/payment/result?order_id=42',
      disabled,
    },
  })
  mountedWrappers.push(wrapper)
  return wrapper
}

describe('StripeGooglePayExpress', () => {
  beforeEach(() => {
    handlers.clear()
    vi.clearAllMocks()
    confirmPayment.mockResolvedValue({})
  })

  afterEach(() => {
    mountedWrappers.forEach((wrapper) => {
      if (wrapper.exists()) wrapper.unmount()
    })
    mountedWrappers.length = 0
    vi.restoreAllMocks()
  })

  it('creates an Express Checkout Element that only allows Google Pay', () => {
    mountComponent()

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
    expect(expressElement.mount).toHaveBeenCalledOnce()
  })

  it('shows a disabled checking placeholder while Stripe determines availability', () => {
    const wrapper = mountComponent()

    expect(wrapper.get('[data-testid="stripe-google-pay-state"]').text())
      .toContain('payment.googlePayChecking')
    expect(wrapper.get('[data-testid="stripe-google-pay-placeholder"]').attributes('disabled'))
      .toBeDefined()
    expect(wrapper.get('[data-testid="stripe-google-pay-mount"]').isVisible()).toBe(false)
  })

  it('shows the real Stripe mount only when ready reports Google Pay available', async () => {
    const wrapper = mountComponent()

    handlers.get('ready')?.({
      elementType: 'expressCheckout',
      availablePaymentMethods: { googlePay: true },
    })
    await nextTick()

    expect(wrapper.get('[data-testid="stripe-google-pay-mount"]').isVisible()).toBe(true)
    expect(wrapper.find('[data-testid="stripe-google-pay-placeholder"]').exists()).toBe(false)
    expect(wrapper.emitted('availabilityChange')).toEqual([[true]])
  })

  it('shows the disabled unavailable state when Google Pay availability is lost', async () => {
    const wrapper = mountComponent()

    handlers.get('ready')?.({
      elementType: 'expressCheckout',
      availablePaymentMethods: { googlePay: true },
    })
    handlers.get('availablepaymentmethodschange')?.({
      elementType: 'expressCheckout',
      paymentMethods: { googlePay: { available: false } },
    })
    await nextTick()

    expect(wrapper.get('[data-testid="stripe-google-pay-state"]').text())
      .toContain('payment.googlePayUnavailable')
    expect(wrapper.get('[data-testid="stripe-google-pay-placeholder"]').attributes('disabled'))
      .toBeDefined()
    expect(wrapper.get('[data-testid="stripe-google-pay-mount"]').isVisible()).toBe(false)
    expect(wrapper.emitted('availabilityChange')).toEqual([[true], [false]])
  })

  it('confirms the existing PaymentIntent and shares submitting state', async () => {
    const wrapper = mountComponent()
    const paymentFailed = vi.fn()

    await handlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed,
    })
    await flushPromises()

    expect(confirmPayment).toHaveBeenCalledWith({
      elements,
      confirmParams: {
        return_url: 'https://example.com/payment/result?order_id=42',
      },
      redirect: 'if_required',
    })
    expect(paymentFailed).not.toHaveBeenCalled()
    expect(wrapper.emitted('submittingChange')).toEqual([[true], [false]])
    expect(wrapper.emitted('confirmed')).toEqual([[]])
  })

  it('reports an immediate Stripe error without losing the fallback form', async () => {
    confirmPayment.mockResolvedValue({ error: { message: 'wallet declined' } })
    const wrapper = mountComponent()
    const paymentFailed = vi.fn()

    await handlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed,
    })
    await flushPromises()

    expect(wrapper.text()).toContain('wallet declined')
    expect(paymentFailed).toHaveBeenCalledWith({
      reason: 'fail',
      message: 'wallet declined',
    })
    expect(wrapper.emitted('confirmed')).toBeUndefined()
    expect(wrapper.emitted('submittingChange')).toEqual([[true], [false]])
  })

  it('does not release an externally owned submitting lock on cancel and destroys the Stripe Element', () => {
    const wrapper = mountComponent(true)

    handlers.get('cancel')?.({ elementType: 'expressCheckout' })
    expect(wrapper.emitted('submittingChange')).toBeUndefined()
    expect(confirmPayment).not.toHaveBeenCalled()

    wrapper.unmount()
    expect(expressElement.off).toHaveBeenNthCalledWith(1, 'ready', expect.any(Function))
    expect(expressElement.off).toHaveBeenNthCalledWith(2, 'availablepaymentmethodschange', expect.any(Function))
    expect(expressElement.off).toHaveBeenNthCalledWith(3, 'confirm', expect.any(Function))
    expect(expressElement.off).toHaveBeenNthCalledWith(4, 'cancel', expect.any(Function))
    expect(expressElement.off).toHaveBeenNthCalledWith(5, 'loaderror', expect.any(Function))
    expect(Math.max(...expressElement.off.mock.invocationCallOrder)).toBeLessThan(
      expressElement.destroy.mock.invocationCallOrder[0],
    )
    expect(expressElement.destroy).toHaveBeenCalledOnce()
  })

  it('shows the disabled error state without releasing an external lock and records sanitized diagnostics', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    const wrapper = mountComponent(true)

    handlers.get('availablepaymentmethodschange')?.({
      elementType: 'expressCheckout',
      paymentMethods: { googlePay: { available: true } },
    })
    handlers.get('loaderror')?.({
      elementType: 'expressCheckout',
      error: {
        type: 'invalid_request_error',
        code: 'payment_method_domain_invalid',
        payment_method: { card: 'must-not-log' },
      },
    })
    await nextTick()

    expect(wrapper.get('[data-testid="stripe-google-pay-state"]').text())
      .toContain('payment.googlePayUnavailable')
    expect(wrapper.get('[data-testid="stripe-google-pay-placeholder"]').attributes('disabled'))
      .toBeDefined()
    expect(wrapper.get('[data-testid="stripe-google-pay-mount"]').isVisible()).toBe(false)
    expect(warn).toHaveBeenCalledWith('[StripeGooglePayExpress] load failed', {
      type: 'invalid_request_error',
      code: 'payment_method_domain_invalid',
    })
    expect(wrapper.emitted('submittingChange')).toBeUndefined()
    warn.mockRestore()
  })
})
