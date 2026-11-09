package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// imageBillingFixture 用内置价格目录构建解析器，可叠加覆盖行与一条渠道定价。
type imageBillingFixture struct {
	pricing  *PricingService
	billing  *BillingService
	resolver *ModelPricingResolver
}

func newImageBillingFixture(t *testing.T, overrides []ModelPriceOverride) *imageBillingFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)
	pricing := NewPricingService(&config.Config{}, nil)
	catalog, err := pricing.parsePricingData(data)
	require.NoError(t, err)
	pricing.SeedCatalogForTest(catalog)
	if overrides != nil {
		pricing.SeedOverridesForTest(overrides)
	}
	billing := NewBillingService(&config.Config{}, pricing)
	return &imageBillingFixture{pricing: pricing, billing: billing, resolver: NewModelPricingResolver(nil, billing)}
}

func (f *imageBillingFixture) withChannel(groupID int64, model string, row *ChannelModelPricing) *imageBillingFixture {
	cache := newEmptyChannelCache()
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: model}] = row
	cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
	cache.groupPlatform[groupID] = ""
	cache.loadedAt = time.Now()
	channels := &ChannelService{}
	channels.cache.Store(cache)
	f.resolver = NewModelPricingResolver(channels, f.billing)
	return f
}

func (f *imageBillingFixture) resolve(t *testing.T, in ImageBillingInput) (*ImageBillingPlan, error) {
	t.Helper()
	if len(in.Platforms) == 0 {
		in.Platforms = []string{PlatformOpenAI}
	}
	return f.resolver.ResolveImageBilling(context.Background(), in)
}

func imagePrice(v float64) *float64 { return &v }

func requireImagePlanRejected(t *testing.T, plan *ImageBillingPlan, err error, contains string) {
	t.Helper()
	require.Nil(t, plan)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	if contains != "" {
		require.ErrorContains(t, err, contains)
	}
}

// 1. 分组卡 token 档：用卡上的价；卡缺输入价时，继承渠道或 ④ 的 token 价；④ 是按张则拒绝。
func TestResolveImageBillingGroupCardTokenMode(t *testing.T) {
	f := newImageBillingFixture(t, nil)
	card := func(models ...string) *Group {
		return &Group{ID: 11, Platform: PlatformOpenAI, ModelPricing: []ChannelModelPricing{{
			Models: models, BillingMode: BillingModeToken, ImageOutputPrice: imagePrice(4e-5),
		}}}
	}

	plan, err := f.resolve(t, ImageBillingInput{Model: "gpt-image-2", Group: card("gpt-image-2"), SizeTier: "1K"})
	require.NoError(t, err)
	require.Equal(t, BillingModeToken, plan.Mode)
	require.Equal(t, ImageBillingDeclaredByGroupCard, plan.DeclaredBy)
	require.Equal(t, PricingSourceGroup, plan.Source)
	require.InDelta(t, 8e-6, plan.ImageInputPrice, 1e-15, "卡没配图片输入价，继承 ④")
	require.InDelta(t, 4e-5, plan.ImageOutputPrice, 1e-15)

	f.withChannel(11, "gpt-image-2", &ChannelModelPricing{BillingMode: BillingModeToken, ImageInputPrice: imagePrice(9e-6)})
	plan, err = f.resolve(t, ImageBillingInput{Model: "gpt-image-2", GroupID: imageInt64(11), Group: card("gpt-image-2"), SizeTier: "1K"})
	require.NoError(t, err)
	require.InDelta(t, 9e-6, plan.ImageInputPrice, 1e-15, "渠道 token 层在 ④ 之前")

	plan, err = f.resolve(t, ImageBillingInput{Model: "gemini-2.5-flash-image", Group: card("gemini-2.5-flash-image"), SizeTier: "1K"})
	requireImagePlanRejected(t, plan, err, "missing image input price")
}

func imageInt64(v int64) *int64 { return &v }

