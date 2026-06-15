import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

import DocsView from '../DocsView.vue'
import i18n from '@/i18n'

const fetchPublicSettings = vi.fn()

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    siteName: 'Sub2API',
    siteLogo: '',
    publicSettingsLoaded: true,
    fetchPublicSettings,
  }),
}))

function createDocsRouter(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/home', component: { template: '<div>Home</div>' } },
      { path: '/login', component: { template: '<div>Login</div>' } },
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
      plugins: [router],
    },
  })

  await nextTick()
  await nextTick()
  return wrapper
}

describe('DocsView', () => {
  beforeEach(() => {
    fetchPublicSettings.mockReset()
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

  it('shows a not found state for unknown slugs', async () => {
    const wrapper = await mountDocs('/docs/missing')

    expect(wrapper.text()).toContain('Document Not Found')
    expect(wrapper.find('a[href="/docs"]').exists()).toBe(true)
  })

  it('renders navigation for every registered document', async () => {
    const wrapper = await mountDocs('/docs')

    for (const label of ['Product Overview', 'Quick Start', 'API Reference', 'Models and Platforms', 'Client Integration', 'Troubleshooting', 'FAQ']) {
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
