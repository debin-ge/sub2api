<template>
  <div class="flex min-h-screen items-center justify-center bg-gray-50 px-4 dark:bg-dark-900">
    <div class="w-full max-w-md space-y-6">
      <!-- Loading -->
      <div v-if="loading" class="flex items-center justify-center py-20">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
      </div>
      <template v-else>
        <!-- Status Icon -->
        <div class="text-center">
          <div v-if="isSuccess"
            class="mx-auto flex h-20 w-20 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30">
            <svg class="h-10 w-10 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"
              stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
            </svg>
          </div>
          <div v-else-if="isPending"
            class="mx-auto flex h-20 w-20 items-center justify-center rounded-full bg-yellow-100 dark:bg-yellow-900/30">
            <div class="h-10 w-10 animate-spin rounded-full border-4 border-yellow-500 border-t-transparent"></div>
          </div>
          <div v-else
            class="mx-auto flex h-20 w-20 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/30">
            <svg class="h-10 w-10 text-red-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </div>
          <h2 class="mt-4 text-2xl font-bold text-gray-900 dark:text-white">
            {{ statusTitle }}
          </h2>
          <p v-if="isPending" class="mt-2 text-sm text-gray-500 dark:text-gray-400">
            {{ t('payment.result.processingHint') }}
          </p>
        </div>
        <!-- Order Info -->
        <div v-if="order" class="rounded-xl bg-white p-5 shadow-sm dark:bg-dark-800">
          <div class="space-y-3 text-sm">
            <div v-if="hasOrderId(order)" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderId') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">#{{ order.id }}</span>
            </div>
            <div v-if="order.out_trade_no" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderNo') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ order.out_trade_no }}</span>
            </div>
            <div v-if="hasAmountFields(order)" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.baseAmount') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ formatGatewayAmount(baseAmount) }}</span>
            </div>
            <div v-if="hasAmountFields(order) && order.fee_rate > 0" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.fee') }} ({{ order.fee_rate }}%)</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ formatGatewayAmount(feeAmount) }}</span>
            </div>
            <div v-if="hasAmountFields(order)" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.payAmount') }}</span>
              <span class="font-bold text-primary-600 dark:text-primary-400">{{ formatGatewayAmount(order.pay_amount) }}</span>
            </div>
            <div v-if="hasAmountFields(order) && order.amount !== order.pay_amount" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.creditedAmount') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ order.order_type === 'balance' ? '$' + order.amount.toFixed(2) : formatGatewayAmount(order.amount) }}</span>
            </div>
            <div v-if="hasPaymentType(order)" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.paymentMethod') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ t(paymentMethodI18nKey(order.payment_type), normalizedOrderPaymentType(order.payment_type)) }}</span>
            </div>
            <div class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.status') }}</span>
              <OrderStatusBadge :status="displayOrderStatus(order.status)" />
            </div>
          </div>
        </div>
        <!-- EasyPay return info (when no order loaded) -->
        <div v-else-if="returnInfo" class="rounded-xl bg-white p-5 shadow-sm dark:bg-dark-800">
          <div class="space-y-3 text-sm">
            <div v-if="returnInfo.outTradeNo" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderId') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ returnInfo.outTradeNo }}</span>
            </div>
            <div v-if="returnInfo.money" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.payAmount') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ formatGatewayAmount(Number(returnInfo.money) || 0) }}</span>
            </div>
            <div v-if="returnInfo.type" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.paymentMethod') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ t(paymentMethodI18nKey(returnInfo.type), normalizedOrderPaymentType(returnInfo.type)) }}</span>
            </div>
          </div>
        </div>
        <!-- Actions -->
        <div class="flex gap-3">
          <button class="btn btn-secondary flex-1" @click="router.push('/purchase')">{{ t('payment.result.backToRecharge') }}</button>
          <button class="btn btn-primary flex-1" @click="router.push('/orders')">{{ t('payment.result.viewOrders') }}</button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import OrderStatusBadge from '@/components/payment/OrderStatusBadge.vue'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  clearPaymentRecoverySnapshot,
  readPaymentRecoverySnapshot,
} from '@/components/payment/paymentFlow'
import { usePaymentStore } from '@/stores/payment'
import { paymentAPI } from '@/api/payment'
import type { PublicOrderResult, PublicOrderVerifyResult } from '@/api/payment'
import type { OrderStatus, PaymentOrder } from '@/types/payment'
import { formatPaymentAmount, normalizePaymentCurrency } from '@/components/payment/currency'
import { normalizePaymentMethodForDisplay, paymentMethodI18nKey } from './paymentUx'

