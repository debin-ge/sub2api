import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../SettingsView.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('SettingsView branding display', () => {
  it('does not hard-code Sub2API for configurable display fields', () => {
    expect(componentSource).not.toContain('placeholder="Sub2API"')
    expect(componentSource).not.toContain('form.payment_product_name_prefix || "Sub2API"')
    expect(componentSource).not.toContain('t("admin.settings.linuxdo.description")')
  })
})
