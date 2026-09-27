import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const railSource = readFileSync(resolve(here, '../SideRail.vue'), 'utf8')
const navSource = readFileSync(resolve(here, '../../../navigation/consoleNav.ts'), 'utf8')
const composableSource = readFileSync(resolve(here, '../../../composables/useConsoleNav.ts'), 'utf8')

describe('SideRail custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(railSource).toContain('.zt-nav-svg {')
    expect(railSource).toContain('color: currentColor;')
    expect(railSource).toContain('display: block;')
    expect(railSource).not.toContain('stroke: currentColor;')
    expect(railSource).not.toContain('fill: none;')
    expect(railSource).toContain('v-html="sanitizeSvg(item.iconSvg)"')
  })
})

describe('SideRail scroll position persistence', () => {
  it('binds a template ref to the nav element', () => {
    expect(railSource).toContain('ref="sidebarNavRef"')
    expect(railSource).toContain('zt-rail-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(railSource).toContain('const sidebarNavRef = ref<HTMLElement | null>(null)')
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(railSource).toContain('onBeforeUnmount')
    expect(railSource).toContain('appStore.sidebarScrollTop')
    expect(railSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(railSource).toContain('onMounted')
    expect(railSource).toContain('appStore.sidebarScrollTop')
    expect(railSource).toContain('nextTick')
  })
})

describe('SideRail collapsible parents', () => {
  it('lets the user collapse a parent even while a child route is active', () => {
    expect(railSource).toContain('const groupExpandOverrides = ref<Map<string, boolean>>(new Map())')
    expect(railSource).not.toContain('expandedGroups.value.has(item.path) || isGroupActive(item)')
  })

  it('persists domain group collapse state through the app store', () => {
    expect(railSource).toContain('appStore.setNavGroupCollapsed(')
    expect(railSource).toContain('appStore.navCollapsedGroups[key] === true')
  })
})

describe('SideRail header', () => {
  it('does not render the version badge in the brand header', () => {
    expect(railSource).not.toContain('<VersionBadge')
    expect(railSource).not.toContain("from '@/components/common/VersionBadge.vue'")
  })

  it('only renders the area switch for administrators', () => {
    expect(railSource).toContain('v-if="isAdmin" class="zt-area"')
  })
})

describe('SideRail navigation model (legacy sidebar parity)', () => {
  it('does not render docs as an in-app sidebar navigation item', () => {
    expect(navSource).not.toContain("{ path: '/docs'")
    expect(navSource).not.toContain('DocsIcon')
  })

  it('exposes the VIP reconciliation page without making the parent users item active', () => {
    expect(navSource).toContain("{ path: '/admin/users/vip-reconcile'")
    expect(navSource).toContain("label: t('nav.vipReconcile')")
    expect(navSource).toContain("{ path: '/admin/users', label: t('nav.users'), icon: UsersIcon, hideInSimpleMode: true, exact: true }")
  })

  it('controls the purchase entry only by the payment feature flag', () => {
    expect(navSource).toContain("const flagUserPurchase: FlagGetter = () => flags.payment() !== false")
    expect(navSource).not.toContain('canShowDuplicateVIPPaymentCTA')
    expect(navSource).not.toContain('canShowPurchaseCTA')
  })

  it('exposes model prices as an independent admin menu item instead of nesting under channels', () => {
    expect(navSource).toContain("{ path: '/admin/model-prices', label: t('nav.modelPrices'), icon: PriceTagIcon }")
    expect(navSource).not.toMatch(/channelPricing[\s\S]*model-prices[\s\S]*channelMonitor/)
  })

  it('gates subscription entries behind the subscription public-settings flag on both areas', () => {
    expect(composableSource).toContain('const flagSubscription = makeSidebarFlag(FeatureFlags.subscription)')
    expect(navSource).toMatch(/path: '\/subscriptions'[^\n]*featureFlag: flags\.subscription/)
    expect(navSource).toMatch(/path: '\/admin\/subscriptions'[^\n]*featureFlag: flags\.subscription/)
  })

  it('derives the purchase entry label from the site billing mode', () => {
    expect(composableSource).toContain("import { resolveSiteBillingMode } from '@/utils/siteBillingMode'")
    expect(composableSource).toMatch(/case 'recharge_only':\s*return t\('nav\.recharge'\)/)
    expect(composableSource).toMatch(/case 'subscription_only':\s*return t\('nav\.subscribe'\)/)
    expect(navSource).toMatch(/path: '\/purchase'[^\n]*label: ctx\.purchaseNavLabel/)
  })

  it('keeps the onboarding tour anchors on the rail links', () => {
    expect(railSource).toContain(':id="item.tourId"')
    expect(railSource).toContain(':data-tour="item.tourAttr"')
    expect(railSource).toContain("'/admin/groups': '#sidebar-group-manage'")
    expect(railSource).toContain("'/admin/accounts': '#sidebar-channel-manage'")
    expect(railSource).toContain(`'/keys': '[data-tour="sidebar-my-keys"]'`)
  })
})
