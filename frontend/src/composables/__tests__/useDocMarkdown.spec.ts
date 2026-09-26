import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { computed, defineComponent, ref } from 'vue'

import { useDocMarkdown, type DocMarkdownUiText, type RenderableDoc } from '../useDocMarkdown'

const uiText: DocMarkdownUiText = {
  copy: 'Copy',
  copied: 'Copied',
  copyFailed: 'Copy failed',
  downloadConfig: 'Download',
  cardBaseUrl: 'Base URL',
  cardConfigFile: 'Config file',
  readingTime: 'min read',
  calloutNote: 'Note',
  calloutTip: 'Tip',
  calloutImportant: 'Important',
  calloutWarning: 'Warning',
  calloutCaution: 'Caution',
}

interface RenderOptions {
  siteName?: string
  siteBaseUrl?: string
}

function renderDoc(content: string, { siteName = 'Acme Gateway', siteBaseUrl = 'https://example.com/' }: RenderOptions = {}) {
  const Host = defineComponent({
    setup() {
      const { renderedHtml, markdownContainer } = useDocMarkdown({
        doc: ref<RenderableDoc | null>({ title: 'Test doc', content }),
        uiText: computed(() => uiText),
        siteName: ref(siteName),
        siteBaseUrl: ref(siteBaseUrl),
        locale: ref<'zh' | 'en'>('en'),
      })
      return { renderedHtml, markdownContainer }
    },
    template: '<div ref="markdownContainer" v-html="renderedHtml"></div>',
  })

  return mount(Host)
}

describe('useDocMarkdown placeholder substitution', () => {
  it('sanitizes a javascript: base URL that lands inside a link', () => {
    const wrapper = renderDoc('[x]({{BASE_URL}}v1)', { siteBaseUrl: 'javascript:alert(1)//' })

    expect(wrapper.text()).toContain('x')
    expect(wrapper.html()).not.toContain('javascript:')
    for (const link of wrapper.findAll('a')) {
      expect(link.attributes('href') ?? '').not.toMatch(/^javascript:/i)
    }
  })

  it('substitutes a normal base URL into links and marks them external', () => {
    const wrapper = renderDoc('[x]({{BASE_URL}}v1) and [legacy](https://tiktoken.net/v1)')

    const links = wrapper.findAll('a')
    expect(links).toHaveLength(2)
    for (const link of links) {
      expect(link.attributes('href')).toBe('https://example.com/v1')
      expect(link.attributes('target')).toBe('_blank')
      expect(link.attributes('rel')).toBe('noopener noreferrer')
    }
  })

  it('renders a site name containing markup literally instead of as HTML', () => {
    const wrapper = renderDoc('Welcome to {{SITE_NAME}}, formerly Sub2API.', {
      siteName: '<b>Evil</b> & Co',
    })

    expect(wrapper.find('b').exists()).toBe(false)
    expect(wrapper.text()).toContain('Welcome to <b>Evil</b> & Co, formerly <b>Evil</b> & Co.')
  })

  it('does not double-escape values substituted inside code', () => {
    const wrapper = renderDoc(
      'Use `{{BASE_URL}}v1` or:\n\n```bash\nexport OPENAI_BASE_URL={{BASE_URL}}v1\n```\n',
      { siteBaseUrl: 'https://api.example.com/?a=1&b=2#' },
    )

    const codes = wrapper.findAll('code').map((node) => node.text())
    expect(codes).toHaveLength(2)
    for (const code of codes) {
      expect(code).toContain('https://api.example.com/?a=1&b=2#v1')
      expect(code).not.toContain('&amp;')
    }
    expect(wrapper.html()).not.toContain('&amp;amp;')
  })

  it('keeps stripping scripts and event handlers from doc markdown', () => {
    const wrapper = renderDoc(
      '# Title\n\n<script>window.__docXss = true</script>\n<img src="x" onerror="window.__docXss = true">\n\nSafe {{SITE_NAME}} text',
    )

    const html = wrapper.html()
    expect(html).not.toContain('<script')
    expect(html).not.toContain('onerror')
    expect(wrapper.text()).toContain('Safe Acme Gateway text')
  })
})
