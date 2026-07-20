import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

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
    for (const locale of [zh, en]) {
      expect(locale.home.footer.allRightsReserved).toBe('TikToken is owned by Jerrywell Pte. Ltd.')
    }
  })

  it('does not render the docs text link in the footer', () => {
    const homeSource = readSource('views/HomeView.vue')

    expect(homeSource).not.toContain("{{ t('home.docs') }}")
  })

  it('links to the model plaza from the shared site header', () => {
    // The header (and its Model Plaza link) is now the shared SiteHeader,
    // rendered by HomeView with current="home".
    const homeSource = readSource('views/HomeView.vue')
    expect(homeSource).toContain('<SiteHeader current="home"')

    const headerSource = readSource('components/common/SiteHeader.vue')
    expect(headerSource).toContain("to: '/plaza'")
    expect(headerSource).toContain("t('plaza.header.label')")
  })
})
