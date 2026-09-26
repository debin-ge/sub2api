package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// tokenCountRateErrorDetails 把 count_tokens / input_tokens 专用的用户级 RPM 超限
// （SEC-009）映射为 429 + Retry-After（当前分钟剩余秒数），其余错误沿用
// billingErrorDetails 的既有映射。四个 token 计数入口共用，保证口径一致。
func tokenCountRateErrorDetails(err error) (status int, code, message string, retryAfter int) {
	if errors.Is(err, service.ErrTokenCountRPMExceeded) {
		msg := pkgerrors.Message(err)
		if msg == "" {
			msg = "token counting requests-per-minute limit exceeded"
		}
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, 60 - int(time.Now().Unix()%60)
	}
	return billingErrorDetails(err)
}

// ResponsesInputTokens handles native OpenAI POST
// /v1/responses/input_tokens requests without routing them through the normal
// Responses generation and usage-recording pipeline.
func (h *OpenAIGatewayHandler) ResponsesInputTokens(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.responses_input_tokens",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || strings.TrimSpace(modelResult.String()) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := strings.TrimSpace(modelResult.String())
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !openAICompatibleTextTargetAllowed(c, apiKey, reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}

	setOpsRequestContext(c, reqModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(false, false)))
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("openai_input_tokens.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	// 零计费端点不占并发槽，须单独限速，否则单用户就能把共享账号推进上游 429（SEC-009）。
	if err := h.billingCacheService.CheckTokenCountRate(c.Request.Context(), apiKey.UserID); err != nil {
		reqLog.Info("openai_input_tokens.rate_limited", zap.Error(err))
		status, code, message, retryAfter := tokenCountRateErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}

	channelMapping := service.ChannelMappingResult{MappedModel: reqModel}
	if apiKey.GroupID != nil {
		channelMapping = h.gatewayService.ResolveChannelMapping(c.Request.Context(), *apiKey.GroupID, reqModel)
	}
	routingModel := reqModel
	forwardBody := body
	if channelMapping.Mapped {
		routingModel = channelMapping.MappedModel
		forwardBody = h.gatewayService.ReplaceModelInBody(body, routingModel)
	}

	// Token counting is not billed, so it must not be excluded by the profit gate.
	c.Request = c.Request.WithContext(service.WithOpenAIProfitControlSuppressed(c.Request.Context()))
	requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)
	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	requestStart := time.Now()
	account, err := h.gatewayService.SelectAccountForTokenCount(
		c.Request.Context(),
		apiKey.GroupID,
		sessionHash,
		routingModel,
		service.OpenAIEndpointCapabilityChatCompletions,
		requestPlatform,
	)
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	if err != nil {
		reqLog.Warn("openai_input_tokens.account_select_failed", zap.Error(openAICompatibleSelectionErrorForLog(err, requestPlatform)))
		cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, err, routingModel, reqModel)
		if !cls.ModelNotFound {
			markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
		}
		h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
		return
	}
	if account == nil {
		cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, service.ErrNoAvailableAccounts, routingModel, reqModel)
		if !cls.ModelNotFound {
			markOpsRoutingCapacityLimited(c)
		}
		h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
		return
	}

	setOpsSelectedAccount(c, account.ID, account.Platform)
	if err := h.gatewayService.ForwardResponsesInputTokens(c.Request.Context(), c, account, forwardBody); err != nil {
		reqLog.Error("openai_input_tokens.forward_failed", zap.Int64("account_id", account.ID), zap.Error(err))
	}
}

