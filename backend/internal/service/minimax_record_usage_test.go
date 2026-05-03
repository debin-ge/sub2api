package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newMiniMaxGatewayRecordUsageServiceForTest(usageRepo UsageLogRepository, userRepo UserRepository, subRepo UserSubscriptionRepository) *GatewayService {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	return NewGatewayService(
		nil,
		nil,
		usageRepo,
		nil,
		userRepo,
		subRepo,
		nil,
		nil,
		cfg,
		nil,
		nil,
		NewBillingService(cfg, nil),
		nil,
		&BillingCacheService{},
		nil,
		nil,
		&DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

func TestMiniMaxRecordUsageMissingPricingReturnsBillingError(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newMiniMaxGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)
	groupID := int64(42)

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "minimax-missing-pricing",
			Model:         "claude-sonnet-4-5",
			UpstreamModel: "MiniMax-M2.7",
			Usage: ClaudeUsage{
				InputTokens:  11,
				OutputTokens: 7,
			},
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:      7,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				Platform:       PlatformMiniMax,
				RateMultiplier: 1,
			},
		},
		User: &User{ID: 99},
		Account: &Account{
			ID:       101,
			Platform: PlatformMiniMax,
			Type:     AccountTypeAPIKey,
		},
	})

	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "pricing")
	require.Nil(t, usageRepo.lastLog)
	require.Equal(t, 0, userRepo.deductCalls)
	require.Equal(t, 0, subRepo.incrementCalls)
}

func TestMiniMaxRecordUsageUsesUpstreamModelWhenPricingIsConfigured(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newMiniMaxGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)
	svc.billingService.fallbackPrices["minimax-m2.7"] = &ModelPricing{
		InputPricePerToken:  1e-6,
		OutputPricePerToken: 2e-6,
	}
	groupID := int64(42)

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "minimax-configured-pricing",
			Model:         "claude-sonnet-4-5",
			UpstreamModel: "MiniMax-M2.7",
			Usage: ClaudeUsage{
				InputTokens:  11,
				OutputTokens: 7,
			},
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:      7,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				Platform:       PlatformMiniMax,
				RateMultiplier: 1,
			},
		},
		User: &User{ID: 99},
		Account: &Account{
			ID:       101,
			Platform: PlatformMiniMax,
			Type:     AccountTypeAPIKey,
		},
		InboundEndpoint:  "/v1/messages",
		UpstreamEndpoint: "/v1/messages",
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "MiniMax-M2.7", usageRepo.lastLog.Model)
	require.Equal(t, "claude-sonnet-4-5", usageRepo.lastLog.RequestedModel)
	require.NotNil(t, usageRepo.lastLog.UpstreamModel)
	require.Equal(t, "MiniMax-M2.7", *usageRepo.lastLog.UpstreamModel)
	require.InDelta(t, 25e-6, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, 25e-6, userRepo.lastAmount, 1e-12)
}
