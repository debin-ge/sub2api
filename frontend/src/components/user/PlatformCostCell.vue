<template>
  <div v-if="usage" class="text-sm">
    <div class="flex items-center gap-1.5">
      <span class="zt-ink-3">{{ t('admin.users.today') }}:</span>
      <span class="font-medium zt-ink">{{ balanceSymbol }}{{ usage.today_actual_cost.toFixed(4) }}</span>
    </div>
    <div class="mt-0.5 flex items-center gap-1.5">
      <span class="zt-ink-3">{{ t('admin.users.total') }}:</span>
      <span class="font-medium zt-ink">{{ balanceSymbol }}{{ usage.total_actual_cost.toFixed(4) }}</span>
    </div>
  </div>
  <span v-else class="text-sm zt-ink-3">—</span>
</template>

<script setup lang="ts">
import { useBalanceCurrency } from '@/composables/useBalanceCurrency'
import { useI18n } from 'vue-i18n'
import type { PlatformUsage } from '@/api/admin/dashboard'

const { balanceSymbol } = useBalanceCurrency()

defineProps<{
  usage?: PlatformUsage
}>()

const { t } = useI18n()
</script>
