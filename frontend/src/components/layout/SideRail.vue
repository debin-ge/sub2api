<template>
  <aside class="zt-rail" :aria-label="t('nav.rail.openMenu')">
    <div class="zt-rail-fx tech-grid" aria-hidden="true"></div>
    <div class="zt-rail-glow" aria-hidden="true"></div>

    <!-- 品牌区 + 区域切换 -->
    <div class="zt-rail-top">
      <router-link :to="homePath" class="zt-rail-brand" @click="handleMenuItemClick(homePath)">
        <span class="zt-rail-logo">
          <img v-if="settingsLoaded" :src="siteLogo || '/logo.svg'" alt="Logo" />
        </span>
        <div class="min-w-0">
          <span class="block truncate">{{ siteName }}</span>
          <small>AI Gateway</small>
        </div>
      </router-link>
      <div v-if="isAdmin" class="zt-area" role="tablist">
        <button
          v-for="opt in areaOptions"
          :key="opt.key"
          type="button"
          role="tab"
          :aria-selected="area === opt.key"
          :class="{ 'is-on': area === opt.key }"
          @click="switchArea(opt.key)"
        >
          {{ opt.label }}
        </button>
      </div>
    </div>

    <!-- 导航 -->
    <nav ref="sidebarNavRef" class="zt-rail-nav scrollbar-hide">
      <template v-for="group in groups" :key="group.key">
        <button
          type="button"
          class="zt-nav-group"
          :class="{ 'is-closed': isGroupCollapsed(group.key), 'is-active': isGroupCollapsed(group.key) && active?.group.key === group.key }"
          :title="isGroupCollapsed(group.key) ? t('nav.rail.expandGroup') : t('nav.rail.collapseGroup')"
          @click="toggleGroup(group.key)"
        >
          <span>{{ group.label }}</span>
          <ChevronDownIcon />
        </button>
        <div v-show="!isGroupCollapsed(group.key)">
          <template v-for="item in group.items" :key="item.path">
            <!-- 可展开父项（改版前的 children / expandOnly 语义） -->
            <template v-if="item.children?.length">
              <button
                type="button"
                class="zt-nav-link"
                :class="{
                  'is-active': isGroupActive(item) && !isGroupExpanded(item),
                  'is-open': isGroupExpanded(item)
                }"
                :title="sidebarCollapsed ? item.label : undefined"
                @click="handleGroupClick(item)"
              >
                <component :is="item.icon" />
                <span class="zt-nav-label">{{ item.label }}</span>
                <ChevronDownIcon class="zt-nav-chev" />
              </button>
              <div v-if="!sidebarCollapsed && isGroupExpanded(item)" class="zt-nav-children">
                <router-link
                  v-for="child in item.children"
                  :key="child.path"
                  :to="child.path"
                  class="zt-nav-link"
                  :class="{ 'is-active': route.path === child.path }"
                  @click="handleMenuItemClick(child.path)"
                >
                  <component :is="child.icon" />
                  <span class="zt-nav-label">{{ child.label }}</span>
                </router-link>
              </div>
            </template>
            <!-- 普通条目 -->
            <router-link
              v-else
              :id="item.tourId"
              :to="item.path"
              class="zt-nav-link"
              :class="{ 'is-active': isActive(item.path, item.exact) }"
              :title="sidebarCollapsed ? item.label : undefined"
              :data-tour="item.tourAttr"
              @click="handleMenuItemClick(item.path)"
            >
              <span v-if="item.iconSvg" class="zt-nav-svg" v-html="sanitizeSvg(item.iconSvg)"></span>
              <component v-else :is="item.icon" />
              <span class="zt-nav-label">{{ item.label }}</span>
            </router-link>
          </template>
        </div>
      </template>
    </nav>

    <!-- 底部：用户卡 / 主题 / 收起 -->
    <div class="zt-rail-foot">
      <RailUserMenu />
      <button
        type="button"
        class="zt-nav-link"
        :title="sidebarCollapsed ? (isDark ? t('nav.lightMode') : t('nav.darkMode')) : undefined"
        @click="toggleTheme"
      >
        <SunIcon v-if="isDark" style="color: #fbbf24" />
        <MoonIcon v-else />
        <span class="zt-nav-label">{{ isDark ? t('nav.lightMode') : t('nav.darkMode') }}</span>
      </button>
      <button
        type="button"
        class="zt-nav-link"
        :title="sidebarCollapsed ? t('nav.expand') : t('nav.collapse')"
        @click="toggleSidebar"
      >
        <ChevronDoubleLeftIcon v-if="!sidebarCollapsed" />
        <ChevronDoubleRightIcon v-else />
        <span class="zt-nav-label">{{ t('nav.collapse') }}</span>
      </button>
    </div>
  </aside>
</template>

