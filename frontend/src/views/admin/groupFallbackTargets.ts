import type { AdminGroup } from '@/types'

/**
 * Subscription groups are never valid fallback targets. Keeping these filters
 * shared prevents the regular and invalid-request selectors from drifting.
 */
const isBaseFallbackTarget = (group: AdminGroup, currentGroupId?: number): boolean =>
  group.platform === 'anthropic'
  && group.status === 'active'
  && group.subscription_type !== 'subscription'
  && group.id !== currentGroupId

export const getClaudeCodeFallbackTargets = (
  groups: AdminGroup[],
  currentGroupId?: number
): AdminGroup[] =>
  groups.filter(
    (group) => isBaseFallbackTarget(group, currentGroupId) && !group.claude_code_only
  )

export const getInvalidRequestFallbackTargets = (
  groups: AdminGroup[],
  currentGroupId?: number
): AdminGroup[] =>
  groups.filter(
    (group) =>
      isBaseFallbackTarget(group, currentGroupId)
      && group.fallback_group_id_on_invalid_request === null
  )