// 2. 分组只配了 2K、请求 1K：④ 按张（Gemini）→ 用 ④ 的 1K 价；④ 按 token（gpt-image-2）→ 拒绝。
func TestResolveImageBillingPartialGroupImagePrice(t *testing.T) {
	f := newImageBillingFixture(t, nil)
	group := &Group{ID: 12, Platform: PlatformOpenAI, ImagePrice2K: imagePrice(0.15)}

	plan, err := f.resolve(t, ImageBillingInput{Model: "gemini-2.5-flash-image", Group: group, SizeTier: "1K"})
	require.NoError(t, err)
	require.Equal(t, BillingModeImage, plan.Mode)
	require.Equal(t, ImageBillingDeclaredByGroupImagePrice, plan.DeclaredBy)
	require.Equal(t, PricingSourceModelPrice, plan.Source)
	require.InDelta(t, 0.039, plan.UnitPrice, 1e-12)

	plan, err = f.resolve(t, ImageBillingInput{Model: "gemini-2.5-flash-image", Group: group, SizeTier: "2K"})
	require.NoError(t, err)
	require.Equal(t, PricingSourceGroup, plan.Source)
	require.InDelta(t, 0.15, plan.UnitPrice, 1e-12)

	plan, err = f.resolve(t, ImageBillingInput{Model: "gpt-image-2", Group: group, SizeTier: "1K"})
	requireImagePlanRejected(t, plan, err, "declared_by=group_image_price")
}

// 3. 分组按张开关只作用于专用图片端点；开启时找不到按张价就拒绝。
func TestResolveImageBillingImageCountSwitch(t *testing.T) {
	group := &Group{ID: 13, Platform: PlatformOpenAI, ImageCountBilling: true}

	f := newImageBillingFixture(t, nil)
	plan, err := f.resolve(t, ImageBillingInput{Model: "gpt-image-2", Group: group, SizeTier: "1K", ImageEndpoint: true})
	requireImagePlanRejected(t, plan, err, "declared_by=group_image_count_billing")

	plan, err = f.resolve(t, ImageBillingInput{Model: "gpt-image-2", Group: group, SizeTier: "1K"})
	require.NoError(t, err, "非专用端点开关不生效")
	require.Equal(t, BillingModeToken, plan.Mode)

	f = newImageBillingFixture(t, []ModelPriceOverride{{
		Platform: PlatformOpenAI, ModelName: "gpt-image-2", BillingMode: BillingModeImage, Enabled: true,
		Payload: ModelPriceOverridePayload{OutputCostPerImage: imagePrice(0.05)},
	}})
	plan, err = f.resolve(t, ImageBillingInput{Model: "gpt-image-2", Group: group, SizeTier: "1K", ImageEndpoint: true})
	require.NoError(t, err)
	require.Equal(t, BillingModeImage, plan.Mode)
	require.Equal(t, ImageBillingDeclaredByGroupImageCount, plan.DeclaredBy)
	require.InDelta(t, 0.05, plan.UnitPrice, 1e-12)
}

// 4. 渠道按张、只配了 1K、请求 2K，④ 是 token → 拒绝（现状会回落到模型 token）。
func TestResolveImageBillingChannelPerImageMissingTierRejectsTokenModel(t *testing.T) {
	f := newImageBillingFixture(t, nil).withChannel(14, "gpt-image-2", &ChannelModelPricing{
		BillingMode: BillingModeImage,
		Intervals:   []PricingInterval{{TierLabel: ImageBillingSize1K, PerRequestPrice: imagePrice(0.06)}},
	})
	plan, err := f.resolve(t, ImageBillingInput{Model: "gpt-image-2", GroupID: imageInt64(14), SizeTier: "1K"})
	require.NoError(t, err)
	require.Equal(t, PricingSourceChannel, plan.Source)
	require.InDelta(t, 0.06, plan.UnitPrice, 1e-12)
	require.NotNil(t, plan.channelPricing)

	plan, err = f.resolve(t, ImageBillingInput{Model: "gpt-image-2", GroupID: imageInt64(14), SizeTier: "2K"})
	requireImagePlanRejected(t, plan, err, "declared_by=channel billing_mode=image")
}

