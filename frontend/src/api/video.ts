import { buildGatewayUrl } from './client'

/**
 * 五家托管视频平台（ByteDance / fal / WaveSpeed / Runware / Flova）对外采用火山方舟
 * （Ark / Seedance）的任务协议：`{prefix}/contents/generations/tasks`。本模块只服务这一形态；
 * OpenAI（Sora）与 Grok 仍走 `/v1/videos`，不在这里。
 *
 * 四个前缀 `/api/v3`、`/v3`、`/v1`、空 互为别名，这里统一用 `/api/v3`。
 */
const ARK_TASKS_PATH = '/api/v3/contents/generations/tasks'
const ARK_MODELS_PATH = '/api/v3/contents/generations/models'

/**
 * 任务的公开状态（Ark 词表）。后端 ProjectArkVideoStatus 的映射规则：
 *   · 内部 preparing/held/submitting → queued；
 *   · completed 且计费已 captured → succeeded，计费还在 capture_pending → running；
 *   · failed / cancelled / expired 一一对应，不再折叠成 failed。
 * 换言之「succeeded」即「已结算」，此时 actual_cost 才可信。
 */
export type VideoTaskStatus =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'cancelled'
  | 'expired'
  | string

export const VIDEO_TERMINAL_FAILURE_STATUSES: readonly string[] = ['failed', 'cancelled', 'expired']

export function isVideoTaskPending(status: VideoTaskStatus): boolean {
  return status === 'queued' || status === 'running'
}

/** 失败、取消、过期：都已结束且没有成片，但三者原因不同，文案要分开说。 */
export function isVideoTaskFailure(status: VideoTaskStatus): boolean {
  return VIDEO_TERMINAL_FAILURE_STATUSES.includes(status)
}

export interface VideoTaskError {
  code: string
  message: string
}

export interface VideoProviderAccess {
  kind: string
  value: string
  scope: string
  expires_at?: number
}

export interface VideoTaskContent {
  /**
   * 带签名、会过期的本地代理直链（`.../tasks/{id}/content?variant=video&exp=&sig=`），
   * 不挂 API Key：可直接喂给 <video src>，也可以 fetch 成 blob。仅在 succeeded 且内容代理
   * 开启且有 video 变体时出现。签名过期后重新 GET 任务即可拿到新链接。
   */
  video_url?: string
  /** 仅 ByteDance 且 return_last_frame=true 时出现。 */
  last_frame_url?: string
}

export interface VideoTaskUsage {
  completion_tokens?: number
  total_tokens?: number
}

/** Ark「查询视频生成任务」响应体，外加几处 sub2api 扩展字段。 */
export interface VideoTask {
  id: string
  model?: string
  status: VideoTaskStatus
  /** 秒级 Unix 时间戳，不是毫秒。 */
  created_at: number
  updated_at?: number
  resolution?: string
  ratio?: string
  /** 整数秒。智能时长的任务在上游回报实际时长前可能缺席。 */
  duration?: number
  frames?: number
  framespersecond?: number
  seed?: number
  camera_fixed?: boolean
  watermark?: boolean
  generate_audio?: boolean
  return_last_frame?: boolean
  service_tier?: string
  execution_expires_after?: number
  revised_prompt?: string
  content?: VideoTaskContent
  usage?: VideoTaskUsage
  /** 仅 failed / cancelled / expired 时出现。 */
  error?: VideoTaskError
  /**
   * sub2api 扩展：提交时的提示词原文，由服务端留在 request_attributes 里随任务回放
   * （上限 2000 字）。缺席即「服务端没存过」，此时才退回本地记忆。
   */
  prompt?: string
  /**
   * sub2api 扩展：仅在任务已结算（status === 'succeeded'）且分组 disclosure_policy 不是
   * none 时出现。字段缺席意味着「尚不可知」，不等于零；不要用 ?? 0 兜底。
   */
  actual_cost?: number
  /** 历史数据可能为空，此时只展示数字，不要编造币种。 */
  currency?: string
  provider?: string
  provider_access?: VideoProviderAccess
}

