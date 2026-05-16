import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

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
    const zhSource = readSource('i18n/locales/zh.ts')
    const enSource = readSource('i18n/locales/en.ts')

    for (const source of [zhSource, enSource]) {
      expect(source).toContain("minimax: 'MiniMax'")
      expect(source).toContain("glm: 'GLM'")
      expect(source).toContain("kimi: 'Kimi'")
    }
  })
})
