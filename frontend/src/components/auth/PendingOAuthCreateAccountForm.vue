<template>
  <form class="space-y-3" @submit.prevent="handleSubmit">
    <input
      v-model="email"
      :data-testid="`${testIdPrefix}-create-account-email`"
      type="email"
      class="input w-full"
      :placeholder="t('auth.emailPlaceholder')"
      :disabled="isSubmitting || isSendingCode"
    />
    <input
      v-model="password"
      :data-testid="`${testIdPrefix}-create-account-password`"
      type="password"
      class="input w-full"
      :placeholder="t('auth.passwordPlaceholder')"
      :disabled="isSubmitting"
    />
    <div v-if="captchaEnabled" class="space-y-2">
      <TurnstileWidget
        ref="turnstileRef"
        v-bind="captchaProps"
        @verify="onTurnstileVerify"
        @expire="onTurnstileExpire"
        @error="onTurnstileError"
      />
    </div>
    <div v-if="emailVerifyEnabled" class="flex gap-3">
      <input
        v-model="verifyCode"
        :data-testid="`${testIdPrefix}-create-account-verify-code`"
        type="text"
        inputmode="numeric"
        maxlength="6"
        class="input min-w-0 flex-1"
        placeholder="123456"
        :disabled="isSubmitting"
      />
      <button
        :data-testid="`${testIdPrefix}-create-account-send-code`"
        type="button"
        class="btn btn-secondary shrink-0"
        :disabled="isSubmitting || isSendingCode || countdown > 0 || !email.trim() || (inlineCaptchaPending && !turnstileToken)"
        @click="handleSendCode"
      >
        {{
          isSendingCode
            ? t('auth.sendingCode')
            : countdown > 0
              ? t('auth.resendCountdown', { countdown })
              : t('auth.sendCode')
        }}
      </button>
    </div>
    <p v-if="emailVerifyEnabled && sendCodeSuccess" class="text-sm text-green-600 dark:text-green-400">
      {{ t('auth.codeSentSuccess') }}
    </p>
    <p v-else-if="emailVerifyEnabled" class="text-xs text-gray-500 dark:text-dark-400">
      {{ t('auth.verificationCodeHint') }}
    </p>
    <div v-if="invitationCodeEnabled" class="space-y-1">
      <label class="input-label">
        {{ t('auth.invitationCodeLabel') }}
        <span
          v-if="!invitationCodeMandatory"
          class="ml-1 text-xs font-normal text-gray-400 dark:text-dark-500"
        >
          ({{ t('common.optional') }})
        </span>
      </label>
      <input
        v-model="invitationCode"
        :data-testid="`${testIdPrefix}-create-account-invitation-code`"
        type="text"
        :required="invitationCodeMandatory"
        class="input w-full"
        :placeholder="t('auth.invitationCodePlaceholder')"
        :disabled="isSubmitting"
      />
      <p
        v-if="invitationSatisfiedByAffiliate"
        class="text-xs text-green-600 dark:text-green-400"
      >
        {{ t('auth.invitationSatisfiedByAffiliate') }}
      </p>
    </div>
    <button
      :data-testid="`${testIdPrefix}-create-account-submit`"
      type="button"
      class="btn btn-primary w-full"
      :disabled="isSubmitting || !email.trim() || password.length < 6 || (invitationCodeMandatory && !invitationCode.trim()) || (inlineCaptchaPending && !turnstileToken)"
      @click="handleSubmit"
    >
      {{ isSubmitting ? t('common.processing') : t('auth.createAccount') }}
    </button>
    <button
      type="button"
      class="btn btn-secondary w-full"
      :disabled="isSubmitting"
      @click="emitSwitchToBind"
    >
      {{ t('auth.alreadyHaveAccount') }}
    </button>
  </form>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import TurnstileWidget from '@/components/CaptchaChallenge.vue'
import { useCaptcha } from '@/composables/useCaptcha'
import { getPublicSettings, sendPendingOAuthVerifyCode } from '@/api/auth'
import { loadOAuthAffiliateCode } from '@/utils/oauthAffiliate'
import { useAppStore } from '@/stores'

export type PendingOAuthCreateAccountPayload = {
  email: string
  password: string
  verifyCode: string
  turnstileToken?: string
  tencentCaptchaTicket?: string
  tencentCaptchaRandstr?: string
  invitationCode?: string
}

const props = defineProps<{
  initialEmail: string
  testIdPrefix: string
  isSubmitting: boolean
  errorMessage?: string
}>()

const emit = defineEmits<{
  submit: [payload: PendingOAuthCreateAccountPayload]
  switchToBind: [email: string]
}>()

const { t } = useI18n()
const appStore = useAppStore()

const email = ref('')
const password = ref('')
const verifyCode = ref('')
const invitationCode = ref('')
const isSendingCode = ref(false)
const sendCodeError = ref('')
const sendCodeSuccess = ref(false)
const countdown = ref(0)
const invitationCodeEnabled = ref(false)
const invitationCodeRequired = ref(true)
const affiliateEnabled = ref(false)
const hasStoredAffiliateCode = ref(false)
const emailVerifyEnabled = ref(true)

