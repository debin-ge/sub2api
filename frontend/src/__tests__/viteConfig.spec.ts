// @vitest-environment node

import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { serializeInlineScriptJson } from '../../build-tools/inline-script-json'

describe('serializeInlineScriptJson', () => {
  it('prevents settings values from terminating the inline script element', () => {
    const value = {
      site_name: '</script><script>alert("xss")</script>',
      site_subtitle: 'A&B\u2028C\u2029D'
    }

    const serialized = serializeInlineScriptJson(value)

    expect(serialized.toLowerCase()).not.toContain('</script')
    expect(serialized).not.toContain('<')
    expect(serialized).not.toContain('&')
    expect(JSON.parse(serialized)).toEqual(value)
  })
})

describe('static entry branding', () => {
  it('uses neutral placeholders and a loading shell before settings are available', () => {
    const html = readFileSync(new URL('../../index.html', import.meta.url), 'utf8')

    expect(html).toContain('<title>AI API Gateway</title>')
    expect(html).toContain('data:image/svg+xml')
    expect(html).toContain('role="status"')
    expect(html).not.toContain('Sub2API')
    expect(html).not.toContain('href="/logo.svg"')
  })
})
