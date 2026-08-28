import { ref } from 'vue'
import { getPublicSettings } from '@/api/auth'
import userChannelsAPI from '@/api/channels'
import type { UserAvailableChannel } from '@/api/channels'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  normalizePlazaMultiplier,
  PLAZA_DEFAULT_MULTIPLIER
} from '@/utils/pricing'

export interface PlazaPricingConfig {
  multiplier: number
  serverUtcOffset: string
  paymentEnabled: boolean
  balanceDisabled: boolean
  description: string
}

const defaultPricingConfig = (): PlazaPricingConfig => ({
  multiplier: PLAZA_DEFAULT_MULTIPLIER,
  serverUtcOffset: '',
  paymentEnabled: false,
  balanceDisabled: false,
  description: ''
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
      serverUtcOffset: settings.server_utc_offset ?? '',
      paymentEnabled: settings.payment_enabled === true,
      balanceDisabled: settings.payment_balance_disabled === true,
      description: settings.model_plaza_description ?? ''
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
