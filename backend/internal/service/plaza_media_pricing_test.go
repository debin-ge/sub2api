//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func plazaTierMap(tiers []PlazaMediaTierPrice) map[string]float64 {
	out := make(map[string]float64, len(tiers))
	for _, tier := range tiers {
		out[tier.Tier] = tier.Price
	}
	return out
}

func TestResolvePlazaMediaPricing_ImageFollowsSettlementPriority(t *testing.T) {
	pricing := &PricingService{pricingData: map[string]*ModelPriceEntry{
		"gpt-image-2": {
			OutputCostPerImage:         0.2,
			OutputCostPerImageExplicit: true,
			OutputCostPerImageToken:    0.00004,
			ImageOutputPriceExplicit:   true,
		},
	}}
	group1K := 0.05
	channel2K := 0.09

	media := ResolvePlazaMediaPricing(pricing, PlatformOpenAI, "gpt-image-2", &ChannelModelPricing{
		BillingMode: BillingModeImage,
		Intervals:   []PricingInterval{{TierLabel: ImageBillingSize2K, PerRequestPrice: &channel2K}},
	}, AvailableGroupRef{ImagePrice1K: &group1K})

	require.Empty(t, media.VideoTiers)
	tiers := plazaTierMap(media.ImageTiers)
	require.InDelta(t, 0.05, tiers[ImageBillingSize1K], 1e-12, "group price wins")
	require.InDelta(t, 0.09, tiers[ImageBillingSize2K], 1e-12, "channel tier beats catalog")
	// Channel has no 4K tier and no per-request default -> catalog base ×2.
	require.InDelta(t, 0.4, tiers[ImageBillingSize4K], 1e-12)
}

func TestResolvePlazaMediaPricing_ImageFromCatalogOnly(t *testing.T) {
	pricing := &PricingService{pricingData: map[string]*ModelPriceEntry{
		"imagen-4": {OutputCostPerImage: 0.04, OutputCostPerImageExplicit: true},
	}}

	media := ResolvePlazaMediaPricing(pricing, PlatformGemini, "imagen-4", nil, AvailableGroupRef{})

	tiers := plazaTierMap(media.ImageTiers)
	require.InDelta(t, 0.04, tiers[ImageBillingSize1K], 1e-12)
	require.InDelta(t, 0.06, tiers[ImageBillingSize2K], 1e-12)
	require.InDelta(t, 0.08, tiers[ImageBillingSize4K], 1e-12)
}

func TestResolvePlazaMediaPricing_TextModelIgnoresGroupMediaPrices(t *testing.T) {
	pricing := &PricingService{pricingData: map[string]*ModelPriceEntry{
		"gpt-5": {InputCostPerToken: 0.000001, OutputCostPerToken: 0.00001},
	}}
	price := 0.1

	media := ResolvePlazaMediaPricing(pricing, PlatformOpenAI, "gpt-5", nil, AvailableGroupRef{
		ImagePrice1K:   &price,
		VideoPrice480P: &price,
	})

	require.Empty(t, media.ImageTiers)
	require.Empty(t, media.VideoTiers)
}

func TestResolvePlazaMediaPricing_VideoFollowsSettlementPriority(t *testing.T) {
	flat720 := 0.2
	media := ResolvePlazaMediaPricing(nil, PlatformGrok, "grok-imagine-video-1.5", nil, AvailableGroupRef{
		VideoPrice720P: &flat720,
		VideoModelPrices: map[string]map[string]float64{
			VideoPriceFamilyGrokImagineVideo15: {VideoBillingResolution480P: 0.1},
		},
	})

	require.Empty(t, media.ImageTiers)
	tiers := plazaTierMap(media.VideoTiers)
	require.InDelta(t, 0.1, tiers[VideoBillingResolution480P], 1e-12, "model family price wins")
	require.InDelta(t, 0.2, tiers[VideoBillingResolution720P], 1e-12, "flat group price next")
	require.InDelta(t, defaultGrokImagineVideo15Price1080P, tiers[VideoBillingResolution1080P], 1e-12, "grok default last")
}

