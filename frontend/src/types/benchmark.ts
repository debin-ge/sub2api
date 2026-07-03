import type { BasePaginationResponse } from './index'

export type BenchmarkRunStatus = 'queued' | 'running' | 'completed' | 'failed' | 'canceled'
export type BenchmarkResultStatus =
  | 'pending'
  | 'running'
  | 'scored'
  | 'failed'
  | 'timeout'
  | 'channel_error'
  | 'parse_error'
  | 'rate_limited'
  | 'verifier_error'
  | 'skipped'

export type BenchmarkPaginatedResponse<T> = BasePaginationResponse<T>

export interface BenchmarkListParams {
  page?: number
  page_size?: number
}

// ---- Targets ----

export interface BenchmarkTarget {
  id: number
  created_at?: string
  updated_at?: string
  model_name: string
  channel_id: number
  display_name?: string | null
  channel_name_snapshot?: string | null
  enabled: boolean
  public_visible: boolean
  sort_order: number
}

export interface CreateBenchmarkTargetRequest {
  model_name: string
  channel_id: number
  display_name?: string
  channel_name_snapshot?: string
  enabled?: boolean
  public_visible?: boolean
  sort_order?: number
}

export interface UpdateBenchmarkTargetRequest {
  model_name: string
  channel_id: number
  display_name?: string
  channel_name_snapshot?: string
  enabled: boolean
  public_visible: boolean
  sort_order: number
}

// ---- Tasks ----

export interface BenchmarkTask {
  id: number
  created_at?: string
  updated_at?: string
  title: string
  type: string
  difficulty?: string | null
  prompt: string
  input_payload?: Record<string, unknown>
  expected_output?: Record<string, unknown>
  verifier_type: string
  verifier_config?: Record<string, unknown>
  weight: number
  public_prompt: boolean
  enabled: boolean
  sort_order: number
}

export interface BenchmarkTaskListParams extends BenchmarkListParams {
  task_types?: string[] | string
  enabled?: boolean
}

export interface CreateBenchmarkTaskRequest {
  title: string
  type: string
  difficulty?: string
  prompt: string
  input_payload?: Record<string, unknown>
  expected_output?: Record<string, unknown>
  verifier_type: string
  verifier_config?: Record<string, unknown>
  weight?: number
  public_prompt?: boolean
  enabled?: boolean
  sort_order?: number
}

export interface UpdateBenchmarkTaskRequest {
  title: string
  type: string
  difficulty?: string
  prompt: string
  input_payload?: Record<string, unknown>
  expected_output?: Record<string, unknown>
  verifier_type: string
  verifier_config?: Record<string, unknown>
  weight: number
  public_prompt: boolean
  enabled: boolean
  sort_order: number
}

export interface BenchmarkStandardTaskApplyResponse {
  created_count: number
  existing_count: number
  enabled_count: number
  tasks: BenchmarkTask[]
}

// ---- Schedules ----

export interface BenchmarkSchedule {
  id: number
  created_at?: string
  updated_at?: string
  name: string
  cron_expr: string
  enabled: boolean
  target_ids: number[]
  task_count: number
  last_run_at?: string | null
  next_run_at?: string | null
}

export interface BenchmarkScheduleListParams extends BenchmarkListParams {
  enabled?: boolean
}

export interface CreateBenchmarkScheduleRequest {
  name: string
  cron_expr: string
  enabled?: boolean
  target_ids?: number[]
  task_count?: number
}

export interface UpdateBenchmarkScheduleRequest {
  name: string
  cron_expr: string
  enabled: boolean
  target_ids: number[]
  task_count: number
}

// ---- Runs ----

export interface BenchmarkRun {
  id: number
  created_at?: string
  updated_at?: string
  status: BenchmarkRunStatus
  trigger_type: string
  schedule_id?: number | null
  task_count: number
  planned_target_count: number
  planned_task_count: number
  planned_result_count: number
  started_at?: string | null
  finished_at?: string | null
  error_message?: string | null
  created_by?: number | null
}

export interface BenchmarkRunListParams extends BenchmarkListParams {
  status?: string[] | string
}

export interface CreateBenchmarkRunRequest {
  target_ids?: number[]
  task_count?: number
  trigger_type?: string
  created_by?: number | null
  process_immediately?: boolean
}

export interface CreateBenchmarkStandardRunRequest {
  target_ids?: number[]
  task_count?: number
  process_immediately?: boolean
  created_by?: number | null
}

export interface BenchmarkRunPreview {
  target_count: number
  task_count: number
  result_count: number
  ranking_basis: 'ability_score_only'
  target_ids: number[]
  task_ids: number[]
}

export interface BenchmarkRunTargetSnapshot {
  id: number
  run_id?: number
  target_id: number
  model_name: string
  channel_id: number
  display_name_snapshot?: string | null
  channel_name_snapshot?: string | null
  target_order?: number
  created_at?: string
}

// ---- Results ----

export interface BenchmarkResult {
  id: number
  created_at?: string
  updated_at?: string
  run_id: number
  run_task_id: number
  run_target_id: number
  request_id?: string | null
  status: BenchmarkResultStatus
  normalized_score?: number | null
  evaluator_type?: string | null
  evaluator_output?: Record<string, unknown>
  latency_ms?: number | null
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  estimated_cost: number
  raw_response?: Record<string, unknown>
  error_code?: string | null
  error_message?: string | null
  attempt_count: number
  started_at?: string | null
  finished_at?: string | null
  edges?: {
    run_target?: BenchmarkRunTargetSnapshot
  }
}

// ---- Target Scores (trend data points) ----

export interface BenchmarkTargetScore {
  id: number
  run_id: number
  run_target_id: number
  model_name: string
  channel_id: number
  overall_score: number
  passed_count: number
  total_count: number
  dimension_scores?: Record<string, number>
  avg_latency_ms?: number | null
  avg_total_tokens?: number | null
  total_cost: number
  invalid_reason_breakdown?: Record<string, number>
  finished_at: string
  created_at?: string
}

// ---- Public Radar ----

export interface BenchmarkPublicRadarLatestRun {
  id: number
  status: string
  task_count: number
  completed_at?: string | null
}

export interface BenchmarkRadarMetrics {
  avg_latency_ms?: number | null
  avg_total_tokens?: number | null
  total_cost: number
}

export interface BenchmarkRadarTarget {
  rank: number
  model: string
  channel_id: number
  channel_name?: string
  display_name?: string
  overall_score: number
  passed_count: number
  total_count: number
  dimensions?: Record<string, number>
  metrics: BenchmarkRadarMetrics
}

export interface BenchmarkTrendPoint {
  run_id: number
  finished_at: string
  overall_score: number
  passed_count: number
  total_count: number
  avg_latency_ms?: number | null
  total_cost: number
}

export interface BenchmarkPublicTrend {
  model: string
  channel_id: number
  channel_name?: string
  display_name?: string
  points: BenchmarkTrendPoint[]
}

export interface BenchmarkPublicRadar {
  ranking_basis: 'ability_score_only'
  published_at?: string | null
  latest_run: BenchmarkPublicRadarLatestRun | null
  targets: BenchmarkRadarTarget[]
  trends: BenchmarkPublicTrend[]
}

// ---- Action responses ----

export interface BenchmarkActionResponse {
  message: string
}

export interface BenchmarkProcessResponse {
  processed: number
}
