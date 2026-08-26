//go:build unit

package service

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func newGatewayPricingGuardService(cfg *config.Config) *GatewayService {
	if cfg == nil {
		cfg = &config.Config{RunMode: config.RunModeStandard}
	}
	billingService := NewBillingService(cfg, nil)
	return &GatewayService{
		cfg:            cfg,
		billingService: billingService,
	}
}

func newGatewayPricingGuardChannel(groupID int64, platform, billingModelSource string) (*ChannelService, *channelCache) {
	cache := newEmptyChannelCache()
	cache.channelByGroupID[groupID] = &Channel{
		ID:                 groupID,
		Status:             StatusActive,
		BillingModelSource: billingModelSource,
	}
	cache.groupPlatform[groupID] = platform
	cache.loadedAt = time.Now()

	channelService := &ChannelService{}
	channelService.cache.Store(cache)
	return channelService, cache
}

func TestGatewayServiceValidateUsagePricing(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *config.Config
		account   *Account
		model     string
		wantError bool
	}{
		{
			name:    "Kimi K3 fallback is priced",
			account: &Account{Platform: PlatformKimi},
			model:   "K3",
		},
		{
			name:      "unknown Kimi model is rejected",
			account:   &Account{Platform: PlatformKimi},
			model:     "kimi-k30",
			wantError: true,
		},
		{
			name:      "whitespace-only model is rejected",
			account:   &Account{Platform: PlatformKimi},
			model:     " \t\n",
			wantError: true,
		},
		{
			name:      "nil account is rejected",
			model:     "kimi-k3",
			wantError: true,
		},
		{
			name:      "unknown Gemini model is rejected",
			account:   &Account{Platform: PlatformGemini},
			model:     "gemini-future-v99",
			wantError: true,
		},
		{
			name:      "unknown Anthropic model is rejected",
			account:   &Account{Platform: PlatformAnthropic},
			model:     "anthropic-future-v99",
			wantError: true,
		},
		{
			name: "account mapping to priced Kimi model is allowed",
			account: &Account{
				Platform: PlatformKimi,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"client-alias": "kimi-k3"},
				},
			},
			model: "client-alias",
		},
		{
			name: "priced requested Kimi model cannot hide unpriced upstream model",
			account: &Account{
				Platform: PlatformKimi,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"kimi-k3": "kimi-future-unpriced-v99"},
				},
			},
			model:     "kimi-k3",
			wantError: true,
		},
		{
			name: "globally free requested GLM model cannot hide unpriced upstream model",
			account: &Account{
				Platform: PlatformGLM,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"glm-4.7-flash": "glm-future-unpriced-v99"},
				},
			},
			model:     "glm-4.7-flash",
			wantError: true,
		},
		{
			name: "priced MiniMax alias cannot hide unpriced upstream model",
			account: &Account{
				Platform: PlatformMiniMax,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"MiniMax-M2.7": "MiniMax-Future-Ultra"},
				},
			},
			model:     "MiniMax-M2.7",
			wantError: true,
		},
		{
			name:      "unknown MiniMax SKU cannot inherit M3 substring price in enforce mode",
			cfg:       &config.Config{RunMode: config.RunModeStandard, Pricing: config.PricingConfig{StrictModelMatchMode: config.PricingGuardModeEnforce}},
			account:   &Account{Platform: PlatformMiniMax},
			model:     "MiniMax-M3-future",
			wantError: true,
		},
		{
			name:    "known MiniMax SKU remains priced in enforce mode",
			cfg:     &config.Config{RunMode: config.RunModeStandard, Pricing: config.PricingConfig{StrictModelMatchMode: config.PricingGuardModeEnforce}},
			account: &Account{Platform: PlatformMiniMax},
			model:   "MiniMax-M3",
		},
		{
			name:    "explicit globally free GLM model is allowed",
			account: &Account{Platform: PlatformGLM},
			model:   "glm-4.7-flash",
		},
		{
			name:      "Simple Mode still rejects an unknown model before upstream",
			cfg:       &config.Config{RunMode: config.RunModeSimple},
			account:   &Account{Platform: PlatformKimi},
			model:     "kimi-future-v99",
			wantError: true,
		},
		{
			name:      "Simple Mode still rejects an empty billing identity",
			cfg:       &config.Config{RunMode: config.RunModeSimple},
			account:   &Account{Platform: PlatformKimi},
			model:     "\u2003",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newGatewayPricingGuardService(tt.cfg)
			err := svc.ValidateUsagePricing(context.Background(), nil, tt.account, tt.model)
			if tt.wantError {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGatewayServiceValidateUsagePricing_AcceptsDeepSeekPlatformOverride(t *testing.T) {
	const model = "deepseek-v4-flash-vision-exp"
	inputPrice := 1.5e-6
	outputPrice := 4.5e-6
	pricingService := NewPricingService(&config.Config{}, nil)
	pricingService.SeedCatalogForTest(map[string]*ModelPriceEntry{})
	pricingService.SeedOverridesForTest([]ModelPriceOverride{{
		Platform:  PlatformDeepSeek,
		ModelName: model,
		Enabled:   true,
		Payload: ModelPriceOverridePayload{
			InputCostPerToken:  &inputPrice,
			OutputCostPerToken: &outputPrice,
		},
	}})

	cfg := &config.Config{RunMode: config.RunModeStandard}
	svc := newGatewayPricingGuardService(cfg)
	svc.billingService = NewBillingService(cfg, pricingService)
	groupID := int64(81)
	apiKey := &APIKey{
		GroupID: &groupID,
		Group:   &Group{ID: groupID, Platform: PlatformDeepSeek},
	}

	require.NoError(t, svc.ValidateUsagePricing(
		context.Background(),
		apiKey,
		&Account{Platform: PlatformDeepSeek},
		model,
	))
	require.ErrorIs(t, svc.ValidateUsagePricing(
		context.Background(),
		&APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI}},
		&Account{Platform: PlatformOpenAI},
		model,
	), ErrModelPricingUnavailable)
}

