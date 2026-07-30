<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="card overflow-hidden" aria-labelledby="vip-reconcile-preview-title">
        <div class="flex flex-col gap-4 border-b border-gray-100 px-4 py-5 dark:border-dark-700 sm:px-6 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <h2 id="vip-reconcile-preview-title" class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('admin.vipReconcile.preview.title') }}
            </h2>
            <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.vipReconcile.preview.description') }}
            </p>
          </div>
          <div class="flex flex-wrap items-end gap-2">
            <label class="block">
              <span class="input-label">{{ t('admin.vipReconcile.preview.pageSize') }}</span>
              <select
                v-model.number="previewLimit"
                class="input min-w-24"
                :disabled="previewLoading"
                @change="restartPreview"
              >
                <option :value="50">50</option>
                <option :value="100">100</option>
                <option :value="200">200</option>
              </select>
            </label>
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="previewLoading"
              data-testid="preview-restart"
              @click="restartPreview"
            >
              {{ previewLoading ? t('common.loading') : t('admin.vipReconcile.preview.newSnapshot') }}
            </button>
          </div>
        </div>

        <div class="space-y-5 p-4 sm:p-6">
          <div
            v-if="previewError"
            class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-200"
            role="alert"
          >
            <div class="flex flex-wrap items-center justify-between gap-3">
              <span>{{ previewError }}</span>
              <button type="button" class="btn btn-secondary btn-sm" @click="loadPreview()">
                {{ t('common.retry') }}
              </button>
            </div>
          </div>

          <template v-if="preview">
            <dl class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div class="rounded-lg bg-gray-50 px-4 py-3 dark:bg-dark-900/60">
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
                  {{ t('admin.vipReconcile.preview.asOf') }}
                </dt>
                <dd class="mt-1 break-all text-sm font-medium text-gray-900 dark:text-white" data-testid="preview-as-of">
                  {{ formatDateTime(preview.as_of) }}
                </dd>
              </div>
              <div class="rounded-lg bg-gray-50 px-4 py-3 dark:bg-dark-900/60">
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
                  {{ t('admin.vipReconcile.preview.total') }}
                </dt>
                <dd class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">
                  {{ formatNumber(preview.total) }}
                </dd>
              </div>
            </dl>

            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-5">
              <div
                v-for="card in previewStatCards"
                :key="card.key"
                class="rounded-xl border px-4 py-4"
                :class="card.className"
              >
                <p class="text-xs font-medium">{{ card.label }}</p>
                <p class="mt-2 text-2xl font-semibold">{{ formatNumber(card.value) }}</p>
                <p class="mt-1 text-xs opacity-80">{{ card.hint }}</p>
              </div>
            </div>
          </template>

          <div class="overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700">
            <table class="w-full min-w-[900px] text-left text-sm">
              <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-900 dark:text-gray-400">
                <tr>
                  <th class="px-4 py-3">{{ t('admin.vipReconcile.preview.columns.category') }}</th>
                  <th class="px-4 py-3">{{ t('admin.vipReconcile.preview.columns.user') }}</th>
                  <th class="px-4 py-3">{{ t('admin.vipReconcile.preview.columns.order') }}</th>
                  <th class="px-4 py-3">{{ t('admin.vipReconcile.preview.columns.completedAt') }}</th>
                  <th class="px-4 py-3">{{ t('admin.vipReconcile.preview.columns.currentMode') }}</th>
                  <th class="px-4 py-3">{{ t('admin.vipReconcile.preview.columns.currentEffective') }}</th>
                  <th class="px-4 py-3">{{ t('admin.vipReconcile.preview.columns.willChange') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                <tr v-if="previewLoading && !preview" data-testid="preview-loading">
                  <td colspan="7" class="px-4 py-10 text-center text-gray-500 dark:text-gray-400">
                    {{ t('common.loading') }}
                  </td>
                </tr>
                <tr v-else-if="previewItems.length === 0">
                  <td colspan="7" class="px-4 py-10 text-center text-gray-500 dark:text-gray-400">
                    {{ t('admin.vipReconcile.preview.empty') }}
                  </td>
                </tr>
                <tr
                  v-for="item in previewItems"
                  :key="`${item.category}-${item.order_id}`"
                  class="text-gray-700 dark:text-gray-300"
                  data-testid="preview-item"
                >
                  <td class="px-4 py-3">
                    <span class="inline-flex rounded-full px-2.5 py-1 text-xs font-medium" :class="categoryClass(item.category)">
                      {{ categoryLabel(item.category) }}
                    </span>
                  </td>
                  <td class="px-4 py-3 font-mono text-xs">{{ item.user_id ?? '—' }}</td>
                  <td class="px-4 py-3 font-mono text-xs">#{{ item.order_id }}</td>
                  <td class="whitespace-nowrap px-4 py-3 text-xs">{{ formatDateTime(item.completed_at) }}</td>
                  <td class="px-4 py-3">{{ vipModeLabel(item.current_vip_mode) }}</td>
                  <td class="px-4 py-3">
                    <span :class="item.current_is_vip ? 'text-emerald-600 dark:text-emerald-300' : 'text-gray-500 dark:text-gray-400'">
                      {{ booleanLabel(item.current_is_vip) }}
                    </span>
                  </td>
                  <td class="px-4 py-3">
                    <span :class="item.will_effective_change ? 'font-medium text-amber-700 dark:text-amber-300' : 'text-gray-500 dark:text-gray-400'">
                      {{ booleanLabel(item.will_effective_change) }}
                    </span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.vipReconcile.preview.page', { page: previewPage }) }}
            </p>
            <div class="flex items-center gap-2">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="previewLoading || previewCursorHistory.length === 0"
                data-testid="preview-previous"
                @click="previousPreviewPage"
              >
                {{ t('common.previous') }}
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="previewLoading || !preview?.next_cursor"
                data-testid="preview-next"
                @click="nextPreviewPage"
              >
                {{ t('common.next') }}
              </button>
            </div>
          </div>
        </div>
      </section>

      <section class="card overflow-hidden" aria-labelledby="vip-reconcile-execute-title">
        <div class="border-b border-gray-100 px-4 py-5 dark:border-dark-700 sm:px-6">
          <h2 id="vip-reconcile-execute-title" class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('admin.vipReconcile.execute.title') }}
          </h2>
          <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.vipReconcile.execute.description') }}
          </p>
        </div>

        <div class="space-y-4 p-4 sm:p-6">
          <div class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
            {{ t('admin.vipReconcile.execute.warning') }}
          </div>

          <div class="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
            <label class="block">
              <span class="input-label">{{ t('admin.vipReconcile.execute.requestId') }}</span>
              <input
                v-model="requestId"
                class="input w-full font-mono text-xs"
                readonly
                data-testid="request-id"
              />
              <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.vipReconcile.execute.requestIdHint') }}
              </span>
            </label>
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="submitting || jobIsActive"
              data-testid="new-request-id"
              @click="prepareNewRequest"
            >
              {{ t('admin.vipReconcile.execute.newRequest') }}
            </button>
          </div>

          <label class="block">
            <span class="input-label">
              {{ t('admin.vipReconcile.execute.reason') }}
              <span class="text-rose-500" aria-hidden="true">*</span>
            </span>
            <textarea
              v-model="reason"
              rows="3"
              class="input w-full resize-y"
              :placeholder="t('admin.vipReconcile.execute.reasonPlaceholder')"
              :aria-invalid="reasonError ? 'true' : 'false'"
              data-testid="reconcile-reason"
            ></textarea>
            <span v-if="reasonError" class="mt-1 block text-xs text-rose-600 dark:text-rose-300" role="alert">
              {{ reasonError }}
            </span>
          </label>

          <div class="flex flex-wrap items-center gap-3">
            <button
              type="button"
              class="btn btn-danger"
              :disabled="submitting || jobIsActive"
              data-testid="create-job"
              @click="submitJob"
            >
              {{ submitting ? t('admin.vipReconcile.execute.submitting') : t('admin.vipReconcile.execute.submit') }}
            </button>
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.vipReconcile.execute.stepUpHint') }}
            </p>
          </div>
        </div>
      </section>

      <section v-if="job || restoringJob" class="card overflow-hidden" aria-labelledby="vip-reconcile-job-title">
        <div class="flex flex-col gap-4 border-b border-gray-100 px-4 py-5 dark:border-dark-700 sm:px-6 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <div class="flex flex-wrap items-center gap-3">
              <h2 id="vip-reconcile-job-title" class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('admin.vipReconcile.job.title') }}
                <span v-if="job" class="font-mono text-sm font-normal text-gray-500">#{{ job.id }}</span>
              </h2>
              <span v-if="job" class="inline-flex rounded-full px-2.5 py-1 text-xs font-semibold" :class="jobStatusClass(job.status)">
                {{ jobStatusLabel(job.status) }}
              </span>
            </div>
            <p v-if="job" class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.vipReconcile.job.requestId') }}:
              <span class="font-mono">{{ job.request_id }}</span>
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="jobLoading || !job"
              data-testid="refresh-job"
              @click="loadJob(true)"
            >
              {{ jobLoading ? t('common.loading') : t('common.refresh') }}
            </button>
            <button
              v-if="job?.status === 'failed'"
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="submitting"
              data-testid="resume-job"
              @click="resumeFailedJob"
            >
              {{ submitting ? t('admin.vipReconcile.execute.submitting') : t('admin.vipReconcile.job.resume') }}
            </button>
          </div>
        </div>

        <div class="space-y-5 p-4 sm:p-6" aria-live="polite">
          <div v-if="restoringJob && !job" class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.vipReconcile.job.restoring') }}
          </div>

          <template v-if="job">
            <div
              v-if="jobStatusUnknown"
              class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200"
              role="alert"
              data-testid="unknown-job-status"
            >
              {{ t('admin.vipReconcile.job.unknownStatus', { status: job.status }) }}
            </div>
            <div
              v-if="jobError"
              class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-200"
              role="alert"
            >
              {{ jobError }}
            </div>
            <div
              v-if="job.last_error"
              class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-200"
              data-testid="job-last-error"
            >
              <span class="font-medium">{{ t('admin.vipReconcile.job.lastError') }}:</span>
              {{ job.last_error }}
            </div>

            <div v-if="jobIsActive" class="space-y-2">
              <div class="flex items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400">
                <span>{{ t('admin.vipReconcile.job.inProgress') }}</span>
                <span>{{ t('admin.vipReconcile.job.scanned') }}: {{ formatNumber(job.scanned) }}</span>
              </div>
              <div
                class="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700"
                role="progressbar"
                :aria-label="t('admin.vipReconcile.job.inProgress')"
                :aria-valuetext="t('admin.vipReconcile.job.scannedCount', { count: formatNumber(job.scanned) })"
              >
                <div class="h-full w-1/2 animate-pulse rounded-full bg-primary-500"></div>
              </div>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.vipReconcile.job.indeterminateHint') }}
              </p>
            </div>

            <dl class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
              <div v-for="metric in jobMetrics" :key="metric.key" class="rounded-lg bg-gray-50 px-4 py-3 dark:bg-dark-900/60">
                <dt class="text-xs text-gray-500 dark:text-gray-400">{{ metric.label }}</dt>
                <dd class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ formatNumber(metric.value) }}</dd>
              </div>
            </dl>

            <dl class="grid grid-cols-1 gap-x-6 gap-y-3 rounded-xl border border-gray-200 p-4 text-sm dark:border-dark-700 sm:grid-cols-2 xl:grid-cols-3">
              <div>
                <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.vipReconcile.job.reason') }}</dt>
                <dd class="mt-1 whitespace-pre-wrap text-gray-900 dark:text-white">{{ job.reason }}</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.vipReconcile.job.actor') }}</dt>
                <dd class="mt-1 break-all text-gray-900 dark:text-white">{{ job.actor_snapshot || `#${job.actor_user_id}` }}</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.vipReconcile.preview.asOf') }}</dt>
                <dd class="mt-1 text-gray-900 dark:text-white">{{ formatDateTime(job.as_of) }}</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.vipReconcile.job.cursor') }}</dt>
                <dd class="mt-1 break-all font-mono text-xs text-gray-900 dark:text-white">
                  {{ formatDateTime(job.cursor_completed_at) }} / #{{ job.cursor_order_id }}
                </dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.vipReconcile.job.startedAt') }}</dt>
                <dd class="mt-1 text-gray-900 dark:text-white">{{ formatDateTime(job.started_at) }}</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.vipReconcile.job.finishedAt') }}</dt>
                <dd class="mt-1 text-gray-900 dark:text-white">{{ formatDateTime(job.finished_at) }}</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.vipReconcile.job.updatedAt') }}</dt>
                <dd class="mt-1 text-gray-900 dark:text-white">{{ formatDateTime(job.updated_at) }}</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.vipReconcile.job.attempts') }}</dt>
                <dd class="mt-1 text-gray-900 dark:text-white">{{ formatNumber(job.attempts) }}</dd>
              </div>
            </dl>
          </template>
        </div>
      </section>
    </div>

    <TotpStepUpDialog :controller="reconcileStepUp" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores'
