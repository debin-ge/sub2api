<template>
  <div class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <SiteHeader current="docs" :progress="readingProgress" />

    <main class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
      <button
        type="button"
        class="mb-4 flex w-full items-center justify-between rounded-lg border border-gray-200 bg-white px-4 py-3 text-left text-sm font-semibold text-gray-900 shadow-sm transition hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-900 dark:text-white dark:hover:bg-dark-800 lg:hidden"
        :aria-expanded="mobileNavOpen"
        @click="mobileNavOpen = !mobileNavOpen"
      >
        <span>{{ activeTab === 'apps' ? uiText.appsNavigation : uiText.docNavigation }}</span>
        <span class="text-lg leading-none text-gray-500 dark:text-dark-300">{{ mobileNavOpen ? '-' : '+' }}</span>
      </button>

      <div class="grid gap-8 lg:grid-cols-[240px_minmax(0,1fr)_220px]">
        <aside
          class="doc-nav lg:sticky lg:top-24 lg:block lg:max-h-[calc(100vh-7rem)] lg:overflow-y-auto"
          :class="{ hidden: !mobileNavOpen }"
        >
          <!-- Tab strip: 文档 | 应用集成 -->
          <div
            role="tablist"
            data-testid="section-tabs"
            class="mb-5 grid grid-cols-2 gap-1 rounded-xl bg-gray-100 p-1 dark:bg-dark-800/80"
          >
            <RouterLink
              to="/docs"
              role="tab"
              :aria-selected="activeTab === 'docs'"
              class="rounded-lg px-3 py-1.5 text-center text-sm font-medium transition"
              :class="activeTab === 'docs'
                ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-900 dark:text-primary-300'
                : 'text-gray-500 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'"
              @click="mobileNavOpen = false"
            >
              {{ uiText.tabDocs }}
            </RouterLink>
            <RouterLink
              to="/apps"
              role="tab"
              :aria-selected="activeTab === 'apps'"
              class="rounded-lg px-3 py-1.5 text-center text-sm font-medium transition"
              :class="activeTab === 'apps'
                ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-900 dark:text-primary-300'
                : 'text-gray-500 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'"
              @click="mobileNavOpen = false"
            >
              {{ uiText.tabApps }}
            </RouterLink>
          </div>

          <!-- ─── Docs tab sidebar ─── -->
          <template v-if="activeTab === 'docs'">
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
          </template>

          <!-- ─── Apps tab sidebar ─── -->
          <nav
            v-else
            data-testid="apps-nav-list"
            class="space-y-1 border-b border-gray-200 pb-6 dark:border-dark-800 lg:border-b-0 lg:pb-0"
          >
            <RouterLink
              v-for="app in localeApps"
              :key="app.slug"
              :to="`/apps/${app.slug}`"
              class="flex items-center gap-2.5 rounded-md px-2 py-2 text-sm transition"
              :class="app.slug === currentApp?.slug
                ? 'bg-primary-50 font-semibold text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
                : 'text-gray-600 hover:bg-gray-100 hover:text-gray-950 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white'"
              @click="mobileNavOpen = false"
            >
              <span class="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-dark-200">
                <AppIcon :icon="app.icon" size="sm" />
              </span>
              <span class="min-w-0 flex-1 truncate">{{ app.name }}</span>
            </RouterLink>
          </nav>
        </aside>

        <section class="min-w-0">
          <div
            v-if="settingsLoading"
            class="mb-4 rounded-lg border border-gray-200 bg-white px-4 py-3 text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-400"
          >
            {{ uiText.loadingSettings }}
          </div>

          <!-- Apps landing (apps tab, no slug) -->
          <div v-if="showAppsLanding" class="min-w-0">
            <header class="mb-8">
              <span class="inline-block rounded-full bg-primary-50 px-2.5 py-0.5 text-xs font-semibold text-primary-700 ring-1 ring-inset ring-primary-600/15 dark:bg-primary-500/10 dark:text-primary-300 dark:ring-primary-400/20">
                {{ uiText.tabApps }}
              </span>
              <h1 class="mt-3 text-3xl font-bold tracking-tight text-gray-950 dark:text-white sm:text-4xl">
                {{ uiText.appsTitle }}
              </h1>
              <p class="mt-2 max-w-2xl text-base leading-7 text-gray-600 dark:text-dark-300">
                {{ appsSubtitle }}
              </p>

              <div class="mt-5 flex flex-wrap items-center gap-3 rounded-xl border border-gray-200 bg-white/70 px-4 py-3 text-sm dark:border-dark-700 dark:bg-dark-900/60">
                <span class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">Base URL</span>
                <code class="rounded bg-gray-100 px-2 py-0.5 font-mono text-xs text-gray-800 dark:bg-dark-800 dark:text-dark-100">{{ siteBaseUrl }}</code>
                <button
                  type="button"
                  class="rounded border border-gray-300 px-2 py-0.5 text-xs text-gray-600 transition hover:bg-gray-100 hover:text-gray-900 dark:border-dark-600 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white"
                  @click="copyBaseUrl"
                >
                  {{ copyState }}
                </button>
                <RouterLink to="/docs/api-keys" class="ml-auto text-xs text-primary-600 underline underline-offset-4 dark:text-primary-300">
                  {{ uiText.appsKeyHelp }} →
                </RouterLink>
              </div>
            </header>

            <div
              data-testid="apps-grid"
              class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3"
            >
              <AppCard
                v-for="app in localeApps"
                :key="app.slug"
                :app="app"
                :locale="currentLocale"
                :site-name="siteName"
              />
            </div>
          </div>

          <!-- Article (docs tab with matched doc, or apps tab with matched app) -->
          <article v-else-if="displayDoc" class="min-w-0">
            <header class="mb-8 border-b border-gray-200 pb-6 dark:border-dark-800">
              <div class="flex flex-wrap items-center gap-3">
                <span class="inline-flex items-center gap-2 rounded-full bg-primary-50 px-2.5 py-0.5 text-xs font-semibold text-primary-700 ring-1 ring-inset ring-primary-600/15 dark:bg-primary-500/10 dark:text-primary-300 dark:ring-primary-400/20">
                  <AppIcon
                    v-if="activeTab === 'apps' && currentApp"
                    :icon="currentApp.icon"
                    size="sm"
                  />
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
              v-if="prevItem || nextItem"
              class="mt-12 grid gap-3 border-t border-gray-200 pt-8 dark:border-dark-800 sm:grid-cols-2"
              :aria-label="uiText.pagerLabel"
            >
              <RouterLink
                v-if="prevItem"
                :to="prevItem.to"
                class="group flex flex-col rounded-xl border border-gray-200 bg-white px-5 py-4 transition hover:border-primary-300 hover:shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:hover:border-primary-500/40"
              >
                <span class="flex items-center gap-1 text-xs font-medium text-gray-400 transition group-hover:text-primary-600 dark:text-dark-400 dark:group-hover:text-primary-300">
                  <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m15 18-6-6 6-6" /></svg>
                  {{ uiText.prevDoc }}
                </span>
                <span class="mt-1.5 truncate text-sm font-semibold text-gray-900 transition group-hover:text-primary-700 dark:text-white dark:group-hover:text-primary-300">
                  {{ prevItem.title }}
                </span>
              </RouterLink>
              <span v-else class="hidden sm:block" aria-hidden="true"></span>
              <RouterLink
                v-if="nextItem"
                :to="nextItem.to"
                class="group flex flex-col items-end rounded-xl border border-gray-200 bg-white px-5 py-4 text-right transition hover:border-primary-300 hover:shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:hover:border-primary-500/40"
              >
                <span class="flex items-center gap-1 text-xs font-medium text-gray-400 transition group-hover:text-primary-600 dark:text-dark-400 dark:group-hover:text-primary-300">
                  {{ uiText.nextDoc }}
                  <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg>
                </span>
                <span class="mt-1.5 w-full truncate text-sm font-semibold text-gray-900 transition group-hover:text-primary-700 dark:text-white dark:group-hover:text-primary-300">
                  {{ nextItem.title }}
                </span>
              </RouterLink>
            </nav>
          </article>

          <!-- Not-found fallback (unknown slug on either tab) -->
          <section
            v-else
            class="rounded-lg border border-gray-200 bg-white px-6 py-12 text-center dark:border-dark-700 dark:bg-dark-900"
          >
            <h1 class="text-2xl font-bold text-gray-950 dark:text-white">
              {{ activeTab === 'apps' ? uiText.notFoundAppTitle : uiText.notFoundTitle }}
            </h1>
            <p class="mx-auto mt-3 max-w-md text-sm leading-6 text-gray-600 dark:text-dark-300">
              {{ activeTab === 'apps' ? uiText.notFoundAppDescription : uiText.notFoundDescription }}
            </p>
            <RouterLink
              :to="activeTab === 'apps' ? '/apps' : '/docs'"
              class="mt-6 inline-flex items-center justify-center rounded-lg bg-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-primary-600/20 transition hover:bg-primary-700"
            >
              {{ activeTab === 'apps' ? uiText.backToApps : uiText.backToDocs }}
            </RouterLink>
          </section>
        </section>

        <aside v-if="displayDoc && !showAppsLanding" class="hidden lg:block">
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
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import {
  defaultUserDocSlug,
  findUserDoc,
  normalizeUserDocLocale,
  userDocsByLocale,
  type UserDocEntry,
} from '@/docs/registry'
import {
  appEntriesByLocale,
  findApp,
} from '@/apps/registry'
import i18n from '@/i18n'
import SiteHeader from '@/components/common/SiteHeader.vue'
import AppIcon from '@/components/apps/AppIcon.vue'
import AppCard from '@/components/apps/AppCard.vue'
import { useAppStore } from '@/stores'
import { useDocMarkdown, type RenderableDoc } from '@/composables/useDocMarkdown'

