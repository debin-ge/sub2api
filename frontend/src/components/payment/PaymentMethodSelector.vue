<template>
  <div>
    <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
      {{ t('payment.paymentMethod') }}
    </label>
    <div
      data-testid="payment-method-grid"
      class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4"
    >
      <button
        v-for="method in sortedMethods"
        :key="method.type"
        type="button"
        :title="methodLabel(method)"
        :disabled="!method.available"
        :class="[
          'relative flex min-h-[60px] min-w-0 flex-col items-center justify-center rounded-lg border px-3 py-2.5 transition-all sm:flex-1',
          !method.available
            ? 'cursor-not-allowed border-gray-200 bg-gray-50 opacity-50 dark:border-dark-700 dark:bg-dark-800/50'
            : selected === method.type
              ? methodSelectedClass(method.type)
              : 'border-gray-300 bg-white text-gray-700 hover:border-gray-400 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200 dark:hover:border-dark-500',
        ]"
        @click="method.available && emit('select', method.type)"
      >
        <span class="flex w-full min-w-0 items-center justify-center gap-2">
          <img :src="methodIcon(method.type)" :alt="methodLabel(method)" class="h-7 w-7 shrink-0 object-contain" />
          <span class="flex min-w-0 flex-col items-start leading-none">
            <span data-testid="payment-method-label" class="block w-full truncate text-base font-semibold">
              {{ methodLabel(method) }}
            </span>
            <span
              v-if="method.fee_rate > 0"
              class="text-[10px] tracking-wide text-gray-500 dark:text-dark-400"
            >
              {{ t('payment.fee') }} {{ method.fee_rate }}%
            </span>
          </span>
        </span>
        <span
          v-if="stripeSubMethods(method).length > 0"
          data-testid="stripe-sub-methods"
          class="mt-1.5 flex flex-wrap items-center justify-center gap-x-2.5 gap-y-1 text-[11px] font-medium leading-none text-gray-500 dark:text-gray-400"
        >
          <span
            v-for="subMethod in stripeSubMethods(method)"
            :key="subMethod"
            :data-testid="`stripe-sub-method-${subMethod}`"
            class="inline-flex items-center gap-1 whitespace-nowrap"
          >
            <img :src="stripeSubMethodIcon(subMethod)" alt="" class="h-3.5 w-3.5 object-contain" />
            <span>{{ stripeSubMethodLabel(subMethod) }}</span>
          </span>
        </span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { METHOD_ORDER, isBuiltInAlipayMethod, isBuiltInWxpayMethod } from './providerConfig'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'
import stripeIcon from '@/assets/icons/stripe.svg'
import airwallexIcon from '@/assets/icons/airwallex.svg'
import paymentIcon from '@/assets/icons/payment.svg'

export interface PaymentMethodOption {
  type: string
  display_name?: string
  supported_types?: string[]
  fee_rate: number
  available: boolean
}

const props = defineProps<{
  methods: PaymentMethodOption[]
  selected: string
}>()

const emit = defineEmits<{
  select: [type: string]
}>()

const { t } = useI18n()

const METHOD_ICONS: Record<string, string> = {
  alipay: alipayIcon,
  wxpay: wxpayIcon,
  stripe: stripeIcon,
  airwallex: airwallexIcon,
  credit_card: paymentIcon,
}

const STRIPE_SUB_METHOD_ORDER = ['card', 'wxpay', 'alipay', 'link'] as const

const sortedMethods = computed(() => {
  const order: readonly string[] = METHOD_ORDER
  return [...props.methods].sort((a, b) => {
    const ai = order.indexOf(a.type)
    const bi = order.indexOf(b.type)
    return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi)
  })
})

function methodIcon(type: string): string {
  if (isBuiltInAlipayMethod(type)) return METHOD_ICONS.alipay
  if (isBuiltInWxpayMethod(type)) return METHOD_ICONS.wxpay
  if (type === 'airwallex') return METHOD_ICONS.airwallex
  return METHOD_ICONS[type] || paymentIcon
}

function methodLabel(method: PaymentMethodOption): string {
  return method.display_name || t(`payment.methods.${method.type}`, method.type)
}

function normalizeStripeSubMethod(type: string): string {
  const normalized = type.trim().toLowerCase()
  if (normalized === 'wechat_pay') return 'wxpay'
  if (normalized === 'credit_card') return 'card'
  return normalized
}

function stripeSubMethods(method: PaymentMethodOption): string[] {
  if (method.type !== 'stripe' || !Array.isArray(method.supported_types)) return []

  const uniqueMethods = new Set(
    method.supported_types
      .map(normalizeStripeSubMethod)
      .filter(Boolean),
  )
  return [...uniqueMethods].sort((a, b) => {
    const ai = STRIPE_SUB_METHOD_ORDER.indexOf(a as typeof STRIPE_SUB_METHOD_ORDER[number])
    const bi = STRIPE_SUB_METHOD_ORDER.indexOf(b as typeof STRIPE_SUB_METHOD_ORDER[number])
    return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi)
  })
}

function stripeSubMethodIcon(type: string): string {
  return methodIcon(type)
}

function stripeSubMethodLabel(type: string): string {
  return t(`payment.methods.${type}`, type)
}

function methodSelectedClass(type: string): string {
  if (isBuiltInAlipayMethod(type)) return 'border-[#02A9F1] bg-blue-50 text-gray-900 shadow-sm dark:bg-blue-950 dark:text-gray-100'
  if (isBuiltInWxpayMethod(type)) return 'border-[#09BB07] bg-green-50 text-gray-900 shadow-sm dark:bg-green-950 dark:text-gray-100'
  if (type === 'stripe') return 'border-[#676BE5] bg-indigo-50 text-gray-900 shadow-sm dark:bg-indigo-950 dark:text-gray-100'
  if (type === 'airwallex') return 'border-[#FF6B3D] bg-orange-50 text-gray-900 shadow-sm dark:border-[#FF8E3C] dark:bg-orange-950 dark:text-gray-100'
  return 'border-primary-500 bg-primary-50 text-gray-900 shadow-sm dark:bg-primary-950 dark:text-gray-100'
}
</script>
