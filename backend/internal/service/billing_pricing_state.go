package service

import (
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// 未定价模型的处理分两层，这个文件是"记账层"那一半。
//
//   - 准入层（转发前）：fail-closed。价格解析不出来就不把请求发给上游 ——
//     参见 GatewayService.ValidateUsagePricing / OpenAIGatewayService 的定价守卫。
//   - 记账层（转发后）：fail-loud + 可补偿。上游成本已经真实发生，此时再丢弃
//     整条记录只会同时丢掉用量、配额计数与对账线索，而钱照样花了。
//
// 换句话说，"未知价格不等于免费"约束的是要不要转发，不是要不要记录。

// classifyRecordUsageCostError 判定后扣阶段的成本计算错误该怎么处理。
//
// 返回 unpriced=true 表示该请求无可解析价格：调用方应按零费用继续走完记账流程，
// 并把 usage_log 标记为 BillingStatePricingUnavailable，等管理员补配价格后由补偿
// 流程重算。返回 fatal != nil 表示这是定价之外的故障（依赖不可用等），重试有意义，
// 应原样上抛而不是伪造一条零费用记录。
func classifyRecordUsageCostError(err error) (unpriced bool, fatal error) {
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, ErrModelPricingUnavailable) {
		return false, err
	}
	return true, nil
}

// unpricedCostBreakdown 构造定价缺失时的零费用明细。
// BillingMode 仍按用量形态标注，便于管理端按计费模式聚合未定价流量。
//
// 口径以入口显式给出的 kind 为准：videos_* 路由即便没拿到 video_count 也是视频
// 流量，标成 token 会让"哪类流量在漏计"这个问题查不出来。kind 为基线/未指定时
// 才退回按上游计数反推。
func unpricedCostBreakdown(kind BillingKind, imageCount, videoCount int) *CostBreakdown {
	mode := BillingModeToken
	switch {
	case !kind.ResponseDrivenMediaOverride():
		mode = kind.BillingMode()
	case videoCount > 0:
		mode = BillingModeVideo
	case imageCount > 0:
		mode = BillingModeImage
	}
	return &CostBreakdown{BillingMode: string(mode)}
}

// logPricingUnavailableUsage 记录一条未定价用量。这是管理员补配价格的唯一信号源，
// 必须带齐模型解析链路上的每个名字，否则无法判断该给哪个名字配价。
func logPricingUnavailableUsage(
	component string,
	simpleMode bool,
	err error,
	fields ...zap.Field,
) {
	entry := logger.L().With(append([]zap.Field{zap.String("component", component)}, fields...)...)
	if simpleMode {
		// Simple Mode 本就不扣费，无价模型是其既有兼容语义，不当作异常告警。
		entry.Warn("billing.simple_mode_pricing_unavailable", zap.Error(err))
		return
	}
	entry.Error("billing.pricing_unavailable_recorded", zap.Error(err))
}

func apiKeyIDForLog(apiKey *APIKey) int64 {
	if apiKey == nil {
		return 0
	}
	return apiKey.ID
}

func accountIDForLog(account *Account) int64 {
	if account == nil {
		return 0
	}
	return account.ID
}

func accountPlatformForLog(account *Account) string {
	if account == nil {
		return ""
	}
	return account.Platform
}
