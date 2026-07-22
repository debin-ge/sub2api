import { effectScope, nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import {
  PublicRadarRequestError,
  usePublicRadar,
  type PublicRadarAPI,
} from '@/composables/usePublicRadar'
import type {
  DataSourceMetaDTO,
  DegradationLatestDTO,
  LMArenaDTO,
  QuotaRadarLatestDTO,
  QuotaTrendDTO,
  ServiceHealthDTO,
} from '@/types/radar'

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

const health = [{ service_key: 'claude_api' }] as ServiceHealthDTO[]
const quota = { buckets: [] } as unknown as QuotaRadarLatestDTO
const degradation = { models: [] } as unknown as DegradationLatestDTO
const lmarena = { leaderboard: [] } as unknown as LMArenaDTO
const sources = [] as DataSourceMetaDTO[]

function createAPI(overrides: Partial<PublicRadarAPI> = {}): PublicRadarAPI {
  return {
    getServiceHealth: vi.fn().mockResolvedValue(health),
    getQuotaBucketsLatest: vi.fn().mockResolvedValue(quota),
    getQuotaBucketsTrend: vi.fn().mockResolvedValue({ data_points: [] }),
    getDegradationLatest: vi.fn().mockResolvedValue(degradation),
    getLMArena: vi.fn().mockResolvedValue(lmarena),
    getDataSources: vi.fn().mockResolvedValue(sources),
    ...overrides,
  }
}

describe('usePublicRadar core resources', () => {
  it('starts all five core requests together and treats empty payloads as successes', async () => {
    const pending = {
      health: deferred<ServiceHealthDTO[]>(),
      quota: deferred<QuotaRadarLatestDTO>(),
      degradation: deferred<DegradationLatestDTO>(),
      lmarena: deferred<LMArenaDTO>(),
      sources: deferred<DataSourceMetaDTO[]>(),
    }
    const api = createAPI({
      getServiceHealth: vi.fn(() => pending.health.promise),
      getQuotaBucketsLatest: vi.fn(() => pending.quota.promise),
      getDegradationLatest: vi.fn(() => pending.degradation.promise),
      getLMArena: vi.fn(() => pending.lmarena.promise),
      getDataSources: vi.fn(() => pending.sources.promise),
    })
    const radar = usePublicRadar({ api })

    const refresh = radar.refresh()

    expect(api.getServiceHealth).toHaveBeenCalledTimes(1)
    expect(api.getQuotaBucketsLatest).toHaveBeenCalledTimes(1)
    expect(api.getDegradationLatest).toHaveBeenCalledTimes(1)
    expect(api.getLMArena).toHaveBeenCalledTimes(1)
    expect(api.getDataSources).toHaveBeenCalledTimes(1)
    expect(radar.isRefreshing.value).toBe(true)
    expect(radar.health.loading.value).toBe(true)
    expect(radar.quotaLatest.loading.value).toBe(true)
    expect(radar.allInitialFailed.value).toBe(false)

    pending.health.resolve([])
    await vi.waitFor(() => expect(radar.health.loading.value).toBe(false))
    expect(radar.health.hasSucceeded.value).toBe(true)
    expect(radar.quotaLatest.loading.value).toBe(true)
    expect(radar.isRefreshing.value).toBe(true)

    pending.quota.resolve(quota)
    pending.degradation.resolve(degradation)
    pending.lmarena.resolve(lmarena)
    pending.sources.resolve([])
    await refresh

    expect(radar.health.data.value).toEqual([])
    expect(radar.sources.data.value).toEqual([])
    expect(radar.health.hasSucceeded.value).toBe(true)
    expect(radar.sources.hasSucceeded.value).toBe(true)
    expect(radar.hasAnySuccess.value).toBe(true)
    expect(radar.allInitialFailed.value).toBe(false)
    expect(radar.isRefreshing.value).toBe(false)
  })

  it('keeps successful modules on partial failure and exposes only a safe error', async () => {
    const secret = 'postgres://admin:secret@internal-db'
    const api = createAPI({
      getQuotaBucketsLatest: vi.fn().mockRejectedValue(new Error(secret)),
    })
    const now = new Date('2026-07-13T01:02:03Z')
    const radar = usePublicRadar({ api, clock: () => now })

    await radar.refresh()

    expect(radar.health.data.value).toBe(health)
    expect(radar.health.hasSucceeded.value).toBe(true)
    expect(radar.quotaLatest.data.value).toBeNull()
    expect(radar.quotaLatest.hasSucceeded.value).toBe(false)
    expect(radar.quotaLatest.error.value).toBeTruthy()
    expect(radar.quotaLatest.error.value).toBe('load_failed')
    expect(radar.quotaLatest.error.value).not.toContain(secret)
    expect(radar.hasAnySuccess.value).toBe(true)
    expect(radar.allInitialFailed.value).toBe(false)
    expect(radar.lastFetchedAt.value).toBe(now)
  })

  it('identifies an all-failed initial round and does not update lastFetchedAt', async () => {
    const failure = new Error('private upstream details')
    const api = createAPI({
      getServiceHealth: vi.fn().mockRejectedValue(failure),
      getQuotaBucketsLatest: vi.fn().mockRejectedValue(failure),
      getDegradationLatest: vi.fn().mockRejectedValue(failure),
      getLMArena: vi.fn().mockRejectedValue(failure),
      getDataSources: vi.fn().mockRejectedValue(failure),
    })
    const radar = usePublicRadar({ api })

    await radar.refresh()

    expect(radar.hasAnySuccess.value).toBe(false)
    expect(radar.allInitialFailed.value).toBe(true)
    expect(radar.lastFetchedAt.value).toBeNull()
    expect(radar.health.error.value).not.toContain('private upstream details')
  })

  it('retains old data while refreshing and after the replacement request fails', async () => {
    const api = createAPI()
    const first = new Date('2026-07-13T01:00:00Z')
    const second = new Date('2026-07-13T02:00:00Z')
    const clock = vi.fn().mockReturnValueOnce(first).mockReturnValue(second)
    const radar = usePublicRadar({ api, clock })
    await radar.refresh()

    const pending = deferred<ServiceHealthDTO[]>()
    vi.mocked(api.getServiceHealth).mockImplementationOnce(() => pending.promise)
    vi.mocked(api.getQuotaBucketsLatest).mockRejectedValueOnce(new Error('down'))
    vi.mocked(api.getDegradationLatest).mockRejectedValueOnce(new Error('down'))
    vi.mocked(api.getLMArena).mockRejectedValueOnce(new Error('down'))
    vi.mocked(api.getDataSources).mockRejectedValueOnce(new Error('down'))

    const refresh = radar.refresh()
    expect(radar.health.data.value).toBe(health)
    expect(radar.health.loading.value).toBe(true)
    expect(radar.quotaLatest.data.value).toBe(quota)

    pending.reject(new Error('down'))
    await refresh

    expect(radar.health.data.value).toBe(health)
    expect(radar.quotaLatest.data.value).toBe(quota)
    expect(radar.lastFetchedAt.value).toBe(first)
  })

  it('coalesces 100 re-entrant refreshes behind one active batch and one trailing batch', async () => {
    const old = {
      health: deferred<ServiceHealthDTO[]>(),
      quota: deferred<QuotaRadarLatestDTO>(),
      degradation: deferred<DegradationLatestDTO>(),
      lmarena: deferred<LMArenaDTO>(),
      sources: deferred<DataSourceMetaDTO[]>(),
    }
    const trailing = {
      health: deferred<ServiceHealthDTO[]>(),
      quota: deferred<QuotaRadarLatestDTO>(),
      degradation: deferred<DegradationLatestDTO>(),
      lmarena: deferred<LMArenaDTO>(),
      sources: deferred<DataSourceMetaDTO[]>(),
    }
    const fresh = [{ service_key: 'openai_api' }] as ServiceHealthDTO[]
    const api = createAPI({
      getServiceHealth: vi
        .fn()
        .mockImplementationOnce(() => old.health.promise)
        .mockImplementationOnce(() => trailing.health.promise),
      getQuotaBucketsLatest: vi
        .fn()
        .mockImplementationOnce(() => old.quota.promise)
        .mockImplementationOnce(() => trailing.quota.promise),
      getDegradationLatest: vi
        .fn()
        .mockImplementationOnce(() => old.degradation.promise)
        .mockImplementationOnce(() => trailing.degradation.promise),
      getLMArena: vi
        .fn()
        .mockImplementationOnce(() => old.lmarena.promise)
        .mockImplementationOnce(() => trailing.lmarena.promise),
      getDataSources: vi
        .fn()
        .mockImplementationOnce(() => old.sources.promise)
        .mockImplementationOnce(() => trailing.sources.promise),
    })
    const radar = usePublicRadar({ api })

    const refreshLoop = radar.refresh()
    const firstSignal = vi.mocked(api.getServiceHealth).mock.calls[0][0]?.signal
    const reentrantCalls = Array.from({ length: 100 }, () => radar.refresh())

    expect(firstSignal?.aborted).toBe(true)
    for (const call of reentrantCalls) expect(call).toBe(refreshLoop)
    expect(api.getServiceHealth).toHaveBeenCalledTimes(1)
    expect(api.getQuotaBucketsLatest).toHaveBeenCalledTimes(1)
    expect(api.getDegradationLatest).toHaveBeenCalledTimes(1)
    expect(api.getLMArena).toHaveBeenCalledTimes(1)
    expect(api.getDataSources).toHaveBeenCalledTimes(1)

    old.health.resolve(health)
    old.quota.resolve(quota)
    old.degradation.resolve(degradation)
    old.lmarena.resolve(lmarena)
    old.sources.resolve(sources)
    await vi.waitFor(() => expect(api.getServiceHealth).toHaveBeenCalledTimes(2))
    expect(api.getQuotaBucketsLatest).toHaveBeenCalledTimes(2)
    expect(api.getDegradationLatest).toHaveBeenCalledTimes(2)
    expect(api.getLMArena).toHaveBeenCalledTimes(2)
    expect(api.getDataSources).toHaveBeenCalledTimes(2)
    expect(radar.health.data.value).toBeNull()

    trailing.health.resolve(fresh)
    trailing.quota.resolve(quota)
    trailing.degradation.resolve(degradation)
    trailing.lmarena.resolve(lmarena)
    trailing.sources.resolve(sources)
    await refreshLoop
    expect(radar.health.data.value).toBe(fresh)
    expect(api.getServiceHealth).toHaveBeenCalledTimes(2)
  })

  it('settles safely for throwing or invalid clocks, preserves timestamps, and remains reusable', async () => {
    const valid = new Date('2026-07-13T01:00:00Z')
    const later = new Date('2026-07-13T02:00:00Z')
    const clock = vi
      .fn<() => Date>()
      .mockImplementationOnce(() => {
        throw new Error('clock failed')
      })
      .mockReturnValueOnce(new Date('invalid'))
      .mockReturnValueOnce(valid)
      .mockImplementationOnce(() => {
        throw new Error('clock failed again')
      })
      .mockReturnValueOnce(later)
    const api = createAPI()
    const radar = usePublicRadar({ api, clock })

    await expect(radar.refresh()).resolves.toBeUndefined()
    expect(radar.hasCompletedRefresh.value).toBe(true)
    expect(radar.lastFetchedAt.value).toBeNull()
    expect(radar.isRefreshing.value).toBe(false)

    await expect(radar.refresh()).resolves.toBeUndefined()
    expect(radar.lastFetchedAt.value).toBeNull()
    await expect(radar.refresh()).resolves.toBeUndefined()
    expect(radar.lastFetchedAt.value).toBe(valid)
    await expect(radar.refresh()).resolves.toBeUndefined()
    expect(radar.lastFetchedAt.value).toBe(valid)
    await expect(radar.refresh()).resolves.toBeUndefined()
    expect(radar.lastFetchedAt.value).toBe(later)
    expect(api.getServiceHealth).toHaveBeenCalledTimes(5)
  })

  it('feeds synchronous API failures to allSettled while preserving partial progress', async () => {
    const nativeAllSettled = Promise.allSettled.bind(Promise)
    let observedResults: PromiseSettledResult<unknown>[] = []
    const allSettledSpy = vi.spyOn(Promise, 'allSettled').mockImplementation(async (values) => {
      const results = await nativeAllSettled(values)
      observedResults = results
      return results
    })
    const api = createAPI({
      getQuotaBucketsLatest: vi.fn(() => {
        throw new Error('secret synchronous failure')
      }),
    })
    const radar = usePublicRadar({ api })

    await expect(radar.refresh()).resolves.toBeUndefined()

    expect(observedResults.filter((result) => result.status === 'rejected')).toHaveLength(1)
    expect(radar.quotaLatest.error.value).toBe('load_failed')
    expect(radar.quotaLatest.hasSucceeded.value).toBe(false)
    expect(radar.health.data.value).toBe(health)
    expect(radar.health.hasSucceeded.value).toBe(true)
    expect(radar.hasAnySuccess.value).toBe(true)
    expect(radar.allInitialFailed.value).toBe(false)
    allSettledSpy.mockRestore()
  })

  it('keeps state isolated between composable instances', async () => {
    const firstHealth = [{ service_key: 'claude_code' }] as ServiceHealthDTO[]
    const first = usePublicRadar({
      api: createAPI({ getServiceHealth: vi.fn().mockResolvedValue(firstHealth) }),
    })
    const second = usePublicRadar({ api: createAPI() })

    await first.refresh()

    expect(first.health.data.value).toBe(firstHealth)
    expect(second.health.data.value).toBeNull()
    expect(second.hasAnySuccess.value).toBe(false)
    expect(second.lastFetchedAt.value).toBeNull()
  })
})

describe('usePublicRadar trend cache', () => {
  it('deduplicates same quota bucket+days promise, caches success, and separates parameters', async () => {
    const pending = deferred<QuotaTrendDTO>()
    const value = { bucket_key: 'pro', days: 7, data_points: [] } as unknown as QuotaTrendDTO
    const other = { bucket_key: 'pro', days: 30, data_points: [] } as unknown as QuotaTrendDTO
    const getQuotaBucketsTrend = vi
      .fn()
      .mockImplementationOnce(() => pending.promise)
      .mockResolvedValueOnce(other)
    const api = createAPI({ getQuotaBucketsTrend })
    const radar = usePublicRadar({ api })

    const first = radar.loadQuotaTrend('pro', 7)
    const duplicate = radar.loadQuotaTrend('pro', 7)

    expect(duplicate).toBe(first)
    expect(getQuotaBucketsTrend).toHaveBeenCalledTimes(1)
    expect(getQuotaBucketsTrend).toHaveBeenCalledWith(
      'pro',
      7,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(radar.getQuotaTrendState('pro', 7).loading.value).toBe(true)

    pending.resolve(value)
    await first
    await radar.loadQuotaTrend('pro', 7)
    expect(getQuotaBucketsTrend).toHaveBeenCalledTimes(1)
    expect(radar.getQuotaTrendState('pro', 7).data.value).toBe(value)

    await radar.loadQuotaTrend('pro', 30)
    expect(getQuotaBucketsTrend).toHaveBeenCalledTimes(2)
    expect(radar.getQuotaTrendState('pro', 30).data.value).toBe(other)
    expect(radar.getQuotaTrendState('pro', 7)).not.toBe(radar.getQuotaTrendState('pro', 30))
  })

  it('does not cache quota failures, keeps a safe per-key error, and permits retry', async () => {
    const value = { bucket_key: 'team', days: 7 } as QuotaTrendDTO
    const getQuotaBucketsTrend = vi
      .fn()
      .mockRejectedValueOnce(new Error('token=super-secret'))
      .mockResolvedValueOnce(value)
    const radar = usePublicRadar({ api: createAPI({ getQuotaBucketsTrend }) })

    await expect(radar.loadQuotaTrend('team', 7)).rejects.not.toThrow('super-secret')
    const state = radar.getQuotaTrendState('team', 7)
    expect(state.error.value).toBeTruthy()
    expect(state.error.value).toBe('load_failed')
    expect(state.error.value).not.toContain('super-secret')
    expect(state.hasSucceeded.value).toBe(false)

    await expect(radar.loadQuotaTrend('team', 7)).resolves.toBe(value)
    expect(getQuotaBucketsTrend).toHaveBeenCalledTimes(2)
    expect(state.data.value).toBe(value)
    expect(state.error.value).toBeNull()
  })

  it('documents force as a cache bypass while still deduplicating an active request', async () => {
    const firstValue = { bucket_key: 'pro', days: 7, stale: true } as QuotaTrendDTO
    const secondValue = { bucket_key: 'pro', days: 7, stale: false } as QuotaTrendDTO
    const forcePending = deferred<QuotaTrendDTO>()
    const getQuotaBucketsTrend = vi
      .fn()
      .mockResolvedValueOnce(firstValue)
      .mockImplementationOnce(() => forcePending.promise)
    const radar = usePublicRadar({ api: createAPI({ getQuotaBucketsTrend }) })
    await radar.loadQuotaTrend('pro', 7)

    const forced = radar.loadQuotaTrend('pro', 7, { force: true })
    const forcedDuplicate = radar.loadQuotaTrend('pro', 7, { force: true })
    expect(forcedDuplicate).toBe(forced)
    expect(getQuotaBucketsTrend).toHaveBeenCalledTimes(2)

    forcePending.resolve(secondValue)
    await expect(forced).resolves.toBe(secondValue)
    expect(radar.getQuotaTrendState('pro', 7).data.value).toBe(secondValue)
  })
})

describe('usePublicRadar lifecycle', () => {
  it('aborts core and trend requests and never writes asynchronous results after dispose', async () => {
    const core = deferred<ServiceHealthDTO[]>()
    const quotaTrend = deferred<QuotaTrendDTO>()
    const getServiceHealth = vi.fn(() => core.promise)
    const getQuotaBucketsTrend = vi.fn(() => quotaTrend.promise)
    const radar = usePublicRadar({
      api: createAPI({ getServiceHealth, getQuotaBucketsTrend }),
    })

    const refresh = radar.refresh()
    const quotaTrendLoad = radar.loadQuotaTrend('pro', 7)
    const quotaState = radar.getQuotaTrendState('pro', 7)
    const coreSignal = vi.mocked(getServiceHealth).mock.calls[0][0]?.signal
    const quotaTrendSignal = vi.mocked(getQuotaBucketsTrend).mock.calls[0][2]?.signal
    radar.dispose()

    expect(coreSignal?.aborted).toBe(true)
    expect(quotaTrendSignal?.aborted).toBe(true)

    core.resolve(health)
    quotaTrend.resolve({ bucket_key: 'pro' } as QuotaTrendDTO)
    const [refreshResult, quotaResult] = await Promise.allSettled([
      refresh,
      quotaTrendLoad,
    ])
    expect(refreshResult.status).toBe('fulfilled')
    expect(quotaResult).toEqual(
      expect.objectContaining({
        status: 'rejected',
        reason: expect.objectContaining({ code: 'disposed' }),
      })
    )
    expect(radar.health.data.value).toBeNull()
    expect(radar.health.hasSucceeded.value).toBe(false)
    expect(quotaState.data.value).toBeNull()
    expect(radar.getQuotaTrendState('pro', 7)).not.toBe(quotaState)
    expect(radar.lastFetchedAt.value).toBeNull()

    await expect(radar.loadQuotaTrend('pro', 7)).rejects.toEqual(
      expect.objectContaining<Partial<PublicRadarRequestError>>({
        name: 'PublicRadarRequestError',
        code: 'disposed',
        message: 'disposed',
      })
    )
    expect(getQuotaBucketsTrend).toHaveBeenCalledTimes(1)
  })

  it('automatically disposes when its active Vue effect scope stops', async () => {
    const pending = deferred<ServiceHealthDTO[]>()
    const getServiceHealth = vi.fn(() => pending.promise)
    const api = createAPI({ getServiceHealth })
    const scope = effectScope()
    let radar!: ReturnType<typeof usePublicRadar>
    let refresh!: Promise<void>

    scope.run(() => {
      radar = usePublicRadar({ api })
      refresh = radar.refresh()
    })
    const signal = vi.mocked(getServiceHealth).mock.calls[0][0]?.signal
    scope.stop()
    await nextTick()

    expect(signal?.aborted).toBe(true)
    pending.resolve(health)
    await refresh
    expect(radar.health.data.value).toBeNull()
  })
})
