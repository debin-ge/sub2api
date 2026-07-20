<template>
  <header class="sticky top-0 z-30 border-b border-gray-200 bg-white/95 backdrop-blur dark:border-dark-800 dark:bg-dark-900/95">
    <!-- Optional reading-progress bar (docs) -->
    <div
      v-if="progress !== null && progress !== undefined"
      class="absolute inset-x-0 bottom-0 h-0.5 origin-left bg-primary-500 transition-transform duration-150 dark:bg-primary-400"
      :style="{ transform: `scaleX(${progress})` }"
      aria-hidden="true"
    ></div>

    <div
      class="mx-auto flex items-center justify-between gap-4 px-4 py-3.5 sm:px-6 lg:px-8"
      :class="containerClass"
    >
      <!-- Logo + site name -->
      <RouterLink to="/home" class="flex min-w-0 items-center gap-3">
        <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center overflow-hidden rounded-xl bg-white shadow-sm ring-1 ring-gray-200 dark:bg-dark-800 dark:ring-dark-700">
          <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
        </span>
        <span class="truncate text-base font-semibold text-gray-950 dark:text-white">
          {{ siteName }}
        </span>
      </RouterLink>

      <!-- Right-side controls -->
      <nav class="flex flex-shrink-0 items-center gap-1.5 sm:gap-2">
        <!-- Cross-page links (the two destinations that are not the current page) -->
        <RouterLink
          v-for="link in crossLinks"
          :key="link.to"
          :to="link.to"
          class="hidden rounded-lg px-3 py-2 text-sm font-medium text-gray-600 transition-colors hover:bg-gray-100 hover:text-gray-950 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white sm:inline-flex"
        >
          {{ link.label }}
        </RouterLink>

        <LocaleSwitcher />

        <!-- Theme toggle -->
        <button
          type="button"
          class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
          :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          @click="toggleTheme"
        >
          <Icon v-if="isDark" name="sun" size="md" />
          <Icon v-else name="moon" size="md" />
        </button>

        <!-- Auth actions -->
        <RouterLink
          v-if="isAuthenticated"
          :to="dashboardPath"
          class="inline-flex items-center justify-center rounded-lg bg-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-primary-600/20 transition hover:bg-primary-700"
        >
          {{ t('plaza.header.dashboard') }}
        </RouterLink>
        <template v-else>
          <RouterLink
            to="/login"
            class="hidden rounded-lg px-3 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-100 hover:text-gray-950 dark:text-dark-200 dark:hover:bg-dark-800 dark:hover:text-white sm:inline-flex"
          >
            {{ t('home.login') }}
          </RouterLink>
          <RouterLink
            to="/register"
            class="inline-flex items-center justify-center rounded-lg bg-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-primary-600/20 transition hover:bg-primary-700"
          >
            {{ t('plaza.header.register') }}
          </RouterLink>
        </template>
      </nav>
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore, useAuthStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

interface Props {
  /** Which page is showing the header — its own cross-link is omitted. */
  current: 'home' | 'plaza' | 'docs'
  /** 0–1 reading progress; when provided renders the thin progress bar. */
  progress?: number | null
  /** Container width class to align with page content (default max-w-7xl). */
  containerClass?: string
}

const props = withDefaults(defineProps<Props>(), {
  progress: null,
  containerClass: 'max-w-7xl',
})

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(
  appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '',
  { allowRelative: true, allowDataUrl: true },
))
const isAuthenticated = computed(() => authStore.isAuthenticated)
const dashboardPath = computed(() => (authStore.isAdmin ? '/admin/dashboard' : '/dashboard'))

const crossLinks = computed(() => {
  const all = [
    { key: 'home', to: '/home', label: t('plaza.header.home') },
    { key: 'plaza', to: '/plaza', label: t('plaza.header.label') },
    { key: 'docs', to: '/docs', label: t('plaza.header.docs') },
  ]
  return all.filter((link) => link.key !== props.current)
})

const isDark = ref(document.documentElement.classList.contains('dark'))

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}
</script>