/** content[] 媒体项的角色。Ark 规定 image_url 缺省为 first_frame。 */
export type VideoContentRole =
  | 'first_frame'
  | 'last_frame'
  | 'reference_image'
  | 'reference_video'
  | 'reference_audio'

export interface VideoContentTextItem {
  type: 'text'
  text: string
}

export interface VideoContentImageItem {
  type: 'image_url'
  image_url: { url: string }
  role?: 'first_frame' | 'last_frame' | 'reference_image'
}

export interface VideoContentVideoItem {
  type: 'video_url'
  video_url: { url: string }
  role?: 'reference_video'
}

export interface VideoContentAudioItem {
  type: 'audio_url'
  audio_url: { url: string }
  role?: 'reference_audio'
}

export type VideoContentItem =
  | VideoContentTextItem
  | VideoContentImageItem
  | VideoContentVideoItem
  | VideoContentAudioItem

/**
 * Ark「创建视频生成任务」请求体。只放用户真的设置了的字段：后端解码器开了
 * DisallowUnknownFields，顶层出现这里没有的键会以 400 收场。
 */
export interface VideoCreateRequest {
  model: string
  /** 非空，至少一个 text 项；媒体项的 URL 接受 https:// 或 data: URI（后者仅图片）。 */
  content: VideoContentItem[]
  /** 整数秒；-1 表示智能时长（仅目录 allow_smart 的模型）；与 frames 二选一。 */
  duration?: number
  frames?: number
  resolution?: string
  /** "adaptive" 只允许在带 first_frame 时出现。 */
  ratio?: string
  seed?: number
  camera_fixed?: boolean
  watermark?: boolean
  generate_audio?: boolean
  return_last_frame?: boolean
  service_tier?: string
  execution_expires_after?: number
  callback_url?: string
  /** sub2api 扩展：厂商特有参数，键以目录 provider_options 为准。 */
  provider_options?: Record<string, unknown>
}

export interface VideoCreateResponse {
  id: string
}

/**
 * 时长的两种形态互斥：values 非空表示只能从这几个离散档位里挑；min/max 非空表示
 * 区间内任意整数秒都可以，应当渲染成滑杆。两者都缺席意味着"这一档说不准"，
 * 由调用方给自由输入。allow_smart 为真时额外接受 duration = -1（智能时长）。
 */
export interface VideoModelDurations {
  values?: number[]
  min?: number
  max?: number
  default?: number
  allow_smart?: boolean
}

/** frames 的取值规则（25 + 4n）。只有 ByteDance 给出。 */
export interface VideoModelFrames {
  min: number
  max: number
  step: number
}

export interface VideoModelChoices {
  values: string[]
  default?: string
}

/** 模型接受的 Ark 开关字段。为 false 的开关提交会被 400。 */
export interface VideoModelFlags {
  seed?: boolean
  camera_fixed?: boolean
  watermark?: boolean
  generate_audio?: boolean
  return_last_frame?: boolean
}

export interface VideoModelContentRole {
  max: number
  /** 'https' / 'data_uri'。 */
  accepts: string[]
  /** 这些角色都填了，本角色才能使用。 */
  requires?: string[]
}

export interface VideoModelProviderOption {
  name: string
  values?: string[]
}

/** 能力目录条目（sub2api 扩展，Ark 自己没有目录接口）。 */
export interface VideoModel {
  id: string
  object: string
  provider: string
  is_default?: boolean
  durations?: VideoModelDurations
  frames?: VideoModelFrames
  resolutions?: VideoModelChoices
  ratios?: VideoModelChoices
  flags?: VideoModelFlags
  content_roles?: Record<string, VideoModelContentRole>
  provider_options?: VideoModelProviderOption[]
}

/** 任务列表：Ark 的页码分页。total 只在翻到末页时给出（仓储是游标分页，没有总数查询）。 */
export interface VideoTaskListResponse {
  items: VideoTask[]
  page_num: number
  page_size: number
  has_more: boolean
  total?: number
}

export interface VideoModelsResponse {
  object: string
  data: VideoModel[]
}

