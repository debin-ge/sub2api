import { readdirSync, readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

type MessageTree = Record<string, unknown>

const dynamicRadarKeys = [
  // Status and error codes are selected at runtime, so source scanning alone
  // cannot prove that every backend variant has a translation.
  'health.status.operational',
  'health.status.degraded',
  'health.status.partialOutage',
  'health.status.majorOutage',
  'health.status.maintenance',
  'health.status.unknown',
  'quota.inference.insufficient',
  'quota.inference.dispersed',
  'quota.inference.invalid',
  'quota.inference.unavailable',
  'sources.healthy',
  'sources.failed',
  'sources.notConfigured',
  'sources.neverAttempted',
  'sources.stale',
  'sources.error.network',
  'sources.error.unauthorized',
  'sources.error.rateLimited',
  'sources.error.invalidResponse',
  'sources.error.upstream',
  'sources.error.generic',
] as const

const semanticStateKeys = [
  'quota.inference.insufficient',
  'quota.inference.dispersed',
  'quota.inference.invalid',
  'quota.inference.unavailable',
  'quota.noWindow',
  'common.stale',
] as const

function radarNamespace(locale: MessageTree, name: string): MessageTree {
  const radar = locale.radar
  expect(radar, `${name}.radar must be a nested message namespace`).toBeTruthy()
  expect(typeof radar, `${name}.radar must be an object`).toBe('object')
  expect(Array.isArray(radar), `${name}.radar must not be an array`).toBe(false)
  return radar as MessageTree
}

function flattenMessages(tree: MessageTree, prefix = ''): Map<string, string> {
  const leaves = new Map<string, string>()

  for (const [key, value] of Object.entries(tree)) {
    expect(key, `radar namespace key ${prefix}${key} must be genuinely nested`).not.toContain('.')
    const path = prefix ? `${prefix}.${key}` : key

    if (typeof value === 'string') {
      expect(value.trim(), `radar.${path} must not be blank`).not.toBe('')
      leaves.set(path, value)
      continue
    }

    expect(value, `radar.${path} must be a string or nested object`).toBeTruthy()
    expect(typeof value, `radar.${path} must be a string or nested object`).toBe('object')
    expect(Array.isArray(value), `radar.${path} must not be an array`).toBe(false)
    for (const [nestedPath, nestedValue] of flattenMessages(value as MessageTree, path)) {
      leaves.set(nestedPath, nestedValue)
    }
  }

  return leaves
}

function placeholders(message: string): string[] {
  return [...new Set(
    [...message.matchAll(/\{\s*([A-Za-z_][A-Za-z0-9_]*)\b[^}]*\}/g)]
      .map((match) => match[1]),
  )].sort()
}

function valueAt(messages: Map<string, string>, key: string): string | undefined {
  return messages.get(key)
}

