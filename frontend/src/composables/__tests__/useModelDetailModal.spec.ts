import { afterEach, describe, expect, it } from 'vitest'
import { useModelDetailModal } from '@/composables/useModelDetailModal'
import type { AggregatedModel } from '@/composables/useModelAggregation'

const model: AggregatedModel = {
  model: 'gpt-4o',
  displayName: 'gpt-4o',
  platform: 'openai',
  minPricing: {
    input: 0.0000025,
    output: 0.00001,
    cacheWrite: null,
    cacheRead: null,
    imageOutput: null,
    perRequest: null
  },
  supportedGroups: []
}

describe('useModelDetailModal', () => {
  afterEach(() => {
    const modal = useModelDetailModal()
    modal.close()
    document.body.style.overflow = ''
  })

  it('opens with the selected model and locks body scroll', () => {
    const modal = useModelDetailModal()

    modal.open(model)

    expect(modal.isOpen.value).toBe(true)
    expect(modal.currentModel.value).toBe(model)
    expect(document.body.style.overflow).toBe('hidden')
  })

  it('closes, clears the selected model, and restores body scroll', () => {
    const modal = useModelDetailModal()
    modal.open(model)

    modal.close()

    expect(modal.isOpen.value).toBe(false)
    expect(modal.currentModel.value).toBeNull()
    expect(document.body.style.overflow).toBe('')
  })

  it('restores the body overflow value from before the first open', () => {
    const modal = useModelDetailModal()
    document.body.style.overflow = 'auto'

    modal.open(model)
    modal.open({ ...model, model: 'gpt-4o-mini', displayName: 'gpt-4o-mini' })
    modal.close()

    expect(modal.isOpen.value).toBe(false)
    expect(modal.currentModel.value).toBeNull()
    expect(document.body.style.overflow).toBe('auto')
  })
})
