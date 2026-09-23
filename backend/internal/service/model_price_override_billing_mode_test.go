package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// 与目录里 gemini-2.5-flash-image 同形：完整 token 价 + 图片 token 价。
const billingModeCatalogJSON = `{
	"gemini-2.5-flash-image": {"litellm_provider": "vertex_ai-language-models", "mode": "image_generation",
		"input_cost_per_token": 3e-07, "output_cost_per_token": 2.5e-06,
		"output_cost_per_image_token": 3e-05},
	"claude-sonnet-4": {"litellm_provider": "anthropic", "mode": "chat",
		"input_cost_per_token": 3e-06, "output_cost_per_token": 1.5e-05}
}`

func newBillingModeTestService(t *testing.T) *PricingService {
	t.Helper()
	svc := newStubPricingServiceFromJSON(t, billingModeCatalogJSON)
	svc.catalogData = svc.pricingData
	return svc
}

func requireAppErrorReason(t *testing.T, err error, reason string) {
	t.Helper()
	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, reason, appErr.Reason)
}

func TestNormalizeModelPriceOverrideBillingMode(t *testing.T) {
	mode, err := NormalizeModelPriceOverrideBillingMode("")
	require.NoError(t, err)
	require.Equal(t, BillingModeToken, mode)

	mode, err = NormalizeModelPriceOverrideBillingMode(" Image ")
	require.NoError(t, err)
	require.Equal(t, BillingModeImage, mode)

	_, err = NormalizeModelPriceOverrideBillingMode("per_request")
	requireAppErrorReason(t, err, "INVALID_BILLING_MODE")
}

// 图片模型既可能按图片 token 计价，也可能按普通 token 计价，所以 image 档接受
// token 字段；只有 video_pricing 是 video 档专属。
func TestBillingModeImageAcceptsTokenFieldsButNotVideoFields(t *testing.T) {
	svc := newBillingModeTestService(t)
	_, err := svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeImage,
		&ModelPriceOverridePayload{OutputCostPerImageToken: ptrPrice(3e-5), OutputCostPerToken: ptrPrice(2e-6)}, true)
	require.NoError(t, err, "出图按普通 token 计价是正常配置")

	// image 档不接受 video_pricing：视频价属于 video 档
	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeImage,
		&ModelPriceOverridePayload{VideoPricing: seedanceVideoPricing()}, true)
	requireAppErrorReason(t, err, "FIELD_NOT_IN_BILLING_MODE")

	// 停用的行同样不允许写入其他档字段
	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeVideo,
		&ModelPriceOverridePayload{OutputCostPerImage: ptrPrice(0.04)}, false)
	requireAppErrorReason(t, err, "FIELD_NOT_IN_BILLING_MODE")

	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeVideo,
		&ModelPriceOverridePayload{OutputCostPerToken: ptrPrice(2e-6)}, false)
	requireAppErrorReason(t, err, "FIELD_NOT_IN_BILLING_MODE")
}

func TestBillingModeTokenAcceptsMixedFieldsForLegacyClients(t *testing.T) {
	svc := newBillingModeTestService(t)
	_, err := svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{
			InputCostPerToken: ptrPrice(3e-7), OutputCostPerToken: ptrPrice(2e-6), OutputCostPerImageToken: ptrPrice(3e-5),
		}, true)
	require.NoError(t, err)
}

// 只有 video 档抑制目录 token 价，所以只要它需要这条提示。
func TestBillingModeVideoWarnsWhenSuppressingCatalogTokenPrice(t *testing.T) {
	svc := newBillingModeTestService(t)
	warnings, err := svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeImage,
		&ModelPriceOverridePayload{OutputCostPerImageToken: ptrPrice(2.5e-5)}, true)
	require.NoError(t, err)
	require.Empty(t, warnings, "image 档不抑制 token 价，无需提示")

	warnings, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeVideo,
		&ModelPriceOverridePayload{VideoPricing: seedanceVideoPricing()}, true)
	require.NoError(t, err)
	require.Contains(t, warnings, ModelPriceWarning{Code: "BILLING_MODE_SUPPRESSES_TOKEN", Field: "billing_mode"})
}

