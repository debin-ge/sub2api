import { apiClient } from '@/api/client'

export const PRICE_FIELDS = [
  'input_cost_per_token',
  'output_cost_per_token',
  'cache_creation_input_token_cost',
  'cache_creation_input_token_cost_above_1hr',
  'cache_read_input_token_cost',
  'input_cost_per_token_priority',
  'output_cost_per_token_priority',
  'cache_creation_input_token_cost_priority',
  'cache_read_input_token_cost_priority',
  'long_context_input_token_threshold',
  'long_context_input_cost_multiplier',
  'long_context_output_cost_multiplier',
  'output_cost_per_image',
  'output_cost_per_image_token',
  'input_cost_per_image_token',
] as const

export type PriceField = (typeof PRICE_FIELDS)[number]
export type ModelPriceCurrency = 'USD' | 'CNY'

export type VideoBillingUnit = 'request' | 'second' | 'video_token'
export type VideoEstimatorType = 'pixel_frame' | 'fixed_tokens_per_second' | 'fixed_max_units'
export type VideoTokenScope = 'output_only' | 'input_plus_output'

export interface VideoPricingDefaults {
  resolution?: string
  generate_audio?: boolean
  request_mode?: 'standard' | 'batch'
  inference_mode?: 'online' | 'offline'
}

export interface VideoResolutionSpec {
  sizes: string[]
}

export interface VideoPricingConditions {
  providers?: string[]
  operations?: string[]
  sizes?: string[]
  resolutions?: string[]
  seconds?: number[]
  input_types?: string[]
  input_has_video?: boolean
  generate_audio?: boolean
  audio_enabled?: boolean
  request_modes?: Array<'standard' | 'batch'>
  inference_modes?: Array<'online' | 'offline'>
  qualities?: string[]
  service_tiers?: string[]
}

export interface VideoMinimumUnitsRule {
  units: number
  conditions?: VideoPricingConditions
}

export interface VideoUsageEstimator {
  type: VideoEstimatorType
  token_scope?: VideoTokenScope
  fps?: number
  divisor?: number
  tokens_per_second?: number
  max_units?: number
  max_input_video_seconds?: number
  minimum_units?: VideoMinimumUnitsRule[]
}

export interface VideoPricingRule {
  key: string
  billing_unit: VideoBillingUnit
  unit_price_usd: number
  estimator?: string
  conditions?: VideoPricingConditions
  priority?: number
  valid_from?: string
  valid_until?: string
}

export interface VideoPricingConfig {
  version: 1
  enabled: boolean
  currency: 'USD'
  defaults?: VideoPricingDefaults
  resolutions?: Record<string, VideoResolutionSpec>
  estimators?: Record<string, VideoUsageEstimator>
  rules?: VideoPricingRule[]
}

export interface VideoPricingPreviewAttributes {
  provider?: string
  model?: string
  operation?: string
  size?: string
  resolution?: string
  seconds?: number
  maximum_output_seconds?: number
  output_spec_unverified?: boolean
  input_type?: string
  input_has_video: boolean
  input_video_seconds?: number
  generate_audio?: boolean
  request_mode?: string
  inference_mode?: string
  quality?: string
  service_tier?: string
  customer_multiplier?: number
  at?: string
}

export interface VideoPricingPreviewMismatch {
  field: string
  expected?: unknown
  actual?: unknown
}

export interface VideoPricingPreviewRejectedRule {
  key: string
  mismatches: VideoPricingPreviewMismatch[]
}

export interface VideoPricingPreviewResult {
  matched: boolean
  rule_key?: string
  billing_unit?: string
  estimated_units?: number
  maximum_units?: number
  estimated_cost?: number
  error_code?: string
  normalized_attributes: VideoPricingPreviewAttributes
  rejected_rules: VideoPricingPreviewRejectedRule[]
}

export type ModelPricePayload = Partial<Record<PriceField, number>> & {
  video_pricing?: VideoPricingConfig
}

/** 倍率语义见 @/api/channels 中的同名类型：都是相对随行的那份生效价。 */
export interface ModelPriceTimeSchedule {
  kind: string
  timezone: string
  peak_windows: string[]
  peak_multiplier: number
  off_peak_multiplier: number
}

