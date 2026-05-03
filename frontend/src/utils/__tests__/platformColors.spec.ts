import { describe, expect, it } from 'vitest'
import { platformBadgeClass, platformButtonClass, platformLabel } from '../platformColors'

describe('platformColors', () => {
  it('returns MiniMax labels and a distinct cyan visual style', () => {
    expect(platformLabel('minimax')).toBe('MiniMax')
    expect(platformBadgeClass('minimax')).toContain('cyan')
    expect(platformButtonClass('minimax')).toContain('cyan')
    expect(platformBadgeClass('minimax')).not.toBe(platformBadgeClass('openai'))
    expect(platformBadgeClass('minimax')).not.toBe(platformBadgeClass('anthropic'))
    expect(platformBadgeClass('minimax')).not.toBe(platformBadgeClass('gemini'))
    expect(platformBadgeClass('minimax')).not.toBe(platformBadgeClass('antigravity'))
  })
})
