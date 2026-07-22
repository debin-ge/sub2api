package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type radarQuotaAccountListerFake struct {
	accounts  []Account
	err       error
	calls     []radarQuotaListCall
	afterList func()
}

type radarQuotaListCall struct {
	platform, accountType, status, search, privacyMode string
	groupID                                            int64
}

func (f *radarQuotaAccountListerFake) ListAllWithFilters(
	_ context.Context,
	platform, accountType, status, search string,
	groupID int64,
	privacyMode string,
) ([]Account, error) {
	f.calls = append(f.calls, radarQuotaListCall{platform, accountType, status, search, privacyMode, groupID})
	if f.afterList != nil {
		f.afterList()
	}
	return f.accounts, f.err
}

type radarQuotaUsageReaderFake struct {
	snapshots map[int64]*UsageInfo
	errors    map[int64]error
	seen      []*Account
}

func (f *radarQuotaUsageReaderFake) GetRadarUsageSnapshot(ctx context.Context, account *Account) (*UsageInfo, error) {
	f.seen = append(f.seen, account)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.errors[account.ID]; err != nil {
		return nil, err
	}
	if snapshot, ok := f.snapshots[account.ID]; ok {
		return snapshot, nil
	}
	return nil, ErrRadarUsageSnapshotUnavailable
}

// GetUsage is deliberately outside RadarUsageSnapshotReader. If aggregation
// ever regresses to the active/general usage path, this panic makes the test fail.
func (*radarQuotaUsageReaderFake) GetUsage(context.Context, int64, string) (*UsageInfo, error) {
	panic("Radar aggregation must not call GetUsage or an upstream")
}

type radarQuotaBatchCall struct {
	accountIDs []int64
	startTime  time.Time
}

type radarQuotaBatchReaderFake struct {
	windowCalls      []radarQuotaBatchCall
	breakdownCalls   []radarQuotaBatchCall
	windowResults    []map[int64]*usagestats.AccountStats
	breakdownResults []map[int64]map[string]ModelCostStats
	windowErrors     map[int]error
	breakdownErrors  map[int]error
}

func (f *radarQuotaBatchReaderFake) GetAccountWindowStatsBatch(
	_ context.Context,
	accountIDs []int64,
	startTime time.Time,
) (map[int64]*usagestats.AccountStats, error) {
	callIndex := len(f.windowCalls)
	f.windowCalls = append(f.windowCalls, radarQuotaBatchCall{append([]int64(nil), accountIDs...), startTime})
	if err := f.windowErrors[callIndex]; err != nil {
		return nil, err
	}
	if callIndex < len(f.windowResults) {
		return f.windowResults[callIndex], nil
	}
	return map[int64]*usagestats.AccountStats{}, nil
}

func (f *radarQuotaBatchReaderFake) GetAccountModelBreakdownBatch(
	_ context.Context,
	accountIDs []int64,
	startTime time.Time,
) (map[int64]map[string]ModelCostStats, error) {
	callIndex := len(f.breakdownCalls)
	f.breakdownCalls = append(f.breakdownCalls, radarQuotaBatchCall{append([]int64(nil), accountIDs...), startTime})
	if err := f.breakdownErrors[callIndex]; err != nil {
		return nil, err
	}
	if callIndex < len(f.breakdownResults) {
		return f.breakdownResults[callIndex], nil
	}
	return map[int64]map[string]ModelCostStats{}, nil
}

// These single-account methods are panic guards against accidental N+1 SQL.
func (*radarQuotaBatchReaderFake) GetAccountWindowStats(context.Context, int64, time.Time) (*usagestats.AccountStats, error) {
	panic("Radar aggregation must batch account window stats")
}

func (*radarQuotaBatchReaderFake) GetAccountModelBreakdown(context.Context, int64, time.Time) (map[string]ModelCostStats, error) {
	panic("Radar aggregation must batch model breakdown")
}

type radarQuotaCacheFake struct {
	writes []BucketSnapshotDTO
	errors map[string]error
}

func (f *radarQuotaCacheFake) AppendBucketSnapshot(_ context.Context, snapshot BucketSnapshotDTO) error {
	f.writes = append(f.writes, snapshot)
	return f.errors[snapshot.BucketKey]
}

func radarQuotaTestConfig() *config.RadarConfig {
	return &config.RadarConfig{
		PublicMinBucketAccounts: 2,
		InferMinUtilization:     5,
		InferMaxStdevRatio:      0.3,
	}
}

func radarQuotaProgress(utilization float64) *UsageProgress {
	return &UsageProgress{Utilization: utilization}
}

func radarQuotaAnthropicAccount(id int64, tier string) Account {
	return Account{
		ID:       id,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"plan_slug": tier},
	}
}

func radarQuotaOpenAIAccount(id int64, tier string) Account {
	return Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"plan_type": tier},
	}
}

func radarQuotaAntigravityAccount(id int64, accountType string) Account {
	return Account{ID: id, Platform: PlatformAntigravity, Type: accountType}
}

func newRadarQuotaTestAggregator(
	t *testing.T,
	accounts *radarQuotaAccountListerFake,
	usage *radarQuotaUsageReaderFake,
	batch *radarQuotaBatchReaderFake,
	cache *radarQuotaCacheFake,
	cfg *config.RadarConfig,
	now func() time.Time,
) *RadarQuotaAggregator {
	t.Helper()
	aggregator, err := newRadarQuotaAggregator(accounts, usage, batch, cache, cfg, now)
	require.NoError(t, err)
	return aggregator
}

