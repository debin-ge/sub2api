<template>
  <div class="gocaptcha-wrapper">
    <button
      type="button"
      class="gocaptcha-trigger"
      :class="verified ? 'gocaptcha-trigger--verified' : ''"
      :disabled="verified || loading"
      data-testid="gocaptcha-trigger"
      @click="openPanel"
    >
      <svg
        v-if="verified"
        class="gocaptcha-icon"
        viewBox="0 0 20 20"
        fill="currentColor"
        aria-hidden="true"
      >
        <path
          fill-rule="evenodd"
          d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z"
          clip-rule="evenodd"
        />
      </svg>
      <svg
        v-else
        class="gocaptcha-icon"
        viewBox="0 0 20 20"
        fill="currentColor"
        aria-hidden="true"
      >
        <path
          fill-rule="evenodd"
          d="M9.661 2.237a.531.531 0 01.678 0 11.947 11.947 0 007.078 2.749.5.5 0 01.479.425c.069.52.104 1.05.104 1.59 0 5.162-3.26 9.563-7.834 11.256a.48.48 0 01-.332 0C5.26 16.564 2 12.163 2 7c0-.538.035-1.069.104-1.589a.5.5 0 01.48-.425 11.947 11.947 0 007.077-2.75z"
          clip-rule="evenodd"
        />
      </svg>
      <span>{{ triggerText }}</span>
    </button>

    <Teleport to="body">
      <div
        v-if="panelVisible"
        class="gocaptcha-overlay"
        data-testid="gocaptcha-panel"
        @click.self="closePanel"
      >
        <div class="gocaptcha-panel">
          <p
            v-if="panelError"
            class="gocaptcha-error"
            role="alert"
            data-testid="gocaptcha-error"
          >
            {{ panelError }}
          </p>
          <GoCaptchaClick
            v-if="isClickMode && challenge"
            ref="clickRef"
            :data="clickData"
            :events="{ confirm: onClickConfirm, refresh: refreshChallenge, close: closePanel }"
            :config="panelConfig"
          />
          <GoCaptchaSlide
            v-else-if="challenge?.mode === 'slide'"
            ref="slideRef"
            :data="slideData"
            :events="{ confirm: onSlideConfirm, refresh: refreshChallenge, close: closePanel }"
            :config="panelConfig"
          />
          <GoCaptchaSlideRegion
            v-else-if="challenge?.mode === 'drag'"
            ref="slideRegionRef"
            :data="slideData"
            :events="{ confirm: onSlideConfirm, refresh: refreshChallenge, close: closePanel }"
            :config="panelConfig"
          />
          <GoCaptchaRotate
            v-else-if="challenge?.mode === 'rotate'"
            ref="rotateRef"
            :data="rotateData"
            :events="{ confirm: onRotateConfirm, refresh: refreshChallenge, close: closePanel }"
            :config="panelConfig"
          />
          <p v-else-if="loading" class="gocaptcha-loading">{{ t('auth.captchaLoading') }}</p>
          <div v-else class="gocaptcha-error-actions">
            <button type="button" class="gocaptcha-retry" @click="refreshChallenge">
              {{ t('auth.captchaRetry') }}
            </button>
            <button type="button" class="gocaptcha-close" @click="closePanel">
              {{ t('common.close') }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
// eslint-disable-next-line @typescript-eslint/ban-ts-comment
// @ts-ignore -- go-captcha-vue 未随包提供类型声明
import {
  Click as GoCaptchaClick,
  Rotate as GoCaptchaRotate,
  Slide as GoCaptchaSlide,
  SlideRegion as GoCaptchaSlideRegion
} from 'go-captcha-vue'

import {
  createGoCaptchaChallenge,
  verifyGoCaptcha,
  type GoCaptchaChallenge,
  type GoCaptchaMode
} from '@/api/auth'
import { extractApiErrorCode } from '@/utils/apiError'

const { t } = useI18n()

const MASTER_WIDTH = 300
const MASTER_HEIGHT = 220
const PANEL_EDGE_SPACE = 48
const MIN_RENDER_WIDTH = 180

withDefaults(defineProps<{ mode?: GoCaptchaMode }>(), { mode: 'click' })

const emit = defineEmits<{
  verify: [token: string]
  expire: []
  error: []
}>()

const panelVisible = ref<boolean>(false)
const loading = ref<boolean>(false)
const submitting = ref<boolean>(false)
const verified = ref<boolean>(false)
const challenge = ref<GoCaptchaChallenge | null>(null)
const issuedToken = ref<string>('')
const panelError = ref<string>('')
const viewportWidth = ref<number>(typeof window === 'undefined' ? 1024 : window.innerWidth)

// verifyAction 的等待者。用户关闭面板时以 null 兑现，与腾讯/阿里云控件语义一致。
let pendingResolve: ((token: string | null) => void) | null = null
let pendingPromise: Promise<string | null> | null = null
let tokenExpiryTimer: ReturnType<typeof setTimeout> | null = null

const isClickMode = computed(
  () => challenge.value?.mode === 'click' || challenge.value?.mode === 'shape'
)

const triggerText = computed(() => {
  if (verified.value) return t('auth.captchaVerified')
  if (loading.value || submitting.value) return t('auth.captchaLoading')
  return t('auth.captchaClickToVerify')
})

const renderWidth = computed(() =>
  Math.min(MASTER_WIDTH, Math.max(MIN_RENDER_WIDTH, viewportWidth.value - PANEL_EDGE_SPACE))
)
const renderScale = computed(() => renderWidth.value / MASTER_WIDTH)
const renderHeight = computed(() => Math.round(MASTER_HEIGHT * renderScale.value))

const panelTitle = computed(() => {
  switch (challenge.value?.mode) {
    case 'slide':
      return t('auth.captchaSlideTitle')
    case 'drag':
      return t('auth.captchaDragTitle')
    case 'rotate':
      return t('auth.captchaRotateTitle')
    default:
      return t('auth.captchaClickTitle')
  }
})

const panelConfig = computed(() => ({
  showTheme: false,
  title: panelTitle.value,
  buttonText: t('auth.captchaConfirm'),
  width: renderWidth.value,
  height: renderHeight.value,
  size: renderHeight.value,
  thumbWidth: Math.round(150 * renderScale.value),
  thumbHeight: Math.round(40 * renderScale.value),
  horizontalPadding: 8,
  verticalPadding: 8
}))

const clickData = computed(() => ({
  image: challenge.value?.master_image ?? '',
  thumb: challenge.value?.thumb_image ?? ''
}))

const slideData = computed(() => ({
  image: challenge.value?.master_image ?? '',
  thumb: challenge.value?.thumb_image ?? '',
  thumbX: Math.round((challenge.value?.tile_x ?? 0) * renderScale.value),
  thumbY: Math.round((challenge.value?.tile_y ?? 0) * renderScale.value),
  thumbWidth: Math.round((challenge.value?.tile_width ?? 0) * renderScale.value),
  thumbHeight: Math.round((challenge.value?.tile_height ?? 0) * renderScale.value)
}))

const rotateData = computed(() => ({
  image: challenge.value?.master_image ?? '',
  thumb: challenge.value?.thumb_image ?? '',
  thumbSize: Math.round((challenge.value?.thumb_size ?? 0) * renderScale.value)
}))

function captchaErrorStatus(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined
  const candidate = error as { status?: number; response?: { status?: number } }
  return candidate.status ?? candidate.response?.status
}

function isTooManyFailures(error: unknown): boolean {
  const code = extractApiErrorCode(error)
  return (
    code === 'GO_CAPTCHA_TOO_MANY_FAILURES' ||
    code === 'RATE_LIMIT_EXCEEDED' ||
    captchaErrorStatus(error) === 429
  )
}

async function loadChallenge(preserveError = false): Promise<boolean> {
  loading.value = true
  if (!preserveError) panelError.value = ''
  try {
    challenge.value = await createGoCaptchaChallenge()
    return true
  } catch (error: unknown) {
    challenge.value = null
    panelError.value = isTooManyFailures(error)
      ? t('auth.captchaTooManyFailures')
      : t('auth.captchaUnavailable')
    if (!isTooManyFailures(error)) emit('error')
    return false
  } finally {
    loading.value = false
  }
}

async function refreshChallenge(): Promise<void> {
  await loadChallenge(false)
}

async function openPanel(): Promise<void> {
  if (verified.value || loading.value || panelVisible.value) return
  panelVisible.value = true
  await loadChallenge(false)
}

function closePanel(): void {
  panelVisible.value = false
  challenge.value = null
  settlePending(null)
}

function settlePending(token: string | null): void {
  const resolve = pendingResolve
  pendingResolve = null
  pendingPromise = null
  resolve?.(token)
}

function waitForVerification(): Promise<string | null> {
  if (pendingPromise) return pendingPromise
  pendingPromise = new Promise<string | null>((resolve) => {
    pendingResolve = resolve
  })
  return pendingPromise
}

function clearTokenExpiryTimer(): void {
  if (tokenExpiryTimer) {
    clearTimeout(tokenExpiryTimer)
    tokenExpiryTimer = null
  }
}

function armTokenExpiry(expiresIn: number): void {
  clearTokenExpiryTimer()
  const milliseconds = Math.max(0, Math.floor(expiresIn * 1000))
  tokenExpiryTimer = setTimeout(() => {
    tokenExpiryTimer = null
    issuedToken.value = ''
    verified.value = false
    emit('expire')
  }, milliseconds)
}

/**
 * 提交作答。答错时立刻换一张新图 —— 一个挑战只有一次作答机会，
 * 服务端在校验时已经把它取走了，沿用旧图重试必定失败。
 */
async function submit(answer: string, resetPanel: () => void): Promise<boolean> {
  const captchaId = challenge.value?.captcha_id
  if (!captchaId || submitting.value) return false

  submitting.value = true
  try {
    const { token, expires_in: expiresIn } = await verifyGoCaptcha(captchaId, answer)
    verified.value = true
    issuedToken.value = token
    panelError.value = ''
    panelVisible.value = false
    challenge.value = null
    armTokenExpiry(expiresIn)
    emit('verify', token)
    settlePending(token)
    return true
  } catch (error: unknown) {
    resetPanel()
    if (isTooManyFailures(error)) {
      challenge.value = null
      panelError.value = t('auth.captchaTooManyFailures')
      return false
    }
    panelError.value = t('auth.captchaIncorrect')
    await loadChallenge(true)
    return false
  } finally {
    submitting.value = false
  }
}

function onClickConfirm(dots: Array<{ x: number; y: number }>, resetPanel: () => void): boolean {
  void submit(
    dots
      .flatMap((dot) => [
        Math.round(dot.x / renderScale.value),
        Math.round(dot.y / renderScale.value)
      ])
      .join(','),
    resetPanel
  )
  return false
}

function onSlideConfirm(point: { x: number; y: number }, resetPanel: () => void): boolean {
  void submit(
    `${Math.round(point.x / renderScale.value)},${Math.round(point.y / renderScale.value)}`,
    resetPanel
  )
  return false
}

function onRotateConfirm(angle: number, resetPanel: () => void): boolean {
  void submit(String(Math.round(angle)), resetPanel)
  return false
}

/**
 * 弹出面板并等待用户完成验证。
 * 已有未消费的令牌时直接返回，避免同一次提交里重复弹窗。
 */
async function verify(): Promise<string | null> {
  if (issuedToken.value) {
    const token = issuedToken.value
    issuedToken.value = ''
    verified.value = false
    clearTokenExpiryTimer()
    return token
  }
  if (pendingPromise) return pendingPromise

  const pending = waitForVerification()
  panelVisible.value = true
  if (!challenge.value && !(await loadChallenge(false))) {
    // Keep the panel open so the user can see the friendly error and retry or close.
  }
  return pending
}

function reset(): void {
  clearTokenExpiryTimer()
  verified.value = false
  issuedToken.value = ''
  challenge.value = null
  panelError.value = ''
  panelVisible.value = false
  settlePending(null)
}

function updateViewportWidth(): void {
  viewportWidth.value = window.innerWidth
}

onMounted(() => window.addEventListener('resize', updateViewportWidth))
onBeforeUnmount(() => {
  window.removeEventListener('resize', updateViewportWidth)
  clearTokenExpiryTimer()
  settlePending(null)
})

defineExpose({ verify, reset })
</script>

<style scoped>
.gocaptcha-wrapper {
  width: 100%;
}

.gocaptcha-trigger {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  border-radius: 0.75rem;
  border: 1px solid rgb(229 231 235);
  background-color: rgb(255 255 255);
  padding: 0.625rem 1rem;
  font-size: 0.875rem;
  font-weight: 500;
  color: rgb(55 65 81);
  transition: all 0.15s ease;
}

.gocaptcha-trigger:hover:not(:disabled) {
  border-color: rgb(20 184 166);
  color: rgb(13 148 136);
}

.gocaptcha-trigger:disabled {
  cursor: default;
}

.gocaptcha-trigger--verified {
  border-color: rgb(20 184 166);
  color: rgb(13 148 136);
}

.gocaptcha-icon {
  height: 1.125rem;
  width: 1.125rem;
}

.gocaptcha-overlay {
  position: fixed;
  inset: 0;
  z-index: 60;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgb(15 23 42 / 45%);
  padding: 0.5rem;
}

.gocaptcha-panel {
  border-radius: 0.75rem;
  background-color: rgb(255 255 255);
  max-width: 100%;
  padding: 0.5rem;
}

.gocaptcha-error {
  margin: 0 0 0.5rem;
  border-radius: 0.5rem;
  background-color: rgb(254 242 242);
  padding: 0.5rem 0.75rem;
  font-size: 0.8125rem;
  color: rgb(185 28 28);
}

.gocaptcha-error-actions {
  display: flex;
  min-width: 16rem;
  justify-content: center;
  gap: 0.75rem;
  padding: 1rem;
}

.gocaptcha-retry,
.gocaptcha-close {
  border-radius: 0.5rem;
  padding: 0.5rem 0.875rem;
  font-size: 0.875rem;
  font-weight: 500;
}

.gocaptcha-retry {
  background-color: rgb(13 148 136);
  color: white;
}

.gocaptcha-close {
  background-color: rgb(241 245 249);
  color: rgb(51 65 85);
}

.gocaptcha-loading {
  min-width: 16rem;
  padding: 2rem 1rem;
  text-align: center;
  font-size: 0.875rem;
  color: rgb(107 114 128);
}

:global(.dark) .gocaptcha-trigger {
  border-color: rgb(51 65 85);
  background-color: rgb(30 41 59);
  color: rgb(226 232 240);
}

:global(.dark) .gocaptcha-trigger--verified {
  border-color: rgb(45 212 191);
  color: rgb(94 234 212);
}

:global(.dark) .gocaptcha-panel {
  --go-captcha-theme-text-color: rgb(226 232 240);
  --go-captcha-theme-bg-color: rgb(30 41 59);
  --go-captcha-theme-icon-color: rgb(203 213 225);
  --go-captcha-theme-body-bg-color: rgb(15 23 42);
  --go-captcha-theme-drag-bar-color: rgb(71 85 105);
  --go-captcha-theme-drag-bg-color: rgb(13 148 136);
  --go-captcha-theme-round-color: rgb(71 85 105);
  --go-captcha-theme-loading-icon-color: rgb(45 212 191);
  --go-captcha-theme-dot-bg-color: rgb(13 148 136);
  --go-captcha-theme-btn-bg-color: rgb(13 148 136);
  --go-captcha-theme-btn-border-color: rgb(13 148 136);
  --go-captcha-theme-btn-disabled-color: rgb(15 118 110);
  background-color: rgb(30 41 59);
}

:global(.dark) .gocaptcha-error {
  background-color: rgb(127 29 29 / 35%);
  color: rgb(254 202 202);
}

:global(.dark) .gocaptcha-close {
  background-color: rgb(51 65 85);
  color: rgb(226 232 240);
}

:global(.dark) .gocaptcha-loading {
  color: rgb(148 163 184);
}
</style>
