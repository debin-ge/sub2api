import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { VideoModel } from '@/api/video'

const listVideoModels = vi.fn()

vi.mock('@/api/video', () => ({
  listVideoModels: (key: string) => listVideoModels(key),
}))

const { useVideoPlaygroundModels, VIDEO_SMART_DURATION } = await import('../useVideoPlaygroundModels')

const IMAGE_ACCEPTS = ['https', 'data_uri']

/** 与 backend ByteDanceVideoProvider.ArkModelSpec 同形：全部开关、全部角色、智能时长。 */
const SEEDANCE: VideoModel = {
  id: 'doubao-seedance-1-0-pro-250528', object: 'video.model', provider: 'bytedance', is_default: true,
  durations: { values: [5, 10], default: 5, allow_smart: true },
  frames: { min: 29, max: 289, step: 4 },
  resolutions: { values: ['480p', '720p', '1080p'], default: '720p' },
  ratios: { values: ['16:9', '9:16', '1:1', 'adaptive'], default: '16:9' },
  flags: { seed: true, camera_fixed: true, watermark: true, generate_audio: true, return_last_frame: true },
  content_roles: {
    first_frame: { max: 1, accepts: IMAGE_ACCEPTS },
    last_frame: { max: 1, accepts: IMAGE_ACCEPTS, requires: ['first_frame'] },
    reference_image: { max: 9, accepts: IMAGE_ACCEPTS },
    reference_video: { max: 3, accepts: ['https'] },
    reference_audio: { max: 3, accepts: ['https'] },
  },
}

/** 与 VideoVendorProvider.ArkModelSpec 同形：区间时长、部分开关、厂商参数、只有首尾帧。 */
const VENDOR: VideoModel = {
  id: 'fal-ai/veo3.1', object: 'video.model', provider: 'fal',
  durations: { min: 4, max: 8, default: 8 },
  resolutions: { values: ['720p', '1080p'], default: '1080p' },
  ratios: { values: ['16:9', '9:16'], default: '16:9' },
  flags: { seed: true, camera_fixed: false, watermark: false, generate_audio: true, return_last_frame: false },
  content_roles: {
    first_frame: { max: 1, accepts: IMAGE_ACCEPTS },
    last_frame: { max: 1, accepts: IMAGE_ACCEPTS, requires: ['first_frame'] },
  },
  provider_options: [{ name: 'enhance_prompt', values: ['true', 'false'] }, { name: 'negative_prompt' }],
}

async function playground(models: VideoModel[]) {
  listVideoModels.mockResolvedValue({ object: 'list', data: models })
  const state = useVideoPlaygroundModels(() => 'sk-test')
  await state.loadModels()
  return state
}

function slot(state: Awaited<ReturnType<typeof playground>>, role: string) {
  const found = state.contentSlots.value.find((item) => item.role === role)
  if (!found) throw new Error(`missing content slot ${role}`)
  return found
}

