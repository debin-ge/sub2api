import { describe, expect, it } from 'vitest'
import { aggregateByPlatformModel } from '@/composables/useModelAggregation'
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
    expect(result[0].models[0].minPricing.input).toBe(0.000002)
    expect(result[0].models[0].supportedGroups).toHaveLength(2)
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

    expect(model.minPricing).toEqual({
      input: 0.000005,
      output: 0.00001,
      cacheWrite: 0.000004,
      cacheRead: 0.000001,
      imageOutput: 0.02,
      perRequest: 0.001
    })
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
})