<script setup lang="ts">
/**
 * 分域侧栏：菜单集合、父子结构、功能开关、简单模式、自定义菜单项与新手引导锚点
 * 全部来自 navigation/consoleNav.ts（与改版前 AppSidebar 一致），这里只负责渲染与交互。
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import { sanitizeSvg } from '@/utils/sanitize'
import { sanitizeUrl } from '@/utils/url'
import { useConsoleNav } from '@/composables/useConsoleNav'
import { areaHome, type NavArea, type NavItem } from '@/navigation/consoleNav'
import { ChevronDoubleLeftIcon, ChevronDoubleRightIcon, ChevronDownIcon, MoonIcon, SunIcon } from '@/navigation/navIcons'
import RailUserMenu from './RailUserMenu.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()
const onboardingStore = useOnboardingStore()
const { area, groups, active, isAdmin } = useConsoleNav()

const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const mobileOpen = computed(() => appStore.mobileOpen)
const sidebarNavRef = ref<HTMLElement | null>(null)
const isDark = ref(document.documentElement.classList.contains('dark'))

const homePath = computed(() => areaHome(area.value))

// Site settings from appStore (cached, no flicker)
const siteName = computed(() => appStore.siteName)
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

const areaOptions = computed(() => [
  { key: 'console' as NavArea, label: t('nav.area.console') },
  { key: 'admin' as NavArea, label: t('nav.area.admin') },
])

function switchArea(target: NavArea) {
  if (target === area.value) return
  const to = areaHome(target)
  if (route.path !== to) router.push(to)
  handleMenuItemClick(to)
}

// ---------- 域分组折叠（持久化） ----------
function isGroupCollapsed(key: string): boolean {
  return appStore.navCollapsedGroups[key] === true
}

function toggleGroup(key: string) {
  if (sidebarCollapsed.value) return
  appStore.setNavGroupCollapsed(key, !isGroupCollapsed(key))
}

// ---------- 可展开父项（改版前语义） ----------
// Per-parent expand/collapse overrides. A parent with no entry follows the
// automatic behavior (expanded while the active route is one of its children);
// a chevron click records the user's choice, which wins over the automatic
// state so an active parent can still be collapsed manually.
const groupExpandOverrides = ref<Map<string, boolean>>(new Map())

function isActive(path: string, exact = false): boolean {
  return route.path === path || (!exact && route.path.startsWith(path + '/'))
}

function isGroupActive(item: NavItem): boolean {
  if (!item.children) return false
  return item.children.some((child) => route.path === child.path)
}

function isGroupExpanded(item: NavItem): boolean {
  const override = groupExpandOverrides.value.get(item.path)
  if (override !== undefined) return override
  return isGroupActive(item)
}

function toggleParent(item: NavItem) {
  groupExpandOverrides.value.set(item.path, !isGroupExpanded(item))
}

/**
 * Click handler for collapsible parent items.
 * - When sidebar is collapsed: do nothing (children are not visible).
 * - When `expandOnly` is true: only toggle expand state.
 * - Otherwise: navigate to the parent path and ensure the group is expanded.
 */
function handleGroupClick(item: NavItem) {
  if (sidebarCollapsed.value) return
  if (item.expandOnly) {
    toggleParent(item)
    return
  }
  if (route.path !== item.path) {
    router.push(item.path)
  }
  groupExpandOverrides.value.set(item.path, true)
}

// ---------- 通用交互 ----------
function toggleSidebar() {
  appStore.toggleSidebar()
}

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

function handleMenuItemClick(itemPath: string) {
  if (mobileOpen.value) {
    setTimeout(() => {
      appStore.setMobileOpen(false)
    }, 150)
  }
  // Map paths to tour selectors
  const pathToSelector: Record<string, string> = {
    '/admin/groups': '#sidebar-group-manage',
    '/admin/accounts': '#sidebar-channel-manage',
    '/keys': '[data-tour="sidebar-my-keys"]'
  }
  const selector = pathToSelector[itemPath]
  if (selector && onboardingStore.isCurrentStep(selector)) {
    onboardingStore.nextStep(500)
  }
}

onMounted(() => {
  // Restore sidebar scroll position after route change re-mounts the component
  if (appStore.sidebarScrollTop > 0 && sidebarNavRef.value) {
    void nextTick(() => {
      if (sidebarNavRef.value) {
        sidebarNavRef.value.scrollTop = appStore.sidebarScrollTop
      }
    })
  }
})

onBeforeUnmount(() => {
  if (sidebarNavRef.value) {
    appStore.sidebarScrollTop = sidebarNavRef.value.scrollTop
  }
})

defineExpose({ isAdmin, authStore })
</script>

<style scoped>
/* Custom SVG icon in sidebar: constrain size without overriding uploaded SVG colors */
.zt-nav-svg {
  color: currentColor;
}

.zt-nav-svg :deep(svg) {
  display: block;
  width: 1rem;
  height: 1rem;
}
</style>
