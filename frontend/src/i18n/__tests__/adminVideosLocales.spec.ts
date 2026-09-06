import { describe, expect, it } from 'vitest'

import en from '../locales/en/admin/videos'
import zh from '../locales/zh/admin/videos'

function keys(value: unknown, prefix = ''): string[] {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return [prefix]
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) => keys(child, prefix ? `${prefix}.${key}` : key))
}

describe('admin video locale parity', () => {
  it('keeps English and Chinese key shapes identical', () => {
    expect(keys(en).sort()).toEqual(keys(zh).sort())
  })
})