// A referral code captured during the OAuth /start hop is attached to the
// create-account request by the callback view and accepted by the backend as
// satisfying the invitation gate, so it must relax the required-invitation
// constraint here — otherwise referred users are dead-ended on a disabled button.
const invitationSatisfiedByAffiliate = computed(
  () => affiliateEnabled.value && hasStoredAffiliateCode.value
)
const invitationCodeMandatory = computed(
  () => invitationCodeRequired.value && !invitationSatisfiedByAffiliate.value
)
// 人机验证：四家 provider 的配置、凭据与请求字段映射统一收敛在 useCaptcha 里
const {
  captchaRef: turnstileRef,
  token: turnstileToken,
  captchaProps,
  captchaEnabled,
  actionCaptchaEnabled,
  inlineCaptchaPending,
  applyPublicSettings: applyCaptchaSettings,
  resetPublicSettings: resetCaptchaSettings,
  onVerify: onTurnstileVerify,
  onExpire: onTurnstileExpire,
  onError: onTurnstileError,
  reset: resetTurnstile,
  acquireActionProof,
  requestPayload: captchaRequestPayload,
  emitPayload: captchaEmitPayload
} = useCaptcha(message => {
  sendCodeError.value = message
})

let countdownTimer: ReturnType<typeof setInterval> | null = null

watch(
  () => props.initialEmail,
  value => {
    email.value = value || ''
  },
  { immediate: true }
)

watch(sendCodeError, value => {
  if (value) {
    appStore.showError(value)
  }
})

watch(
  () => props.errorMessage,
  value => {
    if (value) {
      appStore.showError(value)
      if (captchaEnabled.value) {
        resetTurnstile()
      }
    }
  }
)

function clearCountdown() {
  if (countdownTimer) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
}

function startCountdown(seconds: number) {
  clearCountdown()
  countdown.value = Math.max(0, seconds)

  if (countdown.value <= 0) {
    return
  }

  countdownTimer = setInterval(() => {
    if (countdown.value <= 1) {
      countdown.value = 0
      clearCountdown()
      return
    }

    countdown.value -= 1
  }, 1000)
}

function getRequestErrorMessage(error: unknown, fallback: string): string {
  const err = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
  return err.response?.data?.detail || err.response?.data?.message || err.message || fallback
}

async function handleSendCode() {
  const trimmedEmail = email.value.trim()
  if (!trimmedEmail) {
    return
  }

  if (inlineCaptchaPending.value && !turnstileToken.value) {
    sendCodeError.value = t('auth.completeVerification')
    return
  }

  if (!(await acquireActionProof())) {
    return
  }

  isSendingCode.value = true
  sendCodeError.value = ''
  sendCodeSuccess.value = false

  try {
    const response = await sendPendingOAuthVerifyCode({
      email: trimmedEmail,
      ...captchaRequestPayload()
    })
    sendCodeSuccess.value = true
    startCountdown(response.countdown)
  } catch (error: unknown) {
    sendCodeError.value = getRequestErrorMessage(error, t('auth.sendCodeFailed'))
  } finally {
    if (captchaEnabled.value) {
      resetTurnstile()
    }
    isSendingCode.value = false
  }
}

async function handleSubmit() {
  const trimmedEmail = email.value.trim()
  if (!trimmedEmail || password.value.length < 6) {
    return
  }

  // Turnstile 票据一次性：发送验证码已消耗上一枚，reset 后要等新票据回调。
  // 缺票时不能提交——create-account 端点会校验验证码，空 token 直接被判失败。
  // 表单的隐式提交（输入框回车）绕得过按钮的 disabled，所以这里必须再挡一次。
  if (inlineCaptchaPending.value && !turnstileToken.value) {
    sendCodeError.value = t('auth.completeVerification')
    return
  }

  if (!(await acquireActionProof())) {
    return
  }

  emit('submit', {
    email: trimmedEmail,
    password: password.value,
    verifyCode: emailVerifyEnabled.value ? verifyCode.value.trim() : '',
    ...captchaEmitPayload(),
    invitationCode: invitationCode.value.trim() || undefined
  })

  if (actionCaptchaEnabled.value) {
    resetTurnstile()
  }
}

function emitSwitchToBind() {
  emit('switchToBind', email.value.trim())
}

onMounted(async () => {
  hasStoredAffiliateCode.value = Boolean(loadOAuthAffiliateCode())
  try {
    const settings = await getPublicSettings()
    invitationCodeEnabled.value = settings.invitation_code_enabled === true
    invitationCodeRequired.value =
      invitationCodeEnabled.value && settings.invitation_code_required !== false
    affiliateEnabled.value = settings.affiliate_enabled === true
    emailVerifyEnabled.value = settings.email_verify_enabled !== false
    applyCaptchaSettings(settings)
  } catch {
    invitationCodeEnabled.value = false
    invitationCodeRequired.value = false
    affiliateEnabled.value = false
    emailVerifyEnabled.value = true
    resetCaptchaSettings()
  }
})

onUnmounted(() => {
  clearCountdown()
})
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: all 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
