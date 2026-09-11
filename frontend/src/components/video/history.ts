import type { VideoTask } from '@/api/video'
import { recallVideoPrompt } from '@/utils/videoPromptMemory'
import type { VideoPlaygroundCard } from './types'

export function videoHistoryCard(task: VideoTask): VideoPlaygroundCard {
  return {
    localId: task.id,
    // 提示词优先用服务端回放的那份——它跨浏览器、跨设备，也覆盖从 API 直接发起的任务。
    // 本地记忆只是兜底，管的是服务端开始留存之前创建的那批老任务。两处都没有就留空，
    // 由卡片展示"未保留"，不要拿模型名之类的东西冒充。
    prompt: task.prompt || recallVideoPrompt(task.id),
    model: task.model || '',
    status: task.status,
    submittedAt: task.created_at,
    task,
  }
}

/**
 * 把一页历史并进对话流。
 *
 * 两条规矩：
 *   · 已经在流里的任务一律不动。它们可能正带着播放器状态、blob 缓存和轮询进度，
 *     用一份新的服务端快照覆盖过去，等于把用户正在看的视频重新加载一遍。
 *   · 合并后整条流按时间重排。于是"往回翻更早的"和"刷新拿到更新的"走同一条路径，
 *     各自落到正确的位置，调用方不必区分该往头上插还是往尾上接。
 */
export function mergeVideoHistoryCards(
  cards: VideoPlaygroundCard[],
  tasks: VideoTask[],
): VideoPlaygroundCard[] {
  const known = new Set(cards.map((card) => card.task?.id).filter(Boolean))
  const added = tasks.filter((task) => task.id && !known.has(task.id)).map(videoHistoryCard)
  if (added.length === 0) return cards
  return [...cards, ...added].sort((left, right) => left.submittedAt - right.submittedAt)
}
