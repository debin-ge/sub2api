import { describe, expect, it } from 'vitest'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_TOKEN,
  BILLING_MODE_VIDEO,
  getVideoBillingUnit,
  getVideoUnitPrice,
  getVideoUsageUnits,
  getDisplayBillingMode,
  isImageUsage,
  isVideoUsage,
} from '../billingMode'

describe('billingMode helpers', () => {
  it('prefers explicit video mode over image_count', () => {
    expect(
      getDisplayBillingMode({ image_count: 1, billing_mode: BILLING_MODE_VIDEO })
    ).toBe(BILLING_MODE_VIDEO)
    expect(isImageUsage({ image_count: 1, billing_mode: BILLING_MODE_VIDEO })).toBe(false)
  })

  it('infers image when image_count set and mode missing', () => {
    expect(getDisplayBillingMode({ image_count: 2, billing_mode: null })).toBe(BILLING_MODE_IMAGE)
  })

  it('keeps token mode even with image_count', () => {
    expect(
      getDisplayBillingMode({ image_count: 1, billing_mode: BILLING_MODE_TOKEN })
    ).toBe(BILLING_MODE_TOKEN)
  })

  it('recognizes video rows and calculates units for every video billing unit', () => {
    expect(isVideoUsage({ billing_mode: BILLING_MODE_VIDEO })).toBe(true)
    expect(getVideoUsageUnits({ video_billing_unit: 'video_token', output_tokens: 125_000 })).toBe(125_000)
    expect(getVideoUsageUnits({ video_billing_unit: 'second', video_count: 2, video_duration_seconds: 8 })).toBe(16)
    expect(getVideoUsageUnits({ video_billing_unit: 'request', video_count: 3 })).toBe(3)
    expect(getVideoBillingUnit({ billing_mode: BILLING_MODE_VIDEO, video_duration_seconds: 8 })).toBe('second')
    expect(getVideoUnitPrice({ video_units: 8, total_cost: 0.4 })).toBeCloseTo(0.05)
  })
})
