import type {
  ModelPriceTimeSchedule,
  UserAvailableChannel,
  UserAvailableGroup,
  UserSupportedModelPricing
} from '@/api/channels'

export type PlazaSort = 'popularity' | 'default' | 'input_asc' | 'input_desc'

/** 模型的主计费形态，决定卡片样式与计费类型筛选。 */
export type PlazaBillingKind = 'text' | 'image' | 'video' | 'per_request'

export const PLAZA_PRICING_DIMENSIONS = [
  'input',
  'output',
  'cacheWrite',
  'cacheRead',
  'imageInput',
  'imageOutput',
  'perRequest',
  'image1K',
  'image2K',
  'image4K',
  'video480p',
  'video720p',
  'video1080p'
] as const

export type PlazaPricingDimension = (typeof PLAZA_PRICING_DIMENSIONS)[number]
export type PlazaMinPricing = Record<PlazaPricingDimension, number | null>
export type PlazaPricingRateMultipliers = Record<PlazaPricingDimension, number>
export type PlazaPricingCurrency = 'USD' | 'CNY'
export type PlazaPricingSource = 'override' | 'merged' | 'catalog' | 'official' | 'channel'

type PlazaPricingDimensionMap<T> = Record<PlazaPricingDimension, T>

/** 图片按张档位（后端 tier 值）→ 聚合维度。 */
export const PLAZA_IMAGE_TIER_DIMENSIONS = {
  '1K': 'image1K',
  '2K': 'image2K',
  '4K': 'image4K'
} as const satisfies Record<string, PlazaPricingDimension>

/** 视频按秒档位（后端 tier 值）→ 聚合维度。 */
export const PLAZA_VIDEO_TIER_DIMENSIONS = {
  '480p': 'video480p',
  '720p': 'video720p',
  '1080p': 'video1080p'
} as const satisfies Record<string, PlazaPricingDimension>

