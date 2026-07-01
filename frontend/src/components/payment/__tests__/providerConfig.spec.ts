import { describe, expect, it } from 'vitest'
import {
  PAYMENT_CURRENCY_OPTIONS,
  PROVIDER_CONFIG_FIELDS,
  PROVIDER_SUPPORTED_TYPES,
  WEBHOOK_PATHS,
} from '@/components/payment/providerConfig'

function findField(providerKey: string, key: string) {
  const fields = PROVIDER_CONFIG_FIELDS[providerKey] || []
  return fields.find(field => field.key === key)
}

describe('PROVIDER_CONFIG_FIELDS.wxpay', () => {
  it('keeps admin form validation aligned with backend-required credentials', () => {
    expect(findField('wxpay', 'publicKeyId')?.optional).toBeFalsy()
    expect(findField('wxpay', 'certSerial')?.optional).toBeFalsy()
  })

  it('only keeps the simplified visible credential set in the admin form', () => {
    expect(findField('wxpay', 'mpAppId')).toBeUndefined()
    expect(findField('wxpay', 'h5AppName')).toBeUndefined()
    expect(findField('wxpay', 'h5AppUrl')).toBeUndefined()
  })
})

describe('PROVIDER_CONFIG_FIELDS.airwallex', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('airwallex', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })

  it('marks accountId as optional and explains when it can be left blank', () => {
    const accountId = findField('airwallex', 'accountId')

    expect(accountId?.optional).toBe(true)
    expect(accountId?.clearable).toBe(true)
    expect(accountId?.hintKey).toBe('admin.settings.payment.field_accountIdHint')
  })

  it('explains that apiBase must match the Airwallex key environment', () => {
    expect(findField('airwallex', 'apiBase')?.hintKey).toBe('admin.settings.payment.field_airwallexApiBaseHint')
  })
})

describe('PROVIDER_CONFIG_FIELDS.wise', () => {
  it('declares Wise supported type and callback path', () => {
    expect(PROVIDER_SUPPORTED_TYPES.wise).toEqual(['wise'])
    expect(WEBHOOK_PATHS.wise).toBe('/api/v1/payment/webhook/wise')
  })

  it('marks apiToken sensitive and settlement exact-only fields visible', () => {
    const fields = PROVIDER_CONFIG_FIELDS.wise
    expect(fields.find(field => field.key === 'apiToken')?.sensitive).toBe(true)
    expect(fields.find(field => field.key === 'quickPayBaseUrl')?.sensitive).toBe(false)
    expect(fields.find(field => field.key === 'settlementStrategy')?.defaultValue).toBe('exact_only')
    expect(fields.find(field => field.key === 'allowedMethodsNote')?.optional).toBe(true)
    expect(fields.find(field => field.key === 'reconcileWindowHours')?.defaultValue).toBe('72')
    expect(fields.find(field => field.key === 'autoFulfillFeePayments')?.defaultValue).toBe('false')
  })

  it('adds Wise environment and keeps webhook public key as an optional override', () => {
    const environment = findField('wise', 'environment')
    expect(environment?.defaultValue).toBe('production')
    expect(environment?.options?.map(option => option.value)).toEqual(['production', 'sandbox'])

    const webhookPublicKey = findField('wise', 'webhookPublicKey')
    expect(webhookPublicKey?.sensitive).toBe(false)
    expect(webhookPublicKey?.optional).toBe(true)
    expect(webhookPublicKey?.clearable).toBe(true)
    expect(webhookPublicKey?.hintKey).toBe('admin.settings.payment.field_wiseWebhookPublicKeyHint')
  })
})

describe('PROVIDER_CONFIG_FIELDS.stripe', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('stripe', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })
})
