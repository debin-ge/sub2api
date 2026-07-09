import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

import DocsView from '../DocsView.vue'
import i18n from '@/i18n'

const fetchPublicSettings = vi.fn()
const appState = {
  cachedPublicSettings: null as null | {
    site_name?: string
    site_logo?: string
    api_base_url?: string
  },
  siteName: 'Sub2API',
  siteLogo: '',
  apiBaseUrl: '',
  publicSettingsLoaded: true,
}
const authState = {
  user: null as null | { role: string },
  isAdmin: false,
}

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: appState.cachedPublicSettings,
    siteName: appState.siteName,
    siteLogo: appState.siteLogo,
    apiBaseUrl: appState.apiBaseUrl,
    publicSettingsLoaded: appState.publicSettingsLoaded,
    fetchPublicSettings,
  }),
  useAuthStore: () => authState,
}))

function createDocsRouter(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/home', component: { template: '<div>Home</div>' } },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/dashboard', component: { template: '<div>Dashboard</div>' } },
      { path: '/admin/dashboard', component: { template: '<div>Admin Dashboard</div>' } },
      { path: '/docs', component: DocsView },
      { path: '/docs/:slug', component: DocsView },
    ],
  })
  router.push(path)
  return router
}

async function mountDocs(path = '/docs') {
  const router = createDocsRouter(path)
  await router.isReady()

  const wrapper = mount(DocsView, {
    global: {
      plugins: [router, i18n],
    },
  })

  await nextTick()
  await nextTick()
  return wrapper
}

