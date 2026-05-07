import { describe, expect, it } from 'vitest'

import { formatBalanceAmount } from '../formatters'

describe('formatBalanceAmount', () => {
  it('keeps cents for whole-cent balances', () => {
    expect(formatBalanceAmount(12)).toBe('12.00')
    expect(formatBalanceAmount(12.5)).toBe('12.50')
  })

  it('caps sub-cent balance display at four decimal places', () => {
    expect(formatBalanceAmount(99.999877)).toBe('99.9999')
    expect(formatBalanceAmount(10.004321)).toBe('10.0043')
  })

  it('keeps at least two decimals while trimming redundant trailing zeroes', () => {
    expect(formatBalanceAmount(10.120000)).toBe('10.12')
    expect(formatBalanceAmount(0.000123)).toBe('0.0001')
  })
})
