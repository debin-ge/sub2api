import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

const root = resolve(__dirname, '../..')

function readSource(path: string) {
  return readFileSync(resolve(root, path), 'utf8')
}

describe('HomeView footer', () => {
  it('uses the localized footer notice', () => {
    const homeSource = readSource('views/HomeView.vue')

    expect(homeSource).toContain(
      "&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}",
    )
    expect(homeSource).not.toContain('&copy; {{ currentYear }} TikToken is owned')
  })

  it('defines the footer notice in both locales', () => {
    const zhSource = readSource('i18n/locales/zh.ts')
    const enSource = readSource('i18n/locales/en.ts')

    for (const source of [zhSource, enSource]) {
      expect(source).toContain("allRightsReserved: 'TikToken is owned by Jerrywell Pte. Ltd.'")
    }
  })

  it('does not render the docs text link in the footer', () => {
    const homeSource = readSource('views/HomeView.vue')

    expect(homeSource).not.toContain("{{ t('home.docs') }}")
  })
})
