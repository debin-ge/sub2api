import { describe, expect, it } from 'vitest'
import { marked } from 'marked'
import {
  activateDocTab,
  enhanceCallouts,
  enhanceClientCards,
  enhanceStepHeadings,
  groupCodeTabs,
  parseClientBlock,
  renderDocCode,
  wrapTables,
} from '../docsEnhance'

const CALLOUT_LABELS = {
  note: 'Note',
  tip: 'Tip',
  important: 'Important',
  warning: 'Warning',
  caution: 'Caution',
}

const CARD_LABELS = { baseUrl: 'Base URL', configFile: 'Config file', copy: 'Copy' }

function renderToTemplate(markdown: string): HTMLTemplateElement {
  marked.setOptions({ breaks: true, gfm: true })
  marked.use({ renderer: { code: renderDocCode } })
  const template = document.createElement('template')
  template.innerHTML = marked.parse(markdown) as string
  return template
}

describe('renderDocCode', () => {
  it('renders a plain fenced block with a language attribute', () => {
    const html = renderDocCode({ type: 'code', raw: '', text: 'echo "hi"', lang: 'bash' })
    expect(html).toBe('<pre data-lang="bash"><code class="language-bash">echo &quot;hi&quot;\n</code></pre>\n')
  })

  it('omits the language attribute for text blocks', () => {
    const html = renderDocCode({ type: 'code', raw: '', text: 'plain', lang: 'text' })
    expect(html).not.toContain('data-lang')
  })

  it('renders a block without language', () => {
    const html = renderDocCode({ type: 'code', raw: '', text: 'plain', lang: '' })
    expect(html).toBe('<pre><code>plain\n</code></pre>\n')
  })

  it('adds data attributes when tab and group are present', () => {
    const html = renderDocCode({ type: 'code', raw: '', text: 'x', lang: 'python tab=Python group=chat' })
    expect(html).toContain('data-tab="Python"')
    expect(html).toContain('data-group="chat"')
    expect(html).toContain('class="language-python"')
  })

  it('ignores tab without group', () => {
    const html = renderDocCode({ type: 'code', raw: '', text: 'x', lang: 'bash tab=curl' })
    expect(html).not.toContain('data-tab')
  })

  it('escapes html in code content', () => {
    const html = renderDocCode({ type: 'code', raw: '', text: '<script>alert(1)</script>', lang: 'bash' })
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
  })
})

describe('parseClientBlock', () => {
  it('parses all fields', () => {
    const info = parseClientBlock([
      'name: Cursor',
      'logo: openai',
      'protocols: [openai, anthropic]',
      'endpoint: https://api.example.com/v1',
      'config: ~/.cursor/config.json',
      'homepage: https://cursor.com',
    ].join('\n'))

    expect(info).toEqual({
      name: 'Cursor',
      logo: 'openai',
      protocols: ['openai', 'anthropic'],
      endpoint: 'https://api.example.com/v1',
      config: '~/.cursor/config.json',
      homepage: 'https://cursor.com',
    })
  })

  it('returns null without a name', () => {
    expect(parseClientBlock('logo: openai')).toBeNull()
  })
})