func TestGatewayServiceValidateUsagePricing_CompositeUsesOrderedPlatformOverrides(t *testing.T) {
	const model = "deepseek-v4-flash-vision-exp"
	for _, tc := range []struct {
		name             string
		overridePlatform string
	}{
		{name: "composite override", overridePlatform: PlatformComposite},
		{name: "provider fallback", overridePlatform: PlatformDeepSeek},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputPrice := 1.5e-6
			outputPrice := 4.5e-6
			pricingService := NewPricingService(&config.Config{}, nil)
			pricingService.SeedCatalogForTest(map[string]*ModelPriceEntry{})
			pricingService.SeedOverridesForTest([]ModelPriceOverride{{
				Platform:  tc.overridePlatform,
				ModelName: model,
				Enabled:   true,
				Payload: ModelPriceOverridePayload{
					InputCostPerToken:  &inputPrice,
					OutputCostPerToken: &outputPrice,
				},
			}})

			cfg := &config.Config{RunMode: config.RunModeStandard}
			svc := newGatewayPricingGuardService(cfg)
			svc.billingService = NewBillingService(cfg, pricingService)
			groupID := int64(83)
			apiKey := &APIKey{
				GroupID: &groupID,
				Group:   &Group{ID: groupID, Platform: PlatformComposite},
			}

			require.NoError(t, svc.ValidateUsagePricing(
				context.Background(), apiKey, &Account{Platform: PlatformDeepSeek}, model,
			))
		})
	}
}

