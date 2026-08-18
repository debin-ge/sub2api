<template>
  <AuthLayout>
    <div class="space-y-6">
      <!-- Title -->
      <div class="text-center">
        <h2 class="text-2xl font-bold text-gray-900 dark:text-white">
          {{ t('auth.createAccount') }}
        </h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
          {{ t('auth.signUpToStart', { siteName }) }}
        </p>
      </div>

      <!-- Registration Disabled Message -->
      <div
        v-if="!registrationEnabled && settingsLoaded"
        class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-800/50 dark:bg-amber-900/20"
      >
        <div class="flex items-start gap-3">
          <div class="flex-shrink-0">
            <Icon name="exclamationCircle" size="md" class="text-amber-500" />
          </div>
          <p class="text-sm text-amber-700 dark:text-amber-400">
            {{ t('auth.registrationDisabled') }}
          </p>
        </div>
      </div>

      <!-- Registration Form -->
      <!-- novalidate: browser constraint validation would block the affiliate-code-only
           path (empty invitation input) before validateForm's invitation-or-affiliate
           logic runs; validateForm is the single source of truth. -->
      <form v-else novalidate @submit.prevent="handleRegister" class="space-y-5">
        <!-- Email Input -->
        <div>
          <label for="email" class="input-label">
            {{ t('auth.emailLabel') }}
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="mail" size="md" class="text-gray-400 dark:text-dark-500" />
            </div>
            <input
              id="email"
              v-model="formData.email"
              type="email"
              required
              autofocus
              autocomplete="email"
              :disabled="registrationActionDisabled"
              class="input pl-11"
              :class="{ 'input-error': errors.email }"
              :placeholder="t('auth.emailPlaceholder')"
            />
          </div>
        </div>

        <!-- Password Input -->
        <div>
          <label for="password" class="input-label">
            {{ t('auth.passwordLabel') }}
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="lock" size="md" class="text-gray-400 dark:text-dark-500" />
            </div>
            <input
              id="password"
              v-model="formData.password"
              :type="showPassword ? 'text' : 'password'"
              required
              autocomplete="new-password"
              :disabled="registrationActionDisabled"
              class="input pl-11 pr-11"
              :class="{ 'input-error': errors.password }"
              :placeholder="t('auth.createPasswordPlaceholder')"
            />
            <button
              type="button"
              :disabled="registrationActionDisabled"
              @click="showPassword = !showPassword"
              class="absolute inset-y-0 right-0 flex items-center pr-3.5 text-gray-400 transition-colors hover:text-gray-600 dark:hover:text-dark-300"
            >
              <Icon v-if="showPassword" name="eyeOff" size="md" />
              <Icon v-else name="eye" size="md" />
            </button>
          </div>
          <p class="input-hint">
            {{ t('auth.passwordHint') }}
          </p>
        </div>

        <!-- Invitation Code Input (Required when enabled) -->
        <div v-if="invitationCodeEnabled">
          <label for="invitation_code" class="input-label">
            {{ t('auth.invitationCodeLabel') }}
            <span
              v-if="!invitationCodeRequired"
              class="ml-1 text-xs font-normal text-gray-400 dark:text-dark-500"
            >
              ({{ t('common.optional') }})
            </span>
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="key" size="md" :class="invitationValidation.valid ? 'text-green-500' : 'text-gray-400 dark:text-dark-500'" />
            </div>
            <input
              id="invitation_code"
              v-model="formData.invitation_code"
              type="text"
              :required="invitationCodeRequired"
              :disabled="registrationActionDisabled"
              class="input pl-11 pr-10"
              :class="{
                'border-green-500 focus:border-green-500 focus:ring-green-500': invitationValidation.valid,
                'border-red-500 focus:border-red-500 focus:ring-red-500': invitationValidation.invalid || errors.invitation_code
              }"
              :placeholder="t('auth.invitationCodePlaceholder')"
              @input="handleInvitationCodeInput"
            />
            <!-- Validation indicator -->
            <div v-if="invitationValidating" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <svg class="h-4 w-4 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
            </div>
            <div v-else-if="invitationValidation.valid" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <Icon name="checkCircle" size="md" class="text-green-500" />
            </div>
            <div v-else-if="invitationValidation.invalid || errors.invitation_code" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <Icon name="exclamationCircle" size="md" class="text-red-500" />
            </div>
          </div>
          <!-- Invitation code validation result -->
          <transition name="fade">
            <div v-if="invitationValidation.valid" class="mt-2 flex items-center gap-2 rounded-lg bg-green-50 px-3 py-2 dark:bg-green-900/20">
              <Icon name="checkCircle" size="sm" class="text-green-600 dark:text-green-400" />
              <span class="text-sm text-green-700 dark:text-green-400">
                {{ t('auth.invitationCodeValid') }}
              </span>
            </div>
          </transition>
        </div>

        <!-- Affiliate Referral Code Input (Optional) -->
        <div v-if="affiliateEnabled" data-testid="affiliate-invitation-field">
          <label for="aff_code" class="input-label">
            {{ t('auth.affiliateCodeLabel') }}
            <span class="ml-1 text-xs font-normal text-gray-400 dark:text-dark-500">
              ({{ t('common.optional') }})
            </span>
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon
                name="userPlus"
                size="md"
                :class="affiliateValidation.valid ? 'text-green-500' : 'text-gray-400 dark:text-dark-500'"
              />
            </div>
            <input
              id="aff_code"
              v-model="formData.aff_code"
              type="text"
              autocomplete="off"
              :disabled="registrationActionDisabled"
              class="input pl-11 pr-10"
              :class="{
                'border-green-500 focus:border-green-500 focus:ring-green-500': affiliateValidation.valid,
                'border-red-500 focus:border-red-500 focus:ring-red-500':
                  fallbackAffiliateCodeRelevant && (affiliateValidation.invalid || errors.aff_code)
              }"
              :placeholder="t('auth.affiliateCodePlaceholder')"
              @input="handleAffiliateCodeInput"
            />
            <div
              v-if="affiliateValidating"
              class="absolute inset-y-0 right-0 flex items-center pr-3.5"
            >
              <svg class="h-4 w-4 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
            </div>
            <div
              v-else-if="affiliateValidation.valid"
              class="absolute inset-y-0 right-0 flex items-center pr-3.5"
            >
              <Icon name="checkCircle" size="md" class="text-green-500" />
            </div>
            <div
              v-else-if="
                fallbackAffiliateCodeRelevant && (affiliateValidation.invalid || errors.aff_code)
              "
              class="absolute inset-y-0 right-0 flex items-center pr-3.5"
            >
              <Icon name="exclamationCircle" size="md" class="text-red-500" />
            </div>
          </div>
          <transition name="fade">
            <div
              v-if="affiliateValidation.valid"
              class="mt-2 flex items-center gap-2 rounded-lg bg-green-50 px-3 py-2 dark:bg-green-900/20"
            >
              <Icon name="checkCircle" size="sm" class="text-green-600 dark:text-green-400" />
              <span class="text-sm text-green-700 dark:text-green-400">
                {{ t('auth.affiliateCodeValid') }}
              </span>
            </div>
          </transition>
          <p
            v-if="
              fallbackAffiliateCodeRelevant && (affiliateValidation.invalid || errors.aff_code)
            "
            class="mt-2 text-sm text-red-600 dark:text-red-400"
          >
            {{ errors.aff_code || affiliateValidation.message }}
          </p>
          <p
            v-else-if="fallbackAffiliateCodeRelevant && affiliateValidation.unavailable"
            class="mt-2 text-sm text-amber-600 dark:text-amber-400"
          >
            {{ affiliateValidation.message }}
          </p>
        </div>

        <!-- Promo Code Input (Optional) -->
        <div v-if="promoCodeEnabled">
          <label for="promo_code" class="input-label">
            {{ t('auth.promoCodeLabel') }}
            <span class="ml-1 text-xs font-normal text-gray-400 dark:text-dark-500">({{ t('common.optional') }})</span>
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="gift" size="md" :class="promoValidation.valid ? 'text-green-500' : 'text-gray-400 dark:text-dark-500'" />
            </div>
            <input
              id="promo_code"
              v-model="formData.promo_code"
              type="text"
              :disabled="registrationActionDisabled"
              class="input pl-11 pr-10"
              :class="{
                'border-green-500 focus:border-green-500 focus:ring-green-500': promoValidation.valid,
                'border-red-500 focus:border-red-500 focus:ring-red-500': promoValidation.invalid
              }"
              :placeholder="t('auth.promoCodePlaceholder')"
              @input="handlePromoCodeInput"
            />
            <!-- Validation indicator -->
            <div v-if="promoValidating" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <svg class="h-4 w-4 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
            </div>
            <div v-else-if="promoValidation.valid" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <Icon name="checkCircle" size="md" class="text-green-500" />
            </div>
            <div v-else-if="promoValidation.invalid" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <Icon name="exclamationCircle" size="md" class="text-red-500" />
            </div>
          </div>
          <!-- Promo code validation result -->
          <transition name="fade">
            <div v-if="promoValidation.valid" class="mt-2 flex items-center gap-2 rounded-lg bg-green-50 px-3 py-2 dark:bg-green-900/20">
              <Icon name="gift" size="sm" class="text-green-600 dark:text-green-400" />
              <span class="text-sm text-green-700 dark:text-green-400">
                {{ t('auth.promoCodeValid', { amount: promoValidation.bonusAmount?.toFixed(2) }) }}
              </span>
            </div>
          </transition>
        </div>

        <!-- Turnstile Widget -->
        <div v-if="captchaEnabled" data-testid="registration-turnstile">
          <TurnstileWidget
            ref="turnstileRef"
            v-bind="captchaProps"
            @verify="onTurnstileVerify"
            @expire="onTurnstileExpire"
            @error="onTurnstileError"
          />
        </div>

        <LoginAgreementPrompt
          v-if="loginAgreementEnabled"
          :accepted="agreementAccepted"
          :documents="loginAgreementDocuments"
          :mode="loginAgreementMode"
          :updated-at="loginAgreementUpdatedAt"
          :visible="showAgreementModal"
          @accept="acceptLoginAgreement"
          @reject="rejectLoginAgreement"
          @open="showAgreementModal = true"
        />

        <!-- Submit Button -->
        <button
          type="submit"
          :disabled="registrationSubmitDisabled || (inlineCaptchaPending && !turnstileToken)"
          class="btn btn-primary w-full"
        >
          <svg
            v-if="isLoading"
            class="-ml-1 mr-2 h-4 w-4 animate-spin text-white"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            ></circle>
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
          <Icon v-else name="userPlus" size="md" class="mr-2" />
          {{
            isLoading
              ? t('auth.processing')
              : emailVerifyEnabled
                ? t('auth.continue')
                : t('auth.createAccount')
          }}
        </button>

      </form>

      <div v-if="showOAuthLogin" class="space-y-3 pt-1">
        <div class="flex items-center gap-3">
          <div class="h-px flex-1 bg-gray-200 dark:bg-dark-700"></div>
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('auth.oauthOrContinue') }}
          </span>
          <div class="h-px flex-1 bg-gray-200 dark:bg-dark-700"></div>
        </div>

        <EmailOAuthButtons
          :disabled="registrationSubmitDisabled"
          :aff-code="formData.aff_code"
          :github-enabled="githubOAuthEnabled"
          :google-enabled="googleOAuthEnabled"
          :show-divider="false"
          @start="handleOAuthStart"
        />

        <LinuxDoOAuthSection
          v-if="linuxdoOAuthEnabled"
          :disabled="registrationSubmitDisabled"
          :aff-code="formData.aff_code"
          :show-divider="false"
          @start="handleOAuthStart"
        />
        <WechatOAuthSection
          v-if="wechatOAuthEnabled"
          :disabled="registrationSubmitDisabled"
          :aff-code="formData.aff_code"
          :show-divider="false"
          @start="handleOAuthStart"
        />
        <OidcOAuthSection
          v-if="oidcOAuthEnabled"
          :disabled="registrationSubmitDisabled"
          :provider-name="oidcOAuthProviderName"
          :aff-code="formData.aff_code"
          :show-divider="false"
          @start="handleOAuthStart"
        />
      </div>
    </div>

    <!-- Footer -->
    <template #footer>
      <p class="text-gray-500 dark:text-dark-400">
        {{ t('auth.alreadyHaveAccount') }}
        <router-link
          to="/login"
          @click="cancelAffiliateReferral"
          class="font-medium text-primary-600 transition-colors hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300"
        >
          {{ t('auth.signIn') }}
        </router-link>
      </p>
    </template>
  </AuthLayout>
