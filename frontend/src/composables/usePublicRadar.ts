import {
  computed,
  getCurrentScope,
  onScopeDispose,
  ref,
  shallowRef,
  type ComputedRef,
  type Ref,
  type ShallowRef,
} from 'vue'
import * as defaultPublicRadarAPI from '@/api/publicRadar'
import type { PublicRadarRequestOptions } from '@/api/publicRadar'
import type {
  DataSourceMetaDTO,
  DegradationLatestDTO,
  LMArenaDTO,
  QuotaRadarLatestDTO,
  QuotaTrendDTO,
  ServiceHealthDTO,
} from '@/types/radar'

/** Stable, non-sensitive codes for localization by the presentation layer. */
export type PublicRadarErrorCode = 'load_failed'
export type PublicRadarRequestErrorCode = PublicRadarErrorCode | 'disposed'

export class PublicRadarRequestError extends Error {
  readonly code: PublicRadarRequestErrorCode

  constructor(code: PublicRadarRequestErrorCode) {
    super(code)
    this.name = 'PublicRadarRequestError'
    this.code = code
  }
}

export interface PublicRadarAPI {
  getServiceHealth(options?: PublicRadarRequestOptions): Promise<ServiceHealthDTO[]>
  getQuotaBucketsLatest(options?: PublicRadarRequestOptions): Promise<QuotaRadarLatestDTO>
  getQuotaBucketsTrend(
    bucketKey: string,
    days?: number,
    options?: PublicRadarRequestOptions
  ): Promise<QuotaTrendDTO>
  getDegradationLatest(options?: PublicRadarRequestOptions): Promise<DegradationLatestDTO>
  getLMArena(options?: PublicRadarRequestOptions): Promise<LMArenaDTO>
  getDataSources(options?: PublicRadarRequestOptions): Promise<DataSourceMetaDTO[]>
}

export interface RadarResourceState<T> {
  readonly data: ShallowRef<T | null>
  readonly loading: Ref<boolean>
  readonly error: Ref<PublicRadarErrorCode | null>
  /** True after the resource has succeeded at least once, including with an empty payload. */
  readonly hasSucceeded: Ref<boolean>
}

export interface PublicRadarTrendLoadOptions {
  /** Bypass a successful cache entry. An already-active request is still deduplicated. */
  readonly force?: boolean
}

export interface UsePublicRadarOptions {
  readonly api?: PublicRadarAPI
  readonly clock?: () => Date
}

interface InternalTrendState<T> extends RadarResourceState<T> {
  controller: AbortController | null
  inFlight: Promise<T> | null
}

export interface UsePublicRadarReturn {
  readonly health: RadarResourceState<ServiceHealthDTO[]>
  readonly quotaLatest: RadarResourceState<QuotaRadarLatestDTO>
  readonly degradationLatest: RadarResourceState<DegradationLatestDTO>
  readonly lmarena: RadarResourceState<LMArenaDTO>
  readonly sources: RadarResourceState<DataSourceMetaDTO[]>
  readonly hasAnySuccess: ComputedRef<boolean>
  readonly allInitialFailed: ComputedRef<boolean>
  readonly isRefreshing: ComputedRef<boolean>
  readonly hasCompletedRefresh: Readonly<Ref<boolean>>
  /** Client time of the latest core refresh in which at least one resource succeeded. */
  readonly lastFetchedAt: Readonly<ShallowRef<Date | null>>
  refresh(): Promise<void>
  getQuotaTrendState(bucketKey: string, days?: number): RadarResourceState<QuotaTrendDTO>
  loadQuotaTrend(
    bucketKey: string,
    days?: number,
    options?: PublicRadarTrendLoadOptions
  ): Promise<QuotaTrendDTO>
  dispose(): void
}

function createResourceState<T>(): RadarResourceState<T> {
  return {
    data: shallowRef<T | null>(null),
    loading: ref(false),
    error: ref<PublicRadarErrorCode | null>(null),
    hasSucceeded: ref(false),
  }
}

function createTrendState<T>(): InternalTrendState<T> {
  return {
    ...createResourceState<T>(),
    controller: null,
    inFlight: null,
  }
}

