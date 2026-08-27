package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// noAccountErrorClassification describes the HTTP response to emit when
// account selection failed with ErrNoAvailableAccounts. Handlers obtain it
// via classifyNoAccountError and choose between:
//
//   - 404 model_not_found — the group has accounts, but none of them are
//     configured to serve the requested model (config / typo / unsupported
//     model). Returning 503 here misleads operators and trips reverse-proxy
//     health checks; 404 lets the client surface the real problem.
//
//   - 503 api_error — accounts that could serve the model exist but are
//     temporarily exhausted (rate limit, quota auto-pause, runtime block) OR
//     the group has no accounts at all. Both stay on 503 because retrying
//     after a backoff can plausibly succeed (or, in the empty-pool case, the
//     operator may be in the middle of adding accounts).
//
//   - 503 billing_configuration_error — the model exists but the local
//     billing guard cannot resolve a safe price. This is not an upstream
//     model-not-found response.
type noAccountErrorClassification struct {
	Status  int
	ErrType string
	Message string
	// ModelNotFound also suppresses capacity-limit side effects for permanent
	// model/configuration failures. PricingUnavailable retains that behavior
	// even though its public response is no longer a misleading 404.
	ModelNotFound bool
	// PricingUnavailable marks a local billing configuration rejection rather
	// than an upstream model lookup failure.
	PricingUnavailable bool
}

type groupAccessSelectionError struct {
	Status     int
	Reason     string
	Message    string
	OpenAIType string
}

// classifyGroupAccessSelectionError recognizes the typed access/configuration
// failures emitted while resolving a fallback target. Callers must run this
// before the generic no-account classifier so an exact 403/500/503 is not
// rewritten as a retryable capacity error.
func classifyGroupAccessSelectionError(err error) (groupAccessSelectionError, bool) {
	reason := infraerrors.Reason(err)
	switch service.GroupAccessDenyReason(reason) {
	case service.GroupAccessDenyGroupNotAllowed,
		service.GroupAccessDenyVIPOnly,
		service.GroupAccessDenyProfileMissing,
		service.GroupAccessDenyFallbackInvalidConfig,
		service.GroupAccessDenyGroupInactive,
		service.GroupAccessDenySubscriptionRequired:
		return groupAccessSelectionError{
			Status:     infraerrors.Code(err),
			Reason:     reason,
			Message:    infraerrors.Message(err),
			OpenAIType: openAICompatibleErrorType(infraerrors.Code(err)),
		}, true
	case service.GroupAccessDenyFallbackSubscription:
		// This reason belongs to write-time validation. If it reaches the
		// gateway through a legacy path, preserve the runtime contract: a
		// subscription fallback is invalid configuration, not a client error.
		return groupAccessSelectionError{
			Status:     http.StatusServiceUnavailable,
			Reason:     string(service.GroupAccessDenyFallbackInvalidConfig),
			Message:    "fallback group configuration is invalid",
			OpenAIType: "api_error",
		}, true
	default:
		return groupAccessSelectionError{}, false
	}
}

func openAICompatibleErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	default:
		return "api_error"
	}
}

// handleOpenAICompatibleGroupAccessSelectionError writes the common error
// envelope used by the fork gateways. It deliberately runs before their
// failover/capacity handling so a typed authorization failure cannot become a
// retryable 503. Once streaming has started the response cannot be rewritten,
// but the error is still consumed and the handler must stop.
func handleOpenAICompatibleGroupAccessSelectionError(
	c *gin.Context,
	err error,
	streamStarted bool,
) bool {
	if err == nil {
		return false
	}
	accessErr, ok := classifyGroupAccessSelectionError(err)
	if !ok {
		return false
	}
	if streamStarted || c == nil {
		return true
	}
	c.JSON(accessErr.Status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    accessErr.OpenAIType,
			"code":    accessErr.Reason,
			"message": accessErr.Message,
		},
	})
	return true
}

var selectionModelRateLimitedPattern = regexp.MustCompile(`(?:model_rate_limited|rate_limited)=(\d+)`)

// classifySelectionFailureError preserves the scheduler's compact reason when
// every model-capable account is temporarily rate limited.
func classifySelectionFailureError(err error, fallback noAccountErrorClassification) noAccountErrorClassification {
	if err == nil {
		return fallback
	}
	match := selectionModelRateLimitedPattern.FindStringSubmatch(strings.ToLower(err.Error()))
	if len(match) != 2 {
		return fallback
	}
	count, parseErr := strconv.Atoi(match[1])
	if parseErr != nil || count <= 0 {
		return fallback
	}
	return noAccountErrorClassification{
		Status:  http.StatusTooManyRequests,
		ErrType: "rate_limit_error",
		Message: "All available accounts are currently rate-limited. Please retry later.",
	}
}

