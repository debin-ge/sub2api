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

type deepseekMessagesForwarder interface {
	ForwardMessages(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error)
	ForwardChatCompletions(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error)
}

type deepseekGatewayService interface {
	GenerateSessionHash(parsed *service.ParsedRequest) string
	SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, sub2apiUserID int64) (*service.AccountSelectionResult, error)
	RecordUsage(ctx context.Context, input *service.RecordUsageInput) error
}

type deepseekUpstreamErrorHandler interface {
	HandleDeepSeekUpstreamError(ctx context.Context, account *service.Account, failoverErr *service.UpstreamFailoverError)
}

type noopDeepSeekTempUnscheduler struct{}

func (noopDeepSeekTempUnscheduler) TempUnscheduleRetryableError(context.Context, int64, *service.UpstreamFailoverError) {
}

type deepseekBillingEligibilityChecker interface {
	CheckBillingEligibility(ctx context.Context, user *service.User, apiKey *service.APIKey, group *service.Group, subscription *service.UserSubscription, quotaPlatform string) error
}

type deepseekConcurrencyController interface {
	IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error)
	DecrementWaitCount(ctx context.Context, userID int64)
	IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error)
	DecrementAccountWaitCount(ctx context.Context, accountID int64)
	AcquireUserSlotWithWait(c *gin.Context, userID int64, maxConcurrency int, isStream bool, streamStarted *bool) (func(), error)
	AcquireAccountSlotWithWaitTimeout(c *gin.Context, accountID int64, maxConcurrency int, timeout time.Duration, isStream bool, streamStarted *bool) (func(), error)
}

// DeepSeekGatewayHandler handles DeepSeek API key gateway requests.
type DeepSeekGatewayHandler struct {
	deepseekService       deepseekMessagesForwarder
	gatewayService        deepseekGatewayService
	billingCacheService   deepseekBillingEligibilityChecker
	apiKeyService         *service.APIKeyService
	usageRecordWorkerPool *service.UsageRecordWorkerPool
	concurrencyHelper     deepseekConcurrencyController
	chatConcurrencyHelper deepseekConcurrencyController
	maxAccountSwitches    int
}

func NewDeepSeekGatewayHandler(
	deepseekService *service.DeepSeekGatewayService,
	gatewayService *service.GatewayService,
	billingCacheService *service.BillingCacheService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	concurrencyService *service.ConcurrencyService,
	cfg *config.Config,
) *DeepSeekGatewayHandler {
	pingInterval := time.Duration(0)
	maxAccountSwitches := 3
	if cfg != nil {
		pingInterval = time.Duration(cfg.Concurrency.PingInterval) * time.Second
		if cfg.Gateway.MaxAccountSwitches > 0 {
			maxAccountSwitches = cfg.Gateway.MaxAccountSwitches
		}
	}
	return &DeepSeekGatewayHandler{
		deepseekService:       deepseekService,
		gatewayService:        gatewayService,
		billingCacheService:   billingCacheService,
		apiKeyService:         apiKeyService,
		usageRecordWorkerPool: usageRecordWorkerPool,
		concurrencyHelper:     NewConcurrencyHelper(concurrencyService, SSEPingFormatClaude, pingInterval),
		chatConcurrencyHelper: NewConcurrencyHelper(concurrencyService, SSEPingFormatComment, pingInterval),
		maxAccountSwitches:    maxAccountSwitches,
	}
}

func isDeepSeekGatewayModel(model string) bool {
	for _, defaultModel := range service.DefaultDeepSeekModelIDs() {
		if model == defaultModel {
			return true
		}
	}
	return false
}

