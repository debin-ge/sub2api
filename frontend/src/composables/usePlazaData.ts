import { ref } from 'vue'
import { getPublicSettings } from '@/api/auth'
import userChannelsAPI from '@/api/channels'
import type { UserAvailableChannel } from '@/api/channels'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  normalizePlazaMultiplier,
  normalizePlazaRate,
  PLAZA_DEFAULT_CNY_USD_RATE,
  PLAZA_DEFAULT_MULTIPLIER
} from '@/utils/pricing'

export interface PlazaPricingConfig {
  multiplier: number
  rate: number
  paymentEnabled: boolean
  balanceDisabled: boolean
  availableChannelsEnabled: boolean
}

const defaultPricingConfig = (): PlazaPricingConfig => ({
  multiplier: PLAZA_DEFAULT_MULTIPLIER,
  rate: PLAZA_DEFAULT_CNY_USD_RATE,
  paymentEnabled: false,
  balanceDisabled: false,
  availableChannelsEnabled: false
})

export const channels = ref<UserAvailableChannel[]>([])
export const pricingConfig = ref<PlazaPricingConfig>(defaultPricingConfig())
export const loading = ref(false)
export const error = ref('')

let latestRequestId = 0

export async function fetchAll(): Promise<void> {
  const requestId = ++latestRequestId
  loading.value = true
  error.value = ''

  try {
    const [publicChannels, settings] = await Promise.all([
      userChannelsAPI.getPublic(),
      getPublicSettings()
    ])

    if (requestId !== latestRequestId) return

    channels.value = publicChannels
    pricingConfig.value = {
      multiplier: normalizePlazaMultiplier(settings.payment_balance_recharge_multiplier),
      rate: normalizePlazaRate(settings.payment_cny_usd_rate),
      paymentEnabled: settings.payment_enabled === true,
      balanceDisabled: settings.payment_balance_disabled === true,
      availableChannelsEnabled: settings.available_channels_enabled === true
    }
  } catch (err: unknown) {
    if (requestId !== latestRequestId) return
    error.value = extractApiErrorMessage(err, 'Failed to load model plaza data')
  } finally {
    if (requestId === latestRequestId) {
      loading.value = false
    }
  }
}

export function usePlazaData() {
  return {
    channels,
    pricingConfig,
    loading,
    error,
    fetchAll
  }
}
