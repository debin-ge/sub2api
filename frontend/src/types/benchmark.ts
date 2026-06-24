import type { BasePaginationResponse } from './index'

export type BenchmarkTaskScale = 'small' | 'medium' | 'full' | 'custom'
export type BenchmarkConfidenceLevel = 'high' | 'medium' | 'low'
export type BenchmarkRunStatus =
  | 'pending'
  | 'selecting'
  | 'queued'
  | 'running'
  | 'scoring'
  | 'snapshotting'
  | 'completed'
  | 'failed'
  | 'canceled'
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

export interface BenchmarkSuite {
  id: number
  created_at?: string
  updated_at?: string
  name: string
  slug: string
  description?: string | null
  enabled: boolean
  public_visible: boolean
  default_profile_id?: number | null
  metadata?: Record<string, unknown>
}

export interface CreateBenchmarkSuiteRequest {
  name: string
  slug: string
  description?: string
  enabled?: boolean
  public_visible?: boolean
  default_profile_id?: number | null
  metadata?: Record<string, unknown>
}

export interface BenchmarkTarget {
  id: number
  created_at?: string
  updated_at?: string
  model_name: string
  channel_id: number
  display_name?: string | null
  provider_snapshot?: string | null
  channel_name_snapshot?: string | null
  supported_task_types?: string[]
  max_concurrency?: number
  per_run_budget?: number | null
  daily_budget?: number | null
  enabled: boolean
  public_visible: boolean
  sort_order: number
  metadata?: Record<string, unknown>
}

export interface CreateBenchmarkTargetRequest {
  model_name: string
  channel_id: number
  display_name?: string
  provider_snapshot?: string
  channel_name_snapshot?: string
  supported_task_types?: string[]
  max_concurrency?: number
  per_run_budget?: number | null
  daily_budget?: number | null
  enabled?: boolean
  public_visible?: boolean
  sort_order?: number
  metadata?: Record<string, unknown>
}

export interface BenchmarkTask {
  id: number
  created_at?: string
  updated_at?: string
  suite_id: number
  title: string
  type: string
  category?: string | null
  difficulty?: string | null
  tags?: string[]
  prompt: string
  input_payload?: Record<string, unknown>
  expected_output?: Record<string, unknown>
  verifier_type: string
  verifier_config?: Record<string, unknown>
  weight: number
  min_scale: BenchmarkTaskScale
  public_prompt: boolean
  enabled: boolean
  metadata?: Record<string, unknown>
}

export interface BenchmarkTaskListParams extends BenchmarkListParams {
  suite_id?: number
  task_types?: string[] | string
  enabled?: boolean
}

export interface CreateBenchmarkTaskRequest {
  suite_id: number
  title: string
  type: string
  category?: string
  difficulty?: string
  tags?: string[]
  prompt: string
  input_payload?: Record<string, unknown>
  expected_output?: Record<string, unknown>
  verifier_type: string
  verifier_config?: Record<string, unknown>
  weight?: number
  min_scale?: BenchmarkTaskScale
  public_prompt?: boolean
  enabled?: boolean
  metadata?: Record<string, unknown>
}

export interface BenchmarkProfile {
  id: number
  created_at?: string
  updated_at?: string
  suite_id: number
  name: string
  description?: string | null
  target_ids: number[]
  task_types: string[]
  task_scale: BenchmarkTaskScale
  task_count_limit?: number | null
  per_type_limit?: Record<string, number>
  difficulty_filter?: string[]
  tag_filter?: string[]
  sampling_strategy?: string
  selection_seed?: number | null
  runtime_config?: Record<string, unknown>
  scoring_config?: Record<string, unknown>
  enabled: boolean
  metadata?: Record<string, unknown>
}

export interface CreateBenchmarkProfileRequest {
  suite_id: number
  name: string
  description?: string
  target_ids: number[]
  task_types: string[]
  task_scale?: BenchmarkTaskScale
  task_count_limit?: number | null
  per_type_limit?: Record<string, number>
  difficulty_filter?: string[]
  tag_filter?: string[]
  sampling_strategy?: string
  selection_seed?: number | null
  runtime_config?: Record<string, unknown>
  scoring_config?: Record<string, unknown>
  metadata?: Record<string, unknown>
  enabled?: boolean
}

