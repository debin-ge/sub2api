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

func newOpenAIPricingGuardService(cfg *config.Config) *OpenAIGatewayService {
	if cfg == nil {
		cfg = &config.Config{RunMode: config.RunModeStandard}
	}
	return &OpenAIGatewayService{
		cfg:            cfg,
		billingService: NewBillingService(cfg, nil),
	}
}

func newOpenAIPricingGuardChannel(groupID int64, billingModelSource string) (*ChannelService, *channelCache) {
	cache := newEmptyChannelCache()
	cache.channelByGroupID[groupID] = &Channel{
		ID:                 groupID,
		Status:             StatusActive,
		BillingModelSource: billingModelSource,
	}
	cache.groupPlatform[groupID] = PlatformOpenAI
	cache.loadedAt = time.Now()
	channelService := &ChannelService{}
	channelService.cache.Store(cache)
	return channelService, cache
}

func TestOpenAIGatewayServiceValidateSelectedOpenAIModelPricing(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *config.Config
		account   *Account
		model     string
		wantError bool
	}{
		{
			name:    "known model is priced",
			account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
			model:   "gpt-5.4",
		},
		{
			name:      "unknown model is rejected",
			account:   &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
			model:     "gpt-future-unpriced-v99",
			wantError: true,
		},
		{
			name:      "whitespace-only model is rejected",
			account:   &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
			model:     " \t\n",
			wantError: true,
		},
		{
			name:      "nil account is rejected",
			model:     "gpt-5.4",
			wantError: true,
		},
		{
			name: "account mapping to priced model is allowed",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"public-alias": "gpt-5.4"},
				},
			},
			model: "public-alias",
		},
		{
			name: "priced requested model mapped to unpriced upstream is rejected",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-5.4": "gpt-future-unpriced-v99"},
				},
			},
			model:     "gpt-5.4",
			wantError: true,
		},
		{
			name: "globally free requested model mapped to unpriced upstream is rejected",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"glm-4.5-flash": "vendor-future-unpriced-v99"},
				},
			},
			model:     "glm-4.5-flash",
			wantError: true,
		},
		{
			name:      "Simple Mode still rejects unknown model before upstream",
			cfg:       &config.Config{RunMode: config.RunModeSimple},
			account:   &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
			model:     "gpt-future-unpriced-v99",
			wantError: true,
		},
		{
			name:      "Simple Mode still rejects an empty billing identity",
			cfg:       &config.Config{RunMode: config.RunModeSimple},
			account:   &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
			model:     "\u2003",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newOpenAIPricingGuardService(tt.cfg)
			err := svc.ValidateSelectedOpenAIModelPricing(context.Background(), nil, tt.account, tt.model, false)
			if tt.wantError {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestOpenAIPricingGuardsRejectNilAccount(t *testing.T) {
	svc := newOpenAIPricingGuardService(nil)
	ctx := context.Background()

	require.ErrorIs(t, svc.ValidateSelectedOpenAIMessagesPricing(
		ctx, nil, nil, "gpt-5.4", ChannelMappingResult{MappedModel: "gpt-5.4"}, "",
	), ErrModelPricingUnavailable)
	for _, kind := range []BillingKind{BillingKindImage, BillingKindVideo} {
		require.ErrorIs(t, svc.ValidateSelectedOpenAIMediaPricing(
			ctx, nil, nil, "gpt-image-1", kind,
		), ErrModelPricingUnavailable)
		require.ErrorIs(t, svc.enforceResolvedOpenAIMediaPricing(
			ctx, nil, nil, "gpt-image-1", "gpt-image-1", ImageBillingSize1K, kind,
		), ErrModelPricingUnavailable)
	}
	require.ErrorIs(t, svc.enforceResolvedOpenAITokenPricing(
		ctx, nil, nil, "gpt-5.4", "gpt-5.4",
	), ErrModelPricingUnavailable)
}

func TestOpenAIGatewayServiceValidateSelectedOpenAIModelPricing_RejectsInvalidGlobalCatalogPrice(t *testing.T) {
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
			model := "openai-invalid-catalog-" + strings.ReplaceAll(tt.name, " ", "-")
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
			svc := newOpenAIPricingGuardService(cfg)
			svc.billingService = NewBillingService(cfg, pricingService)

			err := svc.ValidateSelectedOpenAIModelPricing(
				context.Background(),
				nil,
				&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				model,
				false,
			)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
		})
	}
}

