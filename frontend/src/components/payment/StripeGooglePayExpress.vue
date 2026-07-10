<template>
  <div
    data-testid="stripe-google-pay-state"
    class="space-y-3"
    :aria-busy="confirming"
    aria-live="polite"
  >
    <div
      ref="mountTarget"
      v-show="availability === 'available'"
      data-testid="stripe-google-pay-mount"
      :class="{ 'pointer-events-none opacity-60': disabled || confirming }"
    />
    <template v-if="availability !== 'available'">
      <button
        data-testid="stripe-google-pay-placeholder"
        type="button"
        disabled
        :aria-label="t('payment.googlePayUnavailableLabel')"
        class="w-full rounded-md bg-black px-4 py-3 font-medium text-white opacity-50"
      >
        Google Pay
      </button>
      <p class="text-sm text-gray-500 dark:text-gray-400">
        {{ availability === 'checking' ? t('payment.googlePayChecking') : t('payment.googlePayUnavailable') }}
      </p>
    </template>
    <p
      v-if="errorMessage"
      data-testid="stripe-google-pay-error"
      class="text-sm text-red-600 dark:text-red-400"
    >
      {{ errorMessage }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { extractI18nErrorMessage } from '@/utils/apiError'
import type {
  Stripe,
  StripeElements,
  StripeExpressCheckoutElement,
  StripeExpressCheckoutElementAvailablePaymentMethodsChangeEvent,
  StripeExpressCheckoutElementConfirmEvent,
  StripeExpressCheckoutElementOptions,
  StripeExpressCheckoutElementReadyEvent,
} from '@stripe/stripe-js'

const props = withDefaults(defineProps<{
  stripe: Stripe
  elements: StripeElements
  returnUrl: string
  disabled?: boolean
}>(), {
  disabled: false,
})

const emit = defineEmits<{
  availabilityChange: [available: boolean]
  submittingChange: [submitting: boolean]
  confirmed: []
}>()

const { t } = useI18n()
const mountTarget = ref<HTMLElement | null>(null)
type GooglePayAvailability = 'checking' | 'available' | 'unavailable' | 'error'
const availability = ref<GooglePayAvailability>('checking')
const confirming = ref(false)
const errorMessage = ref('')
let expressCheckoutElement: StripeExpressCheckoutElement | null = null

const options = {
  paymentMethods: {
    googlePay: 'auto',
    applePay: 'never',
    link: 'never',
    amazonPay: 'never',
    paypal: 'never',
    klarna: 'never',
  },
} satisfies StripeExpressCheckoutElementOptions

function setAvailability(next: GooglePayAvailability) {
  const wasAvailable = availability.value === 'available'
  availability.value = next
  const isAvailable = next === 'available'
  if (wasAvailable !== isAvailable) {
    emit('availabilityChange', isAvailable)
  }
}

function setSubmitting(next: boolean) {
  confirming.value = next
  emit('submittingChange', next)
}

function handleReady(event: StripeExpressCheckoutElementReadyEvent) {
  setAvailability(event.availablePaymentMethods?.googlePay ? 'available' : 'unavailable')
}

function handleAvailabilityChange(
  event: StripeExpressCheckoutElementAvailablePaymentMethodsChangeEvent,
) {
  setAvailability(event.paymentMethods?.googlePay?.available ? 'available' : 'unavailable')
}

function handleCancel() {
  // Only handleConfirm acquires this component's submitting lock.
}

function handleLoadError(event: {
  error: { type: string; code?: string }
}) {
  console.warn('[StripeGooglePayExpress] load failed', {
    type: event.error.type,
    code: event.error.code,
  })
  setAvailability('error')
}

async function handleConfirm(event: StripeExpressCheckoutElementConfirmEvent) {
  if (props.disabled || confirming.value) {
    event.paymentFailed({ reason: 'fail', message: t('common.processing') })
    return
  }

  setSubmitting(true)
  errorMessage.value = ''
  try {
    const { error } = await props.stripe.confirmPayment({
      elements: props.elements,
      confirmParams: { return_url: props.returnUrl },
      redirect: 'if_required',
    })
    if (error) {
      errorMessage.value = error.message || t('payment.result.failed')
      event.paymentFailed({ reason: 'fail', message: errorMessage.value })
      return
    }
    emit('confirmed')
  } catch (error: unknown) {
    errorMessage.value = extractI18nErrorMessage(
      error,
      t,
      'payment.errors',
      t('payment.result.failed'),
    )
    event.paymentFailed({ reason: 'fail', message: errorMessage.value })
  } finally {
    setSubmitting(false)
  }
}

onMounted(() => {
  if (!mountTarget.value) return
  expressCheckoutElement = props.elements.create('expressCheckout', options)
  expressCheckoutElement.on('ready', handleReady)
  expressCheckoutElement.on('availablepaymentmethodschange', handleAvailabilityChange)
  expressCheckoutElement.on('confirm', handleConfirm)
  expressCheckoutElement.on('cancel', handleCancel)
  expressCheckoutElement.on('loaderror', handleLoadError)
  expressCheckoutElement.mount(mountTarget.value)
})

onBeforeUnmount(() => {
  if (!expressCheckoutElement) return
  expressCheckoutElement.off('ready', handleReady)
  expressCheckoutElement.off('availablepaymentmethodschange', handleAvailabilityChange)
  expressCheckoutElement.off('confirm', handleConfirm)
  expressCheckoutElement.off('cancel', handleCancel)
  expressCheckoutElement.off('loaderror', handleLoadError)
  expressCheckoutElement.destroy()
  expressCheckoutElement = null
})
</script>
