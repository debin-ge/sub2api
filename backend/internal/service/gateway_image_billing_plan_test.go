package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// 生产形态的覆盖行（迁移 289 之后）：openai 平台上 79 号 gpt-image-2 为 image 档 0.05，
// 32 号 gpt-image-2-1k 为 image 档 0.10。
func productionImageOverridesForTest() []ModelPriceOverride {
	return []ModelPriceOverride{
		{
			Platform: PlatformOpenAI, ModelName: "gpt-image-2", BillingMode: BillingModeImage, Enabled: true,
			Payload: ModelPriceOverridePayload{OutputCostPerImage: imagePrice(0.05)},
		},
		{
			Platform: PlatformOpenAI, ModelName: "gpt-image-2-1k", BillingMode: BillingModeImage, Enabled: true,
			Payload: ModelPriceOverridePayload{OutputCostPerImage: imagePrice(0.10)},
		},
	}
}

func newGatewayImageBillingService(t *testing.T, overrides []ModelPriceOverride) *GatewayService {
	t.Helper()
	f := newImageBillingFixture(t, overrides)
	return &GatewayService{billingService: f.billing, resolver: f.resolver}
}

// 组合分组回归（防回归；生产目前没有这类流量）：别名转发到 Antigravity 的 Gemini 出图模型时，
// 别名只有目录 token 价或别的平台的覆盖行，不能拿来乘 Gemini 的用量，回退到实际转发的模型按张收。
func TestGatewayCompositeImageAliasFallsBackToConcreteModel(t *testing.T) {
	svc := newGatewayImageBillingService(t, productionImageOverridesForTest())
	group := &Group{ID: 31, Platform: PlatformComposite}
	apiKey := &APIKey{GroupID: &group.ID, Group: group}
	platforms := []string{PlatformComposite, PlatformAntigravity}
	const upstream = "gemini-3-pro-image-preview"

	for _, tc := range []struct {
		alias string
		tier  string
		want  float64
	}{
		{"gpt-image-2", ImageBillingSize1K, 0.134},
		{"gpt-image-2", ImageBillingSize2K, 0.201},
		{"gpt-image-2-1k", ImageBillingSize2K, 0.201},
	} {
		model := svc.billableImageModelWithFallbackForPlatforms(context.Background(), apiKey, platforms, tc.alias, tc.tier, upstream, upstream)
		require.Equal(t, upstream, model, tc.alias)
		result := &ForwardResult{ImageCount: 1, ImageSize: tc.tier, UpstreamModel: upstream}
		cost, err := svc.calculateGatewayImageCost(context.Background(), result, apiKey, platforms, model, 1, 1)
		require.NoError(t, err)
		require.Equal(t, string(BillingModeImage), cost.BillingMode)
		require.InDelta(t, tc.want, cost.TotalCost, 1e-12, "%s %s", tc.alias, tc.tier)
	}
}

// 别名在 composite 平台上配了 token 档覆盖行：显式配置，按别名的 token 价算 Gemini 的用量。
func TestGatewayCompositeImageAliasWithExplicitTokenOverride(t *testing.T) {
	overrides := append(productionImageOverridesForTest(), ModelPriceOverride{
		Platform: PlatformComposite, ModelName: "studio-image", BillingMode: BillingModeToken, Enabled: true,
		Payload: ModelPriceOverridePayload{InputCostPerImageToken: imagePrice(1e-6), OutputCostPerImageToken: imagePrice(4e-5)},
	})
	svc := newGatewayImageBillingService(t, overrides)
	group := &Group{ID: 32, Platform: PlatformComposite}
	apiKey := &APIKey{GroupID: &group.ID, Group: group}
	platforms := []string{PlatformComposite, PlatformAntigravity}
	const upstream = "gemini-3-pro-image-preview"

	model := svc.billableImageModelWithFallbackForPlatforms(context.Background(), apiKey, platforms, "studio-image", ImageBillingSize2K, upstream, upstream)
	require.Equal(t, "studio-image", model)
	result := &ForwardResult{
		ImageCount: 1, ImageSize: ImageBillingSize2K, UpstreamModel: upstream,
		Usage: ClaudeUsage{InputTokens: 500, OutputTokens: 1130, ImageOutputTokens: 1120},
	}
	cost, err := svc.calculateGatewayImageCost(context.Background(), result, apiKey, platforms, model, 1, 1)
	require.NoError(t, err)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
	require.InDelta(t, 500*1e-6+1130*4e-5, cost.TotalCost, 1e-12)
}

