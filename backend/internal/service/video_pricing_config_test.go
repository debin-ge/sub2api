package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func videoBool(value bool) *bool { return &value }

func seedanceVideoPricing() *VideoPricingConfig {
	return &VideoPricingConfig{
		Version:  VideoPricingConfigVersion,
		Enabled:  true,
		Currency: ModelPriceCurrencyUSD,
		Defaults: VideoPricingDefaults{
			Resolution: "480p", RequestMode: VideoRequestModeStandard, InferenceMode: VideoInferenceModeOnline,
		},
		Resolutions: map[string]VideoResolutionSpec{
			"480p": {Sizes: []string{"864x480", "480x864"}},
			"720p": {Sizes: []string{"1280x720", "720x1280"}},
		},
		Estimators: map[string]VideoUsageEstimator{
			"output-video-token": {
				Type: VideoEstimatorPixelFrame, TokenScope: VideoTokenScopeOutputOnly, FPS: 24, Divisor: 1024,
			},
		},
		Rules: []VideoPricingRule{
			{
				Key: "480p-no-video", BillingUnit: VideoBillingUnitVideoToken, UnitPriceUSD: 1e-6,
				Estimator: "output-video-token", Conditions: VideoPricingConditions{
					Resolutions: []string{"480p"}, InputHasVideo: videoBool(false),
					RequestModes: []string{VideoRequestModeStandard}, InferenceModes: []string{VideoInferenceModeOnline},
				},
			},
			{
				Key: "480p-reference-video", BillingUnit: VideoBillingUnitVideoToken, UnitPriceUSD: 2e-6,
				Estimator: "output-video-token", Conditions: VideoPricingConditions{
					Resolutions: []string{"480p"}, InputHasVideo: videoBool(true),
					RequestModes: []string{VideoRequestModeStandard}, InferenceModes: []string{VideoInferenceModeOnline},
				},
			},
		},
	}
}

func TestVideoPricingConfigSeedanceTokenRulesAndEstimator(t *testing.T) {
	profile := seedanceVideoPricing()
	require.NoError(t, ValidateVideoPricingConfig(profile))

	quote, err := ResolveVideoPricingConfig(profile, VideoPricingAttributes{
		Operation: "generate", Size: "864x480", Seconds: 5,
		InputHasVideo: false, At: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Equal(t, "480p-no-video", quote.RuleKey)
	require.Equal(t, VideoBillingUnitVideoToken, quote.BillingUnit)
	require.Equal(t, "480p", quote.Attributes.Resolution)
	require.Equal(t, 48_600.0, quote.EstimatedUnits)
	require.Equal(t, quote.EstimatedUnits, quote.MaximumUnits)
	require.InDelta(t, 0.0486, quote.HoldAmount, 1e-9)

	quote, err = ResolveVideoPricingConfig(profile, VideoPricingAttributes{
		Operation: "edit", Size: "480p", Seconds: 5, InputHasVideo: true,
	})
	require.NoError(t, err)
	require.Equal(t, "480p-reference-video", quote.RuleKey)
	require.Equal(t, 48_600.0, quote.EstimatedUnits, "output_only does not add input video tokens")
	require.InDelta(t, 0.0972, quote.HoldAmount, 1e-9)
}

func TestVideoPricingConfigPixelFrameUsesLargestDefaultResolutionSize(t *testing.T) {
	profile := seedanceVideoPricing()
	profile.Defaults.Resolution = "720p"
	profile.Rules = []VideoPricingRule{{
		Key: "720p", BillingUnit: VideoBillingUnitVideoToken, UnitPriceUSD: 1e-6, Estimator: "output-video-token",
		Conditions: VideoPricingConditions{Resolutions: []string{"720p"}},
	}}
	quote, err := ResolveVideoPricingConfig(profile, VideoPricingAttributes{Seconds: 5})
	require.NoError(t, err)
	require.Equal(t, 108_000.0, quote.EstimatedUnits)
}

func TestVideoPricingConfigSupportsEveryEstimator(t *testing.T) {
	base := seedanceVideoPricing()
	tests := []struct {
		name      string
		estimator VideoUsageEstimator
		attrs     VideoPricingAttributes
		want      float64
	}{
		{
			name: "input-plus-output", estimator: VideoUsageEstimator{
				Type: VideoEstimatorPixelFrame, TokenScope: VideoTokenScopeInputPlusOutput,
				FPS: 24, Divisor: 1024, MaxInputVideoSeconds: 10,
			}, attrs: VideoPricingAttributes{Size: "864x480", Seconds: 5, InputHasVideo: true, InputVideoSeconds: 2}, want: 68_040,
		},
		{name: "fixed-per-second", estimator: VideoUsageEstimator{Type: VideoEstimatorFixedTokensPerSecond, TokensPerSecond: 123}, attrs: VideoPricingAttributes{Seconds: 5}, want: 615},
		{name: "fixed-max", estimator: VideoUsageEstimator{Type: VideoEstimatorFixedMaxUnits, MaxUnits: 999}, attrs: VideoPricingAttributes{Seconds: 5}, want: 999},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := cloneVideoPricingConfig(base)
			profile.Estimators = map[string]VideoUsageEstimator{"estimate": test.estimator}
			profile.Rules = []VideoPricingRule{{Key: "rule", BillingUnit: VideoBillingUnitVideoToken, UnitPriceUSD: 1e-6, Estimator: "estimate"}}
			quote, err := ResolveVideoPricingConfig(profile, test.attrs)
			require.NoError(t, err)
			require.Equal(t, test.want, quote.EstimatedUnits)
		})
	}
}

