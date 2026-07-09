/**
 * Doc rendering enhancements for the user docs center (/docs).
 *
 * Markdown extensions, all degrading gracefully to plain markdown output:
 *
 * 1. Code tab groups — fenced blocks whose info string carries `tab=` and
 *    `group=` render as a tabbed widget. Consecutive blocks sharing the same
 *    `group` become one tab set, and tab selection syncs across the page by
 *    tab label (pick "Python" once, every group follows).
 *
 *    ```bash tab=curl group=openai-chat
 *
 * 2. Client cards — a fenced block with language `client` containing
 *    `key: value` lines renders as a tool card (name, protocol pills,
 *    endpoint / config-file rows with copy buttons). Content following the
 *    block up to the next heading or client block is wrapped into the card
 *    body.
 *
 * 3. Callouts — GitHub-style alert blockquotes (`> [!TIP]`, `> [!WARNING]`,
 *    …) render as colored callout cards with an icon and localized title.
 *
 * 4. Table wrappers — every table is wrapped in a scrollable rounded shell
 *    so wide tables scroll instead of breaking the layout.
 *
 * 5. Step headings — `## 1. Do something` headings render the leading number
 *    as a step badge.
 */

import type { Tokens } from 'marked'
import { platformBadgeClass, platformLabel } from '@/utils/platformColors'
import { sanitizeUrl } from '@/utils/url'

export interface DocCardLabels {
  baseUrl: string
  configFile: string
  copy: string
}

