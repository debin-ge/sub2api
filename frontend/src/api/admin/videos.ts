import { apiClient } from '@/api/client'

const ADMIN_VIDEOS_PATH = '/admin/videos'

export interface VideoInputManifestEntry {
  role: string
  file_name?: string
  mime_type: string
  size: number
  sha256: string
  width?: number
  height?: number
}

export interface VideoAccessMetadata {
  configured: boolean
  kind?: string
  scope?: string
  expires_at?: string
}

export interface VideoAdminTask {
	billing_review_id?: number
	version: number
	lease_epoch: number
	lease_expires_at?: string
  id: number
  public_id: string
  source: string
  user_id: number
  api_key_id?: number
  group_id?: number
  channel_id?: number
  account_id?: number
  provider: string
  operation: string
  parent_task_id?: number
  root_task_id?: number
  endpoint: string
  requested_model: string
  public_model: string
  channel_model: string
  upstream_model: string
  input_manifest: VideoInputManifestEntry[]
  request_attributes: Record<string, unknown>
  provider_task_id?: string
  provider_status?: string
	provider_created_at?: string
	provider_finished_at?: string
	stable_client_token?: string
  generation_state: string
  billing_state: string
  delete_state: string
  progress?: number
  usage_snapshot: Record<string, unknown>
  response_metadata: Record<string, unknown>
  content_variants: string[]
  content_expires_at?: string
  provider_access: VideoAccessMetadata
  billing_unit?: string
  estimated_units?: number
  actual_units?: number
  unit_price?: number
  customer_multiplier?: number
  estimated_cost?: number
  pricing_source?: string
  pricing_rule_key?: string
  resolution?: string
  duration_seconds?: number
  video_tokens?: number
  price_snapshot: Record<string, unknown>
  provider_cost_snapshot: Record<string, unknown>
  currency: string
  hold_id?: string
  hold_amount?: number
  actual_cost?: number
  callback_configured: boolean
  next_action_at?: string
  poll_attempts: number
  submit_attempts: number
  last_error_kind?: string
  last_error_code?: string
  last_error_message?: string
  created_at: string
  updated_at: string
  submitted_at?: string
  started_at?: string
  finished_at?: string
  settled_at?: string
  submission_unknown_at?: string
  quarantined_at?: string
  deleted_at?: string
}

export interface VideoAdminResource {
  id: number
  public_id: string
  resource_type: string
  user_id: number
  api_key_id?: number
  group_id?: number
  provider: string
  channel_id?: number
  account_id: number
  source_task_id?: number
  provider_resource_id: string
  model: string
  status: string
  metadata: Record<string, unknown>
  provider_access: VideoAccessMetadata
  created_at: string
  updated_at: string
  expires_at?: string
  deleted_at?: string
}

export interface VideoAdminEvent {
  id: number
  task_id?: number
  event_type: string
  provider?: string
  account_id?: number
  provider_task_id?: string
  provider_event_id?: string
  from_generation_state?: string
  to_generation_state?: string
  from_billing_state?: string
  to_billing_state?: string
  payload: Record<string, unknown>
  event_hash?: string
  created_at: string
}

export interface VideoAdminCallback {
  id: number
  task_id: number
  event_id: string
  event_type: string
  payload: Record<string, unknown>
  target_configured: boolean
  status: string
  attempts: number
  next_attempt_at: string
  expires_at: string
  last_error?: string
  last_status_code?: number
  created_at: string
  updated_at: string
  delivered_at?: string
  quarantined_at?: string
}

export interface VideoAdminOverview {
  tasks_by_generation: Record<string, number>
  tasks_by_billing: Record<string, number>
  tasks_by_delete: Record<string, number>
  callbacks_by_status: Record<string, number>
  submission_unknown: number
  unknown_hold_amount: number
	held_amount: number
  unmatched_webhooks: number
  oldest_task_pending_at?: string
  oldest_billing_at?: string
	oldest_manual_review_at?: string
  oldest_callback_at?: string
	queue_status: string
	queue?: {
		ready: number
		delayed: number
		active: number
	}
	spool: {
		enabled: boolean
		active_sessions: number
		current_bytes: number
		max_bytes: number
		utilization: number
		orphan_candidates: number
		last_sweep_at?: string
		last_sweep_result: string
		cleanup_failure_count: number
	}
}