func TestOpenAIGatewayServiceValidateSelectedOpenAIModelPricing_AllowsChannelMappingToPricedModel(t *testing.T) {
	groupID := int64(81)
	channelService, cache := newOpenAIPricingGuardChannel(groupID, BillingModelSourceChannelMapped)
	cache.mappingByGroupModel[channelModelKey{groupID: groupID, platform: PlatformOpenAI, model: "public-alias"}] = "gpt-5.4"
	svc := newOpenAIPricingGuardService(nil)
	svc.channelService = channelService

	require.NoError(t, svc.ValidateSelectedOpenAIModelPricing(
		context.Background(),
		&groupID,
		&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		"public-alias",
		false,
	))
}

func TestOpenAIGatewayServiceValidateSelectedOpenAIModelPricing_AllowsUnpricedUpstreamWithExplicitSelectedChannelPrice(t *testing.T) {
	groupID := int64(83)
	channelService, cache := newOpenAIPricingGuardChannel(groupID, BillingModelSourceRequested)
	cache.mappingByGroupModel[channelModelKey{groupID: groupID, platform: PlatformOpenAI, model: "gpt-5.4"}] = "public-channel-alias"
	price := 1e-6
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, platform: PlatformOpenAI, model: "gpt-5.4"}] = &ChannelModelPricing{
		Platform:        PlatformOpenAI,
		BillingMode:     BillingModeToken,
		InputPrice:      &price,
		OutputPrice:     &price,
		CacheWritePrice: &price,
		CacheReadPrice:  &price,
	}
	svc := newOpenAIPricingGuardService(nil)
	svc.channelService = channelService

	require.NoError(t, svc.ValidateSelectedOpenAIModelPricing(
		context.Background(),
		&groupID,
		&Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"public-channel-alias": "vendor-future-unpriced-v99"},
			},
		},
		"gpt-5.4",
		false,
	))
}

func TestOpenAIGatewayServiceValidateSelectedOpenAIMessagesPricing_UsesExactForwardResolution(t *testing.T) {
	svc := newOpenAIPricingGuardService(nil)
	mapping := ChannelMappingResult{
		Mapped:             true,
		MappedModel:        "channel-unknown-alias-v99",
		BillingModelSource: BillingModelSourceUpstream,
	}

	// With no matching account mapping, the Messages dispatch model is the
	// actual forward model and therefore supplies valid pricing.
	require.NoError(t, svc.ValidateSelectedOpenAIMessagesPricing(
		context.Background(),
		nil,
		&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		"public-unknown-alias-v99",
		mapping,
		"gpt-5.4",
	))

	// A matching account mapping wins over the dispatch fallback. The guard
	// must validate that actual target instead of accepting the priced dispatch
	// model that the scheduler used for account selection.
	err := svc.ValidateSelectedOpenAIMessagesPricing(
		context.Background(),
		nil,
		&Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"channel-unknown-alias-v99": "gpt-future-unpriced-v99",
				},
			},
		},
		"public-unknown-alias-v99",
		mapping,
		"gpt-5.4",
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	// A globally priced requested model must not conceal an account mapping to
	// an unpriced concrete upstream SKU.
	err = svc.ValidateSelectedOpenAIMessagesPricing(
		context.Background(),
		nil,
		&Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5.4": "gpt-future-unpriced-v99",
				},
			},
		},
		"gpt-5.4",
		ChannelMappingResult{
			MappedModel:        "gpt-5.4",
			BillingModelSource: BillingModelSourceUpstream,
		},
		"gpt-5.4",
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestOpenAIGatewayPaidPricingValidatorsRejectWhitespaceOnlyModel(t *testing.T) {
	svc := newOpenAIPricingGuardService(nil)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	require.ErrorIs(t, svc.ValidateSelectedOpenAIMessagesPricing(
		context.Background(),
		nil,
		account,
		" \t",
		ChannelMappingResult{},
		"",
	), ErrModelPricingUnavailable)
	require.ErrorIs(t, svc.ValidateSelectedOpenAIMediaPricing(
		context.Background(),
		nil,
		account,
		"\u2003",
		BillingKindImage,
	), ErrModelPricingUnavailable)
}

func TestOpenAIPricingGuardBlankModelRequiresExplicitNonBillingKind(t *testing.T) {
	svc := newOpenAIPricingGuardService(nil)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	for _, kind := range []BillingKind{
		BillingKindUnspecified,
		BillingKindToken,
		BillingKindImage,
		BillingKindVideo,
		BillingKindWebSearch,
	} {
		t.Run(kind.String(), func(t *testing.T) {
			err := svc.validateSelectedPricingForBillingKind(
				context.Background(),
				nil,
				account,
				" \t",
				false,
				kind,
			)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
		})
	}

	require.NoError(t, svc.validateSelectedPricingForBillingKind(
		context.Background(),
		nil,
		account,
		" \t",
		false,
		BillingKindNone,
	))

	require.ErrorIs(t, svc.validateSelectedPricingForBillingKind(
		context.Background(),
		nil,
		account,
		"gpt-future-unpriced-v99",
		false,
		BillingKindUnspecified,
	), ErrModelPricingUnavailable)

	require.ErrorIs(t, svc.validateSelectedPricingForBillingKind(
		context.Background(),
		nil,
		account,
		"gpt-5.4",
		false,
		BillingKind("future_kind"),
	), ErrModelPricingUnavailable)
}