</template>

<script setup lang="ts">
import { computed, ref, reactive, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import LinuxDoOAuthSection from '@/components/auth/LinuxDoOAuthSection.vue'
import OidcOAuthSection from '@/components/auth/OidcOAuthSection.vue'
import WechatOAuthSection from '@/components/auth/WechatOAuthSection.vue'
import EmailOAuthButtons from '@/components/auth/EmailOAuthButtons.vue'
import LoginAgreementPrompt from '@/components/auth/LoginAgreementPrompt.vue'
import Icon from '@/components/icons/Icon.vue'
import TurnstileWidget from '@/components/CaptchaChallenge.vue'
import { useCaptcha } from '@/composables/useCaptcha'
import { useAuthStore, useAppStore } from '@/stores'
import {
  buildOAuthLoginStartURL,
  isWeChatWebOAuthEnabled,
  startOAuthLogin,
  type OAuthLoginStart,
  validateAffiliateCode,
  validatePromoCode,
  validateInvitationCode
} from '@/api/auth'
import { buildAuthErrorMessage } from '@/utils/authError'
import { extractApiErrorCode, extractI18nErrorMessage } from '@/utils/apiError'
import {
  formatRegistrationEmailSuffixWhitelistForMessage,
  isRegistrationEmailSuffixAllowed,
  isRegistrationEmailSuffixBlocked,
  normalizeRegistrationEmailSuffixBlacklist,
  normalizeRegistrationEmailSuffixWhitelist
} from '@/utils/registrationEmailPolicy'
import {
  clearAffiliateReferralCode,
  loadAffiliateReferralCode,
  resolveAffiliateReferralCode,
  storeAffiliateReferralCode
} from '@/utils/oauthAffiliate'
import type { LoginAgreementDocument } from '@/types'

const { t, locale } = useI18n()
const LOGIN_AGREEMENT_STORAGE_KEY = 'sub2api_login_agreement_consent'

// ==================== Router & Stores ====================

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const appStore = useAppStore()

// ==================== State ====================

const isLoading = ref<boolean>(false)
const settingsLoaded = ref<boolean>(false)
const errorMessage = ref<string>('')
const showPassword = ref<boolean>(false)

// Public settings
const registrationEnabled = ref<boolean>(true)
const emailVerifyEnabled = ref<boolean>(false)
const promoCodeEnabled = ref<boolean>(true)
const invitationCodeEnabled = ref<boolean>(false)
const invitationCodeRequired = ref<boolean>(true)
const affiliateEnabled = ref<boolean>(false)
const siteName = computed(() => appStore.siteName)
const linuxdoOAuthEnabled = ref<boolean>(false)
const wechatOAuthEnabled = ref<boolean>(false)
const oidcOAuthEnabled = ref<boolean>(false)
const oidcOAuthProviderName = ref<string>('OIDC')
const githubOAuthEnabled = ref<boolean>(false)
const googleOAuthEnabled = ref<boolean>(false)
const registrationEmailSuffixBlacklist = ref<string[]>([])
const registrationEmailSuffixWhitelist = ref<string[]>([])
// 域名限量注册开关：开启时非白名单域名可注册 1 个账户（由后端判定），前端不做白名单预检。
const emailDomainQuotaEnabled = ref<boolean>(false)
const loginAgreementEnabled = ref<boolean>(false)
const loginAgreementMode = ref<'modal' | 'checkbox' | string>('modal')
const loginAgreementUpdatedAt = ref<string>('')
const loginAgreementRevision = ref<string>('')
const loginAgreementDocuments = ref<LoginAgreementDocument[]>([])
const agreementAccepted = ref<boolean>(false)
const showAgreementModal = ref<boolean>(false)


// Promo code validation
const promoValidating = ref<boolean>(false)
const promoValidation = reactive({
  valid: false,
  invalid: false,
  bonusAmount: null as number | null,
  message: ''
})
let promoValidateTimeout: ReturnType<typeof setTimeout> | null = null

// Invitation code validation
const invitationValidating = ref<boolean>(false)
const invitationValidation = reactive({
  valid: false,
  invalid: false,
  affiliate: false,
  message: ''
})
let invitationValidateTimeout: ReturnType<typeof setTimeout> | null = null
let invitationValidationSequence = 0

// Affiliate code validation
const affiliateValidating = ref<boolean>(false)
const affiliateValidation = reactive({
  valid: false,
  invalid: false,
  // Set when validation could not complete (rate limit / network), as opposed to a
  // code that was checked and rejected. This state must not block submission — the
  // backend re-validates and is the final arbiter.
  unavailable: false,
  message: ''
})
let affiliateValidateTimeout: ReturnType<typeof setTimeout> | null = null
let affiliateValidationSequence = 0

const formData = reactive({
  email: '',
  password: '',
  promo_code: '',
  invitation_code: '',
  aff_code: ''
})

const errors = reactive({
  email: '',
  password: '',
  turnstile: '',
  invitation_code: '',
  aff_code: ''
})

// 人机验证：四家 provider 的配置、凭据与请求字段映射统一收敛在 useCaptcha 里
const {
  captchaRef: turnstileRef,
  token: turnstileToken,
  captchaProps,
  captchaEnabled,
  actionCaptchaEnabled,
  inlineCaptchaPending,
  applyPublicSettings: applyCaptchaSettings,
  onVerify: onTurnstileVerify,
  onExpire: onTurnstileExpire,
  onError: onTurnstileError,
  reset: resetCaptchaProof,
  acquireActionProof,
  requestPayload: captchaRequestPayload,
  toActionProof
} = useCaptcha((message) => {
  errors.turnstile = message
})

const validationToastMessage = computed(() =>
  errors.email ||
  errors.password ||
  (invitationValidation.invalid ? invitationValidation.message : '') ||
  errors.invitation_code ||
  errors.aff_code ||
  (fallbackAffiliateCodeRelevant.value && affiliateValidation.invalid
    ? affiliateValidation.message
    : '') ||
  (promoValidation.invalid ? promoValidation.message : '') ||
  errors.turnstile ||
  ''
)

const showOAuthLogin = computed(
  () =>
    linuxdoOAuthEnabled.value ||
    wechatOAuthEnabled.value ||
    oidcOAuthEnabled.value ||
    githubOAuthEnabled.value ||
    googleOAuthEnabled.value
)

const agreementGateActive = computed(
  () => loginAgreementEnabled.value && !agreementAccepted.value
)

const registrationActionDisabled = computed(
  () => isLoading.value || !settingsLoaded.value || agreementGateActive.value
)

const fallbackAffiliateCodeRelevant = computed(
  () => !(invitationCodeEnabled.value && invitationValidation.valid && invitationValidation.affiliate)
)

const affiliateValidationBlocksSubmission = computed(() => {
  if (!affiliateEnabled.value || !fallbackAffiliateCodeRelevant.value) {
    return false
  }
  // A code that could not be validated (rate limit / network) must not lock the
  // user out — let the backend re-validate on submit.
  if (affiliateValidation.unavailable) {
    return false
  }
  const code = formData.aff_code.trim()
  return Boolean(code) && (affiliateValidating.value || !affiliateValidation.valid)
})

const registrationSubmitDisabled = computed(
  () => registrationActionDisabled.value || affiliateValidationBlocksSubmission.value
)

watch(validationToastMessage, (value, previousValue) => {
  if (value && value !== previousValue) {
    appStore.showError(value)
  }
})

function syncAffiliateReferralCode(): string {
  const code = resolveAffiliateReferralCode(route.query.aff, route.query.aff_code)
  if (code) {
    formData.aff_code = code
  }
  return code
}

// ==================== Lifecycle ====================

onMounted(async () => {
  syncAffiliateReferralCode()

  try {
    const settings = await appStore.fetchPublicSettings()
    if (!settings) {
      throw new Error('Public settings are unavailable')
    }
    registrationEnabled.value = settings.registration_enabled
    emailVerifyEnabled.value = settings.email_verify_enabled
    promoCodeEnabled.value = settings.promo_code_enabled
    invitationCodeEnabled.value = settings.invitation_code_enabled
    invitationCodeRequired.value =
      invitationCodeEnabled.value && settings.invitation_code_required !== false
    affiliateEnabled.value = settings.affiliate_enabled
    applyCaptchaSettings(settings)
    linuxdoOAuthEnabled.value = settings.linuxdo_oauth_enabled
    wechatOAuthEnabled.value = isWeChatWebOAuthEnabled(settings)
    oidcOAuthEnabled.value = settings.oidc_oauth_enabled
    oidcOAuthProviderName.value = settings.oidc_oauth_provider_name || 'OIDC'
    githubOAuthEnabled.value = settings.github_oauth_enabled
    googleOAuthEnabled.value = settings.google_oauth_enabled
    registrationEmailSuffixBlacklist.value = normalizeRegistrationEmailSuffixBlacklist(
      settings.registration_email_suffix_blacklist || []
    )
    registrationEmailSuffixWhitelist.value = normalizeRegistrationEmailSuffixWhitelist(
      settings.registration_email_suffix_whitelist || []
    )
    emailDomainQuotaEnabled.value = settings.registration_email_domain_quota_enabled === true
    applyLoginAgreementSettings(settings)

    // Read promo code from URL parameter only if promo code is enabled
    if (promoCodeEnabled.value) {
      const promoParam = route.query.promo as string
      if (promoParam) {
        formData.promo_code = promoParam
        // Validate the promo code from URL
        await validatePromoCodeDebounced(promoParam)
      }
    }
    const affiliateCode = syncAffiliateReferralCode()
    if (affiliateEnabled.value && affiliateCode) {
      await validateAffiliateCodeDebounced(affiliateCode)
    }
  } catch (error) {
    console.error('Failed to load public settings:', error)
    loginAgreementEnabled.value = false
    agreementAccepted.value = true
    // Fail open on the invitation gate: the finally block still marks the form
    // submittable, so leaving invitationCodeRequired=true here would dead-end
    // registration with no invitation field rendered. Mirrors the pending-OAuth form.
    invitationCodeEnabled.value = false
    invitationCodeRequired.value = false
  } finally {
    settingsLoaded.value = true
  }
})

watch(
  () => [route.query.aff, route.query.aff_code],
  () => {
    const code = syncAffiliateReferralCode()
    if (settingsLoaded.value && affiliateEnabled.value && code) {
      handleAffiliateCodeInput()
    }
  }
)

onUnmounted(() => {
  if (promoValidateTimeout) {
    clearTimeout(promoValidateTimeout)
  }
  if (invitationValidateTimeout) {
    clearTimeout(invitationValidateTimeout)
  }
  if (affiliateValidateTimeout) {
    clearTimeout(affiliateValidateTimeout)
  }
  affiliateValidationSequence += 1
})

// ==================== Login Agreement ====================

function applyLoginAgreementSettings(settings: {
  login_agreement_enabled?: boolean
  login_agreement_mode?: string
  login_agreement_updated_at?: string
  login_agreement_revision?: string
  login_agreement_documents?: LoginAgreementDocument[]
}): void {
  const documents = Array.isArray(settings.login_agreement_documents)
    ? settings.login_agreement_documents.filter((doc) => doc.title?.trim())
    : []
  loginAgreementDocuments.value = documents
  loginAgreementEnabled.value = settings.login_agreement_enabled === true && documents.length > 0
  loginAgreementMode.value = settings.login_agreement_mode === 'checkbox' ? 'checkbox' : 'modal'
  loginAgreementUpdatedAt.value = settings.login_agreement_updated_at || ''
  loginAgreementRevision.value =
    settings.login_agreement_revision ||
    `${loginAgreementUpdatedAt.value}:${documents.map((doc) => `${doc.id}:${doc.title}`).join('|')}`

  agreementAccepted.value = !loginAgreementEnabled.value || hasAcceptedLoginAgreement(loginAgreementRevision.value)
  showAgreementModal.value =
    loginAgreementEnabled.value && !agreementAccepted.value && loginAgreementMode.value !== 'checkbox'
}

function hasAcceptedLoginAgreement(revision: string): boolean {
  if (!revision) {
    return false
  }
  try {
    const raw = localStorage.getItem(LOGIN_AGREEMENT_STORAGE_KEY)
    if (!raw) {
      return false
    }
    const parsed = JSON.parse(raw) as { revision?: string }
    return parsed.revision === revision
  } catch {
    return false
  }
}

function acceptLoginAgreement(): void {
  if (loginAgreementRevision.value) {
    localStorage.setItem(
      LOGIN_AGREEMENT_STORAGE_KEY,
      JSON.stringify({
        revision: loginAgreementRevision.value,
        accepted_at: new Date().toISOString()
      })
    )
  }
  agreementAccepted.value = true
  showAgreementModal.value = false
}

function rejectLoginAgreement(): void {
  localStorage.removeItem(LOGIN_AGREEMENT_STORAGE_KEY)
  agreementAccepted.value = false
  showAgreementModal.value = false
  appStore.showWarning(t('legal.loginAgreementPrompt.registerRejectedWarning'))
}

// ==================== Promo Code Validation ====================

function handlePromoCodeInput(): void {
  const code = formData.promo_code.trim()

  // Clear previous validation
  promoValidation.valid = false
  promoValidation.invalid = false
  promoValidation.bonusAmount = null
  promoValidation.message = ''

  if (!code) {
    promoValidating.value = false
    return
  }

  // Debounce validation
  if (promoValidateTimeout) {
    clearTimeout(promoValidateTimeout)
  }

  promoValidateTimeout = setTimeout(() => {
    validatePromoCodeDebounced(code)
  }, 500)
}

async function validatePromoCodeDebounced(code: string): Promise<void> {
  if (!code.trim()) return

  promoValidating.value = true

  try {
    const result = await validatePromoCode(code)

    if (result.valid) {
      promoValidation.valid = true
      promoValidation.invalid = false
      promoValidation.bonusAmount = result.bonus_amount || 0
      promoValidation.message = ''
    } else {
      promoValidation.valid = false
      promoValidation.invalid = true
      promoValidation.bonusAmount = null
      // 根据错误码显示对应的翻译
      promoValidation.message = getPromoErrorMessage(result.error_code)
    }
  } catch (error) {
    console.error('Failed to validate promo code:', error)
    promoValidation.valid = false
    promoValidation.invalid = true
    promoValidation.message = t('auth.promoCodeInvalid')
  } finally {
    promoValidating.value = false
  }
}

function getPromoErrorMessage(errorCode?: string): string {
  switch (errorCode) {
    case 'PROMO_CODE_NOT_FOUND':
      return t('auth.promoCodeNotFound')
    case 'PROMO_CODE_EXPIRED':
      return t('auth.promoCodeExpired')
    case 'PROMO_CODE_DISABLED':
      return t('auth.promoCodeDisabled')
    case 'PROMO_CODE_MAX_USED':
      return t('auth.promoCodeMaxUsed')
    case 'PROMO_CODE_ALREADY_USED':
      return t('auth.promoCodeAlreadyUsed')
    default:
      return t('auth.promoCodeInvalid')
  }
}

// ==================== Invitation Code Validation ====================

function handleInvitationCodeInput(): void {
  const code = formData.invitation_code.trim()

  // Clear previous validation
  invitationValidationSequence += 1
  invitationValidating.value = false
  invitationValidation.valid = false
  invitationValidation.invalid = false
  invitationValidation.affiliate = false
  invitationValidation.message = ''
  errors.invitation_code = ''

  if (!code) {
    return
  }

  // Debounce validation
  if (invitationValidateTimeout) {
    clearTimeout(invitationValidateTimeout)
  }

  invitationValidateTimeout = setTimeout(() => {
    validateInvitationCodeDebounced(code)
  }, 500)
}

async function validateInvitationCodeDebounced(code: string): Promise<void> {
  const sequence = ++invitationValidationSequence
  invitationValidating.value = true

  try {
    const result = await validateInvitationCode(code)
    if (sequence !== invitationValidationSequence || code !== formData.invitation_code.trim()) {
      return
    }

    if (result.valid) {
      invitationValidation.valid = true
      invitationValidation.invalid = false
      invitationValidation.message = ''
      invitationValidation.affiliate = false
      if (affiliateEnabled.value) {
        try {
          const affiliateResult = await validateAffiliateCode(code)
          if (
            sequence === invitationValidationSequence &&
            code === formData.invitation_code.trim()
          ) {
            invitationValidation.affiliate = affiliateResult.valid
          }
        } catch {
          if (sequence === invitationValidationSequence) {
            invitationValidation.affiliate = false
          }
        }
      }
    } else {
      invitationValidation.valid = false
      invitationValidation.invalid = true
      invitationValidation.affiliate = false
      invitationValidation.message = getInvitationErrorMessage(result.error_code)
    }
  } catch {
    if (sequence !== invitationValidationSequence) {
      return
    }
    invitationValidation.valid = false
    invitationValidation.invalid = true
    invitationValidation.affiliate = false
    invitationValidation.message = t('auth.invitationCodeInvalid')
  } finally {
    if (sequence === invitationValidationSequence) {
      invitationValidating.value = false
    }
  }
}

function getInvitationErrorMessage(errorCode?: string): string {
  switch (errorCode) {
    case 'INVITATION_CODE_NOT_FOUND':
      return t('auth.invitationCodeInvalid')
    case 'INVITATION_CODE_INVALID':
      return t('auth.invitationCodeInvalid')
    case 'INVITATION_CODE_USED':
      return t('auth.invitationCodeInvalid')
    case 'INVITATION_CODE_DISABLED':
      return t('auth.invitationCodeInvalid')
    default:
      return t('auth.invitationCodeInvalid')
  }
}

// ==================== Affiliate Code Validation ====================

function resetAffiliateValidation(): void {
  affiliateValidation.valid = false
  affiliateValidation.invalid = false
  affiliateValidation.unavailable = false
  affiliateValidation.message = ''
  errors.aff_code = ''
}

function handleAffiliateCodeInput(): void {
  const code = formData.aff_code.trim()

  affiliateValidationSequence += 1
  resetAffiliateValidation()
  if (affiliateValidateTimeout) {
    clearTimeout(affiliateValidateTimeout)
    affiliateValidateTimeout = null
  }

  if (!code) {
    affiliateValidating.value = false
    clearAffiliateReferralCode()
    return
  }

  // Persist only once the code validates (see validateAffiliateCodeDebounced) so
  // arbitrary keystrokes are not written to localStorage for 30 days and later
  // carried into OAuth start URLs.
  affiliateValidateTimeout = setTimeout(() => {
    void validateAffiliateCodeDebounced(code)
  }, 500)
}

async function validateAffiliateCodeDebounced(code: string): Promise<boolean> {
  const normalizedCode = code.trim()
  if (!normalizedCode || !affiliateEnabled.value) {
    return false
  }

  const sequence = ++affiliateValidationSequence
  affiliateValidating.value = true
  resetAffiliateValidation()

  try {
    const result = await validateAffiliateCode(normalizedCode)
    if (sequence !== affiliateValidationSequence || normalizedCode !== formData.aff_code.trim()) {
      return false
    }
    if (result.valid) {
      affiliateValidation.valid = true
      storeAffiliateReferralCode(normalizedCode)
      return true
    }
    affiliateValidation.invalid = true
    affiliateValidation.message = getAffiliateErrorMessage(result.error_code)
    // Drop a code that failed validation so it is not carried into a later OAuth
    // start URL (login page) or reused after the 30-day TTL.
    clearAffiliateReferralCode()
    return false
  } catch (error) {
    console.error('Failed to validate affiliate code:', error)
    if (sequence !== affiliateValidationSequence) {
      return false
    }
    // Validation could not complete (e.g. the endpoint's rate limit returned 429).
    // Treat it as "unavailable", not "invalid": do not block submission or clear the
    // stored code — the backend re-validates on submit and is the final arbiter.
    affiliateValidation.unavailable = true
    affiliateValidation.message = t('auth.affiliateCodeServiceUnavailable')
    return false
  } finally {
    if (sequence === affiliateValidationSequence) {
      affiliateValidating.value = false
    }
  }
}

function getAffiliateErrorMessage(errorCode?: string): string {
  switch (errorCode) {
    case 'AFFILIATE_DISABLED':
      return t('auth.affiliateCodeDisabled')
    case 'SERVICE_UNAVAILABLE':
      return t('auth.affiliateCodeServiceUnavailable')
    default:
      return t('auth.affiliateCodeInvalid')
  }
}

function cancelAffiliateReferral(event?: MouseEvent): void {
  // Vue Router does not navigate on modified/non-primary clicks (cmd/ctrl/shift/alt
  // or middle-click open-in-new-tab), but it still invokes this listener. Skip the
  // reset in that case so opening the login page in a new tab does not silently wipe
  // the referral in the tab the user is still registering in.
  if (event && (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0)) {
    return
  }
  formData.aff_code = ''
  affiliateValidationSequence += 1
  resetAffiliateValidation()
  clearAffiliateReferralCode()
}

async function handleOAuthStart(request: OAuthLoginStart): Promise<void> {
  if (registrationActionDisabled.value) return

  if (!actionCaptchaEnabled.value) {
    window.location.href = buildOAuthLoginStartURL(request)
    return
  }

  isLoading.value = true
  try {
    const proof = await turnstileRef.value?.verifyAction()
    if (!proof) return

    const result = await startOAuthLogin(request, toActionProof(proof))
    window.location.href = result.authorize_url
  } catch (error: unknown) {
    errorMessage.value = extractI18nErrorMessage(
      error,
      t,
      'auth.errors',
      t('auth.turnstileFailed')
    )
    appStore.showError(errorMessage.value)
  } finally {
    resetCaptchaProof()
    isLoading.value = false
  }
}

// ==================== Validation ====================

function validateEmail(email: string): boolean {
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
  return emailRegex.test(email)
}

function buildEmailSuffixNotAllowedMessage(): string {
  const normalizedWhitelist = normalizeRegistrationEmailSuffixWhitelist(
    registrationEmailSuffixWhitelist.value
  )
  if (normalizedWhitelist.length === 0) {
    return t('auth.emailSuffixNotAllowed')
  }
  const separator = String(locale.value || '').toLowerCase().startsWith('zh') ? '、' : ', '
  return t('auth.emailSuffixNotAllowedWithAllowed', {
    suffixes: formatRegistrationEmailSuffixWhitelistForMessage(normalizedWhitelist, {
      separator,
      more: (count) => t('auth.emailSuffixAllowedMore', { count })
    })
  })
}

function validateForm(): boolean {
  // Reset errors
  errors.email = ''
  errors.password = ''
  errors.turnstile = ''
  errors.invitation_code = ''
  errors.aff_code = ''

  let isValid = true

  if (agreementGateActive.value) {
    appStore.showWarning(t('legal.loginAgreementPrompt.registerRequiredWarning'))
    if (loginAgreementMode.value !== 'checkbox') {
      showAgreementModal.value = true
    }
    return false
  }

  // Email validation
  if (!formData.email.trim()) {
    errors.email = t('auth.emailRequired')
    isValid = false
  } else if (!validateEmail(formData.email)) {
    errors.email = t('auth.invalidEmail')
    isValid = false
  } else if (
    isRegistrationEmailSuffixBlocked(formData.email, registrationEmailSuffixBlacklist.value)
  ) {
    errors.email = t('auth.emailSuffixNotAllowed')
    isValid = false
  } else if (
    !emailDomainQuotaEnabled.value &&
    !isRegistrationEmailSuffixAllowed(formData.email, registrationEmailSuffixWhitelist.value)
  ) {
    // 域名限量注册关闭时保持严格白名单预检；开启时交给后端按域名额度判定
    errors.email = buildEmailSuffixNotAllowedMessage()
    isValid = false
  }

  // Password validation
  if (!formData.password) {
    errors.password = t('auth.passwordRequired')
    isValid = false
  } else if (formData.password.length < 6) {
    errors.password = t('auth.passwordMinLength')
    isValid = false
  }

  // Invitation code validation (required when enabled)
  if (invitationCodeRequired.value) {
    const hasInvitationCode = Boolean(formData.invitation_code.trim())
    const hasAffiliateCode = affiliateEnabled.value && Boolean(formData.aff_code.trim())
    if (!hasInvitationCode && !hasAffiliateCode) {
      errors.invitation_code = t('auth.invitationCodeRequired')
      isValid = false
    }
  }

  // Turnstile validation
  if (inlineCaptchaPending.value && !turnstileToken.value) {
    errors.turnstile = t('auth.completeVerification')
    isValid = false
  }

  return isValid
}

// ==================== Form Handlers ====================

async function handleRegister(): Promise<void> {
  // Clear previous error
  errorMessage.value = ''

  // Validate form
  if (!validateForm()) {
    return
  }

  // Check promo code validation status
  if (formData.promo_code.trim()) {
    // If promo code is being validated, wait
    if (promoValidating.value) {
      errorMessage.value = t('auth.promoCodeValidating')
      return
    }
    // If promo code is invalid, block submission
    if (promoValidation.invalid) {
      errorMessage.value = t('auth.promoCodeInvalidCannotRegister')
      return
    }
  }

  // Validate the explicit invitation first because an affiliate code supplied
  // there takes precedence over the fallback aff_code.
  if (invitationCodeEnabled.value && formData.invitation_code.trim()) {
    // If still validating, wait
    if (invitationValidating.value) {
      errorMessage.value = t('auth.invitationCodeValidating')
      errors.invitation_code = errorMessage.value
      return
    }
    // If invitation code is invalid, block submission
    if (invitationValidation.invalid) {
      errorMessage.value = t('auth.invitationCodeInvalidCannotRegister')
      errors.invitation_code = errorMessage.value
      return
    }
    // If invitation code is required but not validated yet
    if (formData.invitation_code.trim() && !invitationValidation.valid) {
      errorMessage.value = t('auth.invitationCodeValidating')
      // Trigger validation
      await validateInvitationCodeDebounced(formData.invitation_code.trim())
      if (!invitationValidation.valid) {
        errorMessage.value = t('auth.invitationCodeInvalidCannotRegister')
        errors.invitation_code = errorMessage.value
        return
      }
    }
  }

  const affiliateCode = formData.aff_code.trim()
  if (
    affiliateEnabled.value &&
    affiliateCode &&
    fallbackAffiliateCodeRelevant.value
  ) {
    if (affiliateValidating.value) {
      errors.aff_code = t('auth.affiliateCodeValidating')
      return
    }
    if (!affiliateValidation.valid) {
      const valid = await validateAffiliateCodeDebounced(affiliateCode)
      if (!valid && !affiliateValidation.unavailable) {
        errors.aff_code =
          affiliateValidation.message || t('auth.affiliateCodeInvalidCannotRegister')
        return
      }
    }
  }

  if (
    invitationCodeRequired.value &&
    !formData.invitation_code.trim() &&
    (!affiliateCode || (!affiliateValidation.valid && !affiliateValidation.unavailable))
  ) {
    errors.invitation_code = t('auth.invitationCodeRequired')
    return
  }

  if (!(await acquireActionProof())) {
    return
  }

  isLoading.value = true

  try {
    const affCode = formData.aff_code.trim() || loadAffiliateReferralCode()
    if (affCode) {
      formData.aff_code = affCode
    }

    // If email verification is enabled, redirect to verification page
    if (emailVerifyEnabled.value) {
      // Store registration data in sessionStorage
      sessionStorage.setItem(
        'register_data',
        JSON.stringify({
          email: formData.email,
          password: formData.password,
          ...captchaRequestPayload(),
          promo_code: formData.promo_code || undefined,
          invitation_code: formData.invitation_code || undefined,
          ...(affCode ? { aff_code: affCode } : {})
        })
      )

      // Navigate to email verification page
      await router.push('/email-verify')
      return
    }

    // Otherwise, directly register
    await authStore.register({
      email: formData.email,
      password: formData.password,
      ...captchaRequestPayload(),
      promo_code: formData.promo_code || undefined,
      invitation_code: formData.invitation_code || undefined,
      ...(affCode ? { aff_code: affCode } : {})
    })
    clearAffiliateReferralCode()

    // Show success toast
    appStore.showSuccess(t('auth.accountCreatedSuccess', { siteName: siteName.value }))

    // Redirect to dashboard
    await router.push('/dashboard')
  } catch (error: unknown) {
    // Handle registration error
    errorMessage.value = buildRegistrationErrorMessage(error, t('auth.registrationFailed'))

    // Also show error toast
    appStore.showError(errorMessage.value)
  } finally {
    if (captchaEnabled.value) {
      resetCaptchaProof()
    }
    isLoading.value = false
  }
}

function buildRegistrationErrorMessage(error: unknown, fallback: string): string {
  if (extractApiErrorCode(error) === 'EMAIL_DOMAIN_REGISTRATION_LIMIT') {
    return t('auth.emailDomainRegistrationLimit')
  }
  return buildAuthErrorMessage(error, { fallback })
}
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
