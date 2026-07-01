import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import PaymentProviderDialog from '@/components/payment/PaymentProviderDialog.vue'
import { STRIPE_SDK_API_VERSION } from '@/components/payment/providerConfig'
import type { ProviderInstance } from '@/types/payment'

const messages: Record<string, string> = {
  'admin.settings.payment.providerConfig': 'Credentials',
  'admin.settings.payment.paymentGuideTrigger': 'View payment guide',
  'admin.settings.payment.alipayGuideSummary': 'Desktop prefers QR precreate and falls back to cashier; mobile prefers WAP checkout.',
  'admin.settings.payment.wxpayGuideSummary': 'Desktop prefers Native QR; mobile routes to JSAPI or H5 based on browser context.',
  'admin.settings.payment.airwallexGuideSummary': 'Use Payment Acceptance read/write only.',
  'admin.settings.payment.stripeWebhookHint': 'Configure Stripe webhook.',
  'admin.settings.payment.stripeWebhookApiVersionHint': 'Use Stripe API version {version}.',
  'admin.settings.payment.airwallexWebhookHint': 'Select payment_intent.succeeded and use the latest stable API version.',
  'admin.settings.payment.wiseWebhookHint': 'Sub2API automatically creates the Wise webhook subscription for this URL.',
  'admin.settings.payment.wiseGuideSummary': 'Wise uses hosted redirect plus automatic reconciliation.',
  'admin.settings.payment.wiseWebhookSubscriptionTitle': 'Wise webhook subscription',
  'admin.settings.payment.wiseWebhookSubscriptionActive': 'Automatically subscribed',
  'admin.settings.payment.wiseWebhookSubscriptionFailed': 'Subscription failed',
  'admin.settings.payment.wiseWebhookSubscriptionUnknown': 'Not synced yet',
  'admin.settings.payment.wiseWebhookSubscriptionId': 'Subscription ID',
  'admin.settings.payment.wiseWebhookSubscriptionError': 'Error',
  'admin.settings.payment.wiseWebhookDeliveryUrl': 'Delivery URL',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, string>) => {
      const message = messages[key] ?? key
      if (!params) return message
      return Object.entries(params).reduce(
        (value, [name, replacement]) => value.replaceAll(`{${name}}`, replacement),
        message,
      )
    },
  }),
}))

function providerFactory(overrides: Partial<ProviderInstance> = {}): ProviderInstance {
  return {
    id: 1,
    provider_key: 'airwallex',
    name: 'Airwallex',
    config: {},
    supported_types: ['airwallex'],
    enabled: true,
    payment_mode: '',
    refund_enabled: false,
    allow_user_refund: false,
    limits: '',
    sort_order: 0,
    ...overrides,
  }
}

function mountDialog(options: { editing?: ProviderInstance | null } = {}) {
  return mount(PaymentProviderDialog, {
    props: {
      show: true,
      saving: false,
      editing: options.editing ?? null,
      allKeyOptions: [
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
        { value: 'stripe', label: 'Stripe' },
        { value: 'airwallex', label: 'Airwallex' },
        { value: 'wise', label: 'Wise' },
      ],
      enabledKeyOptions: [
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
        { value: 'airwallex', label: 'Airwallex' },
        { value: 'wise', label: 'Wise' },
      ],
      allPaymentTypes: [
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
        { value: 'wise', label: 'Wise' },
      ],
      redirectLabel: 'Redirect',
    },
    global: {
      stubs: {
        BaseDialog: {
          template: '<div><slot /><slot name="footer" /></div>',
        },
        Select: {
          props: ['modelValue', 'options', 'disabled'],
          template: '<div />',
        },
        ToggleSwitch: {
          template: '<div />',
        },
      },
    },
  })
}

