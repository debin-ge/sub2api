<template>
  <AppLayout>
    <div class="space-y-5" data-test="video-admin-page">
      <section class="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <div v-for="metric in overviewMetrics" :key="metric.key" class="card min-w-0 p-4">
          <p class="truncate text-xs font-medium text-gray-500 dark:text-gray-400">{{ metric.label }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ metric.value }}</p>
          <p class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400">{{ metric.meta }}</p>
        </div>
      </section>

			<section class="border-y border-gray-200 py-3 dark:border-dark-700" data-test="video-spool-health">
				<div class="mb-3 flex items-center justify-between gap-3">
					<h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.videos.health.title') }}</h2>
					<span :class="statusClass(overview?.spool?.last_sweep_result || 'not_run')">
						{{ stateLabel(overview?.spool?.last_sweep_result || 'not_run') }}
					</span>
				</div>
				<div class="grid grid-cols-2 gap-x-6 gap-y-3 text-sm md:grid-cols-4 xl:grid-cols-7">
					<div v-for="item in healthMetrics" :key="item.key" class="min-w-0">
						<p class="truncate text-xs text-gray-500 dark:text-gray-400">{{ item.label }}</p>
						<p class="mt-1 truncate font-medium text-gray-900 dark:text-gray-100">{{ item.value }}</p>
					</div>
				</div>
			</section>

      <section class="border-b border-gray-200 dark:border-dark-700">
        <div class="flex min-w-0 gap-1 overflow-x-auto" role="tablist" :aria-label="t('admin.videos.tabs.label')">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            role="tab"
            class="inline-flex h-10 flex-none items-center gap-2 border-b-2 px-3 text-sm font-medium transition-colors"
            :class="activeTab === tab.key
              ? 'border-primary-500 text-primary-600 dark:text-primary-400'
              : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'"
            :aria-selected="activeTab === tab.key"
            @click="selectTab(tab.key)"
          >
            <Icon :name="tab.icon" size="sm" />
            <span>{{ tab.label }}</span>
            <span v-if="tab.count !== undefined" class="font-mono text-xs text-gray-400">{{ tab.count }}</span>
          </button>
        </div>
      </section>

      <section class="flex flex-col gap-3 lg:flex-row lg:items-end">
        <label class="min-w-0 flex-1">
          <span class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('admin.videos.filters.search') }}</span>
          <div class="relative">
            <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-2.5 text-gray-400" />
            <input
              v-model.trim="filters.q"
              class="input h-9 w-full pl-9"
              :placeholder="t('admin.videos.filters.searchPlaceholder')"
              @keyup.enter="applyFilters"
            />
          </div>
        </label>
        <label v-if="activeTab === 'tasks'" class="w-full lg:w-44">
          <span class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('admin.videos.filters.generation') }}</span>
          <select v-model="filters.generation_state" class="input h-9 w-full">
            <option value="">{{ t('admin.videos.filters.all') }}</option>
            <option v-for="state in generationStates" :key="state" :value="state">{{ stateLabel(state) }}</option>
          </select>
        </label>
        <label v-if="activeTab === 'tasks'" class="w-full lg:w-44">
          <span class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('admin.videos.filters.billing') }}</span>
          <select v-model="filters.billing_state" class="input h-9 w-full">
            <option value="">{{ t('admin.videos.filters.all') }}</option>
            <option v-for="state in currentBillingStates" :key="state" :value="state">{{ stateLabel(state) }}</option>
          </select>
        </label>
        <label v-if="activeTab === 'resources' || activeTab === 'callbacks'" class="w-full lg:w-44">
          <span class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('admin.videos.filters.status') }}</span>
          <select v-model="filters.status" class="input h-9 w-full">
            <option value="">{{ t('admin.videos.filters.all') }}</option>
            <option v-for="state in currentStatusOptions" :key="state" :value="state">{{ stateLabel(state) }}</option>
          </select>
        </label>
        <label v-if="activeTab === 'tasks' || activeTab === 'resources'" class="w-full lg:w-32">
          <span class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('admin.videos.filters.account') }}</span>
          <input v-model.trim="filters.account_id" inputmode="numeric" class="input h-9 w-full" placeholder="ID" @keyup.enter="applyFilters" />
        </label>
        <div class="flex h-9 gap-2">
          <button type="button" class="btn btn-primary" @click="applyFilters">{{ t('common.apply') }}</button>
          <button type="button" class="btn btn-secondary px-2.5" :title="t('common.refresh')" :disabled="loading" @click="refresh">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </section>

      <section class="card overflow-hidden">
        <div v-if="error" class="border-b border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/20 dark:text-red-300">
          {{ error }}
        </div>
        <div class="overflow-x-auto">
          <table v-if="activeTab === 'tasks'" class="w-full min-w-[1080px] text-sm">
            <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400">
              <tr>
                <th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.task') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.owner') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.provider') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.operation') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.state') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.billing') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.amount') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.updated') }}</th>
                <th class="px-4 py-3 text-right font-medium">{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="task in taskRows" :key="task.public_id" class="hover:bg-gray-50/60 dark:hover:bg-dark-800/60">
                <td class="px-4 py-3">
                  <button class="block max-w-64 truncate font-mono text-xs text-primary-600 hover:underline dark:text-primary-400" @click="openTask(task)">{{ task.public_id }}</button>
                  <p class="mt-1 max-w-64 truncate text-xs text-gray-500">{{ task.public_model || '-' }}</p>
                </td>
                <td class="px-4 py-3 font-mono text-xs">U{{ task.user_id }} / A{{ task.account_id ?? '-' }}</td>
                <td class="px-4 py-3">{{ task.provider }}</td>
                <td class="px-4 py-3">{{ stateLabel(task.operation) }}</td>
                <td class="px-4 py-3"><span :class="statusClass(task.generation_state)">{{ stateLabel(task.generation_state) }}</span></td>
                <td class="px-4 py-3"><span :class="statusClass(task.billing_state)">{{ stateLabel(task.billing_state) }}</span></td>
				<td class="px-4 py-3 text-xs" data-test="video-task-amounts">
					<p v-if="task.actual_cost != null" class="font-mono font-medium text-emerald-600 dark:text-emerald-400">
						<span class="mr-1 font-sans text-gray-400">{{ actualAmountLabel(task) }}</span>{{ formatMoney(task.actual_cost, task.currency) }}
					</p>
					<p v-if="task.hold_amount != null" class="mt-0.5 font-mono text-gray-500 dark:text-gray-400">
						<span class="mr-1 font-sans text-gray-400">{{ t('admin.videos.detail.heldShort') }}</span>{{ formatMoney(task.hold_amount, task.currency) }}
					</p>
					<span v-if="task.actual_cost == null && task.hold_amount == null">-</span>
				</td>
                <td class="px-4 py-3 text-xs text-gray-500">{{ formatDate(task.updated_at) }}</td>
                <td class="px-4 py-3">
                  <div class="flex justify-end gap-1">
                    <button type="button" class="icon-action" :title="t('admin.videos.actions.inspect')" @click="openTask(task)"><Icon name="eye" size="sm" /></button>
                    <button v-if="canRetryGet(task)" type="button" class="icon-action" :disabled="actionLoading" :title="t('admin.videos.actions.retryGet')" @click="runTaskAction(task, 'get')"><Icon name="refresh" size="sm" /></button>
                    <button v-if="canRetrySettlement(task)" type="button" class="icon-action" :disabled="actionLoading" :title="t('admin.videos.actions.retrySettlement')" @click="runTaskAction(task, 'settlement')"><Icon name="dollar" size="sm" /></button>
                    <button v-if="canRetryDelete(task)" type="button" class="icon-action" :disabled="actionLoading" :title="t('admin.videos.actions.retryDelete')" @click="runTaskAction(task, 'delete')"><Icon name="trash" size="sm" /></button>
                  </div>
                </td>
              </tr>
              <tr v-if="!loading && taskRows.length === 0"><td colspan="9" class="px-4 py-12 text-center text-gray-500">{{ t('admin.videos.empty') }}</td></tr>
            </tbody>
          </table>

          <table v-else-if="activeTab === 'resources'" class="w-full min-w-[900px] text-sm">
            <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400"><tr>
              <th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.resource') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.owner') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.provider') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.account') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.model') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.status') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.updated') }}</th>
            </tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="resource in resourceRows" :key="resource.public_id">
                <td class="px-4 py-3"><p class="font-mono text-xs">{{ resource.public_id }}</p><p class="mt-1 font-mono text-xs text-gray-500">{{ resource.provider_resource_id }}</p></td>
                <td class="px-4 py-3 font-mono text-xs">U{{ resource.user_id }}</td><td class="px-4 py-3">{{ resource.provider }}</td><td class="px-4 py-3 font-mono">{{ resource.account_id }}</td><td class="px-4 py-3">{{ resource.model || '-' }}</td><td class="px-4 py-3"><span :class="statusClass(resource.status)">{{ stateLabel(resource.status) }}</span></td><td class="px-4 py-3 text-xs text-gray-500">{{ formatDate(resource.updated_at) }}</td>
              </tr>
              <tr v-if="!loading && resourceRows.length === 0"><td colspan="7" class="px-4 py-12 text-center text-gray-500">{{ t('admin.videos.empty') }}</td></tr>
            </tbody>
          </table>

          <table v-else-if="activeTab === 'unmatched'" class="w-full min-w-[900px] text-sm">
            <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400"><tr>
              <th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.event') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.provider') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.account') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.upstreamTask') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.providerEvent') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.created') }}</th>
            </tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="event in eventRows" :key="event.id"><td class="px-4 py-3">{{ event.event_type }}</td><td class="px-4 py-3">{{ event.provider || '-' }}</td><td class="px-4 py-3 font-mono">{{ event.account_id ?? '-' }}</td><td class="max-w-56 truncate px-4 py-3 font-mono text-xs">{{ event.provider_task_id || '-' }}</td><td class="max-w-56 truncate px-4 py-3 font-mono text-xs">{{ event.provider_event_id || '-' }}</td><td class="px-4 py-3 text-xs text-gray-500">{{ formatDate(event.created_at) }}</td></tr>
              <tr v-if="!loading && eventRows.length === 0"><td colspan="6" class="px-4 py-12 text-center text-gray-500">{{ t('admin.videos.empty') }}</td></tr>
            </tbody>
          </table>

          <table v-else class="w-full min-w-[980px] text-sm">
            <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400"><tr>
              <th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.callback') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.event') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.status') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.attempts') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.nextAttempt') }}</th><th class="px-4 py-3 font-medium">{{ t('admin.videos.columns.error') }}</th><th class="px-4 py-3 text-right font-medium">{{ t('common.actions') }}</th>
            </tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="callback in callbackRows" :key="callback.id"><td class="px-4 py-3 font-mono">#{{ callback.id }} / T{{ callback.task_id }}</td><td class="px-4 py-3">{{ callback.event_type }}</td><td class="px-4 py-3"><span :class="statusClass(callback.status)">{{ stateLabel(callback.status) }}</span></td><td class="px-4 py-3 font-mono">{{ callback.attempts }}</td><td class="px-4 py-3 text-xs text-gray-500">{{ formatDate(callback.next_attempt_at) }}</td><td class="max-w-72 truncate px-4 py-3 text-xs text-red-600 dark:text-red-300">{{ callback.last_error || '-' }}</td><td class="px-4 py-3 text-right"><button v-if="canRetryCallback(callback)" type="button" class="icon-action" :disabled="callbackActionID !== null" :title="t('admin.videos.actions.retryCallback')" @click="retryCallback(callback)"><Icon name="refresh" size="sm" /></button><span v-else>-</span></td></tr>
              <tr v-if="!loading && callbackRows.length === 0"><td colspan="7" class="px-4 py-12 text-center text-gray-500">{{ t('admin.videos.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
        <div v-if="loading" class="border-t border-gray-100 px-4 py-3 text-sm text-gray-500 dark:border-dark-700">{{ t('common.loading') }}</div>
        <Pagination v-if="pagination.total > 0" :page="pagination.page" :page-size="pagination.page_size" :total="pagination.total" @update:page="changePage" @update:page-size="changePageSize" />
      </section>
    </div>

    <BaseDialog :show="Boolean(selectedTask)" :title="t('admin.videos.detail.title')" width="extra-wide" @close="closeTask">
      <div v-if="selectedTask" class="space-y-5">
		<section v-for="section in detailSections" :key="section.key" :data-test="`video-detail-${section.key}`">
			<h4 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">{{ section.title }}</h4>
			<div class="grid grid-cols-1 gap-x-6 gap-y-3 text-sm md:grid-cols-2 xl:grid-cols-3">
				<div v-for="field in section.fields" :key="field.label" class="min-w-0 border-b border-gray-100 pb-2 dark:border-dark-700">
					<p class="text-xs text-gray-500">{{ field.label }}</p>
					<p class="mt-1 break-all font-mono text-gray-900 dark:text-gray-100">{{ field.value }}</p>
				</div>
			</div>
		</section>

        <div v-if="selectedTask.last_error_message" class="border-l-2 border-red-500 bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950/20 dark:text-red-300">
          <p class="font-medium">{{ selectedTask.last_error_code || selectedTask.last_error_kind }}</p><p class="mt-1 break-words">{{ selectedTask.last_error_message }}</p>
        </div>

		<div class="flex flex-wrap gap-2">
          <button v-if="canRetryGet(selectedTask)" type="button" class="btn btn-secondary" :disabled="actionLoading" @click="runTaskAction(selectedTask, 'get')"><Icon name="refresh" size="sm" class="mr-2" />{{ t('admin.videos.actions.retryGet') }}</button>
          <button v-if="canRetrySettlement(selectedTask)" type="button" class="btn btn-secondary" :disabled="actionLoading" @click="runTaskAction(selectedTask, 'settlement')"><Icon name="dollar" size="sm" class="mr-2" />{{ t('admin.videos.actions.retrySettlement') }}</button>
          <button v-if="canRetryDelete(selectedTask)" type="button" class="btn btn-secondary" :disabled="actionLoading" @click="runTaskAction(selectedTask, 'delete')"><Icon name="trash" size="sm" class="mr-2" />{{ t('admin.videos.actions.retryDelete') }}</button>
        </div>

        <section>
          <h4 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.videos.detail.timeline') }}</h4>
          <div class="max-h-80 overflow-y-auto border-y border-gray-100 dark:border-dark-700">
            <div v-for="event in taskEvents" :key="event.id" class="grid grid-cols-[9rem_minmax(0,1fr)] gap-4 border-b border-gray-100 py-3 text-sm last:border-0 dark:border-dark-700">
              <time class="text-xs text-gray-500">{{ formatDate(event.created_at) }}</time>
              <div class="min-w-0"><p class="font-medium text-gray-900 dark:text-gray-100">{{ event.event_type }}</p><p class="mt-1 break-words text-xs text-gray-500">{{ eventTransition(event) }}</p></div>
            </div>
            <p v-if="taskEvents.length === 0" class="py-8 text-center text-sm text-gray-500">{{ t('admin.videos.empty') }}</p>
          </div>
        </section>
      </div>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import videosAPI, {
  type VideoAdminCallback,
  type VideoAdminEvent,
  type VideoAdminOverview,
  type VideoAdminResource,
  type VideoAdminTask,
} from '@/api/admin/videos'

type TabKey = 'tasks' | 'resources' | 'unmatched' | 'callbacks'
type TaskAction = 'get' | 'settlement' | 'delete'

const { t } = useI18n()
const appStore = useAppStore()
const activeTab = ref<TabKey>('tasks')
const loading = ref(false)
const actionLoading = ref(false)
const callbackActionID = ref<number | null>(null)
const error = ref('')
const overview = ref<VideoAdminOverview | null>(null)
const taskRows = ref<VideoAdminTask[]>([])
const resourceRows = ref<VideoAdminResource[]>([])
const eventRows = ref<VideoAdminEvent[]>([])
const callbackRows = ref<VideoAdminCallback[]>([])
const selectedTask = ref<VideoAdminTask | null>(null)
const taskEvents = ref<VideoAdminEvent[]>([])
const pagination = reactive({ page: 1, page_size: 20, total: 0 })
const filters = reactive({ q: '', generation_state: '', billing_state: '', status: '', account_id: '' })
let overviewRequest = 0
let rowsRequest = 0
let detailRequest = 0
let disposed = false

const generationStates = ['preparing', 'held', 'submitting', 'queued', 'in_progress', 'completed', 'failed', 'cancelled', 'expired']
const billingStates = ['none', 'held', 'capture_pending', 'captured', 'release_pending', 'released']
const resourceStates = ['creating', 'ready', 'failed', 'expired', 'deleted']
const callbackStates = ['pending', 'delivering', 'delivered', 'failed', 'quarantined']

const tabs = computed(() => [
  { key: 'tasks' as const, label: t('admin.videos.tabs.tasks'), icon: 'chart' as const, count: taskTotal.value },
  { key: 'resources' as const, label: t('admin.videos.tabs.resources'), icon: 'cube' as const },
  { key: 'unmatched' as const, label: t('admin.videos.tabs.unmatched'), icon: 'link' as const, count: overview.value?.unmatched_webhooks ?? 0 },
  { key: 'callbacks' as const, label: t('admin.videos.tabs.callbacks'), icon: 'sync' as const, count: callbackAttention.value },
])

const taskTotal = computed(() => Object.values(overview.value?.tasks_by_generation ?? {}).reduce((sum, value) => sum + value, 0))
const activeTasks = computed(() => (overview.value?.tasks_by_generation?.queued ?? 0) + (overview.value?.tasks_by_generation?.in_progress ?? 0) + (overview.value?.tasks_by_generation?.submitting ?? 0))
const pendingBilling = computed(() => (overview.value?.tasks_by_billing?.capture_pending ?? 0) + (overview.value?.tasks_by_billing?.release_pending ?? 0))
const callbackAttention = computed(() => (overview.value?.callbacks_by_status?.failed ?? 0) + (overview.value?.callbacks_by_status?.quarantined ?? 0))
const overviewMetrics = computed(() => [
  { key: 'active', label: t('admin.videos.metrics.active'), value: formatNumber(activeTasks.value), meta: relativeAge(overview.value?.oldest_task_pending_at) },
  { key: 'billing', label: t('admin.videos.metrics.billing'), value: formatNumber(pendingBilling.value), meta: relativeAge(overview.value?.oldest_billing_at) },
  { key: 'callbacks', label: t('admin.videos.metrics.callbacks'), value: formatNumber(callbackAttention.value), meta: relativeAge(overview.value?.oldest_callback_at) },
  { key: 'unmatched', label: t('admin.videos.metrics.unmatched'), value: formatNumber(overview.value?.unmatched_webhooks ?? 0), meta: t('admin.videos.metrics.requiresReview') },
])
const healthMetrics = computed(() => {
	const spool = overview.value?.spool
	const queue = overview.value?.queue
	return [
		{ key: 'spool-enabled', label: t('admin.videos.health.spool'), value: spool?.enabled ? t('admin.videos.health.enabled') : t('admin.videos.health.disabled') },
		{ key: 'spool-bytes', label: t('admin.videos.health.disk'), value: `${formatBytes(spool?.current_bytes)} / ${formatBytes(spool?.max_bytes)}` },
		{ key: 'spool-utilization', label: t('admin.videos.health.utilization'), value: formatPercent(spool?.utilization) },
		{ key: 'spool-active', label: t('admin.videos.health.activeSessions'), value: formatNumber(spool?.active_sessions ?? 0) },
		{ key: 'spool-orphans', label: t('admin.videos.health.orphans'), value: formatNumber(spool?.orphan_candidates ?? 0) },
		{ key: 'spool-failures', label: t('admin.videos.health.cleanupFailures'), value: formatNumber(spool?.cleanup_failure_count ?? 0) },
		{ key: 'queue', label: t('admin.videos.health.queue'), value: overview.value?.queue_status === 'available' && queue ? `${queue.ready} / ${queue.delayed} / ${queue.active}` : t('admin.videos.health.unavailable') },
		{ key: 'last-sweep', label: t('admin.videos.health.lastSweep'), value: formatDate(spool?.last_sweep_at) },
	]
})
const currentStatusOptions = computed(() => activeTab.value === 'resources' ? resourceStates : callbackStates)
const currentBillingStates = computed(() => billingStates)
const detailSections = computed(() => {
  const task = selectedTask.value
  if (!task) return []
  return [
	{
		key: 'routing',
		title: t('admin.videos.detail.routing'),
		fields: [
			{ label: t('admin.videos.columns.task'), value: task.public_id },
			{ label: t('admin.videos.columns.upstreamTask'), value: task.provider_task_id || '-' },
			{ label: t('admin.videos.detail.ownerIds'), value: `U${task.user_id} / K${task.api_key_id ?? '-'} / G${task.group_id ?? '-'}` },
			{ label: t('admin.videos.detail.routeIds'), value: `C${task.channel_id ?? '-'} / A${task.account_id ?? '-'}` },
			{ label: t('admin.videos.detail.requestedModel'), value: task.requested_model || '-' },
			{ label: t('admin.videos.detail.publicModel'), value: task.public_model || '-' },
			{ label: t('admin.videos.detail.channelModel'), value: task.channel_model || '-' },
			{ label: t('admin.videos.detail.upstreamModel'), value: task.upstream_model || '-' },
			{ label: t('admin.videos.columns.provider'), value: task.provider || '-' },
			{ label: t('admin.videos.detail.endpoint'), value: task.endpoint || '-' },
			{ label: t('admin.videos.detail.stableClientToken'), value: task.stable_client_token || '-' },
		],
	},
	{
		key: 'generation',
		title: t('admin.videos.detail.generation'),
		fields: [
			{ label: t('admin.videos.columns.operation'), value: stateLabel(task.operation) },
			{ label: t('admin.videos.columns.state'), value: stateLabel(task.generation_state) },
			{ label: t('admin.videos.detail.version'), value: String(task.version ?? '-') },
			{ label: t('admin.videos.detail.leaseEpoch'), value: String(task.lease_epoch ?? '-') },
			{ label: t('admin.videos.detail.leaseExpiresAt'), value: task.lease_expires_at || '-' },
			{ label: t('admin.videos.detail.providerStatus'), value: task.provider_status || '-' },
			{ label: t('admin.videos.detail.progress'), value: formatProgress(task.progress) },
			{ label: t('admin.videos.detail.resolution'), value: task.resolution || '-' },
			{ label: t('admin.videos.detail.duration'), value: formatDurationSeconds(task.duration_seconds) },
			{ label: t('admin.videos.detail.videoTokens'), value: formatNumberValue(task.video_tokens) },
			{ label: t('admin.videos.detail.contentVariants'), value: formatList(task.content_variants) },
			{ label: t('admin.videos.detail.inputCount'), value: formatNumber(task.input_manifest?.length ?? 0) },
			{ label: t('admin.videos.detail.deleteState'), value: stateLabel(task.delete_state) },
		],
	},
	{
		key: 'billing',
		title: t('admin.videos.detail.billing'),
		fields: [
			{ label: t('admin.videos.columns.billing'), value: stateLabel(task.billing_state) },
			{ label: t('admin.videos.detail.billingUnit'), value: billingUnitLabel(task.billing_unit) },
			{ label: t('admin.videos.detail.unitPrice'), value: formatVideoPrice(task.unit_price, task.billing_unit, task.currency) },
			{ label: t('admin.videos.detail.estimatedUnits'), value: formatUnits(task.estimated_units, task.billing_unit) },
			{ label: t('admin.videos.detail.actualUnits'), value: formatUnits(task.actual_units, task.billing_unit) },
			{ label: t('admin.videos.detail.multiplier'), value: task.customer_multiplier == null ? '-' : `${task.customer_multiplier.toFixed(4)}x` },
			{ label: t('admin.videos.detail.estimatedCost'), value: formatMoney(task.estimated_cost, task.currency) },
			{ label: t('admin.videos.detail.heldAmount'), value: formatMoney(task.hold_amount, task.currency) },
			{ label: task.billing_state === 'captured' ? t('admin.videos.detail.chargedAmount') : t('admin.videos.detail.payableAmount'), value: formatMoney(task.actual_cost, task.currency) },
			{ label: t('admin.videos.detail.currency'), value: task.currency || '-' },
			{ label: t('admin.videos.detail.pricingSource'), value: task.pricing_source || '-' },
			{ label: t('admin.videos.detail.pricingRule'), value: task.pricing_rule_key || '-' },
			{ label: t('admin.videos.detail.holdId'), value: task.hold_id || '-' },
		],
	},
	{
		key: 'lifecycle',
		title: t('admin.videos.detail.lifecycle'),
		fields: [
			{ label: t('admin.videos.columns.created'), value: formatDate(task.created_at) },
			{ label: t('admin.videos.detail.submittedAt'), value: formatDate(task.submitted_at) },
			{ label: t('admin.videos.detail.startedAt'), value: formatDate(task.started_at) },
			{ label: t('admin.videos.detail.providerCreatedAt'), value: formatDate(task.provider_created_at) },
			{ label: t('admin.videos.detail.providerFinishedAt'), value: formatDate(task.provider_finished_at) },
			{ label: t('admin.videos.detail.finishedAt'), value: formatDate(task.finished_at) },
			{ label: t('admin.videos.detail.settledAt'), value: formatDate(task.settled_at) },
			{ label: t('admin.videos.columns.updated'), value: formatDate(task.updated_at) },
			{ label: t('admin.videos.detail.contentExpiresAt'), value: formatDate(task.content_expires_at) },
			{ label: t('admin.videos.detail.access'), value: task.provider_access.configured ? `${task.provider_access.kind || '-'} / ${task.provider_access.scope || '-'} / ${formatDate(task.provider_access.expires_at)}` : '-' },
			{ label: t('admin.videos.detail.callback'), value: task.callback_configured ? t('common.yes') : t('common.no') },
		],
	},
  ]
})

function numericFilter(value: string): number | undefined {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined
}

async function loadOverview() {
  if (disposed) return
  const request = ++overviewRequest
  try {
    const value = await videosAPI.overview()
    if (!disposed && request === overviewRequest) overview.value = value
  } catch (cause) {
    if (!disposed && request === overviewRequest) error.value = errorMessage(cause)
  }
}

async function loadRows() {
  if (disposed) return
  const request = ++rowsRequest
  const tab = activeTab.value
  loading.value = true
  error.value = ''
  try {
    if (tab === 'tasks') {
      const query = { page: pagination.page, page_size: pagination.page_size, q: filters.q || undefined, generation_state: filters.generation_state || undefined, billing_state: filters.billing_state || undefined, account_id: numericFilter(filters.account_id) }
      const page = await videosAPI.listTasks(query)
      if (disposed || request !== rowsRequest) return
      taskRows.value = page.items
      setPagination(page)
    } else if (tab === 'resources') {
      const page = await videosAPI.listResources({ page: pagination.page, page_size: pagination.page_size, q: filters.q || undefined, status: filters.status || undefined, account_id: numericFilter(filters.account_id) })
      if (disposed || request !== rowsRequest) return
      resourceRows.value = page.items
      setPagination(page)
    } else if (tab === 'unmatched') {
      const page = await videosAPI.listUnmatchedEvents(pagination.page, pagination.page_size)
      if (disposed || request !== rowsRequest) return
      eventRows.value = page.items
      setPagination(page)
		} else {
      const page = await videosAPI.listCallbacks({ page: pagination.page, page_size: pagination.page_size, status: filters.status || undefined })
      if (disposed || request !== rowsRequest) return
      callbackRows.value = page.items
      setPagination(page)
    }
  } catch (cause) {
    if (!disposed && request === rowsRequest) error.value = errorMessage(cause)
  } finally {
    if (!disposed && request === rowsRequest) loading.value = false
  }
}

function setPagination(page: { total: number; page: number; page_size: number }) {
  pagination.total = page.total
  pagination.page = page.page
  pagination.page_size = page.page_size
}

async function refresh() {
  try { await Promise.all([loadOverview(), loadRows()]) } catch (cause) { error.value = errorMessage(cause) }
}

function selectTab(tab: TabKey) {
  activeTab.value = tab
  pagination.page = 1
	filters.generation_state = ''
	filters.billing_state = ''
  filters.status = ''
  void loadRows()
}

function applyFilters() { pagination.page = 1; void loadRows() }
function changePage(page: number) { pagination.page = page; void loadRows() }
function changePageSize(size: number) { pagination.page_size = size; pagination.page = 1; void loadRows() }

async function openTask(task: VideoAdminTask) {
	if (disposed) return
	const request = ++detailRequest
	selectedTask.value = task
	taskEvents.value = []
  try {
    const [detail, events] = await Promise.all([videosAPI.getTask(task.public_id), videosAPI.listEvents(task.public_id)])
		if (disposed || request !== detailRequest) return
		selectedTask.value = detail
    taskEvents.value = events.items
  } catch (cause) {
    if (!disposed && request === detailRequest) appStore.showError(errorMessage(cause))
  }
}

function closeTask() {
  detailRequest++
  selectedTask.value = null; taskEvents.value = []
}

async function refreshSelectedTask(task: VideoAdminTask, request: number) {
  if (!disposed && request === detailRequest && selectedTask.value?.public_id === task.public_id) await openTask(task)
}

async function runTaskAction(task: VideoAdminTask, action: TaskAction) {
  if (actionLoading.value || disposed) return
  const request = detailRequest
  actionLoading.value = true
  try {
    const updated = action === 'get' ? await videosAPI.retryGet(task.public_id, task.version) : action === 'settlement' ? await videosAPI.retrySettlement(task.public_id, task.version) : await videosAPI.retryDelete(task.public_id, task.version)
    await refreshSelectedTask(updated, request)
    appStore.showSuccess(t('admin.videos.actions.requested'))
    await refresh()
  } catch (cause) { appStore.showError(errorMessage(cause)) } finally { actionLoading.value = false }
}

async function retryCallback(callback: VideoAdminCallback) {
	if (callbackActionID.value !== null || disposed || !canRetryCallback(callback)) return
	callbackActionID.value = callback.id
	try {
		const updated = await videosAPI.retryCallback(callback.id)
		const index = callbackRows.value.findIndex(item => item.id === callback.id)
		if (index >= 0) callbackRows.value.splice(index, 1, updated)
		appStore.showSuccess(t('admin.videos.actions.requested'))
		await loadOverview()
	} catch (cause) {
		appStore.showError(errorMessage(cause))
	} finally {
		callbackActionID.value = null
	}
}

function canRetryGet(task: VideoAdminTask) { return Boolean(task.provider_task_id) && task.billing_state === 'held' && ['queued', 'in_progress'].includes(task.generation_state) }
function canRetrySettlement(task: VideoAdminTask) { return task.billing_state === 'capture_pending' || task.billing_state === 'release_pending' }
function canRetryDelete(task: VideoAdminTask) { return ['requested', 'deleting', 'delete_failed'].includes(task.delete_state) }
function canRetryCallback(callback: VideoAdminCallback) { return ['failed', 'quarantined'].includes(callback.status) && new Date(callback.expires_at).getTime() > Date.now() }

function stateLabel(state: string) { return t(`admin.videos.states.${state}`, state) }
function statusClass(state: string) {
  const base = 'inline-flex min-h-6 items-center rounded px-2 py-0.5 text-xs font-medium'
	  if (['completed', 'captured', 'released', 'ready', 'delivered', 'success', 'done', 'billed', 'waived', 'approved', 'already_durable'].includes(state)) return `${base} bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300`
	  if (['failed', 'delete_failed', 'quarantined', 'error'].includes(state)) return `${base} bg-red-50 text-red-700 dark:bg-red-950/30 dark:text-red-300`
  if (['queued', 'held', 'pending', 'billing', 'capture_pending', 'release_pending', 'retry_billing'].includes(state)) return `${base} bg-amber-50 text-amber-700 dark:bg-amber-950/30 dark:text-amber-300`
  return `${base} bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300`
}
function formatNumber(value: number) { return new Intl.NumberFormat().format(value) }
function formatNumberValue(value?: number) { return value == null ? '-' : new Intl.NumberFormat(undefined, { maximumFractionDigits: 6 }).format(value) }
function formatMoney(value: number | undefined, currency = 'USD') { return value === undefined ? '-' : new Intl.NumberFormat(undefined, { style: 'currency', currency: currency || 'USD', maximumFractionDigits: 6 }).format(value) }
function formatBytes(value?: number) { if (value == null) return '-'; const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']; let current = Math.max(0, value); let unit = 0; while (current >= 1024 && unit < units.length - 1) { current /= 1024; unit += 1 } return `${current.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}` }
function formatPercent(value?: number) { return value == null ? '-' : `${(Math.max(0, value) * 100).toFixed(1)}%` }
function formatProgress(value?: number) { return value == null ? '-' : `${value.toFixed(value % 1 === 0 ? 0 : 1)}%` }
function actualAmountLabel(task: VideoAdminTask) { return task.billing_state === 'captured' ? t('admin.videos.detail.chargedShort') : t('admin.videos.detail.payableShort') }
function formatDurationSeconds(value?: number) { return value == null ? '-' : `${formatNumberValue(value)} ${t('admin.videos.units.seconds')}` }
function formatList(values?: string[]) { return values?.length ? values.join(', ') : '-' }
function billingUnitLabel(unit?: string) { return unit ? t(`admin.videos.units.${unit}`, unit) : '-' }
function formatUnits(value: number | undefined, unit?: string) { return value == null ? '-' : `${formatNumberValue(value)} ${billingUnitLabel(unit)}` }
function formatVideoPrice(value: number | undefined, unit?: string, currency = 'USD') {
	if (value == null) return '-'
	if (unit === 'video_token') return `${formatMoney(value * 1_000_000, currency)} / ${t('admin.videos.units.millionVideoTokens')}`
	return `${formatMoney(value, currency)} / ${billingUnitLabel(unit)}`
}
function formatDate(value?: string) { if (!value) return '-'; const date = new Date(value); return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString() }
function relativeAge(value?: string) { if (!value) return t('admin.videos.metrics.nonePending'); const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000)); if (seconds < 60) return t('admin.videos.metrics.ageSeconds', { value: seconds }); if (seconds < 3600) return t('admin.videos.metrics.ageMinutes', { value: Math.floor(seconds / 60) }); return t('admin.videos.metrics.ageHours', { value: Math.floor(seconds / 3600) }) }
function eventTransition(event: VideoAdminEvent) { const values = [event.from_generation_state && event.to_generation_state ? `${event.from_generation_state} -> ${event.to_generation_state}` : '', event.from_billing_state && event.to_billing_state ? `${event.from_billing_state} -> ${event.to_billing_state}` : '', event.provider_event_id || ''].filter(Boolean); return values.join(' | ') || '-' }
function errorMessage(cause: unknown) { return cause instanceof Error ? cause.message : t('admin.videos.loadFailed') }

onMounted(() => { void refresh() })
onBeforeUnmount(() => { disposed = true; overviewRequest++; rowsRequest++; detailRequest++ })
</script>

<style scoped>
.icon-action {
  @apply inline-flex h-8 w-8 items-center justify-center rounded border border-gray-200 text-gray-500 transition-colors hover:border-primary-300 hover:bg-primary-50 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-600 dark:text-gray-400 dark:hover:border-primary-700 dark:hover:bg-primary-950/20 dark:hover:text-primary-300;
}
</style>
