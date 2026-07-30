//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"

	"github.com/stretchr/testify/require"
)

// composite 分组的公开别名经 BillingModelSource 来源覆盖成为计费模型后有两类错计：
// 任意别名（如 team/best）查无价静默落 $0；含家族词的别名（如 all/claude）被价格表
// 家族模糊匹配错计（Opus 流量按 Sonnet 兜底价）。compositeBillableModel 要求别名必须
// 有显式渠道定价才可参与计费，否则回退实际转发的具体模型。
func TestCompositeBillableModel(t *testing.T) {
	svc := &GatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	apiKey := &APIKey{}
	ctx := context.Background()

	// 别名无渠道定价（含家族词也一样）→ 回退具体模型
	require.Equal(t, "claude-opus-4-7",
		svc.compositeBillableModel(ctx, apiKey, "all/claude", "claude-opus-4-7"))
	require.Equal(t, "claude-sonnet-4",
		svc.compositeBillableModel(ctx, apiKey, "team/best", "claude-sonnet-4"))

	// 未发生来源覆盖（计费模型已是具体模型）→ 原样返回
	require.Equal(t, "claude-sonnet-4",
		svc.compositeBillableModel(ctx, apiKey, "claude-sonnet-4", "claude-sonnet-4"))

	// 具体模型缺失 → 保持原值（走后续通用兜底/既有路径）
	require.Equal(t, "all/claude",
		svc.compositeBillableModel(ctx, apiKey, "all/claude", ""))
}

// billableModelWithFallback 是结算安全网：选定计费模型查不到严格价格时回退到
// 实际转发的具体模型；family 模糊推断不能再阻止回退。
func TestBillableModelWithFallback(t *testing.T) {
	svc := &GatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	apiKey := &APIKey{}
	ctx := context.Background()

	// 完全无价的别名 → 回退到具体转发模型（claude-sonnet-4 有内置回退价格）
	require.Equal(t, "claude-sonnet-4",
		svc.billableModelWithFallback(ctx, apiKey, "team/best", "", "claude-sonnet-4"))

	// 已定价模型不回退，候选被忽略
	require.Equal(t, "claude-sonnet-4",
		svc.billableModelWithFallback(ctx, apiKey, "claude-sonnet-4", "claude-opus-4"))

	// 所有候选都无价 → 保持原值，后扣 fail-loud 并进入待结算
	require.Equal(t, "team/best",
		svc.billableModelWithFallback(ctx, apiKey, "team/best", "another/alias", ""))

	// 空计费模型 + 有价候选 → 取候选
	require.Equal(t, "claude-sonnet-4",
		svc.billableModelWithFallback(ctx, apiKey, "", "claude-sonnet-4"))
}

// v0.1.166 合并移除了 getFallbackPricing 里 deepseek-chat / deepseek-reasoner →
// v4-flash 的兼容别名兜底（旧映射还把 reasoner 错按 flash 的便宜档计价）。
// 移除本身是对的：别名在 GetDeepSeekMappedModel 阶段就已解析成真实上游模型，
// 且这里的通用兜底会接住漏网的裸别名。本用例把这层保护固化下来——否则一旦
// UpstreamModel 传递链断掉，DeepSeek 流量会静默走到 ErrModelPricingUnavailable。
func TestBillableModelWithFallback_DeepSeekCompatAliasFallsBackToUpstreamModel(t *testing.T) {
	svc := &GatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	apiKey := &APIKey{}
	ctx := context.Background()

	// 前提：裸别名本身查不到价（与 TestGetFallbackPricing_DeepSeekCompatNamesDoNotAlias 对应）
	require.False(t, svc.hasResolvableTokenPricing(ctx, "deepseek-chat", apiKey))
	require.False(t, svc.hasResolvableTokenPricing(ctx, "deepseek-reasoner", apiKey))

	// 别名 + 已解析的上游模型 → 按上游模型计费，不会漏计
	require.Equal(t, "deepseek-v4-flash",
		svc.billableModelWithFallback(ctx, apiKey, "deepseek-chat", "deepseek-v4-flash", "deepseek-chat"))
	require.Equal(t, "deepseek-v4-pro",
		svc.billableModelWithFallback(ctx, apiKey, "deepseek-reasoner", "deepseek-v4-pro", "deepseek-reasoner"))
}

func TestHasResolvableTokenPricing(t *testing.T) {
	svc := &GatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	apiKey := &APIKey{}
	ctx := context.Background()

	require.True(t, svc.hasResolvableTokenPricing(ctx, "claude-sonnet-4", apiKey))
	// 结算只认当前 SKU 自己的价；含家族词的公开别名不能借 Sonnet 的价格。
	require.False(t, svc.hasResolvableTokenPricing(ctx, "all/claude", apiKey))
	require.False(t, svc.hasResolvableTokenPricing(ctx, "team/best", apiKey))
	require.False(t, svc.hasResolvableTokenPricing(ctx, "", apiKey))

	// billingService 缺失时 fail-closed（不误判有价）
	empty := &GatewayService{}
	require.False(t, empty.hasResolvableTokenPricing(ctx, "claude-sonnet-4", apiKey))
}