func TestGatewayServiceValidateUsagePricing_RejectsInvalidGlobalCatalogPrice(t *testing.T) {
	tests := []struct {
		name  string
		price float64
	}{
		{name: "negative", price: -1},
		{name: "positive infinity", price: math.Inf(1)},
		{name: "not a number", price: math.NaN()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := "generic-invalid-catalog-" + strings.ReplaceAll(tt.name, " ", "-")
			cfg := &config.Config{
				RunMode: config.RunModeStandard,
				Pricing: config.PricingConfig{
					StrictModelMatchMode: config.PricingGuardModeEnforce,
				},
			}
			pricingService := &PricingService{
				pricingData: map[string]*ModelPriceEntry{
					model: {
						InputCostPerToken:  tt.price,
						OutputCostPerToken: 1e-6,
					},
				},
			}
			svc := newGatewayPricingGuardService(cfg)
			svc.billingService = NewBillingService(cfg, pricingService)

			err := svc.ValidateUsagePricing(
				context.Background(),
				nil,
				&Account{Platform: PlatformKimi},
				model,
			)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
		})
	}
}

func TestGatewayServiceValidateUsagePricing_RejectsPricedChannelModelWhenActualUpstreamIsUnknown(t *testing.T) {
	groupID := int64(76)
	channelService, cache := newGatewayPricingGuardChannel(groupID, PlatformKimi, BillingModelSourceChannelMapped)
	cache.mappingByGroupModel[channelModelKey{
		groupID:  groupID,
		platform: PlatformKimi,
		model:    "public-alias",
	}] = "kimi-k3"

	svc := newGatewayPricingGuardService(nil)
	svc.channelService = channelService
	apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformKimi}}
	account := &Account{
		Platform: PlatformKimi,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"kimi-k3": "kimi-future-unpriced-v99"},
		},
	}

	require.ErrorIs(t, svc.ValidateUsagePricing(
		context.Background(),
		apiKey,
		account,
		"public-alias",
	), ErrModelPricingUnavailable)
}

func TestGatewayPricingIdentityPreventsSecondChannelMappingPass(t *testing.T) {
	const (
		groupID         int64 = 77
		requestedModel        = "gemini-public-alias"
		routedModel           = "gemini-native-unpriced-route"
		secondPassModel       = "gemini-priced-second-pass"
	)
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	channelService, cache := newGatewayPricingGuardChannel(
		groupID,
		PlatformGemini,
		BillingModelSourceChannelMapped,
	)
	cache.mappingByGroupModel[channelModelKey{
		groupID: groupID, platform: PlatformGemini, model: requestedModel,
	}] = routedModel
	cache.mappingByGroupModel[channelModelKey{
		groupID: groupID, platform: PlatformGemini, model: routedModel,
	}] = secondPassModel

	pricingService := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			secondPassModel: {
				InputCostPerToken:  1e-6,
				OutputCostPerToken: 2e-6,
			},
		},
	}
	svc := newGatewayPricingGuardService(cfg)
	svc.billingService = NewBillingService(cfg, pricingService)
	svc.channelService = channelService
	groupIDCopy := groupID
	apiKey := &APIKey{
		GroupID: &groupIDCopy,
		Group:   &Group{ID: groupID, Platform: PlatformGemini},
	}
	account := &Account{Platform: PlatformGemini, Type: AccountTypeOAuth}

	// This is the vulnerable shape: treating the already-routed model as a new
	// request maps it again and proves the price of a model that will not be sent.
	require.NoError(t, svc.ValidateUsagePricing(
		context.Background(),
		apiKey,
		account,
		routedModel,
	))

	mapping := channelService.ResolveRequestChannelMapping(
		context.Background(),
		apiKey.GroupID,
		requestedModel,
	)
	ctx := WithResolvedChannelPricingIdentity(context.Background(), requestedModel, mapping)
	err := svc.ValidateUsagePricing(ctx, apiKey, account, routedModel)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, `requested_model="`+requestedModel+`"`)
	require.ErrorContains(t, err, `mapped_model="`+routedModel+`"`)
	require.ErrorContains(t, err, `upstream_model="`+routedModel+`"`)
}

