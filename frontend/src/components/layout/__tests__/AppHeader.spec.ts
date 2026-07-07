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

  it('opens the built-in docs page from the header', () => {
    expect(componentSource).toContain('href="/docs"')
    expect(componentSource).toContain('target="_blank"')
    expect(componentSource).not.toContain('docUrl')
  })

  it('opens the model plaza from the header', () => {
    expect(componentSource).toContain('to="/plaza"')
    expect(componentSource).toContain("t('plaza.header.label')")
  })
})
