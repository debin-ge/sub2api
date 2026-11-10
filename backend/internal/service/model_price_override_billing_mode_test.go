package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// 与内置目录同形：
//   - gemini-2.5-flash-image：完整 token 价 + 图片输出 token 价 + 按张价（两种价都有，按张）。
//   - gpt-image-2：文本价 + 两项图片 token 价，没有按张价（按 token）。
//   - gemini-3.1-flash-live-preview：只看图的对话模型，只有图片输入价。
//   - gemini-3.1-pro-preview：对话模型，目录里带一个 0.00012 的「按张价」，不算出图声明。
const billingModeCatalogJSON = `{
	"gemini-2.5-flash-image": {"litellm_provider": "vertex_ai-language-models", "mode": "image_generation",
		"input_cost_per_token": 3e-07, "output_cost_per_token": 2.5e-06,
		"output_cost_per_image_token": 3e-05, "output_cost_per_image": 0.039},
	"gpt-image-2": {"litellm_provider": "openai", "mode": "image_generation",
		"input_cost_per_token": 5e-06, "output_cost_per_token": 1e-05,
		"input_cost_per_image_token": 8e-06, "output_cost_per_image_token": 3e-05},
	"gemini-3.1-flash-live-preview": {"litellm_provider": "gemini", "mode": "chat",
		"input_cost_per_token": 7.5e-07, "output_cost_per_token": 4.5e-06, "input_cost_per_image_token": 1e-06},
	"gemini-3.1-pro-preview": {"litellm_provider": "gemini", "mode": "chat",
		"input_cost_per_token": 2e-06, "output_cost_per_token": 1.2e-05, "output_cost_per_image": 0.00012},
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

// image 档 = 按张：只接受按张价与元数据；文本 token 价、图片 token 价、视频价都不属于它。
func TestBillingModeImageOnlyAcceptsPerImageFields(t *testing.T) {
	svc := newBillingModeTestService(t)
	price := 0.05
	enabled := true
	mode := "image_generation"
	_, err := svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeImage,
		&ModelPriceOverridePayload{
			OutputCostPerImage:    ptrPrice(0.04),
			Intervals:             []PricingInterval{{TierLabel: ImageBillingSize1K, PerRequestPrice: &price}},
			SupportsPromptCaching: &enabled,
			Mode:                  &mode,
		}, true)
	require.NoError(t, err)

	for name, payload := range map[string]*ModelPriceOverridePayload{
		"text token":   {OutputCostPerImage: ptrPrice(0.04), InputCostPerToken: ptrPrice(3e-7)},
		"image output": {OutputCostPerImage: ptrPrice(0.04), OutputCostPerImageToken: ptrPrice(3e-5)},
		"image input":  {OutputCostPerImage: ptrPrice(0.04), InputCostPerImageToken: ptrPrice(3e-7)},
		"video":        {VideoPricing: seedanceVideoPricing()},
	} {
		_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeImage, payload, true)
		requireAppErrorReason(t, err, "FIELD_NOT_IN_BILLING_MODE")
		require.NotEmpty(t, name)
	}

	// 停用的行同样不允许写入其他档字段
	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeVideo,
		&ModelPriceOverridePayload{OutputCostPerImage: ptrPrice(0.04)}, false)
	requireAppErrorReason(t, err, "FIELD_NOT_IN_BILLING_MODE")

	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeVideo,
		&ModelPriceOverridePayload{OutputCostPerToken: ptrPrice(2e-6)}, false)
	requireAppErrorReason(t, err, "FIELD_NOT_IN_BILLING_MODE")
}

func TestBillingModeImageValidatesTierIntervals(t *testing.T) {
	svc := newBillingModeTestService(t)
	price := 0.04
	_, err := svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeImage,
		&ModelPriceOverridePayload{Intervals: []PricingInterval{{TierLabel: ImageBillingSize1K, PerRequestPrice: &price}}}, true)
	require.NoError(t, err)

	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeImage,
		&ModelPriceOverridePayload{Intervals: []PricingInterval{{TierLabel: ImageBillingSize1K, PerRequestPrice: &price}, {TierLabel: ImageBillingSize1K, PerRequestPrice: &price}}}, true)
	requireAppErrorReason(t, err, "INVALID_IMAGE_INTERVAL")

	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{Intervals: []PricingInterval{{TierLabel: ImageBillingSize1K, PerRequestPrice: &price}}}, true)
	requireAppErrorReason(t, err, "FIELD_NOT_IN_BILLING_MODE")
}

// token 档 = 按 token：不接受按张价；video_pricing 是 275 迁移前存量行的放行项。
func TestBillingModeTokenRejectsPerImageFields(t *testing.T) {
	svc := newBillingModeTestService(t)
	price := 0.05
	_, err := svc.validateOverrideWrite("*", "gpt-image-2", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{OutputCostPerImage: ptrPrice(0.04)}, true)
	requireAppErrorReason(t, err, "FIELD_NOT_IN_BILLING_MODE")

	_, err = svc.validateOverrideWrite("*", "gpt-image-2", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{Intervals: []PricingInterval{{TierLabel: ImageBillingSize1K, PerRequestPrice: &price}}}, true)
	requireAppErrorReason(t, err, "FIELD_NOT_IN_BILLING_MODE")

	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{
			InputCostPerToken: ptrPrice(3e-7), OutputCostPerToken: ptrPrice(2e-6),
			InputCostPerImageToken: ptrPrice(3e-7), OutputCostPerImageToken: ptrPrice(3e-5),
		}, true)
	require.NoError(t, err, "文本价 + 两项图片价是合法的 token 档")

	_, err = svc.validateOverrideWrite("*", "claude-sonnet-4", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{VideoPricing: seedanceVideoPricing()}, true)
	require.NoError(t, err, "存量 token 行带 video_pricing 照旧放行")
}

// 按 token 出图只有两项价，必须成对（可以从目录继承）；只看图的对话模型不受限制。
func TestBillingModeTokenRequiresImageTokenPair(t *testing.T) {
	svc := newBillingModeTestService(t)

	_, err := svc.validateOverrideWrite("*", "gpt-image-2", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{OutputCostPerImageToken: ptrPrice(4e-5)}, true)
	require.NoError(t, err, "图片输入价从目录继承")

	_, err = svc.validateOverrideWrite("*", "my-custom-image-model", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{OutputCostPerImageToken: ptrPrice(4e-5)}, true)
	requireAppErrorReason(t, err, "INCOMPLETE_IMAGE_TOKEN_PRICING")

	_, err = svc.validateOverrideWrite("*", "my-custom-image-model", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{InputCostPerImageToken: ptrPrice(8e-6), OutputCostPerImageToken: ptrPrice(4e-5)}, true)
	require.NoError(t, err)

	// 目录 mode=image_generation、但没有图片输入价：token 档必须补上
	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{OutputCostPerImageToken: ptrPrice(3e-5)}, true)
	requireAppErrorReason(t, err, "INCOMPLETE_IMAGE_TOKEN_PRICING")
	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeToken,
		&ModelPriceOverridePayload{}, true)
	requireAppErrorReason(t, err, "INCOMPLETE_IMAGE_TOKEN_PRICING")

	for _, payload := range []*ModelPriceOverridePayload{
		{InputCostPerToken: ptrPrice(8e-7), OutputCostPerToken: ptrPrice(5e-6)},
		{InputCostPerImageToken: ptrPrice(2e-6)},
		{},
	} {
		_, err = svc.validateOverrideWrite("*", "gemini-3.1-flash-live-preview", ModelPriceCurrencyUSD, BillingModeToken, payload, true)
		require.NoError(t, err, "只看图的对话模型不触发成对校验")
	}
}

// image / video 档都会抑制目录 token 价（含图片 token 价），保存时要提示运营者。
func TestBillingModeWarnsWhenSuppressingCatalogTokenPrice(t *testing.T) {
	svc := newBillingModeTestService(t)
	warnings, err := svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeImage,
		&ModelPriceOverridePayload{OutputCostPerImage: ptrPrice(0.05)}, true)
	require.NoError(t, err)
	require.Contains(t, warnings, ModelPriceWarning{Code: "BILLING_MODE_SUPPRESSES_TOKEN", Field: "billing_mode"})

	warnings, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeVideo,
		&ModelPriceOverridePayload{VideoPricing: seedanceVideoPricing()}, true)
	require.NoError(t, err)
	require.Contains(t, warnings, ModelPriceWarning{Code: "BILLING_MODE_SUPPRESSES_TOKEN", Field: "billing_mode"})

	warnings, err = svc.validateOverrideWrite("*", "my-custom-image-model", ModelPriceCurrencyUSD, BillingModeImage,
		&ModelPriceOverridePayload{OutputCostPerImage: ptrPrice(0.05)}, true)
	require.NoError(t, err)
	require.Empty(t, warnings, "目录里没有 token 价，无需提示")
}

func TestBillingModeEmptyPayload(t *testing.T) {
	svc := newBillingModeTestService(t)

	// 只用目录按张价：合法
	_, err := svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeImage,
		&ModelPriceOverridePayload{}, true)
	require.NoError(t, err)

	// 目录里没有按张价：image 档不成立（token 价不算按张价）
	for _, model := range []string{"gpt-image-2", "claude-sonnet-4"} {
		_, err = svc.validateOverrideWrite("*", model, ModelPriceCurrencyUSD, BillingModeImage,
			&ModelPriceOverridePayload{}, true)
		requireAppErrorReason(t, err, "EMPTY_PRICING")
	}

	// video 档要求 video_pricing：目录里的 token / 图片价都顶不上
	_, err = svc.validateOverrideWrite("*", "gemini-2.5-flash-image", ModelPriceCurrencyUSD, BillingModeVideo,
		&ModelPriceOverridePayload{}, true)
	requireAppErrorReason(t, err, "EMPTY_PRICING")

	// token 档空 payload 仍是“全部继承”
	for _, model := range []string{"claude-sonnet-4", "gpt-image-2"} {
		_, err = svc.validateOverrideWrite("*", model, ModelPriceCurrencyUSD, BillingModeToken,
			&ModelPriceOverridePayload{}, true)
		require.NoError(t, err)
	}
}

func TestBuildOverrideEntrySuppressesOtherBillingModes(t *testing.T) {
	svc := newBillingModeTestService(t)
	base := svc.catalogData["gemini-2.5-flash-image"]
	base.VideoPricing = seedanceVideoPricing()

	// image 档 = 按张：抹掉目录文本 token 价与图片 token 价，只留按张价；屏蔽目录视频价兜底。
	image := buildOverrideModelPriceEntry("gemini-2.5-flash-image", base, &ModelPriceOverride{
		Currency: ModelPriceCurrencyUSD, BillingMode: BillingModeImage, Enabled: true,
	})
	require.True(t, image.TokenPricingAbsent)
	require.False(t, image.InputPriceExplicit)
	require.False(t, image.OutputPriceExplicit)
	require.Zero(t, image.InputCostPerToken)
	require.Zero(t, image.OutputCostPerToken)
	require.False(t, image.ImageOutputPriceExplicit)
	require.Zero(t, image.OutputCostPerImageToken)
	require.True(t, image.OutputCostPerImageExplicit)
	require.Equal(t, 0.039, image.OutputCostPerImage)
	require.NotNil(t, image.VideoPricing, "video_pricing 永不清空")
	require.True(t, image.VideoPricing.Enabled, "技术档案须保持可用，渠道/分组视频定价依赖它")
	require.Equal(t, BillingModeImage, image.BillingMode)
	require.Equal(t, BillingModeImage, image.ImageBillingMode)
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
	require.Empty(t, video.ImageBillingMode)

	// token 档（含 275 迁移前的存量行）：抹掉目录按张价，保留文本价与图片 token 价。
	for _, mode := range []BillingMode{BillingModeToken, ""} {
		token := buildOverrideModelPriceEntry("gemini-2.5-flash-image", base, &ModelPriceOverride{
			Currency: ModelPriceCurrencyUSD, BillingMode: mode, Enabled: true,
			Payload: ModelPriceOverridePayload{OutputCostPerToken: ptrPrice(2e-6), InputCostPerImageToken: ptrPrice(3e-7)},
		})
		require.False(t, token.TokenPricingAbsent)
		require.Equal(t, 3e-05, token.OutputCostPerImageToken)
		require.Equal(t, 3e-07, token.InputCostPerImageToken)
		require.Zero(t, token.OutputCostPerImage)
		require.False(t, token.OutputCostPerImageExplicit)
		require.Empty(t, token.Intervals)
		require.True(t, token.VideoPricing.Enabled)
		require.True(t, catalogVideoPricingActive(token))
		require.Equal(t, BillingModeToken, token.ImageBillingMode)
	}
}

// 存量行里不属于本档的字段在运行时被忽略，并列为「被忽略字段」。
func TestProjectPayloadToBillingModeIgnoresForeignFields(t *testing.T) {
	price := 0.05
	legacyToken := ModelPriceOverridePayload{
		InputCostPerToken: ptrPrice(5e-6), OutputCostPerToken: ptrPrice(1e-5),
		OutputCostPerImage: ptrPrice(0.1),
		Intervals:          []PricingInterval{{TierLabel: ImageBillingSize1K, PerRequestPrice: &price}},
	}
	projected, ignored := projectPayloadToBillingMode(&legacyToken, BillingModeToken)
	require.Equal(t, []string{"intervals", "output_cost_per_image"}, ignored)
	require.Nil(t, projected.OutputCostPerImage)
	require.Empty(t, projected.Intervals)
	require.Equal(t, 5e-6, *projected.InputCostPerToken)
	require.NotNil(t, legacyToken.OutputCostPerImage, "不改原 payload")

	legacyImage := ModelPriceOverridePayload{
		OutputCostPerImage: ptrPrice(0.05), InputCostPerToken: ptrPrice(5e-6),
		InputCostPerImageToken: ptrPrice(8e-6), OutputCostPerImageToken: ptrPrice(3e-5),
	}
	projected, ignored = projectPayloadToBillingMode(&legacyImage, BillingModeImage)
	require.Equal(t, []string{"input_cost_per_image_token", "input_cost_per_token", "output_cost_per_image_token"}, ignored)
	require.Equal(t, 0.05, *projected.OutputCostPerImage)
	require.Nil(t, projected.InputCostPerToken)

	_, ignored = projectPayloadToBillingMode(&ModelPriceOverridePayload{VideoPricing: seedanceVideoPricing()}, BillingModeToken)
	require.Empty(t, ignored, "token 档放行存量 video_pricing")

	// 生效条目里没有被忽略的按张价；后台列出被忽略字段。
	svc := newBillingModeTestService(t)
	row := ModelPriceOverride{
		Platform: PlatformOpenAI, ModelName: "gpt-image-2", Currency: ModelPriceCurrencyUSD,
		BillingMode: BillingModeToken, Enabled: true, Payload: ModelPriceOverridePayload{OutputCostPerImage: ptrPrice(0.1)},
	}
	entry := buildOverrideModelPriceEntry("gpt-image-2", svc.catalogData["gpt-image-2"], &row)
	require.Zero(t, entry.OutputCostPerImage)
	require.False(t, entry.OutputCostPerImageExplicit)
	require.Equal(t, BillingModeToken, entry.ImageBillingMode)
	require.Equal(t, []string{"output_cost_per_image"}, overrideIgnoredFields(&row))
}

// 出图方式推导（方案 §6.3）。
func TestDeriveImageBillingMode(t *testing.T) {
	svc := newBillingModeTestService(t)
	require.Equal(t, BillingModeToken, svc.catalogData["gpt-image-2"].ImageBillingMode)
	require.Equal(t, BillingModeImage, svc.catalogData["gemini-2.5-flash-image"].ImageBillingMode, "两种价都有时按张（D8）")
	require.Empty(t, svc.catalogData["gemini-3.1-pro-preview"].ImageBillingMode, "对话模型上的按张价不算数")
	require.Empty(t, svc.catalogData["gemini-3.1-flash-live-preview"].ImageBillingMode)
	require.Empty(t, svc.catalogData["claude-sonnet-4"].ImageBillingMode)

	gemini := svc.catalogData["gemini-2.5-flash-image"]
	build := func(model string, base *ModelPriceEntry, mode BillingMode, payload ModelPriceOverridePayload) *ModelPriceEntry {
		return buildOverrideModelPriceEntry(model, base, &ModelPriceOverride{
			Currency: ModelPriceCurrencyUSD, BillingMode: mode, Enabled: true, Payload: payload,
		})
	}
	require.Equal(t, BillingModeImage, build("gemini-2.5-flash-image", gemini, BillingModeImage, ModelPriceOverridePayload{}).ImageBillingMode)
	require.Equal(t, BillingModeToken, build("gemini-2.5-flash-image", gemini, BillingModeToken, ModelPriceOverridePayload{
		InputCostPerImageToken: ptrPrice(3e-7), OutputCostPerImageToken: ptrPrice(3e-5),
	}).ImageBillingMode)
	require.Empty(t, build("gpt-5", pricedEntry(1.25e-6, 1e-5), BillingModeToken, ModelPriceOverridePayload{
		InputCostPerToken: ptrPrice(1e-6),
	}).ImageBillingMode)
	require.Empty(t, build("grok-imagine-image", nil, BillingModeToken, ModelPriceOverridePayload{
		InputCostPerToken: ptrPrice(1e-6), OutputCostPerToken: ptrPrice(1e-6),
	}).ImageBillingMode, "没有图片输出价的 token 行不声明出图方式")
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

// image 档抹掉 token 价后，对话请求同样必须被拒绝，不能被按 1 张收费，也不能回落到硬编码价。
func TestBillingModeImageRejectsChatInsteadOfFallbackPrice(t *testing.T) {
	pricing := newBillingModeTestService(t)
	pricing.SeedOverridesForTest([]ModelPriceOverride{{
		Platform: ModelPriceOverrideWildcardPlatform, ModelName: "claude-sonnet-4", Currency: ModelPriceCurrencyUSD,
		BillingMode: BillingModeImage, Enabled: true,
		Payload: ModelPriceOverridePayload{OutputCostPerImage: ptrPrice(0.05)},
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
	require.False(t, build(ModelPriceOverridePayload{OutputCostPerImage: ptrPrice(0.05)}, BillingModeImage).OperatorOverride)
	require.False(t, build(ModelPriceOverridePayload{OutputCostPerImageToken: ptrPrice(4e-5)}, BillingModeToken).OperatorOverride)
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
