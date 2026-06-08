package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type glmMessagesForwarder interface {
	ForwardMessages(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error)
	ForwardChatCompletions(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error)
}

type glmGatewayService interface {
	GenerateSessionHash(parsed *service.ParsedRequest) string
	SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, sub2apiUserID int64) (*service.AccountSelectionResult, error)
	RecordUsage(ctx context.Context, input *service.RecordUsageInput) error
}

type glmUpstreamErrorHandler interface {
	HandleGLMUpstreamError(ctx context.Context, account *service.Account, failoverErr *service.UpstreamFailoverError)
}

type noopGLMTempUnscheduler struct{}

func (noopGLMTempUnscheduler) TempUnscheduleRetryableError(context.Context, int64, *service.UpstreamFailoverError) {
}

type glmBillingEligibilityChecker interface {
	CheckBillingEligibility(ctx context.Context, user *service.User, apiKey *service.APIKey, group *service.Group, subscription *service.UserSubscription, quotaPlatform string) error
}

type glmConcurrencyController interface {
	IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error)
	DecrementWaitCount(ctx context.Context, userID int64)
	IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error)
	DecrementAccountWaitCount(ctx context.Context, accountID int64)
	AcquireUserSlotWithWait(c *gin.Context, userID int64, maxConcurrency int, isStream bool, streamStarted *bool) (func(), error)
	AcquireAccountSlotWithWaitTimeout(c *gin.Context, accountID int64, maxConcurrency int, timeout time.Duration, isStream bool, streamStarted *bool) (func(), error)
}

// GLMGatewayHandler handles GLM coding-plan gateway requests.
type GLMGatewayHandler struct {
	glmService            glmMessagesForwarder
	gatewayService        glmGatewayService
	billingCacheService   glmBillingEligibilityChecker
	apiKeyService         *service.APIKeyService
	usageRecordWorkerPool *service.UsageRecordWorkerPool
	concurrencyHelper     glmConcurrencyController
	chatConcurrencyHelper glmConcurrencyController
	maxAccountSwitches    int
}

func NewGLMGatewayHandler(
	glmService *service.GLMGatewayService,
	gatewayService *service.GatewayService,
	billingCacheService *service.BillingCacheService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	concurrencyService *service.ConcurrencyService,
	cfg *config.Config,
) *GLMGatewayHandler {
	pingInterval := time.Duration(0)
	maxAccountSwitches := 3
	if cfg != nil {
		pingInterval = time.Duration(cfg.Concurrency.PingInterval) * time.Second
		if cfg.Gateway.MaxAccountSwitches > 0 {
			maxAccountSwitches = cfg.Gateway.MaxAccountSwitches
		}
	}
	return &GLMGatewayHandler{
		glmService:            glmService,
		gatewayService:        gatewayService,
		billingCacheService:   billingCacheService,
		apiKeyService:         apiKeyService,
		usageRecordWorkerPool: usageRecordWorkerPool,
		concurrencyHelper:     NewConcurrencyHelper(concurrencyService, SSEPingFormatClaude, pingInterval),
		chatConcurrencyHelper: NewConcurrencyHelper(concurrencyService, SSEPingFormatComment, pingInterval),
		maxAccountSwitches:    maxAccountSwitches,
	}
}

