<template>
  <header class="sticky top-0 z-40 border-b border-gray-200 bg-white/95 backdrop-blur dark:border-dark-800 dark:bg-dark-950/95">
    <div class="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-6 lg:px-8">
      <router-link to="/" class="flex min-w-0 items-center gap-3">
        <img
          :src="siteLogo || '/logo.svg'"
          alt=""
          class="h-8 w-8 shrink-0 rounded-md object-contain"
        />
        <span class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ siteName }}</span>
      </router-link>

      <div class="flex shrink-0 items-center gap-1 sm:gap-2">
        <LocaleSwitcher />
        <button
          type="button"
          class="rounded-lg p-2 text-gray-500 transition hover:bg-gray-100 hover:text-gray-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-gray-400 dark:hover:bg-dark-800 dark:hover:text-white"
          :aria-label="isDark ? t('radar.header.lightTheme', 'Switch to light theme') : t('radar.header.darkTheme', 'Switch to dark theme')"
          @click="toggleTheme"
        >
          <Icon :name="isDark ? 'sun' : 'moon'" size="md" aria-hidden="true" />
        </button>
        <router-link
          :to="isAuthenticated ? dashboardPath : '/login'"
          class="rounded-lg bg-gray-900 px-3 py-2 text-sm font-medium text-white transition hover:bg-gray-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
        >
          {{ isAuthenticated ? t('radar.header.dashboard', 'Dashboard') : t('radar.header.login', 'Log in') }}
        </router-link>
      </div>
    </div>

  </header>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore, useAuthStore } from '@/stores'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const isDark = ref(document.documentElement.classList.contains('dark'))
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '')
const isAuthenticated = computed(() => authStore.isAuthenticated)
const dashboardPath = computed(() => (authStore.isAdmin ? '/admin/dashboard' : '/dashboard'))

function toggleTheme(): void {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

</script>
