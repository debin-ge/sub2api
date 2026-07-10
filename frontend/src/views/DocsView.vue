<template>
  <div class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <header class="sticky top-0 z-30 border-b border-gray-200 bg-white/95 backdrop-blur dark:border-dark-800 dark:bg-dark-900/95">
      <div
        class="absolute inset-x-0 bottom-0 h-0.5 origin-left bg-primary-500 transition-transform duration-150 dark:bg-primary-400"
        :style="{ transform: `scaleX(${readingProgress})` }"
        aria-hidden="true"
      ></div>
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
          <LocaleSwitcher />
          <RouterLink
            v-if="authStore.user"
            :to="dashboardPath"
            class="inline-flex items-center justify-center rounded-lg bg-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-primary-600/20 transition hover:bg-primary-700"
          >
            {{ uiText.dashboard }}
          </RouterLink>
          <RouterLink
            v-else
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
          <div class="mb-5">
            <label :for="docsSearchInputId" class="sr-only">{{ uiText.searchLabel }}</label>
            <div class="group relative">
              <svg
                class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400 transition group-focus-within:text-primary-500 dark:text-dark-400 dark:group-focus-within:text-primary-300"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
              >
                <circle cx="11" cy="11" r="8" />
                <path d="m21 21-4.3-4.3" />
              </svg>
              <input
                :id="docsSearchInputId"
                ref="searchInputRef"
                v-model="searchQuery"
                type="search"
                :aria-label="uiText.searchLabel"
                :placeholder="uiText.searchPlaceholder"
                class="doc-search-input h-9 w-full rounded-lg border-0 bg-gray-200/60 pl-9 pr-9 text-sm text-gray-900 outline-none ring-1 ring-inset ring-transparent transition duration-150 placeholder:text-gray-400 hover:bg-gray-200/90 focus:bg-white focus:shadow-sm focus:ring-2 focus:ring-primary-500/60 dark:bg-dark-800/80 dark:text-white dark:placeholder:text-dark-400 dark:hover:bg-dark-800 dark:focus:bg-dark-900 dark:focus:ring-primary-400/50"
                @keydown.escape="searchQuery = ''"
              />
              <kbd
                v-if="!searchQuery"
                class="pointer-events-none absolute right-2.5 top-1/2 hidden h-5 min-w-[1.25rem] -translate-y-1/2 items-center justify-center rounded border border-gray-300/80 bg-white px-1 font-sans text-[11px] font-medium text-gray-400 transition group-focus-within:opacity-0 dark:border-dark-600 dark:bg-dark-900 dark:text-dark-400 lg:inline-flex"
                aria-hidden="true"
              >/</kbd>
              <button
                v-if="searchQuery"
                type="button"
                class="absolute right-2 top-1/2 inline-flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded-md text-gray-400 transition hover:bg-gray-200 hover:text-gray-700 focus:outline-none focus:ring-2 focus:ring-primary-500/30 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-white"
                :aria-label="uiText.clearSearch"
                @click="searchQuery = ''"
              >
                <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <path d="M18 6 6 18" />
                  <path d="m6 6 12 12" />
                </svg>
              </button>
            </div>
          </div>

          <section
            v-if="isSearchingDocs"
            data-testid="docs-search-results"
            class="border-b border-gray-200 pb-6 dark:border-dark-800 lg:border-b-0 lg:pb-0"
          >
            <p class="px-2 text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-dark-400">
              {{ searchResults.length }} {{ uiText.searchResults }}
            </p>
            <div v-if="searchResults.length" class="mt-2 space-y-1.5">
              <RouterLink
                v-for="result in searchResults"
                :key="result.doc.slug"
                :to="docPath(result.doc)"
                class="group block rounded-lg border border-transparent px-2.5 py-2.5 transition hover:border-gray-200 hover:bg-white hover:shadow-sm dark:hover:border-dark-700 dark:hover:bg-dark-900"
                @click="mobileNavOpen = false"
              >
                <span class="flex items-center gap-2">
                  <span class="min-w-0 flex-1 truncate text-sm font-semibold text-gray-900 transition group-hover:text-primary-700 dark:text-white dark:group-hover:text-primary-300">
                    {{ resolveDocText(result.doc.title) }}
                  </span>
                  <span class="flex-shrink-0 rounded-full bg-gray-100 px-2 py-0.5 text-[10px] font-medium text-gray-500 ring-1 ring-inset ring-gray-200/70 dark:bg-dark-800 dark:text-dark-300 dark:ring-dark-700">
                    {{ resolveDocText(result.doc.category) }}
                  </span>
                </span>
                <span class="mt-1 block line-clamp-2 text-xs leading-5 text-gray-500 dark:text-dark-300">
                  <template v-for="(segment, index) in excerptSegments(result.excerpt)" :key="index">
                    <mark v-if="segment.hit" class="rounded-sm bg-primary-100 px-0.5 font-medium text-primary-800 dark:bg-primary-500/25 dark:text-primary-200">{{ segment.text }}</mark>
                    <template v-else>{{ segment.text }}</template>
                  </template>
                </span>
              </RouterLink>
            </div>
            <div v-else class="mt-4 flex flex-col items-center gap-2 rounded-lg border border-dashed border-gray-200 px-4 py-6 text-center dark:border-dark-700">
              <svg class="h-5 w-5 text-gray-300 dark:text-dark-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <circle cx="11" cy="11" r="8" />
                <path d="m21 21-4.3-4.3" />
                <path d="M8 11h6" />
              </svg>
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ uiText.noSearchResults }}</p>
            </div>
          </section>

          <nav
            v-else
            data-testid="docs-nav-groups"
            class="space-y-6 border-b border-gray-200 pb-6 dark:border-dark-800 lg:border-b-0 lg:pb-0"
          >
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

          <article v-if="displayDoc" class="min-w-0">
            <header class="mb-8 border-b border-gray-200 pb-6 dark:border-dark-800">
              <div class="flex flex-wrap items-center gap-3">
                <span class="inline-flex items-center rounded-full bg-primary-50 px-2.5 py-0.5 text-xs font-semibold text-primary-700 ring-1 ring-inset ring-primary-600/15 dark:bg-primary-500/10 dark:text-primary-300 dark:ring-primary-400/20">
                  {{ displayDoc.category }}
                </span>
                <span class="inline-flex items-center gap-1 text-xs text-gray-400 dark:text-dark-400">
                  <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                    <circle cx="12" cy="12" r="10" />
                    <path d="M12 6v6l4 2" />
                  </svg>
                  {{ readingTimeText }}
                </span>
              </div>
              <h1 class="mt-3 break-words text-3xl font-bold tracking-tight text-gray-950 dark:text-white sm:text-4xl">
                {{ displayDoc.title }}
              </h1>
              <p class="mt-3 max-w-3xl text-base leading-7 text-gray-600 dark:text-dark-300">
                {{ displayDoc.description }}
              </p>
            </header>

            <div
              ref="markdownContainer"
              class="docs-content"
              v-html="renderedHtml"
              @click="onDocsContentClick"
              @keydown="onDocsContentKeydown"
            ></div>

            <nav
              v-if="prevDoc || nextDoc"
              class="mt-12 grid gap-3 border-t border-gray-200 pt-8 dark:border-dark-800 sm:grid-cols-2"
              :aria-label="uiText.pagerLabel"
            >
              <RouterLink
                v-if="prevDoc"
                :to="docPath(prevDoc)"
                class="group flex flex-col rounded-xl border border-gray-200 bg-white px-5 py-4 transition hover:border-primary-300 hover:shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:hover:border-primary-500/40"
              >
                <span class="flex items-center gap-1 text-xs font-medium text-gray-400 transition group-hover:text-primary-600 dark:text-dark-400 dark:group-hover:text-primary-300">
                  <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m15 18-6-6 6-6" /></svg>
                  {{ uiText.prevDoc }}
                </span>
                <span class="mt-1.5 truncate text-sm font-semibold text-gray-900 transition group-hover:text-primary-700 dark:text-white dark:group-hover:text-primary-300">
                  {{ resolveDocText(prevDoc.title) }}
                </span>
              </RouterLink>
              <span v-else class="hidden sm:block" aria-hidden="true"></span>
              <RouterLink
                v-if="nextDoc"
                :to="docPath(nextDoc)"
                class="group flex flex-col items-end rounded-xl border border-gray-200 bg-white px-5 py-4 text-right transition hover:border-primary-300 hover:shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:hover:border-primary-500/40"
              >
                <span class="flex items-center gap-1 text-xs font-medium text-gray-400 transition group-hover:text-primary-600 dark:text-dark-400 dark:group-hover:text-primary-300">
                  {{ uiText.nextDoc }}
                  <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg>
                </span>
                <span class="mt-1.5 w-full truncate text-sm font-semibold text-gray-900 transition group-hover:text-primary-700 dark:text-white dark:group-hover:text-primary-300">
                  {{ resolveDocText(nextDoc.title) }}
                </span>
              </RouterLink>
            </nav>
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
            <nav v-if="tocItems.length" class="mt-3 border-l border-gray-200 dark:border-dark-700">
              <button
                v-for="item in tocItems"
                :key="item.id"
                type="button"
                class="-ml-px block w-full truncate border-l-2 py-1.5 pr-2 text-left text-[13px] leading-5 transition"
                :class="[
                  item.level <= 2 ? 'pl-3' : item.level === 3 ? 'pl-6' : 'pl-9',
                  activeHeadingId === item.id
                    ? 'border-primary-500 font-medium text-primary-700 dark:border-primary-400 dark:text-primary-300'
                    : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-950 dark:text-dark-300 dark:hover:border-dark-500 dark:hover:text-white',
                ]"
                @click="scrollToHeading(item.id)"
              >
                {{ item.text }}
              </button>
            </nav>
            <p v-else class="mt-3 text-sm text-gray-500 dark:text-dark-400">{{ uiText.emptyToc }}</p>
            <button
              v-if="tocItems.length"
              type="button"
              class="mt-5 inline-flex items-center gap-1.5 text-xs font-medium text-gray-400 transition hover:text-gray-900 dark:text-dark-400 dark:hover:text-white"
              @click="scrollToTop"
            >
              <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m18 15-6-6-6 6" /></svg>
              {{ uiText.backToTop }}
            </button>
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
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import { useAppStore, useAuthStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'
import {
  activateDocTab,
  enhanceCallouts,
  enhanceClientCards,
  enhanceStepHeadings,
  groupCodeTabs,
  renderDocCode,
  wrapTables,
} from '@/utils/docsEnhance'

interface DocGroup {
  category: string
  docs: UserDocEntry[]
}

interface TocItem {
  id: string
  text: string
  level: number
}

interface DocSearchResult {
  doc: UserDocEntry
  excerpt: string
  score: number
}

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()

const markdownContainer = ref<HTMLElement | null>(null)
const renderedHtml = ref('')
const tocItems = ref<TocItem[]>([])
const activeHeadingId = ref('')
const mobileNavOpen = ref(false)
const settingsLoading = ref(false)
const readingProgress = ref(0)
const searchQuery = ref('')
const searchInputRef = ref<HTMLInputElement | null>(null)

let scrollRafId = 0

marked.setOptions({
  breaks: true,
  gfm: true,
})
marked.use({ renderer: { code: renderDocCode } })

const PREFERRED_TAB_KEY = 'docs-preferred-tab'
const docsSearchInputId = 'docs-search-input'

function getPreferredDocTab(): string | null {
  try {
    return sessionStorage.getItem(PREFERRED_TAB_KEY)
  } catch {
    return null
  }
}

function setPreferredDocTab(label: string) {
  try {
    sessionStorage.setItem(PREFERRED_TAB_KEY, label)
  } catch {
    // sessionStorage unavailable — tab sync still works within the page
  }
}

const routeSlug = computed(() => {
  const slug = route.params.slug
  return Array.isArray(slug) ? slug[0] : slug
})

const currentLocale = computed(() => normalizeUserDocLocale(String(i18n.global.locale.value)))
const localeDocs = computed(() => userDocsByLocale[currentLocale.value] ?? userDocsByLocale.zh)
const currentDoc = computed(() => findUserDoc(routeSlug.value, currentLocale.value))
const activeSlug = computed(() => currentDoc.value?.slug ?? routeSlug.value ?? defaultUserDocSlug)
const dashboardPath = computed(() => authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteBaseUrl = computed(() => normalizeBaseUrl(appStore.cachedPublicSettings?.api_base_url || appStore.apiBaseUrl || ''))
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', {
  allowRelative: true,
  allowDataUrl: true,
}))
const displayDoc = computed<UserDocEntry | null>(() => {
  const doc = currentDoc.value
  if (!doc) return null
  return {
    ...doc,
    title: resolveDocText(doc.title),
    description: resolveDocText(doc.description),
    category: resolveDocText(doc.category),
  }
})
const uiText = computed(() => currentLocale.value === 'zh'
  ? {
      home: '首页',
      login: '登录',
      dashboard: '返回控制台',
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
      cardBaseUrl: 'Base URL',
      cardConfigFile: '配置文件',
      readingTime: '分钟读完',
      prevDoc: '上一篇',
      nextDoc: '下一篇',
      pagerLabel: '文档翻页',
      backToTop: '回到顶部',
      searchLabel: '搜索文档',
      searchPlaceholder: '搜索文档...',
      searchResults: '个结果',
      noSearchResults: '未找到匹配文档',
      clearSearch: '清空搜索',
      calloutNote: '说明',
      calloutTip: '提示',
      calloutImportant: '重要',
      calloutWarning: '注意',
      calloutCaution: '警告',
    }
  : {
      home: 'Home',
      login: 'Log in',
      dashboard: 'Back to Dashboard',
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
      cardBaseUrl: 'Base URL',
      cardConfigFile: 'Config file',
      readingTime: 'min read',
      prevDoc: 'Previous',
      nextDoc: 'Next',
      pagerLabel: 'Document pagination',
      backToTop: 'Back to top',
      searchLabel: 'Search docs',
      searchPlaceholder: 'Search docs...',
      searchResults: 'results',
      noSearchResults: 'No matching docs',
      clearSearch: 'Clear search',
      calloutNote: 'Note',
      calloutTip: 'Tip',
      calloutImportant: 'Important',
      calloutWarning: 'Warning',
      calloutCaution: 'Caution',
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

const normalizedSearchQuery = computed(() => normalizeSearchText(searchQuery.value.trim()))
const isSearchingDocs = computed(() => normalizedSearchQuery.value.length > 0)
const searchResults = computed<DocSearchResult[]>(() => {
  const query = normalizedSearchQuery.value
  if (!query) return []

  return localeDocs.value
    .map((doc) => {
      const title = resolveDocText(doc.title)
      const description = resolveDocText(doc.description)
      const category = resolveDocText(doc.category)
      const content = stripMarkdown(resolveDocText(doc.content))
      const titleMatch = normalizeSearchText(title).includes(query)
      const descriptionMatch = normalizeSearchText(description).includes(query)
      const categoryMatch = normalizeSearchText(category).includes(query)
      const contentMatch = normalizeSearchText(content).includes(query)

      if (!titleMatch && !descriptionMatch && !categoryMatch && !contentMatch) {
        return null
      }

      return {
        doc,
        excerpt: makeSearchExcerpt(`${description} ${content}`, query),
        score: titleMatch ? 0 : descriptionMatch ? 1 : categoryMatch ? 2 : 3,
      }
    })
    .filter((result): result is DocSearchResult => result !== null)
    .sort((a, b) => a.score - b.score || localeDocs.value.indexOf(a.doc) - localeDocs.value.indexOf(b.doc))
})

const readingTimeText = computed(() => {
  const content = currentDoc.value?.content ?? ''
  const prose = content.replace(/```[\s\S]*?```/g, ' ')
  const cjkChars = (prose.match(/[一-鿿]/g) ?? []).length
  const words = prose.replace(/[一-鿿]/g, ' ').split(/\s+/).filter(Boolean).length
  const minutes = Math.max(1, Math.round(cjkChars / 400 + words / 180))
  return currentLocale.value === 'zh'
    ? `约 ${minutes} ${uiText.value.readingTime}`
    : `${minutes} ${uiText.value.readingTime}`
})

const prevDoc = computed<UserDocEntry | null>(() => {
  const docs = localeDocs.value
  const index = docs.findIndex((doc) => doc.slug === activeSlug.value)
  return index > 0 ? docs[index - 1] : null
})

const nextDoc = computed<UserDocEntry | null>(() => {
  const docs = localeDocs.value
  const index = docs.findIndex((doc) => doc.slug === activeSlug.value)
  return index >= 0 && index < docs.length - 1 ? docs[index + 1] : null
})

function docPath(doc: UserDocEntry): string {
  return doc.slug === defaultUserDocSlug ? '/docs' : `/docs/${doc.slug}`
}

function generateHeadingId(text: string, index: number): string {
  const base = text
    .toLowerCase()
    .replace(/[^\w一-鿿]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return base ? `${base}-${index}` : `heading-${index}`
}

function normalizeBaseUrl(value: string): string {
  const trimmed = value.trim()
  const fallback = typeof window === 'undefined' ? '/' : `${window.location.origin}/`
  const base = trimmed || fallback
  return base.endsWith('/') ? base : `${base}/`
}

function resolveDocText(value: string): string {
  return value
    .replace(/\{\{SITE_NAME\}\}/g, siteName.value)
    .replace(/\{\{BASE_URL\}\}/g, siteBaseUrl.value)
    .replace(/Sub2API/g, siteName.value)
    .replace(/https:\/\/tiktoken\.net\//g, siteBaseUrl.value)
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function resolveDocHtml(value: string): string {
  const escapedSiteName = escapeHtml(siteName.value)
  const escapedBaseUrl = escapeHtml(siteBaseUrl.value)
  return value
    .replace(/\{\{SITE_NAME\}\}/g, escapedSiteName)
    .replace(/\{\{BASE_URL\}\}/g, escapedBaseUrl)
    .replace(/Sub2API/g, escapedSiteName)
    .replace(/https:\/\/tiktoken\.net\//g, escapedBaseUrl)
}

function normalizeSearchText(value: string): string {
  return value.toLocaleLowerCase()
}

function stripMarkdown(value: string): string {
  return value
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/[#>*_|~-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}

interface ExcerptSegment {
  text: string
  hit: boolean
}

/** Split an excerpt into plain/highlighted segments for the current query. */
function excerptSegments(excerpt: string): ExcerptSegment[] {
  const query = normalizedSearchQuery.value
  if (!excerpt) return []
  if (!query) return [{ text: excerpt, hit: false }]

  const segments: ExcerptSegment[] = []
  const normalized = normalizeSearchText(excerpt)
  let cursor = 0
  let index = normalized.indexOf(query)
  while (index !== -1) {
    if (index > cursor) {
      segments.push({ text: excerpt.slice(cursor, index), hit: false })
    }
    segments.push({ text: excerpt.slice(index, index + query.length), hit: true })
    cursor = index + query.length
    index = normalized.indexOf(query, cursor)
  }
  if (cursor < excerpt.length) {
    segments.push({ text: excerpt.slice(cursor), hit: false })
  }
  return segments
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable
}

function onGlobalKeydown(event: KeyboardEvent) {
  if (event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey) return
  if (isTypingTarget(event.target)) return
  event.preventDefault()
  searchInputRef.value?.focus()
}

function makeSearchExcerpt(value: string, query: string): string {
  const compact = value.replace(/\s+/g, ' ').trim()
  if (!compact) return ''

  const index = normalizeSearchText(compact).indexOf(query)
  if (index === -1) {
    return compact.length > 120 ? `${compact.slice(0, 120)}...` : compact
  }

  const start = Math.max(0, index - 36)
  const end = Math.min(compact.length, index + query.length + 72)
  const prefix = start > 0 ? '...' : ''
  const suffix = end < compact.length ? '...' : ''
  return `${prefix}${compact.slice(start, end)}${suffix}`
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
  template.innerHTML = resolveDocHtml(sanitized)

  // The article header already renders the doc title; drop a leading H1
  // that duplicates it so the body starts with the actual content.
  const leadingHeading = template.content.firstElementChild
  if (leadingHeading?.tagName === 'H1' && leadingHeading.textContent?.trim() === doc.title.trim()) {
    leadingHeading.remove()
  }

  const toc: TocItem[] = []
  template.content.querySelectorAll('h1, h2, h3, h4').forEach((heading, index) => {
    const level = Number(heading.tagName.slice(1))
    const text = heading.textContent?.trim() || `Section ${index + 1}`
    const id = generateHeadingId(text, index)
    heading.setAttribute('id', id)
    toc.push({ id, text, level })

    const anchor = document.createElement('a')
    anchor.className = 'heading-anchor'
    anchor.href = `#${id}`
    anchor.setAttribute('aria-label', text)
    anchor.textContent = '#'
    heading.appendChild(anchor)
  })

  template.content.querySelectorAll('a[href]').forEach((link) => {
    const href = link.getAttribute('href') || ''
    if (/^(https?:)?\/\//i.test(href) || /^[a-z][a-z0-9+.-]*:/i.test(href)) {
      link.setAttribute('target', '_blank')
      link.setAttribute('rel', 'noopener noreferrer')
    }
  })

  enhanceCallouts(template.content, {
    note: uiText.value.calloutNote,
    tip: uiText.value.calloutTip,
    important: uiText.value.calloutImportant,
    warning: uiText.value.calloutWarning,
    caution: uiText.value.calloutCaution,
  })
  enhanceClientCards(template.content, {
    baseUrl: uiText.value.cardBaseUrl,
    configFile: uiText.value.cardConfigFile,
    copy: uiText.value.copy,
  })
  groupCodeTabs(template.content, getPreferredDocTab())
  wrapTables(template.content)
  enhanceStepHeadings(template.content)

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

function scrollToTop() {
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

function updateReadingProgress() {
  const doc = document.documentElement
  const scrollable = doc.scrollHeight - window.innerHeight
  readingProgress.value = scrollable > 0 ? Math.min(1, window.scrollY / scrollable) : 0
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
    updateReadingProgress()
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

function selectDocTab(label: string) {
  const container = markdownContainer.value
  if (!container) return
  activateDocTab(container, label)
  setPreferredDocTab(label)
}

async function copyCardValue(button: HTMLButtonElement) {
  const value = button.getAttribute('data-copy') ?? ''
  try {
    await navigator.clipboard.writeText(value)
    button.textContent = uiText.value.copied
  } catch {
    button.textContent = uiText.value.copyFailed
  }
  window.setTimeout(() => {
    button.textContent = uiText.value.copy
  }, 1800)
}

function onDocsContentClick(event: MouseEvent) {
  const target = event.target as HTMLElement | null
  if (!target) return

  const headingAnchor = target.closest<HTMLAnchorElement>('a.heading-anchor')
  if (headingAnchor) {
    event.preventDefault()
    const id = decodeURIComponent(headingAnchor.getAttribute('href')?.slice(1) ?? '')
    if (id) {
      scrollToHeading(id)
      window.history.replaceState(window.history.state, '', `#${id}`)
    }
    return
  }

  const tabButton = target.closest<HTMLButtonElement>('.doc-tab-btn')
  if (tabButton) {
    const label = tabButton.getAttribute('data-tab')
    if (label) selectDocTab(label)
    return
  }

  const copyButton = target.closest<HTMLButtonElement>('.client-copy-btn')
  if (copyButton) {
    void copyCardValue(copyButton)
  }
}

function onDocsContentKeydown(event: KeyboardEvent) {
  if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
  const target = event.target as HTMLElement | null
  const current = target?.closest<HTMLButtonElement>('.doc-tab-btn')
  if (!current) return

  const bar = current.closest('.doc-tabs-bar')
  if (!bar) return
  const buttons = Array.from(bar.querySelectorAll<HTMLButtonElement>('.doc-tab-btn'))
  const index = buttons.indexOf(current)
  if (index === -1) return

  event.preventDefault()
  const nextIndex = event.key === 'ArrowLeft'
    ? (index - 1 + buttons.length) % buttons.length
    : (index + 1) % buttons.length
  const nextButton = buttons[nextIndex]
  const label = nextButton.getAttribute('data-tab')
  if (label) selectDocTab(label)
  nextButton.focus()
}

watch(displayDoc, (doc) => {
  renderMarkdown(doc)
}, { immediate: true })

onMounted(async () => {
  window.addEventListener('scroll', onWindowScroll, { passive: true })
  window.addEventListener('keydown', onGlobalKeydown)

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
  updateReadingProgress()

  const hash = decodeURIComponent(window.location.hash.slice(1))
  if (hash) {
    findRenderedHeadingById(hash)?.scrollIntoView({ block: 'start' })
  }
})

onUnmounted(() => {
  window.removeEventListener('scroll', onWindowScroll)
  window.removeEventListener('keydown', onGlobalKeydown)
  if (scrollRafId) {
    window.cancelAnimationFrame(scrollRafId)
  }
})
</script>

<style scoped>
.doc-nav {
  scrollbar-width: thin;
}

/* The custom clear button replaces the native one. */
.doc-search-input::-webkit-search-cancel-button,
.doc-search-input::-webkit-search-decoration {
  -webkit-appearance: none;
  appearance: none;
}

.docs-content {
  line-height: 1.75;
  overflow-wrap: anywhere;
  color: inherit;
}

.docs-content :deep(h1) {
  @apply mb-4 mt-10 scroll-mt-24 border-b border-gray-200 pb-3 text-3xl font-bold tracking-tight dark:border-dark-700;
}

.docs-content :deep(h2) {
  @apply mb-4 mt-10 scroll-mt-24 text-2xl font-bold tracking-tight;
}

.docs-content :deep(h3) {
  @apply mb-3 mt-7 scroll-mt-24 text-lg font-semibold;
}

.docs-content :deep(h4) {
  @apply mb-2 mt-6 scroll-mt-24 text-base font-semibold;
}

.docs-content :deep(> :first-child) {
  @apply mt-0;
}

.docs-content :deep(.heading-anchor) {
  @apply ml-2 align-middle text-base font-normal text-gray-300 no-underline opacity-0 transition hover:text-primary-600 dark:text-dark-500 dark:hover:text-primary-300;
}

.docs-content :deep(h1:hover .heading-anchor),
.docs-content :deep(h2:hover .heading-anchor),
.docs-content :deep(h3:hover .heading-anchor),
.docs-content :deep(h4:hover .heading-anchor),
.docs-content :deep(.heading-anchor:focus-visible) {
  @apply opacity-100;
}

.docs-content :deep(.doc-step-num) {
  @apply mr-3 inline-flex h-8 w-8 items-center justify-center rounded-lg bg-primary-600/10 align-[-0.2em] text-base font-bold text-primary-700 ring-1 ring-inset ring-primary-600/20 dark:bg-primary-500/10 dark:text-primary-300 dark:ring-primary-400/25;
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
  @apply my-6 rounded-r-lg border-l-2 border-gray-300 bg-gray-50 py-3 pl-4 pr-4 text-gray-600 dark:border-dark-500 dark:bg-dark-900 dark:text-dark-300;
}

.docs-content :deep(blockquote p:last-child) {
  @apply mb-0;
}

.docs-content :deep(code) {
  @apply rounded-md bg-gray-100 px-1.5 py-0.5 font-mono text-[0.85em] text-gray-800 ring-1 ring-inset ring-gray-200/80 dark:bg-dark-800 dark:text-dark-100 dark:ring-dark-700;
}

.docs-content :deep(pre) {
  @apply relative my-6 overflow-x-auto rounded-xl bg-gray-950 p-4 text-[13px] leading-6 text-gray-100 ring-1 ring-white/10;
}

.docs-content :deep(pre[data-lang]) {
  @apply pt-10;
}

.docs-content :deep(pre[data-lang]::before) {
  content: attr(data-lang);
  @apply pointer-events-none absolute left-4 top-3 font-mono text-[11px] uppercase tracking-wider text-gray-500;
}

.docs-content :deep(pre code) {
  @apply bg-transparent p-0 text-inherit ring-0;
}

.docs-content :deep(.doc-table-wrap) {
  @apply my-6 overflow-x-auto rounded-xl ring-1 ring-gray-200 dark:ring-dark-700;
}

.docs-content :deep(table) {
  @apply m-0 w-full border-collapse text-sm;
}

.docs-content :deep(th) {
  @apply whitespace-nowrap border-b border-gray-200 bg-gray-50 px-4 py-2.5 text-left text-xs font-semibold uppercase tracking-wide text-gray-500 dark:border-dark-700 dark:bg-dark-800/70 dark:text-dark-300;
}

.docs-content :deep(td) {
  @apply border-b border-gray-100 px-4 py-2.5 align-top text-gray-700 dark:border-dark-800 dark:text-dark-200;
}

.docs-content :deep(tbody tr:last-child td) {
  @apply border-b-0;
}

.docs-content :deep(tbody tr:hover td) {
  @apply bg-gray-50/70 dark:bg-dark-800/40;
}

.docs-content :deep(img) {
  @apply my-5 h-auto max-w-full rounded-lg;
}

.docs-content :deep(hr) {
  @apply my-7 border-gray-200 dark:border-dark-700;
}

/* ── Callouts ────────────────────────────────────────────────────── */

.docs-content :deep(.doc-callout) {
  @apply my-6 rounded-xl border px-4 py-3.5 text-sm leading-6;
}

.docs-content :deep(.doc-callout-title) {
  @apply mb-1.5 flex items-center gap-2 text-sm font-semibold;
}

.docs-content :deep(.doc-callout-title svg) {
  @apply h-4 w-4 flex-shrink-0;
}

.docs-content :deep(.doc-callout-body p) {
  @apply mb-2 text-inherit;
}

.docs-content :deep(.doc-callout-body > :last-child) {
  @apply mb-0;
}

.docs-content :deep(.doc-callout-body li) {
  @apply text-inherit;
}

.docs-content :deep(.doc-callout-note) {
  @apply border-blue-200 bg-blue-50/70 text-blue-900 dark:border-blue-400/25 dark:bg-blue-500/10 dark:text-blue-100;
}

.docs-content :deep(.doc-callout-tip) {
  @apply border-emerald-200 bg-emerald-50/70 text-emerald-900 dark:border-emerald-400/25 dark:bg-emerald-500/10 dark:text-emerald-100;
}

.docs-content :deep(.doc-callout-important) {
  @apply border-violet-200 bg-violet-50/70 text-violet-900 dark:border-violet-400/25 dark:bg-violet-500/10 dark:text-violet-100;
}

.docs-content :deep(.doc-callout-warning) {
  @apply border-amber-200 bg-amber-50/70 text-amber-900 dark:border-amber-400/25 dark:bg-amber-500/10 dark:text-amber-100;
}

.docs-content :deep(.doc-callout-caution) {
  @apply border-red-200 bg-red-50/70 text-red-900 dark:border-red-400/25 dark:bg-red-500/10 dark:text-red-100;
}

/* ── Code tab groups ─────────────────────────────────────────────── */

.docs-content :deep(.doc-tabs) {
  @apply my-6 overflow-hidden rounded-xl border border-gray-200 dark:border-dark-700;
}

.docs-content :deep(.doc-tabs-bar) {
  @apply flex flex-wrap gap-1 border-b border-gray-200 bg-gray-50 px-2 pt-2 dark:border-dark-700 dark:bg-dark-900;
}

.docs-content :deep(.doc-tab-btn) {
  @apply rounded-t-md border-b-2 border-transparent px-3 py-1.5 text-sm font-medium text-gray-500 transition hover:text-gray-900 dark:text-dark-300 dark:hover:text-white;
}

.docs-content :deep(.doc-tab-btn.active) {
  @apply border-primary-500 text-primary-700 dark:text-primary-300;
}

.docs-content :deep(.doc-tabs-panels pre) {
  @apply my-0 rounded-none ring-0;
}

/* Inside a tab group the active tab already names the language. */
.docs-content :deep(.doc-tabs-panels pre[data-lang]) {
  @apply pt-4;
}

.docs-content :deep(.doc-tabs-panels pre[data-lang]::before) {
  display: none;
}

.docs-content :deep(.doc-tabs-panels pre[hidden]) {
  display: none;
}

/* ── Client cards ────────────────────────────────────────────────── */

.docs-content :deep(.client-card) {
  @apply my-6 overflow-hidden rounded-xl bg-white ring-1 ring-gray-200 dark:bg-dark-900 dark:ring-dark-700;
}

.docs-content :deep(.client-card-head) {
  @apply border-b border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-800/60;
}

.docs-content :deep(.client-card-title-row) {
  @apply flex flex-wrap items-center gap-2;
}

.docs-content :deep(.client-card-name) {
  @apply text-base font-semibold text-gray-950 no-underline dark:text-white;
}

.docs-content :deep(a.client-card-name:hover) {
  @apply text-primary-700 underline dark:text-primary-300;
}

.docs-content :deep(.client-pill) {
  @apply inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium;
}

.docs-content :deep(.client-card-meta) {
  @apply mt-2 space-y-1;
}

.docs-content :deep(.client-card-meta-row) {
  @apply flex flex-wrap items-center gap-2 text-sm;
}

.docs-content :deep(.client-card-meta-label) {
  @apply w-20 flex-shrink-0 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400;
}

.docs-content :deep(.client-card-meta-value) {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs text-gray-800 dark:bg-dark-800 dark:text-dark-100;
}

.docs-content :deep(.client-copy-btn) {
  @apply rounded border border-gray-300 px-2 py-0.5 text-xs text-gray-600 transition hover:bg-gray-100 hover:text-gray-900 dark:border-dark-600 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white;
}

.docs-content :deep(.client-card-body) {
  @apply px-4 py-1;
}

:deep(.copy-btn) {
  position: absolute;
  top: 8px;
  right: 8px;
  border: 1px solid rgba(255, 255, 255, 0.15);
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.08);
  color: #94a3b8;
  cursor: pointer;
  font-family: inherit;
  font-size: 12px;
  line-height: 1.4;
  opacity: 0.75;
  padding: 4px 10px;
  transition: opacity 0.2s, background 0.2s, color 0.2s;
}

:deep(.copy-btn:hover) {
  background: rgba(255, 255, 255, 0.18);
  color: #e2e8f0;
}

.docs-content :deep(pre:hover .copy-btn),
.docs-content :deep(.copy-btn:focus-visible) {
  opacity: 1;
}
</style>
