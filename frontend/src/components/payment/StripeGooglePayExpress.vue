<template>
  <div
    v-show="available"
    data-testid="stripe-google-pay-express"
    class="space-y-3"
    :aria-busy="confirming"
  >
    <div
      ref="mountTarget"
      :class="{ 'pointer-events-none opacity-60': disabled || confirming }"
    />
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
const available = ref(false)
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

function setAvailable(next: boolean) {
  if (available.value === next) return
  available.value = next
  emit('availabilityChange', next)
}

function setSubmitting(next: boolean) {
  confirming.value = next
  emit('submittingChange', next)
}

function handleReady(event: StripeExpressCheckoutElementReadyEvent) {
  setAvailable(Boolean(event.availablePaymentMethods?.googlePay))
}

function handleAvailabilityChange(
  event: StripeExpressCheckoutElementAvailablePaymentMethodsChangeEvent,
) {
  setAvailable(Boolean(event.paymentMethods?.googlePay?.available))
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
  setAvailable(false)
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
