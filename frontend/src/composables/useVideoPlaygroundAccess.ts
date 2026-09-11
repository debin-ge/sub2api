import { computed, ref } from 'vue'
import { keysAPI } from '@/api/keys'
import { useAuthStore } from '@/stores/auth'
import type { ApiKey } from '@/types'

const loaded = ref(false)
const loading = ref(false)
const hasAllowedVideoKey = ref(false)
let pendingLoad: Promise<boolean> | null = null
const pageSize = 100

/** 网关只把 /v1/videos 放给这三个平台，其余平台连路由都进不去。 */
export const VIDEO_PLAYGROUND_PLATFORMS = ['openai', 'bytedance', 'composite'] as const

/**
 * 判定一个 Key 能否用于视频 Playground。
 *
 * 三条依据都在后端：平台白名单来自网关路由的平台门控，订阅制分组被
 * VideoTaskService 硬拒（视频按次计费，订阅额度模型对不上）。
 *
 * 注意这里**没有** allow_video_generation 这样的分组开关——该字段从未落地
 * （见 migrations/220_clear_non_grok_video_generation_config.sql），不要为
 * 本功能臆造一个。
 */
export function keyAllowsVideoPlayground(key: ApiKey): boolean {
  const platform = key.group?.platform
  return (
    key.status === 'active' &&
    !!platform &&
    (VIDEO_PLAYGROUND_PLATFORMS as readonly string[]).includes(platform) &&
    key.group?.subscription_type !== 'subscription'
  )
}

async function loadVideoPlaygroundAccess(force = false): Promise<boolean> {
  const authStore = useAuthStore()
  if (!authStore.isAuthenticated) {
    loaded.value = true
    hasAllowedVideoKey.value = false
    return false
  }

  if (loaded.value && !force) {
    return hasAllowedVideoKey.value
  }

  if (pendingLoad && !force) {
    return pendingLoad
  }

  loading.value = true
  pendingLoad = (async () => {
    let page = 1
    while (true) {
      const response = await keysAPI.list(page, pageSize, {
        status: 'active',
        sort_by: 'created_at',
        sort_order: 'desc'
      })

      if ((response.items || []).some(keyAllowsVideoPlayground)) {
        hasAllowedVideoKey.value = true
        loaded.value = true
        return true
      }

      if (page >= response.pages || (response.items || []).length === 0) {
        hasAllowedVideoKey.value = false
        loaded.value = true
        return false
      }

      page += 1
    }
  })()
    .catch(() => {
      hasAllowedVideoKey.value = false
      loaded.value = true
      return false
    })
    .finally(() => {
      loading.value = false
      pendingLoad = null
    })

  return pendingLoad
}

export function useVideoPlaygroundAccess() {
  const canUseVideoPlayground = computed(() => hasAllowedVideoKey.value)

  return {
    canUseVideoPlayground,
    videoPlaygroundAccessLoaded: computed(() => loaded.value),
    videoPlaygroundAccessLoading: computed(() => loading.value),
    refreshVideoPlaygroundAccess: loadVideoPlaygroundAccess,
  }
}
