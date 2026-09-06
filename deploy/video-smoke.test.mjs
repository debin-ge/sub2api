import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { configuration, request, runSmoke } from './video-smoke.mjs'

const id = 'video_' + 'a'.repeat(32)
const env = { VIDEO_SMOKE_BASE_URL: 'http://127.0.0.1:8080', VIDEO_SMOKE_KEY: 'synthetic-owner', VIDEO_SMOKE_OTHER_KEY: 'synthetic-other',
  VIDEO_SMOKE_MODEL: 'test-model', VIDEO_SMOKE_SECONDS: '8', VIDEO_SMOKE_SIZE: '1280x720', VIDEO_SMOKE_EXPECTED_COST_USD: '1', VIDEO_SMOKE_MAX_COST_USD: '2' }

test('configuration rejects unsafe or unbudgeted execution', () => {
  for (const update of [{ VIDEO_SMOKE_BASE_URL: '' }, { VIDEO_SMOKE_BASE_URL: 'http://example.com' }, { VIDEO_SMOKE_BASE_URL: 'https://user:password@example.com' },
    { VIDEO_SMOKE_EXPECTED_COST_USD: '3' }, { VIDEO_SMOKE_MAX_COST_USD: 'NaN' }, { VIDEO_SMOKE_OTHER_KEY: env.VIDEO_SMOKE_KEY }]) {
    assert.throws(() => configuration({ ...env, ...update }))
  }
})

test('protocol run uses one key, bounded POSTs and redacted output', async () => {
  const calls = []
  let posts = 0
  const result = await runSmoke(configuration(env), async (_config, method, path, options = {}) => {
    calls.push({ method, path, options })
    if (method === 'POST') return ++posts <= 2 ? { status: 200, data: { id } } : { status: 409 }
    if (options.other) return { status: 404 }
    if (method === 'DELETE') return { status: 200, data: { deleted: true } }
    if (options.content) return { status: 200, bytes: 4, sha256: 'test-hash', type: 'video/mp4' }
    return { status: 200, data: { status: 'completed' } }
  })
  assert.equal(result.status, 'protocol_pass_accounting_pending')
  assert.equal(posts, 3)
  assert.equal(new Set(calls.filter(c => c.method === 'POST').map(c => c.options.idempotencyKey)).size, 1)
  assert.equal(result.accounting, 'pending_read_only_reconciliation')
  assert.ok(!JSON.stringify(result).includes(env.VIDEO_SMOKE_KEY))
})

test('uncertain initial submission stops without a new create or cleanup', async () => {
  let calls = 0
  const result = await runSmoke(configuration(env), async () => { calls++; throw new Error('secret response') })
  assert.equal(calls, 1)
  assert.equal(result.status, 'failed')
  assert.ok(!JSON.stringify(result).includes('secret response'))
})

test('polling is bounded and failure does not blindly delete unsettled work', async () => {
  const config = { ...configuration(env), maxPolls: 2, pollMs: 0 }
  let posts = 0, deletes = 0, gets = 0
  const result = await runSmoke(config, async (_config, method, _path, options = {}) => {
    if (method === 'POST') return ++posts <= 2 ? { status: 200, data: { id } } : { status: 409 }
    if (method === 'DELETE') deletes++
    if (options.other) return { status: 404 }
    gets++
    return { status: 200, data: { status: 'in_progress' } }
  }, async () => {})
  assert.equal(result.status, 'failed')
  assert.equal(gets, 2)
  assert.equal(deletes, 0)
})

test('HTTP client does not follow redirects carrying credentials', async () => {
  let calls = 0
  const server = createServer((_request, response) => {
    calls++
    response.writeHead(302, { Location: '/v1/videos/redirected' })
    response.end()
  })
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  try {
    const config = configuration({ ...env, VIDEO_SMOKE_BASE_URL: `http://127.0.0.1:${server.address().port}` })
    await assert.rejects(request(config, 'GET', '/v1/videos'), /transport_failed/)
    assert.equal(calls, 1)
  } finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)) }
})

test('HTTP client bounds downloads without persisting media', async () => {
  const server = createServer((_request, response) => { response.writeHead(200, { 'Content-Type': 'video/mp4' }); response.end('12345') })
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  try {
    const config = { ...configuration({ ...env, VIDEO_SMOKE_BASE_URL: `http://127.0.0.1:${server.address().port}` }), maxBytes: 4 }
    await assert.rejects(request(config, 'GET', '/v1/videos/test/content', { content: true }), /response_failed_or_too_large/)
  } finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)) }
})