func TestOpenAIMediaPricingGuardCannotBeDisabledOrBypassedBySimpleMode(t *testing.T) {
	for _, tt := range []struct {
		name      string
		runMode   string
		guardMode string
	}{
		{
			name:      "off is treated as enforce",
			runMode:   config.RunModeStandard,
			guardMode: config.PricingGuardModeOff,
		},
		{
			name:      "shadow is treated as enforce",
			runMode:   config.RunModeStandard,
			guardMode: config.PricingGuardModeShadow,
		},
		{
			name:      "simple mode still enforces",
			runMode:   config.RunModeSimple,
			guardMode: config.PricingGuardModeOff,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				RunMode: tt.runMode,
				Pricing: config.PricingConfig{GuardMode: tt.guardMode},
			}
			svc := newOpenAIPricingGuardService(cfg)
			err := svc.enforceResolvedOpenAIMediaPricing(
				context.Background(),
				nil,
				&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				"unknown-image-alias",
				"unknown-image-upstream-v99",
				ImageBillingSize1K,
				BillingKindImage,
			)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
		})
	}
}

func TestOpenAIGatewayServiceValidateSelectedOpenAIModelPricing_ChannelPriceSemantics(t *testing.T) {
	groupID := int64(82)
	zero := 0.0
	max100 := 100
	tests := []struct {
		name      string
		pricing   *ChannelModelPricing
		wantError bool
	}{
		{
			name: "explicit zero flat price is allowed",
			pricing: &ChannelModelPricing{
				Platform:        PlatformOpenAI,
				BillingMode:     BillingModeToken,
				InputPrice:      &zero,
				OutputPrice:     &zero,
				CacheWritePrice: &zero,
				CacheReadPrice:  &zero,
			},
		},
		{
			name: "partial flat price is rejected",
			pricing: &ChannelModelPricing{
				Platform:    PlatformOpenAI,
				BillingMode: BillingModeToken,
				InputPrice:  &zero,
				OutputPrice: &zero,
			},
			wantError: true,
		},
		{
			name:      "empty price entry is rejected",
			pricing:   &ChannelModelPricing{Platform: PlatformOpenAI, BillingMode: BillingModeToken},
			wantError: true,
		},
		{
			name: "interval gap without a global fallback is rejected",
			pricing: &ChannelModelPricing{
				Platform:    PlatformOpenAI,
				BillingMode: BillingModeToken,
				Intervals: []PricingInterval{{
					MinTokens:  0,
					MaxTokens:  &max100,
					InputPrice: &zero,
				}},
			},
			wantError: true,
		},
		{
			name: "complete zero-price interval is allowed",
			pricing: &ChannelModelPricing{
				Platform:    PlatformOpenAI,
				BillingMode: BillingModeToken,
				Intervals: []PricingInterval{{
					MinTokens:       0,
					MaxTokens:       nil,
					InputPrice:      &zero,
					OutputPrice:     &zero,
					CacheWritePrice: &zero,
					CacheReadPrice:  &zero,
				}},
			},
		},
		{
			name: "per-request-only token interval is rejected",
			pricing: &ChannelModelPricing{
				Platform:    PlatformOpenAI,
				BillingMode: BillingModeToken,
				Intervals: []PricingInterval{{
					MinTokens:       0,
					MaxTokens:       nil,
					PerRequestPrice: &zero,
				}},
			},
			wantError: true,
		},
		{
			name: "per-request tier without default is rejected",
			pricing: &ChannelModelPricing{
				Platform:    PlatformOpenAI,
				BillingMode: BillingModePerRequest,
				Intervals: []PricingInterval{{
					TierLabel:       "standard",
					PerRequestPrice: &zero,
				}},
			},
			wantError: true,
		},
		{
			name: "explicit zero per-request default is allowed",
			pricing: &ChannelModelPricing{
				Platform:        PlatformOpenAI,
				BillingMode:     BillingModePerRequest,
				PerRequestPrice: &zero,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := "gpt-channel-only-" + tt.name
			channelService, cache := newOpenAIPricingGuardChannel(groupID, BillingModelSourceChannelMapped)
			cache.pricingByGroupModel[channelModelKey{groupID: groupID, platform: PlatformOpenAI, model: model}] = tt.pricing
			svc := newOpenAIPricingGuardService(nil)
			svc.channelService = channelService

			err := svc.ValidateSelectedOpenAIModelPricing(
				context.Background(),
				&groupID,
				&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				model,
				false,
			)
			if tt.wantError {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestOpenAIGatewayServiceValidateSelectedOpenAIModelPricing_RejectsInvalidChannelOverrideDespiteGlobalPrice(t *testing.T) {
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
			groupID := int64(180 + i)
			channelService, cache := newOpenAIPricingGuardChannel(groupID, BillingModelSourceUpstream)
			price := tt.price
			cache.pricingByGroupModel[channelModelKey{
				groupID:  groupID,
				platform: PlatformOpenAI,
				model:    "gpt-5.4",
			}] = &ChannelModelPricing{
				Platform:    PlatformOpenAI,
				BillingMode: BillingModeToken,
				OutputPrice: &price,
			}

			svc := newOpenAIPricingGuardService(nil)
			svc.channelService = channelService

			require.ErrorIs(t, svc.ValidateSelectedOpenAIModelPricing(
				context.Background(),
				&groupID,
				&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				"gpt-5.4",
				false,
			), ErrModelPricingUnavailable)
		})
	}
}

type openAIPricingGuardSchedulerStub struct {
	selection *AccountSelectionResult
	decision  OpenAIAccountScheduleDecision
}

func (s *openAIPricingGuardSchedulerStub) Select(context.Context, OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	return s.selection, s.decision, nil
}

func (s *openAIPricingGuardSchedulerStub) ReportResult(int64, bool, *int) {}
func (s *openAIPricingGuardSchedulerStub) ReportSwitch()                  {}
func (s *openAIPricingGuardSchedulerStub) SnapshotMetrics() OpenAIAccountSchedulerMetricsSnapshot {
	return OpenAIAccountSchedulerMetricsSnapshot{}
}

func TestOpenAISelectAccountWithSchedulerRejectsUnpricedModelAndReleasesSlot(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled:   true,
		expiresAt: time.Now().Add(time.Hour).UnixNano(),
	})

	releaseCalls := 0
	svc := newOpenAIPricingGuardService(nil)
	svc.openaiScheduler = &openAIPricingGuardSchedulerStub{
		selection: &AccountSelectionResult{
			Account:     &Account{ID: 91, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
			Acquired:    true,
			ReleaseFunc: func() { releaseCalls++ },
		},
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(),
		nil,
		"",
		"",
		"gpt-future-unpriced-v99",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.Nil(t, selection)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, 1, releaseCalls)
}

func TestOpenAISelectAccountWithSchedulerRejectsPricedAliasMappedToUnpricedUpstreamAndReleasesSlot(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled:   true,
		expiresAt: time.Now().Add(time.Hour).UnixNano(),
	})

	releaseCalls := 0
	svc := newOpenAIPricingGuardService(nil)
	svc.openaiScheduler = &openAIPricingGuardSchedulerStub{
		selection: &AccountSelectionResult{
			Account: &Account{
				ID:       94,
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-5.4": "gpt-future-unpriced-v99"},
				},
			},
			Acquired:    true,
			ReleaseFunc: func() { releaseCalls++ },
		},
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(),
		nil,
		"",
		"",
		"gpt-5.4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.Nil(t, selection)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, 1, releaseCalls)
}

