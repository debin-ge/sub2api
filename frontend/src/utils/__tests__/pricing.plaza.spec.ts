import { describe, expect, it } from 'vitest'
import {
  computeValueBoost,
  formatCNYMarket,
  formatCNYRecharged,
  normalizePlazaMultiplier,
  normalizePlazaRate
} from '@/utils/pricing'

describe('plaza pricing helpers', () => {
  it('computes value boost from recharge multiplier and reference rate', () => {
    expect(computeValueBoost(1.5, 6.8)).toBe(10.2)
  })

  it('formats market CNY price from USD price, rate, and scale', () => {
    expect(formatCNYMarket(0.000015, 6.8, 1_000_000)).toBe('¥102')
  })

  it('formats recharged CNY price from USD price, multiplier, and scale', () => {
    expect(formatCNYRecharged(0.000015, 1.5, 1_000_000)).toBe('¥10')
  })

  it('uses dash for missing values', () => {
    expect(formatCNYMarket(null, 6.8, 1_000_000)).toBe('-')
    expect(formatCNYRecharged(null, 1.5, 1_000_000)).toBe('-')
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
    expect(formatCNYRecharged(0.000015, undefined, 1_000_000)).toBe('¥15')
    expect(formatCNYRecharged(0.000015, null, 1_000_000)).toBe('¥15')
    expect(computeValueBoost(undefined, undefined)).toBe(6.8)
    expect(computeValueBoost(null, null)).toBe(6.8)
  })

  it('formats tiny positive non-zero CNY prices below the cent threshold', () => {
    expect(formatCNYMarket(0.0000000001, 6.8, 1_000_000)).toBe('<¥0.01')
    expect(formatCNYRecharged(0.0000000001, 1.5, 1_000_000)).toBe('<¥0.01')
  })
})
