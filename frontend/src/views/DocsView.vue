<template>
  <div class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <header class="sticky top-0 z-30 border-b border-gray-200 bg-white/95 backdrop-blur dark:border-dark-800 dark:bg-dark-900/95">
      <div class="mx-auto flex max-w-7xl items-center justify-between gap-4 px-4 py-4 sm:px-6 lg:px-8">
        <RouterLink to="/home" class="flex min-w-0 items-center gap-3">
          <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center overflow-hidden rounded-xl bg-white shadow-sm ring-1 ring-gray-200 dark:bg-dark-800 dark:ring-dark-700">
            <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
          </span>
          <span class="truncate text-base font-semibold text-gray-950 dark:text-white">
            {{ siteName }}
          </span>
        </RouterLink>

        <nav class="flex flex-shrink-0 items-center gap-2">
          <RouterLink
            to="/home"
            class="rounded-lg px-3 py-2 text-sm font-medium text-gray-600 transition hover:bg-gray-100 hover:text-gray-950 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white"
          >
            {{ uiText.home }}
          </RouterLink>
          <RouterLink
            to="/login"
            class="inline-flex items-center justify-center rounded-lg bg-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-primary-600/20 transition hover:bg-primary-700"
          >
            {{ uiText.login }}
          </RouterLink>
        </nav>
      </div>
    </header>

    <main class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
      <button
        type="button"
        class="mb-4 flex w-full items-center justify-between rounded-lg border border-gray-200 bg-white px-4 py-3 text-left text-sm font-semibold text-gray-900 shadow-sm transition hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-900 dark:text-white dark:hover:bg-dark-800 lg:hidden"
        :aria-expanded="mobileNavOpen"
        @click="mobileNavOpen = !mobileNavOpen"
      >
        <span>{{ uiText.docNavigation }}</span>
        <span class="text-lg leading-none text-gray-500 dark:text-dark-300">{{ mobileNavOpen ? '-' : '+' }}</span>
      </button>

      <div class="grid gap-8 lg:grid-cols-[240px_minmax(0,1fr)_220px]">
        <aside
          class="doc-nav lg:sticky lg:top-24 lg:block lg:max-h-[calc(100vh-7rem)] lg:overflow-y-auto"
          :class="{ hidden: !mobileNavOpen }"
        >
          <nav class="space-y-6 border-b border-gray-200 pb-6 dark:border-dark-800 lg:border-b-0 lg:pb-0">
            <section v-for="group in groupedDocs" :key="group.category">
              <h2 class="px-2 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">
                {{ group.category }}
              </h2>
              <div class="mt-2 space-y-1">
                <RouterLink
                  v-for="doc in group.docs"
                  :key="doc.slug"
                  :to="doc.slug === defaultUserDocSlug ? '/docs' : `/docs/${doc.slug}`"
                  class="block rounded-md px-2 py-2 text-sm transition"
                  :class="doc.slug === activeSlug
                    ? 'bg-primary-50 font-semibold text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
                    : 'text-gray-600 hover:bg-gray-100 hover:text-gray-950 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white'"
                  @click="mobileNavOpen = false"
                >
                  <span class="block truncate">{{ doc.title }}</span>
                  <span class="mt-0.5 block line-clamp-2 text-xs font-normal text-gray-500 dark:text-dark-400">
                    {{ doc.description }}
                  </span>
                </RouterLink>
              </div>
            </section>
          </nav>
        </aside>

        <section class="min-w-0">
          <div
            v-if="settingsLoading"
            class="mb-4 rounded-lg border border-gray-200 bg-white px-4 py-3 text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-400"
          >
            {{ uiText.loadingSettings }}
          </div>

          <article v-if="currentDoc" class="min-w-0">
            <header class="mb-8 border-b border-gray-200 pb-6 dark:border-dark-800">
              <p class="text-sm font-medium text-primary-700 dark:text-primary-300">
                {{ currentDoc.category }}
              </p>
              <h1 class="mt-2 break-words text-3xl font-bold tracking-normal text-gray-950 dark:text-white sm:text-4xl">
                {{ currentDoc.title }}
              </h1>
              <p class="mt-3 max-w-3xl text-base leading-7 text-gray-600 dark:text-dark-300">
                {{ currentDoc.description }}
              </p>
            </header>

            <div
              ref="markdownContainer"
              class="docs-content"
              v-html="renderedHtml"
            ></div>
          </article>

          <section
            v-else
            class="rounded-lg border border-gray-200 bg-white px-6 py-12 text-center dark:border-dark-700 dark:bg-dark-900"
          >
            <h1 class="text-2xl font-bold text-gray-950 dark:text-white">{{ uiText.notFoundTitle }}</h1>
            <p class="mx-auto mt-3 max-w-md text-sm leading-6 text-gray-600 dark:text-dark-300">
              {{ uiText.notFoundDescription }}
            </p>
            <RouterLink
              to="/docs"
              class="mt-6 inline-flex items-center justify-center rounded-lg bg-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-primary-600/20 transition hover:bg-primary-700"
            >
              {{ uiText.backToDocs }}
            </RouterLink>
          </section>
        </section>

        <aside class="hidden lg:block">
          <div class="sticky top-24 max-h-[calc(100vh-7rem)] overflow-y-auto">
            <h2 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">
              {{ uiText.pageToc }}
            </h2>
            <nav v-if="tocItems.length" class="mt-3 space-y-1">
              <button
                v-for="item in tocItems"
                :key="item.id"
                type="button"
                class="block w-full truncate rounded-md py-1.5 pr-2 text-left text-sm transition"
                :class="[
                  item.level === 1 ? 'pl-2' : item.level === 2 ? 'pl-4' : item.level === 3 ? 'pl-6' : 'pl-8',
                  activeHeadingId === item.id
                    ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
                    : 'text-gray-600 hover:bg-gray-100 hover:text-gray-950 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white',
                ]"
                @click="scrollToHeading(item.id)"
              >
                {{ item.text }}
              </button>
            </nav>
            <p v-else class="mt-3 text-sm text-gray-500 dark:text-dark-400">{{ uiText.emptyToc }}</p>
          </div>
        </aside>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import {
  defaultUserDocSlug,
  findUserDoc,
  normalizeUserDocLocale,
  userDocsByLocale,
  type UserDocEntry,
} from '@/docs/registry'
import i18n from '@/i18n'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

