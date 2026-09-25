import type {
  AggregatedModel,
  PlazaPricingDimension,
  PlazaPricingSummary
} from '@/composables/useModelAggregation'
import { PER_MILLION_TOKEN_SCALE, PER_REQUEST_SCALE } from '@/utils/pricing'

/** 广场价格的计价单位：每百万 token / 每次 / 每张 / 每秒。 */
export type PlazaPriceUnit = 'million' | 'request' | 'image' | 'second'

export interface PlazaPricingRow {
  key: PlazaPricingDimension
  /** i18n key，或者档位名（1K / 720p 等）本身。 */
  label: string
  translate: boolean
  unit: PlazaPriceUnit
}

const row = (key: PlazaPricingDimension, label: string, unit: PlazaPriceUnit, translate = true): PlazaPricingRow => ({
  key,
  label,
  unit,
  translate
})

export const TEXT_TOKEN_ROWS: PlazaPricingRow[] = [
  row('input', 'plaza.card.input', 'million'),
  row('output', 'plaza.card.output', 'million'),
  row('cacheWrite', 'plaza.card.cacheWrite', 'million'),
  row('cacheRead', 'plaza.card.cacheRead', 'million'),
  row('imageOutput', 'plaza.card.imageOutput', 'million')
]

export const IMAGE_TOKEN_ROWS: PlazaPricingRow[] = [
  row('input', 'plaza.card.textInput', 'million'),
  row('imageInput', 'plaza.card.imageInput', 'million'),
  row('imageOutput', 'plaza.card.imageOutput', 'million')
]

export const IMAGE_TIER_ROWS: PlazaPricingRow[] = [
  row('image1K', '1K', 'image', false),
  row('image2K', '2K', 'image', false),
  row('image4K', '4K', 'image', false)
]

export const VIDEO_TIER_ROWS: PlazaPricingRow[] = [
  row('video480p', '480p', 'second', false),
  row('video720p', '720p', 'second', false),
  row('video1080p', '1080p', 'second', false)
]

export const PER_REQUEST_ROWS: PlazaPricingRow[] = [
  row('perRequest', 'plaza.card.perRequest', 'request')
]

export function unitScale(unit: PlazaPriceUnit): number {
  return unit === 'million' ? PER_MILLION_TOKEN_SCALE : PER_REQUEST_SCALE
}

type Translate = (key: string) => string

export function plazaUnitLabel(t: Translate, unit: PlazaPriceUnit): string {
  switch (unit) {
    case 'request':
      return t('plaza.price.unitPerRequest')
    case 'image':
      return t('plaza.price.unitPerImage')
    case 'second':
      return t('plaza.price.unitPerSecond')
    default:
      return t('plaza.price.unitPerMillion')
  }
}

export function rowLabel(t: Translate, item: PlazaPricingRow): string {
  return item.translate ? t(item.label) : item.label
}

export function hasDimension(model: AggregatedModel, key: PlazaPricingDimension): boolean {
  return model.standardPricing?.minPricing[key] != null || model.vipPricing?.minPricing[key] != null
}

/** 只保留至少一列有价格的行。 */
export function visibleRows(model: AggregatedModel, rows: PlazaPricingRow[]): PlazaPricingRow[] {
  return rows.filter((item) => hasDimension(model, item.key))
}

/** 一组行对应的展示倍率：取第一行有价格的倍率，用于表头的折扣标签。 */
export function rowsRateMultiplier(
  summary: PlazaPricingSummary | null | undefined,
  rows: PlazaPricingRow[]
): number | undefined {
  if (!summary) return undefined
  const first = rows.find((item) => summary.minPricing[item.key] != null)
  return first ? summary.minPricingRateMultipliers[first.key] : undefined
}

/** 列表模式一行里要列出的全部价格维度，与卡片按计费类型展示的内容一致。 */
export function listRowsFor(model: AggregatedModel): PlazaPricingRow[] {
  switch (model.billingKind) {
    case 'video':
      return visibleRows(model, [...VIDEO_TIER_ROWS, ...PER_REQUEST_ROWS])
    case 'image':
      return visibleRows(model, [...IMAGE_TOKEN_ROWS, ...IMAGE_TIER_ROWS, ...PER_REQUEST_ROWS])
    case 'per_request':
      return visibleRows(model, PER_REQUEST_ROWS)
    default:
      return visibleRows(model, [...TEXT_TOKEN_ROWS, ...PER_REQUEST_ROWS])
  }
}
