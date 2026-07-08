import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

const adminComplianceKeys = [
  'title',
  'blockingNotice',
  'riskNotice',
  'version',
  'openDocument',
  'documentSource',
  'inputLabel',
  'inputPlaceholder',
  'inputMismatch',
  'legalNote',
  'logout',
  'accept',
  'accepted',
  'acceptFailed',
] as const

const legalKeys = [
  'loadFailed',
  'retryLater',
  'notFound',
  'notFoundDescription',
  'loginAgreement',
  'adminCompliance',
  'updatedAt',
  'empty',
  'loginAgreementPrompt.checkboxPrefix',
  'loginAgreementPrompt.documentSeparator',
  'loginAgreementPrompt.noticeTitle',
  'loginAgreementPrompt.noticeDescription',
  'loginAgreementPrompt.viewTerms',
  'loginAgreementPrompt.dialogTitle',
  'loginAgreementPrompt.dialogDescription',
  'loginAgreementPrompt.recently',
  'loginAgreementPrompt.relatedDocuments',
  'loginAgreementPrompt.reject',
  'loginAgreementPrompt.accept',
  'loginAgreementPrompt.loginRejectedWarning',
  'loginAgreementPrompt.loginRequiredWarning',
  'loginAgreementPrompt.registerRejectedWarning',
  'loginAgreementPrompt.registerRequiredWarning',
] as const

function getMessage(messages: Record<string, any>, path: string): unknown {
  return path.split('.').reduce<unknown>((value, key) => {
    if (!value || typeof value !== 'object') {
      return undefined
    }
    return (value as Record<string, unknown>)[key]
  }, messages)
}

describe('admin compliance locale keys', () => {
  it('contains all dialog labels in zh and en', () => {
    for (const locale of [zh, en]) {
      for (const key of adminComplianceKeys) {
        const value = getMessage(locale, `adminCompliance.${key}`)

        expect(value, `adminCompliance.${key}`).toEqual(expect.any(String))
        expect(value).not.toBe(`adminCompliance.${key}`)
      }
    }
  })

  it('contains legal document labels used by agreement and compliance pages', () => {
    for (const locale of [zh, en]) {
      for (const key of legalKeys) {
        const value = getMessage(locale, `legal.${key}`)

        expect(value, `legal.${key}`).toEqual(expect.any(String))
        expect(value).not.toBe(`legal.${key}`)
      }
    }
  })
})
