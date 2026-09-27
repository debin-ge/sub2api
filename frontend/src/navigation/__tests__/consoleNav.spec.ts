import { describe, expect, it } from 'vitest'
import type { CustomMenuItem } from '@/types'
import {
  buildAdminGroups,
  buildConsoleGroups,
  filterGroups,
  findActive,
  flattenPaths,
  resolveArea,
  type NavContext,
} from '../consoleNav'

type FlagOverrides = Partial<Record<keyof NavContext['flags'], boolean | undefined>>

function makeCtx(opts: { simple?: boolean; flags?: FlagOverrides; purchaseLabel?: string; userCustom?: CustomMenuItem[]; adminCustom?: CustomMenuItem[] } = {}): NavContext {
  const flag = (k: keyof NavContext['flags']) => () => (opts.flags && k in opts.flags ? opts.flags[k] : undefined)
  return {
    t: (k) => k,
    isSimpleMode: opts.simple ?? false,
    purchaseNavLabel: opts.purchaseLabel ?? 'nav.buySubscription',
    flags: {
      channelMonitor: flag('channelMonitor'),
      payment: flag('payment'),
      availableChannels: flag('availableChannels'),
      subscription: flag('subscription'),
      affiliate: flag('affiliate'),
      riskControl: flag('riskControl'),
      pluginManagement: flag('pluginManagement'),
      opsMonitoring: flag('opsMonitoring'),
      adminPayment: flag('adminPayment'),
      batchImage: flag('batchImage'),
      videoPlayground: flag('videoPlayground'),
    },
    customMenuItems: { user: opts.userCustom ?? [], admin: opts.adminCustom ?? [] },
  }
}

const custom = (id: string, label: string, visibility: 'user' | 'admin'): CustomMenuItem =>
  ({ id, label, visibility, sort_order: 0, icon_svg: '<svg fill="#ff0000"></svg>' }) as unknown as CustomMenuItem

// 改版前 AppSidebar.vue 的条目集合（未加开关过滤）：用于一对一核对。
const LEGACY_USER_PATHS = [
  '/dashboard', '/keys', '/batch-image', '/video-playground', '/usage', '/available-channels', '/monitor',
  '/subscriptions', '/purchase', '/orders', '/redeem', '/affiliate', '/profile',
]
const LEGACY_ADMIN_PATHS = [
  '/admin/dashboard', '/admin/ops', '/admin/users', '/admin/users/vip-reconcile', '/admin/groups',
  '/admin/channels/pricing', '/admin/channels/monitor', '/admin/model-prices', '/admin/subscriptions',
  '/admin/accounts', '/admin/videos', '/admin/plugins', '/admin/announcements', '/admin/proxies',
  '/admin/risk-control', '/admin/prompt-audit', '/admin/redeem', '/admin/promo-codes',
  '/admin/affiliates/invites', '/admin/affiliates/rebates', '/admin/affiliates/transfers',
  '/admin/orders/dashboard', '/admin/orders', '/admin/orders/plans', '/admin/usage', '/admin/audit-logs',
  '/admin/settings',
]
const LEGACY_PERSONAL_PATHS = LEGACY_USER_PATHS.filter((p) => p !== '/dashboard')

describe('consoleNav item sets match the legacy sidebar', () => {
  it('console area contains exactly the legacy user items when every flag is on', () => {
    const groups = filterGroups(buildConsoleGroups(makeCtx()), makeCtx())
    expect(flattenPaths(groups).sort()).toEqual([...LEGACY_USER_PATHS].sort())
  })

  it('admin area contains only the legacy admin items; the personal section lives in the console area', () => {
    const groups = filterGroups(buildAdminGroups(makeCtx()), makeCtx())
    expect(flattenPaths(groups).sort()).toEqual([...LEGACY_ADMIN_PATHS].sort())
    for (const path of LEGACY_PERSONAL_PATHS) expect(flattenPaths(groups)).not.toContain(path)
    expect(groups.find((g) => g.key.startsWith('my-'))).toBeUndefined()
  })

  it('keeps the collapsible parents with expandOnly semantics', () => {
    const groups = buildAdminGroups(makeCtx())
    const parents = groups.flatMap((g) => g.items).filter((i) => i.children)
    expect(parents.map((p) => p.path).sort()).toEqual(['/admin/affiliates', '/admin/channels', '/admin/orders', '/admin/security-audit'])
    expect(parents.every((p) => p.expandOnly)).toBe(true)
  })

  it('exposes the onboarding tour anchors', () => {
    const admin = buildAdminGroups(makeCtx()).flatMap((g) => g.items)
    expect(admin.find((i) => i.path === '/admin/groups')?.tourId).toBe('sidebar-group-manage')
    expect(admin.find((i) => i.path === '/admin/accounts')?.tourId).toBe('sidebar-channel-manage')
    expect(admin.find((i) => i.path === '/admin/redeem')?.tourId).toBe('sidebar-wallet')
    const user = buildConsoleGroups(makeCtx()).flatMap((g) => g.items)
    expect(user.find((i) => i.path === '/keys')?.tourAttr).toBe('sidebar-my-keys')
  })

  it('keeps the exact-match rule for /admin/users so VIP reconciliation does not activate it', () => {
    const groups = filterGroups(buildAdminGroups(makeCtx()), makeCtx())
    const hit = findActive(groups, '/admin/users/vip-reconcile')
    expect(hit?.item.path).toBe('/admin/users/vip-reconcile')
    expect(findActive(groups, '/admin/users')?.item.path).toBe('/admin/users')
  })
})