func TestOpenAIProductionPricingGuardFailsClosedWhenBillingDependencyMissing(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled:   true,
		expiresAt: time.Now().Add(time.Hour).UnixNano(),
	})

	releaseCalls := 0
	svc := &OpenAIGatewayService{
		cfg:                  &config.Config{RunMode: config.RunModeStandard},
		pricingGuardRequired: true,
		openaiScheduler: &openAIPricingGuardSchedulerStub{selection: &AccountSelectionResult{
			Account:     &Account{ID: 93, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
			Acquired:    true,
			ReleaseFunc: func() { releaseCalls++ },
		}},
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(),
		nil,
		"",
		"",
		"gpt-5.4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.Nil(t, selection)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, 1, releaseCalls)
}

// 图片路由不该套用 token 守卫：出图模型普遍没有 token 价，套上去等于把整条
// /v1/images/* 拒死。它仍须通过默认 enforce 的媒体守卫；这里给模型配置真实图片价，
// 证明缺少 token 价不会误拒。
func TestOpenAISelectAccountWithSchedulerImageKindDoesNotApplyTokenGuard(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled:   true,
		expiresAt: time.Now().Add(time.Hour).UnixNano(),
	})

	releaseCalls := 0
	expected := &AccountSelectionResult{
		Account:     &Account{ID: 92, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		Acquired:    true,
		ReleaseFunc: func() { releaseCalls++ },
	}
	svc := newOpenAIPricingGuardService(nil)
	svc.billingService = NewBillingService(svc.cfg, &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"gpt-image-future-v99": {
				OutputCostPerImage: 0.04,
			},
		},
	})
	svc.openaiScheduler = &openAIPricingGuardSchedulerStub{selection: expected}

	selection, _, err := svc.selectAccountWithScheduler(
		context.Background(),
		nil,
		"",
		"",
		"gpt-image-future-v99",
		nil,
		OpenAIUpstreamTransportHTTPSSE,
		"",
		OpenAIImagesCapabilityNative,
		false,
		PlatformOpenAI,
		false,
		BillingKindImage,
	)

	require.NoError(t, err)
	require.Same(t, expected, selection)
	require.Equal(t, 0, releaseCalls)
}

