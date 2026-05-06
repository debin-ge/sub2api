package service

import (
	"context"
	"testing"
	"time"

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
	require.Nil(t, usageRepo.lastLog.UpstreamModel)
	require.InDelta(t, 6.06, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, 6.06, userRepo.lastAmount, 1e-12)
}
