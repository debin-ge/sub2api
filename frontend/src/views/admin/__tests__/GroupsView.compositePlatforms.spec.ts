import { describe, expect, it } from 'vitest'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'

describe('GroupsView Composite route options', () => {
  it('offers Kimi, Zhipu GLM, DeepSeek, MiniMax, ByteDance and the video vendors as route targets', () => {
    expect(CONCRETE_PLATFORM_OPTIONS.map((option) => option.value)).toEqual(
      expect.arrayContaining(['kimi', 'zhipu', 'deepseek', 'minimax', 'bytedance', 'opencode_go', 'fal', 'wavespeed', 'runware', 'flova'])
    )
  })
})