async function parseVideoError(response: Response): Promise<Error> {
  try {
    const body = await response.json()
    const message = body?.error?.message || body?.message || response.statusText
    const error = new Error(message)
    ;(error as any).code = body?.error?.code || response.status
    ;(error as any).status = response.status
    ;(error as any).requestId = response.headers.get('X-Request-Id') || ''
    ;(error as any).retryAfter = response.headers.get('Retry-After') || ''
    return error
  } catch {
    const error = new Error(response.statusText || `HTTP ${response.status}`)
    ;(error as any).code = response.status
    ;(error as any).status = response.status
    ;(error as any).requestId = response.headers.get('X-Request-Id') || ''
    ;(error as any).retryAfter = response.headers.get('Retry-After') || ''
    return error
  }
}

function authHeaders(apiKey: string, extra?: HeadersInit): HeadersInit {
  return {
    Authorization: `Bearer ${apiKey}`,
    ...extra,
  }
}

/**
 * 提交一次视频生成，应答只有 `{ id }`；状态与成片要靠后续 GET。
 *
 * idempotencyKey 是必填的，不是可选优化：创建开关关闭时不带该头会直接 403，
 * 且它是连点提交不重复扣费的唯一保证。
 */
export async function createVideo(
  apiKey: string,
  payload: VideoCreateRequest,
  idempotencyKey: string,
): Promise<VideoCreateResponse> {
  const response = await fetch(buildGatewayUrl(ARK_TASKS_PATH), {
    method: 'POST',
    headers: authHeaders(apiKey, {
      'Content-Type': 'application/json',
      'Idempotency-Key': idempotencyKey,
    }),
    body: JSON.stringify(payload),
  })
  if (!response.ok) throw await parseVideoError(response)
  return response.json()
}

export async function getVideoTask(apiKey: string, taskId: string): Promise<VideoTask> {
  const response = await fetch(buildGatewayUrl(`${ARK_TASKS_PATH}/${encodeURIComponent(taskId)}`), {
    headers: authHeaders(apiKey),
  })
  if (!response.ok) throw await parseVideoError(response)
  return response.json()
}

/**
 * 拉取历史任务。按 user 维度归属，所以换一把密钥看到的仍是同一份历史。
 *
 * 只会列出五家 Ark 形态 provider 的任务；status 过滤按投影后的公开状态做，与 items[].status
 * 的词表一致，可以放心传。
 */
export async function listVideoTasks(
  apiKey: string,
  options: { pageNum?: number; pageSize?: number; status?: string; model?: string } = {},
): Promise<VideoTaskListResponse> {
  const params = new URLSearchParams()
  if (options.pageNum) params.set('page_num', String(options.pageNum))
  if (options.pageSize) params.set('page_size', String(options.pageSize))
  if (options.status) params.set('filter.status', options.status)
  if (options.model) params.set('filter.model', options.model)
  const query = params.toString()
  const response = await fetch(buildGatewayUrl(`${ARK_TASKS_PATH}${query ? `?${query}` : ''}`), {
    headers: authHeaders(apiKey),
  })
  if (!response.ok) throw await parseVideoError(response)
  return response.json()
}

/**
 * 拉取当前 Key 所属分组可按 Ark 形态提交的模型与请求约束。
 *
 * 这是能力目录而非可调度性保证：未配价的模型、分组内无账号暴露的模型同样会列出，
 * 直到提交时才报错。
 */
export async function listVideoModels(apiKey: string): Promise<VideoModelsResponse> {
  const response = await fetch(buildGatewayUrl(ARK_MODELS_PATH), {
    headers: authHeaders(apiKey),
  })
  if (!response.ok) throw await parseVideoError(response)
  return response.json()
}

/**
 * 按签名直链取回内容本体。授权整个写在 URL 签名里，所以**不带** Authorization 头——
 * 带了反而会让跨域预检多跑一趟。
 *
 * 这是 <video src> 直连之外的第二条路，专治直连播不出来的情形（代理认不出路径时会
 * c.Next() 放行到内嵌 SPA，拿回一份 HTTP 200 的 index.html，<video> 只会报解码失败）。
 * 代价是整段视频进内存，所以只在直连不可用或已经失败时才调用。
 */
export async function fetchVideoContent(contentUrl: string): Promise<Blob> {
  const response = await fetch(contentUrl)
  if (!response.ok) throw await parseVideoError(response)
  return response.blob()
}
