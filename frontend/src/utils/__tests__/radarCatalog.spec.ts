import { describe, expect, it } from 'vitest'
import type { UserAvailableChannel } from '@/api/channels'
import { radarCatalogPlatforms } from '@/utils/radarCatalog'

function channels(modelsByPlatform: Record<string, string[]>): UserAvailableChannel[] {
  return [{
    name: 'Public catalog',
    description: '',
    platforms: Object.entries(modelsByPlatform).map(([platform, models]) => ({
      platform,
      groups: [],
      supported_models: models.map((name) => ({ name, platform, pricing: null })),
    })),
  }]
}

describe('radarCatalog', () => {
  it('derives unique non-empty model platforms for service health', () => {
    const catalog = channels({ OpenAI: [' GPT-5.4 ', 'gpt-5.4'], deepseek: ['deepseek-v4-pro'], empty: [] })

    expect(radarCatalogPlatforms(catalog)).toEqual(['deepseek', 'openai'])
  })
})