describe('useVideoPlaygroundModels (Ark catalog)', () => {
  beforeEach(() => {
    listVideoModels.mockReset()
  })

  it('selects the default model and seeds the form from the catalog defaults', async () => {
    const state = await playground([VENDOR, SEEDANCE])

    expect(state.form.value.model).toBe(SEEDANCE.id)
    expect(state.form.value.duration).toBe(5)
    expect(state.form.value.resolution).toBe('720p')
    expect(state.form.value.ratio).toBe('16:9')
    expect(state.durationChoices.value).toEqual({ options: [5, 10], range: null, allowSmart: true })
    expect(state.flagControls.value).toEqual(['camera_fixed', 'watermark', 'generate_audio', 'return_last_frame'])
    expect(state.seedEnabled.value).toBe(true)
    expect(state.contentSlots.value.map((item) => item.role)).toEqual([
      'first_frame', 'last_frame', 'reference_image', 'reference_video', 'reference_audio',
    ])
  })

  it('builds the Ark body with a leading text item and role-tagged media items', async () => {
    const state = await playground([SEEDANCE])
    state.form.value.prompt = '  海浪轻轻拍打沙滩  '
    state.form.value.duration = 10
    state.form.value.resolution = '1080p'
    state.form.value.ratio = '9:16'
    state.form.value.content.first_frame = [' https://example.com/first.png ']
    state.form.value.content.last_frame = ['data:image/png;base64,AAAA']
    state.form.value.content.reference_video = ['https://example.com/move.mp4']
    state.form.value.content.reference_audio = ['https://example.com/score.mp3']
    state.form.value.flags.camera_fixed = true
    state.form.value.flags.watermark = false
    state.form.value.seed = 11

    expect(state.validate()).toEqual([])
    expect(state.buildRequest()).toEqual({
      model: SEEDANCE.id,
      content: [
        { type: 'text', text: '海浪轻轻拍打沙滩' },
        { type: 'image_url', image_url: { url: 'https://example.com/first.png' }, role: 'first_frame' },
        { type: 'image_url', image_url: { url: 'data:image/png;base64,AAAA' }, role: 'last_frame' },
        { type: 'video_url', video_url: { url: 'https://example.com/move.mp4' }, role: 'reference_video' },
        { type: 'audio_url', audio_url: { url: 'https://example.com/score.mp3' }, role: 'reference_audio' },
      ],
      duration: 10,
      resolution: '1080p',
      ratio: '9:16',
      camera_fixed: true,
      seed: 11,
    })
  })

  // 为 false 的开关与省略等价，写出来只是替用户做了一个他没做的决定；目录没放行的开关
  // 更不能出现，那是上游的硬 400。
  it('omits flags that are unset, false, or not offered by the catalog', async () => {
    const state = await playground([VENDOR])
    state.form.value.prompt = 'a cat'
    state.form.value.flags.generate_audio = false
    state.form.value.flags.camera_fixed = true
    state.form.value.flags.return_last_frame = true

    const payload = state.buildRequest()
    expect(payload).not.toHaveProperty('generate_audio')
    expect(payload).not.toHaveProperty('camera_fixed')
    expect(payload).not.toHaveProperty('return_last_frame')
    expect(payload).not.toHaveProperty('seed')
    expect(payload).not.toHaveProperty('provider_options')

    state.form.value.flags.generate_audio = true
    expect(state.buildRequest().generate_audio).toBe(true)
  })

  it('offers smart duration only when the catalog allows it', async () => {
    const seedance = await playground([SEEDANCE])
    seedance.form.value.prompt = 'a cat'
    seedance.form.value.duration = VIDEO_SMART_DURATION
    expect(seedance.validate()).toEqual([])
    expect(seedance.buildRequest().duration).toBe(-1)

    const vendor = await playground([VENDOR])
    vendor.form.value.prompt = 'a cat'
    expect(vendor.durationChoices.value).toEqual({ options: [], range: { min: 4, max: 8 }, allowSmart: false })
    vendor.form.value.duration = VIDEO_SMART_DURATION
    expect(vendor.buildRequest()).not.toHaveProperty('duration')
  })

  it('requires a duration only when the catalog constrains it', async () => {
    const state = await playground([VENDOR])
    state.form.value.prompt = 'a cat'
    state.form.value.duration = null
    expect(state.validate()).toContainEqual({ code: 'duration_required' })

    const free = await playground([{ ...VENDOR, durations: undefined }])
    free.form.value.prompt = 'a cat'
    expect(free.form.value.duration).toBeNull()
    expect(free.validate()).toEqual([])
  })

  it('gates adaptive ratio behind a first frame and drops it when the frame goes away', async () => {
    const state = await playground([SEEDANCE])
    state.form.value.prompt = 'a cat'

    expect(state.ratioControl.value.adaptiveDisabled).toBe(true)
    state.form.value.ratio = 'adaptive'
    expect(state.validate()).toContainEqual(
      expect.objectContaining({ code: 'ratio_adaptive_requires_first_frame' }),
    )
    // 被拒的比例不能混进请求体：上游会硬 400。
    expect(state.buildRequest()).not.toHaveProperty('ratio')

    state.form.value.content.first_frame = ['https://example.com/first.png']
    expect(state.ratioControl.value.adaptiveDisabled).toBe(false)
    expect(state.validate()).toEqual([])
    expect(state.buildRequest().ratio).toBe('adaptive')

    state.form.value.content.first_frame = []
    expect(state.pruneUnavailableSelections()).toEqual([])
    expect(state.form.value.ratio).toBe('')
  })

  // 有首帧而比例留空：不补目录默认值，让比例跟随上游对首帧的处理。
  it('leaves ratio out when a first frame is present and the user cleared it', async () => {
    const state = await playground([SEEDANCE])
    state.form.value.prompt = 'a cat'
    state.form.value.ratio = ''
    state.form.value.content.first_frame = ['https://example.com/first.png']

    expect(state.buildRequest()).not.toHaveProperty('ratio')
  })

  it('does not preselect an adaptive catalog default', async () => {
    const state = await playground([{ ...SEEDANCE, ratios: { values: ['adaptive', '16:9'], default: 'adaptive' } }])

    expect(state.form.value.ratio).toBe('')
  })

  it('gates the last frame behind the first frame and reports what it cleared', async () => {
    const state = await playground([SEEDANCE])
    state.form.value.prompt = 'a cat'

    expect(slot(state, 'last_frame').missingPrerequisite).toBe(true)
    state.form.value.content.last_frame = ['https://example.com/last.png']
    expect(state.validate()).toContainEqual(
      expect.objectContaining({ code: 'reference_requires', field: 'last_frame', detail: 'first_frame' }),
    )
    // 前置不成立的素材不能进请求体。
    expect(state.buildRequest().content).toHaveLength(1)

    state.form.value.content.first_frame = ['https://example.com/first.png']
    expect(slot(state, 'last_frame').missingPrerequisite).toBe(false)
    expect(state.validate()).toEqual([])

    // 删掉首帧：尾帧随之失效，被清掉的角色要报出来，不能悄悄吃掉用户敲的地址。
    state.form.value.content.first_frame = []
    expect(state.pruneUnavailableSelections()).toEqual(['last_frame'])
    expect(state.form.value.content.last_frame).toEqual([])
  })

  it('enforces per-role limits and accepted schemes', async () => {
    const state = await playground([SEEDANCE])
    state.form.value.prompt = 'a cat'
    state.form.value.content.reference_video = ['data:video/mp4;base64,AAAA']
    state.form.value.content.first_frame = ['https://example.com/a.png', 'https://example.com/b.png']

    const issues = state.validate()
    expect(issues).toContainEqual(
      expect.objectContaining({ code: 'reference_scheme_unsupported', field: 'reference_video', detail: 'https' }),
    )
    expect(issues).toContainEqual(
      expect.objectContaining({ code: 'reference_too_many', field: 'first_frame', detail: 1 }),
    )
  })

  it('packs vendor-specific options into provider_options with boolean words restored', async () => {
    const state = await playground([VENDOR])
    state.form.value.prompt = 'a cat'
    expect(state.providerOptionControls.value).toEqual([
      { name: 'enhance_prompt', values: ['true', 'false'] },
      { name: 'negative_prompt', values: [] },
    ])

    state.form.value.providerOptions.enhance_prompt = 'true'
    state.form.value.providerOptions.negative_prompt = '  blurry  '
    expect(state.buildRequest().provider_options).toEqual({ enhance_prompt: true, negative_prompt: 'blurry' })

    // 没有厂商参数的模型不能带 provider_options：空对象也会被当成未知参数。
    const seedance = await playground([SEEDANCE])
    seedance.form.value.prompt = 'a cat'
    expect(seedance.providerOptionControls.value).toEqual([])
    expect(seedance.buildRequest()).not.toHaveProperty('provider_options')
  })

  it('resets everything but the prompt when switching models', async () => {
    const state = await playground([SEEDANCE, VENDOR])
    state.form.value.prompt = 'keep me'
    state.form.value.content.reference_video = ['https://example.com/move.mp4']
    state.form.value.flags.camera_fixed = true

    state.selectModel(VENDOR.id)

    expect(state.form.value).toEqual({
      model: VENDOR.id,
      prompt: 'keep me',
      duration: 8,
      resolution: '1080p',
      ratio: '16:9',
      flags: {},
      seed: null,
      content: {},
      providerOptions: {},
    })
  })
})