func TestRadarQuotaAggregatorConstructorAndInference(t *testing.T) {
	t.Run("constructor validates dependencies and copies only validated configuration", func(t *testing.T) {
		accounts := &radarQuotaAccountListerFake{}
		usage := &radarQuotaUsageReaderFake{}
		batch := &radarQuotaBatchReaderFake{}
		cache := &radarQuotaCacheFake{}
		valid := radarQuotaTestConfig()

		missingDependencyCases := []struct {
			name     string
			accounts radarQuotaAccountLister
			usage    RadarUsageSnapshotReader
			batch    RadarQuotaBatchReader
			cache    radarQuotaSnapshotWriter
			cfg      *config.RadarConfig
			now      func() time.Time
		}{
			{"accounts", nil, usage, batch, cache, valid, time.Now},
			{"usage", accounts, nil, batch, cache, valid, time.Now},
			{"batch", accounts, usage, nil, cache, valid, time.Now},
			{"cache", accounts, usage, batch, nil, valid, time.Now},
			{"config", accounts, usage, batch, cache, nil, time.Now},
			{"clock", accounts, usage, batch, cache, valid, nil},
		}
		for _, testCase := range missingDependencyCases {
			t.Run(testCase.name, func(t *testing.T) {
				_, err := newRadarQuotaAggregator(
					testCase.accounts,
					testCase.usage,
					testCase.batch,
					testCase.cache,
					testCase.cfg,
					testCase.now,
				)
				require.Error(t, err)
			})
		}

		invalidConfigCases := []struct {
			name   string
			mutate func(*config.RadarConfig)
		}{
			{"utilization zero", func(cfg *config.RadarConfig) { cfg.InferMinUtilization = 0 }},
			{"utilization above one hundred", func(cfg *config.RadarConfig) { cfg.InferMinUtilization = 100.1 }},
			{"utilization nonfinite", func(cfg *config.RadarConfig) { cfg.InferMinUtilization = math.NaN() }},
			{"ratio zero", func(cfg *config.RadarConfig) { cfg.InferMaxStdevRatio = 0 }},
			{"ratio above one", func(cfg *config.RadarConfig) { cfg.InferMaxStdevRatio = 1.1 }},
			{"ratio nonfinite", func(cfg *config.RadarConfig) { cfg.InferMaxStdevRatio = math.Inf(1) }},
		}
		for _, testCase := range invalidConfigCases {
			t.Run(testCase.name, func(t *testing.T) {
				cfg := radarQuotaTestConfig()
				testCase.mutate(cfg)
				_, err := NewRadarQuotaAggregator(accounts, usage, batch, cache, cfg)
				require.Error(t, err)
			})
		}

		cfg := radarQuotaTestConfig()
		cfg.PublicMinBucketAccounts = 0
		aggregator, err := NewRadarQuotaAggregator(accounts, usage, batch, cache, cfg)
		require.NoError(t, err)
		require.Equal(t, 1, aggregator.cfg.PublicMinBucketAccounts)

		cfg.PublicMinBucketAccounts = 99
		cfg.InferMinUtilization = 99
		cfg.InferMaxStdevRatio = 0.99
		require.Equal(t, 1, aggregator.cfg.PublicMinBucketAccounts)
		require.Equal(t, 5.0, aggregator.cfg.InferMinUtilization)
		require.Equal(t, 0.3, aggregator.cfg.InferMaxStdevRatio)
	})

	t.Run("inferLimit covers filtering sample and dispersion boundaries", func(t *testing.T) {
		assertRejected := func(
			t *testing.T,
			result radarQuotaInferenceResult,
			reason InferenceRejectReason,
			sampleSize int,
		) {
			t.Helper()
			require.Nil(t, result.limit)
			require.Nil(t, result.stdev)
			require.Equal(t, sampleSize, result.sampleSize)
			require.NotNil(t, result.rejectReason)
			require.Equal(t, reason, *result.rejectReason)
		}

		result := inferLimit([]radarQuotaInferenceSample{
			{utilization: 4.99, cost: 4.99},
			{utilization: 5.00, cost: 5.00},
		}, 5, 0.3)
		require.Equal(t, 1, result.sampleSize)
		require.Nil(t, result.rejectReason)
		require.InDelta(t, 100, *result.limit, 1e-12)
		require.InDelta(t, 0, *result.stdev, 1e-12)

		result = inferLimit([]radarQuotaInferenceSample{
			{utilization: 5.00, cost: 5.00},
			{utilization: 5.00, cost: 5.00},
		}, 5, 0.3)
		require.Equal(t, 2, result.sampleSize)
		require.Nil(t, result.rejectReason)
		require.InDelta(t, 100, *result.limit, 1e-12)
		require.InDelta(t, 0, *result.stdev, 1e-12)

		result = inferLimit([]radarQuotaInferenceSample{{utilization: 10, cost: 10}}, 5, 0.3)
		require.Equal(t, 1, result.sampleSize)
		require.Nil(t, result.rejectReason)
		require.InDelta(t, 100, *result.limit, 1e-12)
		require.InDelta(t, 0, *result.stdev, 1e-12)

		result = inferLimit([]radarQuotaInferenceSample{{utilization: 4.99, cost: 4.99}}, 5, 0.3)
		assertRejected(t, result, InferenceRejectReasonInsufficientSamples, 0)

		result = inferLimit([]radarQuotaInferenceSample{
			{utilization: 100, cost: 70},
			{utilization: 100, cost: 130},
		}, 5, 0.3)
		require.Nil(t, result.rejectReason, "ratio exactly 0.3 must be accepted")
		require.InDelta(t, 100, *result.limit, 1e-12)
		require.InDelta(t, 30, *result.stdev, 1e-12)

		result = inferLimit([]radarQuotaInferenceSample{
			{utilization: 100, cost: 60},
			{utilization: 100, cost: 140},
		}, 5, 0.3)
		assertRejected(t, result, InferenceRejectReasonHighDispersion, 2)

		result = inferLimit([]radarQuotaInferenceSample{
			{utilization: 10, cost: 0},
			{utilization: 20, cost: 0},
		}, 5, 0.3)
		assertRejected(t, result, InferenceRejectReasonInvalidMean, 2)

		result = inferLimit([]radarQuotaInferenceSample{
			{utilization: 10, cost: -1},
			{utilization: math.NaN(), cost: 1},
			{utilization: math.Inf(1), cost: 1},
			{utilization: 10, cost: math.NaN()},
			{utilization: 10, cost: math.Inf(1)},
			{utilization: 10, cost: 10},
			{utilization: 20, cost: 20},
		}, 5, 0.3)
		require.Equal(t, 2, result.sampleSize)
		require.Nil(t, result.rejectReason)
		require.InDelta(t, 100, *result.limit, 1e-12)
		require.InDelta(t, 0, *result.stdev, 1e-12)
	})

	t.Run("bucket identity normalizes safe tiers without unsafe fallback", func(t *testing.T) {
		tests := []struct {
			name    string
			account Account
			usage   *UsageInfo
			want    radarQuotaBucketIdentity
			wantOK  bool
		}{
			{
				name:    "anthropic Pro",
				account: radarQuotaAnthropicAccount(1, " Claude_Pro "),
				usage:   &UsageInfo{},
				want:    radarQuotaBucketIdentity{"anthropic/pro", "anthropic", "pro", "Claude Pro"},
				wantOK:  true,
			},
			{
				name:    "anthropic 5x Max alias",
				account: radarQuotaAnthropicAccount(1, "5xMax"),
				usage:   &UsageInfo{},
				want:    radarQuotaBucketIdentity{"anthropic/max_5x", "anthropic", "max_5x", "Claude Max 5x"},
				wantOK:  true,
			},
			{
				name:    "anthropic 20x Max",
				account: radarQuotaAnthropicAccount(1, " Max_20X "),
				usage:   &UsageInfo{},
				want:    radarQuotaBucketIdentity{"anthropic/max_20x", "anthropic", "max_20x", "Claude Max 20x"},
				wantOK:  true,
			},
			{
				name:    "openai Plus alias",
				account: radarQuotaOpenAIAccount(1, "ChatGPT_Plus"),
				usage:   &UsageInfo{},
				want:    radarQuotaBucketIdentity{"openai/plus", "openai", "plus", "ChatGPT Plus"},
				wantOK:  true,
			},
			{
				name:    "openai 5x Pro alias",
				account: radarQuotaOpenAIAccount(1, "5xPro"),
				usage:   &UsageInfo{},
				want:    radarQuotaBucketIdentity{"openai/pro", "openai", "pro", "ChatGPT Pro"},
				wantOK:  true,
			},
			{
				name:    "openai 20x Pro alias",
				account: radarQuotaOpenAIAccount(1, "20xPro"),
				usage:   &UsageInfo{},
				want:    radarQuotaBucketIdentity{"openai/pro", "openai", "pro", "ChatGPT Pro"},
				wantOK:  true,
			},
			{
				name:    "upstream plain Pro remains conservative Pro",
				account: radarQuotaOpenAIAccount(1, "pro"),
				usage:   &UsageInfo{},
				want:    radarQuotaBucketIdentity{"openai/pro", "openai", "pro", "ChatGPT Pro"},
				wantOK:  true,
			},
			{
				name:    "anthropic empty is not a supported plan",
				account: radarQuotaAnthropicAccount(1, " "),
				usage:   &UsageInfo{},
				wantOK:  false,
			},
			{
				name:    "openai empty is not a supported plan",
				account: Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth},
				usage:   &UsageInfo{},
				wantOK:  false,
			},
			{
				name:    "antigravity is not a quota radar platform",
				account: Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": "secret"}},
				usage:   &UsageInfo{SubscriptionTier: " ULTRA "},
				wantOK:  false,
			},
			{
				name:    "slash is invalid and must not become generic",
				account: radarQuotaAnthropicAccount(1, "customer/email@example.com"),
				usage:   &UsageInfo{},
				wantOK:  false,
			},
			{
				name:    "non string openai tier is invalid and must not be coerced",
				account: Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": 42}},
				usage:   &UsageInfo{},
				wantOK:  false,
			},
			{
				name:    "invalid first character",
				account: radarQuotaAnthropicAccount(1, "_private"),
				usage:   &UsageInfo{},
				wantOK:  false,
			},
			{
				name:    "non ascii",
				account: radarQuotaAnthropicAccount(1, "套餐"),
				usage:   &UsageInfo{},
				wantOK:  false,
			},
			{
				name:    "over 64 bytes",
				account: radarQuotaAnthropicAccount(1, strings.Repeat("a", 65)),
				usage:   &UsageInfo{},
				wantOK:  false,
			},
		}

		for _, testCase := range tests {
			t.Run(testCase.name, func(t *testing.T) {
				got, ok := buildRadarQuotaBucketIdentity(&testCase.account, testCase.usage)
				require.Equal(t, testCase.wantOK, ok)
				if ok {
					require.Equal(t, testCase.want, got)
				}
			})
		}
	})
}

