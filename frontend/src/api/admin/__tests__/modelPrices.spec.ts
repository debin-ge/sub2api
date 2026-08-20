import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, del } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  del: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, put, delete: del },
}))

import {
  PRICE_FIELDS,
  deleteModelPrice,
  getModelPriceEntry,
  isImageField,
  mTokToToken,
  tokenToMTok,
} from '@/api/admin/modelPrices'
import enModelPrices from '@/i18n/locales/en/admin/modelPrices'
import zhModelPrices from '@/i18n/locales/zh/admin/modelPrices'

describe('model price helpers', () => {
  it('converts token prices to MTok and back, keeping explicit zero', () => {
    expect(tokenToMTok(3e-6)).toBe(3)
    expect(mTokToToken('3')).toBeCloseTo(3e-6)
    expect(tokenToMTok(0)).toBe(0)
    expect(mTokToToken('0')).toBe(0)
    expect(mTokToToken('')).toBeUndefined()
  })

  it('treats only per-image as an image field', () => {
    expect(isImageField('output_cost_per_image')).toBe(true)
    expect(isImageField('input_cost_per_token')).toBe(false)
  })

  it('has localized labels for every editable price field', () => {
    for (const field of PRICE_FIELDS) {
      expect(zhModelPrices.modelPrices.fields[field]).toBeTruthy()
      expect(enModelPrices.modelPrices.fields[field]).toBeTruthy()
      expect(zhModelPrices.modelPrices.fields[field]).not.toBe(field)
      expect(enModelPrices.modelPrices.fields[field]).not.toBe(field)
    }
  })
})

describe('model price API encoding', () => {
  beforeEach(() => {
    get.mockReset()
    del.mockReset()
    get.mockResolvedValue({ data: {} })
    del.mockResolvedValue({ data: {} })
  })

  it('loads and deletes slash model names via query params', async () => {
    await getModelPriceEntry('*', 'openai/gpt-5.4')
    expect(get).toHaveBeenCalledWith('/admin/model-prices/entry', {
      params: { platform: '*', model: 'openai/gpt-5.4' },
    })

    await deleteModelPrice('*', 'models/glm-4.7-flash')
    expect(del).toHaveBeenCalledWith('/admin/model-prices/entry', {
      params: { platform: '*', model: 'models/glm-4.7-flash' },
    })
  })
})
