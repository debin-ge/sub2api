import { computed, ref, watch } from 'vue'
import {
  listVideoModels,
  type VideoContentItem,
  type VideoCreateRequest,
  type VideoModel,
} from '@/api/video'

/** Ark 的「智能时长」：duration = -1，由模型按内容决定实际长度。 */
export const VIDEO_SMART_DURATION = -1
/** Ark 的「比例跟随首帧」。没有首帧时上游硬拒，所以必须以首帧是否已填为门。 */
export const VIDEO_RATIO_ADAPTIVE = 'adaptive'
export const VIDEO_ROLE_FIRST_FRAME = 'first_frame'

/**
 * 角色的展示顺序，也是请求体里媒体项的顺序：首帧在前、尾帧紧随、再是各类参考。
 * 目录以 map 形态给出角色，没有顺序，这里补上；目录将来新增的角色排到最后。
 */
export const VIDEO_CONTENT_ROLE_ORDER: readonly string[] = [
  'first_frame',
  'last_frame',
  'reference_image',
  'reference_video',
  'reference_audio',
]

/** 目录 flags 里的布尔开关。seed 也在 flags 里，但它是数字输入，单独处理。 */
export const VIDEO_BOOLEAN_FLAGS = ['camera_fixed', 'watermark', 'generate_audio', 'return_last_frame'] as const
export type VideoBooleanFlag = (typeof VIDEO_BOOLEAN_FLAGS)[number]

/** 表单状态。键名与 Ark 请求体一一对应，content 按角色分桶。 */
export interface VideoPlaygroundForm {
  /**
   * 选中的目录模型。Playground 只认能力目录里列出的模型——目录之外的模型没有时长、
   * 画幅、参考素材这些约束可依，表单渲染不出正确的控件，提交多半以一个上游 400 收场。
   * 需要用自定义兼容模型的，走 API 而不是这个页面。
   */
  model: string
  prompt: string
  /** 整数秒；VIDEO_SMART_DURATION 表示智能时长；null 表示不指定，交上游默认。 */
  duration: number | null
  resolution: string
  ratio: string
  /** 只记为真的开关：未勾选即不进请求体，让上游用自己的默认值。 */
  flags: Partial<Record<VideoBooleanFlag, boolean>>
  seed: number | null
  /** 角色 → URL 列表（https:// 或 data: URI） */
  content: Record<string, string[]>
  /** 厂商特有参数名 → 取值。统一存字符串，提交时 "true"/"false" 还原成布尔。 */
  providerOptions: Record<string, string>
}

export function createVideoPlaygroundForm(): VideoPlaygroundForm {
  return {
    model: '',
    prompt: '',
    duration: null,
    resolution: '',
    ratio: '',
    flags: {},
    seed: null,
    content: {},
    providerOptions: {},
  }
}

export interface VideoDurationChoices {
  /** 离散档位；非空时只能从中挑。 */
  options: number[]
  /** 连续区间；只有没有离散档位时才给，渲染成滑杆。 */
  range: { min: number; max: number } | null
  /** 接受 duration = -1。 */
  allowSmart: boolean
}

export interface VideoRatioControl {
  values: string[]
  /** 没填首帧时 adaptive 不可选。 */
  adaptiveDisabled: boolean
}

export type VideoContentKind = 'image' | 'video' | 'audio'

export interface VideoContentSlot {
  role: string
  kind: VideoContentKind
  max: number
  /** 'https' / 'data_uri' */
  accepts: string[]
  /** 这些角色都填了本槽位才可用。 */
  requires: string[]
  /** requires 里有角色还没填时为真。 */
  missingPrerequisite: boolean
}

export interface VideoProviderOptionControl {
  name: string
  /** 非空则渲染成单选；为空则自由输入。 */
  values: string[]
}

export type VideoFormIssueCode =
  | 'model_required'
  | 'prompt_required'
  | 'duration_required'
  | 'reference_requires'
  | 'reference_too_many'
  | 'reference_scheme_unsupported'
  | 'ratio_adaptive_requires_first_frame'

export interface VideoFormIssue {
  code: VideoFormIssueCode
  /** 出问题的角色名，用于把错误贴回对应控件。 */
  field?: string
  /** 参与判定的角色名列表或上限值，供文案插值。 */
  detail?: string | number
}

function isDataURI(value: string): boolean {
  return value.trim().toLowerCase().startsWith('data:')
}

function isHTTPURL(value: string): boolean {
  const normalized = value.trim().toLowerCase()
  return normalized.startsWith('https://') || normalized.startsWith('http://')
}

/** 角色决定媒体类型：视频 / 音频参考各自只有一个角色，其余都是图片。 */
export function videoContentKind(role: string): VideoContentKind {
  if (role === 'reference_video') return 'video'
  if (role === 'reference_audio') return 'audio'
  return 'image'
}

function filledContent(form: VideoPlaygroundForm, role: string): string[] {
  return (form.content[role] || []).map((value) => value.trim()).filter((value) => value !== '')
}

