package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newOpenAIImageSettlementService(t *testing.T, overrides []ModelPriceOverride) *OpenAIGatewayService {
	t.Helper()
	f := newImageBillingFixture(t, overrides)
	return &OpenAIGatewayService{billingService: f.billing, resolver: f.resolver}
}

func settleOpenAIImageForTest(
	t *testing.T,
	svc *OpenAIGatewayService,
	result *OpenAIForwardResult,
	platforms []string,
	billingModels []string,
	kind BillingKind,
	tokens UsageTokens,
) (*CostBreakdown, error) {
	t.Helper()
	return svc.calculateOpenAIRecordUsageCostForPlatforms(
		context.Background(), result, &APIKey{}, platforms, billingModels, kind,
		1, 1, 1, 1, tokens, "", boolPtr(false), false, time.Time{},
	)
}

// Responses + gpt-image-2 工具（目录只有图片 token 价）：工具用量按两项价，主模型 token 按
// 主模型价另计（D7），合并后主模型计入文本列、工具计入图片列。
func TestOpenAIResponsesImageToolSettlesByTokenPlan(t *testing.T) {
	svc := newOpenAIImageSettlementService(t, nil)
	result := &OpenAIForwardResult{
		Model: "gpt-5.4", BillingModel: "gpt-image-2", TextBillingModel: "gpt-5.4",
		ImageCount: 1, ImageSize: ImageBillingSize1K,
		Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 200, ImageGen: &OpenAIImageGenUsage{
			InputTokens: 100, OutputTokens: 7033, ImageOutputTokens: 7033,
		}},
	}
	mainTokens := UsageTokens{InputTokens: 1000, OutputTokens: 200}
	cost, err := settleOpenAIImageForTest(t, svc, result, []string{PlatformOpenAI}, []string{"gpt-image-2", "gpt-5.4"}, BillingKindToken, mainTokens)
	require.NoError(t, err)

	mainCost, err := svc.calculateOpenAIRecordUsageTokenCost(context.Background(), &APIKey{}, []string{PlatformOpenAI}, "gpt-5.4",
		1, time.Time{}, mainTokens, "", "", boolPtr(false), false)
	require.NoError(t, err)
	toolCost := 100*8e-6 + 7033*3e-5
	require.InDelta(t, 0.21179, toolCost, 1e-12)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
	require.InDelta(t, mainCost.InputCost, cost.InputCost, 1e-12, "主模型输入按主模型价")
	require.InDelta(t, mainCost.OutputCost, cost.OutputCost, 1e-12)
	require.InDelta(t, 100*8e-6, cost.ImageInputCost, 1e-12, "工具提示词按图片输入价，计入图片输入列")
	require.InDelta(t, 7033*3e-5, cost.ImageOutputCost, 1e-12)
	require.InDelta(t, mainCost.TotalCost+toolCost, cost.TotalCost, 1e-12)
}

// 上游没单独回工具用量时，主 usage 里的图片输出 token 视为工具产出，从主模型 token 里扣掉。
func TestOpenAIResponsesImageToolWithoutToolUsageSplitsImageTokens(t *testing.T) {
	svc := newOpenAIImageSettlementService(t, nil)
	result := &OpenAIForwardResult{
		Model: "gpt-5.4", BillingModel: "gpt-image-2", TextBillingModel: "gpt-5.4",
		ImageCount: 1, ImageSize: ImageBillingSize1K,
	}
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 7233, ImageOutputTokens: 7033}
	cost, err := settleOpenAIImageForTest(t, svc, result, []string{PlatformOpenAI}, []string{"gpt-image-2", "gpt-5.4"}, BillingKindToken, tokens)
	require.NoError(t, err)
	mainCost, err := svc.calculateOpenAIRecordUsageTokenCost(context.Background(), &APIKey{}, []string{PlatformOpenAI}, "gpt-5.4",
		1, time.Time{}, UsageTokens{InputTokens: 1000, OutputTokens: 200}, "", "", boolPtr(false), false)
	require.NoError(t, err)
	require.InDelta(t, mainCost.TotalCost+7033*3e-5, cost.TotalCost, 1e-12)

	// 完全没有图片用量：按尺寸 × 质量估算工具输出（1K、未给质量按 high：7033），标记为估算。
	estimatedResult := &OpenAIForwardResult{
		Model: "gpt-5.4", BillingModel: "gpt-image-2", TextBillingModel: "gpt-5.4",
		ImageCount: 1, ImageSize: ImageBillingSize1K,
	}
	cost, err = settleOpenAIImageForTest(t, svc, estimatedResult, []string{PlatformOpenAI}, []string{"gpt-image-2", "gpt-5.4"}, BillingKindToken,
		UsageTokens{InputTokens: 1000, OutputTokens: 200})
	require.NoError(t, err)
	require.InDelta(t, mainCost.TotalCost+7033*3e-5, cost.TotalCost, 1e-12)
	require.Equal(t, ImageEstimateMethod, estimatedResult.UsageEstimationMethod)
	require.NotNil(t, estimatedResult.Usage.ImageGen)
	require.Equal(t, 7033, estimatedResult.Usage.ImageGen.ImageOutputTokens)
}

