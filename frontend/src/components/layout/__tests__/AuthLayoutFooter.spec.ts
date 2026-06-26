import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

const root = resolve(__dirname, '../../..')

function readSource(path: string) {
  return readFileSync(resolve(root, path), 'utf8')
}

describe('AuthLayout footer', () => {
  it('uses the localized footer notice on auth pages', () => {
    const authLayoutSource = readSource('components/layout/AuthLayout.vue')

    expect(authLayoutSource).toContain(
      "&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}",
    )
    expect(authLayoutSource).not.toContain('&copy; {{ currentYear }} TikToken is owned')
  })
})
