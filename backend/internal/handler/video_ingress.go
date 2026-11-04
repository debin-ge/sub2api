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
	bodyLimit := videoJSONBodyMaxBytes
	if service.IsArkVideoTasksPath(c.Request.URL.Path) || (h.cfg != nil && h.cfg.Gateway.Video.GrokManagedEnabled) {
		bodyLimit = h.arkJSONBodyLimit()
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, bodyLimit+1))
	if err != nil {
		videoError(c, service.ErrVideoInvalidRequest)
		c.Abort()
		return
	}
	if int64(len(body)) > bodyLimit {
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
	// A legacy Grok receipt is authoritative even if the composite model route
	// has since moved to another provider. Never create a second paid task.
	if keyValue != "" && h.openAI != nil && h.openAI.grokVideoTasks != nil {
		endpoint := service.GrokMediaEndpointVideosGenerations
		if operation == service.VideoOperationEdit {
			endpoint = service.GrokMediaEndpointVideosEdits
		}
		if operation == service.VideoOperationExtend {
			endpoint = service.GrokMediaEndpointVideosExtensions
		}
		if operation != service.VideoOperationCharacterCreate {
			legacy, lookupErr := h.openAI.grokVideoTasks.Replay(c.Request.Context(), key, endpoint, keyValue, body)
			if lookupErr != nil {
				h.openAI.grokVideoTrackingError(c, lookupErr)
				c.Abort()
				return
			}
			if legacy != nil {
				h.openAI.writeGrokVideoReplay(c, legacy)
				c.Abort()
				return
			}
		}
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
	// 两套公开形态各走各的路径：落到五家 Ark 形态平台的请求必须来自 Ark 路径，
	// 落到 OpenAI / Grok 的请求必须来自 /v1/videos。
	if err := validateVideoRoutePlatform(c.Request.URL.Path, route.Decision.TargetPlatform); err != nil {
		videoError(c, err)
		c.Abort()
		return
	}
	if route.ManagedReplay || route.ResolveAfterParsing {
		c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), route.Decision.TargetPlatform))
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
	if route.Decision.TargetPlatform == service.PlatformGrok && !route.ManagedReplay {
		if err := checkGrokMediaCreationKey(key); err != nil {
			videoError(c, err)
			c.Abort()
			return
		}
	}
	if route.Decision.TargetPlatform == service.PlatformGrok {
		c.Set("grok_video_original_body", append([]byte(nil), body...))
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

// validateVideoRoutePlatform 校验请求路径与目标平台的协议形态一致。
func validateVideoRoutePlatform(path, targetPlatform string) error {
	arkPath := service.IsArkVideoTasksPath(path)
	arkPlatform := service.IsArkVideoPlatform(targetPlatform)
	switch {
	case arkPath && !arkPlatform:
		return service.ErrVideoArkPlatformUnsupported
	case !arkPath && arkPlatform:
		return service.ErrVideoUseArkEndpoint
	default:
		return nil
	}
}
