//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestMergedReasoningPrecedenceChargesExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name, model, effort string
		values              map[string]float64
		legacy              *float64
		want                float64
	}{
		{"explicit disables fallback", "claude-fable-5-1", "max", map[string]float64{"max": 1}, testPtrFloat64(5), 1},
		{"explicit wins legacy", "claude-fable-5-1", "max", map[string]float64{"max": 2}, testPtrFloat64(5), 2},
		{"legacy fills missing max", "claude-fable-5-1", "max", map[string]float64{"high": 2}, testPtrFloat64(5), 5},
		{"original default", "claude-fable-5-1", "max", map[string]float64{"high": 2}, nil, 3},
		{"ordinary default", "custom", "max", nil, nil, 1},
		{"nonmax discount", "claude-fable-5-1", "high", map[string]float64{"high": 0.5}, testPtrFloat64(5), 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card := ChannelModelPricing{Platform: PlatformAnthropic, Models: []string{tc.model}, BillingMode: BillingModeToken, InputPrice: testPtrFloat64(0.001), OutputPrice: testPtrFloat64(0.002), ReasoningEffortMultipliers: tc.values, MaxReasoningEffortMultiplier: tc.legacy}
			bs, resolver := newTokenCostTestEnv(t, PlatformAnthropic, []ChannelModelPricing{card}, nil)
			group := &Group{ID: 100, Platform: PlatformAnthropic}
			cost, err := bs.CalculateTokenCostForRequest(TokenCostRequest{Ctx: context.Background(), Model: tc.model, Group: group, Resolver: resolver, Tokens: UsageTokens{InputTokens: 10, OutputTokens: 5}, RateMultiplier: 0.7, ReasoningEffort: tc.effort})
			require.NoError(t, err)
			require.InDelta(t, 0.02*tc.want, cost.TotalCost, 1e-12)
			require.InDelta(t, 0.02*tc.want*0.7, cost.ActualCost, 1e-12)
		})
	}
}

func TestMergedCacheStorageIsIndependentAndRecoverable(t *testing.T) {
	svc, _ := newGeminiCacheBillingGatewayForTest(t)
	groupID := int64(100)
	card := ChannelModelPricing{Platform: PlatformGemini, Models: []string{"gemini-2.5-pro"}, BillingMode: BillingModeToken, CacheReadPrice: testPtrFloat64(0), CacheWritePrice: testPtrFloat64(0), InputPrice: testPtrFloat64(0.001), OutputPrice: testPtrFloat64(0.002), ReasoningEffortMultipliers: map[string]float64{"high": 2}}
	_, resolver := newTokenCostTestEnv(t, PlatformGemini, []ChannelModelPricing{card}, nil)
	svc.resolver = resolver
	apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformGemini, Hydrated: true}}
	effort := "high"
	result := &ForwardResult{Model: "gemini-2.5-pro", Usage: ClaudeUsage{InputTokens: 10}, ReasoningEffort: &effort, CacheStorageTokenHours: 20000}
	cost, err := svc.calculateRecordUsageCost(context.Background(), result, apiKey, result.Model, 0.5, 0.5, time.Now())
	require.NoError(t, err)
	require.InDelta(t, 0.09, cost.CacheStorageCost, 1e-12)
	require.InDelta(t, 0.02+0.09, cost.TotalCost, 1e-12)
	require.InDelta(t, (0.02+0.09)*0.5, cost.ActualCost, 1e-12)
	result.Usage = ClaudeUsage{}
	cost, err = svc.calculateRecordUsageCost(context.Background(), result, apiKey, result.Model, 0.5, 0.5, time.Now())
	require.NoError(t, err)
	require.InDelta(t, 0.09, cost.TotalCost, 1e-12, "a TTL extension is neither a request nor reasoning usage")
	recovery := &BillingRecoveryService{billingService: svc.billingService}
	restored, err := recovery.recomputeCost(context.Background(), &UsageLog{Model: result.Model, CacheStorageTokenHours: 20000, RateMultiplier: 0.5}, result.Model, apiKey, nil)
	require.NoError(t, err)
	require.Equal(t, cost.TotalCost, restored.TotalCost)
	require.Equal(t, cost.CacheStorageCost, restored.CacheStorageCost)
}

func TestMergedRecoveryPreservesSubscriptionLongContextExemption(t *testing.T) {
	for _, withResolver := range []bool{false, true} {
		billing := NewBillingService(nil, nil)
		recovery := &BillingRecoveryService{billingService: billing}
		if withResolver {
			recovery.resolver = NewModelPricingResolver(nil, billing)
		}
		groupID := int64(1)
		key := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI, LongContextPricingEnabled: true}}
		upstream := "gpt-6.1-sol"
		log := &UsageLog{Model: upstream, RequestedModel: "public-alias", UpstreamModel: &upstream, GroupID: &groupID, InputTokens: 300000, OutputTokens: 1000, RateMultiplier: 1, CreatedAt: time.Now()}
		oauth, err := recovery.recomputeCost(context.Background(), log, upstream, key, &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth})
		require.NoError(t, err)
		api, err := recovery.recomputeCost(context.Background(), log, upstream, key, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey})
		require.NoError(t, err)
		require.InDelta(t, 0.61, oauth.TotalCost, 1e-10)
		require.InDelta(t, 1.215, api.TotalCost, 1e-10)
		key.Group.LongContextPricingEnabled = false
		api, err = recovery.recomputeCost(context.Background(), log, upstream, key, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey})
		require.NoError(t, err)
		require.InDelta(t, 0.61, api.TotalCost, 1e-10, "recovery must preserve both account and group opt-out")
	}
}