export interface VideoCapabilities {
	default_model: string
	default_seconds: Record<string, number>
	default_sizes: Record<string, string>
	operations: Record<string, boolean>
	input_roles_by_operation: Record<string, Record<string, boolean>>
	input_mime_types: Record<string, Record<string, boolean>>
	max_input_bytes: Record<string, number>
	max_inputs_by_operation: Record<string, number>
	allow_reference_and_file: boolean
	content_variants: Record<string, boolean>
	supported_models: Record<string, boolean>
	supported_seconds: Record<string, number[]>
	supported_sizes: Record<string, string[]>
}

export interface VideoCapabilityCatalogDocument {
	version: number
	providers: Record<string, VideoCapabilities>
}

export interface VideoCapabilityCatalogView extends VideoCapabilityCatalogDocument {
	source: 'builtin' | 'settings'
	loaded_at: string
	last_refresh_error?: string
}

export interface VideoCapabilityProbeResult {
	provider: string
	capability: string
	status: 'supported' | 'unsupported' | 'unknown'
	checked_at: string
	http_status?: number
	error_summary?: string
}

export interface VideoAccountCapabilityStatus {
	account_id: number
	probe?: VideoCapabilityProbeResult
	override_configured: boolean
	override_enabled: boolean
	effective: boolean
}

export interface VideoAdminPage<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export interface VideoTaskQuery {
  page?: number
  page_size?: number
  user_id?: number
  group_id?: number
  account_id?: number
  provider?: string
  operation?: string
  generation_state?: string
  billing_state?: string
  delete_state?: string
  q?: string
  created_after?: string
  created_before?: string
}

export interface VideoResourceQuery {
  page?: number
  page_size?: number
  user_id?: number
  account_id?: number
  provider?: string
  status?: string
  q?: string
}

export interface VideoCallbackQuery {
  page?: number
  page_size?: number
  task_id?: string
  status?: string
}

export async function getVideoOverview(): Promise<VideoAdminOverview> {
  const { data } = await apiClient.get<VideoAdminOverview>(`${ADMIN_VIDEOS_PATH}/overview`)
  return data
}

export async function getVideoCapabilityCatalog(): Promise<VideoCapabilityCatalogView> {
	const { data } = await apiClient.get<VideoCapabilityCatalogView>(`${ADMIN_VIDEOS_PATH}/capabilities`)
	return data
}

export async function updateVideoCapabilityCatalog(document: VideoCapabilityCatalogDocument): Promise<VideoCapabilityCatalogView> {
	const { data } = await apiClient.put<VideoCapabilityCatalogView>(`${ADMIN_VIDEOS_PATH}/capabilities`, document)
	return data
}

export async function getVideoAccountCapability(accountId: number): Promise<VideoAccountCapabilityStatus> {
	const { data } = await apiClient.get<VideoAccountCapabilityStatus>(`${ADMIN_VIDEOS_PATH}/accounts/${accountId}/capability`)
	return data
}

export async function probeVideoAccountCapability(accountId: number): Promise<VideoAccountCapabilityStatus> {
	const { data } = await apiClient.post<VideoAccountCapabilityStatus>(`${ADMIN_VIDEOS_PATH}/accounts/${accountId}/capability/probe`)
	return data
}

export async function listVideoTasks(query: VideoTaskQuery = {}): Promise<VideoAdminPage<VideoAdminTask>> {
  const { data } = await apiClient.get<VideoAdminPage<VideoAdminTask>>(`${ADMIN_VIDEOS_PATH}/tasks`, { params: query })
  return data
}

export async function listUnknownVideoTasks(query: VideoTaskQuery = {}): Promise<VideoAdminPage<VideoAdminTask>> {
  const { data } = await apiClient.get<VideoAdminPage<VideoAdminTask>>(`${ADMIN_VIDEOS_PATH}/tasks/unknown`, { params: query })
  return data
}

