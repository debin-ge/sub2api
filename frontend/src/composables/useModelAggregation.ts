import type {
  UserAvailableChannel,
  UserAvailableGroup,
  UserSupportedModelPricing
} from '@/api/channels'

export type PlazaSort = 'default' | 'input_asc' | 'input_desc'

export interface PlazaMinPricing {
  input: number | null
  output: number | null
  cacheWrite: number | null
  cacheRead: number | null
  imageOutput: number | null
  perRequest: number | null
}

export interface PlazaSupportedGroup {
  channelName: string
  channelDescription: string
  group: UserAvailableGroup
  pricing: UserSupportedModelPricing | null
}

export interface AggregatedModel {
  model: string
  displayName: string
  platform: string
  minPricing: PlazaMinPricing
  supportedGroups: PlazaSupportedGroup[]
}

export interface PlazaPlatformSection {
  platform: string
  models: AggregatedModel[]
}

const emptyPricing = (): PlazaMinPricing => ({
  input: null,
  output: null,
  cacheWrite: null,
  cacheRead: null,
  imageOutput: null,
  perRequest: null
})

function minNullable(a: number | null, b: number | null): number | null {
  if (a == null) return b
  if (b == null) return a
  return Math.min(a, b)
}

function extractMinPricing(pricing: UserSupportedModelPricing | null): PlazaMinPricing {
  if (!pricing) return emptyPricing()
  return {
    input: pricing.input_price,
    output: pricing.output_price,
    cacheWrite: pricing.cache_write_price,
    cacheRead: pricing.cache_read_price,
    imageOutput: pricing.image_output_price,
    perRequest: pricing.per_request_price
  }
}

function mergeMinPricing(a: PlazaMinPricing, b: PlazaMinPricing): PlazaMinPricing {
  return {
    input: minNullable(a.input, b.input),
    output: minNullable(a.output, b.output),
    cacheWrite: minNullable(a.cacheWrite, b.cacheWrite),
    cacheRead: minNullable(a.cacheRead, b.cacheRead),
    imageOutput: minNullable(a.imageOutput, b.imageOutput),
    perRequest: minNullable(a.perRequest, b.perRequest)
  }
}

function sortModels(models: AggregatedModel[], sort: PlazaSort): AggregatedModel[] {
  const copy = [...models]
  if (sort === 'default') {
    return copy.sort((a, b) => a.displayName.localeCompare(b.displayName))
  }

  return copy.sort((a, b) => {
    const av = a.minPricing.input
    const bv = b.minPricing.input
    if (av == null && bv == null) return a.displayName.localeCompare(b.displayName)
    if (av == null) return 1
    if (bv == null) return -1
    if (av === bv) return a.displayName.localeCompare(b.displayName)
    return sort === 'input_asc' ? av - bv : bv - av
  })
}

export function aggregateByPlatformModel(
  channels: UserAvailableChannel[],
  options: { sort?: PlazaSort } = {}
): PlazaPlatformSection[] {
  const byPlatform = new Map<string, Map<string, AggregatedModel>>()

  for (const channel of channels) {
    for (const platformSection of channel.platforms) {
      const platform = platformSection.platform
      let byModel = byPlatform.get(platform)
      if (!byModel) {
        byModel = new Map<string, AggregatedModel>()
        byPlatform.set(platform, byModel)
      }

      for (const model of platformSection.supported_models) {
        const supportedGroups = platformSection.groups.map((group) => ({
          channelName: channel.name,
          channelDescription: channel.description,
          group,
          pricing: model.pricing
        }))
        const existing = byModel.get(model.name)

        if (!existing) {
          byModel.set(model.name, {
            model: model.name,
            displayName: model.name,
            platform,
            minPricing: extractMinPricing(model.pricing),
            supportedGroups
          })
          continue
        }

        existing.minPricing = mergeMinPricing(existing.minPricing, extractMinPricing(model.pricing))
        existing.supportedGroups.push(...supportedGroups)
      }
    }
  }

  return Array.from(byPlatform.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([platform, models]) => ({
      platform,
      models: sortModels(Array.from(models.values()), options.sort ?? 'default')
    }))
    .filter((section) => section.models.length > 0)
}
