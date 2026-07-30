import { describe, expect, it } from 'vitest'

import {
  canShowDuplicateVIPPaymentCTA,
  getGroupDenyMessageKey,
  getKnownVIPAccessState,
  getSafeGroupSuggestedAction,
  isGroupBindable,
  shouldShowGroupPaymentCTA,
  vipAccessDescriptionKey,
} from '../vipAccess'

describe('VIP access fail-safe helpers', () => {
  it.each([
    'ACTIVE',
    'PAYMENT_REQUIRED',
    'ACTIVATION_PENDING',
    'ACTIVATION_FAILED',
    'RESTRICTED',
  ] as const)('recognizes %s', (state) => {
    expect(getKnownVIPAccessState(state)).toBe(state)
  })

  it('treats missing, malformed and future states as unknown', () => {
    expect(getKnownVIPAccessState(undefined)).toBeNull()
    expect(getKnownVIPAccessState('active')).toBeNull()
    expect(getKnownVIPAccessState('FUTURE_STATE')).toBeNull()
    expect(vipAccessDescriptionKey('FUTURE_STATE')).toBe('vip.access.description.UNKNOWN')
  })

  it('suppresses duplicate VIP payment CTAs for unresolved states', () => {
    expect(canShowDuplicateVIPPaymentCTA('ACTIVE')).toBe(true)
    expect(canShowDuplicateVIPPaymentCTA('PAYMENT_REQUIRED')).toBe(true)
    expect(canShowDuplicateVIPPaymentCTA('ACTIVATION_PENDING')).toBe(false)
    expect(canShowDuplicateVIPPaymentCTA('ACTIVATION_FAILED')).toBe(false)
    expect(canShowDuplicateVIPPaymentCTA('RESTRICTED')).toBe(false)
    expect(canShowDuplicateVIPPaymentCTA('FUTURE_STATE')).toBe(false)
    expect(canShowDuplicateVIPPaymentCTA(undefined)).toBe(false)
  })
})

describe('group catalog fail-safe helpers', () => {
  it('allows binding only for a literal true decision', () => {
    expect(isGroupBindable({ can_bind: true })).toBe(true)
    expect(isGroupBindable({ can_bind: false })).toBe(false)
    expect(isGroupBindable({})).toBe(false)
    expect(isGroupBindable({ can_bind: 'true' })).toBe(false)
  })

  it('maps known deny reasons and falls back for unknown values', () => {
    expect(getGroupDenyMessageKey('GROUP_VIP_ONLY')).toBe('vip.group.denied.GROUP_VIP_ONLY')
    expect(getGroupDenyMessageKey('GROUP_NOT_ALLOWED')).toBe('vip.group.denied.GROUP_NOT_ALLOWED')
    expect(getGroupDenyMessageKey('GROUP_NOT_ACTIVE')).toBe('vip.group.denied.GROUP_NOT_ACTIVE')
    expect(getGroupDenyMessageKey('SUBSCRIPTION_REQUIRED')).toBe('vip.group.denied.SUBSCRIPTION_REQUIRED')
    expect(getGroupDenyMessageKey('NEW_DENY_REASON')).toBe('vip.group.denied.UNKNOWN')
  })

  it('shows a payment action only for a known VIP denial and known PAYMENT action', () => {
    expect(shouldShowGroupPaymentCTA(false, 'GROUP_VIP_ONLY', 'PAYMENT')).toBe(true)
    expect(shouldShowGroupPaymentCTA(undefined, 'GROUP_VIP_ONLY', 'PAYMENT')).toBe(true)
    expect(shouldShowGroupPaymentCTA(true, 'GROUP_VIP_ONLY', 'PAYMENT')).toBe(false)
    expect(shouldShowGroupPaymentCTA(false, 'NEW_DENY_REASON', 'PAYMENT')).toBe(false)
    expect(shouldShowGroupPaymentCTA(false, 'GROUP_VIP_ONLY', 'NEW_ACTION')).toBe(false)
    expect(shouldShowGroupPaymentCTA(false, 'GROUP_NOT_ALLOWED', 'PAYMENT')).toBe(false)
  })

  it('does not trust a known action paired with an unknown deny reason', () => {
    expect(getSafeGroupSuggestedAction('NEW_DENY_REASON', 'CONTACT_SUPPORT')).toBeNull()
  })
})
