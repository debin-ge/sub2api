import type { VideoTask } from '@/api/video'

/**
 * 取内容失败的分类。
 *
 * 这四种在用户那里是四件不同的事，混成一句"加载失败"会把可以自愈的（稍后重试）
 * 和根本救不回来的（链接过期）说成同一种，用户只会一直点重试。
 */
export type VideoContentFailure = 'expired' | 'busy' | 'notVideo' | 'failed'

/**
 * 把取内容的错误归类。
 *
 * 服务端把"签名链接已经失效"收敛成了 410 / video_content_expired（取内容时不带任何
 * 上游凭据，授权整个写在 URL 签名里，所以对象存储回 401/403 只可能是链接死了）。
 * 没有这个码时前端只能把上游那句英文 "The request signature expired" 原样贴给用户。
 */
export function classifyContentError(error: unknown): VideoContentFailure {
  const source = (error ?? {}) as { code?: unknown; status?: unknown }
  const code = String(source.code ?? '')
    .trim()
    .toLowerCase()
  const status = Number(source.status ?? 0)
  if (status === 410 || code === 'video_content_expired') return 'expired'
  // 内容代理有并发闸（每用户 4 / 每账号 16），超限返回 429 且不带 Retry-After。
  if (status === 429 || code === 'video_content_concurrency_limited') return 'busy'
  return 'failed'
}

/**
 * 这份字节是不是一段文本。
 *
 * blob: 地址不做容器嗅探——浏览器完全信任 Blob 上声明的 type。于是一份 HTML 或 JSON
 * 只要被贴上 video/mp4，播放器就只会回一句 MEDIA_ERR_SRC_NOT_SUPPORTED（媒体错误 4），
 * 把"我拿到的根本不是视频"说成"这个视频坏了"。内容代理认不出路径时会把请求放行到
 * 内嵌 SPA，恰好就是这种"HTTP 200 的 index.html"，所以这条判断是必要的。
 */
export function isTextualBlob(blob: Blob): boolean {
  return /^(?:text\/|application\/(?:json|xml|xhtml))/i.test(blob.type)
}

/** 空响应同样不是视频，交给播放器只会得到同一句媒体错误 4。 */
export function isPlayableBlob(blob: Blob): boolean {
  return blob.size > 0 && !isTextualBlob(blob)
}

/**
 * 网络直链与 blob 相反：<video> 会去认容器，所以上游 CDN 给 application/octet-stream
 * 时直连照放不误，同一份字节换成 blob 就是媒体错误 4。slice 带 contentType 只换标签、
 * 不拷贝数据。调用前先过 isPlayableBlob——这里只负责补标签，不负责判断内容真伪。
 */
export function playableBlob(blob: Blob): Blob {
  if (/^video\//i.test(blob.type)) return blob
  return blob.slice(0, blob.size, 'video/mp4')
}

/**
 * 按时钟判断内容是否已过期。
 *
 * expires_at 是秒级 Unix 时间戳，等于上游完成时间加上它的保留窗口（ByteDance 是 24
 * 小时）。窗口一过，直链和鉴权取回这两条路一起断，任何重试都救不回来——所以到点就
 * 该改口说"已过期"，而不是让用户对着一个永远失败的播放器点重试。
 *
 * 字段缺席（历史行、上游没给完成时间）意味着"不知道"，一律当作没过期：宁可让用户
 * 点一次重试，也不要凭空把一个还能播的视频宣布成过期。
 */
export function videoContentExpired(task: VideoTask | null | undefined, nowSeconds: number): boolean {
  const expiresAt = task?.expires_at
  if (typeof expiresAt !== 'number' || expiresAt <= 0) return false
  return nowSeconds >= expiresAt
}