const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const paymentStore = usePaymentStore()

type DetailedResolvedOrder = PaymentOrder | PublicOrderResult
type ResolvedOrder = DetailedResolvedOrder | PublicOrderVerifyResult

const order = ref<ResolvedOrder | null>(null)
const loading = ref(true)
const currency = ref('CNY')

interface ReturnInfo {
  outTradeNo: string
  money: string
  type: string
  tradeStatus: string
}
const returnInfo = ref<ReturnInfo | null>(null)

const SUCCESS_STATUSES = new Set(['COMPLETED', 'PAID', 'RECHARGING'])
const PENDING_STATUSES = new Set(['PENDING', 'CREATED', 'WAITING', 'PROCESSING'])
const STATUS_REFRESH_INTERVAL_MS = 2000
const STATUS_REFRESH_BACKOFF_INTERVAL_MS = 10000
const STATUS_REFRESH_FAST_ATTEMPTS = 15
const STRIPE_STATUS_REFRESH_FAST_ATTEMPTS = 150
const STRIPE_VERIFY_INTERVAL_MS = 15000
const STRIPE_VERIFY_MAX_ATTEMPTS = 20

type StatusRefreshOrder = () => Promise<ResolvedOrder | null>

let statusRefreshTimer: ReturnType<typeof setTimeout> | null = null
const refreshAttempts = ref(0)
let upstreamRecoveryAttempts = 0
let lastUpstreamRecoveryAt: number | null = null
let activeStatusRefresh: StatusRefreshOrder | null = null
let resolveStatusRefreshInterval: (() => number) | null = null
let statusRefreshInFlight = false
let statusRefreshStopped = false

/** 充值金额 = pay_amount / (1 + fee_rate/100)，fee_rate=0 时等于 pay_amount */
const baseAmount = computed(() => {
  if (!hasAmountFields(order.value)) return 0
  const feeRate = Number(order.value.fee_rate) || 0
  if (feeRate <= 0) return order.value.pay_amount ?? 0
  return Math.round((order.value.pay_amount / (1 + feeRate / 100)) * 100) / 100
})

/** 手续费 = pay_amount - baseAmount */
const feeAmount = computed(() => {
  if (!hasAmountFields(order.value)) return 0
  const feeRate = Number(order.value.fee_rate) || 0
  if (feeRate <= 0) return 0
  return Math.round((order.value.pay_amount - baseAmount.value) * 100) / 100
})

const localeCode = computed(() => {
  const raw = i18n.locale as unknown
  if (typeof raw === 'string') return raw
  if (raw && typeof raw === 'object' && 'value' in raw) {
    return String((raw as { value?: string }).value || '')
  }
  return undefined
})

const isSuccess = computed(() => {
  return isSuccessStatus(order.value?.status)
})

const isPending = computed(() => {
  return isPendingStatus(order.value?.status)
})

const statusTitle = computed(() => {
  if (isSuccess.value) {
    return t('payment.result.success')
  }
  if (isPending.value) {
    return t('payment.result.processing')
  }
  return t('payment.result.failed')
})

function normalizedOrderPaymentType(paymentType: string): string {
  return normalizePaymentMethodForDisplay(paymentType || '') || paymentType || ''
}

function formatGatewayAmount(value: number): string {
  return formatPaymentAmount(value, currency.value, localeCode.value)
}

function setResolvedOrder(nextOrder: ResolvedOrder | null): void {
  order.value = nextOrder
  if (nextOrder && 'currency' in nextOrder && nextOrder.currency) {
    currency.value = normalizePaymentCurrency(nextOrder.currency)
  }
}

function hasOrderId(nextOrder: ResolvedOrder | null): nextOrder is DetailedResolvedOrder {
  return !!nextOrder && 'id' in nextOrder && typeof nextOrder.id === 'number'
}

function hasAmountFields(nextOrder: ResolvedOrder | null): nextOrder is DetailedResolvedOrder {
  return !!nextOrder && 'pay_amount' in nextOrder && typeof nextOrder.pay_amount === 'number' && 'amount' in nextOrder && typeof nextOrder.amount === 'number'
}

function hasPaymentType(nextOrder: ResolvedOrder | null): nextOrder is DetailedResolvedOrder {
  return !!nextOrder && 'payment_type' in nextOrder && typeof nextOrder.payment_type === 'string' && nextOrder.payment_type.trim() !== ''
}

function normalizeOrderStatus(status: string | null | undefined): string {
  return String(status || '').trim().toUpperCase()
}

function displayOrderStatus(status: string): OrderStatus {
  return normalizeOrderStatus(status) as OrderStatus
}