export interface ModelPriceListItem {
  platform: string
  model: string
  source: 'catalog' | 'override' | 'merged' | 'official'
  currency: ModelPriceCurrency
  catalog_currency?: ModelPriceCurrency
  override_currency?: ModelPriceCurrency
  token_pricing_absent: boolean
  has_image_pricing: boolean
  has_video_pricing: boolean
  video_pricing_valid: boolean
  video_rule_count: number
  video_billing_units: VideoBillingUnit[]
  video_resolutions: string[]
  video_pricing_error?: string
  sync_invalidated: boolean
  redundant: boolean
  effective: Record<string, unknown>
  overridden_fields: string[]
  enabled: boolean
  override_platform?: string
  note?: string
  updated_by?: number
  updated_at?: string
  time_schedule?: ModelPriceTimeSchedule
}

export interface ModelPriceDetail {
  platform: string
  model: string
  currency: ModelPriceCurrency
  catalog_currency?: ModelPriceCurrency
  override_currency?: ModelPriceCurrency
  catalog: Record<string, unknown>
  override?: ModelPricePayload
  effective: Record<string, unknown>
  enabled: boolean
  token_pricing_absent: boolean
  has_image_pricing: boolean
  has_video_pricing: boolean
  video_pricing_valid: boolean
  video_rule_count: number
  video_billing_units: VideoBillingUnit[]
  video_resolutions: string[]
  video_pricing_error?: string
  sync_invalidated: boolean
  redundant: boolean
  override_platform?: string
  note?: string
  updated_by?: number
  updated_at?: string
  time_schedule?: ModelPriceTimeSchedule
}

export interface ModelPriceListParams {
  platform?: string
  q?: string
  status?: string
  page?: number
  page_size?: number
}

export interface ModelPriceListResponse {
  items: ModelPriceListItem[]
  total: number
}

export interface ModelPriceSyncStatus {
  model_count?: number
  catalog_model_count?: number
  override_count?: number
  overrides_reapplied?: number
  last_updated?: string
  local_hash?: string
  success?: boolean
}

export interface UpsertModelPriceRequest {
  platform: string
  model: string
  currency: ModelPriceCurrency
  payload: ModelPricePayload
  enabled?: boolean
  note?: string
}

const TOKENS_PER_MTOK = 1_000_000
const MTOK_PRICE_SIGNIFICANT_DIGITS = 15

export function tokenToMTok(value: number | null | undefined): number | '' {
  if (value == null) return ''
  const scaled = value * TOKENS_PER_MTOK
  if (!Number.isFinite(scaled) || scaled === 0) return scaled

  // Multiplying decimal token prices by 1,000,000 can expose IEEE-754 tails
  // such as 1.9800000000000002. Keep meaningful catalog precision while
  // normalizing those representation artifacts before rendering/editing.
  return Number(scaled.toPrecision(MTOK_PRICE_SIGNIFICANT_DIGITS))
}

export function mTokToToken(value: string | number): number | undefined {
  if (value === '') return undefined
  const parsed = Number(value)
  if (Number.isNaN(parsed)) return undefined
  return parsed / TOKENS_PER_MTOK
}

export function isImageField(field: string): boolean {
  return field === 'output_cost_per_image'
}

export async function listModelPrices(params: ModelPriceListParams): Promise<ModelPriceListResponse> {
  const { data } = await apiClient.get('/admin/model-prices', { params })
  return data
}

export async function listModelPricePlatforms(): Promise<string[]> {
  const { data } = await apiClient.get('/admin/model-prices/platforms')
  return data.platforms || []
}

export async function getModelPriceEntry(platform: string, model: string): Promise<ModelPriceDetail> {
  const { data } = await apiClient.get('/admin/model-prices/entry', {
    params: { platform, model },
  })
  return data
}

export async function previewVideoPrice(
  videoPricing: VideoPricingConfig,
  attributes: VideoPricingPreviewAttributes,
): Promise<VideoPricingPreviewResult> {
  const { data } = await apiClient.post('/admin/model-prices/video-preview', {
    video_pricing: videoPricing,
    attributes,
  })
  return data
}

export async function upsertModelPrice(input: UpsertModelPriceRequest) {
  const { data } = await apiClient.put('/admin/model-prices/entry', input)
  return data
}

export async function deleteModelPrice(platform: string, model: string) {
  const { data } = await apiClient.delete('/admin/model-prices/entry', {
    params: { platform, model },
  })
  return data
}

export async function syncModelPrices(): Promise<ModelPriceSyncStatus> {
  const { data } = await apiClient.post('/admin/model-prices/sync')
  return data
}

export async function getModelPriceSyncStatus(): Promise<ModelPriceSyncStatus> {
  const { data } = await apiClient.get('/admin/model-prices/sync-status')
  return data
}
