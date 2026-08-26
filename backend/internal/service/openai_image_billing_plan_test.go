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

func newOpenAIImageBillingPlanTestService(t *testing.T) *OpenAIGatewayService {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	pricingService := &PricingService{}
	pricingData, err := pricingService.parsePricingData(data)
	require.NoError(t, err)
	pricingService.pricingData = pricingData
	billingService := NewBillingService(&config.Config{}, pricingService)
	return &OpenAIGatewayService{
		billingService: billingService,
		resolver:       NewModelPricingResolver(nil, billingService),
	}
}

func setOpenAIImageChannelPricingForTest(
	t *testing.T,
	svc *OpenAIGatewayService,
	groupID int64,
	model string,
	pricing *ChannelModelPricing,
) {
	t.Helper()
	cache := newEmptyChannelCache()
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: model}] = pricing
	cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
	cache.groupPlatform[groupID] = ""
	cache.loadedAt = time.Now()
	channelService := &ChannelService{}
	channelService.cache.Store(cache)
	svc.channelService = channelService
	svc.resolver = NewModelPricingResolver(channelService, svc.billingService)
}

func TestResolveOpenAIImageBillingPlanUsesRealGPTImageTokenCatalog(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)

	for _, model := range []string{
		"gpt-image-1",
		"gpt-image-1-mini",
		"gpt-image-1.5",
		"gpt-image-2",
	} {
		t.Run(model, func(t *testing.T) {
			plan, err := svc.resolveOpenAIImageBillingPlan(
				context.Background(),
				nil,
				nil,
				model,
				ImageBillingSize1K,
				false,
			)
			require.NoError(t, err)
			require.Equal(t, BillingModeToken, plan.Mode)
			require.Equal(t, model, plan.Model)
			require.Equal(t, PricingSourceModelPrice, plan.Source)
			require.NotNil(t, plan.Resolved)
			require.NotNil(t, plan.Resolved.BasePricing)
			require.True(t, plan.Resolved.BasePricing.ImageOutputPriceExplicit)
			require.Positive(t, plan.Resolved.BasePricing.ImageOutputPricePerToken)
		})
	}
}

func TestResolveOpenAIImageBillingPlanUsesProviderOverrideUnderComposite(t *testing.T) {
	const model = "gpt-image-composite-override"
	groupID := int64(88)
	apiKey := &APIKey{
		GroupID: &groupID,
		Group:   &Group{ID: groupID, Platform: PlatformComposite},
	}
	platforms := []string{PlatformComposite, PlatformOpenAI}

	t.Run("image token pricing", func(t *testing.T) {
		inputPrice := 2e-6
		imageInputPrice := 4e-6
		imageOutputPrice := 20e-6
		pricingService := NewPricingService(&config.Config{}, nil)
		pricingService.SeedCatalogForTest(map[string]*ModelPriceEntry{})
		pricingService.SeedOverridesForTest([]ModelPriceOverride{{
			Platform:  PlatformOpenAI,
			ModelName: model,
			Enabled:   true,
			Payload: ModelPriceOverridePayload{
				InputCostPerToken:       &inputPrice,
				InputCostPerImageToken:  &imageInputPrice,
				OutputCostPerImageToken: &imageOutputPrice,
			},
		}})
		billing := NewBillingService(&config.Config{}, pricingService)
		svc := &OpenAIGatewayService{
			billingService: billing,
			resolver:       NewModelPricingResolver(nil, billing),
		}

		plan, err := svc.resolveOpenAIImageBillingPlanForPlatforms(
			context.Background(), apiKey, &groupID, platforms, model, ImageBillingSize1K, true,
		)
		require.NoError(t, err)
		require.Equal(t, BillingModeToken, plan.Mode)
		require.InDelta(t, imageOutputPrice, plan.Resolved.BasePricing.ImageOutputPricePerToken, 1e-12)
	})

	t.Run("per image pricing", func(t *testing.T) {
		perImagePrice := 0.25
		pricingService := NewPricingService(&config.Config{}, nil)
		pricingService.SeedCatalogForTest(map[string]*ModelPriceEntry{})
		pricingService.SeedOverridesForTest([]ModelPriceOverride{{
			Platform:  PlatformOpenAI,
			ModelName: model,
			Enabled:   true,
			Payload: ModelPriceOverridePayload{
				OutputCostPerImage: &perImagePrice,
			},
		}})
		billing := NewBillingService(&config.Config{}, pricingService)
		svc := &OpenAIGatewayService{billingService: billing}

		plan, err := svc.resolveOpenAIImageBillingPlanForPlatforms(
			context.Background(), apiKey, &groupID, platforms, model, ImageBillingSize1K, false,
		)
		require.NoError(t, err)
		require.Equal(t, BillingModeImage, plan.Mode)
		require.InDelta(t, perImagePrice, plan.Resolved.DefaultPerRequestPrice, 1e-12)
	})
}