describe('groupCodeTabs', () => {
  const MARKDOWN = [
    '```bash tab=curl group=chat',
    'curl example',
    '```',
    '',
    '```python tab=Python group=chat',
    'print("hi")',
    '```',
    '',
    '```bash tab=curl group=other',
    'curl other',
    '```',
    '',
    '```python tab=Python group=other',
    'print("other")',
    '```',
  ].join('\n')

  it('wraps consecutive same-group blocks into a tab widget', () => {
    const template = renderToTemplate(MARKDOWN)
    groupCodeTabs(template.content, null)

    const tabs = template.content.querySelectorAll('.doc-tabs')
    expect(tabs).toHaveLength(2)

    const first = tabs[0]
    const buttons = first.querySelectorAll('.doc-tab-btn')
    expect(buttons).toHaveLength(2)
    expect(buttons[0].textContent).toBe('curl')
    expect(buttons[0].getAttribute('aria-selected')).toBe('true')

    const panels = first.querySelectorAll('.doc-tabs-panels pre')
    expect(panels).toHaveLength(2)
    expect(panels[0].hasAttribute('hidden')).toBe(false)
    expect(panels[1].hasAttribute('hidden')).toBe(true)
  })

  it('activates the preferred tab when present', () => {
    const template = renderToTemplate(MARKDOWN)
    groupCodeTabs(template.content, 'Python')

    const first = template.content.querySelector('.doc-tabs')!
    const active = first.querySelector('.doc-tab-btn.active')
    expect(active?.getAttribute('data-tab')).toBe('Python')
  })

  it('leaves a single grouped block untouched', () => {
    const template = renderToTemplate('```bash tab=curl group=solo\nsolo\n```')
    groupCodeTabs(template.content, null)
    expect(template.content.querySelector('.doc-tabs')).toBeNull()
    expect(template.content.querySelector('pre[data-group="solo"]')).not.toBeNull()
  })

  it('activateDocTab switches every group containing the label', () => {
    const template = renderToTemplate(MARKDOWN)
    groupCodeTabs(template.content, null)

    const host = document.createElement('div')
    host.appendChild(template.content)
    activateDocTab(host, 'Python')

    for (const tabs of Array.from(host.querySelectorAll('.doc-tabs'))) {
      expect(tabs.querySelector('.doc-tab-btn.active')?.getAttribute('data-tab')).toBe('Python')
      const visible = Array.from(tabs.querySelectorAll('.doc-tabs-panels pre'))
        .filter((pre) => !pre.hasAttribute('hidden'))
      expect(visible).toHaveLength(1)
      expect(visible[0].getAttribute('data-tab')).toBe('Python')
    }
  })
})

describe('enhanceCallouts', () => {
  it('turns an alert blockquote into a callout with icon and label', () => {
    const template = renderToTemplate('> [!WARNING]\n> Do not expose keys.\n\nRegular paragraph.')
    enhanceCallouts(template.content, CALLOUT_LABELS)

    const callout = template.content.querySelector('.doc-callout')
    expect(callout).not.toBeNull()
    expect(callout!.classList.contains('doc-callout-warning')).toBe(true)
    expect(callout!.querySelector('.doc-callout-title')?.textContent).toContain('Warning')
    expect(callout!.querySelector('.doc-callout-title svg')).not.toBeNull()
    expect(callout!.querySelector('.doc-callout-body')?.textContent).toContain('Do not expose keys.')
    expect(callout!.textContent).not.toContain('[!WARNING]')
    expect(template.content.querySelector('blockquote')).toBeNull()
  })

  it('supports the marker and content on separate paragraphs', () => {
    const template = renderToTemplate('> [!TIP]\n>\n> First line.\n>\n> Second line.')
    enhanceCallouts(template.content, CALLOUT_LABELS)

    const body = template.content.querySelector('.doc-callout-body')
    expect(body?.textContent).toContain('First line.')
    expect(body?.textContent).toContain('Second line.')
    expect(body?.textContent).not.toContain('[!TIP]')
  })

  it('leaves plain blockquotes untouched', () => {
    const template = renderToTemplate('> Just a quote.')
    enhanceCallouts(template.content, CALLOUT_LABELS)

    expect(template.content.querySelector('.doc-callout')).toBeNull()
    expect(template.content.querySelector('blockquote')?.textContent).toContain('Just a quote.')
  })

  it('ignores unknown markers', () => {
    const template = renderToTemplate('> [!DANGER]\n> Unknown type.')
    enhanceCallouts(template.content, CALLOUT_LABELS)

    expect(template.content.querySelector('.doc-callout')).toBeNull()
    expect(template.content.querySelector('blockquote')).not.toBeNull()
  })
})

