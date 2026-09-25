import {
  imageTierRateMultiplier,
  videoTierRateMultiplier,
  type PlazaBillingKind,
  type PlazaSupportedGroup
} from '@/composables/useModelAggregation'
import { formatPeakRateWindow, hasPeakRate, serverTimezoneLabel } from '@/utils/peak-rate'

export interface PlazaGroupSummary {
  id: number
  name: string
  vip: boolean
  rate: number
  channels: string[]
  /** 独立的图片 / 视频倍率，只在对应计费类型下给出。 */
  mediaRate?: { kind: 'image' | 'video'; rate: number }
  peakWindow?: string
}

/**
 * 同一分组可能挂在多个渠道下，按分组去重并合并渠道名；
 * 普通分组在前、VIP 在后，同类按倍率从低到高。
 */
export function summarizePlazaGroups(
  groups: PlazaSupportedGroup[],
  billingKind: PlazaBillingKind,
  serverUtcOffset?: string
): PlazaGroupSummary[] {
  const serverTimezone = serverTimezoneLabel(serverUtcOffset)
  const byId = new Map<number, PlazaGroupSummary>()
  for (const item of groups) {
    const group = item.group
    let summary = byId.get(group.id)
    if (!summary) {
      summary = {
        id: group.id,
        name: group.name,
        vip: group.vip_only === true,
        rate: group.rate_multiplier || 1,
        channels: []
      }
      if (billingKind === 'image' && group.image_rate_independent === true) {
        summary.mediaRate = { kind: 'image', rate: imageTierRateMultiplier(group) }
      } else if (billingKind === 'video' && group.video_rate_independent === true) {
        summary.mediaRate = { kind: 'video', rate: videoTierRateMultiplier(group) }
      }
      if (hasPeakRate(group)) {
        summary.peakWindow = formatPeakRateWindow(group, serverTimezone)
      }
      byId.set(group.id, summary)
    }
    if (item.channelName && !summary.channels.includes(item.channelName)) {
      summary.channels.push(item.channelName)
    }
  }
  return [...byId.values()].sort((a, b) => Number(a.vip) - Number(b.vip) || a.rate - b.rate)
}
