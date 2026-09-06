package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/jsonstrict"
	"github.com/gin-gonic/gin"
)

const videoAdminMaxBodyBytes = 256 << 10

type videoAdminController interface {
	ListTasks(context.Context, service.VideoAdminTaskFilter) (*service.VideoAdminTaskPage, error)
	GetTask(context.Context, string) (*service.VideoTask, error)
	ListResources(context.Context, service.VideoAdminResourceFilter) (*service.VideoAdminResourcePage, error)
	GetResource(context.Context, string) (*service.VideoResource, error)
	ListEvents(context.Context, string, int, int) (*service.VideoAdminEventPage, error)
	ListUnmatchedEvents(context.Context, int, int) (*service.VideoAdminEventPage, error)
	ListCallbacks(context.Context, service.VideoAdminCallbackFilter) (*service.VideoAdminCallbackPage, error)
	Overview(context.Context) (*service.VideoAdminOverview, error)
	ResolveNotCreated(context.Context, string) (*service.VideoTask, error)
	ResolveCreated(context.Context, string, string) (*service.VideoTask, error)
	RetryProviderGet(context.Context, string) (*service.VideoTask, error)
	RetrySettlement(context.Context, string) (*service.VideoTask, error)
	ResolveBillingCapture(context.Context, string, float64) (*service.VideoTask, error)
	ResolveBillingRelease(context.Context, string) (*service.VideoTask, error)
	ListBillingReviews(context.Context, string) ([]*service.VideoBillingReview, error)
	DecideBillingReview(context.Context, string, int64, bool) (*service.VideoTask, error)
	ListSubmissionReviews(context.Context, string) ([]*service.VideoSubmissionReview, error)
	DecideSubmissionReview(context.Context, string, int64, bool) (*service.VideoTask, error)
	RetryCharacterResource(context.Context, string) (*service.VideoTask, error)
	RetryDelete(context.Context, string) (*service.VideoTask, error)
	RetryCallback(context.Context, int64) (*service.VideoCallbackDelivery, error)
	GetCapabilityCatalog(context.Context) (*service.VideoCapabilityCatalogView, error)
	UpdateCapabilityCatalog(context.Context, service.VideoCapabilityCatalogDocument) (*service.VideoCapabilityCatalogView, error)
	GetAccountCapability(context.Context, int64) (*service.VideoAccountCapabilityStatus, error)
	ProbeAccountCapability(context.Context, int64) (*service.VideoAccountCapabilityStatus, error)
}

type VideoHandler struct {
	controller videoAdminController
}

func NewVideoHandler(controller *service.VideoAdminService) *VideoHandler {
	return &VideoHandler{controller: controller}
}

func newVideoHandler(controller videoAdminController) *VideoHandler {
	return &VideoHandler{controller: controller}
}

