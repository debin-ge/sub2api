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
  useAuthStore: () => ({
    user: null,
    isAdmin: false,
  }),
}))

vi.mock('@/docs/registry', () => {
  const maliciousDoc = {
    slug: 'overview',
    title: '安全渲染',
    description: '验证 Markdown 渲染安全策略和目录生成。',
    category: '测试',
    content: `# 安全标题

<script>window.__docsXss = true</script>
<img src="x" onerror="window.__docsXss = true">
<a href="javascript:alert(1)" onclick="window.__docsXss = true">危险链接</a>

## 子标题

\`\`\`js
console.log('copy')
\`\`\`
`,
  }

  return {
    defaultUserDocSlug: 'overview',
    userDocs: [maliciousDoc],
    userDocsByLocale: {
      en: [maliciousDoc],
      zh: [maliciousDoc],
    },
    normalizeUserDocLocale: (locale?: string) => locale?.toLowerCase().startsWith('zh') ? 'zh' : 'en',
    findUserDoc: (slug?: string) => (!slug || slug === 'overview' ? maliciousDoc : null),
  }
})

function createDocsRouter() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/home', component: { template: '<div>Home</div>' } },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/dashboard', component: { template: '<div>Dashboard</div>' } },
      { path: '/docs', component: DocsView },
      { path: '/docs/:slug', component: DocsView },
    ],
  })
  router.push('/docs')
  return router
}

async function mountDocs() {
  const router = createDocsRouter()
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

describe('DocsView security rendering', () => {
  beforeEach(() => {
    fetchPublicSettings.mockReset()
    i18n.global.locale.value = 'zh'
    Object.assign(navigator, {
      clipboard: {
        writeText: vi.fn().mockResolvedValue(undefined),
      },
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('sanitizes dangerous markdown HTML while preserving document content', async () => {
    const wrapper = await mountDocs()
    const html = wrapper.html()

    expect(wrapper.text()).toContain('安全标题')
    expect(wrapper.text()).toContain('危险链接')
    expect(html).not.toContain('<script')
    expect(html).not.toContain('onerror')
    expect(html).not.toContain('onclick')
    expect(html).not.toContain('javascript:')
  })

  it('generates a page table of contents from rendered headings', async () => {
    const wrapper = await mountDocs()
    const tocButtons = wrapper.findAll('aside button')

    expect(tocButtons.some((button) => button.text() === '安全标题')).toBe(true)
    expect(tocButtons.some((button) => button.text() === '子标题')).toBe(true)
  })
})