func TestGatewayPricingIdentityPreservesExplicitZeroRequestedAliasPrice(t *testing.T) {
	const (
		groupID        int64 = 78
		requestedModel       = "gemini-explicitly-free-alias"
		routedModel          = "gemini-native-unpriced-route"
	)
	channelService, cache := newGatewayPricingGuardChannel(
		groupID,
		PlatformGemini,
		BillingModelSourceRequested,
	)
	cache.mappingByGroupModel[channelModelKey{
		groupID: groupID, platform: PlatformGemini, model: requestedModel,
	}] = routedModel
	zero := 0.0
	cache.pricingByGroupModel[channelModelKey{
		groupID: groupID, platform: PlatformGemini, model: requestedModel,
	}] = &ChannelModelPricing{
		Platform:        PlatformGemini,
		BillingMode:     BillingModeToken,
		InputPrice:      &zero,
		OutputPrice:     &zero,
		CacheWritePrice: &zero,
		CacheReadPrice:  &zero,
	}

	svc := newGatewayPricingGuardService(nil)
	svc.channelService = channelService
	groupIDCopy := groupID
	apiKey := &APIKey{
		GroupID: &groupIDCopy,
		Group:   &Group{ID: groupID, Platform: PlatformGemini},
	}
	mapping := channelService.ResolveRequestChannelMapping(
		context.Background(),
		apiKey.GroupID,
		requestedModel,
	)
	ctx := WithResolvedChannelPricingIdentity(context.Background(), requestedModel, mapping)

	require.NoError(t, svc.ValidateUsagePricing(
		ctx,
		apiKey,
		&Account{Platform: PlatformGemini, Type: AccountTypeOAuth},
		routedModel,
	))
}

func TestGatewayPricingIdentityPreventsChannelRestrictionSecondPass(t *testing.T) {
	const (
		groupID         int64 = 79
		requestedModel        = "gemini-public-alias"
		routedModel           = "gemini-restricted-routed-model"
		secondPassModel       = "gemini-allowed-second-pass"
	)
	channelService, cache := newGatewayPricingGuardChannel(
		groupID,
		PlatformGemini,
		BillingModelSourceChannelMapped,
	)
	cache.channelByGroupID[groupID].RestrictModels = true
	cache.mappingByGroupModel[channelModelKey{
		groupID: groupID, platform: PlatformGemini, model: requestedModel,
	}] = routedModel
	cache.mappingByGroupModel[channelModelKey{
		groupID: groupID, platform: PlatformGemini, model: routedModel,
	}] = secondPassModel
	zero := 0.0
	cache.pricingByGroupModel[channelModelKey{
		groupID: groupID, platform: PlatformGemini, model: secondPassModel,
	}] = &ChannelModelPricing{
		Platform:    PlatformGemini,
		BillingMode: BillingModeToken,
		InputPrice:  &zero,
		OutputPrice: &zero,
	}

	svc := newGatewayPricingGuardService(nil)
	svc.channelService = channelService
	groupIDCopy := groupID
	require.False(t, svc.checkChannelPricingRestriction(
		context.Background(),
		&groupIDCopy,
		routedModel,
	), "a second mapping pass incorrectly makes the routed model look allowed")

	mapping := channelService.ResolveRequestChannelMapping(
		context.Background(),
		&groupIDCopy,
		requestedModel,
	)
	ctx := WithResolvedChannelPricingIdentity(context.Background(), requestedModel, mapping)
	require.True(t, svc.checkChannelPricingRestriction(
		ctx,
		&groupIDCopy,
		routedModel,
	))
}