func TestRadarQuotaAggregatorRunOnceSelectionBatchingAndAggregation(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 9, 10, 987654321, time.FixedZone("CST", 8*60*60))
	parentID := int64(999)
	accounts := &radarQuotaAccountListerFake{accounts: []Account{
		radarQuotaAnthropicAccount(1, " Max_20X "),
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Extra: map[string]any{"plan_slug": "max_20x"}},
		radarQuotaOpenAIAccount(3, " Pro "),
		radarQuotaOpenAIAccount(4, "PRO"),
		radarQuotaAntigravityAccount(5, AccountTypeOAuth),
		radarQuotaAntigravityAccount(6, AccountTypeUpstream),
		{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth, ParentAccountID: &parentID},
		{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID, Credentials: map[string]any{"plan_type": "pro"}},
		{ID: 9, Platform: PlatformAntigravity, Type: AccountTypeOAuth, ParentAccountID: &parentID},
		radarQuotaOpenAIAccount(10, "free"),
		{ID: 11, Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
		{ID: 12, Platform: PlatformAntigravity, Type: AccountTypeAPIKey},
		{ID: 13, Platform: PlatformGemini, Type: AccountTypeOAuth},
	}}
	usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
		1: {FiveHour: radarQuotaProgress(4), SevenDay: radarQuotaProgress(10), SevenDaySonnet: radarQuotaProgress(10), SevenDayFable: radarQuotaProgress(40)},
		2: {FiveHour: radarQuotaProgress(20), SevenDaySonnet: radarQuotaProgress(30)},
		3: {FiveHour: radarQuotaProgress(5)},
		4: {FiveHour: radarQuotaProgress(15)},
		5: {FiveHour: radarQuotaProgress(25), SubscriptionTier: " ULTRA "},
		6: {FiveHour: radarQuotaProgress(35), SubscriptionTier: "ultra"},
	}}
	batch := &radarQuotaBatchReaderFake{
		windowResults: []map[int64]*usagestats.AccountStats{
			{
				1: {Cost: 4}, 2: {Cost: 20},
				3: {Cost: 5}, 4: {Cost: 15},
				5: {Cost: 25}, 6: {Cost: 35},
			},
			{1: {Cost: 10}},
		},
		breakdownResults: []map[int64]map[string]ModelCostStats{
			{
				1: {
					"model-a": {Requests: 1, AccountCost: 2},
					"zero":    {Requests: 3, AccountCost: 0},
				},
				2: {
					"model-a": {Requests: 2, AccountCost: 4},
					"model-b": {Requests: 2, AccountCost: 6},
				},
			},
			{1: {"seven-only": {Requests: 2, AccountCost: 8}}},
		},
	}
	cache := &radarQuotaCacheFake{}
	aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

	require.NoError(t, aggregator.RunOnce(context.Background()))
	require.Equal(t, []radarQuotaListCall{{}}, accounts.calls)

	seenIDs := make([]int64, len(usage.seen))
	for i, account := range usage.seen {
		seenIDs[i] = account.ID
		require.Same(t, &accounts.accounts[i], account, "Reader must receive the original &accounts[i]")
	}
	require.Equal(t, []int64{1, 2, 3, 4}, seenIDs)

	require.Len(t, batch.windowCalls, 2)
	require.Len(t, batch.breakdownCalls, 2)
	for _, call := range append(append([]radarQuotaBatchCall{}, batch.windowCalls...), batch.breakdownCalls...) {
		require.Equal(t, []int64{1, 2, 3, 4}, call.accountIDs)
	}
	require.Equal(t, now.UTC().Add(-5*time.Hour), batch.windowCalls[0].startTime)
	require.Equal(t, now.UTC().Add(-7*24*time.Hour), batch.windowCalls[1].startTime)
	require.Equal(t, now.UTC().Add(-5*time.Hour), batch.breakdownCalls[0].startTime)
	require.Equal(t, now.UTC().Add(-7*24*time.Hour), batch.breakdownCalls[1].startTime)

	require.Len(t, cache.writes, 2)
	require.Equal(t, []string{"anthropic/max_20x", "openai/pro"}, []string{
		cache.writes[0].BucketKey,
		cache.writes[1].BucketKey,
	})
	wantCapturedAt := now.UTC().Truncate(time.Millisecond)
	for _, snapshot := range cache.writes {
		require.Equal(t, wantCapturedAt, snapshot.CapturedAt)
		require.Equal(t, 2, snapshot.AccountsCount)
		require.NotNil(t, snapshot.ModelBreakdown5h)
		require.NotNil(t, snapshot.ModelBreakdown7d)
	}

	anthropic := cache.writes[0]
	require.Equal(t, "Claude Max 20x", anthropic.DisplayName)
	require.InDelta(t, 12, anthropic.FiveHour.AvgUtilization, 1e-12, "low-utilization accounts remain in averages")
	require.InDelta(t, 4, anthropic.FiveHour.MinUtilization, 1e-12)
	require.InDelta(t, 20, anthropic.FiveHour.MaxUtilization, 1e-12)
	require.InDelta(t, 12, anthropic.FiveHour.AvgCost, 1e-12)
	require.Equal(t, 1, anthropic.FiveHour.SampleSize)
	require.Nil(t, anthropic.FiveHour.InferenceRejectReason)
	require.InDelta(t, 100, *anthropic.FiveHour.InferredLimitUSD, 1e-12)
	require.InDelta(t, 0, *anthropic.FiveHour.InferredStdev, 1e-12)
	require.Nil(t, anthropic.SevenDay, "a one-account 7d window must not be public")
	require.Equal(t, &ModelWindowStatsDTO{Model: "claude-sonnet", AvgUtilization: 20, SampleSize: 2}, anthropic.SevenDaySonnet)
	require.Nil(t, anthropic.SevenDayFable, "a one-account Fable window must not be public")
	require.Equal(t, []ModelCostBreakdownDTO{{Model: "other", AvgCost: 6, AvgRequests: 4, Percentage: 100, ContributorsCount: 2}}, anthropic.ModelBreakdown5h)
	require.NotNil(t, anthropic.ModelBreakdown7d)
	require.Empty(t, anthropic.ModelBreakdown7d, "a one-account private model must not be public")

	require.Nil(t, cache.writes[1].SevenDay)
	require.Equal(t, "ChatGPT Pro", cache.writes[1].DisplayName)
	for _, snapshot := range cache.writes {
		require.Equal(t, 2, snapshot.PrivacyThreshold)
		require.NoError(t, ValidateRadarBucketSnapshot(snapshot))
	}

	encoded, err := json.Marshal(cache.writes)
	require.NoError(t, err)
	for _, forbidden := range []string{"account_id", "account_ids", "credentials", "user_id", "email"} {
		require.NotContains(t, string(encoded), forbidden)
	}
}