// 5. 渠道 token 档只配了文本价：④ 是 gpt-image-2 → 用 ④ 的两项价；④ 是 Gemini（按张）→ 拒绝。
func TestResolveImageBillingChannelTokenTextOnly(t *testing.T) {
	row := &ChannelModelPricing{BillingMode: BillingModeToken, InputPrice: imagePrice(1e-6), OutputPrice: imagePrice(2e-6)}
	f := newImageBillingFixture(t, nil).withChannel(15, "gpt-image-2", row)
	plan, err := f.resolve(t, ImageBillingInput{Model: "gpt-image-2", GroupID: imageInt64(15), SizeTier: "1K"})
	require.NoError(t, err)
	require.Equal(t, BillingModeToken, plan.Mode)
	require.Equal(t, ImageBillingDeclaredByChannel, plan.DeclaredBy)
	require.InDelta(t, 8e-6, plan.ImageInputPrice, 1e-15, "渠道文本输入价不再用于提示词")
	require.InDelta(t, 3e-5, plan.ImageOutputPrice, 1e-15)

	f = newImageBillingFixture(t, nil).withChannel(15, "gemini-2.5-flash-image", row)
	plan, err = f.resolve(t, ImageBillingInput{Model: "gemini-2.5-flash-image", GroupID: imageInt64(15), SizeTier: "1K"})
	requireImagePlanRejected(t, plan, err, "declared_by=channel billing_mode=token")
}

// 6. 只有模型价格 ④。
func TestResolveImageBillingModelPriceOnly(t *testing.T) {
	f := newImageBillingFixture(t, []ModelPriceOverride{
		{
			Platform: PlatformOpenAI, ModelName: "gpt-image-2", BillingMode: BillingModeImage, Enabled: true,
			Payload: ModelPriceOverridePayload{Intervals: []PricingInterval{{TierLabel: ImageBillingSize1K, PerRequestPrice: imagePrice(0.05)}}},
		},
	})
	plan, err := f.resolve(t, ImageBillingInput{Model: "gpt-image-1.5", SizeTier: "1K"})
	require.NoError(t, err)
	require.Equal(t, BillingModeToken, plan.Mode)
	require.Equal(t, ImageBillingDeclaredByModelCatalog, plan.DeclaredBy)

	plan, err = f.resolve(t, ImageBillingInput{Model: "gemini-2.5-flash-image", SizeTier: "2K"})
	require.NoError(t, err)
	require.Equal(t, BillingModeImage, plan.Mode)
	require.InDelta(t, 0.0585, plan.UnitPrice, 1e-12)

	plan, err = f.resolve(t, ImageBillingInput{Model: "gpt-image-2", SizeTier: "1K"})
	require.NoError(t, err)
	require.Equal(t, ImageBillingDeclaredByModelOverride, plan.DeclaredBy)
	require.InDelta(t, 0.05, plan.UnitPrice, 1e-12)

	plan, err = f.resolve(t, ImageBillingInput{Model: "gpt-image-2", SizeTier: "2K"})
	requireImagePlanRejected(t, plan, err, "no per-image price for this tier")

	plan, err = f.resolve(t, ImageBillingInput{Model: "gemini-3.1-pro-preview", SizeTier: "1K"})
	requireImagePlanRejected(t, plan, err, "no layer declares")
}

// 7. Grok ⑤：内置价兜底；声明成 token 且入口只能按张时拒绝。
func TestResolveImageBillingGrokBuiltin(t *testing.T) {
	f := newImageBillingFixture(t, nil)
	plan, err := f.resolve(t, ImageBillingInput{Model: "grok-imagine-image", Platforms: []string{PlatformGrok}, SizeTier: "1K", PerImageOnly: true})
	require.NoError(t, err)
	require.Equal(t, ImageBillingDeclaredByBuiltin, plan.DeclaredBy)
	require.InDelta(t, defaultGrokImagineImagePrice1K, plan.UnitPrice, 1e-12)

	f = newImageBillingFixture(t, []ModelPriceOverride{{
		Platform: PlatformGrok, ModelName: "grok-imagine-image", BillingMode: BillingModeImage, Enabled: true,
		Payload: ModelPriceOverridePayload{OutputCostPerImage: imagePrice(0.05)},
	}})
	plan, err = f.resolve(t, ImageBillingInput{Model: "grok-imagine-image", Platforms: []string{PlatformGrok}, SizeTier: "2K", PerImageOnly: true})
	require.NoError(t, err)
	require.InDelta(t, 0.075, plan.UnitPrice, 1e-12, "覆盖价优先于内置价，2K 按 ×1.5 推算")

	f = newImageBillingFixture(t, nil).withChannel(17, "grok-imagine-image", &ChannelModelPricing{
		BillingMode: BillingModeToken, ImageInputPrice: imagePrice(1e-6), ImageOutputPrice: imagePrice(1e-5),
	})
	plan, err = f.resolve(t, ImageBillingInput{Model: "grok-imagine-image", Platforms: []string{PlatformGrok}, GroupID: imageInt64(17), SizeTier: "1K", PerImageOnly: true})
	requireImagePlanRejected(t, plan, err, "only bills per image")
}