// count_tokens / Live 这类没有上游成本或没有模型名的路由不进任何守卫。
func TestOpenAISelectAccountWithSchedulerNoneKindSkipsGuard(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled:   true,
		expiresAt: time.Now().Add(time.Hour).UnixNano(),
	})

	expected := &AccountSelectionResult{
		Account:  &Account{ID: 95, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		Acquired: true,
		ReleaseFunc: func() {
			t.Fatal("none 口径不该触发守卫，更不该释放槽位")
		},
	}
	// enforce 档也一样：没有上游成本就没有价可查，不存在"未定价"这个状态。
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Pricing.GuardMode = config.PricingGuardModeEnforce
	svc := newOpenAIPricingGuardService(cfg)
	svc.openaiScheduler = &openAIPricingGuardSchedulerStub{selection: expected}

	selection, _, err := svc.selectAccountWithScheduler(
		context.Background(),
		nil,
		"",
		"",
		" \t",
		nil,
		OpenAIUpstreamTransportAny,
		"",
		"",
		false,
		PlatformOpenAI,
		false,
		BillingKindNone,
	)

	require.NoError(t, err)
	require.Same(t, expected, selection)
}

// 默认 enforce 的媒体守卫：没有任何真实价格来源时拒绝转发并归还槽位。
func TestOpenAISelectAccountWithSchedulerDefaultEnforcesMediaGuardAndReleasesSlot(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled:   true,
		expiresAt: time.Now().Add(time.Hour).UnixNano(),
	})

	releaseCalls := 0
	svc := newOpenAIPricingGuardService(nil)
	svc.openaiScheduler = &openAIPricingGuardSchedulerStub{selection: &AccountSelectionResult{
		Account:     &Account{ID: 96, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		Acquired:    true,
		ReleaseFunc: func() { releaseCalls++ },
	}}

	selection, _, err := svc.selectAccountWithScheduler(
		context.Background(),
		nil,
		"",
		"",
		"gpt-image-future-v99",
		nil,
		OpenAIUpstreamTransportHTTPSSE,
		"",
		OpenAIImagesCapabilityNative,
		false,
		PlatformOpenAI,
		false,
		BillingKindImage,
	)

	require.Nil(t, selection)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, 1, releaseCalls)
}

// 调度阶段还不知道最终输出档位，因此只有所有图片档位都有价格时才能证明请求
// 一定可结算。
func TestOpenAISelectAccountWithSchedulerMediaGuardAcceptsGroupConfiguredPrice(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled:   true,
		expiresAt: time.Now().Add(time.Hour).UnixNano(),
	})

	groupID := int64(77)
	expected := &AccountSelectionResult{
		Account:  &Account{ID: 97, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		Acquired: true,
		ReleaseFunc: func() {
			t.Fatal("分组已配置图片价，不该被守卫拦截")
		},
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Pricing.GuardMode = config.PricingGuardModeEnforce
	svc := newOpenAIPricingGuardService(cfg)
	svc.openaiScheduler = &openAIPricingGuardSchedulerStub{selection: expected}

	price1K := 0.2
	price2K := 0.3
	price4K := 0.4
	ctx := context.WithValue(context.Background(), ctxkey.Group, &Group{
		ID:           groupID,
		Platform:     PlatformOpenAI,
		Status:       StatusActive,
		Hydrated:     true,
		ImagePrice1K: &price1K,
		ImagePrice2K: &price2K,
		ImagePrice4K: &price4K,
	})

	selection, _, err := svc.selectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-image-future-v99",
		nil,
		OpenAIUpstreamTransportHTTPSSE,
		"",
		OpenAIImagesCapabilityNative,
		false,
		PlatformOpenAI,
		false,
		BillingKindImage,
	)

	require.NoError(t, err)
	require.Same(t, expected, selection)
}