export interface BenchmarkProfilePreviewRequest {
  target_ids?: number[]
  task_types?: string[]
  task_scale?: BenchmarkTaskScale
  task_count_limit?: number | null
  per_type_limit?: Record<string, number>
  difficulty_filter?: string[]
  tag_filter?: string[]
  selection_seed?: number | null
}

export interface BenchmarkProfilePreview {
  target_count: number
  task_count: number
  result_count: number
  task_types: string[]
  task_scale: BenchmarkTaskScale
  ranking_basis: 'ability_score_only'
  estimated_cost: number
  selected_task_ids: number[]
  selected_target_ids: number[]
}

export interface BenchmarkRun {
  id: number
  created_at?: string
  updated_at?: string
  suite_id: number
  profile_id: number
  status: BenchmarkRunStatus
  trigger_type: string
  task_scale: BenchmarkTaskScale
  task_types: string[]
  selection_seed?: number | null
  planned_target_count: number
  planned_task_count: number
  planned_result_count: number
  started_at?: string | null
  finished_at?: string | null
  config_snapshot?: Record<string, unknown>
  error_message?: string | null
  created_by?: number | null
}

export interface BenchmarkRunListParams extends BenchmarkListParams {
  suite_id?: number
  profile_id?: number
  status?: string[] | string
}

export interface CreateBenchmarkRunRequest {
  profile_id: number
  trigger_type?: string
  created_by?: number | null
  override?: BenchmarkProfilePreviewRequest
}

export interface BenchmarkResult {
  id: number
  created_at?: string
  updated_at?: string
  run_id: number
  run_task_id: number
  run_target_id: number
  request_id?: string | null
  status: BenchmarkResultStatus
  score?: number | null
  max_score?: number | null
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
}

export interface BenchmarkScoreSnapshot {
  id: number
  run_id: number
  run_target_id: number
  overall_score: number
  dimension_scores?: Record<string, number>
  planned_tasks: number
  scored_tasks: number
  invalid_tasks: number
  coverage_rate: number
  confidence_level: BenchmarkConfidenceLevel
  insufficient_sample: boolean
  success_rate: number
  latency_p50_ms?: number | null
  latency_p95_ms?: number | null
  avg_total_tokens?: number | null
  estimated_cost: number
  invalid_reason_breakdown?: Record<string, number>
  ranking_metadata?: Record<string, unknown>
  created_at?: string
}

export interface BenchmarkSchedule {
  id: number
  created_at?: string
  updated_at?: string
  profile_id: number
  name: string
  cron_expr: string
  enabled: boolean
  last_run_at?: string | null
  next_run_at?: string | null
  metadata?: Record<string, unknown>
}

export interface BenchmarkScheduleListParams extends BenchmarkListParams {
  profile_id?: number
  enabled?: boolean
}

export interface CreateBenchmarkScheduleRequest {
  profile_id: number
  name: string
  cron_expr: string
  enabled?: boolean
  metadata?: Record<string, unknown>
}

export interface PublishBenchmarkRunResponse {
  message: string
}

export interface BenchmarkPublicRadarLatestRun {
  id: number
  suite_id: number
  profile_id: number
  status: string
  completed_at?: string | null
}

export interface BenchmarkRadarScoreBasis {
  planned_tasks: number
  scored_tasks: number
  invalid_tasks: number
  coverage_rate: number
  confidence_level: BenchmarkConfidenceLevel
  insufficient_sample: boolean
}

export interface BenchmarkRadarMetrics {
  success_rate: number
  latency_p50_ms?: number | null
  latency_p95_ms?: number | null
  avg_total_tokens?: number | null
  estimated_cost: number
}

export interface BenchmarkRadarTarget {
  rank: number
  model: string
  channel_id: number
  channel_name?: string
  display_name?: string
  overall_score: number
  dimensions?: Record<string, number>
  score_basis: BenchmarkRadarScoreBasis
  metrics: BenchmarkRadarMetrics
}

export interface BenchmarkPublicRadar {
  ranking_basis: 'ability_score_only'
  latest_run: BenchmarkPublicRadarLatestRun | null
  targets: BenchmarkRadarTarget[]
}