// 8. 分组卡在 /v1/images 上生效（原 openai_image_billing_plan.go 不带 Group 的回归）。
func TestResolveOpenAIImageBillingPlanAppliesGroupCard(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)
	groupID := int64(18)
	apiKey := &APIKey{GroupID: &groupID, Group: &Group{
		ID: groupID, Platform: PlatformOpenAI, Hydrated: true, ImageRateIndependent: true, ImageRateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"gpt-image-2"}, BillingMode: BillingModeImage, PerRequestPrice: imagePrice(0.09),
		}},
	}}
	plan, err := svc.resolveOpenAIImageBillingPlan(context.Background(), apiKey, &groupID, "gpt-image-2", ImageBillingSize2K)
	require.NoError(t, err)
	require.Equal(t, BillingModeImage, plan.Mode)
	require.Equal(t, ImageBillingDeclaredByGroupCard, plan.DeclaredBy)
	require.InDelta(t, 0.09, plan.UnitPrice, 1e-12)
}

// 跨模型计费（方案 §3.2 第 3 条）：别名只认按张，或显式配置声明的按 token。
func TestResolveImageBillingCrossModelRule(t *testing.T) {
	f := newImageBillingFixture(t, []ModelPriceOverride{{
		Platform: PlatformOpenAI, ModelName: "gpt-image-1.5", BillingMode: BillingModeToken, Enabled: true,
		Payload: ModelPriceOverridePayload{InputCostPerImageToken: imagePrice(8e-6), OutputCostPerImageToken: imagePrice(3.2e-5)},
	}})
	plan, err := f.resolve(t, ImageBillingInput{Model: "gpt-image-2", SizeTier: "1K", CrossModel: true})
	requireImagePlanRejected(t, plan, err, "do not apply to another upstream model")

	plan, err = f.resolve(t, ImageBillingInput{Model: "gpt-image-1.5", SizeTier: "1K", CrossModel: true})
	require.NoError(t, err, "覆盖行是显式配置")
	require.Equal(t, ImageBillingDeclaredByModelOverride, plan.DeclaredBy)

	plan, err = f.resolve(t, ImageBillingInput{Model: "gemini-2.5-flash-image", SizeTier: "1K", CrossModel: true})
	require.NoError(t, err, "按张任何层都可以")

	f.withChannel(19, "gpt-image-2", &ChannelModelPricing{BillingMode: BillingModeToken})
	plan, err = f.resolve(t, ImageBillingInput{Model: "gpt-image-2", GroupID: imageInt64(19), SizeTier: "1K", CrossModel: true})
	require.NoError(t, err, "渠道声明的按 token 是显式配置")
}

// 补账按记录里的方式重算；配置里的方式变了就不补。
func TestResolveImageBillingForceMode(t *testing.T) {
	f := newImageBillingFixture(t, nil)
	plan, err := f.resolve(t, ImageBillingInput{Model: "gpt-image-2", SizeTier: "1K", ForceMode: BillingModeToken})
	require.NoError(t, err)
	require.Equal(t, BillingModeToken, plan.Mode)

	plan, err = f.resolve(t, ImageBillingInput{Model: "gpt-image-2", SizeTier: "1K", ForceMode: BillingModeImage})
	requireImagePlanRejected(t, plan, err, "does not match recorded billing_mode=image")
}

func TestResolveImageBillingRejectsUnknownTier(t *testing.T) {
	f := newImageBillingFixture(t, nil)
	plan, err := f.resolve(t, ImageBillingInput{Model: "gemini-2.5-flash-image", SizeTier: "8K"})
	requireImagePlanRejected(t, plan, err, "")
}