func TestGatewayFinalGeminiImageGuardDefersImageOnlySKUToExactMediaAdmission(t *testing.T) {
	const imageModel = "gemini-3-pro-image"
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	svc := newGatewayPricingGuardService(cfg)
	svc.billingService = NewBillingService(cfg, &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			imageModel: {
				TokenPricingAbsent: true,
				OutputCostPerImage: 0.04,
			},
		},
	})
	account := &Account{Platform: PlatformGemini, Type: AccountTypeOAuth}

	require.ErrorIs(t, svc.ValidateUsagePricing(
		context.Background(),
		nil,
		account,
		imageModel,
	), ErrModelPricingUnavailable,
		"ordinary token routes must not treat an image-only catalog entry as token pricing")

	require.NoError(t, svc.ValidateUsagePricing(
		WithFinalGeminiImagePricingGuard(context.Background()),
		nil,
		account,
		imageModel,
	))
}

func TestGatewayServiceValidateUsagePricing_UnknownUpstreamRequiresChannelPriceOnSelectedBillingModel(t *testing.T) {
	const (
		requestedModel = "public-unknown-alias"
		channelModel   = "channel-unknown-alias"
		upstreamModel  = "kimi-future-unpriced-v99"
	)
	tests := []struct {
		name               string
		billingModelSource string
		pricedModel        string
		wantError          bool
	}{
		{
			name:               "requested source uses explicit requested price",
			billingModelSource: BillingModelSourceRequested,
			pricedModel:        requestedModel,
		},
		{
			name:               "channel source uses explicit channel-mapped price",
			billingModelSource: BillingModelSourceChannelMapped,
			pricedModel:        channelModel,
		},
		{
			name:               "upstream source uses explicit upstream price",
			billingModelSource: BillingModelSourceUpstream,
			pricedModel:        upstreamModel,
		},
		{
			name:               "price on another SKU cannot satisfy requested source",
			billingModelSource: BillingModelSourceRequested,
			pricedModel:        channelModel,
			wantError:          true,
		},
		{
			name:               "price on another SKU cannot satisfy channel source",
			billingModelSource: BillingModelSourceChannelMapped,
			pricedModel:        requestedModel,
			wantError:          true,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groupID := int64(90 + i)
			channelService, cache := newGatewayPricingGuardChannel(groupID, PlatformKimi, tt.billingModelSource)
			cache.mappingByGroupModel[channelModelKey{
				groupID:  groupID,
				platform: PlatformKimi,
				model:    requestedModel,
			}] = channelModel

			zero := 0.0
			cache.pricingByGroupModel[channelModelKey{
				groupID:  groupID,
				platform: PlatformKimi,
				model:    tt.pricedModel,
			}] = &ChannelModelPricing{
				Platform:        PlatformKimi,
				BillingMode:     BillingModeToken,
				InputPrice:      &zero,
				OutputPrice:     &zero,
				CacheWritePrice: &zero,
				CacheReadPrice:  &zero,
			}

			svc := newGatewayPricingGuardService(nil)
			svc.channelService = channelService
			apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformKimi}}
			account := &Account{
				Platform: PlatformKimi,
				Credentials: map[string]any{
					"model_mapping": map[string]any{channelModel: upstreamModel},
				},
			}

			err := svc.ValidateUsagePricing(context.Background(), apiKey, account, requestedModel)
			if tt.wantError {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGatewayServiceValidateUsagePricing_AllowsExplicitZeroChannelPrice(t *testing.T) {
	groupID := int64(73)
	model := "kimi-future-free"
	zero := 0.0
	cache := newEmptyChannelCache()
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, platform: PlatformKimi, model: model}] = &ChannelModelPricing{
		Platform:        PlatformKimi,
		BillingMode:     BillingModeToken,
		InputPrice:      &zero,
		OutputPrice:     &zero,
		CacheWritePrice: &zero,
		CacheReadPrice:  &zero,
	}
	cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
	cache.groupPlatform[groupID] = PlatformKimi
	cache.loadedAt = time.Now()

	channelService := &ChannelService{}
	channelService.cache.Store(cache)
	svc := newGatewayPricingGuardService(nil)
	svc.channelService = channelService
	svc.resolver = NewModelPricingResolver(channelService, svc.billingService)
	apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformKimi}}

	require.NoError(t, svc.ValidateUsagePricing(
		context.Background(),
		apiKey,
		&Account{Platform: PlatformKimi},
		model,
	))
}