describe('consoleNav feature flags', () => {
  it('hides payment-gated entries only when the flag is explicitly false', () => {
    const off = makeCtx({ flags: { payment: false, adminPayment: false } })
    const user = flattenPaths(filterGroups(buildConsoleGroups(off), off))
    expect(user).not.toContain('/purchase')
    expect(user).not.toContain('/orders')
    const admin = flattenPaths(filterGroups(buildAdminGroups(off), off))
    expect(admin).not.toContain('/admin/orders')
    expect(admin).not.toContain('/admin/orders/plans')

    const unloaded = makeCtx()
    expect(flattenPaths(filterGroups(buildConsoleGroups(unloaded), unloaded))).toContain('/purchase')
  })

  it('gates subscriptions on both areas', () => {
    const off = makeCtx({ flags: { subscription: false } })
    expect(flattenPaths(filterGroups(buildConsoleGroups(off), off))).not.toContain('/subscriptions')
    expect(flattenPaths(filterGroups(buildAdminGroups(off), off))).not.toContain('/admin/subscriptions')
  })

  it('gates channel monitor, available channels, affiliate, risk control, plugins, ops, batch image and video', () => {
    const off = makeCtx({ flags: { channelMonitor: false, availableChannels: false, affiliate: false, riskControl: false, pluginManagement: false, opsMonitoring: false, batchImage: false, videoPlayground: false } })
    const user = flattenPaths(filterGroups(buildConsoleGroups(off), off))
    for (const p of ['/monitor', '/available-channels', '/affiliate', '/batch-image', '/video-playground']) expect(user).not.toContain(p)
    const admin = flattenPaths(filterGroups(buildAdminGroups(off), off))
    for (const p of ['/admin/channels/monitor', '/admin/affiliates/invites', '/admin/risk-control', '/admin/prompt-audit', '/admin/plugins', '/admin/ops']) expect(admin).not.toContain(p)
    expect(admin).toContain('/admin/channels/pricing')
  })

  it('drops a group when every item in it is filtered out', () => {
    const off = makeCtx({ flags: { batchImage: false, videoPlayground: false } })
    const groups = filterGroups(buildConsoleGroups(off), off)
    expect(groups.find((g) => g.key === 'generate')).toBeUndefined()
  })

  it('derives the purchase entry label from the site billing mode', () => {
    const ctx = makeCtx({ purchaseLabel: 'nav.recharge' })
    const item = buildConsoleGroups(ctx).flatMap((g) => g.items).find((i) => i.path === '/purchase')
    expect(item?.label).toBe('nav.recharge')
  })
})

describe('consoleNav simple mode', () => {
  it('filters hideInSimpleMode items on the console area', () => {
    const ctx = makeCtx({ simple: true })
    expect(flattenPaths(filterGroups(buildConsoleGroups(ctx), ctx)).sort()).toEqual(['/dashboard', '/keys', '/monitor', '/profile'].sort())
  })

  it('appends API keys before settings and drops the personal section on the admin area', () => {
    const ctx = makeCtx({ simple: true })
    const groups = filterGroups(buildAdminGroups(ctx), ctx)
    const paths = flattenPaths(groups)
    expect(paths).toContain('/keys')
    expect(paths.indexOf('/keys')).toBeLessThan(paths.indexOf('/admin/settings'))
    expect(paths).not.toContain('/profile')
    expect(paths).not.toContain('/admin/users')
    expect(paths).not.toContain('/admin/channels/pricing')
    expect(groups.find((g) => g.key.startsWith('my-'))).toBeUndefined()
  })
})

describe('consoleNav custom menu items', () => {
  it('appends user custom items to the console "more" group and admin items to the admin "more" group', () => {
    const ctx = makeCtx({ userCustom: [custom('u1', 'Docs', 'user')], adminCustom: [custom('a1', 'Runbook', 'admin')] })
    const console = filterGroups(buildConsoleGroups(ctx), ctx)
    expect(console.find((g) => g.key === 'more')?.items.map((i) => i.path)).toEqual(['/custom/u1'])
    const admin = filterGroups(buildAdminGroups(ctx), ctx)
    expect(admin.find((g) => g.key === 'more')?.items.map((i) => i.path)).toEqual(['/custom/a1'])
    expect(admin.find((g) => g.key === 'my-more')).toBeUndefined()
    expect(admin.find((g) => g.key === 'more')?.items[0].iconSvg).toContain('#ff0000')
  })
})

describe('consoleNav active resolution', () => {
  const ctx = makeCtx()
  const admin = filterGroups(buildAdminGroups(ctx), ctx)

  it('resolves the area from the path prefix', () => {
    expect(resolveArea('/admin/accounts')).toBe('admin')
    expect(resolveArea('/admin')).toBe('admin')
    expect(resolveArea('/administrator')).toBe('console')
    expect(resolveArea('/keys')).toBe('console')
  })

  it('prefers a child over its expandOnly parent', () => {
    const hit = findActive(admin, '/admin/channels/pricing')
    expect(hit?.item.path).toBe('/admin/channels/pricing')
    expect(hit?.parent?.path).toBe('/admin/channels')
    expect(hit?.group.key).toBe('channelsModels')
  })

  it('resolves the orders child that shares its parent path', () => {
    const hit = findActive(admin, '/admin/orders')
    expect(hit?.item.path).toBe('/admin/orders')
    expect(hit?.parent?.path).toBe('/admin/orders')
  })

  it('falls back to prefix matching for nested routes of a plain item', () => {
    expect(findActive(admin, '/admin/accounts/123')?.item.path).toBe('/admin/accounts')
    expect(findActive(admin, '/nowhere')).toBeNull()
  })
})
