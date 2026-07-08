/**
 * Doc rendering enhancements for the user docs center (/docs).
 *
 * Two markdown extensions, both degrading gracefully to plain code blocks:
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
  const tabAttrs = tab && group
    ? ` data-tab="${escapeHtml(tab)}" data-group="${escapeHtml(group)}"`
    : ''

  return `<pre${tabAttrs}><code${classAttr}>${body}</code></pre>\n`
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
