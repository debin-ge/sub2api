import { describe, expect, it } from 'vitest'
import {
  computeDiscountFold,
  computeDiscountPercent,
  formatMoney,
  formatCNYEffective,
  formatDiscountFold,
  formatScaled,
  normalizePlazaMultiplier,
  scheduledScaledPrice
} from '@/utils/pricing'

describe('plaza pricing helpers', () => {
  it('rounds long decimal USD prices to at most six decimal places', () => {
    expect(formatScaled(0.123456789, 1)).toBe('$0.123457')
    expect(formatScaled(0.00000123456789, 1_000_000)).toBe('$1.234568')
    expect(formatScaled(0.5, 1)).toBe('$0.5')
    expect(formatScaled(0.000003, 1_000_000)).toBe('$3')
  })

  it('formats the final CNY price from the numeric price and billing multiplier', () => {
    expect(formatCNYEffective(0.000015, 1_000_000)).toBe('¥15')
  })

  it('includes the effective group billing multiplier in CNY prices', () => {
    expect(formatCNYEffective(0.0000014, 1_000_000, 0.6)).toBe('¥0.84')
    expect(formatCNYEffective(0.000003, 1_000_000, 0.6)).toBe('¥1.8')
  })

  it('formats original prices with their source currency without conversion', () => {
    expect(formatMoney(0.000003, 'USD', 1_000_000)).toBe('$3')
    expect(formatMoney(0.0000014, 'CNY', 1_000_000)).toBe('¥1.4')
  })

  it('computes discount directly from the effective group multiplier', () => {
    expect(computeDiscountFold(0.6)).toBe(6)
    expect(computeDiscountFold(2)).toBe(20)
    expect(computeDiscountPercent(0.6)).toBe(60)
    expect(formatDiscountFold(6)).toBe('6')
  })

  it('uses dash for missing values', () => {
    expect(formatCNYEffective(null, 1_000_000)).toBe('-')
  })

  it('normalizes invalid config values', () => {
    expect(normalizePlazaMultiplier(0)).toBe(1)
    expect(normalizePlazaMultiplier(Number.NaN)).toBe(1)
  })

  it('normalizes nullish billing multiplier arguments', () => {
    expect(formatCNYEffective(0.000015, 1_000_000)).toBe('¥15')
    expect(formatCNYEffective(0.000015, 1_000_000, undefined)).toBe('¥15')
    expect(formatCNYEffective(0.000015, 1_000_000, 0)).toBe('¥0')
  })

  it('formats tiny positive non-zero CNY prices without exchange conversion', () => {
    expect(formatCNYEffective(0.0000000001, 1_000_000)).toBe('¥0.0001')
  })

  it('scales the stored price to either schedule tier', () => {
    // 官方兜底表存高峰价：空闲 ×0.5。
    expect(scheduledScaledPrice(0.000003, 0.5)).toBeCloseTo(0.0000015)
    // 价格目录 / 管理端生效价存空闲价：高峰 ×2。
    expect(scheduledScaledPrice(0.000003, 2)).toBeCloseTo(0.000006)
    expect(scheduledScaledPrice(null, 0.5)).toBeNull()
    expect(scheduledScaledPrice(0.000003, undefined)).toBeNull()
  })
})