func TestVideoPricingRequestUnitNeverUsesSecondsAsMaximum(t *testing.T) {
	profile := &VideoPricingConfig{
		Version: 1, Enabled: true, Currency: "USD",
		Rules: []VideoPricingRule{{Key: "request", BillingUnit: VideoBillingUnitRequest, UnitPriceUSD: 2}},
	}
	quote, err := ResolveVideoPricingConfig(profile, VideoPricingAttributes{Seconds: 20, MaximumOutputSeconds: 30})
	require.NoError(t, err)
	require.Equal(t, 1.0, quote.EstimatedUnits)
	require.Equal(t, 1.0, quote.MaximumUnits)
	require.Equal(t, 2.0, quote.HoldAmount)
}

func TestVideoPricingConfigMatchesModesAudioAndValidity(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	from, until := now.Add(-time.Hour), now.Add(time.Hour)
	profile := seedanceVideoPricing()
	profile.Rules = []VideoPricingRule{
		{
			Key: "standard-online-silent", BillingUnit: VideoBillingUnitSecond, UnitPriceUSD: 0.10,
			Conditions: VideoPricingConditions{RequestModes: []string{"standard"}, InferenceModes: []string{"online"}, GenerateAudio: videoBool(false)},
		},
		{
			Key: "batch-offline-audio-promo", BillingUnit: VideoBillingUnitSecond, UnitPriceUSD: 0.04, Priority: 10,
			Conditions: VideoPricingConditions{RequestModes: []string{"batch"}, InferenceModes: []string{"offline"}, GenerateAudio: videoBool(true)},
			ValidFrom:  &from, ValidUntil: &until,
		},
	}
	audio := true
	quote, err := ResolveVideoPricingConfig(profile, VideoPricingAttributes{
		Seconds: 5, RequestMode: "batch", InferenceMode: "offline", AudioEnabled: &audio, At: now,
	})
	require.NoError(t, err)
	require.Equal(t, "batch-offline-audio-promo", quote.RuleKey)
	require.InDelta(t, 0.20, quote.HoldAmount, 1e-9)

	_, err = ResolveVideoPricingConfig(profile, VideoPricingAttributes{
		Seconds: 5, RequestMode: "batch", InferenceMode: "offline", AudioEnabled: &audio, At: until,
	})
	require.ErrorIs(t, err, ErrVideoPricingRuleMissing)
}

