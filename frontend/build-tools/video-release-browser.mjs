import { createRequire } from 'node:module'
import { mkdir, writeFile } from 'node:fs/promises'
import assert from 'node:assert/strict'

const { chromium } = createRequire(import.meta.url)('playwright')
const origin = new URL(process.env.VIDEO_BROWSER_BASE_URL || 'http://127.0.0.1:5178').origin
if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(origin).hostname)) throw new Error('Only isolated loopback frontend servers are allowed')
const output = process.env.VIDEO_BROWSER_OUTPUT || '/tmp/video-release-browser'
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH || undefined })
const now = new Date().toISOString()
const user = { id: 99, username: 'Release QA', email: 'qa@example.invalid', role: 'admin', status: 'active', balance: 10, concurrency: 1 }
const task = { id: 1, public_id: 'video_' + 'a'.repeat(32), version: 1, lease_epoch: 1, source: 'managed', user_id: 42,
  account_id: 11, provider: 'openai', operation: 'generate', generation_state: 'queued', billing_state: 'held', delete_state: 'none',
  provider_task_id: 'upstream-fixture', requested_model: 'test-video-model', public_model: 'test-video-model', upstream_model: 'test-video-model',
  currency: 'USD', hold_amount: 1, estimated_units: 8, input_manifest: [], content_variants: [], request_attributes: {}, price_snapshot: {},
  provider_cost_snapshot: {}, usage_snapshot: {}, response_metadata: {}, provider_access: { configured: false }, callback_configured: false,
  poll_attempts: 1, submit_attempts: 1, created_at: now, updated_at: now }