func TestGatewayServiceValidateUsagePricing_RejectsPartialUnknownChannelTokenPrice(t *testing.T) {
	groupID := int64(74)
	model := "kimi-future-partial"
	zero := 0.0
	cache := newEmptyChannelCache()
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, platform: PlatformKimi, model: model}] = &ChannelModelPricing{
		Platform:    PlatformKimi,
		BillingMode: BillingModeToken,
		InputPrice:  &zero,
	}
	cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
	cache.groupPlatform[groupID] = PlatformKimi
	cache.loadedAt = time.Now()

	channelService := &ChannelService{}
	channelService.cache.Store(cache)
	svc := newGatewayPricingGuardService(nil)
	svc.channelService = channelService
	svc.resolver = NewModelPricingResolver(channelService, svc.billingService)
	apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformKimi}}

	require.ErrorIs(t, svc.ValidateUsagePricing(
		context.Background(),
		apiKey,
		&Account{Platform: PlatformKimi},
		model,
	), ErrModelPricingUnavailable)
}

func TestGatewayServiceValidateUsagePricing_RejectsInvalidChannelOverrideDespiteGlobalPrice(t *testing.T) {
	tests := []struct {
		name  string
		price float64
	}{
		{name: "negative", price: -1},
		{name: "positive infinity", price: math.Inf(1)},
		{name: "not a number", price: math.NaN()},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groupID := int64(170 + i)
			channelService, cache := newGatewayPricingGuardChannel(groupID, PlatformKimi, BillingModelSourceUpstream)
			price := tt.price
			cache.pricingByGroupModel[channelModelKey{
				groupID:  groupID,
				platform: PlatformKimi,
				model:    "kimi-k3",
			}] = &ChannelModelPricing{
				Platform:    PlatformKimi,
				BillingMode: BillingModeToken,
				InputPrice:  &price,
			}

			svc := newGatewayPricingGuardService(nil)
			svc.channelService = channelService
			apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformKimi}}

			require.ErrorIs(t, svc.ValidateUsagePricing(
				context.Background(),
				apiKey,
				&Account{Platform: PlatformKimi},
				"kimi-k3",
			), ErrModelPricingUnavailable)
		})
	}
}

func TestGatewayServiceValidateUsagePricing_RejectsPerRequestTierWithoutDefaultDespiteGlobalPrice(t *testing.T) {
	groupID := int64(75)
	zero := 0.0
	cache := newEmptyChannelCache()
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, platform: PlatformKimi, model: "kimi-k3"}] = &ChannelModelPricing{
		Platform:    PlatformKimi,
		BillingMode: BillingModePerRequest,
		Intervals: []PricingInterval{{
			TierLabel:       "standard",
			PerRequestPrice: &zero,
		}},
	}
	cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
	cache.groupPlatform[groupID] = PlatformKimi
	cache.loadedAt = time.Now()

	channelService := &ChannelService{}
	channelService.cache.Store(cache)
	svc := newGatewayPricingGuardService(nil)
	svc.channelService = channelService
	svc.resolver = NewModelPricingResolver(channelService, svc.billingService)
	apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformKimi}}

	require.ErrorIs(t, svc.ValidateUsagePricing(
		context.Background(),
		apiKey,
		&Account{Platform: PlatformKimi},
		"kimi-k3",
	), ErrModelPricingUnavailable)
}

func TestGatewayServiceValidateUsagePricing_RejectsUnpricedModelForResponsesImageIntent(t *testing.T) {
	svc := newGatewayPricingGuardService(nil)
	ctx := WithOpenAIImageGenerationIntent(context.Background())

	require.ErrorIs(t, svc.ValidateUsagePricing(
		ctx,
		nil,
		&Account{Platform: PlatformGemini},
		"gemini-future-image-v99",
	), ErrModelPricingUnavailable)
}