func TestVideoPricingConfigRejectsAmbiguousAndUnknownFields(t *testing.T) {
	profile := seedanceVideoPricing()
	profile.Rules = []VideoPricingRule{
		{Key: "one", BillingUnit: VideoBillingUnitRequest, UnitPriceUSD: 1, Conditions: VideoPricingConditions{Operations: []string{"generate"}}},
		{Key: "two", BillingUnit: VideoBillingUnitRequest, UnitPriceUSD: 2, Conditions: VideoPricingConditions{Sizes: []string{"864x480"}}},
	}
	err := ValidateVideoPricingConfig(profile)
	require.ErrorIs(t, err, ErrVideoPricingInvalid)

	_, err = DecodeModelPriceOverridePayload(json.RawMessage(`{
		"video_pricing": {
			"version": 1, "enabled": false, "currency": "USD", "typo": true
		}
	}`))
	require.Error(t, err)
}

func TestModelPriceOverrideVideoValidationIncludesActionableMetadata(t *testing.T) {
	profile := &VideoPricingConfig{
		Version: 1, Enabled: true, Currency: "USD",
		Resolutions: map[string]VideoResolutionSpec{"480p": {Sizes: nil}},
		Rules:       []VideoPricingRule{{Key: "default", BillingUnit: VideoBillingUnitSecond, UnitPriceUSD: 0.1}},
	}
	err := validatePayloadNumbers(&ModelPriceOverridePayload{VideoPricing: profile})
	require.Error(t, err)

	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "INVALID_VIDEO_PRICING", appErr.Reason)
	require.Equal(t, "resolutions.480p.sizes", appErr.Metadata["field"])
	require.Equal(t, "a resolution requires at least one size", appErr.Metadata["detail"])
}

func TestModelPriceOverrideSupportsVideoOnlyReplacementInheritanceAndShadow(t *testing.T) {
	catalogProfile := seedanceVideoPricing()
	catalog := &ModelPriceEntry{VideoPricing: catalogProfile, PricePresenceKnown: true, TokenPricingAbsent: true}
	svc := &PricingService{catalogData: map[string]*ModelPriceEntry{"doubao-seedance-2.0-mini-480p": catalog}}

	_, err := svc.validateOverrideWrite("*", "video-only", ModelPriceCurrencyUSD, &ModelPriceOverridePayload{VideoPricing: seedanceVideoPricing()}, true)
	require.NoError(t, err)
	_, err = svc.validateOverrideWrite("*", "mixed-currency-video", ModelPriceCurrencyCNY, &ModelPriceOverridePayload{VideoPricing: seedanceVideoPricing()}, true)
	require.NoError(t, err, "nested video prices remain USD without changing the record currency used by other fields")

	svc.overrideRows = []ModelPriceOverride{{
		Platform: "*", ModelName: "doubao-seedance-2.0-mini-480p", Currency: "USD", Enabled: true,
		Payload: ModelPriceOverridePayload{OutputCostPerToken: ptrPrice(9e-6)},
	}}
	svc.rebuildEffectiveLocked(svc.catalogData)
	inherited := svc.LookupModelPricingStrict("doubao-seedance-2.0-mini-480p")
	require.NotNil(t, inherited.VideoPricing)
	require.Equal(t, "480p-no-video", inherited.VideoPricing.Rules[0].Key)

	replacement := seedanceVideoPricing()
	replacement.Rules[0].UnitPriceUSD = 7e-6
	svc.overrideRows[0].Payload.VideoPricing = replacement
	svc.rebuildEffectiveLocked(svc.catalogData)
	effective := svc.LookupModelPricingStrict("doubao-seedance-2.0-mini-480p")
	require.Equal(t, 7e-6, effective.VideoPricing.Rules[0].UnitPriceUSD)
	require.True(t, effective.VideoPricingOperatorOverride)
	effective.VideoPricing.Rules[0].UnitPriceUSD = 99
	require.Equal(t, 1e-6, svc.catalogData["doubao-seedance-2.0-mini-480p"].VideoPricing.Rules[0].UnitPriceUSD)

	svc.overrideRows[0].Payload.VideoPricing = &VideoPricingConfig{Version: 1, Enabled: false, Currency: "USD"}
	svc.rebuildEffectiveLocked(svc.catalogData)
	shadowed := svc.LookupModelPricingStrict("doubao-seedance-2.0-mini-480p")
	require.NotNil(t, shadowed.VideoPricing)
	require.False(t, shadowed.VideoPricing.Enabled)
}

