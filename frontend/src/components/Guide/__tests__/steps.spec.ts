import { describe, expect, it } from 'vitest'

import { getAdminSteps, getUserSteps } from '../steps'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

type LocaleMessages = Record<string, unknown>
type TranslateParams = Record<string, string | number>

function lookup(messages: LocaleMessages, key: string): string {
  const value = key.split('.').reduce<unknown>((current, part) => {
    if (current && typeof current === 'object' && part in current) {
      return (current as Record<string, unknown>)[part]
    }
    return undefined
  }, messages)

  return typeof value === 'string' ? value : key
}

function createTranslator(messages: LocaleMessages) {
  return (key: string, params: TranslateParams = {}) => {
    return lookup(messages, key).replace(/\{(\w+)\}/g, (_match, name: string) => {
      const value = params[name]
      return value === undefined ? `{${name}}` : String(value)
    })
  }
}

describe('onboarding guide branding', () => {
  it('uses the configured site name in admin welcome text', () => {
    const siteName = 'Acme Gateway'
    const steps = getAdminSteps(createTranslator(zh), false, { siteName })

    expect(steps[0].popover?.title).toContain(siteName)
    expect(steps[0].popover?.description).toContain(siteName)
    expect(steps[0].popover?.title).not.toContain('Sub2API')
    expect(steps[0].popover?.description).not.toContain('Sub2API')
  })

  it('uses the configured site name in user welcome text', () => {
    const siteName = 'Acme Gateway'
    const steps = getUserSteps(createTranslator(en), { siteName })

    expect(steps[0].popover?.title).toContain(siteName)
    expect(steps[0].popover?.description).toContain(siteName)
    expect(steps[0].popover?.title).not.toContain('Sub2API')
    expect(steps[0].popover?.description).not.toContain('Sub2API')
  })

  it('does not render the legacy product name when the app store still has its fallback', () => {
    const steps = getAdminSteps(createTranslator(zh), false, { siteName: 'Sub2API' })

    expect(steps[0].popover?.title).not.toContain('Sub2API')
    expect(steps[0].popover?.description).not.toContain('Sub2API')
  })
})
