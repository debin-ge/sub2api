import { computed, ref, type ComputedRef, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'

import type CaptchaChallenge from '@/components/CaptchaChallenge.vue'
import type { ActionCaptchaRequestProof, PublicSettings } from '@/types'

type CaptchaChallengeInstance = InstanceType<typeof CaptchaChallenge>

/**
 * 随业务请求提交的验证凭据（snake_case，与后端请求体一致）。
 *
 * 阿里云的 captchaVerifyParam 与自建验证码的一次性令牌都复用 `turnstile_token`
 * 字段，这是后端既有约定，好处是 OAuth 回调页等透传层无需为新 provider 改动。
 */
export interface CaptchaRequestPayload {
  turnstile_token?: string
  tencent_captcha_ticket?: string
  tencent_captcha_randstr?: string
}

/** `<CaptchaChallenge>` 的完整 props，供 v-bind 展开。 */
export interface CaptchaChallengeProps {
  turnstileEnabled: boolean
  turnstileSiteKey: string
  tencentEnabled: boolean
  tencentAppId: string
  tencentRegion: string
  aliyunEnabled: boolean
  aliyunSceneId: string
  aliyunPrefix: string
  aliyunRegion: string
  goCaptchaEnabled: boolean
  goCaptchaMode: string
}

/** 子组件向父页面 emit 时使用的 camelCase 形态。 */
export interface CaptchaEmitPayload {
  turnstileToken?: string
  tencentCaptchaTicket?: string
  tencentCaptchaRandstr?: string
}

/**
 * 一个验证凭据槽位。同一个页面可以有多个槽位共享同一份 provider 配置，
 * 例如 EmailVerifyView 的「重发验证码」与「创建账号」各占一个。
 */
export interface CaptchaProofSlot {
  captchaRef: Ref<CaptchaChallengeInstance | null>
  token: Ref<string>
  randstr: Ref<string>
  onVerify: (token: string, randstr?: string) => void
  onExpire: () => void
  onError: () => void
  reset: () => void
  /** 动作触发式 provider 弹窗取证。未启用动作式时直接返回 true。 */
  acquireActionProof: () => Promise<boolean>
  requestPayload: () => CaptchaRequestPayload
  emitPayload: () => CaptchaEmitPayload
}

export interface UseCaptchaReturn extends CaptchaProofSlot {
  turnstileEnabled: Ref<boolean>
  turnstileSiteKey: Ref<string>
  tencentCaptchaEnabled: Ref<boolean>
  tencentCaptchaAppId: Ref<string>
  tencentCaptchaRegion: Ref<string>
  aliyunCaptchaEnabled: Ref<boolean>
  aliyunCaptchaSceneId: Ref<string>
  aliyunCaptchaPrefix: Ref<string>
  aliyunCaptchaRegion: Ref<string>
  goCaptchaEnabled: Ref<boolean>
  goCaptchaMode: Ref<string>
  /** 传给 `<CaptchaChallenge v-bind="captchaProps">` 的完整 props。 */
  captchaProps: ComputedRef<CaptchaChallengeProps>
  /** 是否存在任一「提交时弹窗」的 provider（腾讯 / 阿里云 / 自建）。 */
  actionCaptchaEnabled: ComputedRef<boolean>
  /** 是否需要展示验证控件。 */
  captchaEnabled: ComputedRef<boolean>
  /** Turnstile 是常驻渲染，提交前必须已有 token；其余 provider 提交时才取证。 */
  inlineCaptchaPending: ComputedRef<boolean>
  applyPublicSettings: (settings: PublicSettings) => void
  /** 公开配置拉取失败时回到「全部关闭」，避免拿半份配置渲染出坏掉的控件。 */
  resetPublicSettings: () => void
  createProofSlot: (onErrorMessage?: (message: string) => void) => CaptchaProofSlot
  /**
   * 按当前启用的 provider 把任意一对 token/randstr 映射成请求字段。
   * 用于凭据不来自槽位自身的场景，例如 EmailVerifyView 会优先用重发时新取的
   * token，取不到才回退到注册页传过来的初始 token。
   */
  buildRequestPayload: (token: string, randstr?: string) => CaptchaRequestPayload
  /** 把 verifyAction 的结果转成 OAuth start / passkey begin 需要的请求体。 */
  toActionProof: (result: { token: string; randstr: string }) => ActionCaptchaRequestProof
}

/**
 * 统一四家人机验证 provider 的配置读取、凭据状态与请求字段映射。
 *
 * 抽出来的原因：provider 判断与字段映射原本在 5 个认证入口各抄了一份，
 * 每加一家 provider 都要在每处精确地多加一个布尔，漏一处就是只在特定入口
 * 复现的线上问题。
 *
 * @param onErrorMessage 默认槽位的错误文案回调，用于写回各页面自己的 errors 对象
 */
export function useCaptcha(onErrorMessage?: (message: string) => void): UseCaptchaReturn {
  const { t } = useI18n()

  const turnstileEnabled = ref<boolean>(false)
  const turnstileSiteKey = ref<string>('')
  const tencentCaptchaEnabled = ref<boolean>(false)
  const tencentCaptchaAppId = ref<string>('')
  const tencentCaptchaRegion = ref<string>('cn')
  const aliyunCaptchaEnabled = ref<boolean>(false)
  const aliyunCaptchaSceneId = ref<string>('')
  const aliyunCaptchaPrefix = ref<string>('')
  const aliyunCaptchaRegion = ref<string>('cn')
  const goCaptchaEnabled = ref<boolean>(false)
  const goCaptchaMode = ref<string>('click')

  function applyPublicSettings(settings: PublicSettings): void {
    turnstileEnabled.value = settings.turnstile_enabled
    turnstileSiteKey.value = settings.turnstile_site_key || ''
    tencentCaptchaEnabled.value = settings.tencent_captcha_enabled === true
    tencentCaptchaAppId.value = settings.tencent_captcha_app_id || ''
    tencentCaptchaRegion.value = settings.tencent_captcha_region || 'cn'
    aliyunCaptchaEnabled.value = settings.aliyun_captcha_enabled === true
    aliyunCaptchaSceneId.value = settings.aliyun_captcha_scene_id || ''
    aliyunCaptchaPrefix.value = settings.aliyun_captcha_prefix || ''
    aliyunCaptchaRegion.value = settings.aliyun_captcha_region || 'cn'
    goCaptchaEnabled.value = settings.gocaptcha_enabled === true
    goCaptchaMode.value = settings.gocaptcha_mode || 'click'
  }

  function resetPublicSettings(): void {
    turnstileEnabled.value = false
    turnstileSiteKey.value = ''
    tencentCaptchaEnabled.value = false
    tencentCaptchaAppId.value = ''
    tencentCaptchaRegion.value = 'cn'
    aliyunCaptchaEnabled.value = false
    aliyunCaptchaSceneId.value = ''
    aliyunCaptchaPrefix.value = ''
    aliyunCaptchaRegion.value = 'cn'
    goCaptchaEnabled.value = false
    goCaptchaMode.value = 'click'
  }

  const turnstileReady = computed(
    () => turnstileEnabled.value && Boolean(turnstileSiteKey.value)
  )
  const tencentCaptchaReady = computed(
    () => tencentCaptchaEnabled.value && Boolean(tencentCaptchaAppId.value)
  )
  const aliyunCaptchaReady = computed(
    () =>
      aliyunCaptchaEnabled.value &&
      Boolean(aliyunCaptchaSceneId.value) &&
      Boolean(aliyunCaptchaPrefix.value)
  )

  // 动作触发式验证码（腾讯 / 阿里云 / 自建）：提交、OAuth 启动、passkey 时弹窗验证
  const actionCaptchaEnabled = computed(
    () => tencentCaptchaReady.value || aliyunCaptchaReady.value || goCaptchaEnabled.value
  )
  const captchaEnabled = computed(() => turnstileReady.value || actionCaptchaEnabled.value)
  const inlineCaptchaPending = computed(() => turnstileEnabled.value)

  const captchaProps = computed<CaptchaChallengeProps>(() => ({
    turnstileEnabled: turnstileEnabled.value,
    turnstileSiteKey: turnstileSiteKey.value,
    tencentEnabled: tencentCaptchaEnabled.value,
    tencentAppId: tencentCaptchaAppId.value,
    tencentRegion: tencentCaptchaRegion.value,
    aliyunEnabled: aliyunCaptchaEnabled.value,
    aliyunSceneId: aliyunCaptchaSceneId.value,
    aliyunPrefix: aliyunCaptchaPrefix.value,
    aliyunRegion: aliyunCaptchaRegion.value,
    goCaptchaEnabled: goCaptchaEnabled.value,
    goCaptchaMode: goCaptchaMode.value
  }))

  // 字段始终存在但空值归一为 undefined：避免把空字符串当成凭据发给后端
  function buildRequestPayload(token: string, randstr = ''): CaptchaRequestPayload {
    return {
      turnstile_token:
        turnstileEnabled.value || aliyunCaptchaEnabled.value || goCaptchaEnabled.value
          ? token || undefined
          : undefined,
      tencent_captcha_ticket: tencentCaptchaEnabled.value ? token || undefined : undefined,
      tencent_captcha_randstr: tencentCaptchaEnabled.value ? randstr || undefined : undefined
    }
  }

  function toActionProof(result: { token: string; randstr: string }): ActionCaptchaRequestProof {
    return tencentCaptchaEnabled.value
      ? { tencent_captcha_ticket: result.token, tencent_captcha_randstr: result.randstr }
      : { turnstile_token: result.token }
  }

  function createProofSlot(slotOnErrorMessage?: (message: string) => void): CaptchaProofSlot {
    const captchaRef = ref<CaptchaChallengeInstance | null>(null)
    const token = ref<string>('')
    const randstr = ref<string>('')

    const setErrorMessage = (message: string): void => slotOnErrorMessage?.(message)

    function clearProof(): void {
      token.value = ''
      randstr.value = ''
    }

    return {
      captchaRef,
      token,
      randstr,
      onVerify(nextToken: string, nextRandstr = ''): void {
        token.value = nextToken
        randstr.value = nextRandstr
        setErrorMessage('')
      },
      onExpire(): void {
        clearProof()
        setErrorMessage(t('auth.turnstileExpired'))
      },
      onError(): void {
        clearProof()
        setErrorMessage(t('auth.turnstileFailed'))
      },
      reset(): void {
        captchaRef.value?.reset()
        clearProof()
        setErrorMessage('')
      },
      async acquireActionProof(): Promise<boolean> {
        if (!actionCaptchaEnabled.value) return true

        const proof = await captchaRef.value?.verifyAction()
        if (!proof) return false

        token.value = proof.token
        randstr.value = proof.randstr
        return true
      },
      requestPayload(): CaptchaRequestPayload {
        return buildRequestPayload(token.value, randstr.value)
      },
      // 取不到凭据时整体省略字段，而不是发空字符串：
      // 下游的 create-account 端点会把空 token 当成校验失败。
      emitPayload(): CaptchaEmitPayload {
        if (!token.value) return {}
        return tencentCaptchaEnabled.value
          ? { tencentCaptchaTicket: token.value, tencentCaptchaRandstr: randstr.value }
          : { turnstileToken: token.value }
      }
    }
  }

  const defaultSlot = createProofSlot(onErrorMessage)

  return {
    turnstileEnabled,
    turnstileSiteKey,
    tencentCaptchaEnabled,
    tencentCaptchaAppId,
    tencentCaptchaRegion,
    aliyunCaptchaEnabled,
    aliyunCaptchaSceneId,
    aliyunCaptchaPrefix,
    aliyunCaptchaRegion,
    goCaptchaEnabled,
    goCaptchaMode,
    captchaProps,
    actionCaptchaEnabled,
    captchaEnabled,
    inlineCaptchaPending,
    applyPublicSettings,
    resetPublicSettings,
    createProofSlot,
    buildRequestPayload,
    toActionProof,
    ...defaultSlot
  }
}