export async function getVideoTask(id: string): Promise<VideoAdminTask> {
  const { data } = await apiClient.get<VideoAdminTask>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}`)
  return data
}

export async function listVideoTaskEvents(id: string, page = 1, pageSize = 100): Promise<VideoAdminPage<VideoAdminEvent>> {
  const { data } = await apiClient.get<VideoAdminPage<VideoAdminEvent>>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/events`, {
    params: { page, page_size: pageSize },
  })
  return data
}

export async function listVideoResources(query: VideoResourceQuery = {}): Promise<VideoAdminPage<VideoAdminResource>> {
  const { data } = await apiClient.get<VideoAdminPage<VideoAdminResource>>(`${ADMIN_VIDEOS_PATH}/resources`, { params: query })
  return data
}

export async function listUnmatchedVideoEvents(page = 1, pageSize = 20): Promise<VideoAdminPage<VideoAdminEvent>> {
  const { data } = await apiClient.get<VideoAdminPage<VideoAdminEvent>>(`${ADMIN_VIDEOS_PATH}/webhooks/unmatched`, {
    params: { page, page_size: pageSize },
  })
  return data
}

export async function listVideoCallbacks(query: VideoCallbackQuery = {}): Promise<VideoAdminPage<VideoAdminCallback>> {
  const { data } = await apiClient.get<VideoAdminPage<VideoAdminCallback>>(`${ADMIN_VIDEOS_PATH}/callbacks`, { params: query })
  return data
}

function videoTaskVersionHeaders(version: number) {
	if (!Number.isSafeInteger(version) || version < 0) throw new Error('Refresh the task to obtain a valid version before modifying it')
	return { headers: { 'If-Match': `"${version}"` } }
}

export async function resolveVideoNotCreated(id: string, version: number, evidence: VideoBillingReviewEvidence, operationKey: string): Promise<VideoAdminTask> {
  const { data } = await apiClient.post<VideoAdminTask>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/resolve-not-created`, { reason: evidence.reason, evidence_ref: evidence.evidence_ref }, videoReviewHeaders(version, operationKey))
  return data
}

export async function resolveVideoCreated(id: string, providerTaskId: string, version: number, evidence: VideoBillingReviewEvidence, operationKey: string): Promise<VideoAdminTask> {
  const { data } = await apiClient.post<VideoAdminTask>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/resolve-created`, {
    provider_task_id: providerTaskId,
    reason: evidence.reason, evidence_ref: evidence.evidence_ref,
  }, videoReviewHeaders(version, operationKey))
  return data
}

export async function retryVideoGet(id: string, version: number): Promise<VideoAdminTask> {
  const { data } = await apiClient.post<VideoAdminTask>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/retry-get`, undefined, videoTaskVersionHeaders(version))
  return data
}

export async function retryVideoSettlement(id: string, version: number): Promise<VideoAdminTask> {
  const { data } = await apiClient.post<VideoAdminTask>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/retry-settlement`, undefined, videoTaskVersionHeaders(version))
  return data
}

export interface VideoBillingReviewEvidence {
	reason: string
	evidence_ref: string
	honor_frozen_quote?: boolean
}

export interface VideoBillingReview {
	submission_review_id?: number
	facts: Record<string, unknown>
	id: number
	task_id: number
	action: 'capture' | 'release'
	status: 'pending' | 'approved' | 'rejected'
	proposed_by: number
	decided_by?: number
	actual_units: number
	actual_cost: number
	hold_amount: number
	reason: string
	evidence_ref: string
	honor_frozen_quote: boolean
	requires_second_actor: boolean
	decision_reason?: string
	created_at: string
}

function videoReviewHeaders(version: number, operationKey: string) {
	return { headers: { ...videoTaskVersionHeaders(version).headers, 'Idempotency-Key': operationKey } }
}

export interface VideoSubmissionReview {
	id: number
	task_id: number
	action: 'created' | 'not_created'
	provider_task_id?: string
	status: 'pending' | 'approved' | 'rejected'
	proposed_by: number
	decided_by?: number
	account_identity_version: number
	reason: string
	evidence_ref: string
	facts: Record<string, unknown>
	provider_observation?: Record<string, unknown>
	decision_reason?: string
	created_at: string
}

