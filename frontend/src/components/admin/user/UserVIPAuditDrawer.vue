<template>
  <Teleport to="body">
    <Transition name="vip-audit-drawer">
      <div
        v-if="show"
        class="fixed inset-0 z-[100000010]"
        role="dialog"
        aria-modal="true"
        :aria-label="t('admin.users.vip.auditTitle')"
        data-test="vip-audit-drawer"
      >
        <button
          type="button"
          class="absolute inset-0 bg-black/40"
          :aria-label="t('common.close')"
          @click="emit('close')"
        ></button>
        <aside
          class="absolute inset-y-0 right-0 flex w-full max-w-xl flex-col bg-white shadow-2xl dark:bg-dark-800"
        >
          <header class="flex items-start justify-between border-b border-gray-200 px-5 py-4 dark:border-dark-700">
            <div class="min-w-0">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('admin.users.vip.auditTitle') }}
              </h2>
              <p v-if="user" class="mt-1 truncate text-sm text-gray-500 dark:text-gray-400">
                {{ user.email }} · #{{ user.id }}
              </p>
            </div>
            <button
              type="button"
              class="rounded-lg p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-700 dark:hover:text-gray-200"
              :aria-label="t('common.close')"
              @click="emit('close')"
            >
              <Icon name="x" size="md" />
            </button>
          </header>

          <div class="min-h-0 flex-1 overflow-y-auto p-5">
            <div v-if="loading" class="flex justify-center py-16">
              <Icon name="refresh" size="lg" class="animate-spin text-primary-500" />
            </div>
            <div
              v-else-if="errorMessage"
              class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-300"
              role="alert"
            >
              <p>{{ errorMessage }}</p>
              <button type="button" class="btn btn-secondary mt-3" @click="load">
                {{ t('common.retry') }}
              </button>
            </div>
            <div
              v-else-if="events.length === 0"
              class="rounded-xl border border-dashed border-gray-300 px-4 py-12 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400"
            >
              {{ t('admin.users.vip.auditEmpty') }}
            </div>
            <ol v-else class="space-y-4">
              <li
                v-for="event in events"
                :key="event.id"
                class="rounded-xl border border-gray-200 p-4 dark:border-dark-600"
                data-test="vip-audit-event"
              >
                <div class="flex flex-wrap items-start justify-between gap-2">
                  <div>
                    <p class="font-medium text-gray-900 dark:text-white">
                      {{ event.action || t('admin.users.vip.auditUnknownAction') }}
                    </p>
                    <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ formatDateTime(event.created_at) }}
                    </p>
                  </div>
                  <span class="rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                    {{ event.source || t('admin.users.vip.unknownValue') }}
                  </span>
                </div>

                <div class="mt-3 grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2 rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-700/60">
                  <div>
                    <p class="text-gray-400">{{ t('admin.users.vip.auditBefore') }}</p>
                    <p class="mt-1 font-medium text-gray-700 dark:text-gray-200">
                      {{ effectiveLabel(event.old_is_vip) }}
                    </p>
                    <p class="mt-0.5 text-gray-500 dark:text-gray-400">
                      {{ modeLabel(event.old_manual_override) }}
                    </p>
                  </div>
                  <Icon name="arrowRight" size="sm" class="text-gray-400" />
                  <div>
                    <p class="text-gray-400">{{ t('admin.users.vip.auditAfter') }}</p>
                    <p class="mt-1 font-medium text-gray-700 dark:text-gray-200">
                      {{ effectiveLabel(event.new_is_vip) }}
                    </p>
                    <p class="mt-0.5 text-gray-500 dark:text-gray-400">
                      {{ modeLabel(event.new_manual_override) }}
                    </p>
                  </div>
                </div>

                <dl class="mt-3 space-y-1.5 text-xs">
                  <div class="flex gap-2">
                    <dt class="shrink-0 text-gray-400">{{ t('admin.users.vip.auditActor') }}</dt>
                    <dd class="break-all text-gray-700 dark:text-gray-300">
                      {{ event.actor_snapshot || event.actor_type || t('admin.users.vip.unknownValue') }}
                    </dd>
                  </div>
                  <div class="flex gap-2">
                    <dt class="shrink-0 text-gray-400">{{ t('admin.users.vip.auditReason') }}</dt>
                    <dd class="whitespace-pre-wrap text-gray-700 dark:text-gray-300">
                      {{ event.reason || t('admin.users.vip.unknownValue') }}
                    </dd>
                  </div>
                  <div v-if="event.order_id" class="flex gap-2">
                    <dt class="shrink-0 text-gray-400">{{ t('admin.users.vip.auditOrder') }}</dt>
                    <dd class="font-mono text-gray-700 dark:text-gray-300">#{{ event.order_id }}</dd>
                  </div>
                  <div v-if="event.request_id" class="flex gap-2">
                    <dt class="shrink-0 text-gray-400">{{ t('admin.users.vip.auditRequest') }}</dt>
                    <dd class="break-all font-mono text-gray-700 dark:text-gray-300">
                      {{ event.request_id }}
                    </dd>
                  </div>
                </dl>
              </li>
            </ol>
          </div>

          <Pagination
            v-if="total > 0"
            :page="page"
            :page-size="pageSize"
            :total="total"
            :show-page-size-selector="false"
            @update:page="handlePageChange"
          />
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { AdminUser, VIPAuditEvent } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits<{ (event: 'close'): void }>()
const { t } = useI18n()

const events = ref<VIPAuditEvent[]>([])
const loading = ref(false)
const errorMessage = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
let loadSequence = 0

const load = async () => {
  if (!props.user) return
  const sequence = ++loadSequence
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await adminAPI.users.getVIPAudit(props.user.id, page.value, pageSize)
    if (sequence !== loadSequence) return
    events.value = response.items
    total.value = response.total
  } catch (error) {
    if (sequence !== loadSequence) return
    events.value = []
    total.value = 0
    errorMessage.value = extractApiErrorMessage(error, t('admin.users.vip.auditLoadFailed'))
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

const handlePageChange = (nextPage: number) => {
  page.value = nextPage
  void load()
}

const effectiveLabel = (value: boolean) =>
  value ? t('admin.users.vip.effectiveActive') : t('admin.users.vip.effectiveInactive')

const modeLabel = (override: boolean | null) => {
  if (override === true) return t('admin.users.vip.modes.FORCE_ON')
  if (override === false) return t('admin.users.vip.modes.FORCE_OFF')
  return t('admin.users.vip.modes.AUTO')
}

watch(
  () => [props.show, props.user?.id] as const,
  ([show]) => {
    if (!show) {
      loadSequence += 1
      return
    }
    page.value = 1
    events.value = []
    total.value = 0
    void load()
  },
  { immediate: true }
)

const handleKeydown = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && props.show) emit('close')
}

onMounted(() => document.addEventListener('keydown', handleKeydown))
onUnmounted(() => document.removeEventListener('keydown', handleKeydown))
</script>

<style scoped>
.vip-audit-drawer-enter-active,
.vip-audit-drawer-leave-active {
  transition: opacity 160ms ease;
}
.vip-audit-drawer-enter-active aside,
.vip-audit-drawer-leave-active aside {
  transition: transform 160ms ease;
}
.vip-audit-drawer-enter-from,
.vip-audit-drawer-leave-to {
  opacity: 0;
}
.vip-audit-drawer-enter-from aside,
.vip-audit-drawer-leave-to aside {
  transform: translateX(100%);
}
</style>