// Messages handles GLM Coding Plan POST /v1/messages requests.
func (h *GLMGatewayHandler) Messages(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if apiKey.Group == nil || apiKey.Group.Platform != service.PlatformGLM {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "GLM gateway requires a GLM group")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	setOpsRequestContext(c, "", false)

	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, err := service.ParseGatewayRequest(bodyRef, service.PlatformAnthropic)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	reqModel := strings.TrimSpace(parsedReq.Model)
	if reqModel == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	parsedReq.Model = reqModel
	parsedReq.GroupID = apiKey.GroupID
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	setOpsRequestContext(c, reqModel, parsedReq.Stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(parsedReq.Stream, false)))

	if h.gatewayService == nil || h.glmService == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "glm gateway service unavailable")
		return
	}
	if h.concurrencyHelper == nil || h.billingCacheService == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "glm gateway service unavailable")
		return
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	streamStarted := false
	maxWait := service.CalculateMaxWait(subject.Concurrency)
	canWait, err := h.concurrencyHelper.IncrementWaitCount(c.Request.Context(), subject.UserID, maxWait)
	waitCounted := false
	if err == nil && !canWait {
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later")
		return
	}
	if err == nil && canWait {
		waitCounted = true
	}
	defer func() {
		if waitCounted {
			h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		}
	}()

	userRelease, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, parsedReq.Stream, &streamStarted)
	if err != nil {
		if !streamStarted {
			h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later")
		}
		return
	}
	if waitCounted {
		h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		waitCounted = false
	}
	if userRelease != nil {
		defer userRelease()
	}

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		if !streamStarted {
			h.errorResponse(c, status, code, message)
		}
		return
	}

	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)
	fs := NewFailoverState(h.maxAccountSwitches, false)

	for {
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, sessionHash, reqModel, fs.FailedAccountIDs, parsedReq.MetadataUserID, subject.UserID)
		if err != nil || selection == nil || selection.Account == nil {
			if fs.LastFailoverErr != nil && !streamStarted {
				status, errType, message := glmForwardErrorDetails(fs.LastFailoverErr)
				h.errorResponse(c, status, errType, message)
				return
			}
			h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Service temporarily unavailable", streamStarted)
			return
		}
		release, acquired := h.acquireGLMAccountSlot(c, selection, parsedReq.Stream, &streamStarted, h.concurrencyHelper)
		if !acquired {
			return
		}
		releaseOnce := onceRelease(release)
		defer releaseOnce()

		account := selection.Account
		if !account.IsGLMCodingPlan() {
			h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available GLM coding-plan accounts", streamStarted)
			return
		}
		setOpsSelectedAccount(c, account.ID, account.Platform)

		result, err := h.glmService.ForwardMessages(c.Request.Context(), c, account, body, glmRequestID(c))
		releaseOnce()
		if err != nil {
			if c.Writer.Written() || streamStarted {
				return
			}
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				mapping := service.MapGLMUpstreamStatus(failoverErr.StatusCode)
				if shouldHandleGLMUpstreamDegradation(failoverErr, mapping) {
					h.handleGLMUpstreamDegradation(c.Request.Context(), account, failoverErr)
				}
				if mapping.Retryable {
					failoverAction := fs.HandleFailoverError(c.Request.Context(), h.glmTempUnscheduler(), account.ID, account.Platform, failoverErr)
					switch failoverAction {
					case FailoverContinue:
						continue
					case FailoverCanceled:
						return
					case FailoverExhausted:
						status, errType, message := glmForwardErrorDetails(failoverErr)
						h.errorResponse(c, status, errType, message)
						return
					}
				}
				status, errType, message := glmForwardErrorDetails(failoverErr)
				h.errorResponse(c, status, errType, message)
				return
			}
			status, errType, message := glmForwardErrorDetails(err)
			h.errorResponse(c, status, errType, message)
			return
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)

		h.submitUsageRecordTask(func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
				Result:             result,
				QuotaPlatform:      quotaPlatform,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      h.apiKeyService,
			}); err != nil {
				logger.L().With(
					zap.String("component", "handler.glm_gateway.messages"),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.String("model", reqModel),
					zap.Int64("account_id", account.ID),
				).Error("glm_gateway.record_usage_failed", zap.Error(err))
			}
		})
		return
	}
}

