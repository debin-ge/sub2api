package handler

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) grokVideoTrackingError(c *gin.Context, err error) {
	code := http.StatusServiceUnavailable
	kind := "grok_video_lookup_unavailable"
	message := "Video task service is temporarily unavailable"
	var upstream *service.GrokVideoHTTPError
	switch {
	case errors.As(err, &upstream):
		if upstream.RetryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(int(upstream.RetryAfter/time.Second)+1))
		}
		code = http.StatusBadGateway
		kind = "upstream_error"
		message = "Upstream video status is temporarily unavailable"
		if upstream.Status == 429 {
			code = 429
			kind = "rate_limit_error"
			c.Header("Retry-After", strconv.Itoa(int(upstream.RetryAfter/time.Second)+1))
		}
	case errors.Is(err, context.DeadlineExceeded):
		code = http.StatusGatewayTimeout
		kind = "upstream_timeout"
		message = "Video status lookup timed out"
	case errors.Is(err, service.ErrGrokVideoNotFound):
		code = 404
		kind = "not_found_error"
		message = "Video request not found"
	case errors.Is(err, service.ErrGrokVideoConflict):
		code = 409
		kind = "grok_video_conflict"
		message = "Video task identity or idempotency conflict"
	case errors.Is(err, service.ErrGrokVideoBusy):
		code = 409
		kind = "grok_video_in_progress"
		message = "Video submission is in progress or unconfirmed; do not resubmit with a new key"
	case errors.Is(err, service.ErrGrokVideoUnavailable):
		kind = "grok_video_account_unavailable"
		message = "The account for this video is currently unavailable"
	}
	if code == 503 {
		observability.GrokVideoEvent("owner_read_failed")
	}
	if code == 409 {
		observability.GrokVideoEvent("conflict")
	}
	if code == 503 {
		c.Header("Retry-After", "5")
	}
	h.errorResponse(c, code, kind, message)
}
func (h *OpenAIGatewayHandler) writeGrokVideoReplay(c *gin.Context, t *service.GrokVideoTask) {
	if t.CreateResponseEnc == "" {
		h.grokVideoTrackingError(c, service.ErrGrokVideoBusy)
		return
	}
	b, status, err := h.grokVideoTasks.CreateResponse(t)
	if err != nil {
		h.grokVideoTrackingError(c, err)
		return
	}
	c.Data(status, "application/json", b)
}
func (h *OpenAIGatewayHandler) handleTrackedGrokLookup(c *gin.Context, key *service.APIKey, e service.GrokMediaEndpoint, id string) bool {
	if h.grokVideoTasks == nil {
		return false
	}
	t, err := h.grokVideoTasks.Resolve(c.Request.Context(), key, id)
	if errors.Is(err, service.ErrGrokVideoNotFound) && !h.grokVideoTasks.Enabled() {
		return false
	}
	if err != nil {
		h.grokVideoTrackingError(c, err)
		return true
	}
	t, err = h.grokVideoTasks.Observe(c.Request.Context(), t, false)
	if err != nil {
		h.grokVideoTrackingError(c, err)
		return true
	}
	b, err := h.grokVideoTasks.CachedResponse(t)
	if err != nil {
		h.grokVideoTrackingError(c, err)
		return true
	}
	if e == service.GrokMediaEndpointVideoContent {
		if t.GenerationState != "completed" {
			h.errorResponse(c, http.StatusConflict, "grok_video_not_ready", "Video content is not ready")
			return true
		}
		account, err := h.grokVideoTasks.Account(c.Request.Context(), t)
		if err == nil {
			err = h.gatewayService.ForwardTrackedGrokVideoContent(c.Request.Context(), c, account, id, b)
		}
		if err != nil && !c.Writer.Written() {
			h.grokVideoTrackingError(c, err)
		}
	} else {
		h.gatewayService.WriteTrackedGrokVideoStatus(c, id, b)
	}
	return true
}