interface DocGroup {
  category: string
  docs: UserDocEntry[]
}

interface DocSearchResult {
  doc: UserDocEntry
  excerpt: string
  score: number
}

interface DisplayArticle {
  title: string
  description: string
  category: string
}

interface PagerLink {
  to: string
  title: string
}

const route = useRoute()
const appStore = useAppStore()

const mobileNavOpen = ref(false)
const settingsLoading = ref(false)
const searchQuery = ref('')
const searchInputRef = ref<HTMLInputElement | null>(null)

const docsSearchInputId = 'docs-search-input'

const activeTab = computed<'docs' | 'apps'>(() =>
  route.path === '/apps' || route.path.startsWith('/apps/') ? 'apps' : 'docs',
)

const routeSlug = computed(() => {
  const slug = route.params.slug
  return Array.isArray(slug) ? slug[0] : slug
})

const currentLocale = computed(() => normalizeUserDocLocale(String(i18n.global.locale.value)))
const localeDocs = computed(() => userDocsByLocale[currentLocale.value] ?? userDocsByLocale.zh)
const localeApps = computed(() => appEntriesByLocale[currentLocale.value] ?? appEntriesByLocale.zh)

const currentDoc = computed(() =>
  activeTab.value === 'docs' ? findUserDoc(routeSlug.value, currentLocale.value) : null,
)
const currentApp = computed(() =>
  activeTab.value === 'apps' && routeSlug.value ? findApp(routeSlug.value, currentLocale.value) : null,
)

