import { describe, expect, it } from 'vitest'
import {
  CONCRETE_PLATFORM_OPTIONS,
  CREATABLE_GROUP_PLATFORM_OPTIONS,
  CREATABLE_PLATFORM_OPTIONS,
  GROUP_PLATFORM_OPTIONS
} from '@/constants/platforms'

const concretePlatforms = [
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok',
  'minimax',
  'kimi',
  'zhipu',
  'glm',
  'deepseek',
  'windsurf',
  'opencode_go',
  'bytedance'
]

describe('platform option catalogs', () => {
  it('exposes every concrete account platform', () => {
    expect(CONCRETE_PLATFORM_OPTIONS.map((option) => option.value)).toEqual(concretePlatforms)
  })

  it('adds composite for group-backed filters', () => {
    expect(GROUP_PLATFORM_OPTIONS.map((option) => option.value)).toEqual([
      ...concretePlatforms,
      'composite'
    ])
  })

  it('excludes the legacy glm alias from creatable/filterable pickers', () => {
    expect(CREATABLE_PLATFORM_OPTIONS.map((option) => option.value)).toEqual(
      concretePlatforms.filter((value) => value !== 'glm')
    )
    expect(CREATABLE_PLATFORM_OPTIONS.map((option) => option.value)).not.toContain('glm')
  })

  it('adds composite to the creatable/filterable catalog too', () => {
    expect(CREATABLE_GROUP_PLATFORM_OPTIONS.map((option) => option.value)).toEqual([
      ...concretePlatforms.filter((value) => value !== 'glm'),
      'composite'
    ])
    expect(CREATABLE_GROUP_PLATFORM_OPTIONS.map((option) => option.value)).not.toContain('glm')
  })
})