// ChatCompletions handles GLM OpenAI-compatible POST /v1/chat/completions requests.
func (h *GLMGatewayHandler) ChatCompletions(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if apiKey.Group == nil || apiKey.Group.Platform != service.PlatformGLM {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "GLM gateway requires a GLM group")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	setOpsRequestContext(c, "", false)

	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, err := service.ParseGatewayRequest(bodyRef, "chat_completions")
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	reqModel := strings.TrimSpace(parsedReq.Model)
	if reqModel == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	parsedReq.Model = reqModel
	parsedReq.GroupID = apiKey.GroupID
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	setOpsRequestContext(c, reqModel, parsedReq.Stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(parsedReq.Stream, false)))

	if h.gatewayService == nil || h.glmService == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "glm gateway service unavailable")
		return
	}
	concurrencyHelper := h.chatConcurrencyController()
	if concurrencyHelper == nil || h.billingCacheService == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "glm gateway service unavailable")
		return
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	streamStarted := false
	maxWait := service.CalculateMaxWait(subject.Concurrency)
	canWait, err := concurrencyHelper.IncrementWaitCount(c.Request.Context(), subject.UserID, maxWait)
	waitCounted := false
	if err == nil && !canWait {
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later")
		return
	}
	if err == nil && canWait {
		waitCounted = true
	}
	defer func() {
		if waitCounted {
			concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		}
	}()

	userRelease, err := concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, parsedReq.Stream, &streamStarted)
	if err != nil {
		if !streamStarted {
			h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later")
		}
		return
	}
	if waitCounted {
		concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		waitCounted = false
	}
	if userRelease != nil {
		defer userRelease()
	}

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		if !streamStarted {
			h.errorResponse(c, status, code, message)
		}
		return
	}

	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)
	fs := NewFailoverState(h.maxAccountSwitches, false)

	for {
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, sessionHash, reqModel, fs.FailedAccountIDs, parsedReq.MetadataUserID, subject.UserID)
		if err != nil || selection == nil || selection.Account == nil {
			if fs.LastFailoverErr != nil && !streamStarted {
				status, errType, message := glmForwardErrorDetails(fs.LastFailoverErr)
				h.errorResponse(c, status, errType, message)
				return
			}
			h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Service temporarily unavailable", streamStarted)
			return
		}
		release, acquired := h.acquireGLMAccountSlot(c, selection, parsedReq.Stream, &streamStarted, concurrencyHelper)
		if !acquired {
			return
		}
		releaseOnce := onceRelease(release)
		defer releaseOnce()

		account := selection.Account
		if !account.IsGLMCodingPlan() {
			h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available GLM coding-plan accounts", streamStarted)
			return
		}
		setOpsSelectedAccount(c, account.ID, account.Platform)

		result, err := h.glmService.ForwardChatCompletions(c.Request.Context(), c, account, body, glmRequestID(c))
		releaseOnce()
		if err != nil {
			if c.Writer.Written() || streamStarted {
				return
			}
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				mapping := service.MapGLMUpstreamStatus(failoverErr.StatusCode)
				if shouldHandleGLMUpstreamDegradation(failoverErr, mapping) {
					h.handleGLMUpstreamDegradation(c.Request.Context(), account, failoverErr)
				}
				if mapping.Retryable {
					failoverAction := fs.HandleFailoverError(c.Request.Context(), h.glmTempUnscheduler(), account.ID, account.Platform, failoverErr)
					switch failoverAction {
					case FailoverContinue:
						continue
					case FailoverCanceled:
						return
					case FailoverExhausted:
						status, errType, message := glmForwardErrorDetails(failoverErr)
						h.errorResponse(c, status, errType, message)
						return
					}
				}
				status, errType, message := glmForwardErrorDetails(failoverErr)
				h.errorResponse(c, status, errType, message)
				return
			}
			status, errType, message := glmForwardErrorDetails(err)
			h.errorResponse(c, status, errType, message)
			return
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)

		h.submitUsageRecordTask(func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
				Result:             result,
				QuotaPlatform:      quotaPlatform,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      h.apiKeyService,
			}); err != nil {
				logger.L().With(
					zap.String("component", "handler.glm_gateway.chat_completions"),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.String("model", reqModel),
					zap.Int64("account_id", account.ID),
				).Error("glm_gateway.record_usage_failed", zap.Error(err))
			}
		})
		return
	}
}

func (h *GLMGatewayHandler) Unsupported(c *gin.Context) {
	h.errorResponse(c, http.StatusNotFound, "not_found_error", "GLM gateway supports /v1/messages and /v1/chat/completions only")
}

func (h *GLMGatewayHandler) streamingAwareError(c *gin.Context, status int, errType, message string, streamStarted bool) {
	if streamStarted {
		return
	}
	h.errorResponse(c, status, errType, message)
}

func (h *GLMGatewayHandler) acquireGLMAccountSlot(c *gin.Context, selection *service.AccountSelectionResult, isStream bool, streamStarted *bool, concurrencyHelper glmConcurrencyController) (func(), bool) {
	if selection == nil || selection.Account == nil {
		h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available GLM coding-plan accounts", derefBool(streamStarted))
		return nil, false
	}
	account := selection.Account
	if selection.Acquired {
		return selection.ReleaseFunc, true
	}
	if selection.WaitPlan == nil || concurrencyHelper == nil {
		h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available GLM coding-plan accounts", derefBool(streamStarted))
		return nil, false
	}

	accountWaitCounted := false
	canWait, err := concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
	if err != nil {
		logger.L().Warn("glm_gateway.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
	} else if !canWait {
		h.streamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later", derefBool(streamStarted))
		return nil, false
	}
	if err == nil && canWait {
		accountWaitCounted = true
	}
	releaseAccountWait := func() {
		if accountWaitCounted {
			concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
			accountWaitCounted = false
		}
	}

	release, err := concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
		c,
		account.ID,
		selection.WaitPlan.MaxConcurrency,
		selection.WaitPlan.Timeout,
		isStream,
		streamStarted,
	)
	if err != nil {
		releaseAccountWait()
		h.streamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later", derefBool(streamStarted))
		return nil, false
	}
	releaseAccountWait()
	return release, true
}

