import { computed } from 'vue'
import { useAppStore } from '@/stores'

export type BalanceCurrency = 'USD' | 'CNY'

/** Site-wide balance labels. Amounts and payment gateway currencies are not converted. */
export function useBalanceCurrency() {
  const appStore = useAppStore()
  const balanceCurrency = computed<BalanceCurrency>(() =>
    appStore.cachedPublicSettings?.balance_currency === 'CNY' ? 'CNY' : 'USD',
  )
  const balanceSymbol = computed(() => balanceCurrency.value === 'CNY' ? '¥' : '$')
  return { balanceCurrency, balanceSymbol }
}
