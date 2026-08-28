/**
 * formatScaled formats a per-token (or per-request) USD price scaled by `scale`.
 *
 *   formatScaled(0.000003, 1_000_000) → "$3"     // per 1M tokens
 *   formatScaled(0.5,        1)       → "$0.5"   // per request
 *   formatScaled(null,       1_000_000) → "-"
 *
 * Rounds to at most six decimal places and strips trailing zeros.
 */
export function formatScaled(value: number | null, scale: number): string {
  if (value == null) return '-'
  return `$${formatFixedMax(value * scale, 6)}`
}

export const PLAZA_DEFAULT_MULTIPLIER = 1
export const PER_MILLION_TOKEN_SCALE = 1_000_000
export const PER_REQUEST_SCALE = 1

export type ModelPriceCurrency = 'USD' | 'CNY'

export function normalizeModelPriceCurrency(value: string | null | undefined): ModelPriceCurrency {
  return String(value ?? '').toUpperCase() === 'CNY' ? 'CNY' : 'USD'
}

export function normalizePlazaMultiplier(value: number | null | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? value
    : PLAZA_DEFAULT_MULTIPLIER
}

export function normalizeBillingRateMultiplier(value: number | null | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0
    ? value
    : 1
}

function formatNumber(value: number): string {
  return value.toFixed(2).replace(/\.?0+$/, '')
}

function formatFixedMax(value: number, maxFractionDigits: number): string {
  return value.toFixed(maxFractionDigits).replace(/\.?0+$/, '')
}

export function formatMoney(
  value: number | null,
  currency: ModelPriceCurrency | string | null | undefined,
  scale: number
): string {
  if (value == null) return '-'
  const symbol = normalizeModelPriceCurrency(currency) === 'CNY' ? '¥' : '$'
  return `${symbol}${formatFixedMax(value * scale, 6)}`
}

/**
 * Final plaza price, always rendered with the CNY balance symbol.
 * Currency metadata does not alter the numeric value and no exchange rate is applied.
 */
export function formatCNYEffective(
  value: number | null,
  scale: number,
  billingRateMultiplier?: number | null
): string {
  if (value == null) return '-'
  return formatMoney(value * normalizeBillingRateMultiplier(billingRateMultiplier), 'CNY', scale)
}

export function computeDiscountFold(
  billingRateMultiplier?: number | null
): number {
  return Number((normalizeBillingRateMultiplier(billingRateMultiplier) * 10).toFixed(2))
}

export function computeDiscountPercent(
  billingRateMultiplier?: number | null
): number {
  return Number((normalizeBillingRateMultiplier(billingRateMultiplier) * 100).toFixed(2))
}

export function formatDiscountFold(value: number): string {
  return formatNumber(value)
}

/**
 * 按官方分时规则把「随行的那份生效价」换算到某一档（高峰 / 空闲）。
 * 倍率相对基准价，方向由后端给的 peak_multiplier / off_peak_multiplier 决定：
 * 目录价是空闲价（2 / 1），官方兜底表是高峰价（1 / 0.5）。
 */
export function scheduledScaledPrice(
  value: number | null,
  multiplier: number | null | undefined
): number | null {
  if (value == null || typeof multiplier !== 'number' || !Number.isFinite(multiplier) || multiplier < 0) {
    return null
  }
  return value * multiplier
}
