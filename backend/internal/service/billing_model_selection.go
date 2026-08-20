package service

import (
	"strings"

	"go.uber.org/zap"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// selectBillingModelBySource 把「渠道声明的计费基准 → 该拿哪个模型名去查价」这张
// 映射表收敛到一处。
//
// 准入守卫与结算阶段此前各写了一份：形状不同（一个 switch、一串 if），空值策略不同，
// 分支数量也不同。这意味着任何一侧新增或改动一个基准，都不会让另一侧编译失败——
// 守卫验过的模型和结算真正查价的模型可以悄悄分叉，而这正是不变量 I3
// （准入确定的口径原样传到结算）要防的事。
//
// 返回 recognized=false 表示 source 不是已知的三种基准之一（含空串）。调用方据此
// 保留自己的基准模型，而不是被覆盖成空串。
//
// 两侧对「选中的名字是空串」的处理必须不同，这是有意的：
//   - 准入（I1）：空名字查不出任何价，照常交给 admit 判定自然被拒——不能放行。
//   - 结算（I2）：上游成本已经真实发生，空名字必须回落到基准模型把记录落下来。
//
// 因此本函数只负责选名字，空值策略留在调用方。
func selectBillingModelBySource(source, requested, channelMapped, upstream string) (string, bool) {
	switch source {
	case BillingModelSourceRequested:
		return strings.TrimSpace(requested), true
	case BillingModelSourceChannelMapped:
		return strings.TrimSpace(channelMapped), true
	case BillingModelSourceUpstream:
		return strings.TrimSpace(upstream), true
	default:
		return "", false
	}
}

// billingModelDriftGuardName 是漂移哨兵在日志里的标识，与 pricing_guard_would_block
// 的 guard 字段同一命名空间，便于一起做看板。
const billingModelDriftGuardName = "billing_model_drift"

// billingModelDrifted 判断结算最终查价用的模型是否已经离开准入守卫验过的候选集合。
//
// 守卫的候选集合恰好是两个：来源选定的计费模型，以及实际转发的上游模型
// （见 GatewayService.admitTokenPricing——全局价那一支走的是
// billableModelWithFallback(billingModel, upstreamModel)）。结算的候选集合比它宽，
// 多出 composite 的具体模型与请求模型本身。这个宽窄差是有意的：准入 fail-closed 要窄，
// 结算 fail-loud 要宽，谁向谁看齐都会坏掉一侧。
//
// 落在多出来的那部分候选上，意味着这笔钱是按一个准入从没验过的模型算的。这不是错误
// （价格是查得到的），但它是 I3 被打破的现场，值得单独可见。
func billingModelDrifted(admittedModel, settledModel, upstreamModel string) bool {
	settled := strings.TrimSpace(settledModel)
	if settled == "" {
		return false
	}
	if strings.EqualFold(settled, strings.TrimSpace(admittedModel)) {
		return false
	}
	return !strings.EqualFold(settled, strings.TrimSpace(upstreamModel))
}

// reportBillingModelDrift 在结算模型离开准入候选集合时打一条告警。
//
// 按 (admitted, settled) 去重：漂移由配置形态决定（某个 composite 别名没配渠道价、
// 某个映射名没进价目录），同一组合会在每个请求上重复出现，逐请求打会淹掉日志。
// 这里沿用 BillingService.fallbackWarnSeen 的做法，每个组合每进程一条。
func (s *GatewayService) reportBillingModelDrift(
	apiKey *APIKey,
	account *Account,
	billingModelSource string,
	admittedModel string,
	settledModel string,
	result *ForwardResult,
) {
	if s == nil || result == nil {
		return
	}
	// simple 模式只记用量不计费，模型选谁都不产生金额差异。
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return
	}
	if !billingModelDrifted(admittedModel, settledModel, result.UpstreamModel) {
		return
	}

	admitted := strings.TrimSpace(admittedModel)
	settled := strings.TrimSpace(settledModel)
	if _, seen := s.billingModelDriftSeen.LoadOrStore(admitted+"\x00"+settled, struct{}{}); seen {
		return
	}

	// 结算模型在严格口径下有没有自己的价，是这条日志唯一可直接行动的字段：
	// false 说明这笔钱是拿别的模型的价格套上去的（见 pricing_strict_match.go）。
	settledStrictlyPriced := false
	if s.billingService != nil {
		_, err := s.billingService.GetModelPricingStrictForPlatform(accountPlatformForLog(account), settled)
		settledStrictlyPriced = err == nil
	}

	logger.L().Warn("billing.pricing_model_drift",
		zap.String("component", "service.gateway"),
		zap.String("guard", billingModelDriftGuardName),
		zap.String("billing_model_source", billingModelSource),
		zap.String("admitted_model", admitted),
		zap.String("settled_model", settled),
		zap.Bool("settled_strictly_priced", settledStrictlyPriced),
		zap.String("requested_model", strings.TrimSpace(result.Model)),
		zap.String("upstream_model", strings.TrimSpace(result.UpstreamModel)),
		zap.Int64("api_key_id", apiKeyIDForLog(apiKey)),
		zap.Int64("account_id", accountIDForLog(account)),
		zap.String("account_platform", accountPlatformForLog(account)),
	)
}
