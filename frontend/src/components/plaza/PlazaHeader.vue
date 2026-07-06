<template>
  <header class="border-b border-gray-200 bg-white/95 dark:border-dark-800 dark:bg-dark-950/95">
    <nav class="mx-auto flex max-w-7xl items-center justify-between gap-4 px-4 py-3 sm:px-6 lg:px-8">
      <router-link to="/plaza" class="flex min-w-0 items-center gap-3">
        <img :src="siteLogo || '/logo.png'" alt="" class="h-8 w-8 shrink-0 rounded-md object-contain" />
        <div class="min-w-0">
          <div class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ siteName }}</div>
          <div class="hidden text-xs text-gray-500 dark:text-gray-400 sm:block">{{ t('plaza.header.label') }}</div>
        </div>
      </router-link>

      <div class="flex items-center gap-2">
        <LocaleSwitcher />
        <button
          type="button"
          class="rounded-lg p-2 text-gray-500 transition hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-dark-800 dark:hover:text-white"
          :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          @click="toggleTheme"
        >
          <Icon v-if="isDark" name="sun" size="md" />
          <Icon v-else name="moon" size="md" />
        </button>

        <router-link
          v-if="isAuthenticated"
          :to="dashboardPath"
          class="rounded-lg bg-gray-900 px-3 py-2 text-sm font-medium text-white transition hover:bg-gray-800 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
        >
          {{ t('plaza.header.dashboard') }}
        </router-link>
        <template v-else>
          <router-link
            to="/login"
            class="hidden rounded-lg px-3 py-2 text-sm font-medium text-gray-700 transition hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-800 sm:inline-flex"
          >
            {{ t('common.login') }}
          </router-link>
          <router-link
            to="/register"
            class="rounded-lg bg-gray-900 px-3 py-2 text-sm font-medium text-white transition hover:bg-gray-800 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
          >
            {{ t('plaza.header.register') }}
          </router-link>
        </template>
      </div>
    </nav>
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

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}
</script>
