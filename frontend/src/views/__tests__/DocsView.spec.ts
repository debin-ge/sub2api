import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
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
  useAuthStore: () => ({
    get user() {
      return authState.user
    },
    get isAuthenticated() {
      return authState.user !== null
    },
    get isAdmin() {
      return authState.isAdmin
    },
  }),
}))

function createDocsRouter(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/home', component: { template: '<div>Home</div>' } },
      { path: '/plaza', component: { template: '<div>Plaza</div>' } },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/register', component: { template: '<div>Register</div>' } },
      { path: '/dashboard', component: { template: '<div>Dashboard</div>' } },
      { path: '/admin/dashboard', component: { template: '<div>Admin Dashboard</div>' } },
      { path: '/docs', component: DocsView },
      { path: '/docs/:slug', component: DocsView },
      { path: '/apps', component: DocsView },
      { path: '/apps/:slug', component: DocsView },
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
      'Troubleshooting',
      'Best Practices',
      'FAQ',
    ]) {
      expect(wrapper.text()).toContain(label)
    }
    // The Integration category has moved to /apps and must no longer appear in /docs.
    for (const label of [
      'Integration Overview',
      'Code Integration',
      'Client Integration',
      'CLI Integration',
      'Third-Party Tools',
    ]) {
      expect(wrapper.text()).not.toContain(label)
    }
  })

  it('returns a not-found state for removed integration slugs', async () => {
    for (const slug of [
      'integration-overview',
      'integration-code',
      'integration-clients',
      'integration-cli',
      'integration-tools',
    ]) {
      const wrapper = await mountDocs(`/docs/${slug}`)
      expect(wrapper.text()).toContain('Document Not Found')
      wrapper.unmount()
    }
  })

  it('searches zh docs across page body content', async () => {
    i18n.global.locale.value = 'zh'
    const wrapper = await mountDocs('/docs')

    await wrapper.find('input[aria-label="搜索文档"]').setValue('Base URL')

    const results = wrapper.find('[data-testid="docs-search-results"]')
    expect(results.exists()).toBe(true)
    expect(results.text()).toContain('Base URL')
    expect(wrapper.find('[data-testid="docs-nav-groups"]').exists()).toBe(false)

    const resultLink = results.find('a[href="/docs/quickstart"]')
    expect(resultLink.exists()).toBe(true)

    await resultLink.trigger('click')
    await flushPromises()
    await nextTick()
    await nextTick()

    expect(wrapper.find('article > header h1').text()).toBe('快速开始')
  })

  it('highlights the query inside search result excerpts', async () => {
    i18n.global.locale.value = 'zh'
    const wrapper = await mountDocs('/docs')

    await wrapper.find('input[aria-label="搜索文档"]').setValue('Base URL')

    const marks = wrapper.findAll('[data-testid="docs-search-results"] mark')
    expect(marks.length).toBeGreaterThan(0)
    expect(marks.some((mark) => mark.text().toLowerCase() === 'base url')).toBe(true)
  })

  it('shows an empty state when docs search has no matches', async () => {
    const wrapper = await mountDocs('/docs')

    await wrapper.find('input[aria-label="Search docs"]').setValue('not-a-real-doc-term')

    expect(wrapper.find('[data-testid="docs-search-results"]').text()).toContain('No matching docs')
    expect(wrapper.find('[data-testid="docs-nav-groups"]').exists()).toBe(false)
  })

  it('clears the docs search from the search box action', async () => {
    const wrapper = await mountDocs('/docs')

    await wrapper.find('input[aria-label="Search docs"]').setValue('models')
    expect(wrapper.find('[data-testid="docs-search-results"]').exists()).toBe(true)

    await wrapper.find('button[aria-label="Clear search"]').trigger('click')

    expect((wrapper.find('input[aria-label="Search docs"]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.find('[data-testid="docs-nav-groups"]').exists()).toBe(true)
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

  // ─────────────── Apps tab (merged into DocsView) ───────────────

  it('renders the apps tab strip alongside the docs tab', async () => {
    const wrapper = await mountDocs('/docs')

    const tabs = wrapper.find('[data-testid="section-tabs"]')
    expect(tabs.exists()).toBe(true)
    expect(tabs.find('a[href="/docs"]').exists()).toBe(true)
    expect(tabs.find('a[href="/apps"]').exists()).toBe(true)

    const docsTab = tabs.find('a[href="/docs"]')
    expect(docsTab.attributes('aria-selected')).toBe('true')
    const appsTab = tabs.find('a[href="/apps"]')
    expect(appsTab.attributes('aria-selected')).toBe('false')
  })

  it('renders the apps landing grid at /apps', async () => {
    const wrapper = await mountDocs('/apps')

    expect(wrapper.text()).toContain('Pick your tool')
    const grid = wrapper.find('[data-testid="apps-grid"]')
    expect(grid.exists()).toBe(true)

    // Nine cards: 8 tools + code samples.
    const cards = grid.findAll('a[href^="/apps/"]')
    expect(cards.length).toBe(9)

    // Docs sidebar is hidden while on Apps tab; apps sidebar list is shown.
    expect(wrapper.find('[data-testid="docs-nav-groups"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="apps-nav-list"]').exists()).toBe(true)

    // Apps tab is marked selected in the tab strip.
    const appsTab = wrapper.find('[data-testid="section-tabs"] a[href="/apps"]')
    expect(appsTab.attributes('aria-selected')).toBe('true')
  })

  it('cards on the apps landing link to /apps/{slug}', async () => {
    const wrapper = await mountDocs('/apps')

    for (const slug of [
      'claude-code',
      'codex',
      'cursor',
      'cline',
      'continue',
      'trae',
      'cc-switch',
      'cockpit-tools',
      'code',
    ]) {
      expect(wrapper.find(`a[href="/apps/${slug}"]`).exists()).toBe(true)
    }
  })

  it('switches the apps landing badge to zh when locale flips', async () => {
    i18n.global.locale.value = 'zh'
    const wrapper = await mountDocs('/apps')

    expect(wrapper.text()).toContain('应用集成')
    expect(wrapper.text()).toContain('选择你使用的工具')
  })

  it('renders the Claude Code app detail page with a client card', async () => {
    const wrapper = await mountDocs('/apps/claude-code')

    expect(wrapper.find('article > header h1').text()).toBe('Claude Code')

    const card = wrapper.find('.client-card')
    expect(card.exists()).toBe(true)
    expect(wrapper.find('.client-pill').exists()).toBe(true)
    expect(wrapper.html()).not.toContain('language-client')

    const copyBtn = card.find('.client-copy-btn')
    expect(copyBtn.exists()).toBe(true)
    await copyBtn.trigger('click')
    expect(navigator.clipboard.writeText).toHaveBeenCalled()
  })

  it('renders code tab groups on /apps/code', async () => {
    const wrapper = await mountDocs('/apps/code')

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

  it('renders collapsible details blocks on app pages', async () => {
    const wrapper = await mountDocs('/apps/claude-code')

    const details = wrapper.findAll('details')
    expect(details.length).toBeGreaterThanOrEqual(2)
    expect(wrapper.findAll('summary').length).toBeGreaterThanOrEqual(2)
  })

  it('shows an app-not-found state for unknown app slugs', async () => {
    const wrapper = await mountDocs('/apps/nonexistent-tool')

    expect(wrapper.text()).toContain('App Not Found')
    expect(wrapper.find('a[href="/apps"]').exists()).toBe(true)
  })

  it('resolves {{SITE_NAME}} in app taglines', async () => {
    i18n.global.locale.value = 'zh'
    appState.cachedPublicSettings = {
      site_name: 'Acme AI',
      site_logo: '',
      api_base_url: 'https://api.acme.test/',
    }
    appState.siteName = 'Acme AI'
    appState.apiBaseUrl = 'https://api.acme.test/'

    const wrapper = await mountDocs('/apps')
    const ccCard = wrapper.find('[data-testid="apps-grid"] a[href="/apps/cc-switch"]')
    expect(ccCard.exists()).toBe(true)
    expect(ccCard.text()).toContain('Acme AI')
    expect(ccCard.text()).not.toContain('{{SITE_NAME}}')
  })

  it('does not render script tags from bundled markdown', async () => {
    const wrapper = await mountDocs('/docs/api-reference')

    expect(wrapper.html()).not.toContain('<script')
  })
})