func TestResolveOpenAIImageBillingPlanUsesChannelTokenPrices(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)
	groupID := int64(72)
	inputPrice := 7e-6
	imageInputPrice := 9e-6
	imageOutputPrice := 31e-6
	setOpenAIImageChannelPricingForTest(t, svc, groupID, "gpt-image-2", &ChannelModelPricing{
		BillingMode:      BillingModeToken,
		InputPrice:       &inputPrice,
		ImageInputPrice:  &imageInputPrice,
		ImageOutputPrice: &imageOutputPrice,
	})

	plan, err := svc.resolveOpenAIImageBillingPlan(
		context.Background(),
		&APIKey{GroupID: &groupID},
		&groupID,
		"gpt-image-2",
		ImageBillingSize1K,
		true,
	)
	require.NoError(t, err)
	require.Equal(t, BillingModeToken, plan.Mode)
	require.Equal(t, PricingSourceChannel, plan.Source)
	require.NotNil(t, plan.Resolved)
	require.NotNil(t, plan.Resolved.BasePricing)
	require.InDelta(t, inputPrice, plan.Resolved.BasePricing.InputPricePerToken, 1e-12)
	require.InDelta(t, imageInputPrice, plan.Resolved.BasePricing.ImageInputPricePerToken, 1e-12)
	require.InDelta(t, imageOutputPrice, plan.Resolved.BasePricing.ImageOutputPricePerToken, 1e-12)
}

func TestResolveOpenAIImageBillingPlanUsesChannelImageTierPrice(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)
	groupID := int64(73)
	price := 0.17
	setOpenAIImageChannelPricingForTest(t, svc, groupID, "gpt-image-2", &ChannelModelPricing{
		BillingMode: BillingModeImage,
		Intervals: []PricingInterval{{
			TierLabel:       ImageBillingSize1K,
			PerRequestPrice: &price,
		}},
	})

	plan, err := svc.resolveOpenAIImageBillingPlan(
		context.Background(),
		&APIKey{GroupID: &groupID},
		&groupID,
		"gpt-image-2",
		ImageBillingSize1K,
		false,
	)
	require.NoError(t, err)
	require.Equal(t, BillingModeImage, plan.Mode)
	require.Equal(t, PricingSourceChannel, plan.Source)
	require.True(t, plan.Resolved.DefaultPerRequestPriceSet)
	require.InDelta(t, price, plan.Resolved.DefaultPerRequestPrice, 1e-12)
}

func TestResolveOpenAIImageBillingPlanRejectsIncompleteExplicitChannelImagePrice(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)
	groupID := int64(74)
	price2K := 0.27
	setOpenAIImageChannelPricingForTest(t, svc, groupID, "gpt-image-2", &ChannelModelPricing{
		BillingMode: BillingModeImage,
		Intervals: []PricingInterval{{
			TierLabel:       ImageBillingSize2K,
			PerRequestPrice: &price2K,
		}},
	})

	plan, err := svc.resolveOpenAIImageBillingPlan(
		context.Background(),
		&APIKey{GroupID: &groupID},
		&groupID,
		"gpt-image-2",
		ImageBillingSize1K,
		false,
	)
	require.Nil(t, plan)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, "explicit channel image pricing is incomplete")
}

