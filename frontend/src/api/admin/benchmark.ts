import { apiClient } from '../client'
import type {
  BenchmarkActionResponse,
  BenchmarkListParams,
  BenchmarkPaginatedResponse,
  BenchmarkProcessResponse,
  BenchmarkResult,
  BenchmarkRun,
  BenchmarkRunListParams,
  BenchmarkRunPreview,
  BenchmarkSchedule,
  BenchmarkScheduleListParams,
  BenchmarkStandardTaskApplyResponse,
  BenchmarkTarget,
  BenchmarkTargetScore,
  BenchmarkTask,
  BenchmarkTaskListParams,
  CreateBenchmarkRunRequest,
  CreateBenchmarkScheduleRequest,
  CreateBenchmarkStandardRunRequest,
  CreateBenchmarkTargetRequest,
  CreateBenchmarkTaskRequest,
  UpdateBenchmarkScheduleRequest,
  UpdateBenchmarkTargetRequest,
  UpdateBenchmarkTaskRequest,
} from '@/types/benchmark'

function withEnabledDefault<T extends { enabled?: boolean }>(payload: T): T & { enabled: boolean } {
  return { ...payload, enabled: payload.enabled ?? true }
}

function commaSeparated(value: string[] | string | undefined): string | undefined {
  return Array.isArray(value) ? value.join(',') : value
}

function normalizeTaskListParams(
  params?: BenchmarkTaskListParams,
): BenchmarkTaskListParams | undefined {
  if (!params) return undefined
  return { ...params, task_types: commaSeparated(params.task_types) }
}

function normalizeRunListParams(params?: BenchmarkRunListParams): BenchmarkRunListParams | undefined {
  if (!params) return undefined
  return { ...params, status: commaSeparated(params.status) }
}

// ---- Targets ----

export async function listTargets(
  params?: BenchmarkListParams,
): Promise<BenchmarkPaginatedResponse<BenchmarkTarget>> {
  const { data } = await apiClient.get<BenchmarkPaginatedResponse<BenchmarkTarget>>(
    '/admin/benchmark/targets',
    { params },
  )
  return data
}

export async function getTarget(id: number): Promise<BenchmarkTarget> {
  const { data } = await apiClient.get<BenchmarkTarget>(`/admin/benchmark/targets/${id}`)
  return data
}

export async function createTarget(payload: CreateBenchmarkTargetRequest): Promise<BenchmarkTarget> {
  const { data } = await apiClient.post<BenchmarkTarget>(
    '/admin/benchmark/targets',
    withEnabledDefault(payload),
  )
  return data
}

export async function updateTarget(
  id: number,
  payload: UpdateBenchmarkTargetRequest,
): Promise<BenchmarkTarget> {
  const { data } = await apiClient.put<BenchmarkTarget>(`/admin/benchmark/targets/${id}`, payload)
  return data
}

export async function deleteTarget(id: number): Promise<BenchmarkActionResponse> {
  const { data } = await apiClient.delete<BenchmarkActionResponse>(`/admin/benchmark/targets/${id}`)
  return data
}

// ---- Tasks ----

export async function listTasks(
  params?: BenchmarkTaskListParams,
): Promise<BenchmarkPaginatedResponse<BenchmarkTask>> {
  const { data } = await apiClient.get<BenchmarkPaginatedResponse<BenchmarkTask>>(
    '/admin/benchmark/tasks',
    { params: normalizeTaskListParams(params) },
  )
  return data
}

export async function getTask(id: number): Promise<BenchmarkTask> {
  const { data } = await apiClient.get<BenchmarkTask>(`/admin/benchmark/tasks/${id}`)
  return data
}

export async function createTask(payload: CreateBenchmarkTaskRequest): Promise<BenchmarkTask> {
  const { data } = await apiClient.post<BenchmarkTask>(
    '/admin/benchmark/tasks',
    withEnabledDefault(payload),
  )
  return data
}

export async function updateTask(
  id: number,
  payload: UpdateBenchmarkTaskRequest,
): Promise<BenchmarkTask> {
  const { data } = await apiClient.put<BenchmarkTask>(`/admin/benchmark/tasks/${id}`, payload)
  return data
}

export async function deleteTask(id: number): Promise<BenchmarkActionResponse> {
  const { data } = await apiClient.delete<BenchmarkActionResponse>(`/admin/benchmark/tasks/${id}`)
  return data
}

export async function applyStandardTasks(): Promise<BenchmarkStandardTaskApplyResponse> {
  const { data } = await apiClient.post<BenchmarkStandardTaskApplyResponse>(
    '/admin/benchmark/tasks/standard/apply',
  )
  return data
}

// ---- Schedules ----