func TestOpenAISelectAccountWithSchedulerMediaGuardRejectsPartialGroupTiers(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled:   true,
		expiresAt: time.Now().Add(time.Hour).UnixNano(),
	})

	groupID := int64(78)
	releaseCalls := 0
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Pricing.GuardMode = config.PricingGuardModeEnforce
	svc := newOpenAIPricingGuardService(cfg)
	svc.openaiScheduler = &openAIPricingGuardSchedulerStub{selection: &AccountSelectionResult{
		Account:     &Account{ID: 98, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		Acquired:    true,
		ReleaseFunc: func() { releaseCalls++ },
	}}

	price1K := 0.2
	ctx := context.WithValue(context.Background(), ctxkey.Group, &Group{
		ID:           groupID,
		Platform:     PlatformOpenAI,
		Status:       StatusActive,
		Hydrated:     true,
		ImagePrice1K: &price1K,
	})

	selection, _, err := svc.selectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-image-future-v99",
		nil,
		OpenAIUpstreamTransportHTTPSSE,
		"",
		OpenAIImagesCapabilityNative,
		false,
		PlatformOpenAI,
		false,
		BillingKindImage,
	)

	require.Nil(t, selection)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, 1, releaseCalls)
}

func TestOpenAIMediaPricingGuardUsesExactSettlementModel(t *testing.T) {
	groupID := int64(79)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Pricing.GuardMode = config.PricingGuardModeEnforce
	svc := newOpenAIPricingGuardService(cfg)
	channelService, _ := newOpenAIPricingGuardChannel(groupID, BillingModelSourceRequested)
	svc.channelService = channelService
	pricingService := NewPricingService(cfg, nil)
	pricingService.pricingData["priced-image-model"] = &ModelPriceEntry{
		TokenPricingAbsent: true,
		OutputCostPerImage: 0.25,
	}
	svc.billingService.pricingService = pricingService

	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"public-image-alias": "priced-image-model"},
		},
	}
	err := svc.ValidateSelectedOpenAIMediaPricing(
		context.Background(),
		&groupID,
		account,
		"public-image-alias",
		BillingKindImage,
	)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestOpenAIMediaPricingGuardDoesNotUseTokenChannelPriceAsMediaEvidence(t *testing.T) {
	groupID := int64(84)
	model := "channel-token-only-media-unpriced-v99"
	price := 1e-6
	channelService, cache := newOpenAIPricingGuardChannel(groupID, BillingModelSourceChannelMapped)
	cache.pricingByGroupModel[channelModelKey{
		groupID:  groupID,
		platform: PlatformOpenAI,
		model:    model,
	}] = &ChannelModelPricing{
		Platform:        PlatformOpenAI,
		BillingMode:     BillingModeToken,
		InputPrice:      &price,
		OutputPrice:     &price,
		CacheWritePrice: &price,
		CacheReadPrice:  &price,
	}
	svc := newOpenAIPricingGuardService(nil)
	svc.channelService = channelService
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	for _, kind := range []BillingKind{BillingKindImage, BillingKindVideo} {
		err := svc.ValidateSelectedOpenAIMediaPricing(
			context.Background(),
			&groupID,
			account,
			model,
			kind,
		)
		require.ErrorIs(t, err, ErrModelPricingUnavailable, kind.String())
	}
}

func TestValidateSelectedOpenAIResponsesImagePricingUsesToolModel(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Pricing.GuardMode = config.PricingGuardModeEnforce
	svc := newOpenAIPricingGuardService(cfg)
	pricingService := NewPricingService(cfg, nil)
	pricingService.pricingData["priced-image-model"] = &ModelPriceEntry{
		TokenPricingAbsent: true,
		OutputCostPerImage: 0.25,
	}
	svc.billingService.pricingService = pricingService
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	unknownBody := []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"mystery-image-v99","size":"4K"}]}`)
	err := svc.ValidateSelectedOpenAIResponsesImagePricing(
		context.Background(), nil, account, "gpt-5.4", unknownBody)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	pricedBody := []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"priced-image-model","size":"4K"}]}`)
	require.NoError(t, svc.ValidateSelectedOpenAIResponsesImagePricing(
		context.Background(), nil, account, "gpt-5.4", pricedBody))
}

func TestValidateSelectedOpenAIResponsesImagePricingUsesResponsesLiteToolModel(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Pricing.GuardMode = config.PricingGuardModeEnforce
	svc := newOpenAIPricingGuardService(cfg)
	pricingService := NewPricingService(cfg, nil)
	pricingService.pricingData["priced-image-model"] = &ModelPriceEntry{
		TokenPricingAbsent: true,
		OutputCostPerImage: 0.25,
	}
	svc.billingService.pricingService = pricingService
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	unknownBody := []byte(`{
		"model":"gpt-5.4",
		"input":[{
			"type":"additional_tools",
			"tools":[{"type":"image_generation","model":"unknown-image-model","size":"4K"}]
		}]
	}`)
	err := svc.ValidateSelectedOpenAIResponsesImagePricing(
		context.Background(), nil, account, "gpt-5.4", unknownBody)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	pricedBody := []byte(`{
		"model":"gpt-5.4",
		"input":[{
			"type":"additional_tools",
			"tools":[{"type":"image_generation","model":"priced-image-model","size":"4K"}]
		}]
	}`)
	require.NoError(t, svc.ValidateSelectedOpenAIResponsesImagePricing(
		context.Background(), nil, account, "gpt-5.4", pricedBody))
}

