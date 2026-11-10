package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEstimateImageOutputTokensTable(t *testing.T) {
	for _, tc := range []struct {
		model, size, quality string
		want                 int
	}{
		{"gpt-image-1", "1024x1024", "low", 272},
		{"gpt-image-1.5", "1024x1536", "medium", 1584},
		{"gpt-image-1", "1536x1024", "high", 6208},
		{"gpt-image-1-mini", "1024x1024", "high", 4500},
		{"gpt-image-1-mini", "1536x1024", "low", 750},
		{"gpt-image-2", "1024x1024", "high", 7033},
		{"gpt-image-2", "1024x1536", "medium", 1367},
		{"gpt-image-2.5-flare", "1536x1024", "xhigh", 5500},
		{"gpt-image-2.6-sunburst", "1024x1024", "max", 7033},
		{"gemini-2.5-flash-image", "1K", "", 1290},
		{"gemini-3-pro-image-preview", "4K", "", 2000},
		{"models/gemini-3.1-flash-image-preview", "2K", "", 1680},
		{"gemini-3.1-flash-image", "0.5K", "", 747},
		{"gemini-3.1-flash-lite-image", "1K", "", 1120},
		{"gemini-nano-banana-2.1", "4K", "", 3780},
	} {
		got, basis := EstimateImageOutputTokens(tc.model, tc.size, tc.quality, 0)
		require.Equal(t, tc.want, got, "%s %s %s", tc.model, tc.size, tc.quality)
		require.Equal(t, imageEstimateBasisTable, basis)
	}
}

func TestEstimateImageOutputTokensRules(t *testing.T) {
	square, _ := EstimateImageOutputTokens("gpt-image-2", "1024x1024", "high", 0)
	big, basis := EstimateImageOutputTokens("gpt-image-2", "2048x2048", "high", 0)
	require.Equal(t, 4*square, big, "按面积折算")
	require.Equal(t, imageEstimateBasisArea, basis)

	auto, _ := EstimateImageOutputTokens("gpt-image-2", "1024x1024", "auto", 0)
	require.Equal(t, square, auto, "auto 按 high")
	withPartials, _ := EstimateImageOutputTokens("gpt-image-2", "1024x1024", "high", 2)
	require.Equal(t, square+200, withPartials, "每个 partial_images 加 100")

	unknown, basis := EstimateImageOutputTokens("flux-pro", "1024x1024", "high", 0)
	require.Equal(t, 4160, unknown, "未知模型按 gpt-image-1 表")
	require.Equal(t, imageEstimateBasisFallback, basis)

	unlisted, _ := EstimateImageOutputTokens("gemini-2.5-flash-image", "4K", "", 0)
	require.Equal(t, 1290, unlisted, "Gemini 没列出的档位取已列出的最大值")
	require.LessOrEqual(t, len(ImageEstimateMethod), 16, "usage_estimation_method 列宽 VARCHAR(16)")
}

func TestEstimateImageInputTokens(t *testing.T) {
	require.Equal(t, 60+765, EstimateImageInputTokens("gpt-image-2", 60, []ImageDims{{Width: 1024, Height: 1024}}))
	require.Equal(t, 30+2*560, EstimateImageInputTokens("gemini-3-pro-image-preview", 30, []ImageDims{{}, {}}))
}

// 场景 2：/v1/images gpt-image-2 1024² high，上游没回 usage：输出估算 7033，提示词按本地
// tokenizer 数，标记为估算。
func TestOpenAIImagesTokenPlanEstimatesMissingUsage(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)
	plan, err := svc.resolveOpenAIImageBillingPlan(context.Background(), nil, nil, "gpt-image-2", ImageBillingSize1K)
	require.NoError(t, err)
	prompt := "a watercolor fox reading a book under a maple tree"
	promptTokens, _ := estimateFallbackTextTokens("gpt-image-2", []byte(prompt))
	require.Positive(t, promptTokens)

	result := &OpenAIForwardResult{
		Model: "gpt-image-2", ImageBillingPlan: plan, ImageCount: 1, ImageEndpoint: true,
		ImageSize: ImageBillingSize1K, ImageInputSize: "1024x1024", ImageQuality: "high",
		ImageEstimateInput: &ImageEstimateInput{Prompt: prompt},
	}
	cost, err := svc.calculateOpenAIImageCostFromPlan(context.Background(), result, plan, nil, UsageTokens{}, 1, 1, "", false)
	require.NoError(t, err)
	require.InDelta(t, float64(promptTokens)*8e-6+7033*3e-5, cost.TotalCost, 1e-12)
	require.Equal(t, UsageSourceEstimated, result.UsageSource)
	require.Equal(t, ImageEstimateMethod, result.UsageEstimationMethod)
	require.Equal(t, promptTokens, result.Usage.InputTokens)
	require.Equal(t, 7033, result.Usage.ImageOutputTokens)
}

// Gemini 覆盖成 token 档、上游没回 usageMetadata：按档位估算输出 token。
func TestGatewayGeminiTokenPlanEstimatesMissingUsage(t *testing.T) {
	svc := newGatewayImageBillingService(t, []ModelPriceOverride{{
		Platform: PlatformGemini, ModelName: "gemini-3-pro-image-preview", BillingMode: BillingModeToken, Enabled: true,
		Payload: ModelPriceOverridePayload{InputCostPerImageToken: imagePrice(2e-6), OutputCostPerImageToken: imagePrice(1.2e-4)},
	}})
	result := &ForwardResult{ImageCount: 1, ImageSize: ImageBillingSize4K, UpstreamModel: "gemini-3-pro-image-preview",
		Usage: ClaudeUsage{InputTokens: 300}}
	cost, err := svc.calculateGatewayImageCost(context.Background(), result, nil, []string{PlatformGemini}, "gemini-3-pro-image-preview", 1, 1)
	require.NoError(t, err)
	require.InDelta(t, 300*2e-6+2000*1.2e-4, cost.TotalCost, 1e-12, "保留实际输入，输出按 4K 估 2000")
	require.Equal(t, UsageSourceEstimated, result.UsageSource)
	require.Equal(t, ImageEstimateMethod, result.UsageEstimationMethod)
	require.Equal(t, 2000, result.Usage.ImageOutputTokens)
}