export async function listSchedules(
  params?: BenchmarkScheduleListParams,
): Promise<BenchmarkPaginatedResponse<BenchmarkSchedule>> {
  const { data } = await apiClient.get<BenchmarkPaginatedResponse<BenchmarkSchedule>>(
    '/admin/benchmark/schedules',
    { params },
  )
  return data
}

export async function getSchedule(id: number): Promise<BenchmarkSchedule> {
  const { data } = await apiClient.get<BenchmarkSchedule>(`/admin/benchmark/schedules/${id}`)
  return data
}

export async function createSchedule(
  payload: CreateBenchmarkScheduleRequest,
): Promise<BenchmarkSchedule> {
  const { data } = await apiClient.post<BenchmarkSchedule>(
    '/admin/benchmark/schedules',
    payload,
  )
  return data
}

export async function updateSchedule(
  id: number,
  payload: UpdateBenchmarkScheduleRequest,
): Promise<BenchmarkSchedule> {
  const { data } = await apiClient.put<BenchmarkSchedule>(
    `/admin/benchmark/schedules/${id}`,
    payload,
  )
  return data
}

export async function deleteSchedule(id: number): Promise<BenchmarkActionResponse> {
  const { data } = await apiClient.delete<BenchmarkActionResponse>(
    `/admin/benchmark/schedules/${id}`,
  )
  return data
}

export async function triggerSchedule(id: number): Promise<BenchmarkRun> {
  const { data } = await apiClient.post<BenchmarkRun>(`/admin/benchmark/schedules/${id}/trigger`)
  return data
}

// ---- Runs ----

export async function previewRun(payload: {
  target_ids?: number[]
  task_count?: number
}): Promise<BenchmarkRunPreview> {
  const { data } = await apiClient.post<BenchmarkRunPreview>(
    '/admin/benchmark/runs/preview',
    payload,
  )
  return data
}

export async function listRuns(
  params?: BenchmarkRunListParams,
): Promise<BenchmarkPaginatedResponse<BenchmarkRun>> {
  const { data } = await apiClient.get<BenchmarkPaginatedResponse<BenchmarkRun>>(
    '/admin/benchmark/runs',
    { params: normalizeRunListParams(params) },
  )
  return data
}

export async function createRun(payload: CreateBenchmarkRunRequest): Promise<BenchmarkRun> {
  const { data } = await apiClient.post<BenchmarkRun>('/admin/benchmark/runs', payload)
  return data
}

export async function createStandardRun(
  payload?: CreateBenchmarkStandardRunRequest,
): Promise<BenchmarkRun> {
  const { data } = await apiClient.post<BenchmarkRun>(
    '/admin/benchmark/runs/standard',
    payload,
  )
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

export async function getRunScores(id: number): Promise<BenchmarkTargetScore[]> {
  const { data } = await apiClient.get<BenchmarkTargetScore[]>(`/admin/benchmark/runs/${id}/scores`)
  return data
}

export async function publishRun(id: number): Promise<BenchmarkActionResponse> {
  const { data } = await apiClient.post<BenchmarkActionResponse>(
    `/admin/benchmark/runs/${id}/publish`,
  )
  return data
}

export async function cancelRun(id: number): Promise<BenchmarkActionResponse> {
  const { data } = await apiClient.post<BenchmarkActionResponse>(
    `/admin/benchmark/runs/${id}/cancel`,
  )
  return data
}

export async function processRun(id: number): Promise<BenchmarkProcessResponse> {
  const { data } = await apiClient.post<BenchmarkProcessResponse>(
    `/admin/benchmark/runs/${id}/process`,
  )
  return data
}

export async function processDueRuns(): Promise<BenchmarkProcessResponse> {
  const { data } = await apiClient.post<BenchmarkProcessResponse>(
    '/admin/benchmark/runs/process-due',
  )
  return data
}

// ---- Trends ----

export async function getTrends(params?: {
  days?: number
  limit?: number
}): Promise<{ trends: import('@/types/benchmark').BenchmarkPublicTrend[] }> {
  const { data } = await apiClient.get('/admin/benchmark/trends', { params })
  return data
}

export const adminBenchmarkAPI = {
  listTargets,
  getTarget,
  createTarget,
  updateTarget,
  deleteTarget,
  listTasks,
  getTask,
  createTask,
  updateTask,
  deleteTask,
  applyStandardTasks,
  listSchedules,
  getSchedule,
  createSchedule,
  updateSchedule,
  deleteSchedule,
  triggerSchedule,
  previewRun,
  listRuns,
  createRun,
  createStandardRun,
  getRun,
  listRunResults,
  getRunScores,
  publishRun,
  cancelRun,
  processRun,
  processDueRuns,
  getTrends,
}

export default adminBenchmarkAPI
