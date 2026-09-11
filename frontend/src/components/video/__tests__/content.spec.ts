import { describe, expect, it } from 'vitest'

import type { VideoTask } from '@/api/video'
import {
  classifyContentError,
  isPlayableBlob,
  isTextualBlob,
  playableBlob,
  videoContentExpired,
} from '../content'

function task(overrides: Partial<VideoTask> = {}): VideoTask {
  return { id: 'video_a', object: 'video', created_at: 100, status: 'completed', ...overrides }
}

describe('classifyContentError', () => {
  // 签名链接失效救不回来，并发闸稍后就好——两者让用户做的事完全相反。
  it('separates a dead link from a busy channel', () => {
    expect(classifyContentError({ status: 410 })).toBe('expired')
    expect(classifyContentError({ code: 'video_content_expired' })).toBe('expired')
    expect(classifyContentError({ status: 429 })).toBe('busy')
    expect(classifyContentError({ code: 'video_content_concurrency_limited' })).toBe('busy')
  })

  it('falls back to a generic failure for anything else', () => {
    expect(classifyContentError({ status: 502 })).toBe('failed')
    expect(classifyContentError(new Error('boom'))).toBe('failed')
    expect(classifyContentError(undefined)).toBe('failed')
  })
})

describe('playable blobs', () => {
  /**
   * 这是"媒体错误 4"的正主：代理认不出内容路径时会放行到内嵌 SPA，于是一份 HTTP 200
   * 的 index.html 被当成视频。贴上 video/mp4 只会把"拿到的不是视频"说成"视频坏了"。
   */
  it('refuses to treat a text payload as video', () => {
    expect(isTextualBlob(new Blob(['<!doctype html>'], { type: 'text/html' }))).toBe(true)
    expect(isPlayableBlob(new Blob(['{"error":{}}'], { type: 'application/json' }))).toBe(false)
    expect(isPlayableBlob(new Blob([], { type: 'video/mp4' }))).toBe(false)
    expect(isPlayableBlob(new Blob(['data'], { type: 'video/mp4' }))).toBe(true)
  })

  // 上游 CDN 常给 application/octet-stream，直连播得好好的，blob 却要靠声明的 type。
  it('labels an unlabelled binary payload so the player accepts it', () => {
    expect(playableBlob(new Blob(['data'], { type: 'application/octet-stream' })).type).toBe('video/mp4')
    expect(playableBlob(new Blob(['data'], { type: 'video/webm' })).type).toBe('video/webm')
  })
})

describe('videoContentExpired', () => {
  it('reports expiry once the retention window has passed', () => {
    expect(videoContentExpired(task({ expires_at: 500 }), 499)).toBe(false)
    expect(videoContentExpired(task({ expires_at: 500 }), 500)).toBe(true)
  })

  // 字段缺席是"不知道"，不是"已过期"——宁可让用户点一次重试。
  it('never guesses when the task carries no expiry', () => {
    expect(videoContentExpired(task(), 10_000)).toBe(false)
    expect(videoContentExpired(task({ expires_at: 0 }), 10_000)).toBe(false)
    expect(videoContentExpired(undefined, 10_000)).toBe(false)
  })
})
