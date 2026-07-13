import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

import {
  buildModelMappingObject,
  getModelsByPlatform,
  getPresetMappingsByPlatform,
  splitModelMappingObject
} from '../useModelWhitelist'

describe('useModelWhitelist', () => {
  it('openai 模型列表包含 GPT-5.4 官方快照', () => {
    const models = getModelsByPlatform('openai')

    expect(models).toContain('gpt-5.4')
    expect(models).toContain('gpt-5.4-mini')
    expect(models).toContain('gpt-5.4-2026-03-05')
    expect(models).toContain('codex-auto-review')
  })

  it('openai 模型列表不再暴露已下线的 ChatGPT 登录 Codex 模型', () => {
    const models = getModelsByPlatform('openai')

    expect(models).not.toContain('gpt-5')
    expect(models).not.toContain('gpt-5.1')
    expect(models).not.toContain('gpt-5.1-codex')
    expect(models).not.toContain('gpt-5.1-codex-max')
    expect(models).not.toContain('gpt-5.1-codex-mini')
    expect(models).not.toContain('gpt-5.2-codex')
  })

  it('antigravity 模型列表包含图片模型兼容项', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models).toContain('gemini-2.5-flash-image')
    expect(models).toContain('gemini-3.1-flash-image')
    expect(models).toContain('gemini-3-pro-image')
  })

  it('Claude 模型列表包含新发布的 Claude 模型', () => {
    expect(getModelsByPlatform('claude')).toContain('claude-fable-5')
    expect(getModelsByPlatform('antigravity')).toContain('claude-fable-5')
    expect(getModelsByPlatform('claude')).toContain('claude-opus-4-8')
    expect(getModelsByPlatform('antigravity')).toContain('claude-opus-4-8')
  })

  it('gemini 模型列表包含原生生图模型', () => {
    const models = getModelsByPlatform('gemini')

    expect(models).toContain('gemini-2.5-flash-image')
    expect(models).toContain('gemini-3.1-flash-image')
    expect(models.indexOf('gemini-3.1-flash-image')).toBeLessThan(models.indexOf('gemini-2.0-flash'))
    expect(models.indexOf('gemini-2.5-flash-image')).toBeLessThan(models.indexOf('gemini-2.5-flash'))
  })

  it('minimax 模型列表包含 Token Plan M3 和 M2.7 模型', () => {
    const models = getModelsByPlatform('minimax')

    expect(models).toContain('MiniMax-M2.7')
    expect(models).toContain('MiniMax-M2.7-highspeed')
    expect(models).toContain('MiniMax-M3')
  })

  it('glm 模型列表仅包含 Coding Plan MVP 模型', () => {
    const models = getModelsByPlatform('glm')

    expect(models).toEqual(['GLM-5.1', 'GLM-4.7', 'GLM-4.5-air'])
  })

  it('kimi 模型列表仅包含 Coding Plan 模型', () => {
    const models = getModelsByPlatform('kimi')

    expect(models).toEqual(['kimi-for-coding'])
    expect(models).not.toContain('kimi-latest')
    expect(models).not.toContain('claude-sonnet-4-5')
  })

  it('deepseek 模型列表仅包含 Gateway 默认模型', () => {
    const models = getModelsByPlatform('deepseek')

    expect(models).toEqual(['deepseek-v4-flash', 'deepseek-v4-pro'])
    expect(models).not.toContain('deepseek-chat')
    expect(models).not.toContain('deepseek-reasoner')
  })

  it('windsurf 模型列表包含反代后端支持的官方模型', () => {
    const models = getModelsByPlatform('windsurf')

    expect(models).toEqual(expect.arrayContaining([
      'claude-sonnet-4-6',
      'claude-opus-4-7-xhigh',
      'claude-opus-4-7-max',
      'gpt-5-5-high',
      'gpt-5-5-xhigh-priority',
      'gpt-5-4-high',
      'gemini-3-1-pro-high',
      'swe-1-6'
    ]))
    expect(models).toContain('claude-sonnet-4.6')
    expect(models.indexOf('claude-sonnet-4-6')).toBeLessThan(models.indexOf('claude-sonnet-4.6'))
  })

  it('opencode 模型列表包含 OpenCode2API 默认模型', () => {
    const models = getModelsByPlatform('opencode')

    expect(models).toEqual(['opencode/big-pickle', 'opencode/gpt5-nano', 'gpt5-nano'])
  })

  it('glm 预设映射 Claude family aliases to GLM models', () => {
    const mappings = getPresetMappingsByPlatform('glm')

    expect(mappings).toEqual(expect.arrayContaining([
      expect.objectContaining({ from: 'claude-sonnet-*', to: 'GLM-5.1' }),
      expect.objectContaining({ from: 'claude-sonnet-4-5', to: 'GLM-5.1' }),
      expect.objectContaining({ from: 'claude-opus-*', to: 'GLM-5.1' }),
      expect.objectContaining({ from: 'claude-opus-4-5', to: 'GLM-5.1' }),
      expect.objectContaining({ from: 'claude-haiku-*', to: 'GLM-4.5-air' }),
      expect.objectContaining({ from: 'claude-haiku-4-5', to: 'GLM-4.5-air' })
    ]))
  })

  it('kimi 预设映射提供 Claude Sonnet aliases 到 kimi-for-coding', () => {
    const mappings = getPresetMappingsByPlatform('kimi')

    expect(mappings).toEqual(expect.arrayContaining([
      expect.objectContaining({ from: 'claude-sonnet-*', to: 'kimi-for-coding' }),
      expect.objectContaining({ from: 'claude-sonnet-4-5', to: 'kimi-for-coding' }),
      expect.objectContaining({ from: 'claude-3-5-sonnet-latest', to: 'kimi-for-coding' }),
      expect.objectContaining({ from: 'kimi-for-coding', to: 'kimi-for-coding' })
    ]))
  })

  it('deepseek 预设映射提供官方兼容 aliases 到 Gateway 默认模型', () => {
    const mappings = getPresetMappingsByPlatform('deepseek')

    expect(mappings).toEqual(expect.arrayContaining([
      expect.objectContaining({ from: 'deepseek-chat', to: 'deepseek-v4-flash' }),
      expect.objectContaining({ from: 'deepseek-v3', to: 'deepseek-v4-flash' }),
      expect.objectContaining({ from: 'deepseek-reasoner', to: 'deepseek-v4-pro' }),
      expect.objectContaining({ from: 'deepseek-r1', to: 'deepseek-v4-pro' }),
      expect.objectContaining({ from: 'deepseek-v4-flash', to: 'deepseek-v4-flash' }),
      expect.objectContaining({ from: 'deepseek-v4-pro', to: 'deepseek-v4-pro' })
    ]))
  })

  it('windsurf 预设映射提供 Claude/OpenAI/Gemini 到官方模型', () => {
    const mappings = getPresetMappingsByPlatform('windsurf')

    expect(mappings).toEqual(expect.arrayContaining([
      expect.objectContaining({ from: 'claude-sonnet-*', to: 'claude-sonnet-4-6' }),
      expect.objectContaining({ from: 'claude-opus-*', to: 'claude-opus-4-7-xhigh' }),
      expect.objectContaining({ from: 'gpt-5.5-high', to: 'gpt-5-5-high' }),
      expect.objectContaining({ from: 'gpt-5.5-xhigh-fast', to: 'gpt-5-5-xhigh-priority' }),
      expect.objectContaining({ from: 'gemini-3.1-pro', to: 'gemini-3-1-pro-high' }),
      expect.objectContaining({ from: 'swe-1.6', to: 'swe-1-6' })
    ]))
  })

  it('antigravity 模型列表会把新的 Gemini 图片模型排在前面', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models.indexOf('gemini-3.1-flash-image')).toBeLessThan(models.indexOf('gemini-2.5-flash'))
    expect(models.indexOf('gemini-2.5-flash-image')).toBeLessThan(models.indexOf('gemini-2.5-flash-lite'))
  })

  it('antigravity 模型列表包含 Gemini 3.1 Pro 通用别名', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models).toContain('gemini-3.1-pro')
  })

  it('whitelist 模式会忽略通配符条目', () => {
    const mapping = buildModelMappingObject('whitelist', ['claude-*', 'gemini-3.1-flash-image'], [])
    expect(mapping).toEqual({
      'gemini-3.1-flash-image': 'gemini-3.1-flash-image'
    })
  })

  it('whitelist 模式会保留 GPT-5.4 官方快照的精确映射', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-2026-03-05'], [])

    expect(mapping).toEqual({
      'gpt-5.4-2026-03-05': 'gpt-5.4-2026-03-05'
    })
  })

  it('whitelist keeps GPT-5.4 mini exact mappings', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-mini'], [])

    expect(mapping).toEqual({
      'gpt-5.4-mini': 'gpt-5.4-mini'
    })
  })

  it('opencode 预设映射提供默认模型透传', () => {
    const mappings = getPresetMappingsByPlatform('opencode')

    expect(mappings).toEqual(expect.arrayContaining([
      expect.objectContaining({ from: 'opencode/big-pickle', to: 'opencode/big-pickle' }),
      expect.objectContaining({ from: 'opencode/gpt5-nano', to: 'opencode/gpt5-nano' }),
      expect.objectContaining({ from: 'gpt5-nano', to: 'gpt5-nano' })
    ]))
  })

  it('combined 模式会同时保留白名单身份映射和模型映射', () => {
    const mapping = buildModelMappingObject(
      'combined',
      ['gpt-5.4', 'claude-*'],
      [
        { from: 'gpt-latest', to: 'gpt-5.4' },
        { from: 'gpt-5.4', to: 'gpt-5.4-mini' }
      ]
    )

    expect(mapping).toEqual({
      'gpt-5.4': 'gpt-5.4-mini',
      'gpt-latest': 'gpt-5.4'
    })
  })

  it('splitModelMappingObject 会把身份映射还原成白名单，其余保留为映射', () => {
    const parsed = splitModelMappingObject({
      'gpt-5.4': 'gpt-5.4',
      'gpt-latest': 'gpt-5.4',
      ' ': 'gpt-empty',
      broken: 123
    })

    expect(parsed).toEqual({
      allowedModels: ['gpt-5.4'],
      modelMappings: [{ from: 'gpt-latest', to: 'gpt-5.4' }]
    })
  })
})