const summary = []
try {
  for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
    const context = await browser.newContext({ viewport, locale: 'en-US' })
    const page = await context.newPage()
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    let empty = false, failure = false, mutations = 0, callbackMutations = 0, listPage = 1
    let pendingList = null
    await context.addInitScript(({ user }) => {
      localStorage.setItem('auth_token', 'synthetic-browser-token')
      localStorage.setItem('auth_user', JSON.stringify(user))
      localStorage.setItem('sub2api_locale', 'en')
      localStorage.setItem('admin_guide_99_admin_v4_interactive', 'true')
      window.__APP_CONFIG__ = { site_name: 'Release QA', registration_enabled: false, custom_menu_items: [] }
    }, { user })
    await context.route('**/*', async route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin) return route.abort()
      if (!url.pathname.startsWith('/api/') && !url.pathname.startsWith('/setup/')) return route.continue()
      const respond = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status === 200 ? 0 : status, message: status === 200 ? 'ok' : 'Fixture permission denied', data }) })
      const path = url.pathname.replace('/api/v1', '')
      if (path === '/auth/me') return respond(user)
      if (path === '/setup/status') return respond({ needs_setup: false })
      if (path === '/settings/public') return respond({ site_name: 'Release QA', custom_menu_items: [] })
      if (path === '/admin/videos/overview') return respond({ tasks_by_generation: { queued: 1 }, tasks_by_billing: { held: 1 }, callbacks_by_status: {}, spool: {}, queue: {} })
      if (path === '/admin/videos/tasks') {
        if (pendingList) await pendingList
        listPage = Number(url.searchParams.get('page') || 1)
        await new Promise(resolve => setTimeout(resolve, 150))
        if (failure) return respond(null, 403)
        return respond({ items: empty ? [] : [task, { ...task, id: 2, public_id: 'video_' + 'b'.repeat(32), generation_state: 'completed', billing_state: 'capture_pending', delete_state: 'requested' }], total: empty ? 0 : 40, page: listPage, page_size: 20 })
      }
      if (path.endsWith('/retry-get')) {
        mutations++
        await new Promise(resolve => setTimeout(resolve, 250))
        return respond(task)
      }
      if (path.endsWith('/billing-reviews') || path.endsWith('/submission-reviews')) return respond([])
      if (path.endsWith('/events')) return respond({ items: [], total: 0 })
      if (path.startsWith('/admin/videos/tasks/')) return respond(task)
      if (path === '/admin/videos/callbacks') return respond({ items: [{ id: 1, task_id: 1, status: 'quarantined', attempts: 1, event_type: 'fixture', next_attempt_at: now, expires_at: '2099-01-01T00:00:00Z' }], total: 1, page: 1, page_size: 20 })
      if (path === '/admin/videos/callbacks/1/retry') {
        callbackMutations++
        await new Promise(resolve => setTimeout(resolve, 250))
        return respond({ id: 1, task_id: 1, status: 'pending', attempts: 1, event_type: 'fixture', next_attempt_at: now, expires_at: '2099-01-01T00:00:00Z' })
      }
      if (path.includes('compliance')) return respond({ required: false, acknowledged: true })
      return respond({ items: [], total: 0, custom_menu_items: [] })
    })
    try {
      await page.goto(origin + '/admin/videos')
      await page.locator('[data-test="video-admin-page"]').waitFor()
      await page.getByTitle('Inspect task', { exact: true }).first().waitFor()
      await page.locator('tbody').getByText('Capture pending', { exact: true }).waitFor()
      let releaseList
      pendingList = new Promise(resolve => { releaseList = resolve })
      await page.getByTitle('Refresh', { exact: true }).click()
      assert.equal(await page.getByTitle('Refresh', { exact: true }).isDisabled(), true)
      await page.screenshot({ path: `${output}/${viewport.width}-loading.png`, fullPage: true })
      pendingList = null
      releaseList()
      await page.waitForFunction(() => !document.querySelector('button[title="Refresh"]')?.disabled)
      await page.screenshot({ path: `${output}/${viewport.width}-tasks.png`, fullPage: true })
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)
      assert.equal(overflow, false, 'page-level horizontal overflow')
      await page.getByTitle('Retry Provider Get', { exact: true }).first().evaluate(button => { button.click(); button.click() })
      await page.waitForTimeout(400)
      assert.equal(mutations, 1, 'duplicate mutation from repeated click')
      await page.getByTitle('Inspect task', { exact: true }).first().click()
      await page.getByRole('dialog').waitFor()
      const bounds = await page.locator('.modal-content').boundingBox()
      assert.ok(bounds && bounds.x >= 0 && bounds.x + bounds.width <= viewport.width + 1, 'dialog must fit the viewport')
      await page.screenshot({ path: `${output}/${viewport.width}-detail.png`, fullPage: true })
      await page.getByRole('button', { name: 'Close modal' }).click()
      await page.getByRole('dialog').waitFor({ state: 'hidden' })
      await page.getByRole('button', { name: 'Next', exact: true }).click()
      await page.waitForTimeout(250)
      assert.equal(listPage, 2)
      await page.getByRole('tab', { name: 'Callbacks', exact: false }).click()
      await page.getByText('fixture', { exact: true }).waitFor()
      await page.getByTitle('Retry callback', { exact: true }).evaluate(button => { button.click(); button.click() })
      await page.waitForTimeout(400)
      assert.equal(callbackMutations, 1, 'duplicate callback retry from repeated click')
      await page.getByRole('tab', { name: /^Tasks/ }).click()
      empty = true
      await page.getByTitle('Refresh', { exact: true }).click()
      await page.getByText('No matching records', { exact: true }).waitFor()
      await page.screenshot({ path: `${output}/${viewport.width}-empty.png`, fullPage: true })
      failure = true
      await page.getByTitle('Refresh', { exact: true }).click()
      await page.getByText('Unable to load video platform data.', { exact: true }).waitFor()
      await page.screenshot({ path: `${output}/${viewport.width}-error.png`, fullPage: true })
      assert.deepEqual(errors, [], 'browser runtime errors')
      summary.push({ viewport, status: 'pass', mutations, callbackMutations, api: 'fixtures_only', checks: ['layout', 'loading', 'details', 'pagination', 'pending_states', 'duplicate_click', 'callback_retry', 'empty', 'permission_error'] })
    } catch (error) {
      await page.screenshot({ path: `${output}/${viewport.width}-failure.png`, fullPage: true })
      throw error
    } finally { await context.close() }
  }
  await writeFile(`${output}/results.json`, JSON.stringify(summary, null, 2))
  console.log(JSON.stringify(summary))
} finally { await browser.close() }
