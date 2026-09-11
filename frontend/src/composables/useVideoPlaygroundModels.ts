import { computed, ref, watch } from 'vue'
import {
  listVideoModels,
  type VideoCreateRequest,
  type VideoModel,
  type VideoModelParameter,
  type VideoModelReferenceInput,
} from '@/api/video'

/** size 在响应里是独立的一段而非 parameters 里的一项，但互斥关系用的就是这个名字。 */
export const VIDEO_SIZE_CONTROL = 'size'

/** 表单状态。framing 与 references 的键都直接是请求体里的 JSON 字段名。 */
export interface VideoPlaygroundForm {
  /**
   * 选中的目录模型。Playground 只认能力目录里列出的模型——目录之外的模型没有时长、
   * 画幅、参考素材这些约束可依，表单渲染不出正确的控件，提交多半以一个上游 400 收场。
   * 需要用自定义兼容模型的，走 API 而不是这个页面。
   */
  model: string
  prompt: string
  seconds: number | null
  /** 'size' / 'ratio' / 'aspect_ratio' / 'resolution' → 取值 */
  framing: Record<string, string>
  /** 参考素材字段名 → URL 列表 */
  references: Record<string, string[]>
}

export function createVideoPlaygroundForm(): VideoPlaygroundForm {
  return { model: '', prompt: '', seconds: null, framing: {}, references: {} }
}

/** 一个画幅控件：size 段与 parameters 里的每一项被拉平成同一种东西。 */
export interface VideoFramingControl {
  name: string
  in: 'body' | 'provider_options' | string
  values: string[]
  default: string
  /** 已被别的控件占用时为真，附带占用者名字。 */
  disabledByConflict: string
  /** 画幅由素材决定时为真，附带那个素材字段名。 */
  ignoredBy: string
  /**
   * 目录声明的「这些素材一出现本控件就失效」。即使一个都还没填也要留着：
   * 面板据此在控件还可用时就说明"仅文生视频生效"，而不是等用户填完素材
   * 才发现刚选的画幅被静默丢掉了。Ark 的 resolution/ratio 没有这层关系，
   * 数组为空，那边也就不会误挂这句话。
   */
  ignoredWhenPresent: string[]
}

export interface VideoReferenceSlot extends VideoModelReferenceInput {
  /** 与已填字段互斥时为真，附带冲突字段名。 */
  disabledByConflict: string
  /** requires_any 一个都没填时为真。 */
  missingPrerequisite: boolean
}

export type VideoFormIssueCode =
  | 'model_required'
  | 'prompt_required'
  | 'seconds_required'
  | 'reference_requires_any'
  | 'reference_conflict'
  | 'reference_too_many'
  | 'reference_value_too_large'
  | 'reference_scheme_unsupported'

export interface VideoFormIssue {
  code: VideoFormIssueCode
  /** 出问题的请求体字段名，用于把错误贴回对应控件。 */
  field?: string
  /** 参与判定的另一个字段名或上限值，供文案插值。 */
  detail?: string | number
}

function byteLength(value: string): number {
  return new TextEncoder().encode(value).length
}

function isDataURI(value: string): boolean {
  return value.trim().toLowerCase().startsWith('data:')
}

function isHTTPURL(value: string): boolean {
  const normalized = value.trim().toLowerCase()
  return normalized.startsWith('https://') || normalized.startsWith('http://')
}

/**
 * 把单向声明的冲突补成双向。目录当前两侧都写全了，但把对称性交给数据去保证
 * 意味着将来漏写一侧就会变成"A 禁 B 而 B 不禁 A"的诡异表现。
 */
function symmetricConflicts(pairs: Array<{ name: string; conflictsWith: string[] }>): Map<string, Set<string>> {
  const map = new Map<string, Set<string>>()
  const ensure = (name: string) => {
    let set = map.get(name)
    if (!set) {
      set = new Set<string>()
      map.set(name, set)
    }
    return set
  }
  for (const pair of pairs) {
    ensure(pair.name)
    for (const other of pair.conflictsWith) {
      ensure(pair.name).add(other)
      ensure(other).add(pair.name)
    }
  }
  return map
}

function hasValue(form: VideoPlaygroundForm, field: string): boolean {
  if (field === VIDEO_SIZE_CONTROL) return !!form.framing[VIDEO_SIZE_CONTROL]
  if (form.framing[field]) return true
  return (form.references[field] || []).some((value) => value.trim() !== '')
}