func TestRadarQuotaAggregatorPublishesOnlySupportedAccountPlanIntersection(t *testing.T) {
	now := time.Date(2026, time.July, 20, 13, 0, 0, 0, time.UTC)
	accounts := &radarQuotaAccountListerFake{accounts: []Account{
		radarQuotaAnthropicAccount(1, "pro"),
		radarQuotaAnthropicAccount(2, "claude_pro"),
		radarQuotaAnthropicAccount(3, "max_5x"),
		radarQuotaAnthropicAccount(4, "5xMax"),
		radarQuotaAnthropicAccount(5, "max_20x"),
		radarQuotaAnthropicAccount(6, "20xMax"),
		radarQuotaOpenAIAccount(7, "plus"),
		radarQuotaOpenAIAccount(8, "chatgpt_plus"),
		radarQuotaOpenAIAccount(9, "pro_5x"),
		radarQuotaOpenAIAccount(10, "5xPro"),
		radarQuotaOpenAIAccount(11, "pro"),
		radarQuotaOpenAIAccount(12, "20xPro"),
		radarQuotaAnthropicAccount(13, "team"),
		radarQuotaOpenAIAccount(14, "team"),
		radarQuotaAntigravityAccount(15, AccountTypeOAuth),
	}}
	usage := &radarQuotaUsageReaderFake{snapshots: make(map[int64]*UsageInfo)}
	for id := int64(1); id <= 15; id++ {
		usage.snapshots[id] = &UsageInfo{SevenDay: radarQuotaProgress(10)}
	}
	batch := &radarQuotaBatchReaderFake{}
	cache := &radarQuotaCacheFake{}
	aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

	require.NoError(t, aggregator.RunOnce(context.Background()))
	require.Len(t, usage.seen, 12, "unsupported platforms and plans must not be queried")
	require.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, batch.windowCalls[0].accountIDs)
	require.Len(t, cache.writes, 5, "all OpenAI Pro aliases must share one conservative bucket")
	require.Equal(t, []string{
		"anthropic/max_20x",
		"anthropic/max_5x",
		"anthropic/pro",
		"openai/plus",
		"openai/pro",
	}, []string{
		cache.writes[0].BucketKey,
		cache.writes[1].BucketKey,
		cache.writes[2].BucketKey,
		cache.writes[3].BucketKey,
		cache.writes[4].BucketKey,
	})
	require.Equal(t, []string{
		"Claude Max 20x",
		"Claude Max 5x",
		"Claude Pro",
		"ChatGPT Plus",
		"ChatGPT Pro",
	}, []string{
		cache.writes[0].DisplayName,
		cache.writes[1].DisplayName,
		cache.writes[2].DisplayName,
		cache.writes[3].DisplayName,
		cache.writes[4].DisplayName,
	})
}

func TestRadarQuotaAggregatorPublishesSingleAccountBucketWithSevenDayOnly(t *testing.T) {
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	accounts := &radarQuotaAccountListerFake{accounts: []Account{
		radarQuotaOpenAIAccount(1, "20xPro"),
	}}
	usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
		1: {SevenDay: radarQuotaProgress(24)},
	}}
	batch := &radarQuotaBatchReaderFake{windowResults: []map[int64]*usagestats.AccountStats{
		{},
		{1: {Cost: 12}},
	}}
	cache := &radarQuotaCacheFake{}
	cfg := radarQuotaTestConfig()
	cfg.PublicMinBucketAccounts = 1
	aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, cfg, func() time.Time { return now })

	report, err := aggregator.RunOnceWithReport(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.BucketCount)
	require.Equal(t, 0, report.PrivacyFilteredBucketCount, "a lone account publishes under the single-account floor")
	require.Len(t, cache.writes, 1)
	snapshot := cache.writes[0]
	require.Equal(t, "openai/pro", snapshot.BucketKey)
	require.Equal(t, "ChatGPT Pro", snapshot.DisplayName)
	require.Equal(t, 1, snapshot.AccountsCount)
	require.Equal(t, 1, snapshot.PrivacyThreshold)
	require.Nil(t, snapshot.FiveHour)
	require.NotNil(t, snapshot.SevenDay)
	require.Equal(t, 1, snapshot.SevenDay.ContributorsCount)
	require.Equal(t, 1, snapshot.SevenDay.SampleSize)
	require.InDelta(t, 24, snapshot.SevenDay.AvgUtilization, 1e-12)
	require.InDelta(t, 50, *snapshot.SevenDay.InferredLimitUSD, 1e-12)
	require.InDelta(t, 0, *snapshot.SevenDay.InferredStdev, 1e-12)
	require.Nil(t, snapshot.SevenDay.InferenceRejectReason)
	require.NoError(t, ValidateRadarBucketSnapshot(snapshot))
}

