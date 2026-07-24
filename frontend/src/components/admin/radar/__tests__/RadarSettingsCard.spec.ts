import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const {
  getRadarAdminStatus,
  updateRadarAdminSettings,
  triggerRadarAdminRefresh,
} = vi.hoisted(() => ({
  getRadarAdminStatus: vi.fn(),
  updateRadarAdminSettings: vi.fn(),
  triggerRadarAdminRefresh: vi.fn(),
}))

vi.mock('@/api/admin/radar', () => ({
  getRadarAdminStatus,
  updateRadarAdminSettings,
  triggerRadarAdminRefresh,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'en-US' },
      t: (key: string, params?: Record<string, string>) =>
        params ? `${key}:${Object.values(params).join(',')}` : key,
    }),
  }
})

import RadarSettingsCard from '@/components/admin/radar/RadarSettingsCard.vue'
import type { RadarAdminStatus } from '@/api/admin/radar'

const baseStatus: RadarAdminStatus = {
  enabled: true,
  sources: [
    {
      key: 'lmarena',
      status: 'failed',
      stale: false,
      last_attempt_at: '2026-07-15T02:05:00Z',
      last_success_at: '2026-07-15T01:45:00Z',
      last_failure_at: '2026-07-15T02:05:00Z',
      next_fire_at: '2026-07-15T02:15:00Z',
      http_status: 429,
      error: 'rate_limited',
    },
    {
      key: 'aa',
      status: 'healthy',
      stale: true,
      last_attempt_at: '2026-07-15T02:00:00Z',
      last_success_at: '2026-07-15T02:00:00Z',
      last_failure_at: null,
      next_fire_at: '2026-07-15T04:00:00Z',
      http_status: 200,
      error: null,
    },
    {
      key: 'status_claude',
      status: 'healthy',
      stale: false,
      last_attempt_at: '2026-07-15T02:00:00Z',
      last_success_at: '2026-07-15T02:00:00Z',
      last_failure_at: null,
      next_fire_at: '2026-07-15T02:05:00Z',
      http_status: 200,
      error: null,
    },
    {
      key: 'status_openai',
      status: 'never_attempted',
      stale: false,
      last_attempt_at: null,
      last_success_at: null,
      last_failure_at: null,
      next_fire_at: '2026-07-15T02:30:00Z',
      http_status: null,
      error: null,
    },
  ],
  aggregator: {
    key: 'quota_aggregator',
    status: 'healthy',
    stale: true,
    last_attempt_at: '2026-07-15T02:00:00Z',
    last_success_at: '2026-07-15T02:00:00Z',
    last_failure_at: null,
    next_fire_at: '2026-07-15T02:15:00Z',
    http_status: null,
    error: null,
  },
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('RadarSettingsCard', () => {
  beforeEach(() => {
    getRadarAdminStatus.mockReset()
    updateRadarAdminSettings.mockReset()
    triggerRadarAdminRefresh.mockReset()
    getRadarAdminStatus.mockResolvedValue(structuredClone(baseStatus))
    updateRadarAdminSettings.mockImplementation(async (enabled: boolean) => ({ enabled }))
    triggerRadarAdminRefresh.mockResolvedValue({
      refresh_id: 'refresh-0123456789abcdef0123456789abcdef',
      status: 'triggered',
      tasks: ['lmarena', 'quota_aggregator'],
    })
  })

  it('renders source health, real schedule timestamps, safe errors, and stale state', async () => {
    const wrapper = mount(RadarSettingsCard)
    await flushPromises()

    const failed = wrapper.get('[data-testid="radar-source-lmarena"]')
    expect(failed.text()).toContain('admin.settings.features.radar.status.failed')
    expect(failed.text()).not.toContain('admin.settings.features.radar.status.stale')
    expect(failed.text()).toContain('admin.settings.features.radar.errors.rate_limited')
    expect(failed.text()).toContain('429')
    expect(failed.get('time[data-field="last-success"]').attributes('datetime')).toBe(
      '2026-07-15T01:45:00Z',
    )
    expect(failed.get('time[data-field="last-failure"]').attributes('datetime')).toBe(
      '2026-07-15T02:05:00Z',
    )
    expect(failed.get('time[data-field="next-fire"]').attributes('datetime')).toBe(
      '2026-07-15T02:15:00Z',
    )

    const authoritativeHealthyStale = wrapper.get('[data-testid="radar-source-aa"]')
    expect(authoritativeHealthyStale.text()).toContain(
      'admin.settings.features.radar.status.healthy',
    )
    expect(authoritativeHealthyStale.text()).toContain(
      'admin.settings.features.radar.status.stale',
    )

    const aggregator = wrapper.get('[data-testid="radar-source-quota_aggregator"]')
    expect(aggregator.text()).toContain('admin.settings.features.radar.status.healthy')
    expect(aggregator.text()).toContain('admin.settings.features.radar.status.stale')

    const neverAttempted = wrapper.get('[data-testid="radar-source-status_openai"]')
    expect(neverAttempted.text()).toContain('admin.settings.features.radar.status.never_attempted')
  })

  it('labels every real source key', async () => {
    const wrapper = mount(RadarSettingsCard)
    await flushPromises()

    expect(wrapper.get('[data-testid="radar-source-aa"]').text()).toContain(
      'admin.settings.features.radar.sources.aa',
    )
    expect(wrapper.get('[data-testid="radar-source-lmarena"]').text()).toContain(
      'admin.settings.features.radar.sources.lmarena',
    )
    expect(wrapper.get('[data-testid="radar-source-status_claude"]').text()).toContain(
      'admin.settings.features.radar.sources.status_claude',
    )
    expect(wrapper.get('[data-testid="radar-source-status_openai"]').text()).toContain(
      'admin.settings.features.radar.sources.status_openai',
    )
    expect(wrapper.get('[data-testid="radar-source-quota_aggregator"]').text()).toContain(
      'admin.settings.features.radar.sources.quota_aggregator',
    )

  })

  it('renders an unexpected source key as escaped text, never as markup', async () => {
    const payload = structuredClone(baseStatus)
    payload.sources = [
      {
        ...payload.sources[1],
        key: 'unexpected:<img src=x onerror=alert(1)>',
      },
    ]
    getRadarAdminStatus.mockResolvedValueOnce(payload)

    const wrapper = mount(RadarSettingsCard)
    await flushPromises()

    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('unexpected:<img src=x onerror=alert(1)>')
  })

  it('contains load errors locally and supports retry without affecting the parent form', async () => {
    getRadarAdminStatus
      .mockRejectedValueOnce(new Error('redis://secret@internal'))
      .mockResolvedValueOnce(structuredClone(baseStatus))

    const wrapper = mount(RadarSettingsCard)
    await flushPromises()

    const error = wrapper.get('[role="alert"]')
    expect(error.text()).toContain('admin.settings.features.radar.loadError')
    expect(error.text()).not.toContain('redis://secret@internal')

    await wrapper.get('[data-testid="radar-retry"]').trigger('click')
    await flushPromises()

    expect(getRadarAdminStatus).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="radar-source-lmarena"]').exists()).toBe(true)
  })

  it('optimistically toggles runtime scheduling, disables repeat input, and rolls back on error', async () => {
    const update = deferred<{ enabled: boolean }>()
    updateRadarAdminSettings.mockReturnValueOnce(update.promise)
    const wrapper = mount(RadarSettingsCard)
    await flushPromises()

    const toggle = wrapper.get('[data-testid="radar-enabled-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('true')

    await toggle.trigger('click')
    expect(toggle.attributes('aria-checked')).toBe('false')
    expect(toggle.attributes('disabled')).toBeDefined()

    update.reject(new Error('database password leaked'))
    await flushPromises()

    expect(toggle.attributes('aria-checked')).toBe('true')
    expect(toggle.attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="radar-settings-error"]').text()).toContain(
      'admin.settings.features.radar.updateError',
    )
    expect(wrapper.text()).not.toContain('database password leaked')
  })

  it('uses a non-submit refresh button, disables it while pending, and reports triggered/coalesced', async () => {
    const first = deferred<{
      refresh_id: string
      status: 'triggered'
      tasks: string[]
    }>()
    triggerRadarAdminRefresh.mockReturnValueOnce(first.promise)
    const wrapper = mount(RadarSettingsCard)
    await flushPromises()

    const refresh = wrapper.get('[data-testid="radar-refresh"]')
    expect(refresh.attributes('type')).toBe('button')

    await refresh.trigger('click')
    expect(refresh.attributes('disabled')).toBeDefined()

    first.resolve({ refresh_id: 'refresh-first', status: 'triggered', tasks: ['lmarena'] })
    await flushPromises()
    expect(wrapper.get('[data-testid="radar-refresh-result"]').text()).toContain(
      'admin.settings.features.radar.refresh.triggered',
    )

    triggerRadarAdminRefresh.mockResolvedValueOnce({
      refresh_id: 'refresh-second',
      status: 'coalesced',
      tasks: ['lmarena'],
    })
    await refresh.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="radar-refresh-result"]').text()).toContain(
      'admin.settings.features.radar.refresh.coalesced',
    )
  })

  it('aborts in-flight work and ignores late completion after unmount', async () => {
    const request = deferred<RadarAdminStatus>()
    getRadarAdminStatus.mockReturnValueOnce(request.promise)
    const wrapper = mount(RadarSettingsCard)

    const options = getRadarAdminStatus.mock.calls[0]?.[0] as { signal: AbortSignal }
    expect(options.signal.aborted).toBe(false)

    wrapper.unmount()
    expect(options.signal.aborted).toBe(true)

    request.resolve(structuredClone(baseStatus))
    await flushPromises()
    expect(updateRadarAdminSettings).not.toHaveBeenCalled()
    expect(triggerRadarAdminRefresh).not.toHaveBeenCalled()
  })
})
