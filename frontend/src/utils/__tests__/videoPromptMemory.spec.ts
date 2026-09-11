import { beforeEach, describe, expect, it, vi } from 'vitest'

import { recallVideoPrompt, rememberVideoPrompt } from '../videoPromptMemory'

describe('videoPromptMemory', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('recalls a remembered prompt and reports nothing for unknown tasks', () => {
    rememberVideoPrompt('video_abc', '  一只猫在下雨的东京街头  ')

    expect(recallVideoPrompt('video_abc')).toBe('一只猫在下雨的东京街头')
    expect(recallVideoPrompt('video_never_seen')).toBe('')
    expect(recallVideoPrompt('')).toBe('')
  })

  it('keeps the newest entries and drops the oldest past the cap', () => {
    for (let index = 0; index < 205; index += 1) {
      rememberVideoPrompt(`video_${index}`, `prompt ${index}`)
    }

    expect(recallVideoPrompt('video_0')).toBe('')
    expect(recallVideoPrompt('video_4')).toBe('')
    expect(recallVideoPrompt('video_5')).toBe('prompt 5')
    expect(recallVideoPrompt('video_204')).toBe('prompt 204')
  })

  // 重写同一个任务不该把它算成"旧"的那一条——先删后写正是为了让它回到队尾。
  it('refreshes recency when the same task is written again', () => {
    rememberVideoPrompt('video_first', 'first')
    for (let index = 0; index < 199; index += 1) {
      rememberVideoPrompt(`video_${index}`, `prompt ${index}`)
    }
    rememberVideoPrompt('video_first', 'first again')
    for (let index = 0; index < 199; index += 1) {
      rememberVideoPrompt(`video_late_${index}`, `late ${index}`)
    }

    expect(recallVideoPrompt('video_first')).toBe('first again')
  })

  it('survives unreadable storage and refuses to throw when writes are rejected', () => {
    localStorage.setItem('video-playground-prompts', 'not json at all')
    expect(recallVideoPrompt('video_abc')).toBe('')

    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError')
    })
    expect(() => rememberVideoPrompt('video_abc', 'anything')).not.toThrow()
    setItem.mockRestore()
  })

  it('ignores empty prompts so a blank bubble never masquerades as remembered text', () => {
    rememberVideoPrompt('video_blank', '   ')

    expect(recallVideoPrompt('video_blank')).toBe('')
  })
})