function filledReferences(form: VideoPlaygroundForm, field: string): string[] {
  return (form.references[field] || []).map((value) => value.trim()).filter((value) => value !== '')
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

  const secondsOptions = computed<number[]>(() => selectedModel.value?.seconds?.values || [])

  /**
   * 自由时长区间。目录说得出离散档位时（Sora 的 4/8/12/16/20、或运营在价目里逐条
   * 列出的档位）不给区间——那几档之外提交必挂，滑杆会让用户滑到一个必挂的值上。
   */
  const secondsRange = computed<{ min: number; max: number } | null>(() => {
    if (secondsOptions.value.length > 0) return null
    const seconds = selectedModel.value?.seconds
    const min = seconds?.min || 0
    const max = seconds?.max || 0
    if (min <= 0 || max < min) return null
    return { min, max }
  })

  /**
   * 把 sizes 段与 parameters 拉平成一组同质的画幅控件，再按数据里的互斥关系
   * 逐个判定可用性。OpenAI 侧 size/ratio/aspect_ratio 三者互斥，ByteDance 侧
   * resolution/ratio 彼此独立且都要进 provider_options——两种形态由同一段代码
   * 派生，不做平台分支。
   */
  const framingControls = computed<VideoFramingControl[]>(() => {
    const model = selectedModel.value
    if (!model) return []
    const parameters: VideoModelParameter[] = model.parameters || []
    const declared: Array<{ name: string; conflictsWith: string[] }> = parameters.map((parameter) => ({
      name: parameter.name,
      conflictsWith: parameter.conflicts_with || [],
    }))
    const conflicts = symmetricConflicts(declared)

    const controls: VideoFramingControl[] = []
    if (model.sizes && model.sizes.values.length > 0) {
      controls.push({
        name: VIDEO_SIZE_CONTROL,
        in: 'body',
        values: model.sizes.values,
        default: model.sizes.default || '',
        disabledByConflict: '',
        ignoredBy: '',
        ignoredWhenPresent: [],
      })
    }
    for (const parameter of parameters) {
      controls.push({
        name: parameter.name,
        in: parameter.in,
        values: parameter.values || [],
        default: '',
        disabledByConflict: '',
        ignoredBy: '',
        ignoredWhenPresent: parameter.ignored_when_present || [],
      })
    }

    for (const control of controls) {
      const rivals = conflicts.get(control.name)
      if (rivals) {
        for (const rival of rivals) {
          if (rival !== control.name && hasValue(form.value, rival)) {
            control.disabledByConflict = rival
            break
          }
        }
      }
      for (const field of control.ignoredWhenPresent) {
        if (hasValue(form.value, field)) {
          control.ignoredBy = field
          break
        }
      }
    }
    return controls
  })

  const referenceSlots = computed<VideoReferenceSlot[]>(() => {
    const inputs = selectedModel.value?.reference_inputs || []
    const conflicts = symmetricConflicts(
      inputs.map((input) => ({ name: input.field, conflictsWith: input.conflicts_with || [] })),
    )
    return inputs.map((input) => {
      let disabledByConflict = ''
      for (const rival of conflicts.get(input.field) || []) {
        if (rival !== input.field && hasValue(form.value, rival)) {
          disabledByConflict = rival
          break
        }
      }
      const requires = input.requires_any || []
      const missingPrerequisite =
        requires.length > 0 && !requires.some((field) => hasValue(form.value, field))
      return { ...input, disabledByConflict, missingPrerequisite }
    })
  })

  function selectModel(modelId: string): void {
    form.value.model = modelId
    form.value.framing = {}
    form.value.references = {}
    const model = models.value.find((item) => item.id === modelId)
    form.value.seconds =
      model?.seconds?.default || model?.seconds?.values?.[0] || model?.seconds?.min || null
  }

  /**
   * 清掉因为最新一次改动而变得不可用的控件取值，避免提交一个自相矛盾的请求。
   *
   * 返回被清掉的参考槽位字段名。填过内容的槽位被悄悄清空是最难自证的一种"丢数据"——
   * 用户删掉最后一张参考图，已经填好的参考音频跟着消失，界面上没有任何交代。调用方
   * 拿着这份名单说一句话即可。画幅控件不在名单里：它本来就是从几个选项里挑一个，
   * 重新挑一次的成本和"被清掉"的感知都远低于一串手敲的 URL。
   */
  function pruneUnavailableSelections(): string[] {
    for (const control of framingControls.value) {
      if ((control.disabledByConflict || control.ignoredBy) && form.value.framing[control.name]) {
        delete form.value.framing[control.name]
      }
    }
    const dropped: string[] = []
    for (const slot of referenceSlots.value) {
      if ((slot.disabledByConflict || slot.missingPrerequisite) && filledReferences(form.value, slot.field).length > 0) {
        form.value.references[slot.field] = []
        dropped.push(slot.field)
      }
    }
    return dropped
  }

  function validate(): VideoFormIssue[] {
    const issues: VideoFormIssue[] = []
    if (!form.value.model) issues.push({ code: 'model_required' })
    if (!form.value.prompt.trim()) issues.push({ code: 'prompt_required' })
    // 只有在目录给出了档位时才强制选一个：目录说不出档位（自定义兼容模型、或按秒计价
    // 的价目没有逐条列 conditions.seconds）时，后端对时长压根不做校验，这里再拦就是把
    // 表单锁死——用户看不到任何可选项，却永远提交不出去。
    if ((secondsOptions.value.length > 0 || secondsRange.value) && !form.value.seconds) {
      issues.push({ code: 'seconds_required' })
    }

    for (const slot of referenceSlots.value) {
      const values = filledReferences(form.value, slot.field)
      if (values.length === 0) continue
      if (slot.disabledByConflict) {
        issues.push({ code: 'reference_conflict', field: slot.field, detail: slot.disabledByConflict })
        continue
      }
      if (slot.missingPrerequisite) {
        issues.push({
          code: 'reference_requires_any',
          field: slot.field,
          detail: (slot.requires_any || []).join(', '),
        })
        continue
      }
      if (values.length > slot.max) {
        issues.push({ code: 'reference_too_many', field: slot.field, detail: slot.max })
      }
      for (const value of values) {
        // 这是引用字符串本身的长度上限，不是被引用文件的大小上限。
        if (slot.max_value_bytes && byteLength(value) > slot.max_value_bytes) {
          issues.push({ code: 'reference_value_too_large', field: slot.field, detail: slot.max_value_bytes })
          break
        }
        const acceptsData = slot.accepts.includes('data_uri')
        const acceptsHTTPS = slot.accepts.includes('https')
        const ok = (acceptsData && isDataURI(value)) || (acceptsHTTPS && isHTTPURL(value))
        if (!ok) {
          issues.push({
            code: 'reference_scheme_unsupported',
            field: slot.field,
            detail: slot.accepts.join(', '),
          })
          break
        }
      }
    }
    return issues
  }

  /**
   * 组装创建请求。参数归属完全由目录里的 `in` 决定：body 的直接写在顶层，
   * provider_options 的包进 provider_options——OpenAI 拒收整个 provider_options，
   * 而 Ark 的 resolution/ratio 只认它，写死任一侧都会让另一侧硬 400。
   *
   * 顶层键必须是 videoJSONRequest 上真实存在的字段：后端解码器开了
   * DisallowUnknownFields，目录若哪天新增一个后端未接的 body 参数，会以 400 呈现。
   */
  function buildRequest(): VideoCreateRequest {
    const payload: Record<string, unknown> = {
      model: form.value.model,
      prompt: form.value.prompt.trim(),
    }
    if (form.value.seconds) payload.seconds = form.value.seconds

    const providerOptions: Record<string, unknown> = {}
    for (const control of framingControls.value) {
      const value = form.value.framing[control.name]
      if (!value || control.disabledByConflict || control.ignoredBy) continue
      if (control.in === 'provider_options') providerOptions[control.name] = value
      else payload[control.name] = value
    }
    if (Object.keys(providerOptions).length > 0) payload.provider_options = providerOptions

    for (const slot of referenceSlots.value) {
      if (slot.disabledByConflict || slot.missingPrerequisite) continue
      const values = filledReferences(form.value, slot.field)
      if (values.length === 0) continue
      // max 为 1 的槽位在请求体里是字符串，其余是数组——由 max 派生而非按字段名硬编码。
      payload[slot.field] = slot.max === 1 ? values[0] : values
    }
    return payload as unknown as VideoCreateRequest
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
    secondsOptions,
    secondsRange,
    framingControls,
    referenceSlots,
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
