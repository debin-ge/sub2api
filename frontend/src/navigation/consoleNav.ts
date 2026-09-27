/**
 * 控制台导航模型（纯数据 + 纯函数，无 store 依赖）
 *
 * 条目集合、父子结构（children / expandOnly）、featureFlag、hideInSimpleMode、exact、
 * 自定义菜单项与新手引导锚点全部沿用改版前 AppSidebar.vue 的语义；本模块只在其上
 * 增加两层视觉分区：区域（console / admin）与域分组（NavGroup）。
 *
 * 侧栏（SideRail）与上下文条面包屑（ContextBar）共用同一份模型，由 composables/useConsoleNav.ts 注入上下文。
 */
import type { Component } from 'vue'
import type { CustomMenuItem } from '@/types'
import {
  BellIcon,
  BatchImageIcon,
  ChannelIcon,
  ChartIcon,
  CogIcon,
  CreditCardIcon,
  DashboardIcon,
  FolderIcon,
  GiftIcon,
  GlobeIcon,
  KeyIcon,
  OrderIcon,
  OrderListIcon,
  PluginIcon,
  PriceTagIcon,
  RechargeSubscriptionIcon,
  ServerIcon,
  ShieldIcon,
  SignalIcon,
  TicketIcon,
  UserIcon,
  UsersIcon,
  VideoPlaygroundIcon,
} from './navIcons'

export type NavArea = 'console' | 'admin'

/** 与改版前 AppSidebar 的 NavItem 一致；featureFlag 返回 undefined/true 显示、false 隐藏。 */
export interface NavItem {
  path: string
  label: string
  icon: Component | null
  iconSvg?: string
  hideInSimpleMode?: boolean
  exact?: boolean
  children?: NavItem[]
  /** 父项只负责展开/收起，不导航到自身 path（path 仅作稳定 key）。 */
  expandOnly?: boolean
  featureFlag?: () => boolean | undefined
  /** 输出为元素 id，供新手引导定位（如 sidebar-group-manage）。 */
  tourId?: string
  /** 输出为 data-tour 属性，供新手引导定位（如 sidebar-my-keys）。 */
  tourAttr?: string
}

export interface NavGroup {
  key: string
  label: string
  items: NavItem[]
}

export type FlagGetter = () => boolean | undefined

export interface NavContext {
  t: (key: string) => string
  isSimpleMode: boolean
  /** 「充值 / 订阅」入口文案随站点计费模式切换，由调用方解析后传入。 */
  purchaseNavLabel: string
  flags: {
    channelMonitor: FlagGetter
    payment: FlagGetter
    availableChannels: FlagGetter
    subscription: FlagGetter
    affiliate: FlagGetter
    riskControl: FlagGetter
    pluginManagement: FlagGetter
    opsMonitoring: FlagGetter
    adminPayment: FlagGetter
    batchImage: FlagGetter
    videoPlayground: FlagGetter
  }
  customMenuItems: {
    user: CustomMenuItem[]
    admin: CustomMenuItem[]
  }
}

export interface ActiveNav {
  group: NavGroup
  item: NavItem
  /** 命中的是子项时，父项 */
  parent?: NavItem
}

function customItems(list: CustomMenuItem[]): NavItem[] {
  return list.map((item) => ({
    path: `/custom/${item.id}`,
    label: item.label,
    icon: null,
    iconSvg: item.icon_svg,
  }))
}

/**
 * 用户自己的条目（用户端主菜单与管理员「我的账户」共享同一组声明）。
 * withDashboard=false 时不含仪表盘（管理员已有独立仪表盘）。
 * 集合与改版前 buildSelfNavItems 完全一致，只是不再平铺而是按域分组返回。
 */
