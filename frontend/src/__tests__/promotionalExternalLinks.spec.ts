import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

const repoRoot = resolve(__dirname, '../../..')

function readRepoFile(path: string) {
  return readFileSync(resolve(repoRoot, path), 'utf8')
}

describe('promotional external links', () => {
  it('keeps the upstream sponsor sections in public README files', () => {
    for (const [path, heading] of [
      ['README.md', '## ❤️ Sponsors'],
      ['README_CN.md', '## ❤️ 赞助商'],
      ['README_JA.md', '## ❤️ スポンサー'],
    ]) {
      const source = readRepoFile(path)

      expect(source).toContain(heading)
    }
  })

  it('does not render the proxy ad banner in the frontend', () => {
    const frontendSources = [
      'frontend/src/views/admin/ProxiesView.vue',
      'frontend/src/components/account/CreateAccountModal.vue',
      'frontend/src/components/account/EditAccountModal.vue',
    ]

    for (const path of frontendSources) {
      const source = readRepoFile(path)

      expect(source).not.toContain('bestproxy.com/?keyword=a2e8iuol')
      expect(source).not.toContain('ProxyAdBanner')
    }

    expect(existsSync(resolve(repoRoot, 'frontend/src/components/common/ProxyAdBanner.vue'))).toBe(
      false,
    )
  })
})
