package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

type grokManagedReplayAPI interface {
	FindGrokManagedReplay(context.Context, *service.APIKey, string, string, string) (*service.VideoTask, error)
}

type grokManagedPreparationAPI interface {
	PrepareGrokSubmission(context.Context, *service.APIKey, string, string) (context.Context, error)
}
type grokManagedReceiptAPI interface {
	FindGrokManagedReceipt(context.Context, *service.APIKey, string, string) (*service.VideoTask, error)
}

func (h *VideoHandler) tryGrokManagedCreate(c *gin.Context, operation string) bool {
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil {
		return false
	}
	enabled := h.cfg != nil && h.cfg.Gateway.Video.GrokManagedEnabled
	clientKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if !enabled && clientKey == "" {
		return false
	}
	contentType := c.GetHeader("Content-Type")
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil || (media != "application/json" && media != "multipart/form-data") {
		if !enabled {
			return false
		}
		videoError(c, service.ErrVideoInvalidRequest)
		return true
	}
	limit := int64(service.VideoCreateJSONMaxBytes)
	if media == "multipart/form-data" {
		limit = service.VideoCreateMultipartMaxBytes
	}
	wireRaw, err := io.ReadAll(io.LimitReader(c.Request.Body, limit+1))
	if err != nil || int64(len(wireRaw)) > limit {
		videoError(c, service.ErrVideoInputTooLarge)
		return true
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(wireRaw))
	c.Request.ContentLength = int64(len(wireRaw))
	if err = validateVideoIdempotencyKey(clientKey); err != nil {
		videoError(c, err)
		return true
	}
	if h.openAI != nil && h.openAI.grokVideoTasks != nil && clientKey != "" {
		endpoint := service.GrokMediaEndpointVideosGenerations
		if operation == service.VideoOperationEdit {
			endpoint = service.GrokMediaEndpointVideosEdits
		}
		if operation == service.VideoOperationExtend {
			endpoint = service.GrokMediaEndpointVideosExtensions
		}
		legacy, e := h.openAI.grokVideoTasks.Replay(c.Request.Context(), key, endpoint, clientKey, wireRaw)
		if e != nil {
			h.openAI.grokVideoTrackingError(c, e)
			return true
		}
		if legacy != nil {
			h.openAI.writeGrokVideoReplay(c, legacy)
			return true
		}
	}
	internalKey := service.GrokManagedIdempotencyKey(key, operation, clientKey)
	replay, available := h.tasks.(grokManagedReplayAPI)
	if !available {
		if !enabled {
			return false
		}
		videoError(c, service.ErrBillingServiceUnavailable)
		return true
	}
	// Disabled new creation must not consume/rewrite an unrelated legacy form.
	if media == "multipart/form-data" && !enabled {
		receipts, ok := h.tasks.(grokManagedReceiptAPI)
		if !ok {
			return false
		}
		existing, e := receipts.FindGrokManagedReceipt(c.Request.Context(), key, operation, internalKey)
		if e != nil {
			videoError(c, e)
			return true
		}
		if existing == nil {
			return false
		}
	}
	raw := wireRaw
	clientHash := ""
	contract := service.VideoCreateIntentJSONContract
	var uploads *grokManagedUploads
	if media == "multipart/form-data" {
		var reuse *service.VideoSpoolSession
		if value, ok := c.Get("grok_video_spool_session"); ok {
			reuse, _ = value.(*service.VideoSpoolSession)
		}
		raw, uploads, err = h.parseGrokManagedMultipart(c.Request.Context(), key.UserID, operation, contentType, wireRaw, reuse)
		if uploads != nil && uploads.session != nil {
			defer func() { _ = uploads.session.Close() }()
		}
		if err != nil {
			videoError(c, err)
			return true
		}
		clientHash = uploads.hash
		contract = service.VideoCreateIntentMultipartContract
		if original, exists := c.Get("grok_video_multipart_hash"); exists {
			if value, ok := original.(string); ok {
				clientHash = value
			}
		}
		if original, exists := c.Get("grok_video_original_model"); exists {
			if model, ok := original.(string); ok {
				raw, err = sjson.SetBytes(raw, "model", model)
				if err != nil {
					videoError(c, err)
					return true
				}
			}
		}
	} else {
		if original, exists := c.Get("grok_video_original_body"); exists {
			if value, ok := original.([]byte); ok {
				raw = value
			}
		}
		clientHash, err = service.CanonicalVideoCreateRequestHash(raw)
		if err != nil {
			videoError(c, err)
			return true
		}
	}
	task, err := replay.FindGrokManagedReplay(c.Request.Context(), key, operation, internalKey, clientHash)
	if err != nil {
		videoError(c, err)
		return true
	}
	if task != nil {
		h.setNoStore(c)
		c.Header("X-Video-Idempotency-Replayed", "true")
		c.JSON(200, gin.H{"request_id": grokManagedPublicID(task.PublicID)})
		return true
	}
	if !enabled {
		return false
	}
	if !h.readEnabled(c) {
		return true
	}
	if !service.GroupAllowsImageGeneration(key.Group) {
		videoJSONError(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return true
	}
	run := func() {
		preparation, available := h.tasks.(grokManagedPreparationAPI)
		if !available {
			videoError(c, service.ErrBillingServiceUnavailable)
			return
		}
		preparedCtx, prepareErr := preparation.PrepareGrokSubmission(c.Request.Context(), key, operation, internalKey)
		if prepareErr != nil {
			videoError(c, prepareErr)
			return
		}
		c.Request = c.Request.WithContext(preparedCtx)
		request, session, e := h.parseGrokManagedRequest(c.Request.Context(), key, operation, raw, nil, uploads)
		if session != nil {
			defer func() { _ = session.Close() }()
		}
		if e != nil {
			videoError(c, e)
			return
		}
		request.IdempotencyKey = internalKey
		request.Grok.ClientRequestHash = clientHash
		setOpsRequestContext(c, request.Model, false)
		if !h.checkSecurityAudit(c, key, request) {
			return
		}
		result, e := h.tasks.Submit(c.Request.Context(), request)
		if e != nil {
			videoError(c, e)
			return
		}
		if result == nil || result.Task == nil {
			videoError(c, service.ErrBillingServiceUnavailable)
			return
		}
		h.setNoStore(c)
		c.JSON(http.StatusOK, gin.H{"request_id": grokManagedPublicID(result.Task.PublicID)})
	}
	if internalKey == "" {
		run()
		return true
	}
	api, available := h.tasks.(videoCreateIntentHashAPI)
	if !available {
		videoError(c, service.ErrBillingServiceUnavailable)
		return true
	}
	session, err := api.BeginVideoCreateIntentWithHash(c.Request.Context(), key, operation, internalKey, clientHash, contract)
	if err != nil {
		if errors.Is(err, service.ErrVideoCreateInProgress) {
			c.Header("Retry-After", "3")
		}
		videoError(c, err)
		return true
	}
	h.executeVideoCreateIntentSession(c, session, run, func() {
		t, e := replay.FindGrokManagedReplay(c.Request.Context(), key, operation, internalKey, clientHash)
		if e != nil {
			videoError(c, e)
			return
		}
		if t == nil {
			videoError(c, service.ErrVideoCreateOutcomeUnknown)
			return
		}
		c.JSON(200, gin.H{"request_id": grokManagedPublicID(t.PublicID)})
	})
	return true
}
func (h *VideoHandler) tryGrokManagedLookup(c *gin.Context, content bool) bool {
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || h.tasks == nil {
		return false
	}
	internal, err := grokManagedInternalID(c.Param("request_id"))
	if err != nil {
		return false
	}
	task, err := h.tasks.GetContentTaskForOwner(c.Request.Context(), key.UserID, internal)
	if errors.Is(err, service.ErrVideoTaskNotFound) {
		return false
	}
	if err != nil {
		videoError(c, err)
		return true
	}
	if task.Provider != service.VideoProviderGrok || task.APIKeyID == nil || *task.APIKeyID != key.ID || task.GroupID == nil || key.GroupID == nil || *task.GroupID != *key.GroupID {
		videoError(c, service.ErrVideoTaskNotFound)
		return true
	}
	if !h.readEnabled(c) {
		return true
	}
	if content {
		if task.GenerationState != service.VideoGenerationCompleted {
			videoError(c, service.ErrVideoContentNotReady)
			return true
		}
		if task.BillingState != service.VideoBillingCaptured {
			videoError(c, service.ErrVideoSettlementPending)
			return true
		}
		if h.cfg == nil || !h.cfg.Gateway.Video.ContentProxy.Enabled {
			videoError(c, service.ErrVideoDisabled)
			return true
		}
		h.serveVideoContent(c, task, "video", false, func(ctx context.Context, r service.ProviderContentRequest) (*service.ProviderContent, error) {
			return h.tasks.OpenContentForTask(ctx, task, r)
		})
		return true
	}
	body := gin.H{"request_id": grokManagedPublicID(task.PublicID), "model": task.PublicModel, "status": "pending"}
	if task.Progress != nil {
		body["progress"] = *task.Progress
	}
	switch service.ProjectVideoStatus(task) {
	case service.VideoGenerationCompleted:
		body["status"] = "done"
		body["progress"] = 100
		video := gin.H{}
		if moderation, ok := task.ResponseMetadata["respect_moderation"].(bool); ok {
			video["respect_moderation"] = moderation
		}
		if usage := service.GrokManagedPublicUsage(task.UsageSnapshot); len(usage) > 0 {
			body["usage"] = usage
		}
		if duration, ok := task.ResponseMetadata["seconds"]; ok {
			video["duration"] = duration
		}
		if h.cfg.Gateway.Video.ContentProxy.Enabled {
			if url := h.signedVideoContentURL(task, c.Request, time.Now()); url != "" {
				video["url"] = url
			}
		}
		body["video"] = video
	case service.VideoGenerationFailed, service.VideoGenerationCancelled:
		body["status"] = "failed"
	case service.VideoGenerationExpired:
		body["status"] = "expired"
	default:
		if task.GenerationState == service.VideoGenerationCompleted {
			body["progress"] = 99
		}
		c.Header("Retry-After", "5")
	}
	h.setNoStore(c)
	// Match the other video status endpoints: preserve copyable signed URLs.
	c.PureJSON(http.StatusOK, body)
	return true
}

func allowGrokManagedTaskOwner(key *service.APIKey, task *service.VideoTask) bool {
	if task == nil {
		return false
	}
	if task.Provider != service.VideoProviderGrok {
		return true
	}
	return key != nil && task.UserID == key.UserID && task.APIKeyID != nil && *task.APIKeyID == key.ID && task.GroupID != nil && key.GroupID != nil && *task.GroupID == *key.GroupID
}
