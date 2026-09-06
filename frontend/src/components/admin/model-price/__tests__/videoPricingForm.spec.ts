import { describe, expect, it } from 'vitest'

import type { VideoPricingConfig } from '@/api/admin/modelPrices'
import { prepareVideoPricingForSave, setUnconditionalMinimumUnits, unconditionalMinimumUnits } from '../videoPricingForm'

function perSecondConfig(): VideoPricingConfig {
  return {
    version: 1,
    enabled: true,
    currency: 'USD',
    rules: [{ key: 'default', billing_unit: 'second', unit_price_usd: 0.1 }],
  }
}

describe('prepareVideoPricingForSave', () => {
  it('drops unfinished optional rows that no rule references', () => {
    const config = perSecondConfig()
    config.resolutions = { 'resolution-1': { sizes: [] } }
    config.estimators = {
      draft: { type: 'fixed_tokens_per_second' },
    }

    expect(prepareVideoPricingForSave(config)).toEqual(perSecondConfig())
    expect(config.resolutions).toEqual({ 'resolution-1': { sizes: [] } })
  })

  it('keeps unfinished rows when a rule references them so validation can explain the error', () => {
    const config = perSecondConfig()
    config.resolutions = { '480p': { sizes: [] } }
    config.rules![0].conditions = { resolutions: ['480p'] }

    expect(prepareVideoPricingForSave(config).resolutions).toEqual({ '480p': { sizes: [] } })
  })

  it('keeps complete token estimators and token prices unchanged', () => {
    const config: VideoPricingConfig = {
      version: 1,
      enabled: true,
      currency: 'USD',
      estimators: { output: { type: 'fixed_tokens_per_second', tokens_per_second: 9720 } },
      rules: [{
        key: 'token',
        billing_unit: 'video_token',
        unit_price_usd: 0.00001334,
        estimator: 'output',
      }],
    }

    expect(prepareVideoPricingForSave(config)).toEqual(config)
  })
})

describe('minimum unit editing', () => {
	it('updates only the unconditional rule and preserves conditional rules', () => {
		const estimator = {
			type: 'fixed_tokens_per_second' as const,
			tokens_per_second: 100,
			minimum_units: [
				{ units: 5, conditions: { operations: ['edit'] } },
				{ units: 2 },
				{ units: 8, conditions: { resolutions: ['1080p'] } },
			],
		}

		const updated = setUnconditionalMinimumUnits(estimator, 3)

		expect(unconditionalMinimumUnits(updated)).toBe(3)
		expect(updated.tokens_per_second).toBe(100)
		expect(updated.minimum_units).toEqual([
			{ units: 5, conditions: { operations: ['edit'] } },
			{ units: 3 },
			{ units: 8, conditions: { resolutions: ['1080p'] } },
		])
		expect(estimator.minimum_units[1].units).toBe(2)
	})
})