function isSuccessStatus(status: string | null | undefined): boolean {
  return SUCCESS_STATUSES.has(normalizeOrderStatus(status))
}

function isPendingStatus(status: string | null | undefined): boolean {
  return PENDING_STATUSES.has(normalizeOrderStatus(status))
}

function readRouteQueryString(key: string): string {
  const value = route.query[key]
  if (Array.isArray(value)) {
    return typeof value[0] === 'string' ? value[0] : ''
  }
  return typeof value === 'string' ? value : ''
}

function restoreRecoverySnapshot(context: {
  resumeToken: string
  routeOrderId: number
  routeOutTradeNo: string
}) {
  if (typeof window === 'undefined') {
    return null
  }

  const rawSnapshot = window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)
  if (!rawSnapshot) {
    return null
  }

  if (context.resumeToken) {
    return readPaymentRecoverySnapshot(rawSnapshot, {
      resumeToken: context.resumeToken,
    })
  }

  if (!context.routeOrderId && !context.routeOutTradeNo) {
    return null
  }

  const restored = readPaymentRecoverySnapshot(rawSnapshot)
  if (!restored) {
    return null
  }

  if (context.routeOrderId > 0 && restored.orderId !== context.routeOrderId) {
    return null
  }

  if (context.routeOutTradeNo && restored.outTradeNo !== context.routeOutTradeNo) {
    return null
  }

  return restored
}

async function resolveOrderFromResumeToken(resumeToken: string): Promise<ResolvedOrder | null> {
  try {
    const result = await paymentAPI.resolveOrderPublicByResumeToken(resumeToken)
    return result.data
  } catch (_err: unknown) {
    return null
  }
}

interface ResumeTokenResolution {
  order: ResolvedOrder | null
  attempted: boolean
}

function canAttemptUpstreamRecovery(now = Date.now()): boolean {
  return upstreamRecoveryAttempts < STRIPE_VERIFY_MAX_ATTEMPTS
    && (
      lastUpstreamRecoveryAt === null
      || now - lastUpstreamRecoveryAt >= STRIPE_VERIFY_INTERVAL_MS
    )
}

function recordUpstreamRecoveryAttempt(now = Date.now()): void {
  lastUpstreamRecoveryAt = now
  upstreamRecoveryAttempts += 1
}

async function resolveOrderFromResumeTokenThrottled(
  resumeToken: string,
  force = false,
): Promise<ResumeTokenResolution> {
  const now = Date.now()
  if (!force && !canAttemptUpstreamRecovery(now)) {
    return { order: null, attempted: false }
  }

  recordUpstreamRecoveryAttempt(now)
  return {
    order: await resolveOrderFromResumeToken(resumeToken),
    attempted: true,
  }
}

async function resolveOrderFromOutTradeNo(outTradeNo: string): Promise<ResolvedOrder | null> {
  try {
    const result = await paymentAPI.verifyOrder(outTradeNo)
    return result.data
  } catch (_err: unknown) {
    try {
      const result = await paymentAPI.verifyOrderPublic(outTradeNo)
      return result.data
    } catch (_innerErr: unknown) {
      return null
    }
  }
}

function isStripePaymentContext(
  currentOrder: ResolvedOrder | null,
  fallbackPaymentType: string,
): boolean {
  const orderPaymentType = hasPaymentType(currentOrder)
    ? normalizedOrderPaymentType(currentOrder.payment_type)
    : ''
  const providerKey = hasOrderId(currentOrder)
    ? String(currentOrder.provider_key || '').trim().toLowerCase()
    : ''
  const recoveryPaymentType = normalizedOrderPaymentType(fallbackPaymentType)
  return providerKey === 'stripe'
    || orderPaymentType === 'stripe'
    || recoveryPaymentType === 'stripe'
}

async function tryRecoverPendingStripeOrder(
  currentOrder: ResolvedOrder | null,
  fallbackOutTradeNo: string,
  fallbackPaymentType: string,
): Promise<ResolvedOrder | null> {
  if (!currentOrder || !isPendingStatus(currentOrder.status)) return currentOrder

  if (!isStripePaymentContext(currentOrder, fallbackPaymentType)) {
    return currentOrder
  }

  const outTradeNo = String(currentOrder.out_trade_no || fallbackOutTradeNo || '').trim()
  if (!outTradeNo) return currentOrder

  const now = Date.now()
  if (!canAttemptUpstreamRecovery(now)) {
    return currentOrder
  }

  recordUpstreamRecoveryAttempt(now)
  try {
    const result = await paymentAPI.verifyOrder(outTradeNo)
    return result.data || currentOrder
  } catch {
    return currentOrder
  }
}

