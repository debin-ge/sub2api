import { buildGatewayUrl } from './client'

/**
 * 视频任务的公开状态。后端 ProjectVideoStatus（video_state.go:135-152）只会投影出
 * 这四个值之一：内部的 preparing/held/submitting 一律折叠成 queued，
 * cancelled/expired 一律折叠成 failed，而 completed 还额外要求计费已 captured。
 * 换言之「completed」即「已结算」，此时 actual_cost 才可信。
 */
export type VideoTaskStatus = 'queued' | 'in_progress' | 'completed' | 'failed' | string

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

export interface VideoTask {
  id: string
  object: string
  /** 秒级 Unix 时间戳，不是毫秒。 */
  created_at: number
  completed_at?: number
  expires_at?: number
  status: VideoTaskStatus
  model?: string
  /**
   * 提交时用的提示词原文，由服务端留在 request_attributes 里随任务一起回放
   * （上限 2000 字，超出截断）。本次改动之前创建的任务没有这个字段，
   * 缺席即「服务端没存过」，此时才退回本地记忆。
   */
  prompt?: string
  progress?: number
  /** 后端以字符串回传时长（video_handler.go 用 strconv.Itoa 写入）。 */
  seconds?: string
  size?: string
  operation?: string
  /**
   * 可直接喂给 <video src>：host 已被改写到本平台，由全局中间件 PublicContentProxy
   * 免鉴权代理并支持 Range。仅在「投影状态 completed + 内容代理开启 + 有 video 变体」
   * 三条同时成立时出现。
   */
  url?: string
  error?: VideoTaskError
  content_variants?: string[]
  /**
   * 仅在任务已结算（status === 'completed'）且分组 disclosure_policy 不是 none 时出现。
   * 字段缺席意味着「尚不可知」，不等于零；不要用 ?? 0 兜底。
   */
  actual_cost?: number
  /** 历史数据可能为空，此时只展示数字，不要编造币种。 */
  currency?: string
  provider?: string
  provider_access?: VideoProviderAccess
}

export interface VideoCreateRequest {
  model: string
  prompt: string
  seconds?: number
  size?: string
  ratio?: string
  aspect_ratio?: string
  image_url?: string
  first_image_url?: string
  last_image_url?: string
  reference_images?: string[]
  reference_videos?: string[]
  reference_audios?: string[]
  /** ByteDance 的 resolution/ratio 只认这里；OpenAI 则拒收整个字段。 */
  provider_options?: Record<string, unknown>
}

/**
 * 时长的两种形态互斥：values 非空表示只能从这几个离散档位里挑；min/max 非空表示
 * 区间内任意整数秒都可以，应当渲染成滑杆。两者都缺席意味着"这一档说不准"，
 * 由调用方给自由输入。
 */
export interface VideoModelSeconds {
  values?: number[]
  min?: number
  max?: number
  default?: number
}

export interface VideoModelSizes {
  values: string[]
  default?: string
}

export interface VideoModelParameter {
  name: string
  /** 'body' 写在请求体顶层，'provider_options' 必须包进 provider_options。 */
  in: 'body' | 'provider_options' | string
  values?: string[]
  /** 同时出现会被上游硬拒（400）。 */
  conflicts_with?: string[]
  /** 这些字段一旦出现，本参数被静默忽略——应置灰并说明，而不是等一个不会来的错误。 */
  ignored_when_present?: string[]
}

export interface VideoModelReferenceInput {
  /** 创建请求体里的 JSON 键名，不是内部 input role。 */
  field: string
  kind: 'image' | 'video' | 'audio' | string
  max: number
  accepts: string[]
  /** 单条引用**字符串**的长度上限，不是被引用文件的大小上限。 */
  max_value_bytes?: number
  conflicts_with?: string[]
  /** 这些字段至少要有一个非空，本槽位才能使用。 */
  requires_any?: string[]
}