func TestValidateSelectedOpenAIResponsesImagePricingMatchesExactTierSettlement(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Pricing.GuardMode = config.PricingGuardModeEnforce
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	const imageModel = "exact-tier-image-model"
	bodyForSize := func(size string) []byte {
		return []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"` +
			imageModel + `","size":"` + size + `"}]}`)
	}

	t.Run("group tier", func(t *testing.T) {
		groupID := int64(85)
		price4K := 0.40
		group := &Group{
			ID:           groupID,
			Platform:     PlatformOpenAI,
			Status:       StatusActive,
			Hydrated:     true,
			ImagePrice4K: &price4K,
		}
		ctx := context.WithValue(context.Background(), ctxkey.Group, group)
		svc := newOpenAIPricingGuardService(cfg)

		require.NoError(t, svc.ValidateSelectedOpenAIResponsesImagePricing(
			ctx, &groupID, account, "gpt-5.4", bodyForSize(ImageBillingSize4K)))
		require.ErrorIs(t, svc.ValidateSelectedOpenAIResponsesImagePricing(
			ctx, &groupID, account, "gpt-5.4", bodyForSize(ImageBillingSize2K)),
			ErrModelPricingUnavailable,
		)

		cost, err := svc.calculateOpenAIImageCost(
			ctx,
			imageModel,
			&APIKey{GroupID: &groupID, Group: group},
			&OpenAIForwardResult{ImageCount: 2, ImageSize: ImageBillingSize4K},
			1,
		)
		require.NoError(t, err)
		require.InDelta(t, 0.80, cost.TotalCost, 1e-12)
		require.InDelta(t, 0.80, cost.ActualCost, 1e-12)
	})

	t.Run("channel tier", func(t *testing.T) {
		groupID := int64(86)
		price4K := 0.35
		channelService, cache := newOpenAIPricingGuardChannel(groupID, BillingModelSourceChannelMapped)
		cache.pricingByGroupModel[channelModelKey{
			groupID:  groupID,
			platform: PlatformOpenAI,
			model:    imageModel,
		}] = &ChannelModelPricing{
			Platform:    PlatformOpenAI,
			BillingMode: BillingModeImage,
			Intervals: []PricingInterval{{
				TierLabel:       ImageBillingSize4K,
				PerRequestPrice: &price4K,
			}},
		}
		svc := newOpenAIPricingGuardService(cfg)
		svc.channelService = channelService
		svc.resolver = NewModelPricingResolver(channelService, svc.billingService)
		ctx := context.Background()

		require.NoError(t, svc.ValidateSelectedOpenAIResponsesImagePricing(
			ctx, &groupID, account, "gpt-5.4", bodyForSize(ImageBillingSize4K)))
		require.ErrorIs(t, svc.ValidateSelectedOpenAIResponsesImagePricing(
			ctx, &groupID, account, "gpt-5.4", bodyForSize(ImageBillingSize2K)),
			ErrModelPricingUnavailable,
		)

		cost, err := svc.calculateOpenAIImageCost(
			ctx,
			imageModel,
			&APIKey{GroupID: &groupID, Group: &Group{ID: groupID}},
			&OpenAIForwardResult{ImageCount: 2, ImageSize: ImageBillingSize4K},
			1,
		)
		require.NoError(t, err)
		require.InDelta(t, 0.70, cost.TotalCost, 1e-12)
		require.InDelta(t, 0.70, cost.ActualCost, 1e-12)
	})
}

func TestValidateSelectedOpenAIResponsesImagePricingRejectsAmbiguousToolModels(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Pricing.GuardMode = config.PricingGuardModeEnforce
	svc := newOpenAIPricingGuardService(cfg)
	pricingService := NewPricingService(cfg, nil)
	pricingService.pricingData["priced-image-model"] = &ModelPriceEntry{
		TokenPricingAbsent: true,
		OutputCostPerImage: 0.25,
	}
	svc.billingService.pricingService = pricingService
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	for _, body := range [][]byte{
		[]byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"priced-image-model"},{"type":"image_generation","model":"unknown-image-model"}]}`),
		[]byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"unknown-image-model"},{"type":"image_generation","model":"priced-image-model"}]}`),
	} {
		err := svc.ValidateSelectedOpenAIResponsesImagePricing(
			context.Background(), nil, account, "gpt-5.4", body)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.Contains(t, err.Error(), "multiple image_generation tools")
	}
}

