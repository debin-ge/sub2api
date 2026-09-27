<template>
  <header class="zt-ctx">
    <button class="zt-ibtn zt-only-mobile" :aria-label="t('common.toggleMenu')" @click="toggleMobileSidebar">
      <Icon name="menu" size="md" />
    </button>

    <nav class="zt-crumb" aria-label="breadcrumb">
      <template v-for="(part, index) in crumbParts" :key="index">
        <span v-if="index > 0" class="zt-crumb-sep" aria-hidden="true">/</span>
        <b v-if="index === crumbParts.length - 1">{{ part }}</b>
        <span v-else class="zt-hide-mobile">{{ part }}</span>
      </template>
    </nav>

    <div class="zt-ctx-right">
      <!-- Balance Display -->
      <span
        v-if="user"
        class="zt-ctx-balance zt-hide-mobile"
        :title="`${balanceAvailableText} ${formatHeaderMoney(availableBalance)} · ${balanceFrozenText} ${formatHeaderMoney(frozenBalance)} · ${balanceTotalText} ${formatHeaderMoney(totalBalance)}`"
      >
        <Icon name="creditCard" size="xs" />
        {{ formatHeaderMoney(availableBalance) }}
        <span v-if="frozenBalance > 0" class="zt-frozen">{{ balanceFrozenLabel }}</span>
      </span>

      <!-- Model Plaza Link -->
      <router-link v-if="showModelPlaza" to="/plaza" class="zt-ctx-link zt-hide-mobile">
        <Icon name="grid" size="sm" />
        <span>{{ t('plaza.header.label') }}</span>
      </router-link>

      <!-- Docs Link -->
      <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="zt-ctx-link zt-hide-mobile">
        <Icon name="book" size="sm" />
        <span>{{ t('nav.docs') }}</span>
      </a>

      <!-- Subscription Progress (not mounted at all when the feature is off) -->
      <SubscriptionProgressMini v-if="user && subscriptionFeatureEnabled" />

      <!-- Announcement Bell -->
      <AnnouncementBell v-if="user" />

      <!-- Language Switcher -->
      <LocaleSwitcher />
    </div>
  </header>
</template>

<script setup lang="ts">
/**
 * 上下文条：面包屑（区域 / 分组 / 页面）+ 改版前顶栏右侧的全局入口。
 * 页面标题下沉到内容区的 PageHead；用户菜单迁到侧栏底部的 RailUserMenu。
 */
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import SubscriptionProgressMini from '@/components/common/SubscriptionProgressMini.vue'
import AnnouncementBell from '@/components/common/AnnouncementBell.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatBalanceAmount } from '@/utils/formatters'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { resolveRouteMetaKeys } from '@/router/title'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import { useConsoleNav } from '@/composables/useConsoleNav'

const route = useRoute()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()
const { areaLabel, active } = useConsoleNav()

const user = computed(() => authStore.user)
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
const availableBalance = computed(() => Number(user.value?.balance || 0))
const frozenBalance = computed(() => Number(user.value?.frozen_balance || 0))
const totalBalance = computed(() => availableBalance.value + frozenBalance.value)
const balanceAvailableText = computed(() => (t('common.availableBalance') === 'common.availableBalance' ? '可用余额' : t('common.availableBalance')))
const balanceFrozenText = computed(() => (t('common.frozenBalance') === 'common.frozenBalance' ? '冻结金额' : t('common.frozenBalance')))
const balanceTotalText = computed(() => (t('common.totalBalance') === 'common.totalBalance' ? '总余额' : t('common.totalBalance')))
const balanceFrozenLabel = computed(() => `${balanceFrozenText.value} ${formatHeaderMoney(frozenBalance.value)}`)
const showModelPlaza = computed(() => {
  if (appStore.cachedPublicSettings?.model_plaza_enabled !== true) return false
  return authStore.isAuthenticated || appStore.cachedPublicSettings?.model_plaza_require_auth !== true
})

// 订阅功能关闭时不挂载订阅徽章（组件 onMounted 会拉取订阅接口）。
const subscriptionFeatureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))

// /purchase 的标题随站点计费模式切换，与 document.title 共用同一解析。
const routeMetaKeys = computed(() => resolveRouteMetaKeys(route, {
  billingMode: resolveSiteBillingMode(appStore.cachedPublicSettings),
}))

const pageTitle = computed(() => {
  // For custom pages, use the menu item's label instead of generic "自定义页面"
  if (route.name === 'CustomPage') {
    const id = route.params.id as string
    const publicItems = appStore.cachedPublicSettings?.custom_menu_items ?? []
    const menuItem = publicItems.find((item) => item.id === id)
      ?? (authStore.isAdmin ? adminSettingsStore.customMenuItems.find((item) => item.id === id) : undefined)
    if (menuItem?.label) return menuItem.label
  }
  const titleKey = routeMetaKeys.value.titleKey
  if (titleKey) return t(titleKey)
  return (route.meta.title as string) || ''
})

/** 区域 / 分组 / （父项）/ 页面；不在导航中的路由退化为「区域 / 页面」。 */
const crumbParts = computed(() => {
  const parts = [areaLabel.value]
  const hit = active.value
  if (hit) {
    parts.push(hit.group.label)
    if (hit.parent) parts.push(hit.parent.label)
    parts.push(hit.item.path === route.path || !pageTitle.value ? hit.item.label : pageTitle.value)
  } else if (pageTitle.value) {
    parts.push(pageTitle.value)
  }
  return parts
})

function toggleMobileSidebar() {
  appStore.toggleMobileSidebar()
}

function formatHeaderMoney(value: number) {
  return `$${formatBalanceAmount(value)}`
}
</script>
