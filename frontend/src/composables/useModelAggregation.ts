import type {
  ModelPriceTimeSchedule,
  UserAvailableChannel,
  UserAvailableGroup,
  UserSupportedModelPricing
} from '@/api/channels'

export type PlazaSort = 'popularity' | 'default' | 'input_asc' | 'input_desc'

export interface PlazaMinPricing {
  input: number | null
  output: number | null
  cacheWrite: number | null
  cacheRead: number | null
  imageOutput: number | null
  perRequest: number | null
}

export interface PlazaPricingRateMultipliers {
  input: number
  output: number
  cacheWrite: number
  cacheRead: number
  imageOutput: number
  perRequest: number
}

export interface PlazaPricingSummary {
  minPricing: PlazaMinPricing
  minPricingRateMultipliers: PlazaPricingRateMultipliers
  displayRateMultiplier: number
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
  standardPricing: PlazaPricingSummary | null
  vipPricing: PlazaPricingSummary | null
  supportedGroups: PlazaSupportedGroup[]
  recentCalls: number
  recentCallWindowSeconds: number
  timeSchedule?: ModelPriceTimeSchedule
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

const defaultPricingRateMultipliers = (): PlazaPricingRateMultipliers => ({
  input: 1,
  output: 1,
  cacheWrite: 1,
  cacheRead: 1,
  imageOutput: 1,
  perRequest: 1
})

function minNullable(a: number | null, b: number | null): number | null {
  if (a == null) return b
  if (b == null) return a
  return Math.min(a, b)
}

function groupRateMultiplier(group: UserAvailableGroup): number {
  const rate = group.rate_multiplier
  return typeof rate === 'number' && Number.isFinite(rate) && rate > 0 ? rate : 1
}

function imageRateMultiplier(group: UserAvailableGroup): number {
  const rate = group.image_rate_multiplier
  return typeof rate === 'number' && Number.isFinite(rate) && rate >= 0 ? rate : 1
}

function pricingRateMultipliers(
  group: UserAvailableGroup,
  pricing: UserSupportedModelPricing | null
): PlazaPricingRateMultipliers {
  const rate = groupRateMultiplier(group)
  const rates: PlazaPricingRateMultipliers = {
    input: rate,
    output: rate,
    cacheWrite: rate,
    cacheRead: rate,
    imageOutput: rate,
    perRequest: rate
  }
  if (pricing?.billing_mode === 'image' && group.image_rate_independent === true) {
    rates.perRequest = imageRateMultiplier(group)
  }
  return rates
}

function extractMinPricing(pricing: UserSupportedModelPricing | null): PlazaMinPricing {
  if (!pricing) return emptyPricing()
  const minPricing = {
    input: pricing.input_price,
    output: pricing.output_price,
    cacheWrite: pricing.cache_write_price,
    cacheRead: pricing.cache_read_price,
    imageOutput: pricing.image_output_price,
    perRequest: pricing.per_request_price
  }

  for (const interval of pricing.intervals ?? []) {
    minPricing.input = minNullable(minPricing.input, interval.input_price)
    minPricing.output = minNullable(minPricing.output, interval.output_price)
    minPricing.cacheWrite = minNullable(minPricing.cacheWrite, interval.cache_write_price)
    minPricing.cacheRead = minNullable(minPricing.cacheRead, interval.cache_read_price)
    minPricing.perRequest = minNullable(minPricing.perRequest, interval.per_request_price)
  }

  return minPricing
}

function chooseEffectivePrice(
  currentPrice: number | null,
  currentRate: number,
  candidatePrice: number | null,
  candidateRate: number
): { price: number | null; rate: number } {
  if (candidatePrice == null) return { price: currentPrice, rate: currentRate }
  if (currentPrice == null || candidatePrice * candidateRate < currentPrice * currentRate) {
    return { price: candidatePrice, rate: candidateRate }
  }
  return { price: currentPrice, rate: currentRate }
}

function mergeEffectivePricing(
  currentPricing: PlazaMinPricing,
  currentRates: PlazaPricingRateMultipliers,
  candidatePricing: PlazaMinPricing,
  candidateRates: PlazaPricingRateMultipliers
): { pricing: PlazaMinPricing; rates: PlazaPricingRateMultipliers } {
  const input = chooseEffectivePrice(currentPricing.input, currentRates.input, candidatePricing.input, candidateRates.input)
  const output = chooseEffectivePrice(currentPricing.output, currentRates.output, candidatePricing.output, candidateRates.output)
  const cacheWrite = chooseEffectivePrice(currentPricing.cacheWrite, currentRates.cacheWrite, candidatePricing.cacheWrite, candidateRates.cacheWrite)
  const cacheRead = chooseEffectivePrice(currentPricing.cacheRead, currentRates.cacheRead, candidatePricing.cacheRead, candidateRates.cacheRead)
  const imageOutput = chooseEffectivePrice(currentPricing.imageOutput, currentRates.imageOutput, candidatePricing.imageOutput, candidateRates.imageOutput)
  const perRequest = chooseEffectivePrice(currentPricing.perRequest, currentRates.perRequest, candidatePricing.perRequest, candidateRates.perRequest)

  return {
    pricing: {
      input: input.price,
      output: output.price,
      cacheWrite: cacheWrite.price,
      cacheRead: cacheRead.price,
      imageOutput: imageOutput.price,
      perRequest: perRequest.price
    },
    rates: {
      input: input.rate,
      output: output.rate,
      cacheWrite: cacheWrite.rate,
      cacheRead: cacheRead.rate,
      imageOutput: imageOutput.rate,
      perRequest: perRequest.rate
    }
  }
}

function representativePricingRate(
  pricing: PlazaMinPricing,
  rates: PlazaPricingRateMultipliers
): number {
  const preferredKeys: Array<keyof PlazaMinPricing> = [
    'input',
    'perRequest',
    'output',
    'cacheRead',
    'cacheWrite',
    'imageOutput'
  ]
  for (const key of preferredKeys) {
    if (pricing[key] != null) return rates[key]
  }
  return 1
}

function mergePricingSummary(
  current: PlazaPricingSummary | null,
  candidatePricing: PlazaMinPricing,
  candidateRates: PlazaPricingRateMultipliers
): PlazaPricingSummary {
  const merged = mergeEffectivePricing(
    current?.minPricing ?? emptyPricing(),
    current?.minPricingRateMultipliers ?? defaultPricingRateMultipliers(),
    candidatePricing,
    candidateRates
  )
  return {
    minPricing: merged.pricing,
    minPricingRateMultipliers: merged.rates,
    displayRateMultiplier: representativePricingRate(merged.pricing, merged.rates)
  }
}

function effectiveInputPrice(model: AggregatedModel): number | null {
  const pricing = model.standardPricing?.minPricing.input != null
    ? model.standardPricing
    : model.vipPricing
  if (!pricing || pricing.minPricing.input == null) return null
  return pricing.minPricing.input * pricing.minPricingRateMultipliers.input
}

export function sortAggregatedModels(models: AggregatedModel[], sort: PlazaSort): AggregatedModel[] {
  const copy = [...models]
  if (sort === 'default') {
    return copy.sort((a, b) => a.displayName.localeCompare(b.displayName))
  }
  if (sort === 'popularity') {
    return copy.sort((a, b) => {
      if (a.recentCalls !== b.recentCalls) return b.recentCalls - a.recentCalls
      return a.displayName.localeCompare(b.displayName)
    })
  }

  return copy.sort((a, b) => {
    const av = effectiveInputPrice(a)
    const bv = effectiveInputPrice(b)
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
        const recentCalls = model.recent_call_count ?? 0
        const recentCallWindowSeconds = model.recent_call_window_seconds ?? 0
        const candidatePricing = extractMinPricing(model.pricing)
        let existing = byModel.get(model.name)

        if (!existing) {
          existing = {
            model: model.name,
            displayName: model.name,
            platform,
            standardPricing: null,
            vipPricing: null,
            supportedGroups: [],
            recentCalls,
            recentCallWindowSeconds,
            timeSchedule: model.time_schedule
          }
          byModel.set(model.name, existing)
        }
        if (model.time_schedule) {
          existing.timeSchedule = model.time_schedule
        }

        for (const group of platformSection.groups) {
          existing.supportedGroups.push({
            channelName: channel.name,
            channelDescription: channel.description,
            group,
            pricing: model.pricing
          })
          const pricingKey = group.vip_only === true ? 'vipPricing' : 'standardPricing'
          existing[pricingKey] = mergePricingSummary(
            existing[pricingKey],
            candidatePricing,
            pricingRateMultipliers(group, model.pricing)
          )
        }
        existing.recentCalls = Math.max(existing.recentCalls, recentCalls)
        if (recentCallWindowSeconds > existing.recentCallWindowSeconds) {
          existing.recentCallWindowSeconds = recentCallWindowSeconds
        }
      }
    }
  }

  return Array.from(byPlatform.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([platform, models]) => ({
      platform,
      models: sortAggregatedModels(Array.from(models.values()), options.sort ?? 'default')
    }))
    .filter((section) => section.models.length > 0)
}
