/**
 * 把 store / composable 中的运行时状态注入导航模型，供 SideRail 与 ContextBar 共用。
 * 功能开关的宽容语义、简单模式、后端模式、自定义菜单项与改版前 AppSidebar 一致。
 */
import { computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAdminSettingsStore, useAppStore, useAuthStore } from '@/stores'
import { FeatureFlags, makeSidebarFlag } from '@/utils/featureFlags'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { useVideoPlaygroundAccess } from '@/composables/useVideoPlaygroundAccess'
import {
  buildAdminGroups,
  buildConsoleGroups,
  filterGroups,
  findActive,
  resolveArea,
  type NavArea,
  type NavContext,
  type NavGroup,
} from '@/navigation/consoleNav'

export function useConsoleNav() {
  // 开关在 setup 内解析（而非模块加载时），避免局部 mock featureFlags 的测试在 import 阶段就失败
  const flagChannelMonitor = makeSidebarFlag(FeatureFlags.channelMonitor)
  const flagPayment = makeSidebarFlag(FeatureFlags.payment)
  const flagAvailableChannels = makeSidebarFlag(FeatureFlags.availableChannels)
  const flagSubscription = makeSidebarFlag(FeatureFlags.subscription)
  const flagAffiliate = makeSidebarFlag(FeatureFlags.affiliate)
  const flagRiskControl = makeSidebarFlag(FeatureFlags.riskControl)
  const flagPluginManagement = makeSidebarFlag(FeatureFlags.pluginManagement)

  const { t } = useI18n()
  const route = useRoute()
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const adminSettingsStore = useAdminSettingsStore()
  const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()
  const { canUseVideoPlayground, refreshVideoPlaygroundAccess } = useVideoPlaygroundAccess()

  const isAdmin = computed(() => authStore.isAdmin)

  // 购买入口文案随站点计费模式切换：仅充值 → 「充值」，仅订阅 → 「订阅」，否则「充值/订阅」。
  const purchaseNavLabel = computed(() => {
    switch (resolveSiteBillingMode(appStore.cachedPublicSettings)) {
      case 'recharge_only':
        return t('nav.recharge')
      case 'subscription_only':
        return t('nav.subscribe')
      default:
        return t('nav.buySubscription')
    }
  })

  const customMenuItemsForUser = computed(() =>
    (appStore.cachedPublicSettings?.custom_menu_items ?? [])
      .filter((item) => item.visibility === 'user')
      .sort((a, b) => a.sort_order - b.sort_order),
  )
  const customMenuItemsForAdmin = computed(() =>
    adminSettingsStore.customMenuItems
      .filter((item) => item.visibility === 'admin')
      .sort((a, b) => a.sort_order - b.sort_order),
  )

  const ctx = computed<NavContext>(() => ({
    t,
    isSimpleMode: authStore.isSimpleMode,
    purchaseNavLabel: purchaseNavLabel.value,
    flags: {
      channelMonitor: flagChannelMonitor,
      payment: flagPayment,
      availableChannels: flagAvailableChannels,
      subscription: flagSubscription,
      affiliate: flagAffiliate,
      riskControl: flagRiskControl,
      pluginManagement: flagPluginManagement,
      opsMonitoring: () => adminSettingsStore.opsMonitoringEnabled,
      adminPayment: () => adminSettingsStore.paymentEnabled,
      batchImage: () => canUseBatchImage.value,
      videoPlayground: () => canUseVideoPlayground.value,
    },
    customMenuItems: {
      user: customMenuItemsForUser.value,
      admin: customMenuItemsForAdmin.value,
    },
  }))

  /** 当前区域：管理员按路由前缀判定；非管理员恒为控制台。 */
  const area = computed<NavArea>(() => (isAdmin.value ? resolveArea(route.path) : 'console'))

  const groups = computed<NavGroup[]>(() => {
    if (area.value === 'admin') return filterGroups(buildAdminGroups(ctx.value), ctx.value)
    // 后端模式下普通用户不渲染用户端菜单（与改版前 v-else-if="!appStore.backendModeEnabled" 一致）。
    if (!isAdmin.value && appStore.backendModeEnabled) return []
    return filterGroups(buildConsoleGroups(ctx.value), ctx.value)
  })

  const active = computed(() => findActive(groups.value, route.path))

  const areaLabel = computed(() => t(area.value === 'admin' ? 'nav.area.admin' : 'nav.area.console'))

  // 管理员需要 adminSettings（运维监控 / 支付 / 自定义菜单等开关）。
  watch(
    isAdmin,
    (v) => {
      if (v) adminSettingsStore.fetch()
    },
    { immediate: true },
  )

  onMounted(() => {
    void refreshBatchImageAccess()
    void refreshVideoPlaygroundAccess()
  })

  return { area, areaLabel, groups, active, isAdmin }
}