import {
  isStepUpBlocked,
  isStepUpCancelled,
  stepUpBlockReason,
  useStepUp
} from '@/composables/useStepUp'
import { extractI18nErrorMessage } from '@/utils/apiError'
import type {
  VIPMode,
  VIPReconcileJob,
  VIPReconcilePreview
} from '@/types'

const LAST_JOB_STORAGE_KEY = 'admin-vip-reconcile-last-job-id'
const POLL_INTERVAL_MS = 2500
const ACTIVE_JOB_STATUSES = new Set(['queued', 'running'])
const TERMINAL_JOB_STATUSES = new Set(['succeeded', 'failed'])

const { t } = useI18n()
const appStore = useAppStore()
const reconcileStepUp = useStepUp()

const preview = ref<VIPReconcilePreview | null>(null)
const previewLoading = ref(false)
const previewError = ref('')
const previewLimit = ref(50)
const previewCursor = ref('')
const previewCursorHistory = ref<string[]>([])

const job = ref<VIPReconcileJob | null>(null)
const jobLoading = ref(false)
const jobError = ref('')
const restoringJob = ref(false)
const submitting = ref(false)
const reason = ref('')
const reasonError = ref('')
const requestId = ref(generateRequestId())
let pollTimer: number | null = null

const previewItems = computed(() => preview.value?.items ?? [])
const previewPage = computed(() => previewCursorHistory.value.length + 1)
const jobIsActive = computed(() => Boolean(job.value && ACTIVE_JOB_STATUSES.has(job.value.status)))
const jobStatusUnknown = computed(() => Boolean(
  job.value
  && !ACTIVE_JOB_STATUSES.has(job.value.status)
  && !TERMINAL_JOB_STATUSES.has(job.value.status)
))