export interface PlazaPricingSummary {
  minPricing: PlazaMinPricing
  minPricingRateMultipliers: PlazaPricingRateMultipliers
  minPricingCurrencies?: PlazaPricingDimensionMap<PlazaPricingCurrency>
  minPricingSources?: PlazaPricingDimensionMap<PlazaPricingSource>
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
  billingKind: PlazaBillingKind
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

function dimensionMap<T>(value: T): PlazaPricingDimensionMap<T> {
  const out = {} as PlazaPricingDimensionMap<T>
  for (const key of PLAZA_PRICING_DIMENSIONS) out[key] = value
  return out
}

const emptyPricing = (): PlazaMinPricing => dimensionMap<number | null>(null)
const defaultPricingRateMultipliers = (): PlazaPricingRateMultipliers => dimensionMap(1)
const defaultPricingCurrencies = () => dimensionMap<PlazaPricingCurrency>('USD')
const defaultPricingSources = () => dimensionMap<PlazaPricingSource>('channel')

function minNullable(a: number | null, b: number | null): number | null {
  if (a == null) return b
  if (b == null) return a
  return Math.min(a, b)
}

function groupRateMultiplier(group: UserAvailableGroup): number {
  const rate = group.rate_multiplier
  return typeof rate === 'number' && Number.isFinite(rate) && rate > 0 ? rate : 1
}

function independentRate(value: number | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : 1
}

/** 图片按张结算使用的倍率：分组开启独立图片倍率时用图片倍率，否则用分组倍率。 */
export function imageTierRateMultiplier(group: UserAvailableGroup): number {
  return group.image_rate_independent === true
    ? independentRate(group.image_rate_multiplier)
    : groupRateMultiplier(group)
}

/** 视频按秒结算使用的倍率：分组开启独立视频倍率时用视频倍率，否则用分组倍率。 */
export function videoTierRateMultiplier(group: UserAvailableGroup): number {
  return group.video_rate_independent === true
    ? independentRate(group.video_rate_multiplier)
    : groupRateMultiplier(group)
}

function pricingRateMultipliers(
  group: UserAvailableGroup,
  pricing: UserSupportedModelPricing | null
): PlazaPricingRateMultipliers {
  const rates = dimensionMap(groupRateMultiplier(group))
  const imageRate = imageTierRateMultiplier(group)
  const videoRate = videoTierRateMultiplier(group)
  if (pricing?.billing_mode === 'image') rates.perRequest = imageRate
  if (pricing?.billing_mode === 'video') rates.perRequest = videoRate
  for (const key of Object.values(PLAZA_IMAGE_TIER_DIMENSIONS)) rates[key] = imageRate
  for (const key of Object.values(PLAZA_VIDEO_TIER_DIMENSIONS)) rates[key] = videoRate
  return rates
}

function hasMediaTiers(pricing: UserSupportedModelPricing): boolean {
  return (pricing.image_tier_prices?.length ?? 0) > 0 || (pricing.video_tier_prices?.length ?? 0) > 0
}

function extractMinPricing(pricing: UserSupportedModelPricing | null): PlazaMinPricing {
  const minPricing = emptyPricing()
  if (!pricing) return minPricing
  minPricing.input = pricing.input_price
  minPricing.output = pricing.output_price
  minPricing.cacheWrite = pricing.cache_write_price
  minPricing.cacheRead = pricing.cache_read_price
  minPricing.imageInput = pricing.image_input_price ?? null
  minPricing.imageOutput = pricing.image_output_price
  minPricing.perRequest = pricing.per_request_price

  // 媒体档位已单独给出时，带档位标签的区间不再压成一个「按次最低价」。
  const skipTierIntervals = hasMediaTiers(pricing)
  for (const interval of pricing.intervals ?? []) {
    minPricing.input = minNullable(minPricing.input, interval.input_price)
    minPricing.output = minNullable(minPricing.output, interval.output_price)
    minPricing.cacheWrite = minNullable(minPricing.cacheWrite, interval.cache_write_price)
    minPricing.cacheRead = minNullable(minPricing.cacheRead, interval.cache_read_price)
    if (!(skipTierIntervals && interval.tier_label)) {
      minPricing.perRequest = minNullable(minPricing.perRequest, interval.per_request_price)
    }
  }

  const tierDimensions: Record<string, PlazaPricingDimension> = {
    ...PLAZA_IMAGE_TIER_DIMENSIONS,
    ...PLAZA_VIDEO_TIER_DIMENSIONS
  }
  for (const tier of [...(pricing.image_tier_prices ?? []), ...(pricing.video_tier_prices ?? [])]) {
    const key = tierDimensions[tier.tier]
    if (key && typeof tier.price === 'number' && Number.isFinite(tier.price)) {
      minPricing[key] = minNullable(minPricing[key], tier.price)
    }
  }

  return minPricing
}

function hasAnyPrice(pricing: PlazaMinPricing): boolean {
  return PLAZA_PRICING_DIMENSIONS.some((key) => pricing[key] != null)
}

/** 单条定价的计费形态：视频 > 图片 > 按次 > 文本。 */
export function classifyPricing(pricing: UserSupportedModelPricing | null): PlazaBillingKind {
  if (!pricing) return 'text'
  if (pricing.billing_mode === 'video' || (pricing.video_tier_prices?.length ?? 0) > 0) return 'video'
  if (
    pricing.billing_mode === 'image' ||
    (pricing.image_tier_prices?.length ?? 0) > 0 ||
    pricing.image_output_price != null
  ) {
    return 'image'
  }
  if (pricing.billing_mode === 'per_request') return 'per_request'
  return 'text'
}

const BILLING_KIND_RANK: Record<PlazaBillingKind, number> = {
  text: 0,
  per_request: 1,
  image: 2,
  video: 3
}

function strongerBillingKind(a: PlazaBillingKind, b: PlazaBillingKind): PlazaBillingKind {
  return BILLING_KIND_RANK[b] > BILLING_KIND_RANK[a] ? b : a
}

function chooseEffectivePrice(
  currentPrice: number | null,
  currentRate: number,
  candidatePrice: number | null,
  candidateRate: number
): { price: number | null; rate: number; selected: boolean } {
  if (candidatePrice == null) return { price: currentPrice, rate: currentRate, selected: false }
  if (currentPrice == null || candidatePrice * candidateRate < currentPrice * currentRate) {
    return { price: candidatePrice, rate: candidateRate, selected: true }
  }
  return { price: currentPrice, rate: currentRate, selected: false }
}

function mergeEffectivePricing(
  currentPricing: PlazaMinPricing,
  currentRates: PlazaPricingRateMultipliers,
  currentCurrencies: PlazaPricingDimensionMap<PlazaPricingCurrency>,
  currentSources: PlazaPricingDimensionMap<PlazaPricingSource>,
  candidatePricing: PlazaMinPricing,
  candidateRates: PlazaPricingRateMultipliers,
  candidateCurrencies: PlazaPricingDimensionMap<PlazaPricingCurrency>,
  candidateSources: PlazaPricingDimensionMap<PlazaPricingSource>
): { pricing: PlazaMinPricing; rates: PlazaPricingRateMultipliers; currencies: PlazaPricingDimensionMap<PlazaPricingCurrency>; sources: PlazaPricingDimensionMap<PlazaPricingSource> } {
  const pricing = emptyPricing()
  const rates = defaultPricingRateMultipliers()
  const currencies = defaultPricingCurrencies()
  const sources = defaultPricingSources()
  for (const key of PLAZA_PRICING_DIMENSIONS) {
    const chosen = chooseEffectivePrice(currentPricing[key], currentRates[key], candidatePricing[key], candidateRates[key])
    pricing[key] = chosen.price
    rates[key] = chosen.rate
    currencies[key] = chosen.selected ? candidateCurrencies[key] : currentCurrencies[key]
    sources[key] = chosen.selected ? candidateSources[key] : currentSources[key]
  }
  return { pricing, rates, currencies, sources }
}

function representativePricingRate(
  pricing: PlazaMinPricing,
  rates: PlazaPricingRateMultipliers
): number {
  const preferredKeys: PlazaPricingDimension[] = [
    'input',
    'perRequest',
    'output',
    'cacheRead',
    'cacheWrite',
    'imageInput',
    'imageOutput',
    'image1K',
    'image2K',
    'image4K',
    'video480p',
    'video720p',
    'video1080p'
  ]
  for (const key of preferredKeys) {
    if (pricing[key] != null) return rates[key]
  }
  return 1
}

function mergePricingSummary(
  current: PlazaPricingSummary | null,
  candidatePricing: PlazaMinPricing,
  candidateRates: PlazaPricingRateMultipliers,
  candidateCurrencies: PlazaPricingDimensionMap<PlazaPricingCurrency>,
  candidateSources: PlazaPricingDimensionMap<PlazaPricingSource>
): PlazaPricingSummary {
  const merged = mergeEffectivePricing(
    current?.minPricing ?? emptyPricing(),
    current?.minPricingRateMultipliers ?? defaultPricingRateMultipliers(),
    current?.minPricingCurrencies ?? defaultPricingCurrencies(),
    current?.minPricingSources ?? defaultPricingSources(),
    candidatePricing,
    candidateRates,
    candidateCurrencies,
    candidateSources
  )
  return {
    minPricing: merged.pricing,
    minPricingRateMultipliers: merged.rates,
    minPricingCurrencies: merged.currencies,
    minPricingSources: merged.sources,
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
        // 该分组下查不到任何价格的模型，网关准入守卫会在转发前拒绝（文本 503
        // billing_configuration_error、视频 400 VIDEO_PRICING_MISSING），展示出来只会
        // 让用户看到一张调不通的空价卡片。显式配置的 0 价不是 null，仍会展示。
        if (!hasAnyPrice(candidatePricing)) continue
        let existing = byModel.get(model.name)

        if (!existing) {
          existing = {
            model: model.name,
            displayName: model.name,
            platform,
            billingKind: classifyPricing(model.pricing),
            standardPricing: null,
            vipPricing: null,
            supportedGroups: [],
            recentCalls,
            recentCallWindowSeconds,
            timeSchedule: model.time_schedule
          }
          byModel.set(model.name, existing)
        }
        existing.billingKind = strongerBillingKind(existing.billingKind, classifyPricing(model.pricing))
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
          const currency = model.pricing?.currency === 'CNY' ? 'CNY' : 'USD'
          const source = model.pricing?.source === 'override' || model.pricing?.source === 'merged' || model.pricing?.source === 'catalog' || model.pricing?.source === 'official' || model.pricing?.source === 'channel'
            ? model.pricing.source
            : 'channel'
          const candidateCurrencies = defaultPricingCurrencies()
          const candidateSources = defaultPricingSources()
          for (const key of Object.keys(candidateCurrencies) as PlazaPricingDimension[]) {
            candidateCurrencies[key] = currency
            candidateSources[key] = source
          }
          // 视频档位价来自 video_pricing（后端强制 USD），不跟随模型 token 价的币种。
          for (const key of Object.values(PLAZA_VIDEO_TIER_DIMENSIONS)) {
            candidateCurrencies[key] = 'USD'
          }
          existing[pricingKey] = mergePricingSummary(
            existing[pricingKey],
            candidatePricing,
            pricingRateMultipliers(group, model.pricing),
            candidateCurrencies,
            candidateSources
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
