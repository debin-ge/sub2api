import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

const root = resolve(__dirname, '../..')

function readSource(path: string) {
  return readFileSync(resolve(root, path), 'utf8')
}

describe('HomeView footer', () => {
  it('shows the tiktoken ownership notice', () => {
    const homeSource = readSource('views/HomeView.vue')

    expect(homeSource).toContain('tiktoken is owned by Jerrywell Pte. Ltd.')
  })

  it('does not render the docs text link in the footer', () => {
    const homeSource = readSource('views/HomeView.vue')

    expect(homeSource).not.toContain("{{ t('home.docs') }}")
  })
})