func TestBillingModeEmptyPayload(t *testing.T) {
	svc := newBillingModeTestService(t)

	// 只用目录图片价：合法
	_, err := svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeImage,
		&ModelPriceOverridePayload{}, true)
	require.NoError(t, err)

	// 目录里只有 token 价：image 档照样成立（出图按普通 token 计价）
	_, err = svc.validateOverrideWrite("*", "claude-sonnet-4", ModelPriceCurrencyUSD, BillingModeImage,
		&ModelPriceOverridePayload{}, true)
	require.NoError(t, err)

	// video 档要求 video_pricing：目录里的 token / 图片价都顶不上
	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeVideo,
		&ModelPriceOverridePayload{}, true)
	requireAppErrorReason(t, err, "EMPTY_PRICING")

	// token 档空 payload 仍是“全部继承”
	_, err = svc.validateOverrideWrite("*", "claude-sonnet-4", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{}, true)
	require.NoError(t, err)
}

func TestBuildOverrideEntrySuppressesOtherBillingModes(t *testing.T) {
	svc := newBillingModeTestService(t)
	base := svc.catalogData["gemini-2.5-flash-image"]
	base.VideoPricing = seedanceVideoPricing()

	// image 档保留目录 token 价（出图可能就按普通 token 计价），只屏蔽目录视频价兜底。
	image := buildOverrideModelPriceEntry("gemini-2.5-flash-image", base, &ModelPriceOverride{
		Currency: ModelPriceCurrencyUSD, BillingMode: BillingModeImage, Enabled: true,
	})
	require.False(t, image.TokenPricingAbsent)
	require.True(t, image.InputPriceExplicit)
	require.True(t, image.OutputPriceExplicit)
	require.Equal(t, 3e-07, image.InputCostPerToken)
	require.Equal(t, 2.5e-06, image.OutputCostPerToken)
	require.True(t, image.ImageOutputPriceExplicit)
	require.Equal(t, 3e-05, image.OutputCostPerImageToken)
	require.NotNil(t, image.VideoPricing, "video_pricing 永不清空")
	require.True(t, image.VideoPricing.Enabled, "技术档案须保持可用，渠道/分组视频定价依赖它")
	require.Equal(t, BillingModeImage, image.BillingMode)
	require.False(t, catalogVideoPricingActive(image), "image 档不得以目录视频价报价")
	require.False(t, hasVideoPricing(image))
	require.True(t, catalogVideoPricingActive(base))

	video := buildOverrideModelPriceEntry("gemini-2.5-flash-image", base, &ModelPriceOverride{
		Currency: ModelPriceCurrencyUSD, BillingMode: BillingModeVideo, Enabled: true,
	})
	require.True(t, video.TokenPricingAbsent)
	require.False(t, video.ImageOutputPriceExplicit)
	require.Zero(t, video.OutputCostPerImageToken)
	require.True(t, video.VideoPricing.Enabled)
	require.True(t, catalogVideoPricingActive(video))

	// 按 token 计费的图片模型：image 档 + 只写 token 字段，不必再填图片字段
	tokenPricedImage := buildOverrideModelPriceEntry("claude-sonnet-4", svc.catalogData["claude-sonnet-4"], &ModelPriceOverride{
		Currency: ModelPriceCurrencyUSD, BillingMode: BillingModeImage, Enabled: true,
		Payload: ModelPriceOverridePayload{OutputCostPerToken: ptrPrice(3e-6)},
	})
	require.False(t, tokenPricedImage.TokenPricingAbsent)
	require.Equal(t, 3e-06, tokenPricedImage.OutputCostPerToken)
	require.Zero(t, tokenPricedImage.OutputCostPerImageToken)

	// token 档（含 275 迁移前的存量行）不抑制任何目录价
	for _, mode := range []BillingMode{BillingModeToken, ""} {
		token := buildOverrideModelPriceEntry("gemini-2.5-flash-image", base, &ModelPriceOverride{
			Currency: ModelPriceCurrencyUSD, BillingMode: mode, Enabled: true,
			Payload: ModelPriceOverridePayload{OutputCostPerToken: ptrPrice(2e-6)},
		})
		require.False(t, token.TokenPricingAbsent)
		require.Equal(t, 3e-05, token.OutputCostPerImageToken)
		require.True(t, token.VideoPricing.Enabled)
		require.True(t, catalogVideoPricingActive(token))
	}
}