export interface ClientCardInfo {
  name: string
  logo: string
  protocols: string[]
  endpoint: string
  config: string
  homepage: string
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

/**
 * Custom marked renderer for fenced code blocks. Mirrors marked's default
 * output, but additionally parses `tab=` / `group=` from the full info string
 * into data attributes so the DOM stage can build tab groups.
 */
export function renderDocCode({ text, lang, escaped }: Tokens.Code): string {
  const info = (lang || '').trim()
  const langId = info.match(/^\S*/)?.[0] ?? ''
  const tab = info.match(/(?:^|\s)tab=(\S+)/)?.[1] ?? ''
  const group = info.match(/(?:^|\s)group=(\S+)/)?.[1] ?? ''

  const code = text.replace(/\n$/, '') + '\n'
  const body = escaped ? code : escapeHtml(code)
  const classAttr = langId ? ` class="language-${escapeHtml(langId)}"` : ''
  const langAttr = langId && langId !== 'text' && langId !== 'client'
    ? ` data-lang="${escapeHtml(langId)}"`
    : ''
  const tabAttrs = tab && group
    ? ` data-tab="${escapeHtml(tab)}" data-group="${escapeHtml(group)}"`
    : ''

  return `<pre${langAttr}${tabAttrs}><code${classAttr}>${body}</code></pre>\n`
}

/** Parse the `key: value` lines of a ```client block. Returns null when the
 * block has no usable name, so callers can fall back to the raw code block. */
export function parseClientBlock(text: string): ClientCardInfo | null {
  const info: ClientCardInfo = { name: '', logo: '', protocols: [], endpoint: '', config: '', homepage: '' }
  for (const rawLine of text.split('\n')) {
    const line = rawLine.trim()
    if (!line || line.startsWith('#')) continue
    const match = line.match(/^([a-zA-Z][\w-]*)\s*:\s*(.*)$/)
    if (!match) continue
    const key = match[1].toLowerCase()
    const value = match[2].trim()
    switch (key) {
      case 'name':
        info.name = value
        break
      case 'logo':
        info.logo = value.toLowerCase()
        break
      case 'protocols':
        info.protocols = value
          .replace(/^\[/, '')
          .replace(/\]$/, '')
          .split(',')
          .map((item) => item.trim().toLowerCase())
          .filter(Boolean)
        break
      case 'endpoint':
        info.endpoint = value
        break
      case 'config':
        info.config = value
        break
      case 'homepage':
        info.homepage = value
        break
    }
  }
  return info.name ? info : null
}

function isHeading(node: Element): boolean {
  return /^H[1-4]$/.test(node.tagName)
}

function isClientBlock(node: Element): boolean {
  return node.tagName === 'PRE' && !!node.querySelector('code.language-client')
}

function createMetaRow(doc: Document, label: string, value: string, copyLabel: string): HTMLElement {
  const row = doc.createElement('div')
  row.className = 'client-card-meta-row'

  const labelEl = doc.createElement('span')
  labelEl.className = 'client-card-meta-label'
  labelEl.textContent = label

  const valueEl = doc.createElement('code')
  valueEl.className = 'client-card-meta-value'
  valueEl.textContent = value

  const copyBtn = doc.createElement('button')
  copyBtn.type = 'button'
  copyBtn.className = 'client-copy-btn'
  copyBtn.textContent = copyLabel
  copyBtn.setAttribute('data-copy', value)

  row.append(labelEl, valueEl, copyBtn)
  return row
}

/**
 * Replace each ```client block with a card and pull the content that follows
 * (until the next heading or client block) into the card body.
 */
export function enhanceClientCards(root: ParentNode, labels: DocCardLabels): void {
  const doc = (root as Element).ownerDocument ?? document
  const blocks = Array.from(root.querySelectorAll('pre code.language-client'))

  for (const codeEl of blocks) {
    const pre = codeEl.closest('pre')
    if (!pre || !pre.parentNode) continue

    const info = parseClientBlock(codeEl.textContent ?? '')
    if (!info) continue

    const card = doc.createElement('section')
    card.className = 'client-card'

    const head = doc.createElement('div')
    head.className = 'client-card-head'

    const titleRow = doc.createElement('div')
    titleRow.className = 'client-card-title-row'

    const homepage = sanitizeUrl(info.homepage)
    if (homepage) {
      const nameLink = doc.createElement('a')
      nameLink.className = 'client-card-name'
      nameLink.href = homepage
      nameLink.target = '_blank'
      nameLink.rel = 'noopener noreferrer'
      nameLink.textContent = info.name
      titleRow.appendChild(nameLink)
    } else {
      const nameEl = doc.createElement('span')
      nameEl.className = 'client-card-name'
      nameEl.textContent = info.name
      titleRow.appendChild(nameEl)
    }

    for (const protocol of info.protocols) {
      const pill = doc.createElement('span')
      pill.className = `client-pill border ${platformBadgeClass(protocol)}`
      pill.textContent = platformLabel(protocol)
      titleRow.appendChild(pill)
    }

    head.appendChild(titleRow)

    if (info.endpoint || info.config) {
      const meta = doc.createElement('div')
      meta.className = 'client-card-meta'
      if (info.endpoint) {
        meta.appendChild(createMetaRow(doc, labels.baseUrl, info.endpoint, labels.copy))
      }
      if (info.config) {
        meta.appendChild(createMetaRow(doc, labels.configFile, info.config, labels.copy))
      }
      head.appendChild(meta)
    }

    card.appendChild(head)

    const body = doc.createElement('div')
    body.className = 'client-card-body'

    let cursor = pre.nextSibling
    while (cursor) {
      if (cursor.nodeType === Node.ELEMENT_NODE) {
        const el = cursor as Element
        if (isHeading(el) || isClientBlock(el) || el.classList.contains('client-card')) break
      }
      const next = cursor.nextSibling
      body.appendChild(cursor)
      cursor = next
    }

    if (body.childNodes.length > 0) {
      card.appendChild(body)
    }

    pre.parentNode.replaceChild(card, pre)
  }
}

function nextElementSkippingWhitespace(node: Node): Node | null {
  let cursor: Node | null = node.nextSibling
  while (cursor && cursor.nodeType === Node.TEXT_NODE && !cursor.textContent?.trim()) {
    cursor = cursor.nextSibling
  }
  return cursor
}

/**
 * Wrap runs of consecutive `pre[data-group]` blocks sharing a group into a
 * tabbed widget. `preferredTab` (the user's remembered language choice)
 * selects the initial tab when the group contains it.
 */
export function groupCodeTabs(root: ParentNode, preferredTab: string | null): void {
  const doc = (root as Element).ownerDocument ?? document
  const processed = new Set<Element>()

  for (const pre of Array.from(root.querySelectorAll('pre[data-group]'))) {
    if (processed.has(pre)) continue

    const group = pre.getAttribute('data-group') ?? ''
    const run: Element[] = [pre]
    processed.add(pre)

    let cursor = nextElementSkippingWhitespace(pre)
    while (
      cursor &&
      cursor.nodeType === Node.ELEMENT_NODE &&
      (cursor as Element).tagName === 'PRE' &&
      (cursor as Element).getAttribute('data-group') === group
    ) {
      run.push(cursor as Element)
      processed.add(cursor as Element)
      cursor = nextElementSkippingWhitespace(cursor)
    }

    if (run.length < 2 || !pre.parentNode) continue

    const labels = run.map((el) => el.getAttribute('data-tab') ?? '')
    const activeIndex = preferredTab && labels.includes(preferredTab) ? labels.indexOf(preferredTab) : 0

    const wrapper = doc.createElement('div')
    wrapper.className = 'doc-tabs'
    wrapper.setAttribute('data-group', group)

    const bar = doc.createElement('div')
    bar.className = 'doc-tabs-bar'
    bar.setAttribute('role', 'tablist')

    labels.forEach((label, index) => {
      const btn = doc.createElement('button')
      btn.type = 'button'
      btn.className = 'doc-tab-btn'
      btn.setAttribute('role', 'tab')
      btn.setAttribute('data-tab', label)
      btn.setAttribute('aria-selected', index === activeIndex ? 'true' : 'false')
      btn.setAttribute('tabindex', index === activeIndex ? '0' : '-1')
      if (index === activeIndex) btn.classList.add('active')
      btn.textContent = label
      bar.appendChild(btn)
    })

    const panels = doc.createElement('div')
    panels.className = 'doc-tabs-panels'

    pre.parentNode.insertBefore(wrapper, pre)
    run.forEach((el, index) => {
      if (index !== activeIndex) {
        el.setAttribute('hidden', '')
      }
      panels.appendChild(el)
    })

    wrapper.append(bar, panels)
  }
}

export type DocCalloutType = 'note' | 'tip' | 'important' | 'warning' | 'caution'

export type DocCalloutLabels = Record<DocCalloutType, string>

const CALLOUT_MARKER = /^\s*\[!(note|tip|important|warning|caution)\]\s*/i

/** Static stroke-icon markup per callout type (lucide outlines). */
const CALLOUT_ICONS: Record<DocCalloutType, string> = {
  note: '<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>',
  tip: '<path d="M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1.3.5 2.6 1.5 3.5.8.8 1.3 1.5 1.5 2.5"/><path d="M9 18h6"/><path d="M10 22h4"/>',
  important: '<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/><path d="M12 7v2"/><path d="M12 13h.01"/>',
  warning: '<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/>',
  caution: '<path d="M12 16h.01"/><path d="M12 8v4"/><path d="M15.312 2a2 2 0 0 1 1.414.586l4.688 4.688A2 2 0 0 1 22 8.688v6.624a2 2 0 0 1-.586 1.414l-4.688 4.688a2 2 0 0 1-1.414.586H8.688a2 2 0 0 1-1.414-.586l-4.688-4.688A2 2 0 0 1 2 15.312V8.688a2 2 0 0 1 .586-1.414l4.688-4.688A2 2 0 0 1 8.688 2z"/>',
}

function stripCalloutMarker(paragraph: Element): void {
  const first = paragraph.firstChild
  if (first?.nodeType !== Node.TEXT_NODE) return

  first.textContent = (first.textContent ?? '').replace(CALLOUT_MARKER, '')
  if (first.textContent) return

  const afterMarker = first.nextSibling
  first.remove()
  if (afterMarker?.nodeType === Node.ELEMENT_NODE && (afterMarker as Element).tagName === 'BR') {
    afterMarker.remove()
  }
  const lead = paragraph.firstChild
  if (lead?.nodeType === Node.TEXT_NODE) {
    lead.textContent = (lead.textContent ?? '').replace(/^\s+/, '')
  }
}

/**
 * Turn blockquotes whose first paragraph starts with a GitHub alert marker
 * (`[!TIP]` etc.) into callout cards with an icon and localized title.
 */
export function enhanceCallouts(root: ParentNode, labels: DocCalloutLabels): void {
  const doc = (root as Element).ownerDocument ?? document

  for (const quote of Array.from(root.querySelectorAll('blockquote'))) {
    const firstParagraph = quote.querySelector(':scope > p')
    if (!firstParagraph) continue

    const match = (firstParagraph.textContent ?? '').match(CALLOUT_MARKER)
    if (!match) continue
    const type = match[1].toLowerCase() as DocCalloutType

    stripCalloutMarker(firstParagraph)
    if (!firstParagraph.textContent?.trim() && firstParagraph.children.length === 0) {
      firstParagraph.remove()
    }

    const callout = doc.createElement('aside')
    callout.className = `doc-callout doc-callout-${type}`

    const title = doc.createElement('div')
    title.className = 'doc-callout-title'
    title.innerHTML =
      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${CALLOUT_ICONS[type]}</svg>`
    const label = doc.createElement('span')
    label.textContent = labels[type]
    title.appendChild(label)

    const body = doc.createElement('div')
    body.className = 'doc-callout-body'
    while (quote.firstChild) {
      body.appendChild(quote.firstChild)
    }

    callout.append(title, body)
    quote.replaceWith(callout)
  }
}

/** Wrap each table in a scrollable rounded shell. */
export function wrapTables(root: ParentNode): void {
  const doc = (root as Element).ownerDocument ?? document

  for (const table of Array.from(root.querySelectorAll('table'))) {
    if (table.parentElement?.classList.contains('doc-table-wrap')) continue
    const wrap = doc.createElement('div')
    wrap.className = 'doc-table-wrap'
    table.replaceWith(wrap)
    wrap.appendChild(table)
  }
}

const STEP_HEADING = /^(\d{1,2})[.、．]\s*/

/**
 * Render the leading number of `## 1. Do something` headings as a step badge.
 * Runs on the raw text node so appended anchors are untouched.
 */
export function enhanceStepHeadings(root: ParentNode): void {
  const doc = (root as Element).ownerDocument ?? document

  for (const heading of Array.from(root.querySelectorAll('h2'))) {
    const first = heading.firstChild
    if (first?.nodeType !== Node.TEXT_NODE) continue

    const text = first.textContent ?? ''
    const match = text.match(STEP_HEADING)
    if (!match) continue

    first.textContent = text.slice(match[0].length)
    const badge = doc.createElement('span')
    badge.className = 'doc-step-num'
    badge.setAttribute('aria-hidden', 'true')
    badge.textContent = match[1]
    heading.insertBefore(badge, heading.firstChild)
  }
}

/**
 * Activate every tab matching `label` across the container. Groups that do
 * not contain the label keep their current selection.
 */
export function activateDocTab(container: ParentNode, label: string): void {
  for (const tabs of Array.from(container.querySelectorAll('.doc-tabs'))) {
    const buttons = Array.from(tabs.querySelectorAll<HTMLButtonElement>('.doc-tab-btn'))
    if (!buttons.some((btn) => btn.getAttribute('data-tab') === label)) continue

    for (const btn of buttons) {
      const isActive = btn.getAttribute('data-tab') === label
      btn.classList.toggle('active', isActive)
      btn.setAttribute('aria-selected', isActive ? 'true' : 'false')
      btn.setAttribute('tabindex', isActive ? '0' : '-1')
    }
    for (const panel of Array.from(tabs.querySelectorAll<HTMLElement>('.doc-tabs-panels > pre'))) {
      if (panel.getAttribute('data-tab') === label) {
        panel.removeAttribute('hidden')
      } else {
        panel.setAttribute('hidden', '')
      }
    }
  }
}
