//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayServiceRecordUsage_UnknownOutputImageSizeStaysUnsettled(t *testing.T) {
	for _, outputSize := range []string{"8K", "8192x8192"} {
		t.Run(outputSize, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			subRepo := &openAIRecordUsageSubRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, subRepo, nil)

			pricingService := NewPricingService(svc.cfg, nil)
			pricingService.pricingData["priced-image-model"] = &ModelPriceEntry{
				TokenPricingAbsent: true,
				OutputCostPerImage: 0.25,
			}
			svc.billingService.pricingService = pricingService

			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID:       "openai_unknown_output_size_" + outputSize,
					Model:           "text-model",
					BillingModel:    "priced-image-model",
					UpstreamModel:   "text-model",
					ImageCount:      1,
					ImageInputSize:  ImageBillingSize2K,
					ImageOutputSize: outputSize,
					Duration:        time.Second,
				},
				APIKey:      &APIKey{ID: 10},
				User:        &User{ID: 20},
				Account:     &Account{ID: 30},
				BillingKind: BillingKindImage,
			})

			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			require.Equal(t, BillingStatePricingUnavailable, usageRepo.lastLog.BillingState)
			require.NotNil(t, usageRepo.lastLog.ImageSize)
			require.Equal(t, outputSize, *usageRepo.lastLog.ImageSize)
			require.NotNil(t, usageRepo.lastLog.ImageOutputSize)
			require.Equal(t, outputSize, *usageRepo.lastLog.ImageOutputSize)
			require.Zero(t, usageRepo.lastLog.TotalCost)
			require.Zero(t, usageRepo.lastLog.ActualCost)
			require.Zero(t, userRepo.deductCalls)
			require.Zero(t, subRepo.incrementCalls)
		})
	}
}

func TestGatewayServiceRecordUsage_UnknownOutputImageSizeStaysUnsettled(t *testing.T) {
	for _, outputSize := range []string{"8K", "8192x8192"} {
		t.Run(outputSize, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			subRepo := &openAIRecordUsageSubRepoStub{}
			svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)

			pricingService := NewPricingService(svc.cfg, nil)
			pricingService.pricingData["priced-image-model"] = &ModelPriceEntry{
				TokenPricingAbsent: true,
				OutputCostPerImage: 0.25,
			}
			svc.billingService.pricingService = pricingService

			err := svc.RecordUsage(context.Background(), &RecordUsageInput{
				Result: &ForwardResult{
					RequestID:       "gateway_unknown_output_size_" + outputSize,
					Model:           "text-model",
					BillingModel:    "priced-image-model",
					UpstreamModel:   "text-model",
					ImageCount:      1,
					ImageInputSize:  ImageBillingSize2K,
					ImageOutputSize: outputSize,
					Duration:        time.Second,
				},
				APIKey:  &APIKey{ID: 10},
				User:    &User{ID: 20},
				Account: &Account{ID: 30},
			})

			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			require.Equal(t, BillingStatePricingUnavailable, usageRepo.lastLog.BillingState)
			require.NotNil(t, usageRepo.lastLog.ImageSize)
			require.Equal(t, outputSize, *usageRepo.lastLog.ImageSize)
			require.NotNil(t, usageRepo.lastLog.ImageOutputSize)
			require.Equal(t, outputSize, *usageRepo.lastLog.ImageOutputSize)
			require.Zero(t, usageRepo.lastLog.TotalCost)
			require.Zero(t, usageRepo.lastLog.ActualCost)
			require.Zero(t, userRepo.deductCalls)
			require.Zero(t, subRepo.incrementCalls)
		})
	}
}

func TestGatewayServiceHasResolvableImagePricing_UnknownExplicitTierCannotBorrow2K(t *testing.T) {
	price2K := 0.22
	svc := newGatewayRecordUsageServiceForTest(
		&openAIRecordUsageLogRepoStub{},
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
	)
	apiKey := &APIKey{
		GroupID: i64p(919),
		Group: &Group{
			ID:           919,
			ImagePrice2K: &price2K,
		},
	}

	require.True(t, svc.hasResolvableImagePricing(
		context.Background(),
		"priced-image-model",
		ImageBillingSize2K,
		apiKey,
	))
	for _, size := range []string{"8K", "8192x8192", "largest"} {
		require.False(t, svc.hasResolvableImagePricing(
			context.Background(),
			"priced-image-model",
			size,
			apiKey,
		), size)
	}
}

func TestBillingRecoveryRunOnce_UnknownImageSizeDoesNotBorrowKnownTierOrDefaultPrice(t *testing.T) {
	const (
		groupID  = int64(920)
		apiKeyID = int64(921)
		model    = "priced-image-model"
	)
	price4K := 0.44
	defaultPrice := 0.22
	keyRepo := &recoveryAPIKeyRepoStub{keys: map[int64]*APIKey{
		apiKeyID: {
			ID:      apiKeyID,
			GroupID: i64p(groupID),
			Group: &Group{
				ID:           groupID,
				ImagePrice4K: &price4K,
			},
		},
	}}
	channelService := recoveryChannelService(groupID, model, &ChannelModelPricing{
		BillingMode:     BillingModeImage,
		PerRequestPrice: &defaultPrice,
		Intervals: []PricingInterval{{
			TierLabel:       ImageBillingSize4K,
			PerRequestPrice: &price4K,
		}},
	})

	for _, outputSize := range []string{"8K", "8192x8192"} {
		t.Run(outputSize, func(t *testing.T) {
			log := pendingImageLog(930, model, outputSize)
			log.APIKeyID = apiKeyID
			log.GroupID = i64p(groupID)
			repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{log}}}
			svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, keyRepo)
			pricingService := NewPricingService(svc.cfg, nil)
			pricingService.pricingData[model] = &ModelPriceEntry{
				TokenPricingAbsent: true,
				OutputCostPerImage: 0.25,
			}
			svc.billingService.pricingService = pricingService
			svc.channelService = channelService
			svc.resolver = NewModelPricingResolver(channelService, svc.billingService)

			report, err := svc.RunOnce(context.Background())

			require.NoError(t, err)
			require.Equal(t, 1, report.Scanned)
			require.Equal(t, 1, report.StillUnpriced)
			require.Zero(t, report.Recovered)
			require.Zero(t, report.Failed)
			require.Empty(t, repo.marked)
		})
	}
}