export interface VideoModel {
  id: string
  object: string
  provider: string
  canonical_model: string
  is_default?: boolean
  operations?: string[]
  content_variants?: string[]
  seconds?: VideoModelSeconds
  /** 缺席即「该模型不用 size 表达画幅」，应改用 parameters 里的 ratio/resolution。 */
  sizes?: VideoModelSizes
  parameters?: VideoModelParameter[]
  reference_inputs?: VideoModelReferenceInput[]
}

/**
 * 任务列表。游标语义有两个坑：
 *   1. after 只在 has_more 为真时才有值——空串是"到头了"，不是"从头开始"；
 *   2. status 过滤的是内部 generation_state 而不是这里 data[].status 那个投影后的
 *      公开状态，两者对不上（?status=completed 会带回仍投影为 in_progress 的行），
 *      所以本页面一律不传 status。
 */
export interface VideoTaskListResponse {
  object: string
  data: VideoTask[]
  has_more: boolean
  first_id?: string
  last_id?: string
  after?: string
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
 * 提交一次视频生成。
 *
 * idempotencyKey 是必填的，不是可选优化：创建开关关闭时不带该头会直接 403，
 * 且它是连点提交不重复扣费的唯一保证。
 */
export async function createVideo(
  apiKey: string,
  payload: VideoCreateRequest,
  idempotencyKey: string,
): Promise<VideoTask> {
  const response = await fetch(buildGatewayUrl('/v1/videos'), {
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
  const response = await fetch(buildGatewayUrl(`/v1/videos/${encodeURIComponent(taskId)}`), {
    headers: authHeaders(apiKey),
  })
  if (!response.ok) throw await parseVideoError(response)
  return response.json()
}

/**
 * 拉取历史任务。按 user 维度归属，所以换一把密钥看到的仍是同一份历史。
 *
 * 仓储层附带 source = 'managed' 硬过滤，ingress / composite 直通产生的任务不会出现，
 * 这正是本页面想要的——Playground 只认自己发起的那类任务。
 */
export async function listVideoTasks(
  apiKey: string,
  options: { limit?: number; after?: string } = {},
): Promise<VideoTaskListResponse> {
  const params = new URLSearchParams()
  if (options.limit) params.set('limit', String(options.limit))
  if (options.after) params.set('after', options.after)
  const query = params.toString()
  const response = await fetch(buildGatewayUrl(`/v1/videos${query ? `?${query}` : ''}`), {
    headers: authHeaders(apiKey),
  })
  if (!response.ok) throw await parseVideoError(response)
  return response.json()
}

/**
 * 拉取当前 Key 所属分组的可用模型与请求约束。
 *
 * 这是能力目录而非可调度性保证：未配价的模型、分组内无账号暴露的模型同样会列出，
 * 直到提交时才报错。
 */
export async function listVideoModels(apiKey: string): Promise<VideoModelsResponse> {
  const response = await fetch(buildGatewayUrl('/v1/videos/models'), {
    headers: authHeaders(apiKey),
  })
  if (!response.ok) throw await parseVideoError(response)
  return response.json()
}

/**
 * 取回任务内容本体。
 *
 * 这是 task.url 之外的第二条路，专治 url 直连播不出来的两种情形：
 *   1. url 退化成相对地址 /v1/videos/{id}/content —— 那条路由挂在 apiKeyAuth 后面，
 *      <video src> 发不出 Bearer 头，必然 401；
 *   2. url 是改写过 host 的公开直链，但内容代理没认出这个路径 —— 中间件会 c.Next()
 *      放行到内嵌 SPA，于是拿回一份 HTTP 200 的 index.html，<video> 只会报解码失败。
 *
 * 代价是整段视频进内存，所以只在直连不可用或已经失败时才调用。
 */
export async function fetchVideoContent(
  apiKey: string,
  taskId: string,
  variant = 'video',
): Promise<Blob> {
  const params = new URLSearchParams({ variant })
  const response = await fetch(
    buildGatewayUrl(`/v1/videos/${encodeURIComponent(taskId)}/content?${params.toString()}`),
    { headers: authHeaders(apiKey) },
  )
  if (!response.ok) throw await parseVideoError(response)
  return response.blob()
}