func TestRadarQuotaAggregatorUsesOpenAISparkShadowSnapshotsOncePerParent(t *testing.T) {
	now := time.Date(2026, time.July, 19, 11, 0, 0, 0, time.UTC)
	parents := []Account{
		radarQuotaOpenAIAccount(1, "pro"),
		radarQuotaOpenAIAccount(2, "pro"),
		radarQuotaOpenAIAccount(3, "pro"),
	}
	parent1, parent2, parent3 := int64(1), int64(2), int64(3)
	accounts := &radarQuotaAccountListerFake{accounts: []Account{
		parents[0],
		{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent1, QuotaDimension: QuotaDimensionSpark},
		{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent2, QuotaDimension: QuotaDimensionSpark},
		parents[1],
		{ID: 103, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent3, QuotaDimension: QuotaDimensionSpark},
		parents[2],
	}}
	usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
		1:   {FiveHour: radarQuotaProgress(10)},
		101: {FiveHour: radarQuotaProgress(99)}, // same subscription: parent wins
		102: {FiveHour: radarQuotaProgress(20)}, // parent has no passive snapshot
		103: {FiveHour: radarQuotaProgress(30)},
	}}
	batch := &radarQuotaBatchReaderFake{}
	cache := &radarQuotaCacheFake{}
	aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

	require.NoError(t, aggregator.RunOnce(context.Background()))
	require.Len(t, cache.writes, 1)
	snapshot := cache.writes[0]
	require.Equal(t, "openai/pro", snapshot.BucketKey)
	require.Equal(t, 3, snapshot.AccountsCount, "a parent and its Spark shadow must be one privacy contributor")
	require.NotNil(t, snapshot.FiveHour)
	require.InDelta(t, 20, snapshot.FiveHour.AvgUtilization, 1e-12)
	require.Equal(t, []int64{1, 102, 103}, batch.windowCalls[0].accountIDs)
	require.Equal(t, []int64{1, 102, 103}, batch.breakdownCalls[0].accountIDs)
	require.NoError(t, ValidateRadarBucketSnapshot(snapshot))
}