function clearStatusRefreshTimer(): void {
  if (statusRefreshTimer !== null) {
    clearTimeout(statusRefreshTimer)
    statusRefreshTimer = null
  }
}

function clearRecoverySnapshot(): void {
  if (typeof window === 'undefined') return
  clearPaymentRecoverySnapshot(window.localStorage, PAYMENT_RECOVERY_STORAGE_KEY)
}

function clearRecoverySnapshotForTerminalStatus(status: string | null | undefined): void {
  if (!status) return
  if (!isPendingStatus(status)) {
    clearRecoverySnapshot()
  }
}

function scheduleStatusRefresh(): void {
  clearStatusRefreshTimer()
  if (
    statusRefreshStopped
    || !activeStatusRefresh
    || !isPending.value
    || document.visibilityState === 'hidden'
  ) {
    return
  }

  const interval = resolveStatusRefreshInterval?.() ?? STATUS_REFRESH_INTERVAL_MS
  statusRefreshTimer = setTimeout(() => {
    statusRefreshTimer = null
    void refreshPendingOrder()
  }, interval)
}

async function refreshPendingOrder(): Promise<void> {
  if (
    statusRefreshStopped
    || statusRefreshInFlight
    || !activeStatusRefresh
    || !isPending.value
  ) {
    return
  }

  statusRefreshInFlight = true
  try {
    refreshAttempts.value += 1
    const refreshedOrder = await activeStatusRefresh()
    if (refreshedOrder) {
      setResolvedOrder(refreshedOrder)
      clearRecoverySnapshotForTerminalStatus(refreshedOrder.status)
    }
  } catch {
    // A transient request failure must not permanently stop status refreshes.
  } finally {
    statusRefreshInFlight = false
    if (isPendingStatus(order.value?.status)) {
      scheduleStatusRefresh()
    }
  }
}

function startStatusRefresh(
  refreshOrder: StatusRefreshOrder,
  resolveInterval: () => number,
): void {
  activeStatusRefresh = refreshOrder
  resolveStatusRefreshInterval = resolveInterval
  scheduleStatusRefresh()
}

function requestImmediateStatusRefresh(forceUpstreamRecovery = false): void {
  if (
    statusRefreshStopped
    || statusRefreshInFlight
    || !activeStatusRefresh
    || !isPending.value
  ) {
    return
  }
  if (forceUpstreamRecovery && upstreamRecoveryAttempts < STRIPE_VERIFY_MAX_ATTEMPTS) {
    lastUpstreamRecoveryAt = null
  }
  clearStatusRefreshTimer()
  void refreshPendingOrder()
}

function handleVisibilityChange(): void {
  if (document.visibilityState === 'visible') {
    requestImmediateStatusRefresh(true)
  } else {
    clearStatusRefreshTimer()
  }
}

function handleWindowFocus(): void {
  requestImmediateStatusRefresh()
}