function collectStaticRadarKeys(): Map<string, string[]> {
  const sourceRoot = join(process.cwd(), 'src')
  const componentsRoot = join(sourceRoot, 'components', 'radar')
  const files = [
    ...readdirSync(componentsRoot)
      .filter((name) => name.endsWith('.vue'))
      .map((name) => join(componentsRoot, name)),
    join(sourceRoot, 'views', 'public', 'RadarHomeView.vue'),
    join(sourceRoot, 'router', 'index.ts'),
    join(sourceRoot, 'router', 'title.ts'),
  ]
  const patterns = [
    /\bt\(\s*(['"`])(radar\.[A-Za-z0-9_.]+)\1/g,
    /\btitleKey\s*:\s*(['"`])(radar\.[A-Za-z0-9_.]+)\1/g,
  ]
  const keys = new Map<string, string[]>()

  for (const file of files) {
    const source = readFileSync(file, 'utf8')
    for (const pattern of patterns) {
      let match: RegExpExecArray | null
      while ((match = pattern.exec(source))) {
        const line = source.slice(0, match.index).split('\n').length
        const reference = `${relative(process.cwd(), file)}:${line}`
        keys.set(match[2], [...(keys.get(match[2]) ?? []), reference])
      }
    }
  }

  return keys
}

describe('Model Radar locale contract', () => {
  function flattenedRadars(): { enRadar: Map<string, string>; zhRadar: Map<string, string> } {
    return {
      enRadar: flattenMessages(radarNamespace(en as MessageTree, 'en')),
      zhRadar: flattenMessages(radarNamespace(zh as MessageTree, 'zh')),
    }
  }

  it('keeps every English and Simplified Chinese leaf key in parity', () => {
    const { enRadar, zhRadar } = flattenedRadars()
    expect([...enRadar.keys()].sort()).toEqual([...zhRadar.keys()].sort())
  })

  it('keeps interpolation placeholder sets in parity for every leaf', () => {
    const { enRadar, zhRadar } = flattenedRadars()
    expect(placeholders('{count} records as of {date}; total {count}')).toEqual(['count', 'date'])
    for (const [key, enMessage] of enRadar) {
      expect(placeholders(enMessage), `placeholder mismatch at radar.${key}`).toEqual(
        placeholders(zhRadar.get(key) ?? ''),
      )
    }
  })

  it('defines every literal radar key used by components, the view, and the route', () => {
    const { enRadar, zhRadar } = flattenedRadars()
    const staticKeys = collectStaticRadarKeys()
    const missing: string[] = []

    expect(staticKeys.has('radar.hero.title')).toBe(true)
    expect(staticKeys.has('radar.pageTitle')).toBe(true)

    for (const [fullKey, references] of staticKeys) {
      const key = fullKey.replace(/^radar\./, '')
      for (const [locale, messages] of [['en', enRadar], ['zh', zhRadar]] as const) {
        if (!messages.has(key)) {
          missing.push(`${locale}.${fullKey} (${references.join(', ')})`)
        }
      }
    }

    expect(missing).toEqual([])
  })

  it('explicitly covers runtime status, rejection-reason, and source-error variants', () => {
    const { enRadar, zhRadar } = flattenedRadars()
    for (const key of dynamicRadarKeys) {
      expect(valueAt(enRadar, key), `en.radar.${key}`).toEqual(expect.any(String))
      expect(valueAt(zhRadar, key), `zh.radar.${key}`).toEqual(expect.any(String))
    }
  })

  it('does not collapse distinct inference, missing-window, and stale states', () => {
    const { enRadar, zhRadar } = flattenedRadars()
    for (const messages of [enRadar, zhRadar]) {
      const labels = semanticStateKeys.map((key) => valueAt(messages, key))
      expect(labels.every((label) => typeof label === 'string' && label.length > 0)).toBe(true)
      expect(new Set(labels).size).toBe(semanticStateKeys.length)
    }
  })

  it('defines the nested namespace before patching and never patches radar through missingMessages', () => {
    for (const locale of ['en', 'zh']) {
      const path = join(process.cwd(), 'src', 'i18n', 'locales', `${locale}.ts`)
      const source = readFileSync(path, 'utf8')
      const messagesStart = source.indexOf('const messages = {')
      const missingStart = source.indexOf('const missingMessages:')
      const patcherStart = source.indexOf('function applyMissingLocaleMessages', missingStart)

      expect(messagesStart, `${locale}: messages object must exist`).toBeGreaterThanOrEqual(0)
      expect(missingStart, `${locale}: missingMessages block must exist`).toBeGreaterThan(messagesStart)
      expect(patcherStart, `${locale}: locale patcher must follow missingMessages`).toBeGreaterThan(missingStart)

      const messagesBeforePatches = source.slice(messagesStart, missingStart)
      const missingMessagesSource = source.slice(missingStart, patcherStart)
      expect(messagesBeforePatches, `${locale}: radar must be defined in the real messages tree`)
        .toMatch(/\n {2}radar:\s*{/)
      expect(missingMessagesSource, `${locale}: radar must not be supplied by flat patches`)
        .not.toMatch(/['"`]radar\./)
    }
  })
})
