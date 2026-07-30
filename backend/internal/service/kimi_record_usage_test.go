package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestKimiRecordUsageUsesFallbackPricing(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newMiniMaxGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)
	groupID := int64(42)

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "kimi-fallback-pricing",
			Model:         "kimi-for-coding",
			UpstreamModel: "kimi-for-coding",
			Usage: ClaudeUsage{
				InputTokens:              1_000_000,
				OutputTokens:             1_000_000,
				CacheCreationInputTokens: 1_000_000,
				CacheReadInputTokens:     1_000_000,
			},
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:      7,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				Platform:       PlatformKimi,
				RateMultiplier: 1,
			},
		},
		User: &User{ID: 99},
		Account: &Account{
			ID:       101,
			Platform: PlatformKimi,
			Type:     AccountTypeAPIKey,
		},
		InboundEndpoint:  "/v1/messages",
		UpstreamEndpoint: "/v1/messages",
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "kimi-for-coding", usageRepo.lastLog.Model)
	require.NotNil(t, usageRepo.lastLog.UpstreamModel)
	require.Equal(t, "kimi-for-coding", *usageRepo.lastLog.UpstreamModel)
	require.InDelta(t, 6.06, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, 6.06, userRepo.lastAmount, 1e-12)
}

func TestKimiK3RecordUsageChargesFallbackPricing(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newMiniMaxGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)
	groupID := int64(42)

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "kimi-k3-fallback-pricing",
			Model:         "K3",
			UpstreamModel: "K3",
			Usage: ClaudeUsage{
				InputTokens:          1_000_000,
				OutputTokens:         1_000_000,
				CacheReadInputTokens: 1_000_000,
			},
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:      7,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				Platform:       PlatformKimi,
				RateMultiplier: 1,
			},
		},
		User: &User{ID: 99},
		Account: &Account{
			ID:       101,
			Platform: PlatformKimi,
			Type:     AccountTypeAPIKey,
		},
	})

	require.NoError(t, err)
	require.Equal(t, 1, usageRepo.calls)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, 18.30, usageRepo.lastLog.ActualCost, 1e-12)
	require.Equal(t, 1, userRepo.deductCalls)
	require.InDelta(t, 18.30, userRepo.lastAmount, 1e-12)
}

// 无价模型的拦截点在准入层（转发前）。走到 RecordUsage 说明上游成本已经真实发生，
// 此时丢弃整条记录会连用量、配额计数和对账线索一起丢掉，钱却照花。因此记账层的
// 契约是 fail-loud：不扣费、标记 billing_state=待结算、照常落库。
func TestKimiUnknownModelRecordUsageRecordsPricingUnavailable(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newMiniMaxGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)
	groupID := int64(42)

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "kimi-unknown-pricing",
			Model:         "kimi-k30",
			UpstreamModel: "kimi-k30",
			Usage: ClaudeUsage{
				InputTokens:  20,
				OutputTokens: 10,
			},
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:      7,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				Platform:       PlatformKimi,
				RateMultiplier: 1,
			},
		},
		User: &User{ID: 99},
		Account: &Account{
			ID:       101,
			Platform: PlatformKimi,
			Type:     AccountTypeAPIKey,
		},
	})

	require.NoError(t, err)
	require.Equal(t, 1, usageRepo.calls)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, BillingStatePricingUnavailable, usageRepo.lastLog.BillingState)
	require.Zero(t, usageRepo.lastLog.ActualCost)
	require.Zero(t, usageRepo.lastLog.TotalCost)
	// 用量本身必须完整保留，否则补配价格后无从重算。
	require.Equal(t, 20, usageRepo.lastLog.InputTokens)
	require.Equal(t, 10, usageRepo.lastLog.OutputTokens)
	require.Equal(t, 0, userRepo.deductCalls)
	require.Equal(t, 0, subRepo.incrementCalls)
}

func TestKimiUnknownModelRecordUsageSimpleModeRecordsZeroCost(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newMiniMaxGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)
	svc.cfg.RunMode = config.RunModeSimple
	groupID := int64(42)

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "kimi-simple-unknown-pricing",
			Model:         "kimi-future-v99",
			UpstreamModel: "kimi-future-v99",
			Usage: ClaudeUsage{
				InputTokens:  20,
				OutputTokens: 10,
			},
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:      7,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				Platform:       PlatformKimi,
				RateMultiplier: 1,
			},
		},
		User: &User{ID: 99},
		Account: &Account{
			ID:       101,
			Platform: PlatformKimi,
			Type:     AccountTypeAPIKey,
		},
	})

	require.NoError(t, err)
	require.Equal(t, 1, usageRepo.calls)
	require.NotNil(t, usageRepo.lastLog)
	require.Zero(t, usageRepo.lastLog.ActualCost)
	// Simple Mode 本就不扣费，但"这行为什么是 $0"仍要能区分：无价 ≠ 免费。
	require.Equal(t, BillingStatePricingUnavailable, usageRepo.lastLog.BillingState)
	require.Equal(t, 0, userRepo.deductCalls)
	require.Equal(t, 0, subRepo.incrementCalls)
}