type videoAdminAccessMetadata struct {
	Configured bool       `json:"configured"`
	Kind       string     `json:"kind,omitempty"`
	Scope      string     `json:"scope,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type videoAdminTaskResponse struct {
	Version              int64                             `json:"version"`
	LeaseEpoch           int64                             `json:"lease_epoch"`
	LeaseExpiresAt       *time.Time                        `json:"lease_expires_at,omitempty"`
	ID                   int64                             `json:"id"`
	PublicID             string                            `json:"public_id"`
	Source               string                            `json:"source"`
	UserID               int64                             `json:"user_id"`
	APIKeyID             *int64                            `json:"api_key_id,omitempty"`
	GroupID              *int64                            `json:"group_id,omitempty"`
	ChannelID            *int64                            `json:"channel_id,omitempty"`
	AccountID            *int64                            `json:"account_id,omitempty"`
	Provider             string                            `json:"provider"`
	Operation            string                            `json:"operation"`
	ParentTaskID         *int64                            `json:"parent_task_id,omitempty"`
	RootTaskID           *int64                            `json:"root_task_id,omitempty"`
	Endpoint             string                            `json:"endpoint"`
	RequestedModel       string                            `json:"requested_model"`
	PublicModel          string                            `json:"public_model"`
	ChannelModel         string                            `json:"channel_model"`
	UpstreamModel        string                            `json:"upstream_model"`
	InputManifest        []service.VideoInputManifestEntry `json:"input_manifest"`
	RequestAttributes    map[string]any                    `json:"request_attributes"`
	ProviderTaskID       string                            `json:"provider_task_id,omitempty"`
	ProviderStatus       string                            `json:"provider_status,omitempty"`
	ProviderCreatedAt    *time.Time                        `json:"provider_created_at,omitempty"`
	ProviderFinishedAt   *time.Time                        `json:"provider_finished_at,omitempty"`
	StableClientToken    string                            `json:"stable_client_token,omitempty"`
	GenerationState      string                            `json:"generation_state"`
	BillingState         string                            `json:"billing_state"`
	DeleteState          string                            `json:"delete_state"`
	Progress             *float64                          `json:"progress,omitempty"`
	UsageSnapshot        map[string]any                    `json:"usage_snapshot"`
	ResponseMetadata     map[string]any                    `json:"response_metadata"`
	ContentVariants      []string                          `json:"content_variants"`
	ContentExpiresAt     *time.Time                        `json:"content_expires_at,omitempty"`
	ProviderAccess       videoAdminAccessMetadata          `json:"provider_access"`
	BillingUnit          string                            `json:"billing_unit,omitempty"`
	EstimatedUnits       *float64                          `json:"estimated_units,omitempty"`
	ActualUnits          *float64                          `json:"actual_units,omitempty"`
	UnitPrice            *float64                          `json:"unit_price,omitempty"`
	CustomerMultiplier   *float64                          `json:"customer_multiplier,omitempty"`
	EstimatedCost        *float64                          `json:"estimated_cost,omitempty"`
	PricingSource        string                            `json:"pricing_source,omitempty"`
	PricingRuleKey       string                            `json:"pricing_rule_key,omitempty"`
	Resolution           string                            `json:"resolution,omitempty"`
	DurationSeconds      *float64                          `json:"duration_seconds,omitempty"`
	VideoTokens          *float64                          `json:"video_tokens,omitempty"`
	PriceSnapshot        map[string]any                    `json:"price_snapshot"`
	ProviderCostSnapshot map[string]any                    `json:"provider_cost_snapshot"`
	Currency             string                            `json:"currency"`
	HoldID               string                            `json:"hold_id,omitempty"`
	HoldAmount           *float64                          `json:"hold_amount,omitempty"`
	ActualCost           *float64                          `json:"actual_cost,omitempty"`
	CallbackConfigured   bool                              `json:"callback_configured"`
	CallbackIntentState  string                            `json:"callback_intent_state"`
	BillingReviewID      *int64                            `json:"billing_review_id,omitempty"`
	SubmissionReviewID   *int64                            `json:"submission_review_id,omitempty"`
	NextActionAt         *time.Time                        `json:"next_action_at,omitempty"`
	PollAttempts         int                               `json:"poll_attempts"`
	SubmitAttempts       int                               `json:"submit_attempts"`
	LastErrorKind        string                            `json:"last_error_kind,omitempty"`
	LastErrorCode        string                            `json:"last_error_code,omitempty"`
	LastErrorMessage     string                            `json:"last_error_message,omitempty"`
	CreatedAt            time.Time                         `json:"created_at"`
	UpdatedAt            time.Time                         `json:"updated_at"`
	SubmittedAt          *time.Time                        `json:"submitted_at,omitempty"`
	StartedAt            *time.Time                        `json:"started_at,omitempty"`
	FinishedAt           *time.Time                        `json:"finished_at,omitempty"`
	SettledAt            *time.Time                        `json:"settled_at,omitempty"`
	SubmissionUnknownAt  *time.Time                        `json:"submission_unknown_at,omitempty"`
	QuarantinedAt        *time.Time                        `json:"quarantined_at,omitempty"`
	DeletedAt            *time.Time                        `json:"deleted_at,omitempty"`
}

func videoAdminTask(task *service.VideoTask) *videoAdminTaskResponse {
	if task == nil {
		return nil
	}
	response := &videoAdminTaskResponse{
		Version: task.Version, LeaseEpoch: task.LeaseEpoch, LeaseExpiresAt: task.LeaseExpiresAt,
		ID: task.ID, PublicID: task.PublicID, Source: task.Source, UserID: task.UserID,
		APIKeyID: task.APIKeyID, GroupID: task.GroupID, ChannelID: task.ChannelID, AccountID: task.AccountID,
		Provider: task.Provider, Operation: task.Operation, ParentTaskID: task.ParentTaskID, RootTaskID: task.RootTaskID,
		Endpoint: task.Endpoint, RequestedModel: task.RequestedModel, PublicModel: task.PublicModel,
		ChannelModel: task.ChannelModel, UpstreamModel: task.UpstreamModel,
		InputManifest: task.InputManifest, RequestAttributes: task.RequestAttributes,
		ProviderTaskID: stringValue(task.ProviderTaskID), ProviderStatus: stringValue(task.ProviderStatus),
		ProviderCreatedAt: task.ProviderCreatedAt, ProviderFinishedAt: task.ProviderFinishedAt,
		StableClientToken: stringValue(task.StableClientToken),
		GenerationState:   task.GenerationState, BillingState: task.BillingState, DeleteState: task.DeleteState,
		Progress: task.Progress, UsageSnapshot: task.UsageSnapshot, ResponseMetadata: task.ResponseMetadata,
		ContentVariants: task.ContentVariants, ContentExpiresAt: task.ContentExpiresAt,
		ProviderAccess: videoAdminAccessMetadata{Configured: task.ProviderAccessEnc != nil, Kind: stringValue(task.ProviderAccessKind), Scope: stringValue(task.ProviderAccessScope), ExpiresAt: task.ProviderAccessExpires},
		BillingUnit:    stringValue(task.BillingUnit), EstimatedUnits: task.EstimatedUnits, ActualUnits: task.ActualUnits,
		PriceSnapshot: task.PriceSnapshot, ProviderCostSnapshot: task.ProviderCostSnapshot, Currency: task.Currency,
		HoldID: stringValue(task.HoldID), HoldAmount: task.HoldAmount, ActualCost: task.ActualCost,
		CallbackConfigured: task.CallbackURLEnc != nil, NextActionAt: task.NextActionAt,
		CallbackIntentState: task.CallbackIntentState,
		BillingReviewID:     task.BillingReviewID,
		SubmissionReviewID:  task.SubmissionReviewID,
		PollAttempts:        task.PollAttempts, SubmitAttempts: task.SubmitAttempts,
		LastErrorKind: stringValue(task.LastErrorKind), LastErrorCode: stringValue(task.LastErrorCode), LastErrorMessage: stringValue(task.LastErrorMessage),
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt, SubmittedAt: task.SubmittedAt, StartedAt: task.StartedAt,
		FinishedAt: task.FinishedAt, SettledAt: task.SettledAt, SubmissionUnknownAt: task.SubmissionUnknownAt,
		QuarantinedAt: task.QuarantinedAt, DeletedAt: task.DeletedAt,
	}
	response.UnitPrice = videoAdminNumber(task.PriceSnapshot, "unit_price")
	response.CustomerMultiplier = videoAdminNumber(task.PriceSnapshot, "customer_multiplier")
	response.EstimatedCost = videoAdminNumber(task.PriceSnapshot, "estimated_cost")
	response.PricingSource = videoAdminString(task.PriceSnapshot, "pricing_source")
	response.PricingRuleKey = videoAdminString(task.PriceSnapshot, "rule_key")
	response.Resolution = firstVideoAdminString(
		videoAdminString(task.ResponseMetadata, "size"),
		videoAdminString(task.ResponseMetadata, "resolution"),
		videoAdminString(task.PriceSnapshot, "resolution"),
		videoAdminString(task.RequestAttributes, "size"),
	)
	response.DurationSeconds = firstVideoAdminNumber(
		videoAdminNumber(task.ResponseMetadata, "seconds"),
		videoAdminNumber(task.PriceSnapshot, "seconds"),
		videoAdminNumber(task.RequestAttributes, "seconds"),
	)
	if response.BillingUnit == service.VideoBillingUnitVideoToken {
		response.VideoTokens = task.ActualUnits
		if response.VideoTokens == nil {
			response.VideoTokens = firstVideoAdminNumber(
				videoAdminNumber(task.UsageSnapshot, "video_tokens"),
				videoAdminNumber(task.UsageSnapshot, "completion_tokens"),
				videoAdminNumber(task.UsageSnapshot, "output_tokens"),
			)
		}
	}
	return response
}

func videoAdminNumber(values map[string]any, key string) *float64 {
	if values == nil {
		return nil
	}
	var result float64
	switch value := values[key].(type) {
	case float64:
		result = value
	case float32:
		result = float64(value)
	case int:
		result = float64(value)
	case int64:
		result = float64(value)
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return nil
		}
		result = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil
		}
		result = parsed
	default:
		return nil
	}
	return &result
}

func firstVideoAdminNumber(values ...*float64) *float64 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func videoAdminString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func firstVideoAdminString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type videoAdminResourceResponse struct {
	ID                 int64                    `json:"id"`
	PublicID           string                   `json:"public_id"`
	ResourceType       string                   `json:"resource_type"`
	UserID             int64                    `json:"user_id"`
	APIKeyID           *int64                   `json:"api_key_id,omitempty"`
	GroupID            *int64                   `json:"group_id,omitempty"`
	Provider           string                   `json:"provider"`
	ChannelID          *int64                   `json:"channel_id,omitempty"`
	AccountID          int64                    `json:"account_id"`
	SourceTaskID       *int64                   `json:"source_task_id,omitempty"`
	ProviderResourceID string                   `json:"provider_resource_id"`
	Model              string                   `json:"model"`
	Status             string                   `json:"status"`
	Metadata           map[string]any           `json:"metadata"`
	ProviderAccess     videoAdminAccessMetadata `json:"provider_access"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
	ExpiresAt          *time.Time               `json:"expires_at,omitempty"`
	DeletedAt          *time.Time               `json:"deleted_at,omitempty"`
}