func TestResolveOpenAIImageBillingPlanRejectsInvalidExplicitChannelTokenPrice(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)
	groupID := int64(75)
	inputPrice := 7e-6
	invalidImageOutputPrice := -1.0
	setOpenAIImageChannelPricingForTest(t, svc, groupID, "gpt-image-2", &ChannelModelPricing{
		BillingMode:      BillingModeToken,
		InputPrice:       &inputPrice,
		ImageOutputPrice: &invalidImageOutputPrice,
	})

	plan, err := svc.resolveOpenAIImageBillingPlan(
		context.Background(),
		&APIKey{GroupID: &groupID},
		&groupID,
		"gpt-image-2",
		ImageBillingSize1K,
		false,
	)
	require.Nil(t, plan)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, "explicit channel image token pricing")
}

func TestResolveOpenAIImageBillingPlanPrefersGroupUnitPrice(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)
	groupID := int64(71)
	price := 0.42
	apiKey := &APIKey{
		GroupID: &groupID,
		Group: &Group{
			ID:           groupID,
			ImagePrice1K: &price,
		},
	}

	plan, err := svc.resolveOpenAIImageBillingPlan(
		context.Background(),
		apiKey,
		&groupID,
		"gpt-image-2",
		ImageBillingSize1K,
		false,
	)
	require.NoError(t, err)
	require.Equal(t, BillingModeImage, plan.Mode)
	require.Equal(t, PricingSourceGroup, plan.Source)
	require.True(t, plan.Resolved.DefaultPerRequestPriceSet)
	require.InDelta(t, price, plan.Resolved.DefaultPerRequestPrice, 1e-12)
}

func TestCalculateOpenAIImageCostFromTokenPlan(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)
	plan, err := svc.resolveOpenAIImageBillingPlan(
		context.Background(),
		nil,
		nil,
		"gpt-image-2",
		ImageBillingSize1K,
		false,
	)
	require.NoError(t, err)

	result := &OpenAIForwardResult{
		Model:            "gpt-image-2",
		BillingModel:     "gpt-image-2",
		UpstreamModel:    "gpt-image-2",
		ImageBillingPlan: plan,
		ImageCount:       1,
		ImageSize:        ImageBillingSize1K,
	}
	tokens := UsageTokens{
		InputTokens:       46,
		OutputTokens:      2459,
		ImageOutputTokens: 2459,
	}
	cost, err := svc.calculateOpenAIRecordUsageCost(
		context.Background(),
		result,
		&APIKey{},
		[]string{"gpt-image-2"},
		BillingKindImage,
		1,
		1,
		1,
		1,
		tokens,
		"",
		boolPtr(false),
		time.Time{},
	)
	require.NoError(t, err)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
	require.InDelta(t, 46*5e-6, cost.InputCost, 1e-12)
	require.InDelta(t, 2459*3e-5, cost.ImageOutputCost, 1e-12)
	require.Zero(t, cost.OutputCost)
}

func TestCalculateOpenAIImageCostSupportsCatalogWithoutTextOutputPrice(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)
	plan, err := svc.resolveOpenAIImageBillingPlan(
		context.Background(),
		nil,
		nil,
		"gpt-image-1",
		ImageBillingSize1K,
		false,
	)
	require.NoError(t, err)
	require.False(t, plan.Resolved.BasePricing.OutputPriceExplicit)
	require.Zero(t, plan.Resolved.BasePricing.OutputPricePerToken)

	result := &OpenAIForwardResult{
		Model:            "gpt-image-1",
		BillingModel:     "gpt-image-1",
		UpstreamModel:    "gpt-image-1",
		ImageBillingPlan: plan,
		ImageCount:       1,
		ImageSize:        ImageBillingSize1K,
	}
	tokens := UsageTokens{
		InputTokens:       46,
		OutputTokens:      2459,
		ImageOutputTokens: 2459,
	}
	cost, err := svc.calculateOpenAIRecordUsageCost(
		context.Background(),
		result,
		&APIKey{},
		[]string{"gpt-image-1"},
		BillingKindImage,
		1,
		1,
		1,
		1,
		tokens,
		"",
		boolPtr(false),
		time.Time{},
	)
	require.NoError(t, err)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
	require.InDelta(t, 46*5e-6, cost.InputCost, 1e-12)
	require.InDelta(t, 2459*4e-5, cost.ImageOutputCost, 1e-12)
	require.Zero(t, cost.OutputCost)
}