// GrokCountTokens handles Anthropic-compatible count_tokens requests locally.
// The route middleware already authenticates the API key and resolves the
// group; this handler intentionally does not select an account or check billing.
func (h *OpenAIGatewayHandler) GrokCountTokens(c *gin.Context) {
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.anthropicErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, err := service.ParseGatewayRequest(bodyRef, domain.PlatformAnthropic)
	if err != nil {
		logRequestBodyParseFailure(requestLogger(c, "handler.openai_gateway.grok_count_tokens"), body, err)
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	if strings.TrimSpace(parsedReq.Model) == "" {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	estimated, err := service.EstimateGrokCountTokens(parsedReq.Body.Bytes())
	if err != nil {
		requestLogger(c, "handler.openai_gateway.grok_count_tokens").Warn("grok_count_tokens.local_estimate_failed", zap.Error(err))
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	setOpsRequestContext(c, parsedReq.Model, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(false, false)))
	c.JSON(http.StatusOK, gin.H{"input_tokens": estimated})
}

// CountTokens handles Anthropic-compatible POST /v1/messages/count_tokens for OpenAI groups.
// It validates billing and routes to an OpenAI token-count bridge without recording usage.
func (h *OpenAIGatewayHandler) CountTokens(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.count_tokens",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	if !allowOpenAICompatibleMessagesDispatch(c, apiKey) {
		h.anthropicErrorResponse(c, http.StatusForbidden, "permission_error",
			"This group does not allow /v1/messages dispatch")
		return
	}

	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.anthropicErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, err := service.ParseGatewayRequest(bodyRef, domain.PlatformAnthropic)
	if err != nil {
		logRequestBodyParseFailure(reqLog, body, err)
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	body = parsedReq.Body.Bytes()
	if strings.TrimSpace(parsedReq.Model) == "" {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	reqModel := parsedReq.Model
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	// composite+grok 在路由层已分流到 GrokCountTokens，这里可达的目标平台是
	// openai 与 CN 供应商；CN 账号由 ForwardCountTokensAsAnthropic 本地估算。
	if !openAICompatibleTextTargetAllowed(c, apiKey, reqModel) {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	routingModel := service.NormalizeOpenAICompatRequestedModel(reqModel)
	preferredMappedModel := resolveOpenAIMessagesDispatchMappedModel(c, apiKey, reqModel)
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", parsedReq.Stream))

	setOpsRequestContext(c, reqModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(false, false)))

	channelMapping := h.gatewayService.ResolveRequestChannelMapping(c.Request.Context(), apiKey.GroupID, reqModel)
	mappedBodyForMessages := newOpenAIModelMappedBodyCache(body, h.gatewayService.ReplaceModelInBody)

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("openai_count_tokens.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.anthropicErrorResponse(c, status, code, message)
		return
	}
	// 零计费端点不占并发槽，须单独限速，否则单用户就能把共享账号推进上游 429（SEC-009）。
	if err := h.billingCacheService.CheckTokenCountRate(c.Request.Context(), apiKey.UserID); err != nil {
		reqLog.Info("openai_count_tokens.rate_limited", zap.Error(err))
		status, code, message, retryAfter := tokenCountRateErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.anthropicErrorResponse(c, status, code, message)
		return
	}

	requestStart := time.Now()
	// count_tokens 不计费：显式豁免利润门，避免高倍率账号池被门排除后连
	// token 计数都返回 no available accounts。
	c.Request = c.Request.WithContext(service.WithOpenAIProfitControlSuppressed(c.Request.Context()))
	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	currentRoutingModel := routingModel
	if preferredMappedModel != "" {
		currentRoutingModel = preferredMappedModel
	}
	requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)
	// count_tokens 只做选号+转发：用不占并发槽的 token-count 选号器，
	// 否则调度器占下的账号槽无人释放，只能等 TTL 过期（账号并发虚高）。
	account, selectedRoutingModel, err := service.SelectWithChannelMappingRoutingFallback(
		c.Request.Context(),
		nil, // 单次选号，没有失败重试循环，不需要映射回退 latch
		reqModel,
		currentRoutingModel,
		channelMapping,
		func(attemptCtx context.Context, attemptModel string) (*service.Account, error) {
			return h.gatewayService.SelectAccountForTokenCount(
				attemptCtx,
				apiKey.GroupID,
				sessionHash,
				attemptModel,
				service.OpenAIEndpointCapabilityChatCompletions,
				requestPlatform,
			)
		},
	)
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	if err != nil {
		reqLog.Warn("openai_count_tokens.account_select_failed", zap.Error(openAICompatibleSelectionErrorForLog(err, requestPlatform)))
		cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, err, selectedRoutingModel, reqModel)
		if !cls.ModelNotFound {
			markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
		}
		h.anthropicErrorResponse(c, cls.Status, cls.ErrType, cls.Message)
		return
	}
	if account == nil {
		cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, service.ErrNoAvailableAccounts, selectedRoutingModel, reqModel)
		if !cls.ModelNotFound {
			markOpsRoutingCapacityLimited(c)
		}
		h.anthropicErrorResponse(c, cls.Status, cls.ErrType, cls.Message)
		return
	}

	setOpsSelectedAccount(c, account.ID, account.Platform)
	forwardBody := mappedBodyForMessages(channelMapping.Mapped, channelMapping.MappedModel)
	// 选号回退到渠道映射模型时，分发映射模型的账号池刚被证明为空：不能再把它
	// 作为默认模型转发给只对渠道映射模型负责的账号。
	defaultMappedModel := resolveDispatchMappedModelAfterFallback(preferredMappedModel, currentRoutingModel, selectedRoutingModel)

	if err := h.gatewayService.ForwardCountTokensAsAnthropic(c.Request.Context(), c, account, forwardBody, defaultMappedModel); err != nil {
		reqLog.Error("openai_count_tokens.forward_failed", zap.Int64("account_id", account.ID), zap.Error(err))
	}
}