describe('DocsView', () => {
  beforeEach(() => {
    fetchPublicSettings.mockReset()
    appState.cachedPublicSettings = null
    appState.siteName = 'Sub2API'
    appState.siteLogo = ''
    appState.apiBaseUrl = ''
    appState.publicSettingsLoaded = true
    authState.user = null
    authState.isAdmin = false
    i18n.global.locale.value = 'en'
    Object.assign(navigator, {
      clipboard: {
        writeText: vi.fn().mockResolvedValue(undefined),
      },
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the default English overview document at /docs', async () => {
    const wrapper = await mountDocs('/docs')

    expect(wrapper.text()).toContain('Product Overview')
    expect(wrapper.text()).toContain('Sub2API')
    expect(wrapper.find('a[href="/docs"]').exists()).toBe(true)
  })

  it('renders a document selected by slug', async () => {
    const wrapper = await mountDocs('/docs/quickstart')

    expect(wrapper.text()).toContain('Quick Start')
    expect(wrapper.html()).toContain('/v1/models')
    expect(wrapper.html()).toContain('/v1/chat/completions')
  })

  it('renders Chinese documents when the active locale is zh', async () => {
    i18n.global.locale.value = 'zh'
    const wrapper = await mountDocs('/docs')

    expect(wrapper.text()).toContain('产品概览')
    expect(wrapper.text()).toContain('文档导航')
  })

  it('renders documentation with the configured site name and base URL', async () => {
    appState.cachedPublicSettings = {
      site_name: 'Acme AI',
      site_logo: '',
      api_base_url: 'https://api.acme.test/',
    }
    appState.siteName = 'Acme AI'
    appState.apiBaseUrl = 'https://api.acme.test/'

    const wrapper = await mountDocs('/docs/quickstart')

    expect(wrapper.text()).toContain('Acme AI')
    expect(wrapper.text()).toContain('https://api.acme.test/')
    expect(wrapper.text()).not.toContain('Sub2API')
    expect(wrapper.text()).not.toContain('tiktoken.net')
  })

  it('renders a language switcher in the docs header', async () => {
    const wrapper = await mountDocs('/docs')

    const languageButton = wrapper.find('button[title="English"]')
    expect(languageButton.exists()).toBe(true)

    await languageButton.trigger('click')
    await nextTick()

    const optionLabels = wrapper.findAll('button').map((button) => button.text().trim())
    expect(optionLabels).toContain('🇺🇸English')
    expect(optionLabels).toContain('🇨🇳中文')
  })

  it('shows a not found state for unknown slugs', async () => {
    const wrapper = await mountDocs('/docs/missing')

    expect(wrapper.text()).toContain('Document Not Found')
    expect(wrapper.find('a[href="/docs"]').exists()).toBe(true)
  })

  it('shows a dashboard link instead of login for authenticated users', async () => {
    authState.user = { role: 'user' }
    const wrapper = await mountDocs('/docs')

    expect(wrapper.find('a[href="/dashboard"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/login"]').exists()).toBe(false)
  })

  it('renders navigation for every registered document', async () => {
    const wrapper = await mountDocs('/docs')

    for (const label of [
      'Product Overview',
      'Quick Start',
      'API Keys and Accounts',
      'API Reference',
      'Endpoint Selection Guide',
      'Models and Platforms',
      'Billing and Usage',
      'Client Integration',
      'Copy-Ready Configuration Snippets',
      'Troubleshooting',
      'Best Practices',
      'FAQ',
    ]) {
      expect(wrapper.text()).toContain(label)
    }
  })

  it('renders integration navigation entries for zh locale', async () => {
    i18n.global.locale.value = 'zh'
    const wrapper = await mountDocs('/docs')

    for (const label of ['接入总览', '代码接入', '客户端接入', 'CLI 接入', '第三方工具接入']) {
      expect(wrapper.text()).toContain(label)
    }
  })

  it('renders every zh integration page without falling back to not-found', async () => {
    i18n.global.locale.value = 'zh'
    for (const slug of [
      'integration-overview',
      'integration-code',
      'integration-clients',
      'integration-cli',
      'integration-tools',
    ]) {
      const wrapper = await mountDocs(`/docs/${slug}`)
      expect(wrapper.text()).not.toContain('文档不存在')
      expect(wrapper.find('article h1').exists()).toBe(true)
      wrapper.unmount()
    }
  })

  it('renders code tab groups on the zh integration-code page', async () => {
    i18n.global.locale.value = 'zh'
    const wrapper = await mountDocs('/docs/integration-code')

    const tabs = wrapper.find('.doc-tabs')
    expect(tabs.exists()).toBe(true)

    const labels = tabs.findAll('.doc-tab-btn').map((btn) => btn.text())
    expect(labels).toEqual(['curl', 'Python', 'TypeScript', 'Go'])

    const pythonTab = tabs.findAll('.doc-tab-btn').find((btn) => btn.text() === 'Python')!
    await pythonTab.trigger('click')

    for (const group of wrapper.findAll('.doc-tabs')) {
      const active = group.find('.doc-tab-btn.active')
      if (group.findAll('.doc-tab-btn').some((btn) => btn.text() === 'Python')) {
        expect(active.text()).toBe('Python')
      }
    }
  })

  it('renders client cards on the zh integration-clients page', async () => {
    i18n.global.locale.value = 'zh'
    const wrapper = await mountDocs('/docs/integration-clients')

    const card = wrapper.find('.client-card')
    expect(card.exists()).toBe(true)
    expect(wrapper.find('.client-pill').exists()).toBe(true)
    expect(wrapper.html()).not.toContain('language-client')

    const copyBtn = card.find('.client-copy-btn')
    expect(copyBtn.exists()).toBe(true)
    await copyBtn.trigger('click')
    expect(navigator.clipboard.writeText).toHaveBeenCalled()
  })

  it('injects copy buttons for code blocks', async () => {
    const wrapper = await mountDocs('/docs/quickstart')
    const copyButton = wrapper.find('.copy-btn')

    expect(copyButton.exists()).toBe(true)
    await copyButton.trigger('click')

    expect(navigator.clipboard.writeText).toHaveBeenCalled()
  })

  it('removes the duplicate leading H1 from the rendered body', async () => {
    const wrapper = await mountDocs('/docs/quickstart')

    expect(wrapper.find('article > header h1').text()).toBe('Quick Start')
    expect(wrapper.find('.docs-content h1').exists()).toBe(false)
  })

  it('renders previous/next pagination links', async () => {
    const wrapper = await mountDocs('/docs/quickstart')

    const pager = wrapper.find('nav[aria-label="Document pagination"]')
    expect(pager.exists()).toBe(true)
    expect(pager.text()).toContain('Product Overview')
    expect(pager.text()).toContain('API Keys and Accounts')
    expect(pager.find('a[href="/docs"]').exists()).toBe(true)
    expect(pager.find('a[href="/docs/api-keys"]').exists()).toBe(true)
  })

  it('renders callouts from alert blockquotes', async () => {
    const wrapper = await mountDocs('/docs/quickstart')

    const callout = wrapper.find('.doc-callout-warning')
    expect(callout.exists()).toBe(true)
    expect(callout.text()).toContain('Warning')
    expect(callout.text()).not.toContain('[!WARNING]')
  })

  it('wraps tables and renders step heading badges', async () => {
    const wrapper = await mountDocs('/docs/quickstart')

    expect(wrapper.find('.doc-table-wrap table').exists()).toBe(true)
    expect(wrapper.find('.doc-step-num').exists()).toBe(true)
  })

  it('adds anchor links to section headings', async () => {
    const wrapper = await mountDocs('/docs/quickstart')

    const anchor = wrapper.find('.docs-content h2 a.heading-anchor')
    expect(anchor.exists()).toBe(true)
    expect(anchor.attributes('href')).toMatch(/^#/)
  })

  it('does not render script tags from bundled markdown', async () => {
    const wrapper = await mountDocs('/docs/api-reference')

    expect(wrapper.html()).not.toContain('<script')
  })
})