const previewStatCards = computed(() => {
  const stats = preview.value?.stats
  return [
    {
      key: 'eligibility_repair',
      label: t('admin.vipReconcile.preview.stats.eligibilityRepair'),
      hint: t('admin.vipReconcile.preview.stats.eligibilityRepairHint'),
      value: stats?.eligibility_repair ?? 0,
      className: 'border-sky-200 bg-sky-50 text-sky-800 dark:border-sky-500/30 dark:bg-sky-500/10 dark:text-sky-200'
    },
    {
      key: 'effective_change',
      label: t('admin.vipReconcile.preview.stats.effectiveChange'),
      hint: t('admin.vipReconcile.preview.stats.effectiveChangeHint'),
      value: stats?.effective_change ?? 0,
      className: 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-200'
    },
    {
      key: 'force_off_unchanged',
      label: t('admin.vipReconcile.preview.stats.forceOffUnchanged'),
      hint: t('admin.vipReconcile.preview.stats.forceOffUnchangedHint'),
      value: stats?.force_off_unchanged ?? 0,
      className: 'border-violet-200 bg-violet-50 text-violet-800 dark:border-violet-500/30 dark:bg-violet-500/10 dark:text-violet-200'
    },
    {
      key: 'invalid_order',
      label: t('admin.vipReconcile.preview.stats.invalidOrder'),
      hint: t('admin.vipReconcile.preview.stats.invalidOrderHint'),
      value: stats?.invalid_order ?? 0,
      className: 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200'
    },
    {
      key: 'deleted_user',
      label: t('admin.vipReconcile.preview.stats.deletedUser'),
      hint: t('admin.vipReconcile.preview.stats.deletedUserHint'),
      value: stats?.deleted_user ?? 0,
      className: 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-200'
    }
  ]
})

