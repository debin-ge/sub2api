import { beforeEach, describe, expect, it, vi } from 'vitest'

const { getPublicMock, getPublicSettingsMock } = vi.hoisted(() => ({
  getPublicMock: vi.fn(),
  getPublicSettingsMock: vi.fn(),
}))

vi.mock('@/api/channels', () => ({
  default: {
    getPublic: getPublicMock,
  },
}))

vi.mock('@/api/auth', () => ({
  getPublicSettings: getPublicSettingsMock,
}))

describe('usePlazaData', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    const plazaData = await import('@/composables/usePlazaData')
    plazaData.channels.value = []
    plazaData.error.value = ''
    plazaData.loading.value = false
    plazaData.pricingConfig.value = {
      multiplier: 1,
      rate: 7.2,
      paymentEnabled: false,
      balanceDisabled: false,
    }
  })

  it('loads public channels independently from available channels feature flag', async () => {
    const publicChannels = [{
      name: 'Public Channel',
      description: '',
      platforms: [{
        platform: 'openai',
        groups: [],
        supported_models: [],
      }],
    }]
    getPublicMock.mockResolvedValue(publicChannels)
    getPublicSettingsMock.mockResolvedValue({
      payment_balance_recharge_multiplier: 1.25,
      payment_cny_usd_rate: 6.8,
      payment_enabled: true,
      payment_balance_disabled: false,
      available_channels_enabled: false,
    })

    const plazaData = await import('@/composables/usePlazaData')
    await plazaData.fetchAll()

    expect(getPublicMock).toHaveBeenCalledTimes(1)
    expect(plazaData.channels.value).toEqual(publicChannels)
    expect(plazaData.pricingConfig.value).toEqual({
      multiplier: 1.25,
      rate: 6.8,
      paymentEnabled: true,
      balanceDisabled: false,
    })
    expect('availableChannelsEnabled' in plazaData.pricingConfig.value).toBe(false)
  })
})