func TestResolvePlazaMediaPricing_VideoWithoutKnownPriceOmitsTiers(t *testing.T) {
	media := ResolvePlazaMediaPricing(nil, PlatformGrok, "grok-imagine-video-preview", nil, AvailableGroupRef{})
	require.Empty(t, media.ImageTiers)
	require.Empty(t, media.VideoTiers)
}

func TestResolvePlazaMediaPricing_VideoFromCatalogTokenRules(t *testing.T) {
	// 运营只在模型价格里配了 video_pricing（按视频 token 计价），渠道/分组都没配价。
	pricing := &PricingService{pricingData: map[string]*ModelPriceEntry{
		"doubao-seedance-2.0-mini-480p": videoPricedCatalogEntry(seedanceVideoPricing()),
	}}

	media := ResolvePlazaMediaPricing(pricing, PlatformOpenAI, "doubao-seedance-2.0-mini-480p", nil, AvailableGroupRef{})

	require.Empty(t, media.ImageTiers)
	require.Nil(t, media.VideoPerRequest)
	tiers := plazaTierMap(media.VideoTiers)
	// 864×480 × 24fps ÷ 1024 = 9720 tokens/s × $1e-6（取不带参考视频的最低价）。
	require.InDelta(t, 0.00972, tiers[VideoBillingResolution480P], 1e-12)
	_, has720 := tiers[VideoBillingResolution720P]
	require.False(t, has720, "no rule prices 720p, so the tier is omitted")
}

func TestResolvePlazaMediaPricing_VideoFromCatalogPerSecondRules(t *testing.T) {
	pricing := &PricingService{pricingData: map[string]*ModelPriceEntry{
		"sora-2": videoPricedCatalogEntry(soraVideoPricing()),
	}}

	media := ResolvePlazaMediaPricing(pricing, PlatformOpenAI, "sora-2", nil, AvailableGroupRef{})

	tiers := plazaTierMap(media.VideoTiers)
	require.Len(t, tiers, 2, "only standard tiers declared in resolutions are shown")
	require.InDelta(t, 0.1, tiers[VideoBillingResolution720P], 1e-12)
	require.InDelta(t, 0.1, tiers[VideoBillingResolution1080P], 1e-12)
}

func TestResolvePlazaMediaPricing_VideoCatalogRequestOnly(t *testing.T) {
	profile := &VideoPricingConfig{
		Version: VideoPricingConfigVersion, Enabled: true, Currency: ModelPriceCurrencyUSD,
		Rules: []VideoPricingRule{{
			Key: "per-request", BillingUnit: VideoBillingUnitRequest, UnitPriceUSD: 0.3,
			Conditions: VideoPricingConditions{Operations: []string{"generate"}},
		}},
	}
	pricing := &PricingService{pricingData: map[string]*ModelPriceEntry{
		"custom-video-720p": videoPricedCatalogEntry(profile),
	}}

	media := ResolvePlazaMediaPricing(pricing, PlatformOpenAI, "custom-video-720p", nil, AvailableGroupRef{})

	require.Empty(t, media.VideoTiers)
	require.NotNil(t, media.VideoPerRequest, "operation-scoped rule is still probed")
	require.InDelta(t, 0.3, *media.VideoPerRequest, 1e-12)
}

func TestResolvePlazaMediaPricing_VideoGroupPriceBeatsCatalog(t *testing.T) {
	pricing := &PricingService{pricingData: map[string]*ModelPriceEntry{
		"sora-2": videoPricedCatalogEntry(soraVideoPricing()),
	}}
	group720 := 0.05

	media := ResolvePlazaMediaPricing(pricing, PlatformOpenAI, "sora-2", nil, AvailableGroupRef{VideoPrice720P: &group720})

	tiers := plazaTierMap(media.VideoTiers)
	require.Equal(t, map[string]float64{VideoBillingResolution720P: 0.05}, tiers)
}
