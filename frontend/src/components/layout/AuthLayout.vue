<template>
  <div class="grid min-h-screen md:grid-cols-2">
    <!-- Left: brand panel (aurora + starfield, hidden below md) -->
    <div
      class="relative hidden flex-col overflow-hidden bg-gradient-to-b from-primary-950 to-dark-900 p-10 text-white md:flex lg:p-14"
    >
      <div class="aurora-1 absolute -left-24 -top-28 h-[420px] w-[420px] rounded-full bg-primary-600/25 blur-[130px]"></div>
      <div class="aurora-2 absolute -bottom-32 -right-28 h-[460px] w-[460px] rounded-full bg-cyan-400/20 blur-[140px]"></div>
      <div class="aurora-3 absolute left-[38%] top-[28%] h-[380px] w-[380px] rounded-full bg-primary-400/15 blur-[120px]"></div>

      <span class="star-twinkle absolute h-0.5 w-0.5 rounded-full bg-white/40" style="top: 8%; left: 22%; animation-duration: 3.4s"></span>
      <span class="star-twinkle absolute h-1 w-1 rounded-full bg-cyan-200/50" style="top: 15%; left: 62%; animation-duration: 4.2s; animation-delay: 0.6s"></span>
      <span class="star-twinkle absolute h-0.5 w-0.5 rounded-full bg-white/35" style="top: 12%; left: 85%; animation-duration: 3.8s; animation-delay: 1.4s"></span>
      <span class="star-twinkle absolute h-1.5 w-1.5 rounded-full bg-primary-200/40" style="top: 26%; left: 8%; animation-duration: 5s; animation-delay: 0.3s"></span>
      <span class="star-twinkle absolute h-0.5 w-0.5 rounded-full bg-white/40" style="top: 33%; left: 46%; animation-duration: 3s; animation-delay: 2.1s"></span>
      <span class="star-twinkle absolute h-1 w-1 rounded-full bg-cyan-200/45" style="top: 40%; left: 90%; animation-duration: 4.6s; animation-delay: 0.9s"></span>
      <span class="star-twinkle absolute h-0.5 w-0.5 rounded-full bg-white/30" style="top: 48%; left: 16%; animation-duration: 3.6s; animation-delay: 1.8s"></span>
      <span class="star-twinkle absolute h-1 w-1 rounded-full bg-white/35" style="top: 55%; left: 72%; animation-duration: 4.4s; animation-delay: 0.2s"></span>
      <span class="star-twinkle absolute h-0.5 w-0.5 rounded-full bg-cyan-200/40" style="top: 62%; left: 34%; animation-duration: 3.2s; animation-delay: 2.6s"></span>
      <span class="star-twinkle absolute h-1.5 w-1.5 rounded-full bg-primary-200/35" style="top: 68%; left: 88%; animation-duration: 5.2s; animation-delay: 1.1s"></span>
      <span class="star-twinkle absolute h-0.5 w-0.5 rounded-full bg-white/40" style="top: 76%; left: 12%; animation-duration: 3.9s; animation-delay: 0.5s"></span>
      <span class="star-twinkle absolute h-1 w-1 rounded-full bg-cyan-200/45" style="top: 83%; left: 52%; animation-duration: 4.1s; animation-delay: 2.3s"></span>
      <span class="star-twinkle absolute h-0.5 w-0.5 rounded-full bg-white/30" style="top: 90%; left: 78%; animation-duration: 3.5s; animation-delay: 1.6s"></span>
      <span class="star-twinkle absolute h-1 w-1 rounded-full bg-white/35" style="top: 94%; left: 30%; animation-duration: 4.8s; animation-delay: 0.8s"></span>

      <!-- Top: brand row -->
      <template v-if="settingsLoaded">
        <router-link to="/" class="relative flex items-center gap-3">
          <div class="flex h-9 w-9 items-center justify-center overflow-hidden rounded-lg shadow-glow">
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <span class="text-lg font-bold">{{ siteName }}</span>
        </router-link>
      </template>
      <div v-else class="relative h-9"></div>

      <!-- Middle: headline + checklist -->
      <div class="relative flex flex-1 flex-col justify-center">
        <div class="max-w-sm">
          <h2 class="text-3xl font-bold leading-tight">
            {{ t('radar.auth.brand.headline', 'One gateway, endless AI models') }}
          </h2>
          <p class="mt-4 text-sm leading-6 text-white/70">
            {{
              t(
                'radar.auth.brand.subtitle',
                'One standardized protocol connects your apps to leading AI models, with keys and quotas centrally managed for reliable, always-on access.',
              )
            }}
          </p>
          <ul class="mt-6 space-y-3">
            <li v-for="item in checklist" :key="item" class="flex items-start gap-2 text-sm text-white/80">
              <Icon name="check" size="xs" class="mt-0.5 shrink-0 text-cyan-300" />
              {{ item }}
            </li>
          </ul>
        </div>
      </div>
    </div>

    <!-- Right: form panel -->
    <div class="relative flex items-center justify-center bg-white px-4 py-12 dark:bg-dark-900">
      <div class="w-full max-w-md">
        <!-- Logo/Brand (mobile fallback, left panel is hidden below md) -->
        <div class="mb-8 text-center md:hidden">
          <template v-if="settingsLoaded">
            <div
              class="mb-4 inline-flex h-16 w-16 items-center justify-center overflow-hidden rounded-2xl shadow-lg shadow-primary-500/30"
            >
              <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
            </div>
            <h1 class="text-gradient mb-2 text-3xl font-bold">
              {{ siteName }}
            </h1>
            <p class="text-sm text-gray-500 dark:text-dark-400">
              {{ siteSubtitle }}
            </p>
          </template>
        </div>

        <!-- Card Container -->
        <div class="card-glass rounded-2xl p-8 shadow-glass">
          <slot />
        </div>

        <!-- Footer Links -->
        <div class="mt-6 text-center text-sm">
          <slot name="footer" />
        </div>

        <!-- Copyright -->
        <div class="mt-8 text-center text-xs text-gray-400 dark:text-dark-500">
          &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

const { t } = useI18n()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'ZenTok')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway')
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

const currentYear = computed(() => new Date().getFullYear())

const checklist = computed(() => [
  t('radar.auth.brand.checklist.unifiedApi', 'Unified access to 13+ leading model providers'),
  t('radar.auth.brand.checklist.smartRouting', 'Smart routing with automatic failover'),
  t('radar.auth.brand.checklist.billing', 'Real-time usage and cost visibility'),
  t('radar.auth.brand.checklist.security', 'Enterprise-grade key security and access control'),
])

onMounted(() => {
  appStore.fetchPublicSettings()
})
</script>