func (h *GLMGatewayHandler) chatConcurrencyController() glmConcurrencyController {
	if h != nil && h.chatConcurrencyHelper != nil {
		return h.chatConcurrencyHelper
	}
	if h == nil {
		return nil
	}
	return h.concurrencyHelper
}

func onceRelease(release func()) func() {
	if release == nil {
		return func() {}
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		release()
	}
}

func (h *GLMGatewayHandler) handleGLMUpstreamDegradation(ctx context.Context, account *service.Account, failoverErr *service.UpstreamFailoverError) {
	if h == nil || h.gatewayService == nil || account == nil || failoverErr == nil {
		return
	}
	degrader, ok := h.gatewayService.(glmUpstreamErrorHandler)
	if !ok {
		return
	}
	degrader.HandleGLMUpstreamError(ctx, account, failoverErr)
}

func shouldHandleGLMUpstreamDegradation(failoverErr *service.UpstreamFailoverError, mapping service.GLMUpstreamStatusMapping) bool {
	if failoverErr == nil {
		return false
	}
	return mapping.Retryable ||
		failoverErr.StatusCode == http.StatusUnauthorized ||
		failoverErr.StatusCode == http.StatusForbidden
}

func (h *GLMGatewayHandler) glmTempUnscheduler() TempUnscheduler {
	if h == nil || h.gatewayService == nil {
		return noopGLMTempUnscheduler{}
	}
	if unscheduler, ok := h.gatewayService.(TempUnscheduler); ok {
		return unscheduler
	}
	return noopGLMTempUnscheduler{}
}

func (h *GLMGatewayHandler) errorResponse(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

func (h *GLMGatewayHandler) submitUsageRecordTask(task service.UsageRecordTask) {
	if task == nil {
		return
	}
	if h.usageRecordWorkerPool != nil {
		h.usageRecordWorkerPool.Submit(task)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.L().With(
				zap.String("component", "handler.glm_gateway.messages"),
				zap.Any("panic", recovered),
			).Error("glm_gateway.usage_record_task_panic_recovered")
		}
	}()
	task(ctx)
}

func glmRequestID(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if id, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	if id, _ := c.Request.Context().Value(ctxkey.RequestID).(string); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	return strings.TrimSpace(c.GetHeader("X-Request-Id"))
}

func glmForwardErrorDetails(err error) (int, string, string) {
	var failoverErr *service.UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		mapping := service.MapGLMUpstreamStatus(failoverErr.StatusCode)
		return mapping.ClientStatus, mapping.ErrorType, glmMappedUpstreamErrorMessage(mapping)
	}
	var unsupported *service.GLMUnsupportedContentError
	if errors.As(err, &unsupported) {
		return http.StatusBadRequest, "invalid_request_error", unsupported.Error()
	}
	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "rate limit"):
		return http.StatusTooManyRequests, "rate_limit_error", "GLM upstream rate limit exceeded"
	case strings.Contains(lower, "parse glm messages request"), strings.Contains(lower, "parse glm chat completions request"), strings.Contains(lower, "model is required"), strings.Contains(lower, "invalid"):
		return http.StatusBadRequest, "invalid_request_error", message
	default:
		if message == "" {
			message = "Upstream request failed"
		}
		return http.StatusBadGateway, "api_error", fmt.Sprintf("GLM upstream request failed: %s", message)
	}
}

func glmMappedUpstreamErrorMessage(mapping service.GLMUpstreamStatusMapping) string {
	switch mapping.ErrorType {
	case "upstream_auth_error":
		return "GLM upstream authentication failed, please contact administrator"
	case "rate_limit_error":
		return "GLM upstream rate limit exceeded, please retry later"
	case "overloaded_error":
		return "GLM upstream service overloaded, please retry later"
	case "server_error":
		return "GLM upstream service temporarily unavailable"
	default:
		return "GLM upstream rejected request"
	}
}