describe('PaymentProviderDialog payment guide', () => {
  it('shows no payment guide for providers without a flow guide', () => {
    const wrapper = mountDialog()

    expect(wrapper.text()).not.toContain(messages['admin.settings.payment.alipayGuideSummary'])
    expect(wrapper.text()).not.toContain(messages['admin.settings.payment.wxpayGuideSummary'])
    expect(wrapper.find('button[title="View payment guide"]').exists()).toBe(false)
  })

  it.each([
    ['alipay', 'admin.settings.payment.alipayGuideSummary'],
    ['wxpay', 'admin.settings.payment.wxpayGuideSummary'],
    ['airwallex', 'admin.settings.payment.airwallexGuideSummary'],
  ])('shows the payment guide summary for %s', async (providerKey, summaryKey) => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset(providerKey)
    await nextTick()

    expect(wrapper.text()).toContain(messages[summaryKey])
    expect(wrapper.find('button[title="View payment guide"]').exists()).toBe(true)
  })

  it('shows Airwallex webhook event and API version guidance with the webhook URL', async () => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset('airwallex')
    await nextTick()

    expect(wrapper.text()).toContain(messages['admin.settings.payment.airwallexWebhookHint'])
    expect(wrapper.text()).toContain('/api/v1/payment/webhook/airwallex')
  })

  it('shows Stripe webhook API version guidance with the integrated SDK version', async () => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset('stripe')
    await nextTick()

    expect(wrapper.text()).toContain(messages['admin.settings.payment.stripeWebhookHint'])
    expect(wrapper.text()).toContain(`Use Stripe API version ${STRIPE_SDK_API_VERSION}.`)
    expect(wrapper.text()).toContain('/api/v1/payment/webhook/stripe')
  })

  it('emits an empty Airwallex accountId when the admin clears it', async () => {
    const provider = providerFactory({
      config: {
        clientId: 'cid_123',
        apiBase: 'https://api.airwallex.com/api/v1',
        countryCode: 'CN',
        currency: 'CNY',
        accountId: 'acct_123',
      },
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    const accountIdInput = wrapper
      .findAll('input[type="text"]')
      .find(input => (input.element as HTMLInputElement).value === 'acct_123')
    if (!accountIdInput) throw new Error('accountId input not found')

    await accountIdInput.setValue('')
    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.accountId).toBe('')
  })

  it('does not prefill or resubmit sensitive Wise config values when editing', async () => {
    const provider = providerFactory({
      provider_key: 'wise',
      name: 'Wise',
      supported_types: ['wise'],
      config: {
        quickPayBaseUrl: 'https://wise.com/pay/business/account',
        apiBase: 'https://api.wise.com',
        apiToken: 'wise-secret-token',
        profileId: 'profile-123',
        balanceId: 'balance-123',
        currency: 'USD',
        webhookPublicKey: '-----BEGIN PUBLIC KEY-----\npublic\n-----END PUBLIC KEY-----',
        settlementStrategy: 'exact_only',
      },
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    const fieldValues = [
      ...wrapper.findAll('input').map(input => (input.element as HTMLInputElement).value),
      ...wrapper.findAll('textarea').map(textarea => (textarea.element as HTMLTextAreaElement).value),
    ]
    expect(fieldValues).not.toContain('wise-secret-token')
    expect(fieldValues).toContain('profile-123')

    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.apiToken).toBeUndefined()
    expect(payload.config.profileId).toBe('profile-123')
  })

  it.each([
    ['active', 'Automatically subscribed', undefined],
    ['failed', 'Subscription failed', 'remote rejected credentials'],
    ['', 'Not synced yet', undefined],
  ])('shows Wise webhook subscription %s status while editing', async (status, label, error) => {
    const provider = providerFactory({
      provider_key: 'wise',
      name: 'Wise',
      supported_types: ['wise'],
      webhook_subscription_status: status,
      webhook_subscription_id: status ? 'subscription-123' : '',
      webhook_subscription_error: error,
      webhook_delivery_url: status
        ? 'https://api.example.com/api/v1/payment/webhook/wise'
        : '',
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    expect(wrapper.text()).toContain('Wise webhook subscription')
    expect(wrapper.text()).toContain(label)
    expect(wrapper.text()).toContain(
      status
        ? 'https://api.example.com/api/v1/payment/webhook/wise'
        : '/api/v1/payment/webhook/wise',
    )
    expect(wrapper.text()).toContain('Sub2API automatically creates the Wise webhook subscription for this URL.')
    if (error) {
      expect(wrapper.text()).toContain('Error')
      expect(wrapper.text()).toContain(error)
    }
  })
})
