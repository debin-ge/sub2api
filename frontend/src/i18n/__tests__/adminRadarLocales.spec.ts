import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

type MessageTree = Record<string, unknown>

function adminRadar(locale: MessageTree): MessageTree {
  return (((locale.admin as MessageTree).settings as MessageTree).features as MessageTree)
    .radar as MessageTree
}

function flatten(tree: MessageTree, prefix = ''): Map<string, string> {
  const result = new Map<string, string>()
  for (const [key, value] of Object.entries(tree)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (typeof value === 'string') {
      result.set(path, value)
    } else {
      for (const [nestedKey, nestedValue] of flatten(value as MessageTree, path)) {
        result.set(nestedKey, nestedValue)
      }
    }
  }
  return result
}

describe('admin Radar locale contract', () => {
  it('keeps English and Simplified Chinese keys and placeholders in parity', () => {
    const enMessages = flatten(adminRadar(en as MessageTree))
    const zhMessages = flatten(adminRadar(zh as MessageTree))

    expect([...enMessages.keys()].sort()).toEqual([...zhMessages.keys()].sort())
    for (const [key, enValue] of enMessages) {
      expect(enValue.trim(), `en ${key}`).not.toBe('')
      expect(zhMessages.get(key)?.trim(), `zh ${key}`).not.toBe('')
      const placeholders = (value: string) =>
        [...value.matchAll(/\{([A-Za-z_][A-Za-z0-9_]*)\}/g)].map((match) => match[1]).sort()
      expect(placeholders(enValue), `placeholder mismatch at ${key}`).toEqual(
        placeholders(zhMessages.get(key) ?? ''),
      )
    }
  })

  it('covers every backend state and safe error code used dynamically by the card', () => {
    for (const locale of [en, zh] as MessageTree[]) {
      const messages = flatten(adminRadar(locale))
      for (const key of [
        'status.healthy',
        'status.failed',
        'status.never_attempted',
        'errors.network_error',
        'errors.unauthorized',
        'errors.rate_limited',
        'errors.invalid_response',
        'errors.upstream_error',
        'errors.aggregation_error',
        'sources.aa',
        'sources.lmarena',
        'sources.status_claude',
        'sources.status_openai',
        'sources.quota_aggregator',
        'refresh.triggered',
        'refresh.coalesced',
      ]) {
        expect(messages.get(key), key).toEqual(expect.any(String))
      }
    }
  })
})
