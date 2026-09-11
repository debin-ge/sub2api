import type { VideoTask, VideoTaskStatus } from '@/api/video'

/** Playground 流里的一张卡片：一次提交及其后续状态。 */
export interface VideoPlaygroundCard {
  /**
   * 稳定的本地 key。提交时用幂等键占位，拿到任务后仍保持不变——
   * 换成 task.id 会让 v-for 重建整个卡片，播放器和播放进度都会被丢掉。
   */
  localId: string
  prompt: string
  model: string
  status: VideoTaskStatus
  /** 秒级时间戳，与后端 created_at 同一量纲。 */
  submittedAt: number
  task: VideoTask | null
  /** 提交本身就失败（还没有任务）时的兜底展示。 */
  submitError?: { code: string; message: string }
}
