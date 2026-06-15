import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar header styles', () => {
  it('does not render the version badge in the brand header', () => {
    expect(componentSource).not.toContain('<VersionBadge')
    expect(componentSource).not.toContain("from '@/components/common/VersionBadge.vue'")
  })
})

describe('AppSidebar docs entry', () => {
  it('does not render docs as an in-app sidebar navigation item', () => {
    expect(componentSource).not.toContain("{ path: '/docs'")
    expect(componentSource).not.toContain('DocsIcon')
  })
})