const jobMetrics = computed(() => {
  const current = job.value
  return [
    { key: 'scanned', label: t('admin.vipReconcile.job.scanned'), value: current?.scanned ?? 0 },
    { key: 'eligibility_repaired', label: t('admin.vipReconcile.job.eligibilityRepaired'), value: current?.eligibility_repaired ?? 0 },
    { key: 'effective_changed', label: t('admin.vipReconcile.job.effectiveChanged'), value: current?.effective_changed ?? 0 },
    { key: 'force_off_unchanged', label: t('admin.vipReconcile.job.forceOffUnchanged'), value: current?.force_off_unchanged ?? 0 },
    { key: 'already_correct', label: t('admin.vipReconcile.job.alreadyCorrect'), value: current?.already_correct ?? 0 },
    { key: 'deleted', label: t('admin.vipReconcile.job.deleted'), value: current?.deleted ?? 0 },
    { key: 'invalid_order', label: t('admin.vipReconcile.job.invalidOrder'), value: current?.invalid_order ?? 0 },
    { key: 'failed', label: t('admin.vipReconcile.job.failedCount'), value: current?.failed ?? 0 }
  ]
})

function generateRequestId(): string {
  try {
    if (typeof globalThis.crypto?.randomUUID === 'function') {
      return globalThis.crypto.randomUUID()
    }
  } catch {
    // Fall through to a locally unique request ID for restricted browsers.
  }
  return `vip-reconcile-${Date.now()}-${Math.random().toString(36).slice(2, 12)}`
}