const activeSlug = computed(() => currentDoc.value?.slug ?? routeSlug.value ?? defaultUserDocSlug)
const showAppsLanding = computed(() => activeTab.value === 'apps' && !routeSlug.value)

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteBaseUrl = computed(() => normalizeBaseUrl(appStore.cachedPublicSettings?.api_base_url || appStore.apiBaseUrl || ''))

const uiText = computed(() => currentLocale.value === 'zh'
  ? {
      home: '首页',
      plaza: '模型广场',
      login: '登录',
      dashboard: '返回控制台',
      loadingSettings: '正在加载站点设置...',
      docNavigation: '文档导航',
      appsNavigation: '应用导航',
      tabDocs: '文档',
      tabApps: '应用集成',
      appsTitle: '选择你使用的工具',
      appsKeyHelp: '如何获取 API Key',
      notFoundTitle: '文档不存在',
      notFoundDescription: '未找到当前文档，可能链接已变更或文档尚未发布。',
      notFoundAppTitle: '应用不存在',
      notFoundAppDescription: '未找到该工具，可能链接已变更或工具尚未收录。',
      backToDocs: '返回文档首页',
      backToApps: '返回应用列表',
      pageToc: '页面目录',
      emptyToc: '暂无目录',
      copy: '复制',
      copied: '已复制',
      copyFailed: '复制失败',
      downloadConfig: '下载配置文件',
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
      plaza: 'Model Plaza',
      login: 'Log in',
      dashboard: 'Back to Dashboard',
      loadingSettings: 'Loading site settings...',
      docNavigation: 'Documentation',
      appsNavigation: 'Apps',
      tabDocs: 'Docs',
      tabApps: 'Apps',
      appsTitle: 'Pick your tool',
      appsKeyHelp: 'How to get an API Key',
      notFoundTitle: 'Document Not Found',
      notFoundDescription: 'The current document was not found. The link may have changed or the document has not been published.',
      notFoundAppTitle: 'App Not Found',
      notFoundAppDescription: 'That tool could not be found. The link may have changed or the tool has not been listed yet.',
      backToDocs: 'Back to Docs',
      backToApps: 'Back to Apps',
      pageToc: 'On This Page',
      emptyToc: 'No sections',
      copy: 'Copy',
      copied: 'Copied',
      copyFailed: 'Copy failed',
      downloadConfig: 'Download config file',
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

const appsSubtitle = computed(() =>
  currentLocale.value === 'zh'
    ? `按 3 步接入 ${siteName.value}。每个工具一页，不用再翻长文档。`
    : `Connect any tool to ${siteName.value} in 3 steps — one focused page per tool, no long docs to skim.`,
)

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

// Feed the composable a unified RenderableDoc — whichever of doc/app is active.
const renderableTarget = computed<RenderableDoc | null>(() => {
  if (currentDoc.value) return { title: currentDoc.value.title, content: currentDoc.value.content }
  if (currentApp.value) return { title: currentApp.value.name, content: currentApp.value.content }
  return null
})

const {
  markdownContainer,
  renderedHtml,
  tocItems,
  activeHeadingId,
  readingProgress,
  readingTimeText,
  resolveDocText,
  scrollToHeading,
  scrollToTop,
  onContentClick: onDocsContentClick,
  onContentKeydown: onDocsContentKeydown,
} = useDocMarkdown({
  doc: renderableTarget,
  uiText,
  siteName,
  siteBaseUrl,
  locale: currentLocale,
})

const displayDoc = computed<DisplayArticle | null>(() => {
  if (currentDoc.value) {
    const doc = currentDoc.value
    return {
      title: resolveDocText(doc.title),
      description: resolveDocText(doc.description),
      category: resolveDocText(doc.category),
    }
  }
  if (currentApp.value) {
    const app = currentApp.value
    return {
      title: resolveDocText(app.name),
      description: resolveDocText(app.tagline),
      category: resolveDocText(app.name),
    }
  }
  return null
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

// Prev/Next pager — walks docs list when on docs tab, apps list when on apps tab.
const prevItem = computed<PagerLink | null>(() => {
  if (activeTab.value === 'docs') {
    const docs = localeDocs.value
    const index = docs.findIndex((d) => d.slug === activeSlug.value)
    if (index <= 0) return null
    const prev = docs[index - 1]
    return { to: docPath(prev), title: resolveDocText(prev.title) }
  }
  const apps = localeApps.value
  const index = apps.findIndex((a) => a.slug === currentApp.value?.slug)
  if (index <= 0) return null
  const prev = apps[index - 1]
  return { to: `/apps/${prev.slug}`, title: prev.name }
})

const nextItem = computed<PagerLink | null>(() => {
  if (activeTab.value === 'docs') {
    const docs = localeDocs.value
    const index = docs.findIndex((d) => d.slug === activeSlug.value)
    if (index < 0 || index >= docs.length - 1) return null
    const next = docs[index + 1]
    return { to: docPath(next), title: resolveDocText(next.title) }
  }
  const apps = localeApps.value
  const index = apps.findIndex((a) => a.slug === currentApp.value?.slug)
  if (index < 0 || index >= apps.length - 1) return null
  const next = apps[index + 1]
  return { to: `/apps/${next.slug}`, title: next.name }
})

function docPath(doc: UserDocEntry): string {
  return doc.slug === defaultUserDocSlug ? '/docs' : `/docs/${doc.slug}`
}

function normalizeBaseUrl(value: string): string {
  const trimmed = value.trim()
  const fallback = typeof window === 'undefined' ? '/' : `${window.location.origin}/`
  const base = trimmed || fallback
  return base.endsWith('/') ? base : `${base}/`
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

const copyState = ref('')
async function copyBaseUrl() {
  try {
    await navigator.clipboard.writeText(siteBaseUrl.value)
    copyState.value = uiText.value.copied
  } catch {
    copyState.value = uiText.value.copyFailed
  }
  window.setTimeout(() => {
    copyState.value = uiText.value.copy
  }, 1800)
}

// Reset the copy button label whenever locale or tab flips.
watch([currentLocale, activeTab], () => {
  copyState.value = uiText.value.copy
}, { immediate: true })

// Clear the search when navigating away from the docs tab so it doesn't
// re-surface an empty results panel if the user comes back later.
watch(activeTab, (tab) => {
  if (tab !== 'docs') {
    searchQuery.value = ''
  }
})

onMounted(async () => {
  window.addEventListener('keydown', onGlobalKeydown)

  if (!appStore.publicSettingsLoaded) {
    settingsLoading.value = true
    try {
      await appStore.fetchPublicSettings()
    } finally {
      settingsLoading.value = false
    }
  }
})

onUnmounted(() => {
  window.removeEventListener('keydown', onGlobalKeydown)
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
</style>
