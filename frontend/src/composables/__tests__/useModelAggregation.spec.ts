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
    expect(result[0].models[0].timeSchedule).toBeUndefined()
    expect(result[0].models[0].standardPricing?.minPricing.input).toBe(0.000002)
    expect(result[0].models[0].standardPricing?.minPricingRateMultipliers.input).toBe(1.2)
    expect(result[0].models[0].supportedGroups).toHaveLength(2)
    expect(result[0].models[0].standardPricing?.displayRateMultiplier).toBe(1.2)
    expect(result[0].models[0].vipPricing).toBeNull()
  })

  it('keeps DeepSeek official time schedule on the aggregated model', () => {
    const schedule = {
      kind: 'deepseek_official',
      timezone: 'Asia/Shanghai',
      peak_windows: ['09:00-12:00', '14:00-18:00'],
      peak_multiplier: 2,
      off_peak_multiplier: 1
    }
    const rows: UserAvailableChannel[] = [{
      name: 'ch',
      description: '',
      platforms: [{
        platform: 'deepseek',
        groups: [{ id: 1, name: 'g1', platform: 'deepseek', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
        supported_models: [{
          name: 'deepseek-v4-flash',
          platform: 'deepseek',
          pricing: price(0.000003, 0.000009),
          time_schedule: schedule
        }]
      }]
    }]

    const result = aggregateByPlatformModel(rows)
    expect(result[0].models[0].timeSchedule).toEqual(schedule)
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
      perRequest: 0.001,
      imageInput: null,
      image1K: null,
      image2K: null,
      image4K: null,
      video480p: null,
      video720p: null,
      video1080p: null
    })
  })

  it('keeps the currency and source of the selected minimum for each dimension', () => {
    const rows: UserAvailableChannel[] = [
      {
        name: 'cny-catalog',
        description: '',
        platforms: [{
          platform: 'zhipu',
          groups: [{ id: 1, name: 'cny', platform: 'zhipu', subscription_type: 'standard', rate_multiplier: 0.5, is_exclusive: false }],
          supported_models: [{
            name: 'glm-5.1',
            platform: 'zhipu',
            pricing: price(0.0000014, 0.00001, { currency: 'CNY', source: 'catalog' })
          }]
        }]
      },
      {
        name: 'usd-channel',
        description: '',
        platforms: [{
          platform: 'zhipu',
          groups: [{ id: 2, name: 'usd', platform: 'zhipu', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
          supported_models: [{
            name: 'glm-5.1',
            platform: 'zhipu',
            pricing: price(0.000002, 0.000004, { currency: 'USD', source: 'channel' })
          }]
        }]
      }
    ]

    const summary = aggregateByPlatformModel(rows)[0].models[0].standardPricing

    // input: ¥1.4 × 0.5 = ¥0.7；output: $4 × 1 = ¥4。
    expect(summary?.minPricing.input).toBe(0.0000014)
    expect(summary?.minPricingRateMultipliers.input).toBe(0.5)
    expect(summary?.minPricingCurrencies?.input).toBe('CNY')
    expect(summary?.minPricingSources?.input).toBe('catalog')
    expect(summary?.minPricing.output).toBe(0.000004)
    expect(summary?.minPricingRateMultipliers.output).toBe(1)
    expect(summary?.minPricingCurrencies?.output).toBe('USD')
    expect(summary?.minPricingSources?.output).toBe('channel')
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

  it('classifies video models and applies the independent video multiplier to per-second tiers', () => {
    const rows: UserAvailableChannel[] = [{
      name: 'video',
      description: '',
      platforms: [{
        platform: 'grok',
        groups: [{
          id: 1,
          name: 'video-group',
          platform: 'grok',
          subscription_type: 'standard',
          rate_multiplier: 1,
          video_rate_independent: true,
          video_rate_multiplier: 0.5,
          is_exclusive: false
        }],
        supported_models: [{
          name: 'grok-imagine-video',
          platform: 'grok',
          pricing: price(null, null, {
            billing_mode: 'video',
            video_tier_prices: [
              { tier: '480p', price: 0.05 },
              { tier: '720p', price: 0.1 }
            ]
          })
        }]
      }]
    }]

    const model = aggregateByPlatformModel(rows)[0].models[0]

    expect(model.billingKind).toBe('video')
    expect(model.standardPricing?.minPricing.video480p).toBe(0.05)
    expect(model.standardPricing?.minPricing.video720p).toBe(0.1)
    expect(model.standardPricing?.minPricing.video1080p).toBeNull()
    expect(model.standardPricing?.minPricingRateMultipliers.video720p).toBe(0.5)
  })

  it('keeps video tier prices in USD even when the model token pricing is CNY', () => {
    const rows: UserAvailableChannel[] = [{
      name: 'seedance',
      description: '',
      platforms: [{
        platform: 'openai',
        groups: [{
          id: 1,
          name: 'video-group',
          platform: 'openai',
          subscription_type: 'standard',
          rate_multiplier: 1,
          is_exclusive: false
        }],
        supported_models: [{
          name: 'doubao-seedance-2.0-mini-480p',
          platform: 'openai',
          pricing: price(2, null, {
            billing_mode: 'video',
            currency: 'CNY',
            video_tier_prices: [{ tier: '480p', price: 0.00972 }]
          })
        }]
      }]
    }]

    const summary = aggregateByPlatformModel(rows)[0].models[0].standardPricing

    expect(summary?.minPricing.video480p).toBe(0.00972)
    expect(summary?.minPricingCurrencies?.video480p).toBe('USD')
    expect(summary?.minPricingCurrencies?.input).toBe('CNY')
  })

  it('classifies image models with per-image tiers and token prices', () => {
    const rows: UserAvailableChannel[] = [{
      name: 'image',
      description: '',
      platforms: [{
        platform: 'openai',
        groups: [{
          id: 1,
          name: 'image-group',
          platform: 'openai',
          subscription_type: 'standard',
          rate_multiplier: 0.8,
          image_rate_independent: true,
          image_rate_multiplier: 2,
          is_exclusive: false
        }],
        supported_models: [{
          name: 'gpt-image-2',
          platform: 'openai',
          pricing: price(0.000005, null, {
            image_input_price: 0.00001,
            image_output_price: 0.00004,
            image_tier_prices: [
              { tier: '1K', price: 0.04 },
              { tier: '2K', price: 0.06 },
              { tier: '4K', price: 0.08 }
            ]
          })
        }]
      }]
    }]

    const model = aggregateByPlatformModel(rows)[0].models[0]
    const summary = model.standardPricing

    expect(model.billingKind).toBe('image')
    expect(summary?.minPricing.image2K).toBe(0.06)
    expect(summary?.minPricing.imageInput).toBe(0.00001)
    expect(summary?.minPricingRateMultipliers.image4K).toBe(2)
    expect(summary?.minPricingRateMultipliers.imageOutput).toBe(0.8)
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
          { name: 'missing', platform: 'openai', pricing: price(null, null, { billing_mode: 'per_request', per_request_price: 0.02 }) },
          { name: 'cheap', platform: 'openai', pricing: price(0.000001) }
        ]
      }]
    }]

    const ascending = aggregateByPlatformModel(rows, { sort: 'input_asc' })
    const descending = aggregateByPlatformModel(rows, { sort: 'input_desc' })

    expect(ascending[0].models.map((m) => m.model)).toEqual(['cheap', 'expensive', 'missing'])
    expect(descending[0].models.map((m) => m.model)).toEqual(['expensive', 'cheap', 'missing'])
  })

  it('hides model offers without any price because the gateway rejects them', () => {
    const rows: UserAvailableChannel[] = [{
      name: 'ch',
      description: '',
      platforms: [{
        platform: 'openai',
        groups: [{ id: 1, name: 'priced', platform: 'openai', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
        supported_models: [
          { name: 'priced', platform: 'openai', pricing: price(0.000001) },
          { name: 'free', platform: 'openai', pricing: price(0, 0) },
          { name: 'no-pricing', platform: 'openai', pricing: null },
          { name: 'empty-pricing', platform: 'openai', pricing: price(null, null) },
          { name: 'partial', platform: 'openai', pricing: price(0.000002) }
        ]
      }, {
        platform: 'openai',
        groups: [{ id: 2, name: 'unpriced', platform: 'openai', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false }],
        supported_models: [
          { name: 'partial', platform: 'openai', pricing: null }
        ]
      }]
    }]

    const models = aggregateByPlatformModel(rows)[0].models
    const names = models.map((m) => m.model).sort()

    expect(names).toEqual(['free', 'partial', 'priced'])
    const partial = models.find((m) => m.model === 'partial')
    expect(partial?.supportedGroups.map((g) => g.group.name)).toEqual(['priced'])
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
      imageInput: null,
      image1K: null,
      image2K: null,
      image4K: null,
      video480p: null,
      video720p: null,
      video1080p: null
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