describe('wrapTables', () => {
  it('wraps tables in a scroll shell exactly once', () => {
    const template = renderToTemplate('| A | B |\n| --- | --- |\n| 1 | 2 |')
    wrapTables(template.content)
    wrapTables(template.content)

    const wraps = template.content.querySelectorAll('.doc-table-wrap')
    expect(wraps).toHaveLength(1)
    expect(wraps[0].querySelector('table')).not.toBeNull()
    expect(wraps[0].querySelector('.doc-table-wrap')).toBeNull()
  })
})

describe('enhanceStepHeadings', () => {
  it('converts a leading number into a step badge', () => {
    const template = renderToTemplate('## 1. Configure the Base URL')
    enhanceStepHeadings(template.content)

    const heading = template.content.querySelector('h2')!
    const badge = heading.querySelector('.doc-step-num')
    expect(badge?.textContent).toBe('1')
    expect(heading.textContent).toContain('Configure the Base URL')
    expect(heading.textContent).not.toContain('1.')
  })

  it('leaves headings without a step number untouched', () => {
    const template = renderToTemplate('## Next Steps')
    enhanceStepHeadings(template.content)

    expect(template.content.querySelector('.doc-step-num')).toBeNull()
    expect(template.content.querySelector('h2')?.textContent).toBe('Next Steps')
  })
})

describe('enhanceClientCards', () => {
  const MARKDOWN = [
    '## Cursor',
    '',
    '```client',
    'name: Cursor',
    'logo: openai',
    'protocols: [openai]',
    'endpoint: https://api.example.com/v1',
    'homepage: https://cursor.com',
    '```',
    '',
    'Step one, do this.',
    '',
    'Step two, do that.',
    '',
    '## Next Section',
    '',
    'Outside the card.',
  ].join('\n')

  it('replaces the client block with a card and wraps following content', () => {
    const template = renderToTemplate(MARKDOWN)
    enhanceClientCards(template.content, CARD_LABELS)

    const card = template.content.querySelector('.client-card')
    expect(card).not.toBeNull()
    expect(template.content.querySelector('code.language-client')).toBeNull()

    const link = card!.querySelector('a.client-card-name')
    expect(link?.getAttribute('href')).toBe('https://cursor.com/')
    expect(link?.getAttribute('rel')).toContain('noopener')
    expect(link?.textContent).toBe('Cursor')

    expect(card!.querySelector('.client-pill')?.textContent).toBe('OpenAI')
    expect(card!.querySelector('.client-copy-btn')?.getAttribute('data-copy')).toBe('https://api.example.com/v1')

    const body = card!.querySelector('.client-card-body')!
    expect(body.textContent).toContain('Step one')
    expect(body.textContent).toContain('Step two')
    expect(body.textContent).not.toContain('Outside the card')

    // the next heading stays outside the card
    const headings = template.content.querySelectorAll('h2')
    expect(headings).toHaveLength(2)
    expect(headings[1].closest('.client-card')).toBeNull()
  })

  it('drops unsafe homepage links', () => {
    const template = renderToTemplate([
      '```client',
      'name: Sketchy',
      // eslint-disable-next-line no-script-url
      'homepage: javascript:alert(1)',
      '```',
    ].join('\n'))
    enhanceClientCards(template.content, CARD_LABELS)

    const card = template.content.querySelector('.client-card')!
    expect(card.querySelector('a.client-card-name')).toBeNull()
    expect(card.querySelector('span.client-card-name')?.textContent).toBe('Sketchy')
  })

  it('leaves a malformed client block as a code block', () => {
    const template = renderToTemplate('```client\nlogo: openai\n```')
    enhanceClientCards(template.content, CARD_LABELS)
    expect(template.content.querySelector('.client-card')).toBeNull()
    expect(template.content.querySelector('code.language-client')).not.toBeNull()
  })
})
