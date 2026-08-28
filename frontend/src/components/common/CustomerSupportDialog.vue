<template>
  <BaseDialog
    :show="show"
    :title="t('common.contactSupport')"
    width="narrow"
    close-on-click-outside
    @close="emit('close')"
  >
    <div class="flex flex-col items-center gap-4 py-1">
      <div class="w-full max-w-sm rounded-2xl border border-gray-100 bg-white p-4 shadow-sm dark:border-dark-700">
        <img
          v-if="qrCode"
          :src="qrCode"
          :alt="t('common.supportQrCodeAlt')"
          class="mx-auto max-h-[360px] w-full object-contain"
          data-testid="customer-support-qr-code"
        />
      </div>
      <p class="text-center text-sm text-gray-600 dark:text-gray-300">
        {{ t('common.contactSupportScanHint') }}
      </p>
      <p
        v-if="contactInfo"
        class="max-w-full break-words rounded-lg bg-gray-50 px-3 py-2 text-center text-sm font-medium text-gray-700 dark:bg-dark-800 dark:text-gray-200"
      >
        {{ contactInfo }}
      </p>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-primary" @click="emit('close')">
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'

defineProps<{
  show: boolean
  qrCode: string
  contactInfo?: string
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
</script>
