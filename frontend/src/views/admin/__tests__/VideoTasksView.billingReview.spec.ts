import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import VideoTasksView from '../VideoTasksView.vue'

const { api, app, auth } = vi.hoisted(() => ({
	api: { overview: vi.fn(), listTasks: vi.fn(), getTask: vi.fn(), listEvents: vi.fn(), listBillingReviews: vi.fn(), resolveBillingCapture: vi.fn(), decideBillingReview: vi.fn(), listSubmissionReviews: vi.fn(), resolveCreated: vi.fn(), resolveNotCreated: vi.fn(), decideSubmissionReview: vi.fn(), },
	app: { showError: vi.fn(), showSuccess: vi.fn() },
	auth: { user: { id: 99 } },
}))

vi.mock('@/api/admin/videos', () => ({ default: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const task = {
	id: 1, public_id: 'video_review_test', version: 7, source: 'managed', user_id: 42, account_id: 11,
	provider: 'openai', operation: 'generate', generation_state: 'failed', billing_state: 'manual_review', delete_state: 'none',
	billing_unit: 'second', actual_units: 3, hold_amount: 5, currency: 'USD', price_snapshot: {}, provider_cost_snapshot: {},
	request_attributes: {}, usage_snapshot: {}, response_metadata: {}, provider_access: { configured: false }, callback_configured: false,
}

async function showTask() {
	const wrapper = mount(VideoTasksView, { global: { stubs: {
		AppLayout: { template: '<div><slot /></div>' }, BaseDialog: { template: '<div><slot /></div>' }, Pagination: true, Icon: true,
	} } })
	await flushPromises()
	await wrapper.findAll('button').find(button => button.text() === task.public_id)!.trigger('click')
	await flushPromises()
	return wrapper
}

describe('video billing review interaction', () => {
	beforeEach(() => {
		vi.clearAllMocks()
		api.overview.mockResolvedValue({})
		api.listTasks.mockResolvedValue({ items: [task], total: 1, page: 1, page_size: 20 })
		api.getTask.mockResolvedValue(task)
		api.listEvents.mockResolvedValue({ items: [] })
		api.listBillingReviews.mockResolvedValue([])
		api.listSubmissionReviews.mockResolvedValue([])
		vi.spyOn(window, 'confirm').mockReturnValue(true)
	})

	it('requires evidence and reuses the same key after an uncertain response', async () => {
		api.resolveBillingCapture.mockRejectedValue(new Error('network unavailable'))
		const wrapper = await showTask()
		const capture = wrapper.findAll('button').find(button => button.text() === 'admin.videos.actions.resolveCapture')!
		expect(capture.attributes('disabled')).toBeDefined()
		await wrapper.find('input[placeholder="admin.videos.billingReview.reason"]').setValue('Verified invoice evidence')
		await wrapper.find('input[placeholder="admin.videos.billingReview.evidenceRef"]').setValue('ticket:REVIEW')
		expect(capture.attributes('disabled')).toBeUndefined()
		await capture.trigger('click'); await flushPromises()
		await capture.trigger('click'); await flushPromises()
		expect(api.resolveBillingCapture).toHaveBeenCalledTimes(2)
		expect(api.resolveBillingCapture.mock.calls[0]).toEqual(api.resolveBillingCapture.mock.calls[1])
		expect(api.resolveBillingCapture.mock.calls[0][3]).toEqual({ reason: 'Verified invoice evidence', evidence_ref: 'ticket:REVIEW', honor_frozen_quote: false })
		expect(api.resolveBillingCapture.mock.calls[0][4]).toBeTruthy()
		wrapper.unmount()
	})

	it('shows the frozen evidence and prevents the proposer approving their own request', async () => {
		api.listBillingReviews.mockResolvedValue([{ id: 12, action: 'capture', status: 'pending', proposed_by: 99, actual_cost: 3,
			reason: 'Provider evidence reviewed', evidence_ref: 'ticket:REVIEW', facts: { upstream_model: 'frozen-model' } }])
		const wrapper = await showTask()
		await wrapper.find('input[placeholder="admin.videos.billingReview.decisionReason"]').setValue('Independent evidence checked')
		const approve = wrapper.findAll('button').find(button => button.text() === 'admin.videos.billingReview.approve')!
		expect(approve.attributes('disabled')).toBeDefined()
		expect(wrapper.text()).toContain('frozen-model')
		expect(api.decideBillingReview).not.toHaveBeenCalled()
		wrapper.unmount()
	})

	it('requires submission evidence and preserves the operation key after an uncertain proposal', async () => {
		api.getTask.mockResolvedValue({ ...task, generation_state: 'submission_unknown', billing_state: 'held' })
		api.resolveCreated.mockRejectedValue(new Error('response lost'))
		const wrapper = await showTask()
		const propose = wrapper.findAll('button').find(button => button.text() === 'admin.videos.actions.confirmCreated')!
		expect(propose.attributes('disabled')).toBeDefined()
		await wrapper.find('input[placeholder="admin.videos.billingReview.reason"]').setValue('Verified original submission ownership')
		await wrapper.find('input[placeholder="admin.videos.billingReview.evidenceRef"]').setValue('ticket:UNKNOWN')
		await wrapper.find('input[placeholder="admin.videos.unknown.providerIdPlaceholder"]').setValue('video_exact')
		expect(propose.attributes('disabled')).toBeUndefined()
		await propose.trigger('click'); await flushPromises()
		await propose.trigger('click'); await flushPromises()
		expect(api.resolveCreated).toHaveBeenCalledTimes(2)
		expect(api.resolveCreated.mock.calls[0]).toEqual(api.resolveCreated.mock.calls[1])
		expect(api.resolveCreated.mock.calls[0][4]).toBeTruthy()
		expect(api.resolveNotCreated).not.toHaveBeenCalled()
		wrapper.unmount()
	})

	it('blocks self-approval and duplicate pending submission proposals', async () => {
		api.getTask.mockResolvedValue({ ...task, generation_state: 'submission_unknown', billing_state: 'held' })
		api.listSubmissionReviews.mockResolvedValue([{ id: 22, action: 'created', status: 'pending', proposed_by: 99,
			provider_task_id: 'video_exact', reason: 'Original evidence verified', evidence_ref: 'ticket:UNKNOWN', facts: { request_hash: 'original-submission' } }])
		const wrapper = await showTask()
		await wrapper.find('input[placeholder="admin.videos.billingReview.decisionReason"]').setValue('Decision reason entered')
		const history = wrapper.find('[data-test="video-submission-reviews"]')
		expect(history.text()).toContain('original-submission')
		const approve = history.findAll('button').find(button => button.text() === 'admin.videos.billingReview.approve')!
		expect(approve.attributes('disabled')).toBeDefined()
		expect(api.decideSubmissionReview).not.toHaveBeenCalled()
		wrapper.unmount()
	})
})