export async function listVideoSubmissionReviews(id: string): Promise<VideoSubmissionReview[]> {
	const { data } = await apiClient.get<VideoSubmissionReview[]>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/submission-reviews`)
	return data
}

export async function decideVideoSubmissionReview(id: string, reviewId: number, approve: boolean, reason: string, version: number, operationKey: string): Promise<VideoAdminTask> {
	const { data } = await apiClient.post<VideoAdminTask>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/submission-reviews/${reviewId}/${approve ? 'approve' : 'reject'}`, { reason }, videoReviewHeaders(version, operationKey))
	return data
}

export async function retryVideoCharacterResource(id: string, version: number): Promise<VideoAdminTask> {
	const { data } = await apiClient.post<VideoAdminTask>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/retry-character-resource`, undefined, videoTaskVersionHeaders(version))
	return data
}

export async function resolveVideoBillingCapture(id: string, actualUnits: number, version: number, evidence: VideoBillingReviewEvidence, operationKey: string): Promise<VideoAdminTask> {
	const { data } = await apiClient.post<VideoAdminTask>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/resolve-billing-capture`, {
		actual_units: actualUnits,
		...evidence,
	}, videoReviewHeaders(version, operationKey))
	return data
}

export async function resolveVideoBillingRelease(id: string, version: number, evidence: VideoBillingReviewEvidence, operationKey: string): Promise<VideoAdminTask> {
	const { data } = await apiClient.post<VideoAdminTask>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/resolve-billing-release`, {
		reason: evidence.reason, evidence_ref: evidence.evidence_ref,
	}, videoReviewHeaders(version, operationKey))
	return data
}

export async function listVideoBillingReviews(id: string): Promise<VideoBillingReview[]> {
	const { data } = await apiClient.get<VideoBillingReview[]>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/billing-reviews`)
	return data
}

export async function decideVideoBillingReview(id: string, reviewId: number, approve: boolean, reason: string, version: number, operationKey: string): Promise<VideoAdminTask> {
	const { data } = await apiClient.post<VideoAdminTask>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/billing-reviews/${reviewId}/${approve ? 'approve' : 'reject'}`, { reason }, videoReviewHeaders(version, operationKey))
	return data
}

export async function retryVideoDelete(id: string, version: number): Promise<VideoAdminTask> {
  const { data } = await apiClient.post<VideoAdminTask>(`${ADMIN_VIDEOS_PATH}/tasks/${encodeURIComponent(id)}/retry-delete`, undefined, videoTaskVersionHeaders(version))
  return data
}

export async function retryVideoCallback(id: number): Promise<VideoAdminCallback> {
  const { data } = await apiClient.post<VideoAdminCallback>(`${ADMIN_VIDEOS_PATH}/callbacks/${id}/retry`)
  return data
}

const videosAdminAPI = {
  overview: getVideoOverview,
	getCapabilities: getVideoCapabilityCatalog,
	updateCapabilities: updateVideoCapabilityCatalog,
	getAccountCapability: getVideoAccountCapability,
	probeAccountCapability: probeVideoAccountCapability,
  listTasks: listVideoTasks,
  listUnknown: listUnknownVideoTasks,
  getTask: getVideoTask,
  listEvents: listVideoTaskEvents,
  listResources: listVideoResources,
  listUnmatchedEvents: listUnmatchedVideoEvents,
  listCallbacks: listVideoCallbacks,
  resolveNotCreated: resolveVideoNotCreated,
  resolveCreated: resolveVideoCreated,
  retryGet: retryVideoGet,
  retrySettlement: retryVideoSettlement,
	resolveBillingCapture: resolveVideoBillingCapture,
	resolveBillingRelease: resolveVideoBillingRelease,
	listBillingReviews: listVideoBillingReviews,
	decideBillingReview: decideVideoBillingReview,
	listSubmissionReviews: listVideoSubmissionReviews,
	decideSubmissionReview: decideVideoSubmissionReview,
	retryCharacterResource: retryVideoCharacterResource,
  retryDelete: retryVideoDelete,
  retryCallback: retryVideoCallback,
}

export default videosAdminAPI
