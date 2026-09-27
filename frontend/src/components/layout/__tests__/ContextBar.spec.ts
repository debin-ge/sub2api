import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const barSource = readFileSync(resolve(here, '../ContextBar.vue'), 'utf8')
const menuSource = readFileSync(resolve(here, '../RailUserMenu.vue'), 'utf8')

describe('ContextBar global links', () => {
  it('does not show a project GitHub link anywhere in the shell', () => {
    for (const source of [barSource, menuSource]) {
      expect(source).not.toContain('https://github.com/Wei-Shaw/sub2api')
      expect(source).not.toContain("t('nav.github')")
    }
  })

  it('opens the sanitized configured docs page from the context bar', () => {
    expect(barSource).toContain(':href="docUrl"')
    expect(barSource).toContain('target="_blank"')
    expect(barSource).toContain('computed(() => sanitizeUrl(appStore.docUrl))')
  })

  it('opens the model plaza only when runtime access is enabled', () => {
    expect(barSource).toContain('v-if="showModelPlaza"')
    expect(barSource).toContain('to="/plaza"')
    expect(barSource).toContain("t('plaza.header.label')")
    expect(barSource).toContain('appStore.cachedPublicSettings?.model_plaza_enabled !== true')
    expect(barSource).toContain('appStore.cachedPublicSettings?.model_plaza_require_auth !== true')
  })

  it('keeps the subscription badge unmounted when the feature is off', () => {
    expect(barSource).toContain('v-if="user && subscriptionFeatureEnabled"')
  })
})

describe('RailUserMenu parity with the legacy header dropdown', () => {
  it('keeps profile, keys, support, onboarding replay and logout entries', () => {
    expect(menuSource).toContain('to="/profile"')
    expect(menuSource).toContain('to="/keys"')
    expect(menuSource).toContain('CustomerSupportDialog')
    expect(menuSource).toContain("t('onboarding.restartTour')")
    expect(menuSource).toContain("t('nav.logout')")
    expect(menuSource).toContain('!authStore.isSimpleMode && user.value?.role === \'admin\'')
  })
})
