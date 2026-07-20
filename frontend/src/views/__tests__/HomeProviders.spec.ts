import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

const root = resolve(__dirname, '../..')

function readSource(path: string) {
  return readFileSync(resolve(root, path), 'utf8')
}

describe('HomeView supported providers', () => {
  it('renders supported domestic AI model providers', () => {
    const homeSource = readSource('views/HomeView.vue')

    expect(homeSource).toContain("t('home.providers.minimax')")
    expect(homeSource).toContain("t('home.providers.glm')")
    expect(homeSource).toContain("t('home.providers.kimi')")
  })

  it('defines localized labels for the domestic providers', () => {
    for (const locale of [zh, en]) {
      expect(locale.home.providers.minimax).toBe('MiniMax')
      expect(locale.home.providers.glm).toBe('GLM')
      expect(locale.home.providers.kimi).toBe('Kimi')
    }
  })
})
