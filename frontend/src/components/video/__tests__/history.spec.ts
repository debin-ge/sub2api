import { beforeEach, describe, expect, it } from 'vitest'

import type { VideoTask } from '@/api/video'
import { rememberVideoPrompt } from '@/utils/videoPromptMemory'
import { mergeVideoHistoryCards } from '../history'
import type { VideoPlaygroundCard } from '../types'

function task(id: string, createdAt: number, overrides: Partial<VideoTask> = {}): VideoTask {
  return {
    id,
    object: 'video',
    created_at: createdAt,
    status: 'completed',
    model: 'sora-2',
    ...overrides,
  }
}

function localCard(localId: string, submittedAt: number, taskId?: string): VideoPlaygroundCard {
  return {
    localId,
    prompt: 'local prompt',
    model: 'sora-2',
    status: 'queued',
    submittedAt,
    task: taskId ? task(taskId, submittedAt, { status: 'queued' }) : null,
  }
}

describe('mergeVideoHistoryCards', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('orders the whole stream oldest first regardless of the page order', () => {
    const merged = mergeVideoHistoryCards([], [task('video_c', 300), task('video_b', 200), task('video_a', 100)])

    expect(merged.map((card) => card.localId)).toEqual(['video_a', 'video_b', 'video_c'])
  })

  // 已在流里的任务带着播放器状态与 blob 缓存，覆盖它等于让正在播的视频重新加载。
  it('never replaces a card that is already in the stream', () => {
    const existing = localCard('idempotency-key', 200, 'video_b')
    const merged = mergeVideoHistoryCards(
      [existing],
      [task('video_b', 200, { status: 'completed' }), task('video_a', 100)],
    )

    expect(merged).toHaveLength(2)
    expect(merged[1]).toBe(existing)
    expect(merged[1].status).toBe('queued')
    expect(merged[0].localId).toBe('video_a')
  })

  it('slots newer tasks after the cards already on screen', () => {
    const merged = mergeVideoHistoryCards([localCard('local', 200, 'video_b')], [task('video_c', 300)])

    expect(merged.map((card) => card.localId)).toEqual(['local', 'video_c'])
  })

  it('returns the same array when a page brings nothing new', () => {
    const cards = [localCard('local', 200, 'video_b')]

    expect(mergeVideoHistoryCards(cards, [task('video_b', 200)])).toBe(cards)
  })

  it('restores prompts remembered in this browser and leaves the rest blank', () => {
    rememberVideoPrompt('video_a', '一只猫在下雨的东京街头')
    const merged = mergeVideoHistoryCards([], [task('video_a', 100), task('video_b', 200)])

    expect(merged[0].prompt).toBe('一只猫在下雨的东京街头')
    expect(merged[1].prompt).toBe('')
  })

  // 服务端留存的那份跨浏览器、跨设备，也覆盖从 API 直接发起的任务，所以它说了算。
  it('prefers the prompt replayed by the server over this browser memory', () => {
    rememberVideoPrompt('video_a', 'stale local copy')
    const merged = mergeVideoHistoryCards([], [task('video_a', 100, { prompt: '服务端留存的原文' })])

    expect(merged[0].prompt).toBe('服务端留存的原文')
  })
})