func TestCalculateOpenAIImageCostFromTokenPlanRejectsMissingUsage(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)
	plan, err := svc.resolveOpenAIImageBillingPlan(
		context.Background(),
		nil,
		nil,
		"gpt-image-2",
		ImageBillingSize1K,
		false,
	)
	require.NoError(t, err)

	result := &OpenAIForwardResult{
		ImageBillingPlan: plan,
		ImageCount:       1,
	}
	_, err = svc.calculateOpenAIRecordUsageCost(
		context.Background(),
		result,
		&APIKey{},
		[]string{"gpt-image-2"},
		BillingKindImage,
		1,
		1,
		1,
		1,
		UsageTokens{},
		"",
		boolPtr(false),
		time.Time{},
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, "image output token usage")
}

func TestRecordUsageKeepsMissingImageTokenUsagePending(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	recordSvc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, subRepo, nil)
	pricingSvc := newOpenAIImageBillingPlanTestService(t)
	recordSvc.billingService = pricingSvc.billingService
	recordSvc.resolver = pricingSvc.resolver

	plan, err := recordSvc.resolveOpenAIImageBillingPlan(
		context.Background(),
		nil,
		nil,
		"gpt-image-2",
		ImageBillingSize1K,
		false,
	)
	require.NoError(t, err)

	err = recordSvc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:        "req_image_usage_missing",
			Model:            "gpt-image-2",
			BillingModel:     "gpt-image-2",
			UpstreamModel:    "gpt-image-2",
			ImageBillingPlan: plan,
			ImageCount:       1,
			ImageSize:        ImageBillingSize1K,
		},
		APIKey:      &APIKey{ID: 10},
		User:        &User{ID: 20},
		Account:     &Account{ID: 30},
		BillingKind: BillingKindImage,
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      "gpt-image-2",
			ChannelMappedModel: "gpt-image-2",
			BillingModelSource: BillingModelSourceUpstream,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, BillingStatePricingUnavailable, usageRepo.lastLog.BillingState)
	require.NotNil(t, usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModeToken), *usageRepo.lastLog.BillingMode)
	require.Zero(t, usageRepo.lastLog.ActualCost)
	require.Equal(t, 0, userRepo.deductCalls)

	recovery := &BillingRecoveryService{
		billingService: pricingSvc.billingService,
		resolver:       pricingSvc.resolver,
	}
	require.False(t, recovery.hasStrictPricingForUsage(
		context.Background(),
		"gpt-image-2",
		usageRepo.lastLog,
		&APIKey{},
		nil,
	))
}

func TestBillingRecoveryRecomputesCompleteImageTokenUsage(t *testing.T) {
	pricingSvc := newOpenAIImageBillingPlanTestService(t)
	recovery := &BillingRecoveryService{
		billingService: pricingSvc.billingService,
		resolver:       pricingSvc.resolver,
	}
	mode := string(BillingModeToken)
	endpoint := "/v1/images/edits"
	log := &UsageLog{
		Model:             "gpt-image-2",
		InboundEndpoint:   &endpoint,
		InputTokens:       371,
		ImageInputTokens:  352,
		OutputTokens:      439,
		ImageOutputTokens: 439,
		ImageCount:        1,
		RateMultiplier:    1,
		BillingMode:       &mode,
	}

	require.True(t, recovery.hasStrictPricingForUsage(
		context.Background(),
		"gpt-image-2",
		log,
		&APIKey{},
		nil,
	))
	cost, err := recovery.recomputeCost(
		context.Background(),
		log,
		"gpt-image-2",
		&APIKey{},
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
	require.InDelta(t, 19*5e-6, cost.InputCost, 1e-12)
	require.InDelta(t, 352*8e-6, cost.ImageInputCost, 1e-12)
	require.InDelta(t, 439*3e-5, cost.ImageOutputCost, 1e-12)
	require.Zero(t, cost.OutputCost)
}

func TestNativeResponsesImageGuardDoesNotBorrowImageTokenPlan(t *testing.T) {
	svc := newOpenAIImageBillingPlanTestService(t)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	require.True(t, svc.hasResolvableOpenAIMediaPricing(
		context.Background(),
		nil,
		openAIPricingGuardModels{Primary: "gpt-image-2"},
		BillingKindImage,
	))
	err := svc.enforceResolvedOpenAIMediaPricing(
		context.Background(),
		nil,
		account,
		"gpt-5.4",
		"gpt-image-2",
		ImageBillingSize1K,
		BillingKindImage,
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}