func TestRadarQuotaAggregatorRunOnceFailurePrivacyAndDeterminism(t *testing.T) {
	now := time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC)

	t.Run("empty candidate set performs no reader batch or cache calls", func(t *testing.T) {
		accounts := &radarQuotaAccountListerFake{accounts: []Account{{ID: 1, Platform: PlatformGemini, Type: AccountTypeOAuth}}}
		usage := &radarQuotaUsageReaderFake{}
		batch := &radarQuotaBatchReaderFake{}
		cache := &radarQuotaCacheFake{}
		aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

		require.NoError(t, aggregator.RunOnce(context.Background()))
		require.Empty(t, usage.seen)
		require.Empty(t, batch.windowCalls)
		require.Empty(t, batch.breakdownCalls)
		require.Empty(t, cache.writes)
	})

	t.Run("successful lister cancellation is observed before the zero-candidate return", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		accounts := &radarQuotaAccountListerFake{afterList: cancel}
		aggregator := newRadarQuotaTestAggregator(
			t,
			accounts,
			&radarQuotaUsageReaderFake{},
			&radarQuotaBatchReaderFake{},
			&radarQuotaCacheFake{},
			radarQuotaTestConfig(),
			time.Now,
		)

		require.ErrorIs(t, aggregator.RunOnce(ctx), context.Canceled)
	})

	t.Run("ordinary reader errors and invalid nonempty tiers skip without leaking or fallback", func(t *testing.T) {
		accounts := &radarQuotaAccountListerFake{accounts: []Account{
			radarQuotaAnthropicAccount(1, "max_20x"),
			radarQuotaAnthropicAccount(2, "customer/alice@example.com"),
			radarQuotaAnthropicAccount(3, "max_20x"),
			radarQuotaAnthropicAccount(4, "max_20x"),
		}}
		usage := &radarQuotaUsageReaderFake{
			snapshots: map[int64]*UsageInfo{
				2: {FiveHour: radarQuotaProgress(10)},
				3: {FiveHour: radarQuotaProgress(10)},
			},
			errors: map[int64]error{1: errors.New("upstream secret detail")},
		}
		batch := &radarQuotaBatchReaderFake{}
		cache := &radarQuotaCacheFake{}
		aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

		require.NoError(t, aggregator.RunOnce(context.Background()))
		require.Len(t, usage.seen, 3)
		require.Equal(t, []int64{3}, batch.windowCalls[0].accountIDs)
		require.Empty(t, cache.writes, "one valid account is below the privacy threshold")
	})

	t.Run("unsupported plan tiers are excluded from supported buckets", func(t *testing.T) {
		accounts := &radarQuotaAccountListerFake{accounts: []Account{
			radarQuotaAnthropicAccount(1, "max_20x"),
			radarQuotaAnthropicAccount(2, "max_20x"),
			radarQuotaAnthropicAccount(3, "team"),
			radarQuotaAnthropicAccount(4, "team"),
		}}
		usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
			1: {FiveHour: radarQuotaProgress(10)},
			2: {FiveHour: radarQuotaProgress(10)},
			3: {FiveHour: radarQuotaProgress(10)},
			4: {FiveHour: radarQuotaProgress(10)},
		}}
		batch := &radarQuotaBatchReaderFake{}
		cache := &radarQuotaCacheFake{}
		aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

		require.NoError(t, aggregator.RunOnce(context.Background()))
		require.Len(t, cache.writes, 1)
		require.Equal(t, "anthropic/max_20x", cache.writes[0].BucketKey)
		require.Len(t, usage.seen, 2)
	})

	t.Run("duplicate successful account ids are queried and counted once", func(t *testing.T) {
		accounts := &radarQuotaAccountListerFake{accounts: []Account{
			radarQuotaAnthropicAccount(5, "max_20x"),
			radarQuotaAnthropicAccount(5, "max_20x"),
		}}
		usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{5: {FiveHour: radarQuotaProgress(10)}}}
		batch := &radarQuotaBatchReaderFake{}
		cache := &radarQuotaCacheFake{}
		aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

		require.NoError(t, aggregator.RunOnce(context.Background()))
		require.Len(t, usage.seen, 2)
		require.Equal(t, []int64{5}, batch.windowCalls[0].accountIDs)
		require.Empty(t, cache.writes)
	})

	t.Run("nonpositive ids and reader results without any window are skipped defensively", func(t *testing.T) {
		accounts := &radarQuotaAccountListerFake{accounts: []Account{
			radarQuotaAnthropicAccount(-1, "max_20x"),
			radarQuotaAnthropicAccount(0, "max_20x"),
			radarQuotaAnthropicAccount(1, "max_20x"),
			radarQuotaAnthropicAccount(2, "max_20x"),
			radarQuotaAnthropicAccount(3, "max_20x"),
		}}
		usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
			-1: {FiveHour: radarQuotaProgress(10)},
			0:  {FiveHour: radarQuotaProgress(10)},
			1:  {},
			2:  {FiveHour: radarQuotaProgress(10)},
			3:  {SevenDaySonnet: radarQuotaProgress(10)},
		}}
		batch := &radarQuotaBatchReaderFake{}
		cache := &radarQuotaCacheFake{}
		aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

		require.NoError(t, aggregator.RunOnce(context.Background()))
		require.Len(t, usage.seen, 3)
		require.Equal(t, []int64{2, 3}, batch.windowCalls[0].accountIDs)
		require.Len(t, cache.writes, 1)
		require.Equal(t, 2, cache.writes[0].AccountsCount)
	})

	t.Run("only accounts with at least one finite in-range window count toward privacy", func(t *testing.T) {
		accounts := &radarQuotaAccountListerFake{accounts: []Account{
			radarQuotaAnthropicAccount(1, "max_20x"),
			radarQuotaAnthropicAccount(2, "max_20x"),
		}}
		usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
			1: {
				FiveHour:       radarQuotaProgress(math.NaN()),
				SevenDay:       radarQuotaProgress(math.Inf(1)),
				SevenDaySonnet: radarQuotaProgress(-0.01),
				SevenDayFable:  radarQuotaProgress(100.01),
			},
			2: {FiveHour: radarQuotaProgress(10)},
		}}
		batch := &radarQuotaBatchReaderFake{}
		cache := &radarQuotaCacheFake{}
		aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

		require.NoError(t, aggregator.RunOnce(context.Background()))
		require.Equal(t, []int64{2}, batch.windowCalls[0].accountIDs)
		require.Empty(t, cache.writes, "only one account has a valid passive window")
	})

	t.Run("an account with mixed invalid and valid windows still counts by its valid window", func(t *testing.T) {
		accounts := &radarQuotaAccountListerFake{accounts: []Account{
			radarQuotaAnthropicAccount(1, "max_20x"),
			radarQuotaAnthropicAccount(2, "max_20x"),
		}}
		usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
			1: {FiveHour: radarQuotaProgress(math.NaN()), SevenDay: radarQuotaProgress(10)},
			2: {FiveHour: radarQuotaProgress(20)},
		}}
		batch := &radarQuotaBatchReaderFake{}
		cache := &radarQuotaCacheFake{}
		aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

		require.NoError(t, aggregator.RunOnce(context.Background()))
		require.Equal(t, []int64{1, 2}, batch.windowCalls[0].accountIDs)
		require.Len(t, cache.writes, 1)
		require.Equal(t, 2, cache.writes[0].AccountsCount)
		require.Nil(t, cache.writes[0].FiveHour)
		require.Nil(t, cache.writes[0].SevenDay)
	})

	t.Run("window inference sample size is independent from publication anonymity", func(t *testing.T) {
		accounts := &radarQuotaAccountListerFake{accounts: []Account{
			radarQuotaAnthropicAccount(1, "max_20x"),
			radarQuotaAnthropicAccount(2, "max_20x"),
			radarQuotaAnthropicAccount(3, "max_20x"),
		}}
		usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
			1: {FiveHour: radarQuotaProgress(10)},
			2: {FiveHour: radarQuotaProgress(20)},
			3: {FiveHour: radarQuotaProgress(30)},
		}}
		batch := &radarQuotaBatchReaderFake{windowResults: []map[int64]*usagestats.AccountStats{
			{1: {Cost: 10}, 2: {Cost: 20}},
			{},
		}}
		cache := &radarQuotaCacheFake{}
		cfg := radarQuotaTestConfig()
		cfg.PublicMinBucketAccounts = 3
		aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, cfg, func() time.Time { return now })

		require.NoError(t, aggregator.RunOnce(context.Background()))
		require.Len(t, cache.writes, 1)
		window := cache.writes[0].FiveHour
		require.NotNil(t, window)
		require.Equal(t, 2, window.SampleSize)
		require.InDelta(t, 100, *window.InferredLimitUSD, 1e-12)
		require.InDelta(t, 0, *window.InferredStdev, 1e-12)
		require.Nil(t, window.InferenceRejectReason)
	})

	t.Run("per-window and per-model k anonymity prevents raw label disclosure", func(t *testing.T) {
		catalog := DefaultModelCatalogIDs(PlatformAnthropic)
		require.GreaterOrEqual(t, len(catalog), 2)
		knownPublic := catalog[0]
		knownSingleAccount := catalog[1]
		privateEmail := "alice@example.com"
		privateUnicode := "私有-模型"
		privateLong := strings.Repeat("private-alias-", 20)

		accounts := &radarQuotaAccountListerFake{accounts: []Account{
			radarQuotaAnthropicAccount(1, "max_20x"),
			radarQuotaAnthropicAccount(2, "max_20x"),
		}}
		usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
			1: {FiveHour: radarQuotaProgress(10), SevenDaySonnet: radarQuotaProgress(20)},
			2: {SevenDay: radarQuotaProgress(30), SevenDayFable: radarQuotaProgress(40)},
		}}
		batch := &radarQuotaBatchReaderFake{breakdownResults: []map[int64]map[string]ModelCostStats{
			{
				1: {
					knownPublic:        {Requests: 1, AccountCost: 2},
					knownSingleAccount: {Requests: 1, AccountCost: 3},
					privateEmail:       {Requests: 1, AccountCost: 4},
				},
				2: {
					"  " + strings.ToUpper(knownPublic) + "  ": {Requests: 1, AccountCost: 6},
					privateUnicode: {Requests: 2, AccountCost: 5},
					privateLong:    {Requests: 1, AccountCost: 0},
				},
			},
			{},
		}}
		cache := &radarQuotaCacheFake{}
		aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

		require.NoError(t, aggregator.RunOnce(context.Background()))
		require.Len(t, cache.writes, 1)
		snapshot := cache.writes[0]
		require.Nil(t, snapshot.FiveHour)
		require.Nil(t, snapshot.SevenDay)
		require.Nil(t, snapshot.SevenDaySonnet)
		require.Nil(t, snapshot.SevenDayFable)
		require.Equal(t, []string{"other", knownPublic}, []string{
			snapshot.ModelBreakdown5h[0].Model,
			snapshot.ModelBreakdown5h[1].Model,
		})
		require.InDelta(t, 6, snapshot.ModelBreakdown5h[0].AvgCost, 1e-12)
		require.Equal(t, int64(3), snapshot.ModelBreakdown5h[0].AvgRequests)
		require.InDelta(t, 4, snapshot.ModelBreakdown5h[1].AvgCost, 1e-12)
		require.Equal(t, int64(1), snapshot.ModelBreakdown5h[1].AvgRequests)

		encoded, err := json.Marshal(snapshot)
		require.NoError(t, err)
		for _, forbidden := range []string{knownSingleAccount, privateEmail, privateUnicode, privateLong} {
			require.NotContains(t, string(encoded), forbidden)
		}
	})

	t.Run("openai unsupported plans never reach quota collection or publication", func(t *testing.T) {
		parentID := int64(99)
		accounts := &radarQuotaAccountListerFake{accounts: []Account{
			radarQuotaOpenAIAccount(1, ""),
			radarQuotaOpenAIAccount(2, "   "),
			radarQuotaOpenAIAccount(3, "free"),
			radarQuotaOpenAIAccount(4, " AbNormal "),
			{ID: 5, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
			{ID: 6, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID},
		}}
		usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
			1: {FiveHour: radarQuotaProgress(10)},
			2: {FiveHour: radarQuotaProgress(20)},
		}}
		batch := &radarQuotaBatchReaderFake{}
		cache := &radarQuotaCacheFake{}
		aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

		require.NoError(t, aggregator.RunOnce(context.Background()))
		require.Empty(t, usage.seen)
		require.Empty(t, batch.windowCalls)
		require.Empty(t, batch.breakdownCalls)
		require.Empty(t, cache.writes)
	})

	t.Run("reader cancellation and deadline stop immediately", func(t *testing.T) {
		for _, terminalError := range []error{context.Canceled, context.DeadlineExceeded} {
			accounts := &radarQuotaAccountListerFake{accounts: []Account{
				radarQuotaAnthropicAccount(1, "max_20x"),
				radarQuotaAnthropicAccount(2, "max_20x"),
			}}
			usage := &radarQuotaUsageReaderFake{
				snapshots: map[int64]*UsageInfo{2: {FiveHour: radarQuotaProgress(10)}},
				errors:    map[int64]error{1: terminalError},
			}
			batch := &radarQuotaBatchReaderFake{}
			cache := &radarQuotaCacheFake{}
			aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

			err := aggregator.RunOnce(context.Background())
			require.ErrorIs(t, err, terminalError)
			require.Len(t, usage.seen, 1)
			require.Empty(t, batch.windowCalls)
			require.Empty(t, cache.writes)
		}
	})

	t.Run("account list and batch failures are sanitized and never write partial snapshots", func(t *testing.T) {
		secretError := errors.New("postgres://user:password@private-host")
		accounts := &radarQuotaAccountListerFake{err: secretError}
		aggregator := newRadarQuotaTestAggregator(
			t,
			accounts,
			&radarQuotaUsageReaderFake{},
			&radarQuotaBatchReaderFake{},
			&radarQuotaCacheFake{},
			radarQuotaTestConfig(),
			func() time.Time { return now },
		)
		err := aggregator.RunOnce(context.Background())
		require.ErrorIs(t, err, ErrRadarQuotaAggregation)
		require.NotContains(t, err.Error(), "password")

		batchFailureCases := []struct {
			name               string
			windowErrors       map[int]error
			breakdownErrors    map[int]error
			wantWindowCalls    int
			wantBreakdownCalls int
		}{
			{"5h account stats", map[int]error{0: secretError}, nil, 1, 0},
			{"7d account stats", map[int]error{1: secretError}, nil, 2, 0},
			{"5h model breakdown", nil, map[int]error{0: secretError}, 2, 1},
			{"7d model breakdown", nil, map[int]error{1: secretError}, 2, 2},
		}
		for _, testCase := range batchFailureCases {
			t.Run(testCase.name, func(t *testing.T) {
				accounts := &radarQuotaAccountListerFake{accounts: []Account{
					radarQuotaAnthropicAccount(1, "max_20x"),
					radarQuotaAnthropicAccount(2, "max_20x"),
				}}
				usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
					1: {FiveHour: radarQuotaProgress(10)},
					2: {FiveHour: radarQuotaProgress(10)},
				}}
				batch := &radarQuotaBatchReaderFake{windowErrors: testCase.windowErrors, breakdownErrors: testCase.breakdownErrors}
				cache := &radarQuotaCacheFake{}
				aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

				err := aggregator.RunOnce(context.Background())
				require.ErrorIs(t, err, ErrRadarQuotaAggregation)
				require.NotContains(t, err.Error(), "password")
				require.Len(t, batch.windowCalls, testCase.wantWindowCalls)
				require.Len(t, batch.breakdownCalls, testCase.wantBreakdownCalls)
				require.Empty(t, cache.writes)
			})
		}
	})

	t.Run("write failures continue in deterministic order and return one safe error", func(t *testing.T) {
		accounts := &radarQuotaAccountListerFake{accounts: []Account{
			radarQuotaOpenAIAccount(1, "pro"),
			radarQuotaOpenAIAccount(2, "pro"),
			radarQuotaAnthropicAccount(5, "max_20x"),
			radarQuotaAnthropicAccount(6, "max_20x"),
		}}
		usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
			1: {FiveHour: radarQuotaProgress(10)}, 2: {FiveHour: radarQuotaProgress(10)},
			5: {FiveHour: radarQuotaProgress(10)}, 6: {FiveHour: radarQuotaProgress(10)},
		}}
		batch := &radarQuotaBatchReaderFake{}
		cache := &radarQuotaCacheFake{errors: map[string]error{
			"anthropic/max_20x": errors.New("redis private address one"),
			"openai/pro":        errors.New("redis private address two"),
		}}
		aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

		err := aggregator.RunOnce(context.Background())
		require.ErrorIs(t, err, ErrRadarQuotaAggregation)
		require.NotContains(t, err.Error(), "private address")
		require.Equal(t, []string{"anthropic/max_20x", "openai/pro"}, []string{
			cache.writes[0].BucketKey,
			cache.writes[1].BucketKey,
		})
	})

	t.Run("writer cancellation and deadline propagate immediately", func(t *testing.T) {
		for _, terminalError := range []error{context.Canceled, context.DeadlineExceeded} {
			accounts := &radarQuotaAccountListerFake{accounts: []Account{
				radarQuotaOpenAIAccount(1, "pro"),
				radarQuotaOpenAIAccount(2, "pro"),
				radarQuotaAnthropicAccount(5, "max_20x"),
				radarQuotaAnthropicAccount(6, "max_20x"),
			}}
			usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
				1: {FiveHour: radarQuotaProgress(10)}, 2: {FiveHour: radarQuotaProgress(10)},
				5: {FiveHour: radarQuotaProgress(10)}, 6: {FiveHour: radarQuotaProgress(10)},
			}}
			batch := &radarQuotaBatchReaderFake{}
			cache := &radarQuotaCacheFake{errors: map[string]error{"anthropic/max_20x": terminalError}}
			aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

			err := aggregator.RunOnce(context.Background())
			require.ErrorIs(t, err, terminalError)
			require.Len(t, cache.writes, 1, "terminal writer errors stop meaningless later writes")
		}
	})

	t.Run("model breakdown handles zero totals nonfinite values and stable integer rounding", func(t *testing.T) {
		singleAccountAliases := aggregateRadarModelBreakdown(
			[]radarQuotaBucketAccount{{accountID: 1}, {accountID: 2}},
			map[int64]map[string]ModelCostStats{
				1: {
					"private-one": {Requests: 1},
					"private-two": {Requests: 1},
				},
			},
			PlatformAnthropic,
			2,
		)
		require.Empty(t, singleAccountAliases, "multiple raw aliases from one account are one contributor")
		require.NotNil(t, singleAccountAliases)

		got := aggregateRadarModelBreakdown([]radarQuotaBucketAccount{{accountID: 1}, {accountID: 2}}, map[int64]map[string]ModelCostStats{
			1: {
				"b":       {Requests: 1},
				"a":       {Requests: 2},
				"invalid": {Requests: 1, AccountCost: math.NaN()},
			},
			2: {
				"b": {Requests: 2},
				"a": {Requests: 1},
			},
		}, PlatformAnthropic, 2)
		require.Equal(t, []ModelCostBreakdownDTO{
			{Model: "other", AvgRequests: 4, ContributorsCount: 2},
		}, got)
		for _, model := range got {
			require.False(t, math.IsNaN(model.AvgCost) || math.IsInf(model.AvgCost, 0))
			require.Zero(t, model.Percentage)
		}

		positive := aggregateRadarModelBreakdown([]radarQuotaBucketAccount{{accountID: 1}, {accountID: 2}}, map[int64]map[string]ModelCostStats{
			1: {"large": {Requests: 1, AccountCost: 8}, "small": {Requests: 1, AccountCost: 2}},
			2: {"large": {Requests: 2, AccountCost: 4}, "small": {Requests: 2, AccountCost: 2}},
		}, PlatformAnthropic, 2)
		require.Equal(t, []ModelCostBreakdownDTO{{Model: "other", AvgCost: 8, AvgRequests: 3, Percentage: 100, ContributorsCount: 2}}, positive)
	})
}

