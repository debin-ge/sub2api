import { computed, nextTick, onMounted, onUnmounted, ref, watch, type ComputedRef, type Ref } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import {
  activateDocTab,
  enhanceCallouts,
  enhanceClientCards,
  enhanceStepHeadings,
  groupCodeTabs,
  renderDocCode,
  wrapTables,
} from '@/utils/docsEnhance'

/**
 * Minimal doc shape that the composable understands.
 * Both UserDocEntry and AppEntry are structurally compatible with this.
 */
export interface RenderableDoc {
  title: string
  content: string
}

export interface DocMarkdownUiText {
  copy: string
  copied: string
  copyFailed: string
  downloadConfig: string
  cardBaseUrl: string
  cardConfigFile: string
  readingTime: string
  calloutNote: string
  calloutTip: string
  calloutImportant: string
  calloutWarning: string
  calloutCaution: string
}

export interface TocItem {
  id: string
  text: string
  level: number
}

export interface UseDocMarkdownOptions {
  doc: ComputedRef<RenderableDoc | null> | Ref<RenderableDoc | null>
  uiText: ComputedRef<DocMarkdownUiText> | Ref<DocMarkdownUiText>
  siteName: ComputedRef<string> | Ref<string>
  siteBaseUrl: ComputedRef<string> | Ref<string>
  locale: ComputedRef<'zh' | 'en'> | Ref<'zh' | 'en'>
}

const PREFERRED_TAB_KEY = 'docs-preferred-tab'

let markedConfigured = false
function ensureMarkedConfigured() {
  if (markedConfigured) return
  marked.setOptions({ breaks: true, gfm: true })
  marked.use({ renderer: { code: renderDocCode } })
  markedConfigured = true
}

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

function generateHeadingId(text: string, index: number): string {
  const base = text
    .toLowerCase()
    .replace(/[^\w一-鿿]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return base ? `${base}-${index}` : `heading-${index}`
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function getTextContent(html: string): string {
  const template = document.createElement('template')
  template.innerHTML = html
  return template.content.textContent?.trim() ?? ''
}

export function useDocMarkdown(options: UseDocMarkdownOptions) {
  ensureMarkedConfigured()

  const { doc, uiText, siteName, siteBaseUrl, locale } = options

  const markdownContainer = ref<HTMLElement | null>(null)
  const renderedHtml = ref('')
  const tocItems = ref<TocItem[]>([])
  const activeHeadingId = ref('')
  const readingProgress = ref(0)

  let scrollRafId = 0

  function resolveDocText(value: string): string {
    return value
      .replace(/\{\{SITE_NAME\}\}/g, siteName.value)
      .replace(/\{\{BASE_URL\}\}/g, siteBaseUrl.value)
      .replace(/Sub2API/g, siteName.value)
      .replace(/https:\/\/tiktoken\.net\//g, siteBaseUrl.value)
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

  const readingTimeText = computed(() => {
    const content = doc.value?.content ?? ''
    const prose = content.replace(/```[\s\S]*?```/g, ' ')
    const cjkChars = (prose.match(/[一-鿿]/g) ?? []).length
    const words = prose.replace(/[一-鿿]/g, ' ').split(/\s+/).filter(Boolean).length
    const minutes = Math.max(1, Math.round(cjkChars / 400 + words / 180))
    return locale.value === 'zh'
      ? `约 ${minutes} ${uiText.value.readingTime}`
      : `${minutes} ${uiText.value.readingTime}`
  })

  function findRenderedHeadingById(id: string): HTMLElement | null {
    const container = markdownContainer.value
    if (!container) return null
    const headings = container.querySelectorAll<HTMLElement>('h1, h2, h3, h4')
    return Array.from(headings).find((heading) => heading.id === id) ?? null
  }

  function renderMarkdown(current: RenderableDoc | null) {
    tocItems.value = []
    activeHeadingId.value = ''
    renderedHtml.value = ''

    if (!current) {
      return
    }

    const html = marked.parse(current.content) as string
    const sanitized = DOMPurify.sanitize(html, {
      FORBID_TAGS: ['iframe', 'script'],
      FORBID_ATTR: ['onerror', 'onload', 'onclick'],
    })

    const template = document.createElement('template')
    template.innerHTML = resolveDocHtml(sanitized)

    // The article header already renders the doc title; drop a leading H1
    // that duplicates it so the body starts with the actual content.
    const leadingHeading = template.content.firstElementChild
    const resolvedTitle = resolveDocText(current.title).trim()
    if (leadingHeading?.tagName === 'H1' && leadingHeading.textContent?.trim() === resolvedTitle) {
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
      download: uiText.value.downloadConfig,
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
    const docEl = document.documentElement
    const scrollable = docEl.scrollHeight - window.innerHeight
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

  function triggerConfigDownload(filename: string, text: string) {
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = filename
    document.body.appendChild(anchor)
    anchor.click()
    anchor.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 0)
  }

  function injectCopyButtons() {
    const container = markdownContainer.value
    if (!container) return

    container.querySelectorAll('pre').forEach((pre) => {
      if (pre.querySelector('.pre-actions')) return

      const actions = document.createElement('div')
      actions.className = 'pre-actions'

      const downloadName = pre.getAttribute('data-download-name')
      if (downloadName) {
        const downloadBtn = document.createElement('button')
        downloadBtn.type = 'button'
        downloadBtn.className = 'download-btn'
        downloadBtn.textContent = uiText.value.downloadConfig
        downloadBtn.addEventListener('click', () => {
          const code = pre.querySelector('code')?.textContent ?? getTextContent(pre.innerHTML)
          triggerConfigDownload(downloadName, code)
        })
        actions.appendChild(downloadBtn)
      }

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
      actions.appendChild(button)

      pre.appendChild(actions)
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

  function onContentClick(event: MouseEvent) {
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
      return
    }

    const downloadButton = target.closest<HTMLButtonElement>('.client-download-btn')
    if (downloadButton) {
      const name = downloadButton.getAttribute('data-download-name') ?? ''
      const container = markdownContainer.value
      const block = name && container
        ? container.querySelector(`pre[data-download-name="${CSS.escape(name)}"] code`)
        : null
      const text = block?.textContent ?? ''
      if (name && text) {
        triggerConfigDownload(name, text)
      }
    }
  }

  function onContentKeydown(event: KeyboardEvent) {
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

  watch(doc, (current) => {
    renderMarkdown(current)
  }, { immediate: true })

  onMounted(async () => {
    window.addEventListener('scroll', onWindowScroll, { passive: true })

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
    if (scrollRafId) {
      window.cancelAnimationFrame(scrollRafId)
    }
  })

  return {
    markdownContainer,
    renderedHtml,
    tocItems,
    activeHeadingId,
    readingProgress,
    readingTimeText,
    resolveDocText,
    scrollToHeading,
    scrollToTop,
    onContentClick,
    onContentKeydown,
  }
}