// Messages handles DeepSeek API Key POST /v1/messages requests.
func (h *DeepSeekGatewayHandler) Messages(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if apiKey.Group == nil || apiKey.Group.Platform != service.PlatformDeepSeek {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "DeepSeek gateway requires a DeepSeek group")
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
	reqModel := parsedReq.Model
	if reqModel == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if !isDeepSeekGatewayModel(reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "DeepSeek gateway only supports deepseek-v4-flash and deepseek-v4-pro")
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

	if h.gatewayService == nil || h.deepseekService == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "deepseek gateway service unavailable")
		return
	}
	if h.concurrencyHelper == nil || h.billingCacheService == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "deepseek gateway service unavailable")
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
				status, errType, message := deepseekForwardErrorDetails(fs.LastFailoverErr)
				h.errorResponse(c, status, errType, message)
				return
			}
			h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Service temporarily unavailable", streamStarted)
			return
		}
		release, acquired := h.acquireDeepSeekAccountSlot(c, selection, parsedReq.Stream, &streamStarted, h.concurrencyHelper)
		if !acquired {
			return
		}
		releaseOnce := onceRelease(release)
		defer releaseOnce()

		account := selection.Account
		if !account.IsDeepSeekAPIKey() {
			h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available DeepSeek API key accounts", streamStarted)
			return
		}
		setOpsSelectedAccount(c, account.ID, account.Platform)

		result, err := h.deepseekService.ForwardMessages(c.Request.Context(), c, account, body, deepseekRequestID(c))
		releaseOnce()
		if err != nil {
			if c.Writer.Written() || streamStarted {
				return
			}
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				mapping := service.MapDeepSeekUpstreamStatus(failoverErr.StatusCode)
				if shouldHandleDeepSeekUpstreamDegradation(failoverErr, mapping) {
					h.handleDeepSeekUpstreamDegradation(c.Request.Context(), account, failoverErr)
				}
				if mapping.Retryable {
					failoverAction := fs.HandleFailoverError(c.Request.Context(), h.deepseekTempUnscheduler(), account.ID, account.Platform, failoverErr)
					switch failoverAction {
					case FailoverContinue:
						continue
					case FailoverCanceled:
						return
					case FailoverExhausted:
						status, errType, message := deepseekForwardErrorDetails(failoverErr)
						h.errorResponse(c, status, errType, message)
						return
					}
				}
				status, errType, message := deepseekForwardErrorDetails(failoverErr)
				h.errorResponse(c, status, errType, message)
				return
			}
			status, errType, message := deepseekForwardErrorDetails(err)
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
					zap.String("component", "handler.deepseek_gateway.messages"),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.String("model", reqModel),
					zap.Int64("account_id", account.ID),
				).Error("deepseek_gateway.record_usage_failed", zap.Error(err))
			}
		})
		return
	}
}

// ChatCompletions handles DeepSeek OpenAI-compatible POST /v1/chat/completions requests.
func (h *DeepSeekGatewayHandler) ChatCompletions(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if apiKey.Group == nil || apiKey.Group.Platform != service.PlatformDeepSeek {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "DeepSeek gateway requires a DeepSeek group")
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
	reqModel := parsedReq.Model
	if reqModel == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if !isDeepSeekGatewayModel(reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "DeepSeek gateway only supports deepseek-v4-flash and deepseek-v4-pro")
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

	if h.gatewayService == nil || h.deepseekService == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "deepseek gateway service unavailable")
		return
	}
	concurrencyHelper := h.chatConcurrencyController()
	if concurrencyHelper == nil || h.billingCacheService == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "deepseek gateway service unavailable")
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
				status, errType, message := deepseekForwardErrorDetails(fs.LastFailoverErr)
				h.errorResponse(c, status, errType, message)
				return
			}
			h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Service temporarily unavailable", streamStarted)
			return
		}
		release, acquired := h.acquireDeepSeekAccountSlot(c, selection, parsedReq.Stream, &streamStarted, concurrencyHelper)
		if !acquired {
			return
		}
		releaseOnce := onceRelease(release)
		defer releaseOnce()

		account := selection.Account
		if !account.IsDeepSeekAPIKey() {
			h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available DeepSeek API key accounts", streamStarted)
			return
		}
		setOpsSelectedAccount(c, account.ID, account.Platform)

		result, err := h.deepseekService.ForwardChatCompletions(c.Request.Context(), c, account, body, deepseekRequestID(c))
		releaseOnce()
		if err != nil {
			if c.Writer.Written() || streamStarted {
				return
			}
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				mapping := service.MapDeepSeekUpstreamStatus(failoverErr.StatusCode)
				if shouldHandleDeepSeekUpstreamDegradation(failoverErr, mapping) {
					h.handleDeepSeekUpstreamDegradation(c.Request.Context(), account, failoverErr)
				}
				if mapping.Retryable {
					failoverAction := fs.HandleFailoverError(c.Request.Context(), h.deepseekTempUnscheduler(), account.ID, account.Platform, failoverErr)
					switch failoverAction {
					case FailoverContinue:
						continue
					case FailoverCanceled:
						return
					case FailoverExhausted:
						status, errType, message := deepseekForwardErrorDetails(failoverErr)
						h.errorResponse(c, status, errType, message)
						return
					}
				}
				status, errType, message := deepseekForwardErrorDetails(failoverErr)
				h.errorResponse(c, status, errType, message)
				return
			}
			status, errType, message := deepseekForwardErrorDetails(err)
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
					zap.String("component", "handler.deepseek_gateway.chat_completions"),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.String("model", reqModel),
					zap.Int64("account_id", account.ID),
				).Error("deepseek_gateway.record_usage_failed", zap.Error(err))
			}
		})
		return
	}
}

func (h *DeepSeekGatewayHandler) Unsupported(c *gin.Context) {
	h.errorResponse(c, http.StatusNotFound, "not_found_error", "DeepSeek gateway supports /v1/messages and /v1/chat/completions only")
}

func (h *DeepSeekGatewayHandler) streamingAwareError(c *gin.Context, status int, errType, message string, streamStarted bool) {
	if streamStarted {
		return
	}
	h.errorResponse(c, status, errType, message)
}