export function buildSelfGroups(ctx: NavContext, withDashboard: boolean): NavGroup[] {
  const { t, flags } = ctx
  const flagUserPurchase: FlagGetter = () => flags.payment() !== false
  const workspace: NavItem[] = []
  if (withDashboard) {
    workspace.push({ path: '/dashboard', label: t('nav.dashboard'), icon: DashboardIcon })
  }
  workspace.push(
    { path: '/keys', label: t('nav.apiKeys'), icon: KeyIcon, tourAttr: 'sidebar-my-keys' },
    { path: '/usage', label: t('nav.usage'), icon: ChartIcon, hideInSimpleMode: true },
  )
  const groups: NavGroup[] = [
    { key: 'workspace', label: t('nav.group.workspace'), items: workspace },
    {
      key: 'generate',
      label: t('nav.group.generate'),
      items: [
        { path: '/batch-image', label: t('nav.batchImage'), icon: BatchImageIcon, hideInSimpleMode: true, featureFlag: flags.batchImage },
        { path: '/video-playground', label: t('nav.videoPlayground'), icon: VideoPlaygroundIcon, hideInSimpleMode: true, featureFlag: flags.videoPlayground },
      ],
    },
    {
      key: 'channels',
      label: t('nav.group.channels'),
      items: [
        { path: '/available-channels', label: t('nav.availableChannels'), icon: ChannelIcon, hideInSimpleMode: true, featureFlag: flags.availableChannels },
        { path: '/monitor', label: t('nav.channelStatus'), icon: SignalIcon, featureFlag: flags.channelMonitor },
      ],
    },
    {
      key: 'billing',
      label: t('nav.group.billing'),
      items: [
        { path: '/subscriptions', label: t('nav.mySubscriptions'), icon: CreditCardIcon, hideInSimpleMode: true, featureFlag: flags.subscription },
        { path: '/purchase', label: ctx.purchaseNavLabel, icon: RechargeSubscriptionIcon, hideInSimpleMode: true, featureFlag: flagUserPurchase },
        { path: '/orders', label: t('nav.myOrders'), icon: OrderListIcon, hideInSimpleMode: true, featureFlag: flags.payment },
        { path: '/redeem', label: t('nav.redeem'), icon: GiftIcon, hideInSimpleMode: true },
        { path: '/affiliate', label: t('nav.affiliate'), icon: UsersIcon, hideInSimpleMode: true, featureFlag: flags.affiliate },
      ],
    },
    {
      key: 'account',
      label: t('nav.group.account'),
      items: [{ path: '/profile', label: t('nav.profile'), icon: UserIcon }],
    },
    { key: 'more', label: t('nav.group.more'), items: customItems(ctx.customMenuItems.user) },
  ]
  return groups
}

/** 用户端（控制台区域）分组。 */
export function buildConsoleGroups(ctx: NavContext): NavGroup[] {
  return buildSelfGroups(ctx, true)
}

/** 管理端（管理后台区域）分组。 */
export function buildAdminGroups(ctx: NavContext): NavGroup[] {
  const { t, flags } = ctx
  const settingsItem: NavItem = { path: '/admin/settings', label: t('nav.settings'), icon: CogIcon }
  const systemItems: NavItem[] = [
    { path: '/admin/announcements', label: t('nav.announcements'), icon: BellIcon },
  ]
  // 简单模式：改版前在系统设置前插入「API 密钥」，并且不渲染「我的账户」区。
  if (ctx.isSimpleMode) {
    systemItems.push({ path: '/keys', label: t('nav.apiKeys'), icon: KeyIcon })
  }
  systemItems.push(settingsItem)

  const groups: NavGroup[] = [
    {
      key: 'overview',
      label: t('nav.group.overview'),
      items: [
        { path: '/admin/dashboard', label: t('nav.dashboard'), icon: DashboardIcon },
        { path: '/admin/ops', label: t('nav.ops'), icon: ChartIcon, featureFlag: flags.opsMonitoring },
      ],
    },
    {
      key: 'operations',
      label: t('nav.group.operations'),
      items: [
        { path: '/admin/users', label: t('nav.users'), icon: UsersIcon, hideInSimpleMode: true, exact: true },
        { path: '/admin/users/vip-reconcile', label: t('nav.vipReconcile'), icon: ChartIcon, hideInSimpleMode: true },
        { path: '/admin/groups', label: t('nav.groups'), icon: FolderIcon, tourId: 'sidebar-group-manage' },
        { path: '/admin/subscriptions', label: t('nav.subscriptions'), icon: CreditCardIcon, hideInSimpleMode: true, featureFlag: flags.subscription },
      ],
    },
    {
      key: 'channelsModels',
      label: t('nav.group.channelsModels'),
      items: [
        {
          path: '/admin/channels',
          label: t('nav.channelManagement'),
          icon: ChannelIcon,
          hideInSimpleMode: true,
          expandOnly: true,
          children: [
            { path: '/admin/channels/pricing', label: t('nav.channelPricing'), icon: PriceTagIcon },
            { path: '/admin/channels/monitor', label: t('nav.channelMonitor'), icon: SignalIcon, featureFlag: flags.channelMonitor },
          ],
        },
        { path: '/admin/model-prices', label: t('nav.modelPrices'), icon: PriceTagIcon },
        { path: '/admin/accounts', label: t('nav.accounts'), icon: GlobeIcon, tourId: 'sidebar-channel-manage' },
        { path: '/admin/proxies', label: t('nav.proxies'), icon: ServerIcon },
        { path: '/admin/plugins', label: t('nav.plugins'), icon: PluginIcon, featureFlag: flags.pluginManagement },
      ],
    },
    {
      key: 'monitoring',
      label: t('nav.group.monitoring'),
      items: [
        { path: '/admin/usage', label: t('nav.usage'), icon: ChartIcon },
        { path: '/admin/videos', label: t('nav.videoTasks'), icon: ChartIcon },
        {
          path: '/admin/security-audit',
          label: t('nav.securityAudit'),
          icon: ShieldIcon,
          expandOnly: true,
          featureFlag: flags.riskControl,
          children: [
            { path: '/admin/risk-control', label: t('nav.contentModeration'), icon: ShieldIcon },
            { path: '/admin/prompt-audit', label: t('nav.promptAudit'), icon: ShieldIcon },
          ],
        },
        { path: '/admin/audit-logs', label: t('nav.auditLogs'), icon: ShieldIcon, hideInSimpleMode: true },
      ],
    },
    {
      key: 'finance',
      label: t('nav.group.finance'),
      items: [
        {
          path: '/admin/orders',
          label: t('nav.orderManagement'),
          icon: OrderIcon,
          hideInSimpleMode: true,
          expandOnly: true,
          featureFlag: flags.adminPayment,
          children: [
            { path: '/admin/orders/dashboard', label: t('nav.paymentDashboard'), icon: ChartIcon },
            { path: '/admin/orders', label: t('nav.orderManagement'), icon: OrderIcon },
            { path: '/admin/orders/plans', label: t('nav.paymentPlans'), icon: CreditCardIcon },
          ],
        },
        { path: '/admin/redeem', label: t('nav.redeemCodes'), icon: TicketIcon, hideInSimpleMode: true, tourId: 'sidebar-wallet' },
        { path: '/admin/promo-codes', label: t('nav.promoCodes'), icon: GiftIcon, hideInSimpleMode: true },
        {
          path: '/admin/affiliates',
          label: t('nav.affiliateManagement'),
          icon: UsersIcon,
          hideInSimpleMode: true,
          expandOnly: true,
          featureFlag: flags.affiliate,
          children: [
            { path: '/admin/affiliates/invites', label: t('nav.affiliateInviteRecords'), icon: UsersIcon },
            { path: '/admin/affiliates/rebates', label: t('nav.affiliateRebateRecords'), icon: OrderIcon },
            { path: '/admin/affiliates/transfers', label: t('nav.affiliateTransferRecords'), icon: CreditCardIcon },
          ],
        },
      ],
    },
    { key: 'system', label: t('nav.group.system'), items: systemItems },
  ]

  // 个人页面只在「控制台」区域展示（顶部区域切换可达），管理后台不再重复渲染「我的账户」；
  // 简单模式下沿用改版前的做法，把 API 密钥插在系统设置之前。
  groups.push({ key: 'more', label: t('nav.group.more'), items: customItems(ctx.customMenuItems.admin) })
  return groups
}