// classifyNoAccountError decides between 404 model_not_found and 503
// api_error for account-selection failures.
//
// selErr is the error selection actually returned. Exactly one of its causes
// is self-describing: the pricing guard (ErrModelPricingUnavailable) rejects
// before forwarding because the model has no billable price, and it says so
// verbatim. That case is answered from the error alone — see
// pricingUnavailableClassification for why it must not fall through to the
// pool diagnosis.
//
// For every other cause the error is uninformative: the selection layer never
// tells us *why* the pool came up empty (rate-limited vs. unsupported model
// are both wrapped as ErrNoAvailableAccounts). There we re-check pool
// composition through DiagnoseModelAvailabilityForPlatform. Its dedicated
// database query considers only persistent eligibility (active status +
// schedulable setting) and model_mapping, bypassing scheduler snapshots and
// transient filters. That guarantees a 404 is only returned when persistent
// account/group/model configuration must change before the request can
// succeed.
//
// routingModel is the model name that account selection actually compared
// against (i.e. after group-level dispatch mapping). displayModel is the
// raw model the caller asked for; it is used only in the user-facing error
// message so that internal mapping details don't leak. Most callers pass
// the same value for both.
//
// platform is the platform the request was routed to (use
// service.PlatformOpenAI / PlatformAnthropic / PlatformGemini). It is
// required because Anthropic/Gemini routes additionally surface
// mixed-scheduled Antigravity accounts; passing the wrong platform would
// flip a legitimate 503 to a misleading 404 (or vice versa).
func classifyNoAccountError(
	ctx context.Context,
	diag service.ModelAvailabilityDiagnoser,
	apiKey *service.APIKey,
	selErr error,
	routingModel string,
	displayModel string,
	platform string,
) noAccountErrorClassification {
	fallback := noAccountErrorClassification{
		Status:  http.StatusServiceUnavailable,
		ErrType: "api_error",
		Message: "Service temporarily unavailable",
	}

	routingModel = strings.TrimSpace(routingModel)
	displayModel = strings.TrimSpace(displayModel)
	if displayModel == "" {
		displayModel = routingModel
	}

	// 价格门禁的拒绝先于账号池诊断结算：它与账号池状态无关，池子再健康也一样拒。
	// 放在诊断之后会被"池子里有账号且支持该模型"这一支盖成 503，正好把一个改配置
	// 才能修的问题报成"稍后重试"。
	if errors.Is(selErr, service.ErrModelPricingUnavailable) {
		return pricingUnavailableClassification(apiKey, selErr, routingModel, displayModel, platform)
	}

	if diag == nil || apiKey == nil || apiKey.GroupID == nil || routingModel == "" {
		return fallback
	}

	result := diag.DiagnoseModelAvailabilityForPlatform(ctx, apiKey.GroupID, routingModel, platform)
	if result.HasAccountsInPool && !result.HasModelSupport {
		return noAccountErrorClassification{
			Status:        http.StatusNotFound,
			ErrType:       "model_not_found",
			Message:       fmt.Sprintf("Model %q is not supported by any configured account in this group", displayModel),
			ModelNotFound: true,
		}
	}
	return fallback
}

// pricingUnavailableClassification 是价格门禁拒绝的统一出口。
//
// Price resolution is a local service dependency, not evidence that the
// requested upstream model does not exist. Return a billing-specific 503 while
// retaining ModelNotFound=true internally only to skip
// markOpsRoutingCapacityLimited* side effects.
//
// 客户端只拿到"这个模型不可用"，模型链细节（requested/channel/billing/upstream）
// 留在日志里，那是运维要看的东西，不该出现在响应体。
func pricingUnavailableClassification(
	apiKey *service.APIKey,
	selErr error,
	routingModel string,
	displayModel string,
	platform string,
) noAccountErrorClassification {
	logger.L().Warn("gateway.pricing_unavailable_rejected",
		zap.String("component", "handler.gateway"),
		zap.String("routing_model", routingModel),
		zap.String("display_model", displayModel),
		zap.String("platform", platform),
		zap.Int64("api_key_id", apiKeyIDForNoAccountLog(apiKey)),
		zap.Int64("group_id", groupIDForNoAccountLog(apiKey)),
		zap.Error(selErr),
	)
	return noAccountErrorClassification{
		Status:             http.StatusServiceUnavailable,
		ErrType:            "billing_configuration_error",
		Message:            fmt.Sprintf("Model %q is temporarily unavailable due to billing configuration", displayModel),
		ModelNotFound:      true,
		PricingUnavailable: true,
	}
}

func apiKeyIDForNoAccountLog(apiKey *service.APIKey) int64 {
	if apiKey == nil {
		return 0
	}
	return apiKey.ID
}

func groupIDForNoAccountLog(apiKey *service.APIKey) int64 {
	if apiKey == nil || apiKey.GroupID == nil {
		return 0
	}
	return *apiKey.GroupID
}

// classifyNoAccountErrorFromGin is a thin wrapper that forwards the gin
// context's underlying request context. Most call sites already have a
// *gin.Context handy, so this keeps the call sites uncluttered.
func classifyNoAccountErrorFromGin(
	c *gin.Context,
	diag service.ModelAvailabilityDiagnoser,
	apiKey *service.APIKey,
	selErr error,
	routingModel string,
	displayModel string,
	platform string,
) noAccountErrorClassification {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	classification := classifyNoAccountError(ctx, diag, apiKey, selErr, routingModel, displayModel, platform)
	if classification.ModelNotFound {
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalModelConfiguration)
	}
	return classification
}

func classifyOpenAICompatibleNoAccountErrorFromGin(
	c *gin.Context,
	diag service.ModelAvailabilityDiagnoser,
	apiKey *service.APIKey,
	selErr error,
	routingModel string,
	displayModel string,
) noAccountErrorClassification {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	return classifyNoAccountErrorFromGin(
		c,
		diag,
		apiKey,
		selErr,
		routingModel,
		displayModel,
		openAICompatibleRequestPlatform(ctx, apiKey),
	)
}

func openAICompatibleSelectionErrorForLog(err error, platform string) error {
	if err == nil || platform != service.PlatformGrok {
		return err
	}
	message := strings.ReplaceAll(err.Error(), "OpenAI accounts", "Grok accounts")
	if message == err.Error() {
		return err
	}
	return fmt.Errorf("%s", message)
}
