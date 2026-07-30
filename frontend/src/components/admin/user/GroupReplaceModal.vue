<template>
  <BaseDialog :show="show" :title="t('admin.users.replaceGroupTitle')" width="narrow" @close="$emit('close')">
    <div v-if="oldGroup" class="space-y-4">
      <!-- 提示信息 -->
      <p class="text-sm text-gray-600 dark:text-gray-400">
        {{ t('admin.users.replaceGroupHint', { old: oldGroup.name }) }}
      </p>

      <!-- 当前分组 -->
      <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-600 dark:bg-dark-800">
        <div class="flex items-center gap-2">
          <Icon name="shield" size="sm" class="text-purple-500" />
          <span class="font-medium text-gray-900 dark:text-white">{{ oldGroup.name }}</span>
          <Icon name="arrowRight" size="sm" class="ml-auto text-gray-400" />
          <span v-if="selectedGroupId" class="font-medium text-primary-600 dark:text-primary-400">
            {{ availableGroups.find(g => g.id === selectedGroupId)?.name }}
          </span>
          <span v-else class="text-sm text-gray-400">?</span>
        </div>
      </div>

      <div v-if="loading" class="flex justify-center py-8">
        <Icon name="refresh" size="lg" class="animate-spin text-primary-500" />
      </div>

      <div
        v-else-if="loadError"
        class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-300"
        role="alert"
      >
        <p>{{ loadError }}</p>
        <button type="button" class="btn btn-secondary mt-3" @click="loadCatalog">
          {{ t('common.retry') }}
        </button>
      </div>

      <!-- 可选分组列表 -->
      <div v-else-if="availableGroups.length > 0" class="max-h-80 space-y-2 overflow-y-auto">
        <label
          v-for="group in availableGroups"
          :key="group.id"
          class="flex items-start gap-3 rounded-lg border-2 p-3 transition-all"
          :class="[
            selectedGroupId === group.id
              ? 'border-primary-400 bg-primary-50/50 dark:border-primary-500 dark:bg-primary-900/20'
              : 'border-gray-200 dark:border-dark-600',
            group.can_bind === true
              ? 'cursor-pointer hover:border-gray-300 dark:hover:border-dark-500'
              : 'cursor-not-allowed opacity-70'
          ]"
          :data-test="`replace-group-option-${group.id}`"
        >
          <input
            type="radio"
            :value="group.id"
            v-model="selectedGroupId"
            :disabled="group.can_bind !== true"
            class="sr-only"
          />
          <div
            class="mt-1 flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full border-2 transition-all"
            :class="selectedGroupId === group.id
              ? 'border-primary-500 bg-primary-500'
              : 'border-gray-300 dark:border-dark-500'"
          >
            <div v-if="selectedGroupId === group.id" class="h-2 w-2 rounded-full bg-white"></div>
          </div>
          <div class="min-w-0 flex-1">
            <GroupOptionItem
              :name="group.name"
              :platform="group.platform"
              :subscription-type="group.subscription_type"
              :rate-multiplier="group.rate_multiplier"
              :description="group.description"
              :selected="selectedGroupId === group.id"
              :show-checkmark="false"
              :vip-only="group.vip_only"
              :can-bind="group.can_bind"
              :deny-reason="group.deny_reason"
              :suggested-action="group.suggested_action"
              :allow-payment-action="false"
            />
            <p
              v-if="group.will_grant_exclusive"
              class="mt-1.5 text-[11px] font-medium text-purple-600 dark:text-purple-300"
            >
              {{ t('admin.users.vip.willGrantExclusive') }}
            </p>
          </div>
        </label>
      </div>

      <!-- 无可选分组 -->
      <div v-else class="py-6 text-center text-sm text-gray-400">
        {{ t('admin.users.noOtherGroups') }}
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" class="btn btn-secondary px-5">{{ t('common.cancel') }}</button>
        <button
          @click="handleReplace"
          :disabled="!selectedGroupId || submitting"
          class="btn btn-primary px-6"
        >
          <svg v-if="submitting" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          {{ submitting ? t('common.saving') : t('admin.users.replaceGroupConfirm') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'
import type { AdminGroupCatalogEntry, AdminUser } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'

interface Props {
  show: boolean
  user: AdminUser | null
  oldGroup: { id: number; name: string } | null
}

const props = defineProps<Props>()
const emit = defineEmits(['close', 'success'])
const { t } = useI18n()
const appStore = useAppStore()

const selectedGroupId = ref<number | null>(null)
const submitting = ref(false)
const loading = ref(false)
const loadError = ref('')
const catalog = ref<AdminGroupCatalogEntry[]>([])
let catalogSequence = 0

// 可选的专属标准分组（排除当前 oldGroup）
const availableGroups = computed(() => {
  if (!props.oldGroup) return []
  return catalog.value.filter(
    g => g.status === 'active' && g.is_exclusive && g.subscription_type === 'standard' && g.id !== props.oldGroup!.id
  )
})

watch(() => props.show, (v) => {
  if (v) {
    selectedGroupId.value = null
    void loadCatalog()
  } else {
    catalogSequence += 1
  }
})

const loadCatalog = async () => {
  if (!props.user) return
  const sequence = ++catalogSequence
  loading.value = true
  loadError.value = ''
  catalog.value = []
  try {
    const result = await adminAPI.users.getGroupCatalog(props.user.id)
    if (sequence !== catalogSequence) return
    catalog.value = result
  } catch (error) {
    if (sequence !== catalogSequence) return
    loadError.value = extractApiErrorMessage(error, t('admin.users.vip.catalogLoadFailed'))
  } finally {
    if (sequence === catalogSequence) loading.value = false
  }
}

const handleReplace = async () => {
  if (!props.user || !props.oldGroup || !selectedGroupId.value) return
  const target = catalog.value.find((group) => group.id === selectedGroupId.value)
  if (!target || target.can_bind !== true) {
    appStore.showError(t('admin.users.vip.catalogDenied'))
    return
  }
  submitting.value = true

  try {
    const result = await adminAPI.users.replaceGroup(props.user.id, props.oldGroup.id, selectedGroupId.value)
    appStore.showSuccess(t('admin.users.replaceGroupSuccess', { count: result.migrated_keys }))
    emit('success')
    emit('close')
  } catch (error) {
    appStore.showError(
      extractI18nErrorMessage(
        error,
        t,
        'vip.group.errors',
        t('admin.users.replaceGroupFailed')
      )
    )
    console.error('Failed to replace group:', error)
  } finally {
    submitting.value = false
  }
}
</script>