function formatNumber(value: number): string {
  return Number.isFinite(value) ? value.toLocaleString() : '0'
}

function formatDateTime(value?: string | null): string {
  if (!value || value.startsWith('0001-01-01')) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function booleanLabel(value: boolean): string {
  return value ? t('common.yes') : t('common.no')
}

function vipModeLabel(mode: VIPMode): string {
  if (mode === 'AUTO' || mode === 'FORCE_ON' || mode === 'FORCE_OFF') {
    return t(`admin.users.vip.modes.${mode}`)
  }
  return t('admin.users.vip.unknownMode', { mode: mode || '—' })
}

function categoryLabel(category: string): string {
  const known: Record<string, string> = {
    ELIGIBILITY_REPAIR: 'eligibilityRepair',
    EFFECTIVE_CHANGE: 'effectiveChange',
    FORCE_OFF_UNCHANGED: 'forceOffUnchanged',
    INVALID_ORDER: 'invalidOrder',
    DELETED_USER: 'deletedUser'
  }
  const key = known[category]
  return key ? t(`admin.vipReconcile.preview.categories.${key}`) : category
}

function categoryClass(category: string): string {
  const classes: Record<string, string> = {
    ELIGIBILITY_REPAIR: 'bg-sky-100 text-sky-700 dark:bg-sky-500/20 dark:text-sky-200',
    EFFECTIVE_CHANGE: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-200',
    FORCE_OFF_UNCHANGED: 'bg-violet-100 text-violet-700 dark:bg-violet-500/20 dark:text-violet-200',
    INVALID_ORDER: 'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-200',
    DELETED_USER: 'bg-rose-100 text-rose-700 dark:bg-rose-500/20 dark:text-rose-200'
  }
  return classes[category] ?? 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-200'
}

function jobStatusLabel(status: string): string {
  if (ACTIVE_JOB_STATUSES.has(status) || TERMINAL_JOB_STATUSES.has(status)) {
    return t(`admin.vipReconcile.job.status.${status}`)
  }
  return t('admin.vipReconcile.job.status.unknown', { status })
}

function jobStatusClass(status: string): string {
  const classes: Record<string, string> = {
    queued: 'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-200',
    running: 'bg-sky-100 text-sky-700 dark:bg-sky-500/20 dark:text-sky-200',
    succeeded: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-200',
    failed: 'bg-rose-100 text-rose-700 dark:bg-rose-500/20 dark:text-rose-200'
  }
  return classes[status] ?? 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-200'
}

async function loadPreview(cursor: string = previewCursor.value): Promise<void> {
  if (previewLoading.value) return
  previewLoading.value = true
  previewError.value = ''
  try {
    preview.value = await adminAPI.users.getVIPReconcilePreview(cursor, previewLimit.value)
  } catch (error) {
    previewError.value = extractI18nErrorMessage(
      error,
      t,
      'admin.vipReconcile.errors',
      t('admin.vipReconcile.preview.loadFailed')
    )
  } finally {
    previewLoading.value = false
  }
}

async function restartPreview(): Promise<void> {
  previewCursor.value = ''
  previewCursorHistory.value = []
  preview.value = null
  await loadPreview('')
}

async function nextPreviewPage(): Promise<void> {
  const nextCursor = preview.value?.next_cursor
  if (!nextCursor || previewLoading.value) return
  previewCursorHistory.value.push(previewCursor.value)
  previewCursor.value = nextCursor
  await loadPreview(nextCursor)
}

async function previousPreviewPage(): Promise<void> {
  if (previewLoading.value || previewCursorHistory.value.length === 0) return
  const previousCursor = previewCursorHistory.value.pop() ?? ''
  previewCursor.value = previousCursor
  await loadPreview(previousCursor)
}

function prepareNewRequest(): void {
  if (submitting.value || jobIsActive.value) return
  requestId.value = generateRequestId()
  reason.value = ''
  reasonError.value = ''
}

function reportStepUpBlocked(error: unknown): boolean {
  if (!isStepUpBlocked(error)) return false
  appStore.showError(
    stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
      ? t('stepUp.adminApiKeyForbidden')
      : t('stepUp.notEnabled')
  )
  return true
}

async function submitJob(): Promise<void> {
  if (submitting.value || jobIsActive.value) return
  const normalizedReason = reason.value.trim()
  if (!normalizedReason) {
    reasonError.value = t('admin.vipReconcile.execute.reasonRequired')
    return
  }
  if (!requestId.value || requestId.value.length > 128) {
    appStore.showError(t('admin.vipReconcile.execute.invalidRequestId'))
    return
  }

  reasonError.value = ''
  submitting.value = true
  jobError.value = ''
  try {
    const response = await reconcileStepUp.run(() => adminAPI.users.createVIPReconcileJob({
      request_id: requestId.value,
      reason: normalizedReason
    }))
    job.value = response.job
    persistLastJobId(response.job_id)
    appStore.showSuccess(t('admin.vipReconcile.execute.accepted', { id: response.job_id }))
    applyJobPollingState()
  } catch (error) {
    if (isStepUpCancelled(error)) return
    if (reportStepUpBlocked(error)) return
    appStore.showError(extractI18nErrorMessage(
      error,
      t,
      'admin.vipReconcile.errors',
      t('admin.vipReconcile.execute.failed')
    ))
  } finally {
    submitting.value = false
  }
}

async function resumeFailedJob(): Promise<void> {
  if (!job.value || job.value.status !== 'failed' || submitting.value) return
  requestId.value = job.value.request_id
  reason.value = job.value.reason
  await submitJob()
}

function persistLastJobId(jobId: number): void {
  try {
    localStorage.setItem(LAST_JOB_STORAGE_KEY, String(jobId))
  } catch {
    // Persistence is a convenience; job execution does not depend on it.
  }
}

function removePersistedJobId(): void {
  try {
    localStorage.removeItem(LAST_JOB_STORAGE_KEY)
  } catch {
    // Ignore storage restrictions.
  }
}

function readPersistedJobId(): number | null {
  try {
    const value = Number(localStorage.getItem(LAST_JOB_STORAGE_KEY))
    return Number.isSafeInteger(value) && value > 0 ? value : null
  } catch {
    return null
  }
}

function stopPolling(): void {
  if (pollTimer !== null) {
    window.clearTimeout(pollTimer)
    pollTimer = null
  }
}

function schedulePolling(): void {
  stopPolling()
  if (!jobIsActive.value) return
  pollTimer = window.setTimeout(() => {
    pollTimer = null
    void loadJob(false)
  }, POLL_INTERVAL_MS)
}

function applyJobPollingState(): void {
  if (jobIsActive.value) {
    schedulePolling()
  } else {
    stopPolling()
  }
}

async function loadJob(showError: boolean = true): Promise<void> {
  if (!job.value || jobLoading.value) return
  stopPolling()
  jobLoading.value = true
  if (showError) jobError.value = ''
  try {
    job.value = await adminAPI.users.getVIPReconcileJob(job.value.id)
    jobError.value = ''
  } catch (error) {
    const message = extractI18nErrorMessage(
      error,
      t,
      'admin.vipReconcile.errors',
      t('admin.vipReconcile.job.loadFailed')
    )
    jobError.value = message
    if (showError) appStore.showError(message)
  } finally {
    jobLoading.value = false
    applyJobPollingState()
  }
}

async function restoreLastJob(): Promise<void> {
  const jobId = readPersistedJobId()
  if (!jobId) return
  restoringJob.value = true
  jobLoading.value = true
  try {
    job.value = await adminAPI.users.getVIPReconcileJob(jobId)
    requestId.value = job.value.request_id
    reason.value = job.value.reason
    applyJobPollingState()
  } catch (error) {
    removePersistedJobId()
    job.value = null
    jobError.value = extractI18nErrorMessage(
      error,
      t,
      'admin.vipReconcile.errors',
      t('admin.vipReconcile.job.restoreFailed')
    )
  } finally {
    jobLoading.value = false
    restoringJob.value = false
  }
}

onMounted(() => {
  void loadPreview('')
  void restoreLastJob()
})

onBeforeUnmount(() => {
  stopPolling()
})
</script>
