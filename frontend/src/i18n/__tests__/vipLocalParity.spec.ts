import { describe, expect, it } from 'vitest'

import enLocal from '@/i18n/locales/en/local'
import zhLocal from '@/i18n/locales/zh/local'

function keyShape(value: unknown, prefix = ''): string[] {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return [prefix]
  return Object.entries(value as Record<string, unknown>)
    .flatMap(([key, child]) => keyShape(child, prefix ? `${prefix}.${key}` : key))
    .sort()
}

describe('local VIP locale parity', () => {
  it('keeps the Chinese and English VIP key shapes identical', () => {
    expect(keyShape(zhLocal.vip)).toEqual(keyShape(enLocal.vip))
  })

  it('keeps the administrator reconciliation and navigation key shapes identical', () => {
    expect(keyShape(zhLocal.admin.vipReconcile)).toEqual(keyShape(enLocal.admin.vipReconcile))
    expect(zhLocal.nav.vipReconcile).toBeTruthy()
    expect(enLocal.nav.vipReconcile).toBeTruthy()
  })
})