func TestPricingCatalogParsesVideoOnlyEntry(t *testing.T) {
	body, err := json.Marshal(map[string]any{"video-only": map[string]any{"video_pricing": seedanceVideoPricing()}})
	require.NoError(t, err)
	svc := NewPricingService(&config.Config{}, nil)
	parsed, err := svc.parsePricingData(body)
	require.NoError(t, err)
	require.True(t, hasVideoPricing(parsed["video-only"]))
	require.True(t, parsed["video-only"].TokenPricingAbsent)
}

func TestPricingCatalogRejectsInvalidVideoProfileWithoutDroppingTokenPrice(t *testing.T) {
	svc := NewPricingService(&config.Config{}, nil)
	parsed, err := svc.parsePricingData([]byte(`{
		"mixed": {
			"input_cost_per_token": 0.000001,
			"output_cost_per_token": 0.000002,
			"video_pricing": {"version": 1, "enabled": false, "currency": "USD", "typo": true}
		}
	}`))
	require.NoError(t, err)
	require.NotNil(t, parsed["mixed"])
	require.Nil(t, parsed["mixed"].VideoPricing)
	require.False(t, parsed["mixed"].TokenPricingAbsent)
}

func TestVideoPricingResolverUsesGlobalVideoPriceAndStrictPriority(t *testing.T) {
	const model = "doubao-seedance-2.0-mini-480p"
	pricing := NewPricingService(&config.Config{}, nil)
	pricing.SeedCatalogForTest(map[string]*ModelPriceEntry{
		model: {VideoPricing: seedanceVideoPricing(), PricePresenceKnown: true, TokenPricingAbsent: true},
	})
	resolver := NewVideoPricingResolver(nil, pricing)
	group := &Group{ID: 7, Platform: PlatformOpenAI, RateMultiplier: 1}
	quote, err := resolver.Resolve(context.Background(), VideoPricingResolveRequest{
		Group: group, Platform: PlatformOpenAI,
		Mapping:        ChannelMappingResult{MappedModel: model, BillingModelSource: BillingModelSourceChannelMapped},
		RequestedModel: model, ChannelModel: model, UpstreamModel: model, Provider: VideoProviderOpenAI,
		Attributes: VideoPricingAttributes{Operation: VideoOperationGenerate, Size: "864x480", Seconds: 5},
	})
	require.NoError(t, err)
	require.Equal(t, VideoPricingSourceCatalog, quote.Source)
	require.Equal(t, model, quote.BillingModel)
	require.Equal(t, "480p-no-video", quote.RuleKey)

	overrideProfile := seedanceVideoPricing()
	overrideProfile.Rules[0].UnitPriceUSD = 3e-6
	pricing.SeedOverridesForTest([]ModelPriceOverride{{
		Platform: PlatformOpenAI, ModelName: model, Currency: ModelPriceCurrencyUSD, Enabled: true,
		Payload: ModelPriceOverridePayload{VideoPricing: overrideProfile},
	}})
	quote, err = resolver.Resolve(context.Background(), VideoPricingResolveRequest{
		Group: group, Platform: PlatformOpenAI,
		Mapping:        ChannelMappingResult{MappedModel: model, BillingModelSource: BillingModelSourceChannelMapped},
		RequestedModel: model, ChannelModel: model, UpstreamModel: model, Provider: VideoProviderOpenAI,
		Attributes: VideoPricingAttributes{Operation: VideoOperationGenerate, Size: "864x480", Seconds: 5},
	})
	require.NoError(t, err)
	require.Equal(t, VideoPricingSourceModelPriceOverride, quote.Source)
	require.Equal(t, 3e-6, quote.UnitPrice)

	unit, price := VideoBillingUnitSecond, 0.25
	group.ModelPricing = []ChannelModelPricing{{
		Platform: PlatformOpenAI, Models: []string{model}, BillingMode: BillingModeVideo,
		Intervals: []PricingInterval{{ID: 9, BillingUnit: &unit, PerRequestPrice: &price, Conditions: json.RawMessage(`{"request_modes":["batch"]}`)}},
	}}
	_, err = resolver.Resolve(context.Background(), VideoPricingResolveRequest{
		Group: group, Platform: PlatformOpenAI,
		Mapping:        ChannelMappingResult{MappedModel: model, BillingModelSource: BillingModelSourceChannelMapped},
		RequestedModel: model, ChannelModel: model, UpstreamModel: model, Provider: VideoProviderOpenAI,
		Attributes: VideoPricingAttributes{Size: "864x480", Seconds: 5, RequestMode: "standard"},
	})
	require.ErrorIs(t, err, ErrVideoPricingRuleMissing, "a configured higher-priority source must not fall through")
}

