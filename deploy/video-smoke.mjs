import { randomUUID, createHash } from 'node:crypto'
import { pathToFileURL } from 'node:url'

export function configuration(env) {
  if (!env.VIDEO_SMOKE_BASE_URL) throw new Error('explicit_test_origin_required')
  const base = new URL(env.VIDEO_SMOKE_BASE_URL)
  if (base.username || base.password || base.search || base.hash || base.pathname !== '/') throw new Error('base_url_must_be_an_origin')
  if (base.protocol !== 'https:' && !(base.protocol === 'http:' && ['127.0.0.1', '[::1]', 'localhost'].includes(base.hostname))) throw new Error('https_required_outside_loopback')
  const budget = Number(env.VIDEO_SMOKE_MAX_COST_USD)
  const expected = Number(env.VIDEO_SMOKE_EXPECTED_COST_USD)
  const seconds = Number(env.VIDEO_SMOKE_SECONDS)
  if (!Number.isFinite(budget) || budget <= 0 || !Number.isFinite(expected) || expected <= 0 || expected > budget) throw new Error('confirmed_price_and_budget_required')
  if (!env.VIDEO_SMOKE_KEY || !env.VIDEO_SMOKE_OTHER_KEY || env.VIDEO_SMOKE_KEY === env.VIDEO_SMOKE_OTHER_KEY) throw new Error('two_distinct_user_keys_required')
  if (!env.VIDEO_SMOKE_MODEL || !Number.isInteger(seconds) || seconds <= 0 || !/^\d+x\d+$/.test(env.VIDEO_SMOKE_SIZE || '')) throw new Error('confirmed_model_seconds_size_required')
  return { origin: base.origin, key: env.VIDEO_SMOKE_KEY, otherKey: env.VIDEO_SMOKE_OTHER_KEY,
    model: env.VIDEO_SMOKE_MODEL, seconds, size: env.VIDEO_SMOKE_SIZE, budget, expected,
    timeoutMs: 30_000, pollMs: 3000, maxPolls: 200, maxDurationMs: 600_000, maxBytes: 200 * 1024 * 1024 }
}

export async function request(config, method, path, { body, other = false, idempotencyKey, content = false } = {}) {
  if (!path.startsWith('/v1/videos')) throw new Error('unexpected_path')
  const headers = { Authorization: `Bearer ${other ? config.otherKey : config.key}` }
  if (body) headers['Content-Type'] = 'application/json'
  if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey
  let response
  try {
    response = await fetch(config.origin + path, { method, headers, body: body ? JSON.stringify(body) : undefined,
      redirect: 'error', signal: AbortSignal.timeout(config.timeoutMs) })
  } catch { throw new Error('transport_failed_no_automatic_create_retry') }
  const hash = createHash('sha256')
  const chunks = []
  let bytes = 0
  const limit = content ? config.maxBytes : 1024 * 1024
  try {
    for await (const chunk of response.body || []) {
      bytes += chunk.length
      if (bytes > limit) throw new Error('response_too_large')
      if (content) hash.update(chunk)
      else chunks.push(chunk)
    }
  } catch { throw new Error('response_failed_or_too_large') }
  if (content) return { status: response.status, bytes, sha256: hash.digest('hex'), type: response.headers.get('content-type') || '' }
  let data = null
  try { data = JSON.parse(Buffer.concat(chunks).toString('utf8')) } catch { /* Never log raw upstream bodies. */ }
  return { status: response.status, data }
}

export async function runSmoke(config, send = request, wait = ms => new Promise(resolve => setTimeout(resolve, ms))) {
  const report = { status: 'failed', checks: [], task_id: null, idempotency_key: randomUUID(), accounting: 'pending_read_only_reconciliation' }
  const deadline = Date.now() + config.maxDurationMs
  const check = (name, valid) => {
    report.checks.push({ name, passed: Boolean(valid) })
    if (!valid) throw new Error(`check_failed:${name}`)
  }
  const body = { model: config.model, seconds: config.seconds, size: config.size, prompt: 'A stationary camera viewing a blue cube on a white table.' }
  try {
    // Exactly one initial create; two further POSTs test the same key, never a new key.
    let result = await send(config, 'POST', '/v1/videos', { body, idempotencyKey: report.idempotency_key })
    check('create', result.status >= 200 && result.status < 300 && /^video_[a-f0-9]{32}$/.test(result.data?.id || ''))
    report.task_id = result.data.id
    const path = `/v1/videos/${report.task_id}`
    result = await send(config, 'POST', '/v1/videos', { body, idempotencyKey: report.idempotency_key })
    check('same_key_same_task', result.status === 200 && result.data?.id === report.task_id)
    result = await send(config, 'POST', '/v1/videos', { body: { ...body, prompt: 'Changed smoke request' }, idempotencyKey: report.idempotency_key })
    check('same_key_changed_body_conflicts', result.status === 409)
    for (const suffix of ['', '/content']) {
      result = await send(config, 'GET', path + suffix, { other: true })
      check(suffix ? 'other_user_content_denied' : 'other_user_task_denied', [403, 404].includes(result.status))
    }
    let completed = false
    for (let i = 0; i < config.maxPolls && Date.now() < deadline; i++) {
      result = await send(config, 'GET', path)
      if (result.status === 200 && result.data?.status === 'completed') { completed = true; break }
      if (![200, 503].includes(result.status) || ['failed', 'cancelled', 'deleted'].includes(result.data?.status)) break
      await wait(config.pollMs)
    }
    check('completed', completed)
    result = await send(config, 'GET', path + '/content', { content: true })
    check('content_download', result.status === 200 && result.bytes > 0 && result.type.startsWith('video/'))
    report.download = { bytes: result.bytes, sha256: result.sha256 }
    let deleted = false
    for (let i = 0; i < config.maxPolls && Date.now() < deadline; i++) {
      result = await send(config, 'DELETE', path)
      if (result.status === 200 && result.data?.deleted === true) { deleted = true; break }
      if (result.status !== 409) break
      await wait(config.pollMs)
    }
    check('deleted', deleted)
    result = await send(config, 'DELETE', path)
    check('repeat_delete', result.status === 200 && result.data?.deleted === true)
    report.status = 'protocol_pass_accounting_pending'
  } catch (error) {
    report.error = error.message.startsWith('check_failed:') ? error.message : 'request_failed_stop_and_reconcile'
  }
  return report
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  if (!process.argv.includes('--run')) {
    console.log('Dry run only. Set VIDEO_SMOKE_BASE_URL, KEY, OTHER_KEY, MODEL, SECONDS, SIZE, EXPECTED_COST_USD, MAX_COST_USD (all prefixed VIDEO_SMOKE_). Explicit execution also requires --run and VIDEO_SMOKE_AUTHORIZED=yes. Confirm a dedicated test-key quota before use. The declared budget is not a server-enforced spending cap. No network requests were made.')
  } else {
    try {
      if (process.env.VIDEO_SMOKE_AUTHORIZED !== 'yes') throw new Error('explicit_paid_test_authorization_required')
      const result = await runSmoke(configuration(process.env))
      console.log(JSON.stringify(result, null, 2))
      process.exitCode = result.status === 'failed' ? 1 : 0
    } catch { console.error('Invalid or unapproved smoke configuration; no create attempted.'); process.exitCode = 2 }
  }
}
