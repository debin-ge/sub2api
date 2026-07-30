package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

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
type noAccountErrorClassification struct {
	Status        int
	ErrType       string
	Message       string
	ModelNotFound bool // true when this is a 404 model_not_found classification
	// PricingUnavailable 标记这次 404 是价格门禁拒绝的，而不是"没有账号支持该模型"。
	// 两者对客户端是同一件事（改配置前重试没有意义），但对运维是完全不同的两处配置：
	// 一处在账号/模型映射，一处在价目录。调用方据此区分埋点与告警。
	PricingUnavailable bool
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
// 状态码沿用 404 model_not_found，理由与"没有账号支持该模型"那一支相同：都要先改
// 服务端配置，重试再多次也不会成功，503 会让客户端和反向代理把它当成瞬时故障一直
// 重试。ModelNotFound=true 同时让调用方跳过 markOpsRoutingCapacityLimited*——把
// 定价缺失记成"容量打满"会污染扩容决策。
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
		Status:             http.StatusNotFound,
		ErrType:            "model_not_found",
		Message:            fmt.Sprintf("Model %q is not available: no billing price is configured for it", displayModel),
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
	return classifyNoAccountError(ctx, diag, apiKey, selErr, routingModel, displayModel, platform)
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