func videoAdminResource(resource *service.VideoResource) *videoAdminResourceResponse {
	if resource == nil {
		return nil
	}
	return &videoAdminResourceResponse{
		ID: resource.ID, PublicID: resource.PublicID, ResourceType: resource.ResourceType, UserID: resource.UserID,
		APIKeyID: resource.APIKeyID, GroupID: resource.GroupID, Provider: resource.Provider, ChannelID: resource.ChannelID,
		AccountID: resource.AccountID, SourceTaskID: resource.SourceTaskID, ProviderResourceID: resource.ProviderResourceID,
		Model: resource.Model, Status: resource.Status, Metadata: resource.Metadata,
		ProviderAccess: videoAdminAccessMetadata{Configured: resource.ProviderAccessEnc != nil, Kind: stringValue(resource.ProviderAccessKind), Scope: stringValue(resource.ProviderAccessScope), ExpiresAt: resource.ProviderAccessExpires},
		CreatedAt:      resource.CreatedAt, UpdatedAt: resource.UpdatedAt, ExpiresAt: resource.ExpiresAt, DeletedAt: resource.DeletedAt,
	}
}

type videoAdminCallbackResponse struct {
	ID               int64          `json:"id"`
	TaskID           int64          `json:"task_id"`
	EventID          string         `json:"event_id"`
	EventType        string         `json:"event_type"`
	Payload          map[string]any `json:"payload"`
	TargetConfigured bool           `json:"target_configured"`
	Status           string         `json:"status"`
	Attempts         int            `json:"attempts"`
	NextAttemptAt    time.Time      `json:"next_attempt_at"`
	ExpiresAt        time.Time      `json:"expires_at"`
	LastError        string         `json:"last_error,omitempty"`
	LastStatusCode   *int           `json:"last_status_code,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeliveredAt      *time.Time     `json:"delivered_at,omitempty"`
	QuarantinedAt    *time.Time     `json:"quarantined_at,omitempty"`
}

func videoAdminCallback(callback *service.VideoCallbackDelivery) *videoAdminCallbackResponse {
	if callback == nil {
		return nil
	}
	return &videoAdminCallbackResponse{
		ID: callback.ID, TaskID: callback.TaskID, EventID: callback.EventID, EventType: callback.EventType,
		Payload: callback.Payload, TargetConfigured: strings.TrimSpace(callback.TargetURLEnc) != "",
		Status: callback.Status, Attempts: callback.Attempts, NextAttemptAt: callback.NextAttemptAt,
		ExpiresAt: callback.ExpiresAt, LastError: stringValue(callback.LastError), LastStatusCode: callback.LastStatusCode,
		CreatedAt: callback.CreatedAt, UpdatedAt: callback.UpdatedAt, DeliveredAt: callback.DeliveredAt, QuarantinedAt: callback.QuarantinedAt,
	}
}

func (h *VideoHandler) Overview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	value, err := h.requireController().Overview(c.Request.Context())
	videoAdminRespond(c, value, err)
}

func (h *VideoHandler) GetCapabilityCatalog(c *gin.Context) {
	value, err := h.requireController().GetCapabilityCatalog(c.Request.Context())
	videoAdminRespond(c, value, err)
}

func (h *VideoHandler) UpdateCapabilityCatalog(c *gin.Context) {
	var document service.VideoCapabilityCatalogDocument
	if err := decodeStrictVideoAdminJSON(c, &document); err != nil {
		response.BadRequest(c, "invalid video capability catalog")
		return
	}
	value, err := h.requireController().UpdateCapabilityCatalog(c.Request.Context(), document)
	videoAdminRespond(c, value, err)
}

func (h *VideoHandler) GetAccountCapability(c *gin.Context) {
	accountID, err := videoAdminRequiredID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid account id")
		return
	}
	value, err := h.requireController().GetAccountCapability(c.Request.Context(), accountID)
	videoAdminRespond(c, value, err)
}

func (h *VideoHandler) ProbeAccountCapability(c *gin.Context) {
	if !videoAdminEmptyBody(c) {
		response.BadRequest(c, "request body must be empty")
		return
	}
	accountID, err := videoAdminRequiredID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid account id")
		return
	}
	value, err := h.requireController().ProbeAccountCapability(c.Request.Context(), accountID)
	videoAdminRespond(c, value, err)
}

func (h *VideoHandler) ListTasks(c *gin.Context) {
	filter, err := videoAdminTaskFilter(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	page, err := h.requireController().ListTasks(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]*videoAdminTaskResponse, 0, len(page.Tasks))
	for _, task := range page.Tasks {
		items = append(items, videoAdminTask(task))
	}
	response.Success(c, gin.H{"items": items, "total": page.Total, "page": page.Page, "page_size": page.PageSize})
}

func (h *VideoHandler) ListUnknown(c *gin.Context) {
	filter, err := videoAdminTaskFilter(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	filter.GenerationState = service.VideoGenerationSubmissionUnknown
	page, err := h.requireController().ListTasks(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]*videoAdminTaskResponse, 0, len(page.Tasks))
	for _, task := range page.Tasks {
		items = append(items, videoAdminTask(task))
	}
	response.Success(c, gin.H{"items": items, "total": page.Total, "page": page.Page, "page_size": page.PageSize})
}

func (h *VideoHandler) GetTask(c *gin.Context) {
	task, err := h.requireController().GetTask(c.Request.Context(), c.Param("id"))
	videoAdminRespond(c, videoAdminTask(task), err)
}

func (h *VideoHandler) ListEvents(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	result, err := h.requireController().ListEvents(c.Request.Context(), c.Param("id"), page, pageSize)
	videoAdminRespond(c, result, err)
}

func (h *VideoHandler) ListUnmatchedEvents(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	result, err := h.requireController().ListUnmatchedEvents(c.Request.Context(), page, pageSize)
	videoAdminRespond(c, result, err)
}

func (h *VideoHandler) ListResources(c *gin.Context) {
	filter, err := videoAdminResourceFilter(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	page, err := h.requireController().ListResources(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]*videoAdminResourceResponse, 0, len(page.Resources))
	for _, resource := range page.Resources {
		items = append(items, videoAdminResource(resource))
	}
	response.Success(c, gin.H{"items": items, "total": page.Total, "page": page.Page, "page_size": page.PageSize})
}

func (h *VideoHandler) GetResource(c *gin.Context) {
	resource, err := h.requireController().GetResource(c.Request.Context(), c.Param("id"))
	videoAdminRespond(c, videoAdminResource(resource), err)
}

func (h *VideoHandler) ListCallbacks(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	result, err := h.requireController().ListCallbacks(c.Request.Context(), service.VideoAdminCallbackFilter{
		Page: page, PageSize: pageSize, TaskPublicID: strings.TrimSpace(c.Query("task_id")), Status: strings.TrimSpace(c.Query("status")),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]*videoAdminCallbackResponse, 0, len(result.Callbacks))
	for _, callback := range result.Callbacks {
		items = append(items, videoAdminCallback(callback))
	}
	response.Success(c, gin.H{"items": items, "total": result.Total, "page": result.Page, "page_size": result.PageSize})
}

func (h *VideoHandler) ResolveNotCreated(c *gin.Context) {
	if !requireVideoAdminVersion(c) {
		return
	}
	var request struct {
		Reason      string `json:"reason"`
		EvidenceRef string `json:"evidence_ref"`
	}
	if err := decodeStrictVideoAdminJSON(c, &request); err != nil {
		response.BadRequest(c, "review evidence is required")
		return
	}
	if !requireVideoReviewAudit(c, service.VideoBillingReviewRequest{Action: service.BalanceSettlementRelease, Reason: request.Reason, EvidenceRef: request.EvidenceRef}, false) {
		return
	}
	task, err := h.requireController().ResolveNotCreated(c.Request.Context(), c.Param("id"))
	videoAdminRespond(c, videoAdminTask(task), err)
}

type videoAdminResolveCreatedRequest struct {
	ProviderTaskID string `json:"provider_task_id"`
	Reason         string `json:"reason"`
	EvidenceRef    string `json:"evidence_ref"`
}

func (h *VideoHandler) ResolveCreated(c *gin.Context) {
	if !requireVideoAdminVersion(c) {
		return
	}
	var request videoAdminResolveCreatedRequest
	if err := decodeStrictVideoAdminJSON(c, &request); err != nil || strings.TrimSpace(request.ProviderTaskID) == "" {
		response.BadRequest(c, "provider_task_id is required")
		return
	}
	if !requireVideoReviewAudit(c, service.VideoBillingReviewRequest{Action: service.BalanceSettlementRelease, Reason: request.Reason, EvidenceRef: request.EvidenceRef}, false) {
		return
	}
	task, err := h.requireController().ResolveCreated(c.Request.Context(), c.Param("id"), request.ProviderTaskID)
	videoAdminRespond(c, videoAdminTask(task), err)
}

func (h *VideoHandler) RetryProviderGet(c *gin.Context) {
	h.retryEmptyTaskAction(c, h.requireController().RetryProviderGet)
}

func (h *VideoHandler) RetryCharacterResource(c *gin.Context) {
	h.retryEmptyTaskAction(c, h.requireController().RetryCharacterResource)
}

func (h *VideoHandler) ListSubmissionReviews(c *gin.Context) {
	reviews, err := h.requireController().ListSubmissionReviews(c.Request.Context(), c.Param("id"))
	videoAdminRespond(c, reviews, err)
}

func (h *VideoHandler) ApproveSubmissionReview(c *gin.Context) { h.decideSubmissionReview(c, true) }
func (h *VideoHandler) RejectSubmissionReview(c *gin.Context)  { h.decideSubmissionReview(c, false) }

func (h *VideoHandler) decideSubmissionReview(c *gin.Context, approve bool) {
	if !requireVideoAdminVersion(c) {
		return
	}
	reviewID, err := strconv.ParseInt(c.Param("review_id"), 10, 64)
	if err != nil || reviewID <= 0 {
		response.BadRequest(c, "invalid submission review id")
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if err := decodeStrictVideoAdminJSON(c, &request); err != nil {
		response.BadRequest(c, "decision reason is required")
		return
	}
	if !requireVideoReviewAudit(c, service.VideoBillingReviewRequest{Reason: request.Reason}, true) {
		return
	}
	task, err := h.requireController().DecideSubmissionReview(c.Request.Context(), c.Param("id"), reviewID, approve)
	videoAdminRespond(c, videoAdminTask(task), err)
}

func (h *VideoHandler) RetrySettlement(c *gin.Context) {
	h.retryEmptyTaskAction(c, h.requireController().RetrySettlement)
}

type videoAdminResolveBillingCaptureRequest struct {
	ActualUnits      *float64 `json:"actual_units"`
	Reason           string   `json:"reason"`
	EvidenceRef      string   `json:"evidence_ref"`
	HonorFrozenQuote bool     `json:"honor_frozen_quote,omitempty"`
}

func (h *VideoHandler) ResolveBillingCapture(c *gin.Context) {
	if !requireVideoAdminVersion(c) {
		return
	}
	var request videoAdminResolveBillingCaptureRequest
	if err := decodeStrictVideoAdminJSON(c, &request); err != nil || request.ActualUnits == nil {
		response.BadRequest(c, "actual_units is invalid")
		return
	}
	if !requireVideoReviewAudit(c, service.VideoBillingReviewRequest{Action: service.BalanceSettlementCapture, ActualUnits: *request.ActualUnits,
		Reason: request.Reason, EvidenceRef: request.EvidenceRef, HonorFrozenQuote: request.HonorFrozenQuote}, false) {
		return
	}
	task, err := h.requireController().ResolveBillingCapture(c.Request.Context(), c.Param("id"), *request.ActualUnits)
	videoAdminRespond(c, videoAdminTask(task), err)
}

func (h *VideoHandler) ResolveBillingRelease(c *gin.Context) {
	if !requireVideoAdminVersion(c) {
		return
	}
	var request struct {
		Reason      string `json:"reason"`
		EvidenceRef string `json:"evidence_ref"`
	}
	if err := decodeStrictVideoAdminJSON(c, &request); err != nil {
		response.BadRequest(c, "review evidence is required")
		return
	}
	if !requireVideoReviewAudit(c, service.VideoBillingReviewRequest{Action: service.BalanceSettlementRelease, Reason: request.Reason, EvidenceRef: request.EvidenceRef}, false) {
		return
	}
	task, err := h.requireController().ResolveBillingRelease(c.Request.Context(), c.Param("id"))
	videoAdminRespond(c, videoAdminTask(task), err)
}

func requireVideoReviewAudit(c *gin.Context, request service.VideoBillingReviewRequest, decision bool) bool {
	request.ActorID, request.OperationKey = getAdminIDFromContext(c), strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	request.ApprovalThresholdUSD = service.VideoBillingReviewDefaultThresholdUSD
	if request.ActorID <= 0 {
		response.Error(c, http.StatusUnauthorized, "an individually authenticated administrator is required")
		return false
	}
	var err error
	if decision {
		err = service.ValidateVideoBillingReviewDecision(service.VideoBillingReviewDecision{ActorID: request.ActorID, OperationKey: request.OperationKey, Reason: request.Reason})
	} else {
		err = service.ValidateVideoBillingReviewRequest(request)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	c.Request = c.Request.WithContext(service.WithVideoBillingReviewRequest(c.Request.Context(), request))
	return true
}

func (h *VideoHandler) ListBillingReviews(c *gin.Context) {
	reviews, err := h.requireController().ListBillingReviews(c.Request.Context(), c.Param("id"))
	videoAdminRespond(c, reviews, err)
}

func (h *VideoHandler) ApproveBillingReview(c *gin.Context) { h.decideBillingReview(c, true) }
func (h *VideoHandler) RejectBillingReview(c *gin.Context)  { h.decideBillingReview(c, false) }

func (h *VideoHandler) decideBillingReview(c *gin.Context, approve bool) {
	if !requireVideoAdminVersion(c) {
		return
	}
	reviewID, err := strconv.ParseInt(c.Param("review_id"), 10, 64)
	if err != nil || reviewID <= 0 {
		response.BadRequest(c, "invalid billing review id")
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if err := decodeStrictVideoAdminJSON(c, &request); err != nil {
		response.BadRequest(c, "decision reason is required")
		return
	}
	if !requireVideoReviewAudit(c, service.VideoBillingReviewRequest{Reason: request.Reason}, true) {
		return
	}
	task, err := h.requireController().DecideBillingReview(c.Request.Context(), c.Param("id"), reviewID, approve)
	videoAdminRespond(c, videoAdminTask(task), err)
}

func (h *VideoHandler) RetryDelete(c *gin.Context) {
	h.retryEmptyTaskAction(c, h.requireController().RetryDelete)
}

func (h *VideoHandler) RetryCallback(c *gin.Context) {
	if !videoAdminEmptyBody(c) {
		response.BadRequest(c, "request body must be empty")
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid callback id")
		return
	}
	callback, err := h.requireController().RetryCallback(c.Request.Context(), id)
	videoAdminRespond(c, videoAdminCallback(callback), err)
}

func (h *VideoHandler) retryEmptyTaskAction(c *gin.Context, action func(context.Context, string) (*service.VideoTask, error)) {
	if !requireVideoAdminVersion(c) {
		return
	}
	if !videoAdminEmptyBody(c) {
		response.BadRequest(c, "request body must be empty")
		return
	}
	task, err := action(c.Request.Context(), c.Param("id"))
	videoAdminRespond(c, videoAdminTask(task), err)
}

func requireVideoAdminVersion(c *gin.Context) bool {
	value := strings.TrimSpace(c.GetHeader("If-Match"))
	if value == "" {
		response.Error(c, http.StatusPreconditionRequired, "If-Match with the displayed resource version is required")
		return false
	}
	if len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' {
		response.BadRequest(c, "If-Match must be a quoted non-negative resource version")
		return false
	}
	version, err := strconv.ParseInt(value[1:len(value)-1], 10, 64)
	if err != nil || version < 0 {
		response.BadRequest(c, "If-Match must be a quoted non-negative resource version")
		return false
	}
	c.Request = c.Request.WithContext(service.WithVideoAdminExpectedVersion(c.Request.Context(), c.Param("id"), version))
	return true
}

func (h *VideoHandler) requireController() videoAdminController {
	if h == nil || h.controller == nil {
		return unavailableVideoAdminController{}
	}
	return h.controller
}

type unavailableVideoAdminController struct{}

func (unavailableVideoAdminController) ListBillingReviews(context.Context, string) ([]*service.VideoBillingReview, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) DecideBillingReview(context.Context, string, int64, bool) (*service.VideoTask, error) {
	return nil, service.ErrVideoDisabled
}

func (unavailableVideoAdminController) ListTasks(context.Context, service.VideoAdminTaskFilter) (*service.VideoAdminTaskPage, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) GetTask(context.Context, string) (*service.VideoTask, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) ListResources(context.Context, service.VideoAdminResourceFilter) (*service.VideoAdminResourcePage, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) GetResource(context.Context, string) (*service.VideoResource, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) ListEvents(context.Context, string, int, int) (*service.VideoAdminEventPage, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) ListUnmatchedEvents(context.Context, int, int) (*service.VideoAdminEventPage, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) ListCallbacks(context.Context, service.VideoAdminCallbackFilter) (*service.VideoAdminCallbackPage, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) Overview(context.Context) (*service.VideoAdminOverview, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) ResolveNotCreated(context.Context, string) (*service.VideoTask, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) ResolveCreated(context.Context, string, string) (*service.VideoTask, error) {
	return nil, service.ErrVideoDisabled
}

func (unavailableVideoAdminController) ListSubmissionReviews(context.Context, string) ([]*service.VideoSubmissionReview, error) {
	return nil, service.ErrVideoInvalidRequest
}
func (unavailableVideoAdminController) DecideSubmissionReview(context.Context, string, int64, bool) (*service.VideoTask, error) {
	return nil, service.ErrVideoInvalidRequest
}
func (unavailableVideoAdminController) RetryCharacterResource(context.Context, string) (*service.VideoTask, error) {
	return nil, service.ErrVideoInvalidRequest
}
func (unavailableVideoAdminController) RetryProviderGet(context.Context, string) (*service.VideoTask, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) RetrySettlement(context.Context, string) (*service.VideoTask, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) ResolveBillingCapture(context.Context, string, float64) (*service.VideoTask, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) ResolveBillingRelease(context.Context, string) (*service.VideoTask, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) RetryDelete(context.Context, string) (*service.VideoTask, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) RetryCallback(context.Context, int64) (*service.VideoCallbackDelivery, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) GetCapabilityCatalog(context.Context) (*service.VideoCapabilityCatalogView, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) UpdateCapabilityCatalog(context.Context, service.VideoCapabilityCatalogDocument) (*service.VideoCapabilityCatalogView, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) GetAccountCapability(context.Context, int64) (*service.VideoAccountCapabilityStatus, error) {
	return nil, service.ErrVideoDisabled
}
func (unavailableVideoAdminController) ProbeAccountCapability(context.Context, int64) (*service.VideoAccountCapabilityStatus, error) {
	return nil, service.ErrVideoDisabled
}

func videoAdminRespond(c *gin.Context, value any, err error) {
	c.Header("Cache-Control", "no-store")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, value)
}

func videoAdminTaskFilter(c *gin.Context) (service.VideoAdminTaskFilter, error) {
	page, pageSize := response.ParsePagination(c)
	filter := service.VideoAdminTaskFilter{
		Page: page, PageSize: pageSize, Provider: strings.TrimSpace(c.Query("provider")), Operation: strings.TrimSpace(c.Query("operation")),
		GenerationState: strings.TrimSpace(c.Query("generation_state")), BillingState: strings.TrimSpace(c.Query("billing_state")),
		DeleteState: strings.TrimSpace(c.Query("delete_state")), Query: strings.TrimSpace(c.Query("q")),
	}
	var err error
	if filter.UserID, err = videoAdminOptionalID(c.Query("user_id")); err != nil {
		return filter, errors.New("invalid user_id")
	}
	if filter.GroupID, err = videoAdminOptionalID(c.Query("group_id")); err != nil {
		return filter, errors.New("invalid group_id")
	}
	if filter.AccountID, err = videoAdminOptionalID(c.Query("account_id")); err != nil {
		return filter, errors.New("invalid account_id")
	}
	if filter.CreatedAfter, err = videoAdminOptionalTime(c.Query("created_after")); err != nil {
		return filter, errors.New("invalid created_after")
	}
	if filter.CreatedBefore, err = videoAdminOptionalTime(c.Query("created_before")); err != nil {
		return filter, errors.New("invalid created_before")
	}
	return filter, nil
}

func videoAdminResourceFilter(c *gin.Context) (service.VideoAdminResourceFilter, error) {
	page, pageSize := response.ParsePagination(c)
	filter := service.VideoAdminResourceFilter{Page: page, PageSize: pageSize, Provider: strings.TrimSpace(c.Query("provider")), Status: strings.TrimSpace(c.Query("status")), Query: strings.TrimSpace(c.Query("q"))}
	var err error
	if filter.UserID, err = videoAdminOptionalID(c.Query("user_id")); err != nil {
		return filter, errors.New("invalid user_id")
	}
	if filter.AccountID, err = videoAdminOptionalID(c.Query("account_id")); err != nil {
		return filter, errors.New("invalid account_id")
	}
	return filter, nil
}

func videoAdminOptionalID(value string) (*int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return nil, errors.New("invalid id")
	}
	return &id, nil
}

func videoAdminRequiredID(value string) (int64, error) {
	id, err := videoAdminOptionalID(value)
	if err != nil || id == nil {
		return 0, errors.New("invalid id")
	}
	return *id, nil
}

func videoAdminOptionalTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func decodeStrictVideoAdminJSON(c *gin.Context, target any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, videoAdminMaxBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	if err := jsonstrict.RejectDuplicateKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func videoAdminEmptyBody(c *gin.Context) bool {
	if c.Request.ContentLength > 0 {
		return false
	}
	if c.Request.Body == nil {
		return true
	}
	var one [1]byte
	n, err := c.Request.Body.Read(one[:])
	return n == 0 && errors.Is(err, io.EOF)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
