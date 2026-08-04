import { describe, expect, it } from 'vitest'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

// 三个 key 都是新加的，且只在模板里以字符串路径引用（t('admin.accounts.xxx')），
// TS 类型检查覆盖不到。这里钉住它们确实挂在 admin.accounts 下且两种语言都有，
// 避免国产网关透传开关渲染成裸 key。
const KEYS = [
  'providerPassthrough',
  'providerPassthroughDesc',
  'modelRestrictionDisabledByProviderPassthrough'
] as const

describe('provider passthrough locale keys', () => {
  it.each([
    ['zh', zh],
    ['en', en]
  ])('%s exposes the admin.accounts keys', (_name, bundle) => {
    const accounts = (bundle as Record<string, any>).admin.accounts as Record<string, unknown>
    for (const key of KEYS) {
      expect(typeof accounts[key]).toBe('string')
      expect((accounts[key] as string).length).toBeGreaterThan(0)
    }
  })
})
