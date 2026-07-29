import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

const { copyToClipboard, syncUpstreamModelsMock, syncUpstreamModelsPreviewMock } = vi.hoisted(() => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
  syncUpstreamModelsMock: vi.fn(),
  syncUpstreamModelsPreviewMock: vi.fn()
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: {
    syncUpstreamModels: syncUpstreamModelsMock,
    syncUpstreamModelsPreview: syncUpstreamModelsPreviewMock
  },
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue({})
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'common.copy') return '复制'
        return params?.count !== undefined ? `${key}:${params.count}` : key
      }
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showInfo: vi.fn(),
    showSuccess: vi.fn()
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

function mountSelector() {
  return mount(ModelWhitelistSelector, {
    props: {
      modelValue: [],
      platform: 'openai'
    },
    global: {
      stubs: {
        Icon: true,
        ModelIcon: true
      }
    }
  })
}

function findModelRow(wrapper: ReturnType<typeof mountSelector>, modelId: string) {
  const row = wrapper
    .findAll('[data-testid="model-option"]')
    .find(candidate => candidate.text().includes(modelId))

  if (!row) {
    throw new Error(`Model row not found: ${modelId}`)
  }

  return row
}

describe('ModelWhitelistSelector', () => {
  beforeEach(() => {
    copyToClipboard.mockClear()
  })

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

  it('syncs live models for MiniMax-compatible credentials', async () => {
    syncUpstreamModelsPreviewMock.mockReset()
    syncUpstreamModelsPreviewMock.mockResolvedValue({ models: ['MiniMax-M3', 'MiniMax-next'] })
    const credentials = {
      platform: 'minimax',
      type: 'apikey',
      base_url: 'https://api.minimax.io/v1',
      api_key: 'sk-minimax'
    }
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'minimax',
        syncCredentials: credentials
      },
      global: {
        stubs: {
          Icon: true,
          ModelIcon: true
        }
      }
    })

    await wrapper.get('[data-testid="sync-upstream-models"]').trigger('click')

    expect(syncUpstreamModelsPreviewMock).toHaveBeenCalledWith(credentials)
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual(['MiniMax-M3', 'MiniMax-next'])
  })

  it('syncs an existing account through the live endpoint with current edit overrides', async () => {
    syncUpstreamModelsMock.mockReset()
    syncUpstreamModelsPreviewMock.mockReset()
    syncUpstreamModelsMock.mockResolvedValue({ models: ['MiniMax-M3', 'MiniMax-future'] })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'minimax',
        accountId: 42,
        syncCredentials: {
          platform: 'minimax',
          type: 'apikey',
          base_url: 'https://api.minimaxi.com/v1'
        }
      },
      global: {
        stubs: {
          Icon: true,
          ModelIcon: true
        }
      }
    })

    await wrapper.get('[data-testid="sync-upstream-models"]').trigger('click')

    expect(syncUpstreamModelsMock).toHaveBeenCalledWith(42, {
      base_url: 'https://api.minimaxi.com/v1',
      api_key: undefined
    })
    expect(syncUpstreamModelsPreviewMock).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual(['MiniMax-M3', 'MiniMax-future'])
  })

  it('copies a model ID without selecting the model', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')
    const copyButton = row.get('[data-testid="copy-model-id"]')
    expect(copyButton.attributes('aria-label')).toBe('复制 gpt-5.6-sol')

    await copyButton.trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenCalledWith('gpt-5.6-sol')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps the existing model selection behavior', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')
    await row.get('[data-testid="select-model"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-5.6-sol']]])
    expect(copyToClipboard).not.toHaveBeenCalled()
  })
})
