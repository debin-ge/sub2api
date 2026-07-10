<template>
  <div class="space-y-4">
    <div v-if="loading" class="flex items-center justify-center py-12">
      <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
    </div>
    <div v-else-if="initError" class="card p-6 text-center">
      <p class="text-sm text-red-600 dark:text-red-400">{{ initError }}</p>
      <button class="btn btn-secondary mt-4" @click="$emit('back')">{{ t('payment.result.backToRecharge') }}</button>
    </div>
    <template v-else>
      <!-- Amount -->
      <div class="card overflow-hidden">
        <div class="bg-gradient-to-br from-[#635bff] to-[#4f46e5] px-6 py-5 text-center">
          <p class="text-sm font-medium text-indigo-200">{{ t('payment.actualPay') }}</p>
          <p class="mt-1 text-3xl font-bold text-white">{{ paymentAmountSymbol }}{{ payAmount.toFixed(2) }}</p>
        </div>
      </div>
      <!-- Stripe Payment Element -->
      <div class="card p-6">
        <StripeGooglePayExpress
          v-if="googlePayEnabled && stripeInstance && elementsInstance"
          :stripe="stripeInstance"
          :elements="elementsInstance"
          :return-url="returnUrl"
          :disabled="submitting"
          @submitting-change="submitting = $event"
          @confirmed="handleGooglePayConfirmed"
        />
        <div
          v-if="googlePayEnabled"
          data-testid="stripe-google-pay-divider"
          class="my-5 border-t border-gray-200 dark:border-dark-600"
          aria-hidden="true"
        />
        <div ref="stripeMount" class="min-h-[200px]"></div>
        <p v-if="error" class="mt-4 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
        <button class="btn btn-stripe mt-6 w-full py-3 text-base" :disabled="submitting || !ready" @click="handlePay">
          <span v-if="submitting" class="flex items-center justify-center gap-2">
            <span class="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent"></span>
            {{ t('common.processing') }}
          </span>
          <span v-else>{{ t('payment.stripePay') }}</span>
        </button>
      </div>
      <!-- Cancel order -->
      <button class="btn btn-secondary w-full" :disabled="cancelling" @click="handleCancel">
        {{ cancelling ? t('common.processing') : t('payment.qr.cancelOrder') }}
      </button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, shallowRef, onBeforeUnmount, onMounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { paymentAPI } from '@/api/payment'
import { useAppStore } from '@/stores'
import { getPaymentPopupFeatures } from '@/components/payment/providerConfig'
import { currencySymbol } from '@/components/payment/currency'
import type { Stripe, StripeElements, StripePaymentElement } from '@stripe/stripe-js'
import StripeGooglePayExpress from '@/components/payment/StripeGooglePayExpress.vue'

// Stripe payment methods that open a popup (redirect or QR code)
const POPUP_METHODS = new Set(['alipay', 'wechat_pay'])
const POPUP_CLOSE_POLL_INTERVAL_MS = 250

const props = defineProps<{
  orderId: number
  amount: number
  clientSecret: string
  orderType?: 'balance' | 'subscription'
  publishableKey: string
  googlePayEnabled: boolean
  payAmount: number
  currency?: string
}>()

const emit = defineEmits<{ confirmed: []; back: []; redirect: [orderId: number, payUrl: string] }>()

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()

const stripeMount = ref<HTMLElement | null>(null)
const loading = ref(true)
const initError = ref('')
const error = ref('')
const submitting = ref(false)
const cancelling = ref(false)
const ready = ref(false)
const selectedType = ref('')
const paymentAmountSymbol = computed(() => currencySymbol(props.currency))
const stripeInstance = shallowRef<Stripe | null>(null)
const elementsInstance = shallowRef<StripeElements | null>(null)
let paymentElement: StripePaymentElement | null = null
let pendingPopupReadyHandler: ((event: MessageEvent) => void) | null = null
let popupCloseTimer: number | null = null
const returnUrl = computed(() => (
  window.location.origin + '/payment/result?order_id=' + props.orderId + '&status=success'
))