func TestIsRedundantPayloadRespectsBillingMode(t *testing.T) {
	svc := newBillingModeTestService(t)
	catalog := svc.catalogData["gemini-2.5-flash-image"]
	payload := &ModelPriceOverridePayload{OutputCostPerImageToken: ptrPrice(3e-05)}
	require.True(t, isRedundantPayload(catalog, ModelPriceCurrencyUSD, BillingModeToken, payload))
	require.False(t, isRedundantPayload(catalog, ModelPriceCurrencyUSD, BillingModeImage, payload))
}

// image 档只屏蔽目录视频价兜底；分组视频定价仍须拿到完整的技术档案（分辨率归一化 +
// video_token 预估器），否则会命中更便宜的兜底规则或直接 ESTIMATOR_MISSING。
func TestBillingModeImageKeepsVideoTechnicalProfileForGroupPricing(t *testing.T) {
	const model = "doubao-seedance-1-0-lite-t2v-250428"
	pricing := NewPricingService(&config.Config{}, nil)
	pricing.SeedCatalogForTest(map[string]*ModelPriceEntry{
		model: {VideoPricing: seedanceVideoPricing(), PricePresenceKnown: true, TokenPricingAbsent: true},
	})
	pricing.SeedOverridesForTest([]ModelPriceOverride{{
		Platform: PlatformOpenAI, ModelName: model, Currency: ModelPriceCurrencyUSD,
		BillingMode: BillingModeImage, Enabled: true,
	}})
	resolver := NewVideoPricingResolver(nil, pricing)
	request := func(group *Group) VideoPricingResolveRequest {
		return VideoPricingResolveRequest{
			Group: group, Platform: PlatformOpenAI,
			Mapping:        ChannelMappingResult{MappedModel: model, BillingModelSource: BillingModelSourceChannelMapped},
			RequestedModel: model, ChannelModel: model, UpstreamModel: model, Provider: VideoProviderOpenAI,
			Attributes: VideoPricingAttributes{Operation: VideoOperationGenerate, Size: "864x480", Seconds: 5},
		}
	}

	_, err := resolver.Resolve(context.Background(), request(&Group{ID: 7, Platform: PlatformOpenAI, RateMultiplier: 1}))
	require.ErrorIs(t, err, ErrVideoPricingMissing, "image 档不得以目录视频价报价")

	tokenUnit, tokenPrice := VideoBillingUnitVideoToken, 2e-6
	quote, err := resolver.Resolve(context.Background(), request(&Group{ID: 7, Platform: PlatformOpenAI, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformOpenAI, Models: []string{model}, BillingMode: BillingModeVideo,
			Intervals: []PricingInterval{{BillingUnit: &tokenUnit, PerRequestPrice: &tokenPrice}},
		}}}))
	require.NoError(t, err, "分组 video_token 定价须能用上目录预估器")
	require.Equal(t, VideoPricingSourceGroup, quote.Source)
	require.NotNil(t, quote.Estimator)

	secondUnit, cheap, p480 := VideoBillingUnitSecond, 0.10, 0.20
	quote, err = resolver.Resolve(context.Background(), request(&Group{ID: 7, Platform: PlatformOpenAI, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformOpenAI, Models: []string{model}, BillingMode: BillingModeVideo,
			Intervals: []PricingInterval{
				{ID: 1, BillingUnit: &secondUnit, PerRequestPrice: &p480, Conditions: json.RawMessage(`{"resolutions":["480p"]}`)},
				{ID: 2, BillingUnit: &secondUnit, PerRequestPrice: &cheap},
			},
		}}}))
	require.NoError(t, err)
	require.Equal(t, p480, quote.UnitPrice, "尺寸须经技术档案归一化为分辨率，不能落到更便宜的兜底规则")
}

