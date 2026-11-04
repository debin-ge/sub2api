import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get, post, put } }))

import {
	getVideoCapabilityCatalog,
	getVideoAccountCapability,
  listVideoTaskEvents,
  previewGrokVideoMigration,
  retryVideoCallback,
  retryVideoGet,
	retryVideoSettlement,
	probeVideoAccountCapability,
	updateVideoCapabilityCatalog,
} from '@/api/admin/videos'
import videosAPI from '@/api/admin/videos'

describe('admin videos api', () => {
  it('exposes only the read-only Grok migration preview, without correction commands', () => {
    expect(Object.keys(videosAPI).filter(key => /grok|createintent/i.test(key))).toEqual(['previewGrokMigration'])
  })

  it('does not export any manual review command', () => {
    // Only this read-only preview is allowed; retain the guard on other review commands.
    expect(Object.keys(videosAPI).filter(key => key !== 'previewGrokMigration' && /review|resolve|unknown/i.test(key))).toEqual([])
  })

  beforeEach(() => {
    get.mockReset()
		post.mockReset()
		put.mockReset()
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

  it('posts migration samples to the read-only preview endpoint', async () => {
    const request = {
      api_key_id: 12,
      account_id: 42,
      cases: [{ model: 'grok-imagine-video-1.5', operation: 'generate' as const, resolution: '480p', seconds: 8 }],
    }
    const report = { ready: true, comparisons: [], warnings: [], fingerprint: 'preview-fingerprint' }
    post.mockResolvedValue({ data: report })

    expect(videosAPI.previewGrokMigration).toBe(previewGrokVideoMigration)
    await expect(previewGrokVideoMigration(request)).resolves.toEqual(report)
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith('/admin/videos/grok-migration/preview', request)
    expect(put).not.toHaveBeenCalled()
  })

  it('uses the task timeline endpoint', async () => {
    get.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20 } })

    await listVideoTaskEvents('video_local', 1, 100)

    expect(get).toHaveBeenCalledWith('/admin/videos/tasks/video_local/events', {
      params: { page: 1, page_size: 100 },
    })
  })

  it('exposes only idempotent retries, never a create replay helper', async () => {
    post.mockResolvedValue({ data: {} })

    await retryVideoGet('video_local', 7)
    await retryVideoSettlement('video_local', 7)
    await retryVideoCallback(17)

    expect(post).toHaveBeenNthCalledWith(1, '/admin/videos/tasks/video_local/retry-get', undefined, { headers: { 'If-Match': '"7"' } })
    expect(post).toHaveBeenNthCalledWith(2, '/admin/videos/tasks/video_local/retry-settlement', undefined, { headers: { 'If-Match': '"7"' } })
    expect(post).toHaveBeenNthCalledWith(3, '/admin/videos/callbacks/17/retry')
  })

  it('rejects missing or invalid versions before sending a mutation', async () => {
    await expect(retryVideoGet('video_local', Number.NaN)).rejects.toThrow('valid version')
    await expect(retryVideoGet('video_local', -1)).rejects.toThrow('valid version')
    expect(post).not.toHaveBeenCalled()
  })
})