// 不是跨模型（计费名等于实际出图的模型）：目录 token 价照常有效。
func TestGatewayImageSameModelCatalogTokenPlan(t *testing.T) {
	svc := newGatewayImageBillingService(t, nil)
	platforms := []string{PlatformOpenAI}
	require.True(t, svc.hasResolvableImagePricingForPlatforms(context.Background(), "gpt-image-1.5", ImageBillingSize1K, nil, platforms, false, false))
	require.False(t, svc.hasResolvableImagePricingForPlatforms(context.Background(), "gpt-image-1.5", ImageBillingSize1K, nil, platforms, false, true),
		"跨模型时目录 token 价不算数")
}

// Gemini 出图：目录按张金额不变；覆盖成 token 档按 usageMetadata × 两项价（场景 5–7）。
func TestGatewayGeminiImagePlans(t *testing.T) {
	svc := newGatewayImageBillingService(t, nil)
	platforms := []string{PlatformGemini}
	result := &ForwardResult{ImageCount: 1, ImageSize: "", UpstreamModel: "gemini-2.5-flash-image",
		Usage: ClaudeUsage{InputTokens: 500, OutputTokens: 1310, ImageOutputTokens: 1290}}
	cost, err := svc.calculateGatewayImageCost(context.Background(), result, nil, platforms, "gemini-2.5-flash-image", 1, 1)
	require.NoError(t, err)
	require.InDelta(t, 0.0585, cost.TotalCost, 1e-12, "没写 imageSize 按 2K")

	svc = newGatewayImageBillingService(t, []ModelPriceOverride{{
		Platform: PlatformGemini, ModelName: "gemini-2.5-flash-image", BillingMode: BillingModeImage, Enabled: true,
		Payload: ModelPriceOverridePayload{Intervals: []PricingInterval{
			{TierLabel: ImageBillingSize1K, PerRequestPrice: imagePrice(0.03)},
			{TierLabel: ImageBillingSize2K, PerRequestPrice: imagePrice(0.05)},
			{TierLabel: ImageBillingSize4K, PerRequestPrice: imagePrice(0.08)},
		}},
	}})
	result.ImageSize = ImageBillingSize4K
	cost, err = svc.calculateGatewayImageCost(context.Background(), result, nil, platforms, "gemini-2.5-flash-image", 1, 1)
	require.NoError(t, err)
	require.InDelta(t, 0.08, cost.TotalCost, 1e-12)

	svc = newGatewayImageBillingService(t, []ModelPriceOverride{{
		Platform: PlatformGemini, ModelName: "gemini-2.5-flash-image", BillingMode: BillingModeToken, Enabled: true,
		Payload: ModelPriceOverridePayload{InputCostPerImageToken: imagePrice(3e-7), OutputCostPerImageToken: imagePrice(3e-5)},
	}})
	cost, err = svc.calculateGatewayImageCost(context.Background(), result, nil, platforms, "gemini-2.5-flash-image", 1, 1)
	require.NoError(t, err)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
	require.InDelta(t, 0.03945, cost.TotalCost, 1e-12)
}

// 渠道 token 档只配了文本价 + Gemini（按张）：方式锁定为 token，Gemini 没有图片 token 价，拒绝。
// 分组开关开启、找不到按张价：拒绝，不再回落 token。
func TestGatewayImageRejectsMixedModeConfigurations(t *testing.T) {
	f := newImageBillingFixture(t, nil).withChannel(33, "gemini-2.5-flash-image", &ChannelModelPricing{
		BillingMode: BillingModeToken, InputPrice: imagePrice(3e-7), OutputPrice: imagePrice(2.5e-6),
	})
	svc := &GatewayService{billingService: f.billing, resolver: f.resolver}
	apiKey := &APIKey{GroupID: imageInt64(33), Group: &Group{ID: 33, Platform: PlatformGemini}}
	require.False(t, svc.hasResolvableImagePricingForPlatforms(context.Background(), "gemini-2.5-flash-image", ImageBillingSize1K, apiKey, []string{PlatformGemini}, false, false))

	svc = newGatewayImageBillingService(t, nil)
	switchKey := &APIKey{GroupID: imageInt64(34), Group: &Group{ID: 34, Platform: PlatformOpenAI, ImageCountBilling: true}}
	require.False(t, svc.hasResolvableImagePricingForPlatforms(context.Background(), "gpt-image-2", ImageBillingSize1K, switchKey, []string{PlatformOpenAI}, true, false))
	require.True(t, svc.hasResolvableImagePricingForPlatforms(context.Background(), "gpt-image-2", ImageBillingSize1K, switchKey, []string{PlatformOpenAI}, false, false),
		"开关只作用于专用图片端点")
}