func TestVideoPricingResolverChannelPricePrecedesModelPrice(t *testing.T) {
	const model = "video-channel-priced"
	pricing := NewPricingService(&config.Config{}, nil)
	pricing.SeedCatalogForTest(map[string]*ModelPriceEntry{
		model: {VideoPricing: seedanceVideoPricing(), PricePresenceKnown: true, TokenPricingAbsent: true},
	})
	unit, price := VideoBillingUnitSecond, 0.25
	channel := &Channel{
		ID: 5, Status: StatusActive, BillingModelSource: BillingModelSourceChannelMapped,
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformOpenAI, Models: []string{model}, BillingMode: BillingModeVideo,
			Intervals: []PricingInterval{{ID: 8, BillingUnit: &unit, PerRequestPrice: &price}},
		}},
	}
	cache := newEmptyChannelCache()
	cache.loadedAt = time.Now()
	cache.channelByGroupID[7] = channel
	cache.groupPlatform[7] = PlatformOpenAI
	expandPricingToCache(cache, channel, 7, PlatformOpenAI)
	channels := &ChannelService{}
	channels.cache.Store(cache)
	resolver := NewVideoPricingResolver(channels, pricing)
	group := &Group{ID: 7, Platform: PlatformOpenAI, RateMultiplier: 1}
	request := VideoPricingResolveRequest{
		Group: group, Platform: PlatformOpenAI,
		Mapping:        ChannelMappingResult{MappedModel: model, BillingModelSource: BillingModelSourceChannelMapped},
		RequestedModel: model, ChannelModel: model, UpstreamModel: model, Provider: VideoProviderOpenAI,
		Attributes: VideoPricingAttributes{Size: "864x480", Seconds: 5},
	}
	quote, err := resolver.Resolve(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, VideoPricingSourceChannel, quote.Source)
	require.Equal(t, 0.25, quote.UnitPrice)

	group.ModelPricing = append([]ChannelModelPricing(nil), channel.ModelPricing...)
	group.ModelPricing[0].Intervals = append([]PricingInterval(nil), channel.ModelPricing[0].Intervals...)
	groupPrice := 0.5
	group.ModelPricing[0].Intervals[0].PerRequestPrice = &groupPrice
	quote, err = resolver.Resolve(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, VideoPricingSourceGroup, quote.Source)
	require.Equal(t, 0.5, quote.UnitPrice)
}

