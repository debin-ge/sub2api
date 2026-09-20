import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar collapsible groups', () => {
  it('lets the user collapse a group even while a child route is active', () => {
    // The expand state must come from the user's override first, falling back
    // to the active-route heuristic only when the user has not clicked yet.
    expect(componentSource).toContain('const groupExpandOverrides = ref<Map<string, boolean>>(new Map())')
    expect(componentSource).not.toContain('expandedGroups.value.has(item.path) || isGroupActive(item)')
  })
})

describe('AppSidebar header styles', () => {
  it('does not render the version badge in the brand header', () => {
    expect(componentSource).not.toContain('<VersionBadge')
    expect(componentSource).not.toContain("from '@/components/common/VersionBadge.vue'")
  })
})

describe('AppSidebar docs entry', () => {
  it('does not render docs as an in-app sidebar navigation item', () => {
    expect(componentSource).not.toContain("{ path: '/docs'")
    expect(componentSource).not.toContain('DocsIcon')
  })
})

describe('AppSidebar VIP reconciliation entry', () => {
  it('exposes the administrator page without making the parent users item active', () => {
    expect(componentSource).toContain("{ path: '/admin/users/vip-reconcile'")
    expect(componentSource).toContain("label: t('nav.vipReconcile')")
    expect(componentSource).toContain("{ path: '/admin/users', label: t('nav.users'), icon: UsersIcon, hideInSimpleMode: true, exact: true }")
  })
})

describe('AppSidebar purchase entry', () => {
  it('is controlled only by the payment feature flag', () => {
    expect(componentSource).toContain('const flagUserPurchase = () => flagPayment() !== false')
    expect(componentSource).not.toContain('canShowDuplicateVIPPaymentCTA')
    expect(componentSource).not.toContain('canShowPurchaseCTA')
  })
})

describe('AppSidebar model prices entry', () => {
  it('exposes an independent admin menu item instead of nesting under channels', () => {
    expect(componentSource).toContain("{ path: '/admin/model-prices', label: t('nav.modelPrices'), icon: PriceTagIcon }")
    expect(componentSource).not.toMatch(/channelPricing[\s\S]*model-prices[\s\S]*channelMonitor/)
  })
})

describe('AppSidebar subscription feature flag', () => {
  it('gates the My Subscriptions entry behind the subscription public-settings flag', () => {
    expect(componentSource).toContain('const flagSubscription = makeSidebarFlag(FeatureFlags.subscription)')
    expect(componentSource).toMatch(/path: '\/subscriptions'[^\n]*featureFlag: flagSubscription/)
  })

  it('also hides the admin Subscription Management entry on recharge-only sites', () => {
    expect(componentSource).toMatch(/path: '\/admin\/subscriptions'[^\n]*featureFlag: flagSubscription/)
  })

  it('derives the purchase entry label from the site billing mode', () => {
    expect(componentSource).toContain("import { resolveSiteBillingMode } from '@/utils/siteBillingMode'")
    expect(componentSource).toMatch(/case 'recharge_only':\s*return t\('nav\.recharge'\)/)
    expect(componentSource).toMatch(/case 'subscription_only':\s*return t\('nav\.subscribe'\)/)
    expect(componentSource).toMatch(/path: '\/purchase'[^\n]*label: purchaseNavLabel\.value/)
  })
})
