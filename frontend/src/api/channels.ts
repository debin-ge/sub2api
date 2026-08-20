/**
 * User Channels API endpoints (non-admin)
 * 用户侧「可用渠道」聚合查询：渠道 + 用户可访问的分组 + 支持模型（含定价）。
 */

import { apiClient } from './client'
import type { BillingMode } from '@/constants/channel'
import type { GroupDenyReason, GroupSuggestedAction } from '@/types'

export interface UserAvailableGroup {
  id: number
  name: string
  platform: string
  /** 'standard' | 'subscription' — 订阅分组视觉加深，和 API 密钥页保持一致。 */
  subscription_type: string
  /** 分组默认倍率。用户专属倍率（若有）通过 /groups/rates 获取后在前端 join。 */
  rate_multiplier: number
  peak_rate_enabled: boolean
  peak_start: string
  peak_end: string
  peak_rate_multiplier: number
  /** true = 专属分组（小范围授权）；false = 公开分组。 */
  is_exclusive: boolean
  /** Standard group reserved for users with effective VIP access. */
  vip_only?: boolean
  /** 图片计费模型是否使用独立倍率，不叠加普通分组倍率。 */
  image_rate_independent?: boolean
  /** 图片计费模型启用独立倍率时使用的倍率。 */
  image_rate_multiplier?: number
  /**
   * Authenticated binding decision. Only a literal true is safe to treat as
   * bindable; the public catalog may omit this field.
   */
  can_bind?: boolean
  deny_reason?: GroupDenyReason | null
  suggested_action?: GroupSuggestedAction | null
}

export interface UserPricingInterval {
  min_tokens: number
  max_tokens: number | null
  tier_label?: string
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_read_price: number | null
  per_request_price: number | null
}

export interface UserSupportedModelPricing {
  billing_mode: BillingMode
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_read_price: number | null
  image_input_price: number | null
  image_output_price: number | null
  per_request_price: number | null
  intervals: UserPricingInterval[]
}

/**
 * 官方分时定价规则。两个倍率都是相对同一条响应里随行的那份价：价格目录 /
 * 管理端生效价存的是空闲价（peak=2、off_peak=1），代码内官方兜底表存的是
 * 高峰价（peak=1、off_peak=0.5）。展示侧一律用倍率换算，不要假设基准是哪一档。
 */
export interface ModelPriceTimeSchedule {
  kind: string
  timezone: string
  peak_windows: string[]
  peak_multiplier: number
  off_peak_multiplier: number
}

export interface UserSupportedModel {
  name: string
  platform: string
  pricing: UserSupportedModelPricing | null
  recent_call_count?: number
  recent_call_window_seconds?: number
  time_schedule?: ModelPriceTimeSchedule
}

/**
 * 渠道下单个平台的子视图：用户可访问的分组 + 该平台支持的模型。
 * 后端把一个渠道按平台聚合成 sections，前端可以把渠道名作为 row-group
 * 一次渲染，后面按 sections 顺序用 rowspan 铺开。
 */
export interface UserChannelPlatformSection {
  platform: string
  groups: UserAvailableGroup[]
  supported_models: UserSupportedModel[]
}

export interface UserAvailableChannel {
  name: string
  description: string
  platforms: UserChannelPlatformSection[]
}

/** 列出当前用户可见的「可用渠道」（与 /groups/available 保持一致，返回平数组）。 */
export async function getAvailable(options?: { signal?: AbortSignal }): Promise<UserAvailableChannel[]> {
  const { data } = await apiClient.get<UserAvailableChannel[]>('/channels/available', {
    signal: options?.signal
  })
  return data
}

/** 列出公开可见的「可用渠道」，用于匿名模型广场。 */
export async function getPublic(options?: { signal?: AbortSignal }): Promise<UserAvailableChannel[]> {
  const { data } = await apiClient.get<UserAvailableChannel[]>('/channels/public', {
    signal: options?.signal
  })
  return data
}

export const userChannelsAPI = { getAvailable, getPublic }

export default userChannelsAPI