onMounted(async () => {
  try {
    const { loadStripe } = await import('@stripe/stripe-js')
    const stripe = await loadStripe(props.publishableKey)
    if (!stripe) { initError.value = t('payment.stripeLoadFailed'); return }

    stripeInstance.value = stripe
    loading.value = false
    await nextTick()
    if (!stripeMount.value) return

    const isDark = document.documentElement.classList.contains('dark')
    const elements = stripe.elements({
      clientSecret: props.clientSecret,
      appearance: { theme: isDark ? 'night' : 'stripe', variables: { borderRadius: '8px' } },
    })
    elementsInstance.value = elements
    paymentElement = elements.create('payment', {
      layout: 'tabs',
      paymentMethodOrder: ['alipay', 'wechat_pay', 'card', 'link'],
    } as Record<string, unknown>)
    paymentElement.mount(stripeMount.value)
    paymentElement.on('ready', handlePaymentReady)
    paymentElement.on('change', handlePaymentChange)
  } catch (err: unknown) {
    initError.value = extractI18nErrorMessage(err, t, 'payment.errors', t('payment.stripeLoadFailed'))
  } finally {
    loading.value = false
  }
})

async function handlePay() {
  if (!stripeInstance.value || !elementsInstance.value || submitting.value) return

  // Alipay / WeChat Pay: open popup for redirect or QR display
  if (POPUP_METHODS.has(selectedType.value)) {
    const popupUrl = router.resolve({
      path: '/payment/stripe-popup',
      query: {
        order_id: String(props.orderId),
        method: selectedType.value,
        amount: String(props.payAmount),
      },
    }).href
    submitting.value = true
    error.value = ''
    const popup = window.open(popupUrl, 'paymentPopup', getPaymentPopupFeatures())
    if (!popup || popup.closed) {
      releasePopupLock()
      return
    }

    clearPendingPopupReadyHandler()
    pendingPopupReadyHandler = (event: MessageEvent) => {
      if (event.source !== popup || event.data?.type !== 'STRIPE_POPUP_READY') return
      clearPendingPopupReadyHandler()
      popup?.postMessage({
        type: 'STRIPE_POPUP_INIT',
        clientSecret: props.clientSecret,
        publishableKey: props.publishableKey,
      }, window.location.origin)
    }
    window.addEventListener('message', pendingPopupReadyHandler)
    popupCloseTimer = window.setInterval(() => {
      if (popup.closed) releasePopupLock()
    }, POPUP_CLOSE_POLL_INTERVAL_MS)

    emit('redirect', props.orderId, popupUrl)
    return
  }

  // Card / Link: confirm inline
  submitting.value = true
  error.value = ''
  try {
    const { error: stripeError } = await stripeInstance.value.confirmPayment({
      elements: elementsInstance.value,
      confirmParams: {
        return_url: returnUrl.value,
      },
      redirect: 'if_required',
    })
    if (stripeError) {
      error.value = stripeError.message || t('payment.result.failed')
    } else {
      emit('confirmed')
    }
  } catch (err: unknown) {
    error.value = extractI18nErrorMessage(err, t, 'payment.errors', t('payment.result.failed'))
  } finally {
    submitting.value = false
  }
}

function handleGooglePayConfirmed() {
  emit('confirmed')
}

function handlePaymentReady() {
  ready.value = true
}

function handlePaymentChange(event: { value: { type: string } }) {
  selectedType.value = event.value.type
}

function clearPendingPopupReadyHandler() {
  if (!pendingPopupReadyHandler) return
  window.removeEventListener('message', pendingPopupReadyHandler)
  pendingPopupReadyHandler = null
}

function clearPopupCloseTimer() {
  if (popupCloseTimer === null) return
  window.clearInterval(popupCloseTimer)
  popupCloseTimer = null
}

function releasePopupLock() {
  clearPendingPopupReadyHandler()
  clearPopupCloseTimer()
  submitting.value = false
}

async function handleCancel() {
  if (!props.orderId || cancelling.value) return
  cancelling.value = true
  try {
    await paymentAPI.cancelOrder(props.orderId)
    emit('back')
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    cancelling.value = false
  }
}

onBeforeUnmount(() => {
  releasePopupLock()
  if (!paymentElement) return
  paymentElement.off('ready', handlePaymentReady)
  paymentElement.off('change', handlePaymentChange)
  paymentElement.destroy()
  paymentElement = null
})
</script>