// T2.2 两项价计算。
func TestCalculateImageTokenCost(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	plan := &ImageBillingPlan{Mode: BillingModeToken, Model: "gpt-image-2", ImageInputPrice: 8e-6, ImageOutputPrice: 3e-5}
	usage := ImageTokenUsage{
		TextInputTokens: 60, ImageInputTokens: 100, CacheReadTokens: 40, CacheCreationTokens: 10,
		TextOutputTokens: 20, ImageOutputTokens: 7000,
	}

	cost, err := billing.CalculateImageTokenCost(plan, usage, 1, "", "")
	require.NoError(t, err)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
	require.InDelta(t, 60*8e-6, cost.InputCost, 1e-15)
	require.InDelta(t, 100*8e-6, cost.ImageInputCost, 1e-15)
	require.InDelta(t, 40*8e-6, cost.CacheReadCost, 1e-15, "缓存不打折")
	require.InDelta(t, 10*8e-6, cost.CacheCreationCost, 1e-15)
	require.InDelta(t, 20*3e-5, cost.OutputCost, 1e-15)
	require.InDelta(t, 7000*3e-5, cost.ImageOutputCost, 1e-15)
	total := 210*8e-6 + 7020*3e-5
	require.InDelta(t, total, cost.TotalCost, 1e-12)
	require.InDelta(t, total, cost.ActualCost, 1e-12)

	cost, err = billing.CalculateImageTokenCost(plan, usage, 1.5, "priority", "")
	require.NoError(t, err)
	require.InDelta(t, total*2, cost.TotalCost, 1e-12, "没有配置时 priority 按 2 倍")
	require.InDelta(t, total*2*1.5, cost.ActualCost, 1e-12)

	fast := 1.3
	plan.channelPricing = &ChannelModelPricing{FastMultiplier: &fast, ReasoningEffortMultipliers: map[string]float64{"high": 1.2}}
	cost, err = billing.CalculateImageTokenCost(plan, usage, 1, "priority", "high")
	require.NoError(t, err)
	require.InDelta(t, total*1.3*1.2, cost.TotalCost, 1e-12, "渠道 Fast 倍率与推理等级倍率")

	cost, err = billing.CalculateImageTokenCost(plan, usage, 0, "", "")
	require.NoError(t, err)
	require.Zero(t, cost.ActualCost)

	_, err = billing.CalculateImageTokenCost(&ImageBillingPlan{Mode: BillingModeToken, ImageInputPrice: -1, ImageOutputPrice: 1}, usage, 1, "", "")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	_, err = billing.CalculateImageTokenCost(&ImageBillingPlan{Mode: BillingModeImage, UnitPrice: 0.05}, usage, 1, "", "")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestCalculateImagePerImageCost(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	plan := &ImageBillingPlan{Mode: BillingModeImage, Model: "gpt-image-2", UnitPrice: 0.05}
	cost, err := billing.CalculateImagePerImageCost(plan, 3, 2, "max")
	require.NoError(t, err)
	require.Equal(t, string(BillingModeImage), cost.BillingMode)
	require.InDelta(t, 0.15, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.30, cost.ActualCost, 1e-12)

	plan.channelPricing = &ChannelModelPricing{ReasoningEffortMultipliers: map[string]float64{"max": 2}}
	cost, err = billing.CalculateImagePerImageCost(plan, 1, 1, "max")
	require.NoError(t, err)
	require.InDelta(t, 0.10, cost.TotalCost, 1e-12)

	_, err = billing.CalculateImagePerImageCost(plan, 0, 1, "")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

// 问题 1 回归：gpt-image-2 配 image 档 $0.05 后，token 请求不再被按 1 张收 $0.05，而是缺价拒绝。
func TestImageModeOverrideNoLongerBillsTokenRequestsPerImage(t *testing.T) {
	f := newImageBillingFixture(t, []ModelPriceOverride{{
		Platform: PlatformOpenAI, ModelName: "gpt-image-2", BillingMode: BillingModeImage, Enabled: true,
		Payload: ModelPriceOverridePayload{OutputCostPerImage: imagePrice(0.05)},
	}})
	resolved, err := f.resolver.ResolveStrictToken(context.Background(), PricingInput{Model: "gpt-image-2", Platforms: []string{PlatformOpenAI}})
	require.Nil(t, resolved)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	_, err = f.billing.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "gpt-image-2", Platforms: []string{PlatformOpenAI},
		Tokens: UsageTokens{InputTokens: 100, OutputTokens: 50}, RequestCount: 1, RateMultiplier: 1, Resolver: f.resolver,
	})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

// 问题 2 回归：渠道 token 档（带图片价）+ 模型 image 档 → 统一入口与出图计划都按渠道 token 计。
func TestChannelTokenPricingWinsOverImageModeOverride(t *testing.T) {
	f := newImageBillingFixture(t, []ModelPriceOverride{{
		Platform: PlatformOpenAI, ModelName: "gpt-image-2", BillingMode: BillingModeImage, Enabled: true,
		Payload: ModelPriceOverridePayload{OutputCostPerImage: imagePrice(0.05)},
	}}).withChannel(21, "gpt-image-2", &ChannelModelPricing{
		BillingMode: BillingModeToken, InputPrice: imagePrice(6e-6), OutputPrice: imagePrice(2e-5),
		ImageInputPrice: imagePrice(9e-6), ImageOutputPrice: imagePrice(3.5e-5),
	})
	resolved := f.resolver.Resolve(context.Background(), PricingInput{Model: "gpt-image-2", Platforms: []string{PlatformOpenAI}, GroupID: imageInt64(21)})
	require.Equal(t, BillingModeToken, resolved.Mode)
	require.Equal(t, PricingSourceChannel, resolved.Source)

	plan, err := f.resolve(t, ImageBillingInput{Model: "gpt-image-2", GroupID: imageInt64(21), SizeTier: "1K", ImageEndpoint: true})
	require.NoError(t, err)
	require.Equal(t, BillingModeToken, plan.Mode)
	require.Equal(t, ImageBillingDeclaredByChannel, plan.DeclaredBy)
	require.InDelta(t, 9e-6, plan.ImageInputPrice, 1e-15)
	require.InDelta(t, 3.5e-5, plan.ImageOutputPrice, 1e-15)
}

// T2.8：预扣按出图计划估算。档位已知按该档；按 token 按估算 token × 两项价；没配价不再出现 $0.134。
func TestInflightImageEstimateUsesImageBillingPlan(t *testing.T) {
	f := newImageBillingFixture(t, []ModelPriceOverride{{
		Platform: PlatformGemini, ModelName: "gemini-2.5-flash-image", BillingMode: BillingModeImage, Enabled: true,
		Payload: ModelPriceOverridePayload{Intervals: []PricingInterval{
			{TierLabel: ImageBillingSize1K, PerRequestPrice: imagePrice(0.03)},
			{TierLabel: ImageBillingSize4K, PerRequestPrice: imagePrice(0.08)},
		}},
	}})
	d := inflightEstimateDeps{billing: f.billing, resolver: f.resolver}
	group := &Group{ID: 41, Platform: PlatformGemini}
	apiKey := &APIKey{GroupID: &group.ID, Group: group}

	cost := d.estimateInflightImageCost(context.Background(), apiKey, "gemini-2.5-flash-image",
		InflightEstimateRequest{ImageSizeTier: ImageBillingSize1K}, 2, 0, 1, 1)
	require.InDelta(t, 0.06, cost, 1e-12, "按精确档位预扣")
	cost = d.estimateInflightImageCost(context.Background(), apiKey, "gemini-2.5-flash-image", InflightEstimateRequest{}, 1, 0, 1, 1)
	require.InDelta(t, 0.08, cost, 1e-12, "档位未知取三档最高价")

	openAIGroup := &Group{ID: 42, Platform: PlatformOpenAI}
	cost = d.estimateInflightImageCost(context.Background(), &APIKey{GroupID: &openAIGroup.ID, Group: openAIGroup}, "gpt-image-2",
		InflightEstimateRequest{ImageSizeTier: ImageBillingSize1K, ImageSize: "1024x1024", ImageQuality: "high"}, 1, 60, 1, 1)
	require.InDelta(t, 60*8e-6+7033*3e-5, cost, 1e-12, "按 token 按估算预扣")

	cost = d.estimateInflightImageCost(context.Background(), &APIKey{GroupID: &openAIGroup.ID, Group: openAIGroup}, "gpt-image-future-v99",
		InflightEstimateRequest{}, 1, 0, 1, 1)
	require.Zero(t, cost, "没配价时不再出现 $0.134")
}
