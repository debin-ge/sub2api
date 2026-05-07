/**
 * 格式化缓存 token 数量（1K/1M 缩写）
 */
export function formatCacheTokens(tokens: number): string {
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return tokens.toLocaleString()
}

/**
 * 自适应精度格式化倍率（确保小数值如 0.001 不被截断）
 */
export function formatMultiplier(val: number): string {
  if (val >= 0.01) return val.toFixed(2)
  if (val >= 0.001) return val.toFixed(3)
  if (val >= 0.0001) return val.toFixed(4)
  return val.toPrecision(2)
}

/**
 * 格式化钱包余额。余额在后端按 DECIMAL(20,8) 存储，token 计费常低于 1 美分；
 * 展示最多 4 位小数，避免固定 2 位把真实扣费完全隐藏。
 */
export function formatBalanceAmount(value: number | null | undefined): string {
  const amount = Number(value ?? 0)
  if (!Number.isFinite(amount)) {
    return '0.00'
  }

  const normalized = Number(amount.toFixed(4))
  const roundedToCents = Math.round(normalized * 100) / 100
  if (Math.abs(normalized - roundedToCents) < 0.000000005) {
    return normalized.toFixed(2)
  }

  const fixed = normalized.toFixed(4)
  const trimmed = fixed.replace(/(\.\d*?[1-9])0+$/, '$1').replace(/\.0+$/, '.00')
  const decimalIndex = trimmed.indexOf('.')
  if (decimalIndex === -1) {
    return `${trimmed}.00`
  }
  const decimals = trimmed.length - decimalIndex - 1
  if (decimals < 2) {
    return `${trimmed}${'0'.repeat(2 - decimals)}`
  }
  return trimmed
}