// video 档抹掉 token 价后，对话请求必须被拒绝，不能回落到按模型名关键词匹配的硬编码价。
func TestBillingModeVideoRejectsChatInsteadOfFallbackPrice(t *testing.T) {
	pricing := newBillingModeTestService(t)
	pricing.SeedOverridesForTest([]ModelPriceOverride{{
		Platform: ModelPriceOverrideWildcardPlatform, ModelName: "claude-sonnet-4", Currency: ModelPriceCurrencyUSD,
		BillingMode: BillingModeVideo, Enabled: true,
		Payload: ModelPriceOverridePayload{VideoPricing: seedanceVideoPricing()},
	}})
	billing := NewBillingService(&config.Config{}, pricing)
	_, err := billing.GetModelPricingStrictForPlatforms([]string{PlatformAnthropic}, "claude-sonnet-4")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	_, err = billing.GetModelPricing("claude-sonnet-4")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

// 只有真的改了 token 价的覆盖才关掉 DeepSeek 官方峰谷翻倍；全部继承的空覆盖、
// 只改图片价的覆盖仍用目录空闲价，高峰必须照常翻倍。
func TestOverrideOperatorFlagOnlyWhenTokenPricesOverridden(t *testing.T) {
	svc := newBillingModeTestService(t)
	base := svc.catalogData["gemini-2.5-flash-image"]
	build := func(payload ModelPriceOverridePayload, mode BillingMode) *ModelPriceEntry {
		return buildOverrideModelPriceEntry("gemini-2.5-flash-image", base, &ModelPriceOverride{
			Currency: ModelPriceCurrencyUSD, BillingMode: mode, Enabled: true, Payload: payload,
		})
	}
	require.False(t, build(ModelPriceOverridePayload{}, BillingModeToken).OperatorOverride)
	require.False(t, build(ModelPriceOverridePayload{}, BillingModeImage).OperatorOverride)
	require.False(t, build(ModelPriceOverridePayload{OutputCostPerImageToken: ptrPrice(4e-5)}, BillingModeImage).OperatorOverride)
	require.False(t, build(ModelPriceOverridePayload{SupportsPromptCaching: func() *bool { v := true; return &v }()}, BillingModeToken).OperatorOverride)
	require.True(t, build(ModelPriceOverridePayload{InputCostPerToken: ptrPrice(1e-7)}, BillingModeToken).OperatorOverride)
	require.True(t, build(ModelPriceOverridePayload{CacheReadInputTokenCost: ptrPrice(1e-8)}, BillingModeToken).OperatorOverride)
}

// 请求不带 billing_mode（旧前端缓存 / 外部脚本）时，更新保留已存的计费方式。
func TestStoredOverrideBillingModeDefaultsToToken(t *testing.T) {
	svc := newBillingModeTestService(t)
	require.Equal(t, BillingModeToken, svc.storedOverrideBillingMode(PlatformOpenAI, "gemini-2.5-flash-image"))
	svc.SeedOverridesForTest([]ModelPriceOverride{{
		Platform: PlatformOpenAI, ModelName: "gemini-2.5-flash-image", Currency: ModelPriceCurrencyUSD,
		BillingMode: BillingModeImage, Enabled: true,
	}})
	require.Equal(t, BillingModeImage, svc.storedOverrideBillingMode(PlatformOpenAI, "gemini-2.5-flash-image"))
	require.Equal(t, BillingModeToken, svc.storedOverrideBillingMode(PlatformAnthropic, "gemini-2.5-flash-image"))
}

// video 档写入显式关闭的视频价会让模型既没有 token 价也没有视频价，须拒绝。
func TestBillingModeVideoRejectsDisabledVideoPricing(t *testing.T) {
	svc := newBillingModeTestService(t)
	disabled := seedanceVideoPricing()
	disabled.Enabled = false
	_, err := svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeVideo,
		&ModelPriceOverridePayload{VideoPricing: disabled}, true)
	requireAppErrorReason(t, err, "EMPTY_PRICING")
}