func TestGatewayServiceValidateUsagePricing_RejectsTokenPricedButImageUnpricedModel(t *testing.T) {
	svc := newGatewayPricingGuardService(nil)
	account := &Account{Platform: PlatformKimi}

	require.NoError(t, svc.ValidateUsagePricing(
		context.Background(),
		nil,
		account,
		"K3",
	), "control: the model must have complete token pricing")

	err := svc.ValidateUsagePricing(
		WithOpenAIImageGenerationIntent(context.Background()),
		nil,
		account,
		"K3",
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, "billing_kind=image")
}

func TestGatewayServiceValidateUsagePricing_AllowsTokenAndImagePricedModel(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	svc := newGatewayPricingGuardService(cfg)
	pricingService := NewPricingService(cfg, nil)
	pricingService.pricingData["gateway-text-and-image-priced"] = &ModelPriceEntry{
		InputCostPerToken:  1e-6,
		OutputCostPerToken: 2e-6,
		OutputCostPerImage: 0.03,
	}
	svc.billingService.pricingService = pricingService

	require.NoError(t, svc.ValidateUsagePricing(
		WithOpenAIImageGenerationIntent(context.Background()),
		nil,
		&Account{Platform: PlatformGemini},
		"gateway-text-and-image-priced",
	))
}

func TestGatewayServiceResponsesImageIntentRejectsBeforeAnyUpstreamCall(t *testing.T) {
	account := Account{
		ID:          104,
		Platform:    PlatformKimi,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	upstream := &httpUpstreamRecorder{}
	svc := newGatewayPricingGuardService(nil)
	pricingService := NewPricingService(svc.cfg, nil)
	pricingService.pricingData["gateway-text-with-image-price"] = &ModelPriceEntry{
		InputCostPerToken:  1e-6,
		OutputCostPerToken: 2e-6,
		OutputCostPerImage: 0.03,
	}
	svc.billingService.pricingService = pricingService
	svc.accountRepo = repo
	svc.httpUpstream = upstream
	body := []byte(`{
		"model":"gateway-text-with-image-price",
		"input":[{
			"type":"additional_tools",
			"tools":[{"type":"image_generation","model":"unpriced-image-tool-v99","size":"3840x2160"}]
		}]
	}`)
	require.True(t, IsExplicitImageGenerationIntent(openAIResponsesEndpoint, "gateway-text-with-image-price", body))
	ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformKimi)
	imageCfg, cfgErr := ResolveOpenAIResponsesImageBillingConfigFromBody(body, "gateway-text-with-image-price")
	require.NoError(t, cfgErr)
	require.True(t, imageCfg.NativeTool)
	require.Equal(t, "unpriced-image-tool-v99", imageCfg.Model)
	require.Equal(t, ImageBillingSize4K, imageCfg.SizeTier)
	ctx = WithOpenAIImageGenerationPricingIntent(ctx, imageCfg.Model, imageCfg.SizeTier)

	selected, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gateway-text-with-image-price", nil)

	require.Nil(t, selected)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, `media_billing_model="unpriced-image-tool-v99"`)
	require.ErrorContains(t, err, `media_size_tier="4K"`)
	require.Nil(t, upstream.lastReq)
}