function roleOrder(role: string): number {
  const index = VIDEO_CONTENT_ROLE_ORDER.indexOf(role)
  return index === -1 ? VIDEO_CONTENT_ROLE_ORDER.length : index
}

/** 把表单里的字符串取值还原成 provider_options 的 JSON 标量：布尔词还原，其余原样。 */
function providerOptionValue(raw: string): unknown {
  const value = raw.trim()
  if (value === 'true') return true
  if (value === 'false') return false
  return value
}

function contentItem(slot: VideoContentSlot, url: string): VideoContentItem {
  switch (slot.kind) {
    case 'video':
      return { type: 'video_url', video_url: { url }, role: 'reference_video' }
    case 'audio':
      return { type: 'audio_url', audio_url: { url }, role: 'reference_audio' }
    default:
      return {
        type: 'image_url',
        image_url: { url },
        role: slot.role as 'first_frame' | 'last_frame' | 'reference_image',
      }
  }
}

export function useVideoPlaygroundModels(apiKey: () => string) {
  const models = ref<VideoModel[]>([])
  const loading = ref(false)
  const loadError = ref<Error | null>(null)
  const loadedForKey = ref('')
  const form = ref<VideoPlaygroundForm>(createVideoPlaygroundForm())

  // 只认最后一次请求的结果：切换 Key 时旧响应可能后到。
  let requestToken = 0

  async function loadModels(): Promise<void> {
    const key = apiKey()
    if (!key) {
      models.value = []
      loadedForKey.value = ''
      loadError.value = null
      return
    }
    const token = ++requestToken
    loading.value = true
    loadError.value = null
    try {
      const response = await listVideoModels(key)
      if (token !== requestToken) return
      models.value = response.data || []
      loadedForKey.value = key
      const current = models.value.find((model) => model.id === form.value.model)
      if (!current) {
        const fallback = models.value.find((model) => model.is_default) || models.value[0]
        selectModel(fallback ? fallback.id : '')
      }
    } catch (error) {
      if (token !== requestToken) return
      models.value = []
      loadedForKey.value = ''
      loadError.value = error as Error
    } finally {
      if (token === requestToken) loading.value = false
    }
  }

  const selectedModel = computed<VideoModel | null>(
    () => models.value.find((model) => model.id === form.value.model) || null,
  )

  /**
   * 时长三种形态全由目录决定。目录说得出离散档位时不给区间——那几档之外提交必挂，
   * 滑杆会让用户滑到一个必挂的值上。
   */
  const durationChoices = computed<VideoDurationChoices>(() => {
    const durations = selectedModel.value?.durations
    const options = durations?.values || []
    let range: { min: number; max: number } | null = null
    if (options.length === 0) {
      const min = durations?.min || 0
      const max = durations?.max || 0
      if (min > 0 && max >= min) range = { min, max }
    }
    return { options, range, allowSmart: !!durations?.allow_smart }
  })

  const resolutionOptions = computed<string[]>(() => selectedModel.value?.resolutions?.values || [])

  const hasFirstFrame = computed(() => filledContent(form.value, VIDEO_ROLE_FIRST_FRAME).length > 0)

  const ratioControl = computed<VideoRatioControl>(() => ({
    values: selectedModel.value?.ratios?.values || [],
    adaptiveDisabled: !hasFirstFrame.value,
  }))

  /** 只列目录标为 true 的开关；标 false 的提交就是 400，不如不摆出来。 */
  const flagControls = computed<VideoBooleanFlag[]>(() => {
    const flags = selectedModel.value?.flags
    if (!flags) return []
    return VIDEO_BOOLEAN_FLAGS.filter((flag) => flags[flag] === true)
  })

  const seedEnabled = computed(() => selectedModel.value?.flags?.seed === true)

  const contentSlots = computed<VideoContentSlot[]>(() => {
    const roles = selectedModel.value?.content_roles || {}
    return Object.keys(roles)
      .sort((left, right) => roleOrder(left) - roleOrder(right) || left.localeCompare(right))
      .map((role) => {
        const constraint = roles[role]
        const requires = constraint.requires || []
        return {
          role,
          kind: videoContentKind(role),
          max: constraint.max,
          accepts: constraint.accepts || [],
          requires,
          missingPrerequisite: requires.some((required) => filledContent(form.value, required).length === 0),
        }
      })
  })

  const providerOptionControls = computed<VideoProviderOptionControl[]>(() =>
    (selectedModel.value?.provider_options || []).map((option) => ({
      name: option.name,
      values: option.values || [],
    })),
  )

  function selectModel(modelId: string): void {
    const model = models.value.find((item) => item.id === modelId)
    const next = createVideoPlaygroundForm()
    next.model = modelId
    next.prompt = form.value.prompt
    next.duration =
      model?.durations?.default || model?.durations?.values?.[0] || model?.durations?.min || null
    next.resolution = model?.resolutions?.default || ''
    // adaptive 做默认值的话，没填首帧就提交必挂；留空让用户填了首帧再选。
    const ratio = model?.ratios?.default || ''
    next.ratio = ratio === VIDEO_RATIO_ADAPTIVE ? '' : ratio
    form.value = next
  }

  /**
   * 清掉因为最新一次改动而变得不可用的取值，避免提交一个自相矛盾的请求。
   *
   * 返回被清掉的参考槽位角色名。填过内容的槽位被悄悄清空是最难自证的一种"丢数据"——
   * 用户删掉首帧，已经填好的尾帧跟着消失，界面上没有任何交代。调用方拿着这份名单
   * 说一句话即可。ratio 不在名单里：它本来就是从几个选项里挑一个，重新挑一次的成本
   * 和"被清掉"的感知都远低于一串手敲的 URL。
   */
  function pruneUnavailableSelections(): string[] {
    if (form.value.ratio === VIDEO_RATIO_ADAPTIVE && !hasFirstFrame.value) {
      form.value.ratio = ''
    }
    const dropped: string[] = []
    for (const slot of contentSlots.value) {
      if (slot.missingPrerequisite && filledContent(form.value, slot.role).length > 0) {
        form.value.content[slot.role] = []
        dropped.push(slot.role)
      }
    }
    return dropped
  }

  function validate(): VideoFormIssue[] {
    const issues: VideoFormIssue[] = []
    if (!form.value.model) issues.push({ code: 'model_required' })
    if (!form.value.prompt.trim()) issues.push({ code: 'prompt_required' })
    // 只有在目录给出了档位时才强制选一个：目录说不出档位时后端对时长压根不做校验，
    // 这里再拦就是把表单锁死——用户看不到任何可选项，却永远提交不出去。
    const { options, range } = durationChoices.value
    if ((options.length > 0 || range) && form.value.duration === null) {
      issues.push({ code: 'duration_required' })
    }
    if (form.value.ratio === VIDEO_RATIO_ADAPTIVE && !hasFirstFrame.value) {
      issues.push({ code: 'ratio_adaptive_requires_first_frame', field: 'ratio' })
    }

    for (const slot of contentSlots.value) {
      const values = filledContent(form.value, slot.role)
      if (values.length === 0) continue
      if (slot.missingPrerequisite) {
        issues.push({ code: 'reference_requires', field: slot.role, detail: slot.requires.join(', ') })
        continue
      }
      if (values.length > slot.max) {
        issues.push({ code: 'reference_too_many', field: slot.role, detail: slot.max })
      }
      const acceptsData = slot.accepts.includes('data_uri')
      const acceptsHTTPS = slot.accepts.includes('https')
      for (const value of values) {
        const ok = (acceptsData && isDataURI(value)) || (acceptsHTTPS && isHTTPURL(value))
        if (!ok) {
          issues.push({ code: 'reference_scheme_unsupported', field: slot.role, detail: slot.accepts.join(', ') })
          break
        }
      }
    }
    return issues
  }

  /**
   * 组装 Ark 创建请求。只放用户真的设置了的字段：后端解码器开了 DisallowUnknownFields，
   * 而为 false 的开关、空的画幅与其省略等价——省略让上游用自己的默认值，写 false 则是
   * 替用户做了一个他没做的决定。
   */
  function buildRequest(): VideoCreateRequest {
    const content: VideoContentItem[] = [{ type: 'text', text: form.value.prompt.trim() }]
    for (const slot of contentSlots.value) {
      if (slot.missingPrerequisite) continue
      for (const url of filledContent(form.value, slot.role)) {
        content.push(contentItem(slot, url))
      }
    }
    const payload: VideoCreateRequest = { model: form.value.model, content }

    const duration = form.value.duration
    if (duration !== null && (duration > 0 || (duration === VIDEO_SMART_DURATION && durationChoices.value.allowSmart))) {
      payload.duration = duration
    }
    if (form.value.resolution) payload.resolution = form.value.resolution
    // 有首帧而比例留空：不补默认值，让比例跟随上游对首帧的处理。
    if (form.value.ratio && !(form.value.ratio === VIDEO_RATIO_ADAPTIVE && !hasFirstFrame.value)) {
      payload.ratio = form.value.ratio
    }
    for (const flag of flagControls.value) {
      if (form.value.flags[flag] === true) payload[flag] = true
    }
    if (seedEnabled.value && form.value.seed !== null && Number.isInteger(form.value.seed)) {
      payload.seed = form.value.seed
    }

    const providerOptions: Record<string, unknown> = {}
    for (const control of providerOptionControls.value) {
      const raw = (form.value.providerOptions[control.name] || '').trim()
      if (raw) providerOptions[control.name] = providerOptionValue(raw)
    }
    if (Object.keys(providerOptions).length > 0) payload.provider_options = providerOptions
    return payload
  }

  watch(apiKey, () => {
    void loadModels()
  })

  return {
    models,
    loading,
    loadError,
    loadedForKey,
    form,
    selectedModel,
    durationChoices,
    resolutionOptions,
    ratioControl,
    flagControls,
    seedEnabled,
    contentSlots,
    providerOptionControls,
    hasFirstFrame,
    loadModels,
    selectModel,
    pruneUnavailableSelections,
    validate,
    buildRequest,
    resetForm: () => {
      form.value = createVideoPlaygroundForm()
    },
  }
}