func TestValidateSelectedOpenAIResponsesImagePricingRejectsDuplicateToolIdentityBeforeIntentDetection(t *testing.T) {
	svc := newOpenAIPricingGuardService(nil)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	for _, body := range [][]byte{
		[]byte(`{"model":"gpt-5.4","tools":[{"type":"function","type":"image_generation","model":"unknown-image-model"}]}`),
		[]byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"gpt-image-2","model":"unknown-image-model"}]}`),
		[]byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"gpt-image-2","size":"1K","size":"4K"}]}`),
	} {
		err := svc.ValidateSelectedOpenAIResponsesImagePricing(
			context.Background(), nil, account, "gpt-5.4", body)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.Contains(t, err.Error(), "duplicate")
	}
}

// 视频路由：Grok Imagine 的按 SKU 默认价是真实来源，不是 $0.134 那个通用占位值。
func TestOpenAIMediaPricingGuardAcceptsGrokImagineVideoDefaults(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Pricing.GuardMode = config.PricingGuardModeEnforce
	svc := newOpenAIPricingGuardService(cfg)
	account := &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey}

	require.NoError(t, svc.ValidateSelectedOpenAIMediaPricing(
		context.Background(), nil, account, "grok-imagine-video-1.5", BillingKindVideo))

	// Hard-coded prices are an exact SKU allowlist. A future suffix must be
	// explicitly priced before it can incur upstream cost.
	err := svc.ValidateSelectedOpenAIMediaPricing(
		context.Background(), nil, account, "grok-imagine-video-future", BillingKindVideo)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	// 同一台服务上未知的出图模型没有任何真实价格来源，结算时只会落到占位价。
	err = svc.ValidateSelectedOpenAIMediaPricing(
		context.Background(), nil, account, "totally-unknown-video-v99", BillingKindVideo)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	pricingService := NewPricingService(cfg, nil)
	pricingService.pricingData["image-only-catalog-model"] = &ModelPriceEntry{
		TokenPricingAbsent: true,
		OutputCostPerImage: 0.25,
	}
	svc.billingService.pricingService = pricingService
	err = svc.ValidateSelectedOpenAIMediaPricing(
		context.Background(), nil, account, "image-only-catalog-model", BillingKindVideo)
	require.ErrorIs(t, err, ErrModelPricingUnavailable,
		"output_cost_per_image is not evidence of a video-per-second price")
}

func TestOpenAIMediaPricingGuard_GrokImageUsesOnlyExactHardcodedTiers(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Pricing.GuardMode = config.PricingGuardModeEnforce
	svc := newOpenAIPricingGuardService(cfg)
	account := &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey}

	require.True(t, svc.catalogHasMediaPricing("grok-imagine-image-quality", BillingKindImage),
		"scheduler model-level probe should keep a model with real 1K/2K tiers eligible")
	for _, tier := range []string{ImageBillingSize1K, ImageBillingSize2K} {
		require.NoError(t, svc.enforceResolvedOpenAIMediaPricing(
			context.Background(),
			nil,
			account,
			"grok-imagine-image-quality",
			"grok-imagine-image-quality",
			tier,
			BillingKindImage,
		), tier)
	}

	err := svc.enforceResolvedOpenAIMediaPricing(
		context.Background(),
		nil,
		account,
		"grok-imagine-image-quality",
		"grok-imagine-image-quality",
		ImageBillingSize4K,
		BillingKindImage,
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

// $0.134 是 getDefaultImagePrice 查不到任何价格时的兜底占位值。
// 若守卫把它当成"有价"，这层检查就永远不会拒绝任何模型，等于没有。
func TestOpenAIMediaPricingGuardRejectsPlaceholderImagePrice(t *testing.T) {
	svc := newOpenAIPricingGuardService(nil)
	require.False(t, svc.catalogHasMediaPricing("totally-unknown-image-v99", BillingKindImage))
	// 兜底占位值确实存在且非零——正是它让"未定价"在媒体路由上伪装成了"已正常收费"。
	require.Greater(t, svc.billingService.getDefaultImagePrice("totally-unknown-image-v99", ImageBillingSize1K), 0.0)
}

func TestNormalizePricingGuardMode(t *testing.T) {
	require.Equal(t, config.PricingGuardModeEnforce, config.NormalizePricingGuardMode(" OFF "))
	require.Equal(t, config.PricingGuardModeEnforce, config.NormalizePricingGuardMode("shadow"))
	require.Equal(t, config.PricingGuardModeEnforce, config.NormalizePricingGuardMode("enforce"))
	// 拼错或缺失的准入开关必须保持 fail-closed。
	require.Equal(t, config.PricingGuardModeEnforce, config.NormalizePricingGuardMode("enfroce"))
	require.Equal(t, config.PricingGuardModeEnforce, config.NormalizePricingGuardMode(""))
}