func TestGatewayServiceValidateUsagePricing_UsesExactNestedImageToolModelAndTier(t *testing.T) {
	svc := newGatewayPricingGuardService(nil)
	pricingService := NewPricingService(svc.cfg, nil)
	pricingService.pricingData["gateway-text-with-image-price"] = &ModelPriceEntry{
		InputCostPerToken:  1e-6,
		OutputCostPerToken: 2e-6,
		OutputCostPerImage: 0.03,
	}
	pricingService.pricingData["priced-nested-image-tool"] = &ModelPriceEntry{
		TokenPricingAbsent: true,
		OutputCostPerImage: 0.07,
	}
	svc.billingService.pricingService = pricingService
	account := &Account{Platform: PlatformKimi}

	// Control: the legacy coarse intent can be admitted by the top-level
	// model's image price.
	require.NoError(t, svc.ValidateUsagePricing(
		WithOpenAIImageGenerationIntent(context.Background()),
		nil,
		account,
		"gateway-text-with-image-price",
	))

	err := svc.ValidateUsagePricing(
		WithOpenAIImageGenerationPricingIntent(
			context.Background(),
			"unpriced-nested-image-tool",
			ImageBillingSize4K,
		),
		nil,
		account,
		"gateway-text-with-image-price",
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, `media_billing_model="unpriced-nested-image-tool"`)
	require.ErrorContains(t, err, `media_size_tier="4K"`)

	require.NoError(t, svc.ValidateUsagePricing(
		WithOpenAIImageGenerationPricingIntent(
			context.Background(),
			"priced-nested-image-tool",
			ImageBillingSize4K,
		),
		nil,
		account,
		"gateway-text-with-image-price",
	))
}

func TestGatewayServiceSelectAccountForModelWithExclusionsRejectsUnpricedDynamicModel(t *testing.T) {
	account := Account{
		ID:          101,
		Platform:    PlatformKimi,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	svc := newGatewayPricingGuardService(nil)
	svc.accountRepo = repo
	ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformKimi)

	selected, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "kimi-future-v99", nil)

	require.Nil(t, selected)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestGatewayServiceNonBillingEndpointPricingExemptionIsExplicit(t *testing.T) {
	account := Account{
		ID:          103,
		Platform:    PlatformKimi,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	svc := newGatewayPricingGuardService(nil)
	svc.accountRepo = repo
	ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformKimi)

	selected, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "", nil)
	require.Nil(t, selected)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	selected, err = svc.SelectAccountForModelForNonBillingEndpoint(ctx, nil, "", "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, account.ID, selected.ID)
}

func TestGatewayServiceLoadAwareNonBillingEndpointPricingExemption(t *testing.T) {
	account := Account{
		ID:          104,
		Platform:    PlatformKimi,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	svc := newGatewayPricingGuardService(nil)
	svc.accountRepo = repo
	ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformKimi)

	selection, err := svc.SelectAccountWithLoadAwarenessForNonBillingEndpoint(
		ctx,
		nil,
		"models-endpoint",
		"",
		nil,
		"",
		0,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
}

func TestGatewayServiceProductionPricingGuardFailsClosedWhenBillingDependencyMissing(t *testing.T) {
	svc := &GatewayService{
		cfg:                  &config.Config{RunMode: config.RunModeStandard},
		pricingGuardRequired: true,
	}

	selected, err := svc.hydrateAndValidateSelectedAccount(
		context.Background(),
		nil,
		"kimi-k3",
		&Account{ID: 1001, Platform: PlatformKimi},
	)
	require.Nil(t, selected)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestGatewayServiceSelectAccountForModelWithExclusionsRejectsUnpricedGeminiModel(t *testing.T) {
	account := Account{
		ID:          102,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	svc := newGatewayPricingGuardService(nil)
	svc.accountRepo = repo
	ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformGemini)

	selected, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", "gemini-future-v99", nil)

	require.Nil(t, selected)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestGatewayServiceNewSelectionResultReleasesAcquiredSlotOnPricingError(t *testing.T) {
	svc := newGatewayPricingGuardService(nil)
	releaseCalls := 0

	selection, err := svc.newSelectionResult(
		context.Background(),
		nil,
		"kimi-future-v99",
		&Account{ID: 101, Platform: PlatformKimi},
		true,
		func() { releaseCalls++ },
		nil,
	)

	require.Nil(t, selection)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, 1, releaseCalls)
}
