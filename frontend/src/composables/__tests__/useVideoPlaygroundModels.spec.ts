import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { VideoModel, VideoModelReferenceInput } from '@/api/video'

const listVideoModels = vi.fn()

vi.mock('@/api/video', () => ({
  listVideoModels: (key: string) => listVideoModels(key),
}))

const { useVideoPlaygroundModels } = await import('../useVideoPlaygroundModels')

const MAX_BYTES = 512 * 1024

/**
 * 与 backend video_model_catalog.go 的静态槽位表同形。六条规则（API 文档 §4.3）
 * 全部编码在这张表里，前端不做任何硬编码判断，所以这里改错一处，下面的用例会炸。
 */
function referenceInputs(accepts: string[]): VideoModelReferenceInput[] {
  const image = [...accepts]
  return [
    { field: 'image_url', kind: 'image', max: 1, accepts: image, max_value_bytes: MAX_BYTES,
      conflicts_with: ['first_image_url', 'last_image_url'] },
    { field: 'first_image_url', kind: 'image', max: 1, accepts: image, max_value_bytes: MAX_BYTES,
      conflicts_with: ['image_url', 'reference_images', 'reference_videos'] },
    { field: 'last_image_url', kind: 'image', max: 1, accepts: image, max_value_bytes: MAX_BYTES,
      conflicts_with: ['image_url', 'reference_images', 'reference_videos'] },
    { field: 'reference_images', kind: 'image', max: 9, accepts: image, max_value_bytes: MAX_BYTES,
      conflicts_with: ['first_image_url', 'last_image_url'] },
    { field: 'reference_videos', kind: 'video', max: 3, accepts: ['https'], max_value_bytes: MAX_BYTES,
      conflicts_with: ['first_image_url', 'last_image_url'] },
    { field: 'reference_audios', kind: 'audio', max: 3, accepts: image, max_value_bytes: MAX_BYTES,
      requires_any: ['image_url', 'first_image_url', 'last_image_url', 'reference_images', 'reference_videos'] },
  ]
}

const SORA: VideoModel = {
  id: 'sora-2', object: 'video.model', provider: 'openai', canonical_model: 'sora-2', is_default: true,
  seconds: { values: [4, 8], default: 4 },
  sizes: { values: ['720x1280', '1280x720'], default: '720x1280' },
  parameters: [
    {
      name: 'ratio', in: 'body', values: ['16:9', '9:16'],
      conflicts_with: ['aspect_ratio', 'size'],
      ignored_when_present: ['image_url', 'first_image_url', 'last_image_url', 'reference_videos'],
    },
  ],
  reference_inputs: referenceInputs(['https', 'data_uri']),
}

const SEEDANCE: VideoModel = {
  id: 'doubao-seedance-1-0-pro', object: 'video.model', provider: 'bytedance',
  canonical_model: 'doubao-seedance-1-0-pro',
  seconds: { min: 4, max: 15, default: 5 },
  parameters: [
    { name: 'resolution', in: 'provider_options', values: ['480p', '720p'] },
    { name: 'ratio', in: 'provider_options', values: ['adaptive', '16:9'] },
  ],
  reference_inputs: referenceInputs(['https']),
}

async function playground(models: VideoModel[]) {
  listVideoModels.mockResolvedValue({ object: 'list', data: models })
  const state = useVideoPlaygroundModels(() => 'sk-test')
  await state.loadModels()
  return state
}

function control(state: Awaited<ReturnType<typeof playground>>, name: string) {
  const found = state.framingControls.value.find((item) => item.name === name)
  if (!found) throw new Error(`missing framing control ${name}`)
  return found
}

function slot(state: Awaited<ReturnType<typeof playground>>, field: string) {
  const found = state.referenceSlots.value.find((item) => item.field === field)
  if (!found) throw new Error(`missing reference slot ${field}`)
  return found
}

describe('useVideoPlaygroundModels reference rules', () => {
  beforeEach(() => {
    listVideoModels.mockReset()
  })

  it('marks body framing as text-to-video only and drops it once media decides the framing', async () => {
    const state = await playground([SORA])

    // 一个素材都还没填时控件完全可用，但目录已经说明了它会被哪些素材架空——
    // 面板据此提前说话，而不是等用户白选一次画幅。
    expect(control(state, 'ratio').ignoredWhenPresent).toContain('image_url')
    expect(control(state, 'ratio').ignoredBy).toBe('')
    expect(control(state, 'size').ignoredWhenPresent).toEqual([])

    state.form.value.framing.ratio = '16:9'
    state.form.value.references.image_url = ['https://example.com/first.png']

    expect(control(state, 'ratio').ignoredBy).toBe('image_url')
    // 被忽略的画幅不能混进请求体：上游会静默丢掉它，留着只会让人以为生效了。
    expect(state.buildRequest()).not.toHaveProperty('ratio')
  })

  it('keeps Ark provider_options framing alive alongside media', async () => {
    const state = await playground([SEEDANCE])

    expect(control(state, 'resolution').ignoredWhenPresent).toEqual([])

    state.form.value.framing.resolution = '720p'
    state.form.value.references.image_url = ['https://example.com/first.png']

    expect(control(state, 'resolution').ignoredBy).toBe('')
    expect(state.buildRequest().provider_options).toEqual({ resolution: '720p' })
  })

  it('makes the first frame and the first/last pair mutually exclusive in both directions', async () => {
    const state = await playground([SORA])

    state.form.value.references.image_url = ['https://example.com/first.png']
    expect(slot(state, 'first_image_url').disabledByConflict).toBe('image_url')
    expect(slot(state, 'last_image_url').disabledByConflict).toBe('image_url')
    expect(slot(state, 'reference_images').disabledByConflict).toBe('')

    state.form.value.references.image_url = []
    state.form.value.references.first_image_url = ['https://example.com/first.png']
    expect(slot(state, 'image_url').disabledByConflict).toBe('first_image_url')
    expect(slot(state, 'reference_images').disabledByConflict).toBe('first_image_url')
    expect(slot(state, 'reference_videos').disabledByConflict).toBe('first_image_url')
  })

  it('gates reference audio behind at least one image or video reference', async () => {
    const state = await playground([SORA])

    expect(slot(state, 'reference_audios').missingPrerequisite).toBe(true)

    state.form.value.model = SORA.id
    state.form.value.prompt = 'a cat'
    state.form.value.references.reference_audios = ['https://example.com/score.mp3']
    expect(state.validate()).toContainEqual(
      expect.objectContaining({ code: 'reference_requires_any', field: 'reference_audios' }),
    )

    state.form.value.references.reference_images = ['https://example.com/style.png']
    expect(slot(state, 'reference_audios').missingPrerequisite).toBe(false)
    expect(state.validate()).toEqual([])
  })

  it('serialises single-value slots as strings and multi-value slots as arrays', async () => {
    const state = await playground([SORA])
    state.form.value.prompt = '  a cat  '
    state.form.value.references.image_url = [' https://example.com/first.png ']
    state.form.value.references.reference_audios = ['https://example.com/score.mp3']

    const payload = state.buildRequest() as unknown as Record<string, unknown>
    expect(payload.prompt).toBe('a cat')
    expect(payload.image_url).toBe('https://example.com/first.png')
    expect(payload.reference_audios).toEqual(['https://example.com/score.mp3'])
  })
})
