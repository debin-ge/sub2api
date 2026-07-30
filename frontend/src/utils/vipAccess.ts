import type {
  GroupDenyReason,
  GroupSuggestedAction,
  VIPAccessState,
} from '@/types'

export const KNOWN_VIP_ACCESS_STATES = [
  'ACTIVE',
  'PAYMENT_REQUIRED',
  'ACTIVATION_PENDING',
  'ACTIVATION_FAILED',
  'RESTRICTED',
] as const

export type KnownVIPAccessState = (typeof KNOWN_VIP_ACCESS_STATES)[number]

const knownVIPAccessStates = new Set<string>(KNOWN_VIP_ACCESS_STATES)

export function getKnownVIPAccessState(value: unknown): KnownVIPAccessState | null {
  return typeof value === 'string' && knownVIPAccessStates.has(value)
    ? value as KnownVIPAccessState
    : null
}

export function isVIPActive(value: VIPAccessState | null | undefined): boolean {
  return getKnownVIPAccessState(value) === 'ACTIVE'
}

/**
 * This helper is intentionally limited to a payment prompt tied to VIP
 * activation. Generic balance recharge and ordinary subscription entry points
 * must not use it.
 */
export function canShowDuplicateVIPPaymentCTA(
  value: VIPAccessState | null | undefined,
): boolean {
  const state = getKnownVIPAccessState(value)
  return state === 'ACTIVE' || state === 'PAYMENT_REQUIRED'
}

export function vipAccessLabelKey(value: VIPAccessState | null | undefined): string {
  const state = getKnownVIPAccessState(value)
  return state ? `vip.access.label.${state}` : 'vip.access.label.UNKNOWN'
}

export function vipAccessDescriptionKey(value: VIPAccessState | null | undefined): string {
  const state = getKnownVIPAccessState(value)
  return state ? `vip.access.description.${state}` : 'vip.access.description.UNKNOWN'
}

const KNOWN_GROUP_DENY_REASONS = new Set<string>([
  'GROUP_NOT_ALLOWED',
  'GROUP_VIP_ONLY',
  'GROUP_NOT_ACTIVE',
  'SUBSCRIPTION_REQUIRED',
  'GROUP_FALLBACK_SUBSCRIPTION_FORBIDDEN',
  'GROUP_FALLBACK_EXCLUSIVE_ESCALATION',
  'GROUP_FALLBACK_VIP_ESCALATION',
  'GROUP_ACCESS_PROFILE_MISSING',
  'GROUP_FALLBACK_INVALID_CONFIG',
])

const KNOWN_GROUP_SUGGESTED_ACTIONS = new Set<string>([
  'PAYMENT',
  'CONTACT_SUPPORT',
])

export function isGroupBindable(value: { can_bind?: unknown } | null | undefined): boolean {
  return value?.can_bind === true
}

export function getKnownGroupDenyReason(value: unknown): GroupDenyReason | null {
  return typeof value === 'string' && KNOWN_GROUP_DENY_REASONS.has(value)
    ? value as GroupDenyReason
    : null
}

export function getGroupDenyMessageKey(value: unknown): string {
  switch (getKnownGroupDenyReason(value)) {
    case 'GROUP_VIP_ONLY':
      return 'vip.group.denied.GROUP_VIP_ONLY'
    case 'GROUP_NOT_ALLOWED':
      return 'vip.group.denied.GROUP_NOT_ALLOWED'
    case 'GROUP_ACCESS_PROFILE_MISSING':
      return 'vip.group.denied.GROUP_ACCESS_PROFILE_MISSING'
    case 'GROUP_NOT_ACTIVE':
      return 'vip.group.denied.GROUP_NOT_ACTIVE'
    case 'SUBSCRIPTION_REQUIRED':
      return 'vip.group.denied.SUBSCRIPTION_REQUIRED'
    case 'GROUP_FALLBACK_SUBSCRIPTION_FORBIDDEN':
    case 'GROUP_FALLBACK_EXCLUSIVE_ESCALATION':
    case 'GROUP_FALLBACK_VIP_ESCALATION':
    case 'GROUP_FALLBACK_INVALID_CONFIG':
      return 'vip.group.denied.INVALID_CONFIGURATION'
    default:
      return 'vip.group.denied.UNKNOWN'
  }
}

/**
 * A suggested action is trusted only when both its deny reason and action are
 * known. This prevents a newer backend enum from accidentally turning into a
 * payment prompt on an older client.
 */
export function getSafeGroupSuggestedAction(
  denyReason: unknown,
  suggestedAction: unknown,
): GroupSuggestedAction | null {
  if (!getKnownGroupDenyReason(denyReason)) return null
  if (typeof suggestedAction !== 'string' || !KNOWN_GROUP_SUGGESTED_ACTIONS.has(suggestedAction)) {
    return null
  }
  return suggestedAction as GroupSuggestedAction
}

export function shouldShowGroupPaymentCTA(
  canBind: unknown,
  denyReason: unknown,
  suggestedAction: unknown,
): boolean {
  return canBind !== true
    && getKnownGroupDenyReason(denyReason) === 'GROUP_VIP_ONLY'
    && getSafeGroupSuggestedAction(denyReason, suggestedAction) === 'PAYMENT'
}
