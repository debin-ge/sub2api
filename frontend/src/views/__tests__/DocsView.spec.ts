import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

import DocsView from '../DocsView.vue'
import i18n from '@/i18n'

const fetchPublicSettings = vi.fn()
const authState = {
  user: null as null | { role: string },
  isAdmin: false,
}

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    siteName: 'Sub2API',
    siteLogo: '',
    publicSettingsLoaded: true,
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

  it('injects copy buttons for code blocks', async () => {
    const wrapper = await mountDocs('/docs/quickstart')
    const copyButton = wrapper.find('.copy-btn')

    expect(copyButton.exists()).toBe(true)
    await copyButton.trigger('click')

    expect(navigator.clipboard.writeText).toHaveBeenCalled()
  })

  it('does not render script tags from bundled markdown', async () => {
    const wrapper = await mountDocs('/docs/api-reference')

    expect(wrapper.html()).not.toContain('<script')
  })
})
