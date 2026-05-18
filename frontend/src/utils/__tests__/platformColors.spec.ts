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

  it('returns GLM labels and a distinct rose visual style', () => {
    expect(platformLabel('glm')).toBe('GLM')
    expect(platformBadgeClass('glm')).toContain('rose')
    expect(platformButtonClass('glm')).toContain('rose')
    expect(platformBadgeClass('glm')).not.toBe(platformBadgeClass('openai'))
    expect(platformBadgeClass('glm')).not.toBe(platformBadgeClass('anthropic'))
    expect(platformBadgeClass('glm')).not.toBe(platformBadgeClass('gemini'))
    expect(platformBadgeClass('glm')).not.toBe(platformBadgeClass('antigravity'))
    expect(platformBadgeClass('glm')).not.toBe(platformBadgeClass('minimax'))
  })

  it('returns Kimi labels and a distinct lime visual style', () => {
    expect(platformLabel('kimi')).toBe('Kimi')
    expect(platformBadgeClass('kimi')).toContain('lime')
    expect(platformButtonClass('kimi')).toContain('lime')
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('openai'))
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('anthropic'))
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('gemini'))
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('antigravity'))
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('minimax'))
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('glm'))
  })

  it('returns DeepSeek labels and a distinct indigo visual style', () => {
    expect(platformLabel('deepseek')).toBe('DeepSeek')
    expect(platformBadgeClass('deepseek')).toContain('indigo')
    expect(platformButtonClass('deepseek')).toContain('indigo')
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('openai'))
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('anthropic'))
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('gemini'))
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('antigravity'))
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('minimax'))
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('glm'))
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('kimi'))
  })

  it('returns Windsurf labels and a distinct teal visual style', () => {
    expect(platformLabel('windsurf')).toBe('Windsurf')
    expect(platformBadgeClass('windsurf')).toContain('teal')
    expect(platformButtonClass('windsurf')).toContain('teal')
    expect(platformBadgeClass('windsurf')).not.toBe(platformBadgeClass('openai'))
    expect(platformBadgeClass('windsurf')).not.toBe(platformBadgeClass('anthropic'))
    expect(platformBadgeClass('windsurf')).not.toBe(platformBadgeClass('deepseek'))
  })

  it('returns OpenCode labels and a distinct slate visual style', () => {
    expect(platformLabel('opencode')).toBe('OpenCode')
    expect(platformBadgeClass('opencode')).toContain('slate')
    expect(platformButtonClass('opencode')).toContain('slate')
    expect(platformBadgeClass('opencode')).not.toBe(platformBadgeClass('openai'))
    expect(platformBadgeClass('opencode')).not.toBe(platformBadgeClass('windsurf'))
  })
})