// gpt-image-2 配了 image 档 2K = $0.05：只收张数，主模型 token 不收（D4）。
func TestOpenAIResponsesImageToolSettlesPerImagePlan(t *testing.T) {
	svc := newOpenAIImageSettlementService(t, []ModelPriceOverride{{
		Platform: PlatformOpenAI, ModelName: "gpt-image-2", BillingMode: BillingModeImage, Enabled: true,
		Payload: ModelPriceOverridePayload{Intervals: []PricingInterval{{TierLabel: ImageBillingSize2K, PerRequestPrice: imagePrice(0.05)}}},
	}})
	result := &OpenAIForwardResult{
		Model: "gpt-5.4", BillingModel: "gpt-image-2", TextBillingModel: "gpt-5.4",
		ImageCount: 1, ImageSize: ImageBillingSize2K,
		Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 200, ImageGen: &OpenAIImageGenUsage{InputTokens: 100, OutputTokens: 5500, ImageOutputTokens: 5500}},
	}
	cost, err := settleOpenAIImageForTest(t, svc, result, []string{PlatformOpenAI}, []string{"gpt-image-2", "gpt-5.4"}, BillingKindToken,
		UsageTokens{InputTokens: 1000, OutputTokens: 200})
	require.NoError(t, err)
	require.Equal(t, string(BillingModeImage), cost.BillingMode)
	require.InDelta(t, 0.05, cost.TotalCost, 1e-12)
	require.Zero(t, cost.InputCost)
	require.Zero(t, cost.OutputCost)
}

// Grok imagine：内置按张价照旧；哪层把它声明成按 token 就拒绝。
func TestOpenAIGrokImageSettlementIsPerImageOnly(t *testing.T) {
	svc := newOpenAIImageSettlementService(t, nil)
	result := &OpenAIForwardResult{Model: "grok-imagine-image", ImageCount: 2, ImageSize: ImageBillingSize1K}
	cost, err := settleOpenAIImageForTest(t, svc, result, []string{PlatformGrok}, []string{"grok-imagine-image"}, BillingKindImage, UsageTokens{})
	require.NoError(t, err)
	require.InDelta(t, 2*defaultGrokImagineImagePrice1K, cost.TotalCost, 1e-12)

	svc = newOpenAIImageSettlementService(t, []ModelPriceOverride{{
		Platform: PlatformGrok, ModelName: "grok-imagine-image", BillingMode: BillingModeToken, Enabled: true,
		Payload: ModelPriceOverridePayload{InputCostPerImageToken: imagePrice(1e-6), OutputCostPerImageToken: imagePrice(1e-5)},
	}})
	_, err = settleOpenAIImageForTest(t, svc, result, []string{PlatformGrok}, []string{"grok-imagine-image"}, BillingKindImage,
		UsageTokens{OutputTokens: 1000, ImageOutputTokens: 1000})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, "only bills per image")

	cost, err = settleOpenAIImageForTest(t, svc, &OpenAIForwardResult{Model: "grok-imagine-image"}, []string{PlatformGrok},
		[]string{"grok-imagine-image"}, BillingKindImage, UsageTokens{})
	require.NoError(t, err, "一张都没出不收费")
	require.Zero(t, cost.TotalCost)
}

// 主模型候选：记下了 TextBillingModel 时先用它，并去掉生图工具的模型。
func TestOpenAIResponsesTextBillingModels(t *testing.T) {
	result := &OpenAIForwardResult{TextBillingModel: "gpt-5.4", UpstreamModel: "gpt-5.4-2026", Model: "gpt-5.4"}
	require.Equal(t, []string{"gpt-5.4", "alias", "gpt-5.4-2026"},
		openAIResponsesTextBillingModels(result, []string{"gpt-image-2", "alias", "GPT-IMAGE-2"}, "gpt-image-2"))
	require.Equal(t, []string{"gpt-5.1"},
		openAIResponsesTextBillingModels(&OpenAIForwardResult{}, []string{"gpt-5.1"}, "gpt-5.1"), "没有切换记录时原样使用")
}
