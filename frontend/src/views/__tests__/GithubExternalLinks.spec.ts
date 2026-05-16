import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

const root = resolve(__dirname, '../..')

function readSource(path: string) {
  return readFileSync(resolve(root, path), 'utf8')
}

describe('frontend hard-coded GitHub external links', () => {
  it('does not render project GitHub links from public pages', () => {
    const homeSource = readSource('views/HomeView.vue')
    const keyUsageSource = readSource('views/KeyUsageView.vue')

    for (const source of [homeSource, keyUsageSource]) {
      expect(source).not.toContain('githubUrl')
      expect(source).not.toContain('https://github.com/Wei-Shaw/sub2api')
      expect(source).not.toContain('>GitHub</a>')
    }
  })

  it('does not keep GitHub-hosted help links in admin settings', () => {
    const settingsSource = readSource('views/admin/SettingsView.vue')

    expect(settingsSource).not.toContain('https://github.com/settings/developers')
    expect(settingsSource).not.toContain('https://github.com/Wei-Shaw/sub2api')
    expect(settingsSource).not.toContain('data-testid="github-oauth-apps-guide-link"')
  })

  it('does not keep unused view-on-github labels', () => {
    const zhSource = readSource('i18n/locales/zh.ts')
    const enSource = readSource('i18n/locales/en.ts')

    expect(zhSource).not.toContain('viewOnGithub')
    expect(enSource).not.toContain('viewOnGithub')
  })
})