/** 递归过滤 featureFlag() === false 的节点；undefined / true 视为显示（避免公开设置未加载时闪烁）。 */
export function applyFeatureFlags(items: NavItem[]): NavItem[] {
  const out: NavItem[] = []
  for (const item of items) {
    if (item.featureFlag && item.featureFlag() === false) continue
    out.push(item.children ? { ...item, children: applyFeatureFlags(item.children) } : item)
  }
  return out
}

export function applySimpleMode(items: NavItem[], isSimpleMode: boolean): NavItem[] {
  if (!isSimpleMode) return items
  return items
    .filter((item) => !item.hideInSimpleMode)
    .map((item) => (item.children ? { ...item, children: applySimpleMode(item.children, true) } : item))
}

/** 功能开关 + 简单模式过滤，并丢弃空分组。 */
export function filterGroups(groups: NavGroup[], ctx: Pick<NavContext, 'isSimpleMode'>): NavGroup[] {
  return groups
    .map((g) => ({ ...g, items: applySimpleMode(applyFeatureFlags(g.items), ctx.isSimpleMode) }))
    .filter((g) => g.items.length > 0)
}

export function resolveArea(path: string): NavArea {
  return path === '/admin' || path.startsWith('/admin/') ? 'admin' : 'console'
}

export function areaHome(area: NavArea): string {
  return area === 'admin' ? '/admin/dashboard' : '/dashboard'
}

/** 与改版前 isActive 一致：精确匹配，或（非 exact 时）前缀匹配。 */
export function isItemActive(item: NavItem, path: string): boolean {
  return path === item.path || (!item.exact && path.startsWith(item.path + '/'))
}

/** 查找当前路由命中的条目；子项优先于父项，避免 expandOnly 父项因前缀匹配抢高亮。 */
export function findActive(groups: NavGroup[], path: string): ActiveNav | null {
  for (const group of groups) {
    for (const item of group.items) {
      if (item.children) {
        const child = item.children.find((c) => c.path === path)
        if (child) return { group, item: child, parent: item }
      }
    }
  }
  for (const group of groups) {
    for (const item of group.items) {
      if (item.children) {
        const child = item.children.find((c) => isItemActive(c, path))
        if (child) return { group, item: child, parent: item }
        continue
      }
      if (item.path === path) return { group, item }
    }
  }
  for (const group of groups) {
    for (const item of group.items) {
      if (!item.children && isItemActive(item, path)) return { group, item }
    }
  }
  return null
}

/** 全部条目（含子项）的路径集合，用于与改版前菜单做一对一核对。 */
export function flattenPaths(groups: NavGroup[]): string[] {
  const out: string[] = []
  for (const g of groups) {
    for (const item of g.items) {
      if (item.children) {
        if (!item.expandOnly) out.push(item.path)
        out.push(...item.children.map((c) => c.path))
      } else {
        out.push(item.path)
      }
    }
  }
  return out
}
