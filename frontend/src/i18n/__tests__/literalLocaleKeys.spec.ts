import { readdirSync, readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

type LocaleMessages = Record<string, unknown>

interface MissingLocaleKey {
  key: string
  locales: string[]
  refs: string[]
}

function walkSourceFiles(dir: string, files: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (['node_modules', 'dist', 'coverage'].includes(entry.name)) {
      continue
    }

    const fullPath = join(dir, entry.name)
    if (entry.isDirectory()) {
      walkSourceFiles(fullPath, files)
      continue
    }

    if (
      /\.(vue|ts|tsx|js|jsx)$/.test(entry.name) &&
      !fullPath.includes(`${join('src', '__tests__')}`) &&
      !fullPath.includes(`${join('__tests__')}`)
    ) {
      files.push(fullPath)
    }
  }
  return files
}

function hasLocalePath(messages: LocaleMessages, path: string): boolean {
  let current: unknown = messages
  for (const part of path.split('.')) {
    if (!current || typeof current !== 'object' || !(part in current)) {
      return false
    }
    current = (current as LocaleMessages)[part]
  }
  return true
}

function lineNumberAt(source: string, index: number): number {
  return source.slice(0, index).split('\n').length
}

function collectLiteralLocaleKeys(): Map<string, string[]> {
  const srcRoot = join(process.cwd(), 'src')
  const callPattern = /(?:\b(?:t|\$t)|i18n\.global\.t)\s*\(\s*(['"`])((?:\\.|(?!\1)[\s\S])*?)\1/g
  const keys = new Map<string, string[]>()

  for (const file of walkSourceFiles(srcRoot)) {
    const source = readFileSync(file, 'utf8')
    let match: RegExpExecArray | null
    while ((match = callPattern.exec(source))) {
      let cursor = callPattern.lastIndex
      while (/\s/.test(source[cursor] || '')) {
        cursor += 1
      }

      if (source[cursor] === '+') {
        continue
      }

      const key = match[2]
        .replace(/\\'/g, "'")
        .replace(/\\"/g, '"')
        .replace(/\\`/g, '`')

      if (!key || key.includes('${')) {
        continue
      }

      const ref = `${relative(process.cwd(), file)}:${lineNumberAt(source, match.index)}`
      keys.set(key, [...(keys.get(key) ?? []), ref])
    }
  }

  return keys
}

describe('literal locale keys', () => {
  it('defines every literal t() key used by source files in zh and en', () => {
    const locales: Record<string, LocaleMessages> = { zh, en }
    const missing: MissingLocaleKey[] = []

    for (const [key, refs] of collectLiteralLocaleKeys()) {
      const missingLocales = Object.entries(locales)
        .filter(([, messages]) => !hasLocalePath(messages, key))
        .map(([locale]) => locale)

      if (missingLocales.length > 0) {
        missing.push({ key, locales: missingLocales, refs })
      }
    }

    expect(
      missing.map((item) => `${item.key} [${item.locales.join(', ')}] ${item.refs.join(', ')}`)
    ).toEqual([])
  })
})
