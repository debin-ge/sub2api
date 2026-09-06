import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const viewPath = resolve(dirname(fileURLToPath(import.meta.url)), '../VideoTasksView.vue')
const source = readFileSync(viewPath, 'utf8')

describe('VideoTasksView recovery surface', () => {
  it('does not expose Grok migration, correction, or dispatch-review tools', () => {
    for (const removed of ['selectedGrokJob', 'selectedLegacyImport', 'selectedCreateIntent', 'grok-video-jobs', 'grok-correction-execution', 'scanLegacyImports']) {
      expect(source).not.toContain(removed)
    }
  })

  it('covers every operational queue and keeps Create replay absent', () => {
    for (const tab of ['tasks', 'unknown', 'resources', 'unmatched', 'callbacks']) {
      expect(source).toContain(`'${tab}'`)
    }
    expect(source).toContain('resolveCreated')
    expect(source).toContain('resolveNotCreated')
    expect(source).toContain('retryGet')
    expect(source).toContain('retrySettlement')
    expect(source).toContain('retryDelete')
	expect(source).toContain('retryCallback')
	expect(source).toContain('canRetryCallback')
		expect(source).toContain('resolveBillingCapture')
		expect(source).toContain('resolveBillingRelease')
		expect(source).toContain('repairCharacterResource')
		expect(source).toContain('isCharacterPersistenceReview')
		expect(source).toContain("t('admin.videos.actions.repairResource')")
    expect(source).not.toContain('retryCreate')
    expect(source).not.toContain('replayCreate')
  })

  it('renders only access metadata fields supplied by the safe admin DTO', () => {
    expect(source).toContain('provider_access.configured')
    expect(source).toContain('provider_access.kind')
    expect(source).toContain('provider_access.scope')
    expect(source).toContain('provider_access.expires_at')
    expect(source).not.toContain('provider_access.value')
    expect(source).not.toContain('provider_access_enc')
    expect(source).not.toContain('callback_url_enc')
  })

	it('separates routing, generation, billing, lifecycle, held amount, and actual charge', () => {
		for (const section of ['routing', 'generation', 'billing', 'lifecycle']) {
			expect(source).toContain(`key: '${section}'`)
		}
		expect(source).toContain('video-detail-${section.key}')
		for (const field of [
			'task.requested_model', 'task.public_model', 'task.channel_model', 'task.upstream_model',
			'task.billing_unit', 'task.unit_price', 'task.estimated_units', 'task.actual_units',
			'task.customer_multiplier', 'task.estimated_cost', 'task.hold_amount', 'task.actual_cost',
			'task.pricing_source', 'task.pricing_rule_key', 'task.video_tokens',
			'task.stable_client_token', 'task.provider_created_at', 'task.provider_finished_at',
		]) {
			expect(source).toContain(field)
		}
		expect(source).toContain('video-task-amounts')
	})

	it('shows safe spool and queue health without exposing filesystem paths', () => {
		expect(source).toContain('video-spool-health')
		expect(source).toContain('spool?.current_bytes')
		expect(source).toContain('spool?.orphan_candidates')
		expect(source).toContain('spool?.cleanup_failure_count')
		expect(source).toContain('overview.value?.queue_status')
		expect(source).not.toContain('spool.directory')
	})
})
