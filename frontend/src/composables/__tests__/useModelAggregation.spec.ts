import { describe, expect, it } from 'vitest'
import { aggregateByPlatformModel, sortAggregatedModels } from '@/composables/useModelAggregation'
import type { UserAvailableChannel, UserSupportedModelPricing } from '@/api/channels'

const price = (
  input: number | null,
  output: number | null = null,
  overrides: Partial<UserSupportedModelPricing> = {}
): UserSupportedModelPricing => ({
  billing_mode: 'token' as const,
  input_price: input,
  output_price: output,
  cache_write_price: null,
  cache_read_price: null,
  image_output_price: null,
  per_request_price: null,
  intervals: [],
  ...overrides
})

describe('aggregateByPlatformModel', () => {
  it('merges same platform and same model into one model card', () => {
    const rows: UserAvailableChannel[] = [
      {
        name: 'ch-a',
        description: 'primary channel',
        platforms: [{
          platform: 'anthropic',
          groups: [{ id: 1, name: 'g1', platform: 'anthropic', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
          supported_models: [{ name: 'claude-sonnet', platform: 'anthropic', pricing: price(0.000003) }]
        }]
      },
      {
        name: 'ch-b',
        description: 'backup channel',
        platforms: [{
          platform: 'anthropic',
          groups: [{ id: 2, name: 'g2', platform: 'anthropic', subscription_type: 'standard', rate_multiplier: 1.2, is_exclusive: false }],
          supported_models: [{ name: 'claude-sonnet', platform: 'anthropic', pricing: price(0.000002) }]
        }]
      }
    ]

    const result = aggregateByPlatformModel(rows)

    expect(result).toHaveLength(1)
    expect(result[0].platform).toBe('anthropic')
    expect(result[0].models).toHaveLength(1)
    expect(result[0].models[0].model).toBe('claude-sonnet')
    expect(result[0].models[0].displayName).toBe('claude-sonnet')
    expect(result[0].models[0].standardPricing?.minPricing.input).toBe(0.000002)
    expect(result[0].models[0].standardPricing?.minPricingRateMultipliers.input).toBe(1.2)
    expect(result[0].models[0].supportedGroups).toHaveLength(2)
    expect(result[0].models[0].standardPricing?.displayRateMultiplier).toBe(1.2)
    expect(result[0].models[0].vipPricing).toBeNull()
  })

  it('tracks the lowest valid group multiplier for discount and recharged price display', () => {
    const rows: UserAvailableChannel[] = [{
      name: 'ch',
      description: '',
      platforms: [{
        platform: 'anthropic',
        groups: [
          { id: 1, name: 'standard', platform: 'anthropic', subscription_type: 'standard', rate_multiplier: 1.2, is_exclusive: false },
          { id: 2, name: 'discount', platform: 'anthropic', subscription_type: 'standard', rate_multiplier: 0.8, is_exclusive: false }
        ],
        supported_models: [{ name: 'claude-sonnet', platform: 'anthropic', pricing: price(0.000003) }]
      }]
    }]

    const result = aggregateByPlatformModel(rows)

    expect(result[0].models[0].standardPricing?.displayRateMultiplier).toBe(0.8)
  })

  it('keeps the lowest multiplier when every public group is above one', () => {
    const rows: UserAvailableChannel[] = [{
      name: 'premium',
      description: '',
      platforms: [{
        platform: 'anthropic',
        groups: [
          { id: 1, name: 'premium-12', platform: 'anthropic', subscription_type: 'standard', rate_multiplier: 1.2, is_exclusive: false },
          { id: 2, name: 'premium-15', platform: 'anthropic', subscription_type: 'standard', rate_multiplier: 1.5, is_exclusive: false }
        ],
        supported_models: [{ name: 'claude-premium', platform: 'anthropic', pricing: price(0.000003) }]
      }]
    }]

    const model = aggregateByPlatformModel(rows)[0].models[0]

    expect(model.standardPricing?.displayRateMultiplier).toBe(1.2)
    expect(model.standardPricing?.minPricingRateMultipliers.input).toBe(1.2)
  })

  it('selects and sorts by the lowest effective model and group price pair', () => {
    const rows: UserAvailableChannel[] = [
      {
        name: 'cheap-raw-premium-group',
        description: '',
        platforms: [{
          platform: 'openai',
          groups: [{ id: 1, name: 'premium', platform: 'openai', subscription_type: 'standard', rate_multiplier: 2, is_exclusive: false }],
          supported_models: [
            { name: 'paired-model', platform: 'openai', pricing: price(0.000001) },
            { name: 'effective-expensive', platform: 'openai', pricing: price(0.000001) }
          ]
        }]
      },
      {
        name: 'higher-raw-standard-group',
        description: '',
        platforms: [{
          platform: 'openai',
          groups: [{ id: 2, name: 'standard', platform: 'openai', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
          supported_models: [
            { name: 'paired-model', platform: 'openai', pricing: price(0.0000015) },
            { name: 'effective-cheap', platform: 'openai', pricing: price(0.0000016) }
          ]
        }]
      }
    ]

    const models = aggregateByPlatformModel(rows, { sort: 'input_asc' })[0].models
    const paired = models.find((model) => model.model === 'paired-model')!

    expect(paired.standardPricing?.minPricing.input).toBe(0.0000015)
    expect(paired.standardPricing?.minPricingRateMultipliers.input).toBe(1)
    expect(models.map((model) => model.model)).toEqual([
      'paired-model',
      'effective-cheap',
      'effective-expensive'
    ])
  })

  it('does not merge same model name across different platforms', () => {
    const rows: UserAvailableChannel[] = [{
      name: 'ch',
      description: '',
      platforms: [
        {
          platform: 'anthropic',
          groups: [{ id: 1, name: 'ant', platform: 'anthropic', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
          supported_models: [{ name: 'shared-model', platform: 'anthropic', pricing: price(0.000003) }]
        },
        {
          platform: 'openai',
          groups: [{ id: 2, name: 'oa', platform: 'openai', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
          supported_models: [{ name: 'shared-model', platform: 'openai', pricing: price(0.000004) }]
        }
      ]
    }]

    const result = aggregateByPlatformModel(rows)

    expect(result).toHaveLength(2)
    expect(result.map((section) => section.platform)).toEqual(['anthropic', 'openai'])
    expect(result.flatMap((section) => section.models).map((model) => model.platform)).toEqual(['anthropic', 'openai'])
  })

  it('takes the minimum non-null price independently per dimension', () => {
    const rows: UserAvailableChannel[] = [
      {
        name: 'ch-a',
        description: '',
        platforms: [{
          platform: 'openai',
          groups: [{ id: 1, name: 'g1', platform: 'openai', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
          supported_models: [{
            name: 'gpt-image',
            platform: 'openai',
            pricing: price(0.000005, null, {
              cache_write_price: 0.000006,
              cache_read_price: 0.000001,
              image_output_price: 0.02,
              per_request_price: null
            })
          }]
        }]
      },
      {
        name: 'ch-b',
        description: '',
        platforms: [{
          platform: 'openai',
          groups: [{ id: 2, name: 'g2', platform: 'openai', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
          supported_models: [{
            name: 'gpt-image',
            platform: 'openai',
            pricing: price(null, 0.00001, {
              cache_write_price: 0.000004,
              cache_read_price: null,
              image_output_price: 0.03,
              per_request_price: 0.001
            })
          }]
        }]
      }
    ]

    const model = aggregateByPlatformModel(rows)[0].models[0]

    expect(model.standardPricing?.minPricing).toEqual({
      input: 0.000005,
      output: 0.00001,
      cacheWrite: 0.000004,
      cacheRead: 0.000001,
      imageOutput: 0.02,
      perRequest: 0.001
    })
  })

  it('compares image per-request offers with each group effective image multiplier', () => {
    const imagePrice = (perRequest: number): UserSupportedModelPricing => price(null, null, {
      billing_mode: 'image',
      per_request_price: perRequest
    })
    const rows: UserAvailableChannel[] = [
      {
        name: 'lower-raw-independent',
        description: '',
        platforms: [{
          platform: 'openai',
          groups: [{
            id: 1,
            name: 'independent',
            platform: 'openai',
            subscription_type: 'standard',
            rate_multiplier: 0.1,
            image_rate_independent: true,
            image_rate_multiplier: 2,
            is_exclusive: false
          }],
          supported_models: [{
            name: 'gpt-image',
            platform: 'openai',
            pricing: imagePrice(0.01)
          }]
        }]
      },
      {
        name: 'higher-raw-cheaper-effective',
        description: '',
        platforms: [{
          platform: 'openai',
          groups: [{
            id: 2,
            name: 'regular',
            platform: 'openai',
            subscription_type: 'standard',
            rate_multiplier: 1,
            image_rate_independent: false,
            image_rate_multiplier: 1,
            is_exclusive: false
          }],
          supported_models: [{
            name: 'gpt-image',
            platform: 'openai',
            pricing: imagePrice(0.015)
          }]
        }]
      }
    ]

    const model = aggregateByPlatformModel(rows)[0].models[0]

    // 0.01 × 2 = 0.02，高于 0.015 × 1；最低价必须选择后者。
    expect(model.standardPricing?.minPricing.perRequest).toBe(0.015)
    expect(model.standardPricing?.minPricingRateMultipliers.perRequest).toBe(1)
    expect(model.standardPricing?.displayRateMultiplier).toBe(1)
  })

  it('collects every public group with channel metadata and model pricing', () => {
    const channelPricing = price(0.000003)
    const rows: UserAvailableChannel[] = [{
      name: 'ch',
      description: 'public channel',
      platforms: [{
        platform: 'anthropic',
        groups: [
          { id: 1, name: 'g1', platform: 'anthropic', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false },
          { id: 2, name: 'g2', platform: 'anthropic', subscription_type: 'standard', rate_multiplier: 1.2, is_exclusive: false }
        ],
        supported_models: [{ name: 'claude-sonnet', platform: 'anthropic', pricing: channelPricing }]
      }]
    }]

    const groups = aggregateByPlatformModel(rows)[0].models[0].supportedGroups

    expect(groups).toHaveLength(2)
    expect(groups.map((entry) => entry.channelName)).toEqual(['ch', 'ch'])
    expect(groups.map((entry) => entry.channelDescription)).toEqual(['public channel', 'public channel'])
    expect(groups.map((entry) => entry.group.name)).toEqual(['g1', 'g2'])
    expect(groups.every((entry) => entry.pricing === channelPricing)).toBe(true)
  })

  it('sorts input price ascending and descending with null prices last', () => {
    const rows: UserAvailableChannel[] = [{
      name: 'ch',
      description: '',
      platforms: [{
        platform: 'openai',
        groups: [{ id: 1, name: 'g', platform: 'openai', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
        supported_models: [
          { name: 'expensive', platform: 'openai', pricing: price(0.00001) },
          { name: 'missing', platform: 'openai', pricing: null },
          { name: 'cheap', platform: 'openai', pricing: price(0.000001) }
        ]
      }]
    }]

    const ascending = aggregateByPlatformModel(rows, { sort: 'input_asc' })
    const descending = aggregateByPlatformModel(rows, { sort: 'input_desc' })

    expect(ascending[0].models.map((m) => m.model)).toEqual(['cheap', 'expensive', 'missing'])
    expect(descending[0].models.map((m) => m.model)).toEqual(['expensive', 'cheap', 'missing'])
  })

  it('uses interval-only pricing when computing minimum prices and input sorting', () => {
    const rows: UserAvailableChannel[] = [{
      name: 'ch',
      description: '',
      platforms: [{
        platform: 'openai',
        groups: [{ id: 1, name: 'g', platform: 'openai', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
        supported_models: [
          {
            name: 'tiered-cheap',
            platform: 'openai',
            pricing: price(null, null, {
              intervals: [
                {
                  start: 0,
                  end: 1000,
                  input_price: 0.000003,
                  output_price: 0.00002,
                  cache_write_price: 0.000004,
                  cache_read_price: 0.000001,
                  per_request_price: 0.002,
                },
                {
                  start: 1000,
                  end: null,
                  input_price: 0.000001,
                  output_price: 0.00001,
                  cache_write_price: null,
                  cache_read_price: 0.0000005,
                  per_request_price: 0.001,
                },
              ],
            }),
          },
          { name: 'flat-expensive', platform: 'openai', pricing: price(0.000002) },
        ],
      }],
    }]

    const ascending = aggregateByPlatformModel(rows, { sort: 'input_asc' })
    const tiered = ascending[0].models[0]

    expect(ascending[0].models.map((m) => m.model)).toEqual(['tiered-cheap', 'flat-expensive'])
    expect(tiered.standardPricing?.minPricing).toEqual({
      input: 0.000001,
      output: 0.00001,
      cacheWrite: 0.000004,
      cacheRead: 0.0000005,
      imageOutput: null,
      perRequest: 0.001,
    })
  })

  it('sorts aggregated models globally without platform grouping taking precedence', () => {
    const rows: UserAvailableChannel[] = [{
      name: 'ch',
      description: '',
      platforms: [
        {
          platform: 'anthropic',
          groups: [{ id: 1, name: 'ant', platform: 'anthropic', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
          supported_models: [{ name: 'z-last', platform: 'anthropic', pricing: price(0.00001) }]
        },
        {
          platform: 'openai',
          groups: [{ id: 2, name: 'oa', platform: 'openai', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
          supported_models: [{ name: 'a-first', platform: 'openai', pricing: price(0.000001) }]
        }
      ]
    }]

    const groupedModels = aggregateByPlatformModel(rows)
      .flatMap((section) => section.models)
    const globallySortedModels = sortAggregatedModels(groupedModels, 'input_asc')

    expect(globallySortedModels.map((model) => `${model.platform}:${model.model}`)).toEqual([
      'openai:a-first',
      'anthropic:z-last'
    ])
  })

  it('keeps standard and VIP pricing independent for the same model', () => {
    const rows: UserAvailableChannel[] = [{
      name: 'mixed-groups',
      description: '',
      platforms: [{
        platform: 'openai',
        groups: [
          {
            id: 1,
            name: 'standard',
            platform: 'openai',
            subscription_type: 'standard',
            rate_multiplier: 1.2,
            is_exclusive: false
          },
          {
            id: 2,
            name: 'vip',
            platform: 'openai',
            subscription_type: 'standard',
            rate_multiplier: 0.8,
            is_exclusive: false,
            vip_only: true
          }
        ],
        supported_models: [{ name: 'gpt-vip', platform: 'openai', pricing: price(0.000002, 0.000008) }]
      }]
    }]

    const model = aggregateByPlatformModel(rows)[0].models[0]

    expect(model.standardPricing).toMatchObject({
      minPricing: { input: 0.000002, output: 0.000008 },
      minPricingRateMultipliers: { input: 1.2, output: 1.2 },
      displayRateMultiplier: 1.2
    })
    expect(model.vipPricing).toMatchObject({
      minPricing: { input: 0.000002, output: 0.000008 },
      minPricingRateMultipliers: { input: 0.8, output: 0.8 },
      displayRateMultiplier: 0.8
    })
  })

  it('does not let a cheaper VIP offer overwrite the standard offer', () => {
    const rows: UserAvailableChannel[] = [
      {
        name: 'standard-channel',
        description: '',
        platforms: [{
          platform: 'openai',
          groups: [{
            id: 1,
            name: 'standard',
            platform: 'openai',
            subscription_type: 'standard',
            rate_multiplier: 1.2,
            is_exclusive: false
          }],
          supported_models: [{ name: 'gpt-vip', platform: 'openai', pricing: price(0.000002) }]
        }]
      },
      {
        name: 'vip-channel',
        description: '',
        platforms: [{
          platform: 'openai',
          groups: [{
            id: 2,
            name: 'vip',
            platform: 'openai',
            subscription_type: 'standard',
            rate_multiplier: 0.8,
            is_exclusive: false,
            vip_only: true
          }],
          supported_models: [{ name: 'gpt-vip', platform: 'openai', pricing: price(0.000001) }]
        }]
      }
    ]

    const model = aggregateByPlatformModel(rows)[0].models[0]

    expect(model.standardPricing?.minPricing.input).toBe(0.000002)
    expect(model.standardPricing?.minPricingRateMultipliers.input).toBe(1.2)
    expect(model.vipPricing?.minPricing.input).toBe(0.000001)
    expect(model.vipPricing?.minPricingRateMultipliers.input).toBe(0.8)
  })

  it('sorts by standard input pricing and falls back to VIP when standard pricing is absent', () => {
    const summary = (input: number, rate: number) => ({
      minPricing: {
        input,
        output: null,
        cacheWrite: null,
        cacheRead: null,
        imageOutput: null,
        perRequest: null
      },
      minPricingRateMultipliers: {
        input: rate,
        output: rate,
        cacheWrite: rate,
        cacheRead: rate,
        imageOutput: rate,
        perRequest: rate
      },
      displayRateMultiplier: rate
    })
    const base = {
      displayName: '',
      platform: 'openai',
      supportedGroups: [],
      recentCalls: 0,
      recentCallWindowSeconds: 0
    }
    const models = [
      {
        ...base,
        model: 'standard-first',
        displayName: 'standard-first',
        standardPricing: summary(0.000002, 1),
        vipPricing: summary(0.0000001, 0.5)
      },
      {
        ...base,
        model: 'vip-only',
        displayName: 'vip-only',
        standardPricing: null,
        vipPricing: summary(0.000003, 1)
      },
      {
        ...base,
        model: 'standard-second',
        displayName: 'standard-second',
        standardPricing: summary(0.000004, 1),
        vipPricing: summary(0.00000005, 0.5)
      }
    ]

    expect(sortAggregatedModels(models, 'input_asc').map((model) => model.model)).toEqual([
      'standard-first',
      'vip-only',
      'standard-second'
    ])
  })
})
