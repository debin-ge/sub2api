import { describe, expect, it } from 'vitest'

import type { VideoPricingConfig } from '@/api/admin/modelPrices'
import {
  buildSimpleVideoPricingConfig,
  parseSimpleVideoPricingConfig,
  suggestSimpleVideoPricingProfile,
  type SimpleVideoPricingProfile,
} from '../simpleVideoPricing'

describe('simple video pricing profiles', () => {
  it('round-trips independent standard and reference-video prices', () => {
    const profile: SimpleVideoPricingProfile = {
      billingUnit: 'video_token',
      standardEnabled: true,
      standardUnitPriceUsd: 13.34e-6,
      referenceEnabled: true,
      referenceSameAsStandard: false,
      referenceUnitPriceUsd: 7.7e-6,
      tokensPerSecond: 48_600,
    }

    const config = buildSimpleVideoPricingConfig(profile)

    expect(parseSimpleVideoPricingConfig(config)).toEqual(profile)
    expect(config.rules).toEqual([
      expect.objectContaining({ key: 'standard', conditions: expect.objectContaining({ input_has_video: false }) }),
      expect.objectContaining({ key: 'reference-video', conditions: expect.objectContaining({ input_has_video: true }) }),
    ])
  })

  it('supports a reference-video-only simple profile', () => {
    const profile: SimpleVideoPricingProfile = {
      billingUnit: 'video_token',
      standardEnabled: false,
      standardUnitPriceUsd: 0,
      referenceEnabled: true,
      referenceSameAsStandard: false,
      referenceUnitPriceUsd: 23.25e-6,
      tokensPerSecond: 48_600,
    }

    expect(parseSimpleVideoPricingConfig(buildSimpleVideoPricingConfig(profile))).toEqual(profile)
  })

  it('keeps profiles with advanced quality conditions out of simple mode', () => {
    const config = buildSimpleVideoPricingConfig({
      billingUnit: 'video_token', standardEnabled: false, standardUnitPriceUsd: 0,
      referenceEnabled: true, referenceSameAsStandard: false, referenceUnitPriceUsd: 23.25e-6,
      tokensPerSecond: 48_600,
    })
    config.rules![0].conditions!.qualities = ['standard']

    expect(parseSimpleVideoPricingConfig(config)).toBeNull()
  })

  it('suggests a safe explicit conversion without carrying advanced conditions', () => {
    const config: VideoPricingConfig = {
      version: 1,
      enabled: true,
      currency: 'USD',
      defaults: { request_mode: 'standard', inference_mode: 'online', generate_audio: true },
      estimators: { output: { type: 'fixed_tokens_per_second', tokens_per_second: 48_600 } },
      rules: [{
        key: 'custom', billing_unit: 'video_token', unit_price_usd: 23.25e-6, estimator: 'output',
        conditions: { operations: ['generate'], input_has_video: true, qualities: ['standard'], generate_audio: true },
      }],
    }

    const suggested = suggestSimpleVideoPricingProfile(config)
    const converted = buildSimpleVideoPricingConfig(suggested)

    expect(suggested.standardEnabled).toBe(false)
    expect(suggested.referenceEnabled).toBe(true)
    expect(suggested.tokensPerSecond).toBe(48_600)
    expect(converted.rules![0].conditions).toEqual({
      operations: ['generate'], input_has_video: true,
      request_modes: ['standard'], inference_modes: ['online'],
    })
  })
})
