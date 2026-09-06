import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get, post, put } }))

import {
	getVideoCapabilityCatalog,
	getVideoAccountCapability,
  listUnknownVideoTasks,
  listVideoTaskEvents,
	resolveVideoCreated,
	resolveVideoBillingCapture,
	resolveVideoBillingRelease,
  resolveVideoNotCreated,
  retryVideoCallback,
  retryVideoGet,
	retryVideoSettlement,
	probeVideoAccountCapability,
	updateVideoCapabilityCatalog,
	listVideoBillingReviews,
	decideVideoBillingReview,
	listVideoSubmissionReviews,
	decideVideoSubmissionReview,
	retryVideoCharacterResource,
} from '@/api/admin/videos'
import videosAPI from '@/api/admin/videos'

describe('admin videos api', () => {
  it('does not export Grok migration or correction commands', () => {
    expect(Object.keys(videosAPI).filter(key => /grok|createintent/i.test(key))).toEqual([])
  })

  beforeEach(() => {
    get.mockReset()
		post.mockReset()
		put.mockReset()
	})

	it('resolves manual billing review without accepting an arbitrary cost', async () => {
		post.mockResolvedValue({ data: {} })

		const evidence = { reason: 'Verified provider usage', evidence_ref: 'ticket:TEST' }
		await resolveVideoBillingCapture('video_local', 3.5, 7, evidence, 'capture:TEST')
		await resolveVideoBillingRelease('video_local', 7, evidence, 'release:TEST')

		expect(post).toHaveBeenNthCalledWith(1, '/admin/videos/tasks/video_local/resolve-billing-capture', { actual_units: 3.5, ...evidence }, { headers: { 'If-Match': '"7"', 'Idempotency-Key': 'capture:TEST' } })
		expect(post).toHaveBeenNthCalledWith(2, '/admin/videos/tasks/video_local/resolve-billing-release', evidence, { headers: { 'If-Match': '"7"', 'Idempotency-Key': 'release:TEST' } })
	})

	it('lists and decides durable reviews with version and idempotency', async () => {
		get.mockResolvedValue({ data: [] }); post.mockResolvedValue({ data: {} })
		await listVideoBillingReviews('video_local')
		await decideVideoBillingReview('video_local', 12, true, 'Independently verified', 8, 'approve:TEST')
		expect(get).toHaveBeenCalledWith('/admin/videos/tasks/video_local/billing-reviews')
		expect(post).toHaveBeenCalledWith('/admin/videos/tasks/video_local/billing-reviews/12/approve', { reason: 'Independently verified' }, { headers: { 'If-Match': '"8"', 'Idempotency-Key': 'approve:TEST' } })
	})

	it('reads and replaces the versioned capability catalog', async () => {
		const catalog = { version: 1, providers: {} }
		get.mockResolvedValue({ data: catalog })
		put.mockResolvedValue({ data: { ...catalog, source: 'settings' } })

		await getVideoCapabilityCatalog()
		await updateVideoCapabilityCatalog(catalog)

		expect(get).toHaveBeenCalledWith('/admin/videos/capabilities')
		expect(put).toHaveBeenCalledWith('/admin/videos/capabilities', catalog)
	})

	it('reads and reruns a non-billing account capability probe', async () => {
		get.mockResolvedValue({ data: { account_id: 42 } })
		post.mockResolvedValue({ data: { account_id: 42 } })

		await getVideoAccountCapability(42)
		await probeVideoAccountCapability(42)

		expect(get).toHaveBeenCalledWith('/admin/videos/accounts/42/capability')
		expect(post).toHaveBeenCalledWith('/admin/videos/accounts/42/capability/probe')
	})

  it('uses the dedicated unknown queue and task timeline endpoints', async () => {
    get.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20 } })

    await listUnknownVideoTasks({ page: 2, account_id: 9 })
    await listVideoTaskEvents('video_local', 1, 100)

    expect(get).toHaveBeenNthCalledWith(1, '/admin/videos/tasks/unknown', {
      params: { page: 2, account_id: 9 },
    })
    expect(get).toHaveBeenNthCalledWith(2, '/admin/videos/tasks/video_local/events', {
      params: { page: 1, page_size: 100 },
    })
  })

  it('exposes exact unknown resolution and safe retries without a create replay helper', async () => {
    post.mockResolvedValue({ data: {} })

    const evidence = { reason: 'Original submission verified', evidence_ref: 'ticket:UNKNOWN' }
    await resolveVideoCreated('video_local', 'video_exact', 7, evidence, 'submission:create')
    await resolveVideoNotCreated('video_local', 7, evidence, 'submission:release')
    await retryVideoGet('video_local', 7)
    await retryVideoSettlement('video_local', 7)
    await retryVideoCallback(17)

    expect(post).toHaveBeenNthCalledWith(1, '/admin/videos/tasks/video_local/resolve-created', {
      provider_task_id: 'video_exact',
      ...evidence,
    }, { headers: { 'If-Match': '"7"', 'Idempotency-Key': 'submission:create' } })
    expect(post).toHaveBeenNthCalledWith(2, '/admin/videos/tasks/video_local/resolve-not-created', evidence, { headers: { 'If-Match': '"7"', 'Idempotency-Key': 'submission:release' } })
    expect(post).toHaveBeenNthCalledWith(3, '/admin/videos/tasks/video_local/retry-get', undefined, { headers: { 'If-Match': '"7"' } })
    expect(post).toHaveBeenNthCalledWith(4, '/admin/videos/tasks/video_local/retry-settlement', undefined, { headers: { 'If-Match': '"7"' } })
    expect(post).toHaveBeenNthCalledWith(5, '/admin/videos/callbacks/17/retry')
  })
  it('rejects missing or invalid versions before sending a mutation', async () => {
    await expect(retryVideoGet('video_local', Number.NaN)).rejects.toThrow('valid version')
    await expect(retryVideoGet('video_local', -1)).rejects.toThrow('valid version')
    expect(post).not.toHaveBeenCalled()
  })

  it('separates submission approvals from bound-character repair', async () => {
    get.mockResolvedValue({ data: [] }); post.mockResolvedValue({ data: {} })
    await listVideoSubmissionReviews('video_local')
    await decideVideoSubmissionReview('video_local', 22, true, 'Independent evidence verified', 8, 'submission:approve')
    await retryVideoCharacterResource('video_local', 9)
    expect(get).toHaveBeenCalledWith('/admin/videos/tasks/video_local/submission-reviews')
    expect(post).toHaveBeenNthCalledWith(1, '/admin/videos/tasks/video_local/submission-reviews/22/approve', { reason: 'Independent evidence verified' }, { headers: { 'If-Match': '"8"', 'Idempotency-Key': 'submission:approve' } })
    expect(post).toHaveBeenNthCalledWith(2, '/admin/videos/tasks/video_local/retry-character-resource', undefined, { headers: { 'If-Match': '"9"' } })
  })
})
