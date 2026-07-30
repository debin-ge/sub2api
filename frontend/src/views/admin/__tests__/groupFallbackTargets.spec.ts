import { describe, expect, it } from 'vitest'
import type { AdminGroup } from '@/types'
import {
  getClaudeCodeFallbackTargets,
  getInvalidRequestFallbackTargets
} from '../groupFallbackTargets'

const makeGroup = (overrides: Partial<AdminGroup>): AdminGroup => ({
  id: 1,
  name: 'group',
  description: null,
  platform: 'anthropic',
  rate_multiplier: 1,
  is_exclusive: false,
  vip_only: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_live: false,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-29T00:00:00Z',
  updated_at: '2026-07-29T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  sort_order: 1,
  ...overrides
})

describe('admin fallback target selectors', () => {
  const groups = [
    makeGroup({ id: 1, name: 'standard' }),
    makeGroup({ id: 2, name: 'subscription', subscription_type: 'subscription' }),
    makeGroup({ id: 3, name: 'disabled', status: 'inactive' }),
    makeGroup({ id: 4, name: 'already invalid fallback', fallback_group_id_on_invalid_request: 9 }),
    makeGroup({ id: 5, name: 'claude-only', claude_code_only: true })
  ]

  it('excludes subscription targets from the regular fallback selector', () => {
    expect(getClaudeCodeFallbackTargets(groups).map((group) => group.id)).toEqual([1, 4])
  })

  it('excludes subscription targets from the invalid-request selector', () => {
    expect(getInvalidRequestFallbackTargets(groups).map((group) => group.id)).toEqual([1, 5])
  })

  it('excludes the group currently being edited from both selectors', () => {
    expect(getClaudeCodeFallbackTargets(groups, 1).map((group) => group.id)).toEqual([4])
    expect(getInvalidRequestFallbackTargets(groups, 1).map((group) => group.id)).toEqual([5])
  })
})