interface DocGroup {
  category: string
  docs: UserDocEntry[]
}

interface TocItem {
  id: string
  text: string
  level: number
}

const route = useRoute()
const appStore = useAppStore()

const markdownContainer = ref<HTMLElement | null>(null)
const renderedHtml = ref('')
const tocItems = ref<TocItem[]>([])
const activeHeadingId = ref('')
const mobileNavOpen = ref(false)
const settingsLoading = ref(false)

let scrollRafId = 0

marked.setOptions({
  breaks: true,
  gfm: true,
})

const routeSlug = computed(() => {
  const slug = route.params.slug
  return Array.isArray(slug) ? slug[0] : slug
})

const currentLocale = computed(() => normalizeUserDocLocale(String(i18n.global.locale.value)))
const localeDocs = computed(() => userDocsByLocale[currentLocale.value] ?? userDocsByLocale.zh)
const currentDoc = computed(() => findUserDoc(routeSlug.value, currentLocale.value))
const activeSlug = computed(() => currentDoc.value?.slug ?? routeSlug.value ?? defaultUserDocSlug)
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', {
  allowRelative: true,
  allowDataUrl: true,
}))
const uiText = computed(() => currentLocale.value === 'zh'
  ? {
      home: '首页',
      login: '登录',
      loadingSettings: '正在加载站点设置...',
      docNavigation: '文档导航',
      notFoundTitle: '文档不存在',
      notFoundDescription: '未找到当前文档，可能链接已变更或文档尚未发布。',
      backToDocs: '返回文档首页',
      pageToc: '页面目录',
      emptyToc: '暂无目录',
      copy: '复制',
      copied: '已复制',
      copyFailed: '复制失败',
    }
  : {
      home: 'Home',
      login: 'Log in',
      loadingSettings: 'Loading site settings...',
      docNavigation: 'Documentation',
      notFoundTitle: 'Document Not Found',
      notFoundDescription: 'The current document was not found. The link may have changed or the document has not been published.',
      backToDocs: 'Back to Docs',
      pageToc: 'On This Page',
      emptyToc: 'No sections',
      copy: 'Copy',
      copied: 'Copied',
      copyFailed: 'Copy failed',
    })