func TestRadarQuotaAggregatorRunOnceWithReportProvidesOnlyLowCardinalityCounts(t *testing.T) {
	now := time.Date(2026, time.July, 13, 10, 0, 0, 0, time.UTC)
	accounts := &radarQuotaAccountListerFake{accounts: []Account{
		radarQuotaAnthropicAccount(1, "max_20x"),
		radarQuotaAnthropicAccount(2, "max_20x"),
		radarQuotaAnthropicAccount(3, "solo"),
		radarQuotaOpenAIAccount(4, "pro"),
		radarQuotaOpenAIAccount(5, "pro"),
		radarQuotaOpenAIAccount(6, "unsafe/tier"),
		{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
	}}
	usage := &radarQuotaUsageReaderFake{
		snapshots: map[int64]*UsageInfo{
			1: {FiveHour: radarQuotaProgress(10)},
			2: {FiveHour: radarQuotaProgress(20)},
			3: {FiveHour: radarQuotaProgress(30)},
			5: {},
			6: {FiveHour: radarQuotaProgress(40)},
		},
		errors: map[int64]error{4: errors.New("private account read failure")},
	}
	batch := &radarQuotaBatchReaderFake{
		windowResults: []map[int64]*usagestats.AccountStats{
			{1: {Cost: 10}, 2: {Cost: 20}, 3: {Cost: 30}},
			{},
		},
		breakdownResults: []map[int64]map[string]ModelCostStats{{}, {}},
	}
	cache := &radarQuotaCacheFake{}
	aggregator := newRadarQuotaTestAggregator(t, accounts, usage, batch, cache, radarQuotaTestConfig(), func() time.Time { return now })

	report, err := aggregator.RunOnceWithReport(context.Background())
	require.NoError(t, err)
	require.Equal(t, 7, report.ScannedAccountCount)
	require.Equal(t, 4, report.CandidateAccountCount)
	require.Equal(t, 2, report.UsableAccountCount)
	require.Equal(t, 1, report.BucketCount)
	require.Equal(t, 2, report.SkippedAccountCount)
	require.Equal(t, 0, report.PrivacyFilteredBucketCount)
	require.Empty(t, report.InferenceRejectCounts)
	require.Equal(t, map[string]int{
		radarQuotaSkipUsageReadError: 1,
		radarQuotaSkipInvalidWindow:  1,
	}, report.SkippedAccountCounts)
	require.Equal(t, map[RadarQuotaInferenceMetric]int{
		{Bucket: PlatformAnthropic, Result: "success"}: 1,
	}, report.InferenceCounts)
	require.Len(t, cache.writes, 1)

	// The compatibility entry point must preserve the original behavior.
	require.NoError(t, aggregator.RunOnce(context.Background()))
}

func TestRadarQuotaAggregatorRunOnceWithReportCountsCanonicalInferenceReasons(t *testing.T) {
	accounts := &radarQuotaAccountListerFake{accounts: []Account{
		radarQuotaAnthropicAccount(1, "max_20x"),
		radarQuotaAnthropicAccount(2, "max_20x"),
	}}
	usage := &radarQuotaUsageReaderFake{snapshots: map[int64]*UsageInfo{
		1: {FiveHour: radarQuotaProgress(5)},
		2: {FiveHour: radarQuotaProgress(10)},
	}}
	batch := &radarQuotaBatchReaderFake{
		windowResults:    []map[int64]*usagestats.AccountStats{{}, {}},
		breakdownResults: []map[int64]map[string]ModelCostStats{{}, {}},
	}
	aggregator := newRadarQuotaTestAggregator(
		t,
		accounts,
		usage,
		batch,
		&radarQuotaCacheFake{},
		radarQuotaTestConfig(),
		func() time.Time { return time.Date(2026, time.July, 13, 11, 0, 0, 0, time.UTC) },
	)

	report, err := aggregator.RunOnceWithReport(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[InferenceRejectReason]int{
		InferenceRejectReasonInsufficientSamples: 1,
	}, report.InferenceRejectCounts)
	require.Equal(t, map[RadarQuotaInferenceMetric]int{
		{Bucket: PlatformAnthropic, Result: "rejected", Reason: InferenceRejectReasonInsufficientSamples}: 1,
	}, report.InferenceCounts)
}
