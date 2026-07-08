import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { renderAdminComplianceDocumentTemplate } from '../adminComplianceDocument'

describe('renderAdminComplianceDocumentTemplate', () => {
  it('renders compliance document copy with the configured site name', () => {
    const content = [
      '# {{SITE_NAME}} 部署与运营合规承诺',
      '继续访问 Sub2API 控制台前，请确认 {{SITE_NAME}} 的合规承诺。',
    ].join('\n')

    const rendered = renderAdminComplianceDocumentTemplate(content, 'Acme Gateway')

    expect(rendered).toContain('Acme Gateway 部署与运营合规承诺')
    expect(rendered).toContain('确认 Acme Gateway 的合规承诺')
    expect(rendered).not.toContain('Sub2API')
    expect(rendered).not.toContain('{{SITE_NAME}}')
  })

  it('keeps bundled compliance documents as site-name templates', () => {
    const repoRoot = resolve(process.cwd(), '..')
    const zh = readFileSync(resolve(repoRoot, 'docs/legal/admin-compliance.zh.md'), 'utf8')
    const en = readFileSync(resolve(repoRoot, 'docs/legal/admin-compliance.en.md'), 'utf8')

    expect(`${zh}\n${en}`).toContain('{{SITE_NAME}}')
    expect(`${zh}\n${en}`).not.toContain('Sub2API')
  })
})