const groupedDocs = computed<DocGroup[]>(() => {
  const groups = new Map<string, UserDocEntry[]>()
  for (const doc of localeDocs.value) {
    const docs = groups.get(doc.category)
    if (docs) {
      docs.push(doc)
    } else {
      groups.set(doc.category, [doc])
    }
  }
  return Array.from(groups, ([category, docs]) => ({ category, docs }))
})

function generateHeadingId(text: string, index: number): string {
  const base = text
    .toLowerCase()
    .replace(/[^\w一-鿿]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return base ? `${base}-${index}` : `heading-${index}`
}

function getTextContent(html: string): string {
  const template = document.createElement('template')
  template.innerHTML = html
  return template.content.textContent?.trim() ?? ''
}

function findRenderedHeadingById(id: string): HTMLElement | null {
  const container = markdownContainer.value
  if (!container) return null
  const headings = container.querySelectorAll<HTMLElement>('h1, h2, h3, h4')
  return Array.from(headings).find((heading) => heading.id === id) ?? null
}

function renderMarkdown(doc: UserDocEntry | null) {
  tocItems.value = []
  activeHeadingId.value = ''
  renderedHtml.value = ''

  if (!doc) {
    return
  }

  const html = marked.parse(doc.content) as string
  const sanitized = DOMPurify.sanitize(html, {
    FORBID_TAGS: ['iframe', 'script'],
    FORBID_ATTR: ['onerror', 'onload', 'onclick'],
  })

  const template = document.createElement('template')
  template.innerHTML = sanitized

  const toc: TocItem[] = []
  template.content.querySelectorAll('h1, h2, h3, h4').forEach((heading, index) => {
    const level = Number(heading.tagName.slice(1))
    const text = heading.textContent?.trim() || `Section ${index + 1}`
    const id = generateHeadingId(text, index)
    heading.setAttribute('id', id)
    toc.push({ id, text, level })
  })

  template.content.querySelectorAll('a[href]').forEach((link) => {
    const href = link.getAttribute('href') || ''
    if (/^(https?:)?\/\//i.test(href) || /^[a-z][a-z0-9+.-]*:/i.test(href)) {
      link.setAttribute('target', '_blank')
      link.setAttribute('rel', 'noopener noreferrer')
    }
  })

  tocItems.value = toc
  renderedHtml.value = template.innerHTML

  nextTick(() => {
    injectCopyButtons()
    updateActiveHeading()
  })
}

function scrollToHeading(id: string) {
  const heading = findRenderedHeadingById(id)
  if (!heading) return
  heading.scrollIntoView({ behavior: 'smooth', block: 'start' })
  activeHeadingId.value = id
}

function updateActiveHeading() {
  const container = markdownContainer.value
  if (!container || tocItems.value.length === 0) return

  let current = tocItems.value[0]?.id ?? ''
  for (const item of tocItems.value) {
    const heading = findRenderedHeadingById(item.id)
    if (heading && heading.getBoundingClientRect().top <= 140) {
      current = item.id
    }
  }
  activeHeadingId.value = current
}

function onWindowScroll() {
  if (scrollRafId) return
  scrollRafId = window.requestAnimationFrame(() => {
    scrollRafId = 0
    updateActiveHeading()
  })
}

function injectCopyButtons() {
  const container = markdownContainer.value
  if (!container) return

  container.querySelectorAll('pre').forEach((pre) => {
    if (pre.querySelector('.copy-btn')) return

    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'copy-btn'
    button.textContent = uiText.value.copy
    button.addEventListener('click', async () => {
      const code = pre.querySelector('code')?.textContent ?? getTextContent(pre.innerHTML)
      try {
        await navigator.clipboard.writeText(code)
        button.textContent = uiText.value.copied
      } catch {
        button.textContent = uiText.value.copyFailed
      }
      window.setTimeout(() => {
        button.textContent = uiText.value.copy
      }, 1800)
    })
    pre.appendChild(button)
  })
}

watch(currentDoc, (doc) => {
  renderMarkdown(doc)
}, { immediate: true })

onMounted(async () => {
  window.addEventListener('scroll', onWindowScroll, { passive: true })

  if (!appStore.publicSettingsLoaded) {
    settingsLoading.value = true
    try {
      await appStore.fetchPublicSettings()
    } finally {
      settingsLoading.value = false
    }
  }

  await nextTick()
  updateActiveHeading()
})

onUnmounted(() => {
  window.removeEventListener('scroll', onWindowScroll)
  if (scrollRafId) {
    window.cancelAnimationFrame(scrollRafId)
  }
})
</script>

<style scoped>
.doc-nav {
  scrollbar-width: thin;
}

.docs-content {
  line-height: 1.75;
  overflow-wrap: anywhere;
  color: inherit;
}

.docs-content :deep(h1) {
  @apply mb-4 mt-8 scroll-mt-24 border-b border-gray-200 pb-3 text-3xl font-bold dark:border-dark-700;
}

.docs-content :deep(h2) {
  @apply mb-3 mt-7 scroll-mt-24 text-2xl font-bold;
}

.docs-content :deep(h3) {
  @apply mb-2 mt-6 scroll-mt-24 text-xl font-semibold;
}

.docs-content :deep(h4) {
  @apply mb-2 mt-5 scroll-mt-24 text-lg font-semibold;
}

.docs-content :deep(p) {
  @apply mb-4 text-gray-700 dark:text-dark-200;
}

.docs-content :deep(a) {
  @apply text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300 dark:hover:text-primary-200;
}

.docs-content :deep(ul) {
  @apply mb-4 list-disc pl-6;
}

.docs-content :deep(ol) {
  @apply mb-4 list-decimal pl-6;
}

.docs-content :deep(li) {
  @apply mb-1 text-gray-700 dark:text-dark-200;
}

.docs-content :deep(blockquote) {
  @apply my-5 border-l-4 border-gray-300 pl-4 text-gray-600 dark:border-dark-600 dark:text-dark-300;
}

.docs-content :deep(code) {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-sm dark:bg-dark-800;
}

.docs-content :deep(pre) {
  @apply relative my-5 overflow-x-auto rounded-lg bg-gray-950 p-4 text-gray-100;
}

.docs-content :deep(pre code) {
  @apply bg-transparent p-0 text-inherit;
}

.docs-content :deep(table) {
  @apply my-5 block w-full overflow-x-auto border-collapse;
}

.docs-content :deep(th) {
  @apply border border-gray-300 bg-gray-50 px-3 py-2 text-left font-semibold dark:border-dark-600 dark:bg-dark-800;
}

.docs-content :deep(td) {
  @apply border border-gray-300 px-3 py-2 dark:border-dark-600;
}

.docs-content :deep(img) {
  @apply my-5 h-auto max-w-full rounded-lg;
}

.docs-content :deep(hr) {
  @apply my-7 border-gray-200 dark:border-dark-700;
}

:deep(.copy-btn) {
  position: absolute;
  top: 8px;
  right: 8px;
  border: 1px solid rgba(255, 255, 255, 0.2);
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.15);
  color: #e2e8f0;
  cursor: pointer;
  font-family: inherit;
  font-size: 12px;
  line-height: 1.4;
  opacity: 0;
  padding: 4px 10px;
  transition: opacity 0.2s, background 0.2s;
}

:deep(.copy-btn:hover) {
  background: rgba(255, 255, 255, 0.25);
}

.docs-content :deep(pre:hover .copy-btn),
.docs-content :deep(.copy-btn:focus-visible) {
  opacity: 1;
}
</style>
