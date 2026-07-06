/**
 * formatScaled formats a per-token (or per-request) USD price scaled by `scale`.
 *
 *   formatScaled(0.000003, 1_000_000) → "$3"        // per 1M tokens
 *   formatScaled(0.5,        1)        → "$0.5"      // per request
 *   formatScaled(null,       1_000_000) → "-"
 *
 * Uses toPrecision(10) then strips trailing zeros to avoid IEEE 754 display noise.
 */
export function formatScaled(value: number | null, scale: number): string {
  if (value == null) return '-'
  return `$${(value * scale).toPrecision(10).replace(/\.?0+$/, '')}`
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

function formatCNY(value: number): string {
  if (value > 0 && value < 0.005) return '<¥0.01'
  const formatted = value.toFixed(2).replace(/\.?0+$/, '')
  return `¥${formatted}`
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
  scale: number
): string {
  if (usd == null) return '-'
  return formatCNY((usd / normalizePlazaMultiplier(multiplier)) * scale)
}

export function computeValueBoost(
  multiplier: number | null | undefined,
  rate: number | null | undefined
): number {
  return Number((normalizePlazaMultiplier(multiplier) * normalizePlazaRate(rate)).toFixed(2))
}
