package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/jsonstrict"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

type videoIngressRouter interface {
	ResolveVideoIngress(context.Context, *service.APIKey, string, string, string, string) (*service.VideoIngressRoute, error)
}

func (h *VideoHandler) PrepareCompositeVideoRoute(c *gin.Context) {
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.Group == nil || key.Group.Platform != service.PlatformComposite {
		videoError(c, service.ErrVideoInvalidRequest)
		c.Abort()
		return
	}
	operation, supported := service.ManagedVideoOperationForPath(c.Request.URL.Path)
	mediaType, _, mediaErr := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaErr != nil || !strings.EqualFold(mediaType, "application/json") {
		videoError(c, service.ErrVideoInvalidRequest)
		c.Abort()
		return
	}
	if h == nil {
		videoError(c, service.ErrBillingServiceUnavailable)
		c.Abort()
		return
	}
	router, configured := h.tasks.(videoIngressRouter)
	if c.Request.Method != http.MethodPost || !supported || !configured {
		videoError(c, service.ErrBillingServiceUnavailable)
		c.Abort()
		return
	}
	keyValue := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateVideoIdempotencyKey(keyValue); err != nil {
		videoError(c, err)
		c.Abort()
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, videoJSONBodyMaxBytes+1))
	if err != nil {
		videoError(c, service.ErrVideoInvalidRequest)
		c.Abort()
		return
	}
	if int64(len(body)) > videoJSONBodyMaxBytes {
		videoError(c, service.ErrVideoInputTooLarge)
		c.Abort()
		return
	}
	if err := jsonstrict.RejectDuplicateKeys(body); err != nil {
		videoError(c, service.ErrVideoInvalidRequest)
		c.Abort()
		return
	}
	var envelope struct {
		Model string          `json:"model"`
		Video json.RawMessage `json:"video"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		videoError(c, service.ErrVideoInvalidRequest)
		c.Abort()
		return
	}
	var source videoIDReference
	if len(envelope.Video) != 0 {
		_ = json.Unmarshal(envelope.Video, &source)
	}
	route, err := router.ResolveVideoIngress(c.Request.Context(), key, operation, strings.TrimSpace(envelope.Model), strings.TrimSpace(source.ID), keyValue)
	if err != nil || route == nil {
		if err == nil {
			err = service.ErrVideoNoAccountAvailable
		}
		videoError(c, err)
		c.Abort()
		return
	}
	if route.ManagedReplay || route.ResolveAfterParsing {
		c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformOpenAI))
	} else {
		c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), route.Decision))
	}
	if nativeVideoReleasePolicyApplies(c) {
		if err := service.ValidateVideoReleaseJSON(operation, body); err != nil {
			videoError(c, err)
			c.Abort()
			return
		}
	}
	if route.Decision.TargetPlatform == service.PlatformGrok {
		if err := checkGrokMediaCreationKey(key); err != nil {
			videoError(c, err)
			c.Abort()
			return
		}
	}
	if route.Decision.TargetPlatform == service.PlatformGrok && route.Decision.UpstreamModel != strings.TrimSpace(envelope.Model) {
		body, err = sjson.SetBytes(body, "model", route.Decision.UpstreamModel)
		if err != nil {
			videoError(c, service.ErrVideoInvalidRequest)
			c.Abort()
			return
		}
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	c.Request.Header.Set("Content-Type", "application/json")
}

func checkGrokMediaCreationKey(key *service.APIKey) error {
	if key == nil || key.User == nil || key.ID <= 0 || key.UserID != key.User.ID || !key.User.IsActive() {
		return service.ErrVideoInvalidRequest
	}
	if key.IsExpired() || key.Status == service.StatusAPIKeyExpired {
		return service.ErrAPIKeyExpired
	}
	if key.Status == service.StatusAPIKeyQuotaExhausted || key.IsQuotaExhausted() {
		return service.ErrAPIKeyQuotaExhausted
	}
	if key.Status != service.StatusActive {
		return service.ErrVideoInvalidRequest
	}
	return nil
}
