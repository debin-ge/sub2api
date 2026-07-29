/**
 * formatScaled formats a per-token (or per-request) USD price scaled by `scale`.
 *
 *   formatScaled(0.000003, 1_000_000)    → "$3"      // per 1M tokens
 *   formatScaled(0.5,        1)          → "$0.5"    // per request
 *   formatScaled(null,       1_000_000)  → "-"
 *   formatScaled(0.000003, 1_000_000, 2) → "$3.00"   // pad to ≥2 decimals
 *   formatScaled(1.25e-8,  1_000_000, 2) → "$0.0125" // longer decimals kept as-is
 *
 * Rounds to at most six decimal places and strips trailing zeros.
 * `minFractionDigits` pads the result back up to a minimum number of decimals.
 */
export function formatScaled(value: number | null, scale: number, minFractionDigits = 0): string {
  if (value == null) return '-'
  let s = formatFixedMax(value * scale, 6)
  if (minFractionDigits > 0 && !s.includes('e')) {
    const dot = s.indexOf('.')
    const digits = dot === -1 ? 0 : s.length - dot - 1
    if (digits < minFractionDigits) {
      s = (dot === -1 ? `${s}.` : s) + '0'.repeat(minFractionDigits - digits)
    }
  }
  return `$${s}`
}

export const PLAZA_DEFAULT_MULTIPLIER = 1
export const PLAZA_DEFAULT_CNY_USD_RATE = 6.8
export const PER_MILLION_TOKEN_SCALE = 1_000_000
export const PER_REQUEST_SCALE = 1

export function normalizePlazaMultiplier(value: number | null | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? value
    : PLAZA_DEFAULT_MULTIPLIER
}

export function normalizePlazaRate(value: number | null | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? value
    : PLAZA_DEFAULT_CNY_USD_RATE
}

export function normalizeBillingRateMultiplier(value: number | null | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? value
    : 1
}

function formatCNY(value: number): string {
  if (value > 0 && value < 0.005) return '<¥0.01'
  const formatted = value.toFixed(2).replace(/\.?0+$/, '')
  return `¥${formatted}`
}

function formatNumber(value: number): string {
  return value.toFixed(2).replace(/\.?0+$/, '')
}

function formatFixedMax(value: number, maxFractionDigits: number): string {
  return value.toFixed(maxFractionDigits).replace(/\.?0+$/, '')
}

export function formatCNYMarket(
  usd: number | null,
  rate: number | null | undefined,
  scale: number
): string {
  if (usd == null) return '-'
  return formatCNY(usd * normalizePlazaRate(rate) * scale)
}

export function formatCNYRecharged(
  usd: number | null,
  multiplier: number | null | undefined,
  scale: number,
  billingRateMultiplier?: number | null
): string {
  if (usd == null) return '-'
  return formatCNY(
    (usd * normalizeBillingRateMultiplier(billingRateMultiplier) / normalizePlazaMultiplier(multiplier)) * scale
  )
}

export function computeValueBoost(
  multiplier: number | null | undefined,
  rate: number | null | undefined
): number {
  return Number((normalizePlazaMultiplier(multiplier) * normalizePlazaRate(rate)).toFixed(2))
}

export function computeDiscountFold(
  multiplier: number | null | undefined,
  rate: number | null | undefined,
  billingRateMultiplier?: number | null
): number {
  const ratio = normalizeBillingRateMultiplier(billingRateMultiplier) /
    (normalizePlazaMultiplier(multiplier) * normalizePlazaRate(rate))
  return Number((ratio * 10).toFixed(2))
}

export function computeDiscountPercent(
  multiplier: number | null | undefined,
  rate: number | null | undefined,
  billingRateMultiplier?: number | null
): number {
  const ratio = normalizeBillingRateMultiplier(billingRateMultiplier) /
    (normalizePlazaMultiplier(multiplier) * normalizePlazaRate(rate))
  return Number((ratio * 100).toFixed(2))
}

export function formatDiscountFold(value: number): string {
  return formatNumber(value)
}
