package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 图片计费二选一验收场景（docs/图片计费二选一实施计划.md T5.2），倍率均为 1。
// 用 go test -run TestImageBillingAcceptanceScenarios -v 可以看到每个场景的实际金额。
func TestImageBillingAcceptanceScenarios(t *testing.T) {
	ctx := context.Background()
	report := func(no int, scenario string, actual string) {
		t.Logf("| %d | %s | %s |", no, scenario, actual)
	}
	usd := func(v float64) string { return fmt.Sprintf("$%.6g", v) }

	gptImage2PerImage := func(payload ModelPriceOverridePayload) []ModelPriceOverride {
		return []ModelPriceOverride{{
			Platform: PlatformOpenAI, ModelName: "gpt-image-2", BillingMode: BillingModeImage, Enabled: true, Payload: payload,
		}}
	}

	t.Run("01 images token usage", func(t *testing.T) {
		svc := newOpenAIImageSettlementService(t, nil)
		plan, err := svc.resolveOpenAIImageBillingPlan(ctx, nil, nil, "gpt-image-2", ImageBillingSize1K)
		require.NoError(t, err)
		result := &OpenAIForwardResult{ImageBillingPlan: plan, ImageCount: 1, ImageEndpoint: true, ImageSize: ImageBillingSize1K}
		cost, err := svc.calculateOpenAIImageCostFromPlan(ctx, result, plan, nil,
			UsageTokens{InputTokens: 60, OutputTokens: 7000, ImageOutputTokens: 7000}, 1, 1, "", false)
		require.NoError(t, err)
		require.InDelta(t, 0.21048, cost.TotalCost, 1e-12)
		report(1, "/v1/images gpt-image-2 1024² high，usage 输入 60 / 输出 7000", usd(cost.TotalCost)+"（按 token）")
	})

	t.Run("02 images token estimated", func(t *testing.T) {
		svc := newOpenAIImageSettlementService(t, nil)
		plan, err := svc.resolveOpenAIImageBillingPlan(ctx, nil, nil, "gpt-image-2", ImageBillingSize1K)
		require.NoError(t, err)
		prompt := "A cozy reading nook by a rainy window, warm lamp light, a sleeping cat on a knitted blanket, " +
			"watercolor style with soft edges, muted autumn palette, shelves full of old books, a steaming mug of tea"
		promptTokens, _ := estimateFallbackTextTokens("gpt-image-2", []byte(prompt))
		result := &OpenAIForwardResult{ImageBillingPlan: plan, ImageCount: 1, ImageEndpoint: true, ImageSize: ImageBillingSize1K,
			ImageInputSize: "1024x1024", ImageQuality: "high", ImageEstimateInput: &ImageEstimateInput{Prompt: prompt}}
		cost, err := svc.calculateOpenAIImageCostFromPlan(ctx, result, plan, nil, UsageTokens{}, 1, 1, "", false)
		require.NoError(t, err)
		require.InDelta(t, float64(promptTokens)*8e-6+7033*3e-5, cost.TotalCost, 1e-12)
		require.Equal(t, UsageSourceEstimated, result.UsageSource)
		require.Equal(t, ImageEstimateMethod, result.UsageEstimationMethod)
		report(2, "同上，上游没给 usage，quality=high", fmt.Sprintf("输出估算 7033、提示词 %d token：%s；usage_source=estimated", promptTokens, usd(cost.TotalCost)))
	})

	t.Run("03 responses tool token", func(t *testing.T) {
		svc := newOpenAIImageSettlementService(t, nil)
		mainTokens := UsageTokens{InputTokens: 1000, OutputTokens: 200}
		result := &OpenAIForwardResult{
			Model: "gpt-5.4", BillingModel: "gpt-image-2", TextBillingModel: "gpt-5.4", ImageCount: 1, ImageSize: ImageBillingSize1K,
			Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 200, ImageGen: &OpenAIImageGenUsage{InputTokens: 100, OutputTokens: 7033, ImageOutputTokens: 7033}},
		}
		cost, err := settleOpenAIImageForTest(t, svc, result, []string{PlatformOpenAI}, []string{"gpt-image-2", "gpt-5.4"}, BillingKindToken, mainTokens)
		require.NoError(t, err)
		mainCost, err := svc.calculateOpenAIRecordUsageTokenCost(ctx, &APIKey{}, []string{PlatformOpenAI}, "gpt-5.4", 1, time.Time{}, mainTokens, "", "", boolPtr(false), false)
		require.NoError(t, err)
		require.InDelta(t, mainCost.TotalCost+0.21179, cost.TotalCost, 1e-12)
		report(3, "/responses（gpt-5.4）+ gpt-image-2 工具，工具 usage 100 / 7033，主模型 1000 / 200",
			fmt.Sprintf("主模型 %s + 工具 $0.21179 = %s", usd(mainCost.TotalCost), usd(cost.TotalCost)))
	})

	t.Run("04 responses tool per image", func(t *testing.T) {
		svc := newOpenAIImageSettlementService(t, gptImage2PerImage(ModelPriceOverridePayload{
			Intervals: []PricingInterval{{TierLabel: ImageBillingSize2K, PerRequestPrice: imagePrice(0.05)}},
		}))
		result := &OpenAIForwardResult{
			Model: "gpt-5.4", BillingModel: "gpt-image-2", TextBillingModel: "gpt-5.4", ImageCount: 1, ImageSize: "1024x1536",
			Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 200, ImageGen: &OpenAIImageGenUsage{InputTokens: 100, OutputTokens: 5500, ImageOutputTokens: 5500}},
		}
		cost, err := settleOpenAIImageForTest(t, svc, result, []string{PlatformOpenAI}, []string{"gpt-image-2", "gpt-5.4"}, BillingKindToken,
			UsageTokens{InputTokens: 1000, OutputTokens: 200})
		require.NoError(t, err)
		require.InDelta(t, 0.05, cost.TotalCost, 1e-12)
		require.Zero(t, cost.InputCost)
		report(4, "同上，gpt-image-2 配 image 档 2K = $0.05，出图 1024×1536", usd(cost.TotalCost)+"，主模型 token 不收")
	})

	t.Run("05-07 gemini", func(t *testing.T) {
		svc := newGatewayImageBillingService(t, nil)
		result := &ForwardResult{ImageCount: 1, UpstreamModel: "gemini-2.5-flash-image", Usage: ClaudeUsage{InputTokens: 500, OutputTokens: 1310, ImageOutputTokens: 1290}}
		cost, err := svc.calculateGatewayImageCost(ctx, result, nil, []string{PlatformGemini}, "gemini-2.5-flash-image", 1, 1)
		require.NoError(t, err)
		require.InDelta(t, 0.0585, cost.TotalCost, 1e-12)
		report(5, "Gemini 原生 gemini-2.5-flash-image，无覆盖，没写 imageSize", usd(cost.TotalCost)+"（按 2K）")

		svc = newGatewayImageBillingService(t, []ModelPriceOverride{{
			Platform: PlatformGemini, ModelName: "gemini-2.5-flash-image", BillingMode: BillingModeImage, Enabled: true,
			Payload: ModelPriceOverridePayload{Intervals: []PricingInterval{
				{TierLabel: ImageBillingSize1K, PerRequestPrice: imagePrice(0.03)},
				{TierLabel: ImageBillingSize2K, PerRequestPrice: imagePrice(0.05)},
				{TierLabel: ImageBillingSize4K, PerRequestPrice: imagePrice(0.08)},
			}},
		}})
		result.ImageSize = ImageBillingSize4K
		cost, err = svc.calculateGatewayImageCost(ctx, result, nil, []string{PlatformGemini}, "gemini-2.5-flash-image", 1, 1)
		require.NoError(t, err)
		require.InDelta(t, 0.08, cost.TotalCost, 1e-12)
		report(6, "同上，覆盖 image 档 1K/2K/4K = 0.03/0.05/0.08，请求 4K", usd(cost.TotalCost))

		svc = newGatewayImageBillingService(t, []ModelPriceOverride{{
			Platform: PlatformGemini, ModelName: "gemini-2.5-flash-image", BillingMode: BillingModeToken, Enabled: true,
			Payload: ModelPriceOverridePayload{InputCostPerImageToken: imagePrice(3e-7), OutputCostPerImageToken: imagePrice(3e-5)},
		}})
		cost, err = svc.calculateGatewayImageCost(ctx, result, nil, []string{PlatformGemini}, "gemini-2.5-flash-image", 1, 1)
		require.NoError(t, err)
		require.InDelta(t, 0.03945, cost.TotalCost, 1e-12)
		report(7, "同上，覆盖 token 档（输入 3e-7、输出 3e-5），usage 500 / 1310", usd(cost.TotalCost))
	})

	t.Run("08 antigravity text only", func(t *testing.T) {
		svc := newGatewayImageBillingService(t, nil)
		// D6：Antigravity 只回文字也按 1 张计（计数在准入身份里固定为 1），档位默认 2K。
		result := &ForwardResult{ImageCount: 1, ImageSize: ImageBillingSize2K, UpstreamModel: "gemini-3-pro-image"}
		cost, err := svc.calculateGatewayImageCost(ctx, result, nil, []string{PlatformAntigravity}, "gemini-3-pro-image", 1, 1)
		require.NoError(t, err)
		require.InDelta(t, 0.201, cost.TotalCost, 1e-12)
		report(8, "Antigravity gemini-3-pro-image 只回了文字", "1 张 × 2K："+usd(cost.TotalCost))
	})

	t.Run("09 batch", func(t *testing.T) {
		f := newImageBillingFixture(t, []ModelPriceOverride{{
			Platform: PlatformGemini, ModelName: "gemini-2.5-flash-image", BillingMode: BillingModeImage, Enabled: true,
			Payload: ModelPriceOverridePayload{Intervals: []PricingInterval{{TierLabel: ImageBillingSize1K, PerRequestPrice: imagePrice(0.05)}}},
		}})
		price, err := (&BatchImageModelPricingResolver{Resolver: f.resolver}).BatchImageUnitPrice(ctx,
			&BatchImageJob{Provider: BatchImageProviderGeminiAPI, Model: "gemini-2.5-flash-image"})
		require.NoError(t, err)
		require.InDelta(t, 0.05, price, 1e-12)
		report(9, "批量出图 gemini-2.5-flash-image，覆盖 1K 档位价 $0.05", "每张 "+usd(price))
	})

	t.Run("10 image mode rejects token request", func(t *testing.T) {
		f := newImageBillingFixture(t, gptImage2PerImage(ModelPriceOverridePayload{OutputCostPerImage: imagePrice(0.05)}))
		_, err := f.resolver.ResolveStrictToken(ctx, PricingInput{Model: "gpt-image-2", Platforms: []string{PlatformOpenAI}})
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		report(10, "gpt-image-2 配 image 档 $0.05，发 token 请求", "准入拒绝（缺价）")
	})

	t.Run("11 image count switch", func(t *testing.T) {
		svc := newOpenAIImageSettlementService(t, nil)
		groupID := int64(51)
		apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI, Hydrated: true, ImageCountBilling: true,
			ImageRateIndependent: true, ImageRateMultiplier: 1}}
		_, err := svc.resolveOpenAIImageBillingPlan(ctx, apiKey, &groupID, "gpt-image-2", ImageBillingSize1K)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		plan, err := svc.resolveOpenAIImagePlan(ctx, apiKey, &groupID, []string{PlatformOpenAI}, "gpt-image-2", ImageBillingSize1K, false, false)
		require.NoError(t, err)
		require.Equal(t, BillingModeToken, plan.Mode)
		report(11, "分组开关开启，gpt-image-2 只有 token 价", "/v1/images 拒绝；/responses 按 token")
	})

	t.Run("12 channel per-image missing tier", func(t *testing.T) {
		f := newImageBillingFixture(t, nil).withChannel(52, "gpt-image-2", &ChannelModelPricing{
			BillingMode: BillingModeImage, Intervals: []PricingInterval{{TierLabel: ImageBillingSize1K, PerRequestPrice: imagePrice(0.06)}},
		})
		_, err := f.resolver.ResolveImageBilling(ctx, ImageBillingInput{Model: "gpt-image-2", Platforms: []string{PlatformOpenAI},
			GroupID: imageInt64(52), SizeTier: ImageBillingSize2K, ImageEndpoint: true})
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		report(12, "渠道按张只配了 1K，gpt-image-2 请求 2K", "拒绝")
	})

	t.Run("13 grok", func(t *testing.T) {
		grok := func(overrides []ModelPriceOverride, tier string) (*ImageBillingPlan, error) {
			f := newImageBillingFixture(t, overrides)
			return f.resolver.ResolveImageBilling(ctx, ImageBillingInput{Model: "grok-imagine-image", Platforms: []string{PlatformGrok}, SizeTier: tier, PerImageOnly: true})
		}
		builtin, err := grok(nil, ImageBillingSize1K)
		require.NoError(t, err)
		disabledRow := []ModelPriceOverride{{Platform: PlatformGrok, ModelName: "grok-imagine-image", BillingMode: BillingModeImage, Enabled: false,
			Payload: ModelPriceOverridePayload{OutputCostPerImage: imagePrice(0.05)}}}
		disabled, err := grok(disabledRow, ImageBillingSize1K)
		require.NoError(t, err)
		enabledRow := []ModelPriceOverride{{Platform: PlatformGrok, ModelName: "grok-imagine-image", BillingMode: BillingModeImage, Enabled: true,
			Payload: ModelPriceOverridePayload{OutputCostPerImage: imagePrice(0.05)}}}
		at1K, err := grok(enabledRow, ImageBillingSize1K)
		require.NoError(t, err)
		at2K, err := grok(enabledRow, ImageBillingSize2K)
		require.NoError(t, err)
		f := newImageBillingFixture(t, nil).withChannel(53, "grok-imagine-image", &ChannelModelPricing{
			BillingMode: BillingModeToken, ImageInputPrice: imagePrice(1e-6), ImageOutputPrice: imagePrice(1e-5),
		})
		_, err = f.resolver.ResolveImageBilling(ctx, ImageBillingInput{Model: "grok-imagine-image", Platforms: []string{PlatformGrok},
			GroupID: imageInt64(53), SizeTier: ImageBillingSize1K, PerImageOnly: true})
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.InDelta(t, 0.02, builtin.UnitPrice, 1e-12)
		require.InDelta(t, 0.02, disabled.UnitPrice, 1e-12)
		require.InDelta(t, 0.05, at1K.UnitPrice, 1e-12)
		require.InDelta(t, 0.075, at2K.UnitPrice, 1e-12)
		report(13, "grok-imagine-image 1K", fmt.Sprintf("内置 %s（覆盖行停用时同）；覆盖 image 档 0.05：1K %s、2K %s；渠道声明 token：拒绝",
			usd(builtin.UnitPrice), usd(at1K.UnitPrice), usd(at2K.UnitPrice)))
	})

	t.Run("14 plaza", func(t *testing.T) {
		f := newImageBillingFixture(t, nil)
		gemini := ResolvePlazaMediaPricing(f.pricing, PlatformGemini, "gemini-2.5-flash-image", nil, AvailableGroupRef{})
		gptImage := ResolvePlazaMediaPricing(f.pricing, PlatformOpenAI, "gpt-image-2", nil, AvailableGroupRef{})
		require.Equal(t, BillingModeImage, gemini.ImageBillingMode)
		require.Nil(t, gemini.ImageOutputPrice)
		require.Equal(t, BillingModeToken, gptImage.ImageBillingMode)
		require.Empty(t, gptImage.ImageTiers)
		report(14, "模型广场", fmt.Sprintf("Gemini 只有档位价（%d 档）；gpt-image-2 只有两项 token 价", len(gemini.ImageTiers)))
	})

	t.Run("15 admin", func(t *testing.T) {
		f := newImageBillingFixture(t, nil)
		_, err := f.pricing.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeImage,
			&ModelPriceOverridePayload{OutputCostPerImage: imagePrice(0.04), InputCostPerToken: imagePrice(3e-7)}, true)
		requireAppErrorReason(t, err, "FIELD_NOT_IN_BILLING_MODE")
		ignored := overrideIgnoredFields(&ModelPriceOverride{BillingMode: BillingModeToken,
			Payload: ModelPriceOverridePayload{InputCostPerToken: imagePrice(5e-6), OutputCostPerImage: imagePrice(0.1)}})
		require.Equal(t, []string{"output_cost_per_image"}, ignored)
		report(15, "后台", "image 档写文本价报 FIELD_NOT_IN_BILLING_MODE；存量 token 行的按张价列为被忽略字段")
	})

	t.Run("16 group tiers", func(t *testing.T) {
		svc := newOpenAIImageSettlementService(t, nil)
		groupID := int64(56)
		apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI, Hydrated: true,
			ImageRateIndependent: true, ImageRateMultiplier: 1,
			ImagePrice1K: imagePrice(0.10), ImagePrice2K: imagePrice(0.15), ImagePrice4K: imagePrice(0.30)}}
		plan, err := svc.resolveOpenAIImageBillingPlan(ctx, apiKey, &groupID, "gpt-image-2", ImageBillingSize2K)
		require.NoError(t, err)
		images, err := svc.calculateOpenAIImageCostFromPlan(ctx, &OpenAIForwardResult{ImageCount: 1}, plan, apiKey, UsageTokens{}, 1, 1, "", false)
		require.NoError(t, err)
		result := &OpenAIForwardResult{Model: "gpt-5.4", BillingModel: "gpt-image-2", TextBillingModel: "gpt-5.4", ImageCount: 1, ImageSize: ImageBillingSize2K}
		responses, err := svc.calculateOpenAIRecordUsageCostForPlatforms(ctx, result, apiKey, []string{PlatformOpenAI}, []string{"gpt-image-2", "gpt-5.4"},
			BillingKindToken, 1, 1, 1, 1, UsageTokens{InputTokens: 1000, OutputTokens: 200}, "", boolPtr(false), false, time.Time{})
		require.NoError(t, err)
		require.InDelta(t, 0.15, images.TotalCost, 1e-12)
		require.InDelta(t, 0.15, responses.TotalCost, 1e-12)
		report(16, "分组 1K/2K/4K = 0.10/0.15/0.30；/v1/images 2K 与 /responses 出 1 张 2K",
			fmt.Sprintf("都是 %s，主模型 token 不收", usd(responses.TotalCost)))
	})

	t.Run("17 migrated gpt-image-2-1k", func(t *testing.T) {
		svc := newOpenAIImageSettlementService(t, []ModelPriceOverride{{
			Platform: PlatformOpenAI, ModelName: "gpt-image-2-1k", BillingMode: BillingModeImage, Enabled: true,
			Payload: ModelPriceOverridePayload{OutputCostPerImage: imagePrice(0.10)},
		}})
		plan, err := svc.resolveOpenAIImageBillingPlanForPlatforms(ctx, nil, nil, []string{PlatformOpenAI}, "gpt-image-2-1k", ImageBillingSize1K)
		require.NoError(t, err)
		cost, err := svc.calculateOpenAIImageCostFromPlan(ctx, &OpenAIForwardResult{ImageCount: 1}, plan, nil, UsageTokens{}, 1, 1, "", false)
		require.NoError(t, err)
		require.InDelta(t, 0.10, cost.TotalCost, 1e-12)
		report(17, "迁移后的 gpt-image-2-1k（image 档 0.10），分组没有图片价，/v1/images 1K", usd(cost.TotalCost))
	})

	t.Run("18 composite alias", func(t *testing.T) {
		svc := newGatewayImageBillingService(t, productionImageOverridesForTest())
		group := &Group{ID: 58, Platform: PlatformComposite}
		apiKey := &APIKey{GroupID: &group.ID, Group: group}
		platforms := []string{PlatformComposite, PlatformAntigravity}
		const upstream = "gemini-3-pro-image-preview"
		model := svc.billableImageModelWithFallbackForPlatforms(ctx, apiKey, platforms, "gpt-image-2", ImageBillingSize2K, upstream, upstream)
		cost, err := svc.calculateGatewayImageCost(ctx, &ForwardResult{ImageCount: 1, ImageSize: ImageBillingSize2K, UpstreamModel: upstream},
			apiKey, platforms, model, 1, 1)
		require.NoError(t, err)
		require.Equal(t, upstream, model)
		require.InDelta(t, 0.201, cost.TotalCost, 1e-12)
		report(18, "组合分组：别名 gpt-image-2 转发到 Antigravity gemini-3-pro-image-preview，2K", usd(cost.TotalCost)+"，按实际转发模型")
	})

	t.Run("19 group without image price", func(t *testing.T) {
		svc := newOpenAIImageSettlementService(t, productionImageOverridesForTest())
		groupID := int64(59)
		apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI, Hydrated: true, ImageRateIndependent: true, ImageRateMultiplier: 1}}
		plan, err := svc.resolveOpenAIImageBillingPlan(ctx, apiKey, &groupID, "gpt-image-2", ImageBillingSize1K)
		require.NoError(t, err)
		cost, err := svc.calculateOpenAIImageCostFromPlan(ctx, &OpenAIForwardResult{ImageCount: 1}, plan, apiKey, UsageTokens{}, 1, 1, "", false)
		require.NoError(t, err)
		require.Equal(t, BillingModeImage, plan.Mode)
		require.InDelta(t, 0.05, cost.TotalCost, 1e-12)
		report(19, "OpenAI 分组、没有分组图片价（同分组 26），gpt-image-2 /v1/images 1K，上游没回 usage", usd(cost.TotalCost)+"（R1 后按张）")
	})
}