function invokeRequest<T>(request: () => Promise<T>): Promise<T> {
  try {
    return Promise.resolve(request())
  } catch (error) {
    return Promise.reject(error)
  }
}

function quotaTrendKey(bucketKey: string, days: number): string {
  return JSON.stringify([bucketKey, days])
}

export function usePublicRadar(options: UsePublicRadarOptions = {}): UsePublicRadarReturn {
  const api = options.api ?? defaultPublicRadarAPI
  const clock = options.clock ?? (() => new Date())

  const health = createResourceState<ServiceHealthDTO[]>()
  const quotaLatest = createResourceState<QuotaRadarLatestDTO>()
  const degradationLatest = createResourceState<DegradationLatestDTO>()
  const lmarena = createResourceState<LMArenaDTO>()
  const sources = createResourceState<DataSourceMetaDTO[]>()
  const coreStates = [health, quotaLatest, degradationLatest, lmarena, sources] as const

  const hasCompletedRefresh = ref(false)
  const lastFetchedAt = shallowRef<Date | null>(null)
  const hasAnySuccess = computed(() => coreStates.some((state) => state.hasSucceeded.value))
  const isRefreshing = computed(() => coreStates.some((state) => state.loading.value))
  const allInitialFailed = computed(
    () => hasCompletedRefresh.value && !isRefreshing.value && !hasAnySuccess.value
  )

  const quotaTrends = new Map<string, InternalTrendState<QuotaTrendDTO>>()

  let disposed = false
  let refreshGeneration = 0
  let refreshController: AbortController | null = null
  let refreshQueued = false
  let refreshLoopPromise: Promise<void> | null = null

  function isCurrentRefresh(generation: number, controller: AbortController): boolean {
    return !disposed && generation === refreshGeneration && refreshController === controller
  }

  function runCoreRequest<T>(
    state: RadarResourceState<T>,
    generation: number,
    controller: AbortController,
    request: () => Promise<T>
  ): Promise<boolean> {
    return invokeRequest(request)
      .then((value) => {
        if (!isCurrentRefresh(generation, controller)) return false
        state.data.value = value
        state.error.value = null
        state.hasSucceeded.value = true
        return true
      })
      .catch(() => {
        if (isCurrentRefresh(generation, controller)) {
          state.error.value = 'load_failed'
        }
        throw new PublicRadarRequestError('load_failed')
      })
      .finally(() => {
        if (isCurrentRefresh(generation, controller)) {
          state.loading.value = false
        }
      })
  }

  function readValidClientTime(): Date | null {
    try {
      const timestamp = clock()
      return timestamp instanceof Date && Number.isFinite(timestamp.getTime()) ? timestamp : null
    } catch {
      return null
    }
  }

  async function runRefreshBatch(): Promise<void> {
    const controller = new AbortController()
    refreshController = controller
    const generation = ++refreshGeneration

    for (const state of coreStates) {
      state.loading.value = true
      state.error.value = null
    }

    const requestOptions = { signal: controller.signal }
    const requests = [
      runCoreRequest(health, generation, controller, () => api.getServiceHealth(requestOptions)),
      runCoreRequest(quotaLatest, generation, controller, () =>
        api.getQuotaBucketsLatest(requestOptions)
      ),
      runCoreRequest(degradationLatest, generation, controller, () =>
        api.getDegradationLatest(requestOptions)
      ),
      runCoreRequest(lmarena, generation, controller, () => api.getLMArena(requestOptions)),
      runCoreRequest(sources, generation, controller, () => api.getDataSources(requestOptions)),
    ]

    try {
      const results = await Promise.allSettled(requests)
      if (!isCurrentRefresh(generation, controller)) return

      const hadSuccess = results.some(
        (result) => result.status === 'fulfilled' && result.value === true
      )
      if (hadSuccess) {
        const timestamp = readValidClientTime()
        if (timestamp) lastFetchedAt.value = timestamp
      }
      hasCompletedRefresh.value = true
    } finally {
      if (refreshController === controller) {
        refreshController = null
        if (!disposed && generation === refreshGeneration) {
          for (const state of coreStates) state.loading.value = false
        }
      }
    }
  }

  async function drainRefreshQueue(loopPromise: Promise<void>): Promise<void> {
    try {
      while (!disposed && refreshQueued) {
        refreshQueued = false
        try {
          await runRefreshBatch()
        } catch {
          // A batch normally contains all failures via allSettled. This guard
          // keeps the loop recoverable without dropping a queued trailing run.
          if (!disposed) {
            for (const state of coreStates) {
              if (state.loading.value) state.error.value = 'load_failed'
              state.loading.value = false
            }
            hasCompletedRefresh.value = true
          }
        }
      }
    } finally {
      if (refreshLoopPromise === loopPromise) refreshLoopPromise = null
    }
  }

  function refresh(): Promise<void> {
    if (disposed) return Promise.resolve()

    refreshQueued = true
    if (refreshLoopPromise) {
      if (refreshController && !refreshController.signal.aborted) {
        refreshGeneration += 1
        refreshController.abort()
      }
      return refreshLoopPromise
    }

    let resolveLoop!: () => void
    const loopPromise = new Promise<void>((resolve) => {
      resolveLoop = resolve
    })
    refreshLoopPromise = loopPromise
    void drainRefreshQueue(loopPromise).then(resolveLoop, resolveLoop)
    return loopPromise
  }

  function ensureTrendState<T>(
    cache: Map<string, InternalTrendState<T>>,
    key: string
  ): InternalTrendState<T> {
    let state = cache.get(key)
    if (!state) {
      state = createTrendState<T>()
      cache.set(key, state)
    }
    return state
  }

  function loadTrend<T>(
    state: InternalTrendState<T>,
    request: (options: PublicRadarRequestOptions) => Promise<T>,
    options?: PublicRadarTrendLoadOptions
  ): Promise<T> {
    if (disposed) return Promise.reject(new PublicRadarRequestError('disposed'))
    if (state.inFlight) return state.inFlight
    if (!options?.force && state.hasSucceeded.value && state.data.value !== null) {
      return Promise.resolve(state.data.value)
    }

    const controller = new AbortController()
    state.controller = controller
    state.loading.value = true
    state.error.value = null

    const requestPromise = invokeRequest(() => request({ signal: controller.signal }))
    const inFlight = requestPromise
      .then((value) => {
        if (disposed || state.controller !== controller) {
          throw new PublicRadarRequestError('disposed')
        }
        state.data.value = value
        state.error.value = null
        state.hasSucceeded.value = true
        return value
      })
      .catch(() => {
        if (disposed || state.controller !== controller) {
          throw new PublicRadarRequestError('disposed')
        }
        state.error.value = 'load_failed'
        throw new PublicRadarRequestError('load_failed')
      })
      .finally(() => {
        if (state.controller === controller) {
          state.loading.value = false
          state.controller = null
          state.inFlight = null
        }
      })

    state.inFlight = inFlight
    return inFlight
  }

  function getQuotaTrendState(bucketKey: string, days = 7): RadarResourceState<QuotaTrendDTO> {
    return ensureTrendState(quotaTrends, quotaTrendKey(bucketKey, days))
  }

  function loadQuotaTrend(
    bucketKey: string,
    days = 7,
    options?: PublicRadarTrendLoadOptions
  ): Promise<QuotaTrendDTO> {
    const state = ensureTrendState(quotaTrends, quotaTrendKey(bucketKey, days))
    return loadTrend(
      state,
      (requestOptions) => api.getQuotaBucketsTrend(bucketKey, days, requestOptions),
      options
    )
  }

  function dispose(): void {
    if (disposed) return
    disposed = true
    refreshQueued = false
    refreshGeneration += 1
    refreshController?.abort()
    refreshController = null

    for (const state of coreStates) {
      state.loading.value = false
    }
    for (const state of quotaTrends.values()) {
      state.controller?.abort()
      state.controller = null
      state.inFlight = null
      state.loading.value = false
    }
    quotaTrends.clear()
  }

  if (getCurrentScope()) {
    onScopeDispose(dispose)
  }

  return {
    health,
    quotaLatest,
    degradationLatest,
    lmarena,
    sources,
    hasAnySuccess,
    allInitialFailed,
    isRefreshing,
    hasCompletedRefresh,
    lastFetchedAt,
    refresh,
    getQuotaTrendState,
    loadQuotaTrend,
    dispose,
  }
}
