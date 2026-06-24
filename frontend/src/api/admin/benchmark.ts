import { apiClient } from '../client'
import type {
  BenchmarkPaginatedResponse,
  BenchmarkProfile,
  BenchmarkProfilePreview,
  BenchmarkProfilePreviewRequest,
  BenchmarkResult,
  BenchmarkRun,
  BenchmarkRunListParams,
  BenchmarkSchedule,
  BenchmarkScheduleListParams,
  BenchmarkScoreSnapshot,
  BenchmarkSuite,
  BenchmarkTarget,
  BenchmarkTask,
  BenchmarkTaskListParams,
  CreateBenchmarkProfileRequest,
  CreateBenchmarkRunRequest,
  CreateBenchmarkScheduleRequest,
  CreateBenchmarkSuiteRequest,
  CreateBenchmarkTargetRequest,
  CreateBenchmarkTaskRequest,
  BenchmarkListParams,
  PublishBenchmarkRunResponse,
} from '@/types/benchmark'

function withEnabledDefault<T extends { enabled?: boolean }>(payload: T): T & { enabled: boolean } {
  return {
    ...payload,
    enabled: payload.enabled ?? true,
  }
}

function commaSeparated(value: string[] | string | undefined): string | undefined {
  if (Array.isArray(value)) {
    return value.join(',')
  }
  return value
}

function normalizeTaskListParams(
  params?: BenchmarkTaskListParams
): BenchmarkTaskListParams | undefined {
  if (!params) {
    return undefined
  }
  return {
    ...params,
    task_types: commaSeparated(params.task_types),
  }
}

function normalizeRunListParams(params?: BenchmarkRunListParams): BenchmarkRunListParams | undefined {
  if (!params) {
    return undefined
  }
  return {
    ...params,
    status: commaSeparated(params.status),
  }
}

export async function listSuites(
  params?: BenchmarkListParams
): Promise<BenchmarkPaginatedResponse<BenchmarkSuite>> {
  const { data } = await apiClient.get<BenchmarkPaginatedResponse<BenchmarkSuite>>(
    '/admin/benchmark/suites',
    { params }
  )
  return data
}

export async function createSuite(payload: CreateBenchmarkSuiteRequest): Promise<BenchmarkSuite> {
  const { data } = await apiClient.post<BenchmarkSuite>(
    '/admin/benchmark/suites',
    withEnabledDefault(payload)
  )
  return data
}

export async function listTargets(
  params?: BenchmarkListParams
): Promise<BenchmarkPaginatedResponse<BenchmarkTarget>> {
  const { data } = await apiClient.get<BenchmarkPaginatedResponse<BenchmarkTarget>>(
    '/admin/benchmark/targets',
    { params }
  )
  return data
}

export async function createTarget(payload: CreateBenchmarkTargetRequest): Promise<BenchmarkTarget> {
  const { data } = await apiClient.post<BenchmarkTarget>(
    '/admin/benchmark/targets',
    withEnabledDefault(payload)
  )
  return data
}

export async function listTasks(
  params?: BenchmarkTaskListParams
): Promise<BenchmarkPaginatedResponse<BenchmarkTask>> {
  const { data } = await apiClient.get<BenchmarkPaginatedResponse<BenchmarkTask>>(
    '/admin/benchmark/tasks',
    { params: normalizeTaskListParams(params) }
  )
  return data
}

export async function createTask(payload: CreateBenchmarkTaskRequest): Promise<BenchmarkTask> {
  const { data } = await apiClient.post<BenchmarkTask>(
    '/admin/benchmark/tasks',
    withEnabledDefault(payload)
  )
  return data
}

export async function listProfiles(
  params?: BenchmarkListParams
): Promise<BenchmarkPaginatedResponse<BenchmarkProfile>> {
  const { data } = await apiClient.get<BenchmarkPaginatedResponse<BenchmarkProfile>>(
    '/admin/benchmark/profiles',
    { params }
  )
  return data
}

export async function createProfile(
  payload: CreateBenchmarkProfileRequest
): Promise<BenchmarkProfile> {
  const { data } = await apiClient.post<BenchmarkProfile>(
    '/admin/benchmark/profiles',
    withEnabledDefault(payload)
  )
  return data
}

export async function getProfile(id: number): Promise<BenchmarkProfile> {
  const { data } = await apiClient.get<BenchmarkProfile>(`/admin/benchmark/profiles/${id}`)
  return data
}

export async function previewProfile(
  id: number,
  payload: BenchmarkProfilePreviewRequest
): Promise<BenchmarkProfilePreview> {
  const { data } = await apiClient.post<BenchmarkProfilePreview>(
    `/admin/benchmark/profiles/${id}/preview`,
    payload
  )
  return data
}

export async function listRuns(
  params?: BenchmarkRunListParams
): Promise<BenchmarkPaginatedResponse<BenchmarkRun>> {
  const { data } = await apiClient.get<BenchmarkPaginatedResponse<BenchmarkRun>>(
    '/admin/benchmark/runs',
    { params: normalizeRunListParams(params) }
  )
  return data
}

export async function createRun(payload: CreateBenchmarkRunRequest): Promise<BenchmarkRun> {
  const { data } = await apiClient.post<BenchmarkRun>('/admin/benchmark/runs', payload)
  return data
}

export async function getRun(id: number): Promise<BenchmarkRun> {
  const { data } = await apiClient.get<BenchmarkRun>(`/admin/benchmark/runs/${id}`)
  return data
}

export async function listRunResults(id: number): Promise<BenchmarkResult[]> {
  const { data } = await apiClient.get<BenchmarkResult[]>(`/admin/benchmark/runs/${id}/results`)
  return data
}

export async function getRunScores(id: number): Promise<BenchmarkScoreSnapshot[]> {
  const { data } = await apiClient.get<BenchmarkScoreSnapshot[]>(
    `/admin/benchmark/runs/${id}/scores`
  )
  return data
}

export async function publishRun(id: number): Promise<PublishBenchmarkRunResponse> {
  const { data } = await apiClient.post<PublishBenchmarkRunResponse>(
    `/admin/benchmark/runs/${id}/publish`
  )
  return data
}

export async function listSchedules(
  params?: BenchmarkScheduleListParams
): Promise<BenchmarkPaginatedResponse<BenchmarkSchedule>> {
  const { data } = await apiClient.get<BenchmarkPaginatedResponse<BenchmarkSchedule>>(
    '/admin/benchmark/schedules',
    { params }
  )
  return data
}

export async function createSchedule(
  payload: CreateBenchmarkScheduleRequest
): Promise<BenchmarkSchedule> {
  const { data } = await apiClient.post<BenchmarkSchedule>('/admin/benchmark/schedules', payload)
  return data
}

export async function triggerSchedule(id: number): Promise<BenchmarkRun> {
  const { data } = await apiClient.post<BenchmarkRun>(`/admin/benchmark/schedules/${id}/trigger`)
  return data
}

export const adminBenchmarkAPI = {
  listSuites,
  createSuite,
  listTargets,
  createTarget,
  listTasks,
  createTask,
  listProfiles,
  createProfile,
  getProfile,
  previewProfile,
  listRuns,
  createRun,
  getRun,
  listRunResults,
  getRunScores,
  publishRun,
  listSchedules,
  createSchedule,
  triggerSchedule,
}

export default adminBenchmarkAPI