onMounted(async () => {
  document.addEventListener('visibilitychange', handleVisibilityChange)
  window.addEventListener('focus', handleWindowFocus)

  let resumeToken = readRouteQueryString('resume_token')
  const routeOrderId = Number(readRouteQueryString('order_id')) || 0
  let outTradeNo = readRouteQueryString('out_trade_no')
  let orderId = 0
  let resumeTokenLookupFailed = false
  let initialResumeResolutionSucceeded = false
  let recoveryPaymentType = ''

  const restored = restoreRecoverySnapshot({
    resumeToken,
    routeOrderId,
    routeOutTradeNo: outTradeNo,
  })
  if (restored?.orderId) {
    orderId = restored.orderId
  }
  if (restored?.currency) {
    currency.value = normalizePaymentCurrency(restored.currency)
  }
  if (restored?.paymentType) {
    recoveryPaymentType = restored.paymentType
  }
  if (!resumeToken && restored?.resumeToken) {
    resumeToken = restored.resumeToken
  }
  if (!outTradeNo && restored?.outTradeNo) {
    outTradeNo = restored.outTradeNo
  }

  if (resumeToken) {
    const resumeResolution = await resolveOrderFromResumeTokenThrottled(resumeToken, true)
    const resolvedOrder = resumeResolution.order
    if (resolvedOrder) {
      initialResumeResolutionSucceeded = true
      setResolvedOrder(resolvedOrder)
      if (!orderId) {
        orderId = hasOrderId(resolvedOrder) ? resolvedOrder.id : routeOrderId
      }
    } else if (routeOrderId > 0) {
      resumeTokenLookupFailed = true
      orderId = routeOrderId
    } else {
      resumeTokenLookupFailed = true
    }
  } else if (routeOrderId > 0) {
    orderId = routeOrderId
  }

  const hasLegacyFallbackContext = readRouteQueryString('trade_status').trim() !== ''
  const shouldUsePublicOutTradeNo = outTradeNo !== '' && (hasLegacyFallbackContext || routeOrderId > 0 || orderId > 0)

  if (!order.value && orderId && (!resumeToken || routeOrderId > 0)) {
    try {
      setResolvedOrder(await paymentStore.pollOrderStatus(orderId))
    } catch (_err: unknown) {
      // Order lookup failed, will try legacy fallback below when possible.
    }
  }

  if (!order.value && shouldUsePublicOutTradeNo && (!resumeToken || resumeTokenLookupFailed)) {
    const legacyOrder = await resolveOrderFromOutTradeNo(outTradeNo)
    if (legacyOrder) {
      setResolvedOrder(legacyOrder)
      if (!orderId) {
        orderId = hasOrderId(legacyOrder) ? legacyOrder.id : 0
      }
    }
  }

  if (isPendingStatus(order.value?.status) && !initialResumeResolutionSucceeded) {
    const recoveredOrder = await tryRecoverPendingStripeOrder(
      order.value,
      outTradeNo,
      recoveryPaymentType,
    )
    if (recoveredOrder) {
      setResolvedOrder(recoveredOrder)
      if (!orderId) {
        orderId = hasOrderId(recoveredOrder) ? recoveredOrder.id : 0
      }
    }
  }

  if (!order.value && !orderId && outTradeNo && hasLegacyFallbackContext) {
    returnInfo.value = {
      outTradeNo,
      money: String(route.query.money || ''),
      type: String(route.query.type || ''),
      tradeStatus: String(route.query.trade_status || ''),
    }
  }

  const refreshOrder = async (): Promise<ResolvedOrder | null> => {
    let refreshedOrder: ResolvedOrder | null = null
    let resumeResolutionSucceeded = false

    if (orderId) {
      try {
        refreshedOrder = await paymentStore.pollOrderStatus(orderId)
      } catch (_err: unknown) {
        // Fall through to the throttled signed recovery lookup when available.
      }
    }

    const stripeContext = isStripePaymentContext(
      refreshedOrder || order.value,
      recoveryPaymentType,
    )
    const shouldUseSignedRecovery = !!resumeToken
      && (
        !refreshedOrder
        || (stripeContext && isPendingStatus(refreshedOrder.status))
      )

    if (shouldUseSignedRecovery) {
      if (stripeContext) {
        const resumeResolution = await resolveOrderFromResumeTokenThrottled(resumeToken)
        if (resumeResolution.order) {
          refreshedOrder = resumeResolution.order
          resumeResolutionSucceeded = true
        }
      } else {
        const resolvedOrder = await resolveOrderFromResumeToken(resumeToken)
        if (resolvedOrder) {
          refreshedOrder = resolvedOrder
          resumeResolutionSucceeded = true
        }
      }
    }

    if (!refreshedOrder && !resumeToken && shouldUsePublicOutTradeNo) {
      refreshedOrder = await resolveOrderFromOutTradeNo(outTradeNo)
    }

    if (resumeResolutionSucceeded) {
      return refreshedOrder
    }

    return await tryRecoverPendingStripeOrder(
      refreshedOrder,
      outTradeNo,
      recoveryPaymentType,
    )
  }

  if (isPendingStatus(order.value?.status)) {
    const resolveRefreshInterval = () => {
      const fastAttempts = isStripePaymentContext(order.value, recoveryPaymentType)
        ? STRIPE_STATUS_REFRESH_FAST_ATTEMPTS
        : STATUS_REFRESH_FAST_ATTEMPTS
      return refreshAttempts.value < fastAttempts
        ? STATUS_REFRESH_INTERVAL_MS
        : STATUS_REFRESH_BACKOFF_INTERVAL_MS
    }
    startStatusRefresh(refreshOrder, resolveRefreshInterval)
  } else if (order.value) {
    clearRecoverySnapshotForTerminalStatus(order.value.status)
  } else if (returnInfo.value) {
    clearRecoverySnapshot()
  }
  loading.value = false
})

onBeforeUnmount(() => {
  statusRefreshStopped = true
  activeStatusRefresh = null
  resolveStatusRefreshInterval = null
  clearStatusRefreshTimer()
  document.removeEventListener('visibilitychange', handleVisibilityChange)
  window.removeEventListener('focus', handleWindowFocus)
})
</script>
