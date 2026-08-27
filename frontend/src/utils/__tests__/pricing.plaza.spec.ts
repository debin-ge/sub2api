import { describe, expect, it } from 'vitest'
import {
  computeValueBoost,
  computeDiscountFold,
  computeDiscountPercent,
  formatCNYEffective,
  formatCNYMarket,
  formatDiscountFold,
  formatScaled,
  normalizePlazaMultiplier,
  normalizePlazaRate,
  scheduledScaledPrice
} from '@/utils/pricing'

describe('plaza pricing helpers', () => {
  it('computes value boost from recharge multiplier and reference rate', () => {
    expect(computeValueBoost(1.5, 6.8)).toBe(10.2)
  })

  it('formats market CNY price from USD price, rate, and scale', () => {
    expect(formatCNYMarket(0.000015, 6.8, 1_000_000)).toBe('¥102')
  })

  it('rounds long decimal USD prices to at most six decimal places', () => {
    expect(formatScaled(0.123456789, 1)).toBe('$0.123457')
    expect(formatScaled(0.00000123456789, 1_000_000)).toBe('$1.234568')
    expect(formatScaled(0.5, 1)).toBe('$0.5')
    expect(formatScaled(0.000003, 1_000_000)).toBe('$3')
  })

  it('converts the USD model price with the configured exchange rate', () => {
    expect(formatCNYEffective(0.000015, 6.8, 1_000_000)).toBe('¥102')
  })

  it('includes the effective group billing multiplier in CNY prices', () => {
    expect(formatCNYEffective(0.0000014, 6.8, 1_000_000, 0.6)).toBe('¥5.712')
  })

  it('computes discount directly from the effective group multiplier', () => {
    expect(computeDiscountFold(0.6)).toBe(6)
    expect(computeDiscountFold(2)).toBe(20)
    expect(computeDiscountPercent(0.6)).toBe(60)
    expect(formatDiscountFold(6)).toBe('6')
  })

  it('uses dash for missing values', () => {
    expect(formatCNYMarket(null, 6.8, 1_000_000)).toBe('-')
    expect(formatCNYEffective(null, 6.8, 1_000_000)).toBe('-')
  })

  it('normalizes invalid config values', () => {
    expect(normalizePlazaMultiplier(0)).toBe(1)
    expect(normalizePlazaMultiplier(Number.NaN)).toBe(1)
    expect(normalizePlazaRate(0)).toBe(6.8)
    expect(normalizePlazaRate(Number.POSITIVE_INFINITY)).toBe(6.8)
  })

  it('normalizes nullish config arguments in formatting and boost helpers', () => {
    expect(formatCNYMarket(0.000015, undefined, 1_000_000)).toBe('¥102')
    expect(formatCNYMarket(0.000015, null, 1_000_000)).toBe('¥102')
    expect(formatCNYEffective(0.000015, undefined, 1_000_000)).toBe('¥102')
    expect(formatCNYEffective(0.000015, null, 1_000_000)).toBe('¥102')
    expect(computeValueBoost(undefined, undefined)).toBe(6.8)
    expect(computeValueBoost(null, null)).toBe(6.8)
  })

  it('formats tiny positive non-zero CNY prices below the cent threshold', () => {
    expect(formatCNYMarket(0.0000000001, 6.8, 1_000_000)).toBe('<¥0.01')
    expect(formatCNYEffective(0.0000000001, 6.8, 1_000_000)).toBe('<¥0.01')
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
