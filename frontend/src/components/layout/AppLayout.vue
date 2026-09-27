<template>
  <div class="zt-shell min-h-screen" :class="{ 'is-collapsed': sidebarCollapsed, 'is-mobile-open': mobileOpen }">
    <SideRail />

    <!-- Mobile Overlay -->
    <transition name="fade">
      <div v-if="mobileOpen" class="zt-scrim lg:hidden" @click="closeMobile"></div>
    </transition>

    <div class="zt-main">
      <ContextBar />

      <main class="zt-body">
        <PageHead v-if="pageHead" :title="pageTitle" :description="pageDescription">
          <template v-if="$slots.actions" #actions>
            <slot name="actions" />
          </template>
        </PageHead>
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 控制台壳（分域侧栏工作台）：SideRail + ContextBar + PageHead + 内容插槽。
 * 文件名与导出名保持不变，40 个视图无需改动；页头默认按路由 meta 渲染，
 * 视图可通过 #actions 插槽放置主操作，或传 :page-head="false" 自行绘制。
 */
import '@/styles/onboarding.css'
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import { resolveRouteMetaKeys } from '@/router/title'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import PageHead from '@/components/console/PageHead.vue'
import SideRail from './SideRail.vue'
import ContextBar from './ContextBar.vue'

withDefaults(defineProps<{ pageHead?: boolean }>(), { pageHead: true })

const route = useRoute()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const mobileOpen = computed(() => appStore.mobileOpen)
const isAdmin = computed(() => authStore.user?.role === 'admin')

// /purchase 的标题/描述随站点计费模式切换，与 document.title 共用同一解析。
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

const pageDescription = computed(() => {
  const descKey = routeMetaKeys.value.descriptionKey
  if (descKey) return t(descKey)
  return (route.meta.description as string) || ''
})

function closeMobile() {
  appStore.setMobileOpen(false)
}

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

defineExpose({ replayTour })
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
