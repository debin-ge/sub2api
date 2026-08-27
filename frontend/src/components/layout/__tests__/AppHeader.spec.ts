import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppHeader.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('AppHeader user menu', () => {
  it('does not show a project GitHub link in the dropdown', () => {
    expect(componentSource).not.toContain('https://github.com/Wei-Shaw/sub2api')
    expect(componentSource).not.toContain("t('nav.github')")
  })

  it('opens the sanitized configured docs page from the header', () => {
    expect(componentSource).toContain(':href="docUrl"')
    expect(componentSource).toContain('target="_blank"')
    expect(componentSource).toContain('computed(() => sanitizeUrl(appStore.docUrl))')
  })

  it('opens the model plaza from the header only when runtime access is enabled', () => {
    expect(componentSource).toContain('v-if="showModelPlaza"')
    expect(componentSource).toContain('to="/plaza"')
    expect(componentSource).toContain("t('plaza.header.label')")
    expect(componentSource).toContain(
      'appStore.cachedPublicSettings?.model_plaza_enabled !== true',
    )
    expect(componentSource).toContain(
      'appStore.cachedPublicSettings?.model_plaza_require_auth !== true',
    )
  })
})
