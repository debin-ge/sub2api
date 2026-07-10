import { beforeEach, describe, expect, it, vi } from 'vitest'
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

function mountComponent(disabled = false) {
  return mount(StripeGooglePayExpress, {
    attachTo: document.body,
    props: {
      stripe,
      elements,
      returnUrl: 'https://example.com/payment/result?order_id=42',
      disabled,
    },
  })
}

describe('StripeGooglePayExpress', () => {
  beforeEach(() => {
    handlers.clear()
    vi.clearAllMocks()
    confirmPayment.mockResolvedValue({})
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

  it('shows only when Google Pay becomes available', async () => {
    const wrapper = mountComponent()

    expect(wrapper.get('[data-testid="stripe-google-pay-express"]').isVisible()).toBe(false)
    handlers.get('availablepaymentmethodschange')?.({
      elementType: 'expressCheckout',
      paymentMethods: { googlePay: { available: true } },
    })
    await nextTick()

    expect(wrapper.get('[data-testid="stripe-google-pay-express"]').isVisible()).toBe(true)
    expect(wrapper.emitted('availabilityChange')).toEqual([[true]])
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

  it('clears submitting state on cancel and destroys the Stripe Element', () => {
    const wrapper = mountComponent()

    handlers.get('cancel')?.({ elementType: 'expressCheckout' })
    expect(wrapper.emitted('submittingChange')).toEqual([[false]])

    wrapper.unmount()
    expect(expressElement.destroy).toHaveBeenCalledOnce()
  })

  it('hides a failed Element and records only sanitized diagnostics', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    const wrapper = mountComponent()

    handlers.get('availablepaymentmethodschange')?.({
      elementType: 'expressCheckout',
      paymentMethods: { googlePay: { available: true } },
    })
    handlers.get('loaderror')?.({
      elementType: 'expressCheckout',
      error: { type: 'invalid_request_error', code: 'payment_method_domain_invalid' },
    })
    await nextTick()

    expect(wrapper.get('[data-testid="stripe-google-pay-express"]').isVisible()).toBe(false)
    expect(warn).toHaveBeenCalledWith('[StripeGooglePayExpress] load failed', {
      type: 'invalid_request_error',
      code: 'payment_method_domain_invalid',
    })
    warn.mockRestore()
  })
})
