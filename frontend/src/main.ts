import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import i18n, { initI18n } from './i18n'
import { useAppStore } from '@/stores/app'
import { initializePublicSettings } from '@/startup/publicSettings'
import { updateFavicon } from '@/utils/branding'
import {
  isChunkLoadError,
  notifyFrontendUpdateRequired,
  recoverFromChunkLoadError
} from '@/utils/chunkLoadRecovery'
import { isIOSDevice } from '@/utils/device'
import './style.css'
import './assets/styles/docsContent.css'

function handleVitePreloadError(event: Event) {
  const preloadEvent = event as Event & { payload?: unknown }
  if (!isChunkLoadError(preloadEvent.payload)) {
    return
  }

  event.preventDefault()
  const recovery = recoverFromChunkLoadError(preloadEvent.payload)
  if (recovery === 'already-reloaded') {
    notifyFrontendUpdateRequired()
  }
}

window.addEventListener('vite:preloadError', handleVitePreloadError)

function initIOSViewportZoomFix() {
  // iOS Safari 在输入框字号小于 16px 时聚焦会自动放大页面，且失焦后不会恢复。
  // 限制 maximum-scale 可阻止该行为；iOS 10+ 用户仍可双指手动缩放，不影响可访问性。
  // 仅在 iOS 设备上注入，避免影响 Android Chrome 的手动缩放能力。
  if (!isIOSDevice()) return

  const viewport = document.querySelector('meta[name="viewport"]')
  if (!viewport) return

  const content = viewport.getAttribute('content') || ''
  if (/maximum-scale/i.test(content)) return
  viewport.setAttribute('content', `${content}, maximum-scale=1.0`)
}

function initThemeClass() {
  const savedTheme = localStorage.getItem('theme')
  const shouldUseDark =
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', shouldUseDark)
}

async function bootstrap() {
  // Apply theme class globally before app mount to keep all routes consistent.
  initThemeClass()
  initIOSViewportZoomFix()

  const app = createApp(App)
  const pinia = createPinia()
  app.use(pinia)

  // Initialize branding BEFORE the router's first navigation and app mount.
  // If server injection is unavailable, wait for the normal settings request.
  const appStore = useAppStore()
  await Promise.all([
    initializePublicSettings(appStore),
    initI18n()
  ])

  // Set document title/favicon immediately after config is loaded. The static
  // HTML uses neutral placeholders so missing injection never exposes the
  // built-in brand while the real settings request is still in flight.
  if (appStore.siteName) {
    document.title = `${appStore.siteName} - AI API Gateway`
  }
  updateFavicon(appStore.siteLogo || '/logo.svg')

  app.use(router)
  app.use(i18n)

  // 等待路由器完成初始导航后再挂载，避免竞态条件导致的空白渲染
  try {
    await router.isReady()
  } catch (error) {
    const recovery = recoverFromChunkLoadError(error)
    if (recovery === 'reloading') {
      return
    }
    if (recovery === 'already-reloaded') {
      notifyFrontendUpdateRequired()
    } else {
      console.error('Initial router navigation failed:', error)
    }
  }

  // Mount even after a persistent chunk failure so the toast host can explain
  // that the user must reopen the updated application.
  app.mount('#app')
}

bootstrap().catch((error) => {
  console.error('Failed to bootstrap application:', error)
})
