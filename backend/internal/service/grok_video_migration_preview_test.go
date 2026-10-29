package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type grokPreviewKeyRepo struct {
	APIKeyRepository
	key *APIKey
}

func (r grokPreviewKeyRepo) GetByID(context.Context, int64) (*APIKey, error) {
	copy := *r.key
	return &copy, nil
}
func grokMigrationPreviewFixture(t *testing.T) (*VideoAdminService, GrokVideoMigrationPreviewRequest, *Group, *grokManagedHTTPStub) {
	t.Helper()
	tasks, _, http, request := newManagedGrokTaskTest(t)
	group := tasks.groups.(*videoGroupRepoStub).group
	old := 0.5
	group.VideoPrice480P = &old
	group.Hydrated = true
	account := tasks.accounts.(*videoAccountRepoStub).accounts[0]
	account.GroupIDs = []int64{group.ID}
	cfg := &config.Config{}
	gateway := &OpenAIGatewayService{cfg: cfg, billingService: NewBillingService(cfg, nil), httpUpstream: http}
	legacy := &GrokVideoTaskService{cfg: cfg, gateway: gateway, accounts: grokTaskAccounts{account: &account}, keys: &APIKeyService{apiKeyRepo: grokPreviewKeyRepo{key: request.APIKey}}}
	svc := &VideoAdminService{grok: legacy, taskSvc: tasks}
	input := GrokVideoMigrationPreviewRequest{APIKeyID: request.APIKey.ID, AccountID: account.ID, Cases: []GrokVideoMigrationCase{{Model: "grok-imagine-video-1.5", Resolution: "480p", Seconds: 8}}}
	return svc, input, group, http
}
func TestGrokMigrationPreviewUsesBothPriceResolversWithoutMutations(t *testing.T) {
	svc, input, group, upstream := grokMigrationPreviewFixture(t)
	report, err := svc.PreviewGrokMigration(context.Background(), input)
	require.NoError(t, err)
	require.True(t, report.Ready, report.Comparisons)
	require.Len(t, report.Comparisons, 1)
	row := report.Comparisons[0]
	require.InDelta(t, 8, *row.LegacyCost, 1e-8)
	require.InDelta(t, 8, *row.ManagedCost, 1e-8)
	require.Equal(t, VideoBillingUnitSecond, row.SuggestedRule.BillingUnit)
	require.Equal(t, 0.5, row.SuggestedRule.UnitPriceUSD)
	require.Zero(t, upstream.calls)
	unit := VideoBillingUnitSecond
	newPrice := 0.6
	input.ProposedModelPricing = []ChannelModelPricing{{Platform: PlatformGrok, Models: []string{"grok-imagine-video-1.5"}, BillingMode: BillingModeVideo, Intervals: []PricingInterval{{ID: 42, PerRequestPrice: &newPrice, BillingUnit: &unit}}}}
	changed, err := svc.PreviewGrokMigration(context.Background(), input)
	require.NoError(t, err)
	require.False(t, changed.Ready)
	require.Equal(t, "customer_price_difference", changed.Comparisons[0].Error)
	require.InDelta(t, 8, *changed.Comparisons[0].LegacyCost, 1e-8)
	require.InDelta(t, 9.6, *changed.Comparisons[0].ManagedCost, 1e-8)
	require.Equal(t, 0.5, *group.ModelPricing[0].Intervals[0].PerRequestPrice)
	require.NotEqual(t, report.Fingerprint, changed.Fingerprint)
	require.Zero(t, upstream.calls)
}
func TestGrokMigrationPreviewBlocksUnexplainedBillingModes(t *testing.T) {
	svc, input, group, _ := grokMigrationPreviewFixture(t)
	group.SubscriptionType = SubscriptionTypeSubscription
	result, err := svc.PreviewGrokMigration(context.Background(), input)
	require.NoError(t, err)
	require.False(t, result.Ready)
	require.Contains(t, result.Warnings, "subscription_groups_are_not_supported_by_managed_video")
	group.SubscriptionType = SubscriptionTypeStandard
	svc.grok.cfg.RunMode = config.RunModeSimple
	result, err = svc.PreviewGrokMigration(context.Background(), input)
	require.NoError(t, err)
	require.False(t, result.Ready)
	require.Contains(t, result.Warnings, "simple_mode_changes_from_legacy_usage_only_to_managed_balance_holds")
}

func TestGrokMigrationPreviewProtectsPendingLegacyJobs(t *testing.T) {
	svc, input, _, upstream := grokMigrationPreviewFixture(t)
	svc.grok.gateway.resolver = NewModelPricingResolver(nil, svc.grok.gateway.billingService)
	unit := VideoBillingUnitSecond
	price := 0.6
	input.ProposedModelPricing = []ChannelModelPricing{{Platform: PlatformGrok, Models: []string{"grok-imagine-video-1.5"}, BillingMode: BillingModeVideo, Intervals: []PricingInterval{{ID: 44, TierLabel: "480p", PerRequestPrice: &price, BillingUnit: &unit}}}}
	report, err := svc.PreviewGrokMigration(context.Background(), input)
	require.NoError(t, err)
	require.False(t, report.Ready)
	require.Equal(t, "legacy_pending_task_price_would_change", report.Comparisons[0].Error)
	require.InDelta(t, 8, *report.Comparisons[0].LegacyCost, 1e-8)
	require.InDelta(t, 9.6, *report.Comparisons[0].LegacyAfterCost, 1e-8)
	require.Zero(t, upstream.calls)
}