func TestVideoPricingResolverRequiresTokenEstimatorForTokenSaleOverride(t *testing.T) {
	const model = "video-token-override"
	perRequestProfile := &VideoPricingConfig{
		Version: 1, Enabled: true, Currency: "USD",
		Rules: []VideoPricingRule{{Key: "request", BillingUnit: VideoBillingUnitRequest, UnitPriceUSD: 1}},
	}
	pricing := NewPricingService(&config.Config{}, nil)
	pricing.SeedCatalogForTest(map[string]*ModelPriceEntry{
		model: {VideoPricing: perRequestProfile, PricePresenceKnown: true, TokenPricingAbsent: true},
	})
	unit, price := VideoBillingUnitVideoToken, 1e-6
	group := &Group{ID: 7, Platform: PlatformOpenAI, ModelPricing: []ChannelModelPricing{{
		Platform: PlatformOpenAI, Models: []string{model}, BillingMode: BillingModeVideo,
		Intervals: []PricingInterval{{BillingUnit: &unit, PerRequestPrice: &price}},
	}}}
	_, err := NewVideoPricingResolver(nil, pricing).Resolve(context.Background(), VideoPricingResolveRequest{
		Group: group, Platform: PlatformOpenAI,
		Mapping:        ChannelMappingResult{MappedModel: model, BillingModelSource: BillingModelSourceChannelMapped},
		RequestedModel: model, ChannelModel: model, UpstreamModel: model,
		Attributes: VideoPricingAttributes{Seconds: 5},
	})
	require.ErrorIs(t, err, ErrVideoPricingEstimatorMissing)
}

func TestVideoPricingResolverRejectsFlatTokenPriceAndResponseModelSource(t *testing.T) {
	const model = "video-flat-only"
	pricing := NewPricingService(&config.Config{}, nil)
	pricing.SeedCatalogForTest(map[string]*ModelPriceEntry{model: pricedEntry(1e-6, 2e-6)})
	resolver := NewVideoPricingResolver(nil, pricing)
	request := VideoPricingResolveRequest{
		Group: &Group{ID: 7, Platform: PlatformOpenAI}, Platform: PlatformOpenAI,
		Mapping:        ChannelMappingResult{MappedModel: model, BillingModelSource: BillingModelSourceChannelMapped},
		RequestedModel: model, ChannelModel: model, UpstreamModel: model,
		Attributes: VideoPricingAttributes{Seconds: 5},
	}
	_, err := resolver.Resolve(context.Background(), request)
	require.ErrorIs(t, err, ErrVideoPricingMissing)

	request.Mapping.BillingModelSource = BillingModelSourceResponse
	_, err = resolver.Resolve(context.Background(), request)
	require.ErrorIs(t, err, ErrVideoPricingInvalid)
}

func TestVideoUsageAliasesNormalizeAndConflict(t *testing.T) {
	clean := sanitizeVideoProviderUsage(map[string]any{"completion_tokens": 120.0, "output_tokens": 120})
	require.Equal(t, 120.0, clean["video_tokens"])
	require.NotContains(t, clean, "completion_tokens")
	require.NotContains(t, clean, "output_tokens")

	conflict := sanitizeVideoProviderUsage(map[string]any{"video_tokens": 100, "output_tokens": 101})
	require.NotContains(t, conflict, "video_tokens")
	require.Equal(t, 1.0, conflict["video_tokens_conflict"])
	require.Equal(t, conflict, sanitizeVideoProviderUsage(conflict), "conflicts survive repeated normalization")

	billingUnit := VideoBillingUnitVideoToken
	task := &VideoTask{BillingUnit: &billingUnit, PriceSnapshot: map[string]any{"unit_price": 1e-6, "customer_multiplier": 1.0}}
	_, _, err := videoActualCost(task, &ProviderVideoTask{Usage: map[string]any{"completion_tokens": 10, "output_tokens": 11}})
	require.Error(t, err)
}
