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

  it('returns Zhipu GLM labels and a distinct indigo visual style', () => {
    expect(platformLabel('zhipu')).toBe('Zhipu GLM')
    expect(platformLabel('glm')).toBe('Zhipu GLM')
    expect(platformBadgeClass('zhipu')).toContain('indigo')
    expect(platformButtonClass('zhipu')).toContain('indigo')
    expect(platformBadgeClass('glm')).toContain('rose')
    expect(platformBadgeClass('zhipu')).not.toBe(platformBadgeClass('openai'))
    expect(platformBadgeClass('zhipu')).not.toBe(platformBadgeClass('kimi'))
    expect(platformBadgeClass('zhipu')).not.toBe(platformBadgeClass('deepseek'))
  })

  it('returns Kimi labels and a distinct pink visual style', () => {
    expect(platformLabel('kimi')).toBe('Kimi')
    expect(platformBadgeClass('kimi')).toContain('pink')
    expect(platformButtonClass('kimi')).toContain('pink')
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('openai'))
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('anthropic'))
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('gemini'))
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('antigravity'))
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('minimax'))
    expect(platformBadgeClass('kimi')).not.toBe(platformBadgeClass('zhipu'))
  })

  it('returns DeepSeek labels and a distinct teal visual style', () => {
    expect(platformLabel('deepseek')).toBe('DeepSeek')
    expect(platformBadgeClass('deepseek')).toContain('teal')
    expect(platformButtonClass('deepseek')).toContain('teal')
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('openai'))
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('anthropic'))
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('gemini'))
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('antigravity'))
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('minimax'))
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('zhipu'))
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

  it('returns Grok labels and a distinct neutral visual style', () => {
    expect(platformLabel('grok')).toBe('Grok')
    expect(platformBadgeClass('grok')).toContain('zinc')
    expect(platformButtonClass('grok')).toContain('zinc')
    expect(platformBadgeClass('grok')).not.toBe(platformBadgeClass('openai'))
    expect(platformBadgeClass('grok')).not.toBe(platformBadgeClass('opencode'))
  })
})
