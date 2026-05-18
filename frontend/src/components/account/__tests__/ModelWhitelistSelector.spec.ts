import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => params?.count !== undefined ? `${key}:${params.count}` : key
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showInfo: vi.fn()
  })
}))

describe('ModelWhitelistSelector', () => {
  it('fills latest models from async loader and exposes them as options', async () => {
    const loadRelatedModels = vi.fn().mockResolvedValue(['opencode/gpt5-high', 'opencode/new-model'])
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'opencode',
        loadRelatedModels
      },
      global: {
        stubs: {
          Icon: true,
          ModelIcon: true
        }
      }
    })

    await wrapper.get('[data-testid="fill-related-models"]').trigger('click')

    expect(loadRelatedModels).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual(['opencode/gpt5-high', 'opencode/new-model'])

    await wrapper.find('.cursor-pointer').trigger('click')
    expect(wrapper.text()).toContain('opencode/new-model')
  })
})