func (h *DeepSeekGatewayHandler) acquireDeepSeekAccountSlot(c *gin.Context, selection *service.AccountSelectionResult, isStream bool, streamStarted *bool, concurrencyHelper deepseekConcurrencyController) (func(), bool) {
	if selection == nil || selection.Account == nil {
		h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available DeepSeek API key accounts", derefBool(streamStarted))
		return nil, false
	}
	account := selection.Account
	if selection.Acquired {
		return selection.ReleaseFunc, true
	}
	if selection.WaitPlan == nil || concurrencyHelper == nil {
		h.streamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available DeepSeek API key accounts", derefBool(streamStarted))
		return nil, false
	}

	accountWaitCounted := false
	canWait, err := concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
	if err != nil {
		logger.L().Warn("deepseek_gateway.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
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

func (h *DeepSeekGatewayHandler) chatConcurrencyController() deepseekConcurrencyController {
	if h != nil && h.chatConcurrencyHelper != nil {
		return h.chatConcurrencyHelper
	}
	if h == nil {
		return nil
	}
	return h.concurrencyHelper
}

func (h *DeepSeekGatewayHandler) handleDeepSeekUpstreamDegradation(ctx context.Context, account *service.Account, failoverErr *service.UpstreamFailoverError) {
	if h == nil || h.gatewayService == nil || account == nil || failoverErr == nil {
		return
	}
	degrader, ok := h.gatewayService.(deepseekUpstreamErrorHandler)
	if !ok {
		return
	}
	degrader.HandleDeepSeekUpstreamError(ctx, account, failoverErr)
}

func shouldHandleDeepSeekUpstreamDegradation(failoverErr *service.UpstreamFailoverError, mapping service.DeepSeekUpstreamStatusMapping) bool {
	if failoverErr == nil {
		return false
	}
	return mapping.Retryable ||
		failoverErr.StatusCode == http.StatusUnauthorized ||
		failoverErr.StatusCode == http.StatusForbidden ||
		failoverErr.StatusCode == http.StatusPaymentRequired
}

func (h *DeepSeekGatewayHandler) deepseekTempUnscheduler() TempUnscheduler {
	if h == nil || h.gatewayService == nil {
		return noopDeepSeekTempUnscheduler{}
	}
	if unscheduler, ok := h.gatewayService.(TempUnscheduler); ok {
		return unscheduler
	}
	return noopDeepSeekTempUnscheduler{}
}

func (h *DeepSeekGatewayHandler) errorResponse(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

func (h *DeepSeekGatewayHandler) submitUsageRecordTask(task service.UsageRecordTask) {
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
				zap.String("component", "handler.deepseek_gateway.messages"),
				zap.Any("panic", recovered),
			).Error("deepseek_gateway.usage_record_task_panic_recovered")
		}
	}()
	task(ctx)
}

func deepseekRequestID(c *gin.Context) string {
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

func deepseekForwardErrorDetails(err error) (int, string, string) {
	var failoverErr *service.UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		mapping := service.MapDeepSeekUpstreamStatus(failoverErr.StatusCode)
		return mapping.ClientStatus, mapping.ErrorType, deepseekMappedUpstreamErrorMessage(mapping, failoverErr.ResponseBody)
	}
	var unsupported *service.DeepSeekUnsupportedContentError
	if errors.As(err, &unsupported) {
		return http.StatusBadRequest, "invalid_request_error", unsupported.Error()
	}
	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "rate limit"):
		return http.StatusTooManyRequests, "rate_limit_error", "DeepSeek upstream rate limit exceeded"
	case strings.Contains(lower, "parse deepseek messages request"), strings.Contains(lower, "parse deepseek chat completions request"), strings.Contains(lower, "model is required"), strings.Contains(lower, "invalid"):
		return http.StatusBadRequest, "invalid_request_error", message
	default:
		if message == "" {
			message = "Upstream request failed"
		}
		return http.StatusBadGateway, "api_error", fmt.Sprintf("DeepSeek upstream request failed: %s", message)
	}
}

func deepseekMappedUpstreamErrorMessage(mapping service.DeepSeekUpstreamStatusMapping, body []byte) string {
	switch mapping.ErrorType {
	case "upstream_auth_error":
		return "DeepSeek upstream authentication failed, please contact administrator"
	case "insufficient_balance":
		return "DeepSeek upstream balance is insufficient, please contact administrator"
	case "rate_limit_error":
		return "DeepSeek upstream rate limit exceeded, please retry later"
	case "overloaded_error":
		return "DeepSeek upstream service overloaded, please retry later"
	case "server_error":
		return "DeepSeek upstream service temporarily unavailable"
	default:
		if msg := truncateDeepSeekUpstreamMessage(service.ExtractUpstreamErrorMessage(body)); msg != "" {
			return "DeepSeek upstream rejected request: " + msg
		}
		return "DeepSeek upstream rejected request"
	}
}

func truncateDeepSeekUpstreamMessage(message string) string {
	message = strings.Join(strings.Fields(strings.TrimSpace(message)), " ")
	if len(message) <= 300 {
		return message
	}
	return message[:300] + "..."
}
