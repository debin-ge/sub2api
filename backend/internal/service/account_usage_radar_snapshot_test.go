package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type radarNoCallAccountRepo struct {
	AccountRepository
	reads  atomic.Int32
	writes atomic.Int32
}

func (r *radarNoCallAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	r.reads.Add(1)
	panic("GetByID must not be called by radar passive snapshot reader")
}

func (r *radarNoCallAccountRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	r.writes.Add(1)
	panic("UpdateExtra must not be called by radar passive snapshot reader")
}

type radarNoCallUsageLogRepo struct {
	UsageLogRepository
	calls atomic.Int32
}

func (r *radarNoCallUsageLogRepo) GetAccountWindowStats(context.Context, int64, time.Time) (*usagestats.AccountStats, error) {
	r.calls.Add(1)
	panic("usage logs must not be queried by radar passive snapshot reader")
}

type radarPanicClaudeUsageFetcher struct{}

func (radarPanicClaudeUsageFetcher) FetchUsage(context.Context, string, string) (*ClaudeUsageResponse, error) {
	panic("Anthropic upstream must not be called by radar passive snapshot reader")
}

func (radarPanicClaudeUsageFetcher) FetchUsageWithOptions(context.Context, *ClaudeUsageFetchOptions) (*ClaudeUsageResponse, error) {
	panic("Anthropic upstream must not be called by radar passive snapshot reader")
}

type radarPanicOpenAIQuotaQuerier struct{}

func (radarPanicOpenAIQuotaQuerier) QueryUsage(context.Context, int64) (*OpenAIQuotaUsage, error) {
	panic("OpenAI quota upstream must not be called by radar passive snapshot reader")
}

type radarPanicAntigravityQuotaFetcher struct{}

func (radarPanicAntigravityQuotaFetcher) CanFetch(*Account) bool {
	panic("Antigravity CanFetch must not be called by radar passive snapshot reader")
}

func (radarPanicAntigravityQuotaFetcher) GetProxyURL(context.Context, *Account) string {
	panic("Antigravity proxy lookup must not be called by radar passive snapshot reader")
}

func (radarPanicAntigravityQuotaFetcher) FetchQuota(context.Context, *Account, string) (*QuotaResult, error) {
	panic("Antigravity upstream must not be called by radar passive snapshot reader")
}

func newRadarSnapshotTestService(now time.Time, retention time.Duration) *AccountUsageService {
	return &AccountUsageService{
		accountRepo:                       &radarNoCallAccountRepo{},
		usageLogRepo:                      &radarNoCallUsageLogRepo{},
		usageFetcher:                      radarPanicClaudeUsageFetcher{},
		antigravityQuotaFetcher:           radarPanicAntigravityQuotaFetcher{},
		openAIQuotaService:                radarPanicOpenAIQuotaQuerier{},
		radarSnapshotHardRetention:        retention,
		radarNow:                          func() time.Time { return now },
		antigravitySnapshotPersistTimeout: time.Second,
	}
}

type radarConstructorAccountRepo struct {
	AccountRepository
	accounts map[int64]*Account
}

func (r *radarConstructorAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	return r.accounts[id], nil
}

func TestNewAccountUsageService_DoesNotStoreTypedNilQuotaDependencies(t *testing.T) {
	t.Parallel()

	repo := &radarConstructorAccountRepo{accounts: map[int64]*Account{}}
	svc := NewAccountUsageService(repo, nil, nil, nil, nil, nil, nil, nil, NewUsageCache(), nil, nil)
	if svc.antigravityQuotaFetcher != nil {
		t.Fatal("nil *AntigravityQuotaFetcher must produce a nil interface")
	}
	if svc.openAIQuotaService != nil {
		t.Fatal("nil *OpenAIQuotaService must produce a nil interface")
	}

	proxyID := int64(9)
	repo.accounts[1] = &Account{
		ID: 1, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive,
		ProxyID: &proxyID, Credentials: map[string]any{"access_token": "must-not-fetch"},
	}
	parentID := int64(10)
	repo.accounts[2] = &Account{
		ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		ParentAccountID: &parentID, QuotaDimension: QuotaDimensionSpark,
	}

	var (
		antigravityUsage *UsageInfo
		openAIUsage      *UsageInfo
		antigravityErr   error
		openAIErr        error
	)
	require.NotPanics(t, func() {
		antigravityUsage, antigravityErr = svc.GetUsage(context.Background(), 1)
		openAIUsage, openAIErr = svc.GetUsage(context.Background(), 2, true)
	})
	require.NoError(t, antigravityErr)
	require.NoError(t, openAIErr)
	require.NotNil(t, antigravityUsage)
	require.NotNil(t, openAIUsage)
}

func TestAccountUsageService_GetRadarUsageSnapshot_IsStrictlyPassive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	sampledAt := now.Add(-5 * time.Minute)
	fiveHourReset := now.Add(2 * time.Hour)
	sevenDayReset := now.Add(5 * 24 * time.Hour)
	parentID := int64(99)

	tests := []struct {
		name    string
		account *Account
		check   func(*testing.T, *UsageInfo)
	}{
		{
			name: "Anthropic OAuth reads account fields only",
			account: &Account{
				ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
				Credentials:      map[string]any{"access_token": "anthropic-secret"},
				SessionWindowEnd: &fiveHourReset,
				Extra: map[string]any{
					"session_window_utilization":      0.42,
					"passive_usage_7d_utilization":    0.18,
					"passive_usage_7d_reset":          sevenDayReset.Unix(),
					"passive_usage_sampled_at":        sampledAt.Format(time.RFC3339),
					"unrelated_sensitive_extra_value": "do-not-copy",
				},
			},
			check: func(t *testing.T, got *UsageInfo) {
				require.Equal(t, 42.0, got.FiveHour.Utilization)
				require.Equal(t, int((2 * time.Hour).Seconds()), got.FiveHour.RemainingSeconds)
				require.Equal(t, 18.0, got.SevenDay.Utilization)
				require.Nil(t, got.FiveHour.WindowStats)
			},
		},
		{
			name: "OpenAI shadow reads its own Extra only",
			account: &Account{
				ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				ParentAccountID: &parentID,
				Credentials:     map[string]any{"access_token": "openai-secret"},
				Extra: map[string]any{
					"codex_5h_used_percent":  33.5,
					"codex_5h_reset_at":      fiveHourReset.Format(time.RFC3339),
					"codex_usage_updated_at": sampledAt.Format(time.RFC3339),
				},
			},
			check: func(t *testing.T, got *UsageInfo) {
				require.Equal(t, 33.5, got.FiveHour.Utilization)
				require.Nil(t, got.SevenDay, "missing 7d must stay nil")
			},
		},
		{
			name: "Antigravity reads dedicated anonymous snapshot only",
			account: &Account{
				ID: 3, Platform: PlatformAntigravity, Type: AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "antigravity-secret", "project_id": "user-project"},
				Extra: map[string]any{
					antigravityRadarSampledAtExtraKey:        sampledAt.Format(time.RFC3339),
					antigravityRadarFiveHourUtilExtraKey:     21.25,
					antigravityRadarFiveHourResetAtExtraKey:  fiveHourReset.Format(time.RFC3339),
					antigravityRadarSubscriptionTierExtraKey: "PRO",
				},
			},
			check: func(t *testing.T, got *UsageInfo) {
				require.Equal(t, 21.25, got.FiveHour.Utilization)
				require.Equal(t, "PRO", got.SubscriptionTier)
				require.Empty(t, got.SubscriptionTierRaw)
				require.Empty(t, got.AntigravityQuota)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := newRadarSnapshotTestService(now, 7*24*time.Hour)
			got, err := svc.GetRadarUsageSnapshot(context.Background(), tt.account)
			require.NoError(t, err)
			require.Equal(t, "passive", got.Source)
			require.NotNil(t, got.UpdatedAt)
			require.Equal(t, sampledAt, *got.UpdatedAt)
			require.NotNil(t, got.FiveHour)
			tt.check(t, got)

			// A returned snapshot is freshly allocated and never aliases Account fields.
			got.FiveHour.Utilization = 99
			require.NotEqual(t, 99.0, parseExtraFloat64(tt.account.Extra["session_window_utilization"]))
			require.Equal(t, int32(0), svc.accountRepo.(*radarNoCallAccountRepo).reads.Load())
			require.Equal(t, int32(0), svc.accountRepo.(*radarNoCallAccountRepo).writes.Load())
			require.Equal(t, int32(0), svc.usageLogRepo.(*radarNoCallUsageLogRepo).calls.Load())
		})
	}
}

func TestAccountUsageService_GetRadarUsageSnapshot_ExpiryRules(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	sampledAt := now.Add(-time.Minute).Format(time.RFC3339)
	expired := now.Add(-time.Hour)

	tests := []struct {
		name             string
		account          *Account
		wantResetCleared bool
	}{
		{
			name: "Anthropic clears an expired reset",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeSetupToken, SessionWindowEnd: &expired, Extra: map[string]any{
				"session_window_utilization": 0.7, "passive_usage_sampled_at": sampledAt,
			}},
			wantResetCleared: true,
		},
		{
			name: "OpenAI retains expired reset metadata but zeroes utilization",
			account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
				"codex_5h_used_percent": 70.0, "codex_5h_reset_at": expired.Format(time.RFC3339), "codex_usage_updated_at": sampledAt,
			}},
		},
		{
			name: "Antigravity retains expired reset metadata but zeroes utilization",
			account: &Account{Platform: PlatformAntigravity, Type: AccountTypeUpstream, Extra: map[string]any{
				antigravityRadarSampledAtExtraKey: sampledAt, antigravityRadarFiveHourUtilExtraKey: 70.0,
				antigravityRadarFiveHourResetAtExtraKey: expired.Format(time.RFC3339), antigravityRadarSubscriptionTierExtraKey: "FREE",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := newRadarSnapshotTestService(now, 7*24*time.Hour).GetRadarUsageSnapshot(context.Background(), tt.account)
			require.NoError(t, err)
			require.NotNil(t, got.FiveHour)
			require.Zero(t, got.FiveHour.Utilization)
			require.Zero(t, got.FiveHour.RemainingSeconds)
			if tt.wantResetCleared {
				require.Nil(t, got.FiveHour.ResetsAt)
			} else {
				require.NotNil(t, got.FiveHour.ResetsAt)
			}
		})
	}
}

func TestAccountUsageService_GetRadarUsageSnapshot_AnthropicDoesNotInventZeroUtilization(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	sampledAt := now.Add(-time.Minute).Format(time.RFC3339)
	resetAt := now.Add(time.Hour)

	tests := []struct {
		name    string
		account *Account
		check   func(*testing.T, *UsageInfo, error)
	}{
		{
			name: "SessionWindowEnd only is unavailable",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowEnd: &resetAt, Extra: map[string]any{
				"passive_usage_sampled_at": sampledAt,
			}},
		},
		{
			name: "allowed status without utilization is unavailable",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowEnd: &resetAt, SessionWindowStatus: "allowed", Extra: map[string]any{
				"passive_usage_sampled_at": sampledAt,
			}},
		},
		{
			name: "unknown status without utilization is unavailable",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowEnd: &resetAt, SessionWindowStatus: "mystery", Extra: map[string]any{
				"passive_usage_sampled_at": sampledAt,
			}},
		},
		{
			name: "7d reset only is unavailable",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{
				"passive_usage_sampled_at": sampledAt,
				"passive_usage_7d_reset":   resetAt.Unix(),
			}},
		},
		{
			name: "Fable reset only is unavailable",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{
				"passive_usage_sampled_at":  sampledAt,
				"passive_usage_7d_oi_reset": resetAt.Unix(),
			}},
		},
		{
			name: "allowed warning remains an explicit utilization signal",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowEnd: &resetAt, SessionWindowStatus: "allowed_warning", Extra: map[string]any{
				"passive_usage_sampled_at": sampledAt,
			}},
			check: func(t *testing.T, got *UsageInfo, err error) {
				require.NoError(t, err)
				require.NotNil(t, got.FiveHour)
				require.Equal(t, 80.0, got.FiveHour.Utilization)
			},
		},
		{
			name: "invalid 5h does not hide a valid 7d window",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowEnd: &resetAt, SessionWindowStatus: "allowed", Extra: map[string]any{
				"passive_usage_sampled_at":     sampledAt,
				"passive_usage_7d_utilization": 0.25,
				"passive_usage_7d_reset":       resetAt.Unix(),
			}},
			check: func(t *testing.T, got *UsageInfo, err error) {
				require.NoError(t, err)
				require.Nil(t, got.FiveHour)
				require.NotNil(t, got.SevenDay)
				require.Equal(t, 25.0, got.SevenDay.Utilization)
			},
		},
		{
			name: "7d reset only stays nil when 5h is valid",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowEnd: &resetAt, Extra: map[string]any{
				"passive_usage_sampled_at":   sampledAt,
				"session_window_utilization": 0.4,
				"passive_usage_7d_reset":     resetAt.Unix(),
			}},
			check: func(t *testing.T, got *UsageInfo, err error) {
				require.NoError(t, err)
				require.NotNil(t, got.FiveHour)
				require.Equal(t, 40.0, got.FiveHour.Utilization)
				require.Nil(t, got.SevenDay)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := newRadarSnapshotTestService(now, 7*24*time.Hour).GetRadarUsageSnapshot(context.Background(), tt.account)
			if tt.check != nil {
				tt.check(t, got, err)
				return
			}
			require.Nil(t, got)
			require.ErrorIs(t, err, ErrRadarUsageSnapshotUnavailable)
		})
	}
}

func TestAccountUsageService_GetRadarUsageSnapshot_RejectsUnavailableSnapshots(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	validReset := now.Add(time.Hour).Format(time.RFC3339)
	validSample := now.Add(-time.Minute).Format(time.RFC3339)

	openAIAccount := func(sample any) *Account {
		return &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "must-not-leak"}, Extra: map[string]any{
			"codex_usage_updated_at": sample, "codex_5h_used_percent": 10.0, "codex_5h_reset_at": validReset,
		}}
	}

	tests := []struct {
		name    string
		account *Account
	}{
		{name: "nil account", account: nil},
		{name: "missing sampled_at", account: openAIAccount(nil)},
		{name: "corrupt sampled_at", account: openAIAccount("not-a-time")},
		{name: "future sampled_at", account: openAIAccount(now.Add(time.Second).Format(time.RFC3339))},
		{name: "stale sampled_at", account: openAIAccount(now.Add(-7*24*time.Hour - time.Second).Format(time.RFC3339))},
		{name: "unsupported platform", account: &Account{Platform: PlatformGemini, Type: AccountTypeOAuth, Extra: map[string]any{"codex_usage_updated_at": validSample}}},
		{name: "unsupported Anthropic type", account: &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Extra: map[string]any{"passive_usage_sampled_at": validSample}}},
		{name: "unsupported OpenAI type", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"codex_usage_updated_at": validSample}}},
		{name: "unsupported Antigravity type", account: &Account{Platform: PlatformAntigravity, Type: AccountTypeAPIKey, Extra: map[string]any{antigravityRadarSampledAtExtraKey: validSample}}},
		{name: "no usable window", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"codex_usage_updated_at": validSample}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := newRadarSnapshotTestService(now, 7*24*time.Hour).GetRadarUsageSnapshot(context.Background(), tt.account)
			require.Nil(t, got)
			require.ErrorIs(t, err, ErrRadarUsageSnapshotUnavailable)
			require.NotContains(t, err.Error(), "must-not-leak")
		})
	}
}

func TestAccountUsageService_GetRadarUsageSnapshot_NormalizesRFC3339Offsets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	svc := newRadarSnapshotTestService(now, 7*24*time.Hour)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
		"codex_usage_updated_at": "2026-07-13T15:59:00+08:00",
		"codex_5h_used_percent":  10.0,
		"codex_5h_reset_at":      "2026-07-13T18:00:00+08:00",
	}}

	got, err := svc.GetRadarUsageSnapshot(context.Background(), account)

	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 13, 7, 59, 0, 0, time.UTC), *got.UpdatedAt)
	require.Equal(t, time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC), *got.FiveHour.ResetsAt)
	require.Equal(t, 2*60*60, got.FiveHour.RemainingSeconds)
}

func TestAccountUsageService_GetRadarUsageSnapshot_UsesSevenDayWhenFiveHourIsUnavailable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
		"codex_usage_updated_at":       now.Add(-time.Minute).Format(time.RFC3339),
		openAICodex5hAvailableExtraKey: false,
		openAICodex7dAvailableExtraKey: true,
		"codex_5h_used_percent":        81.0,
		"codex_7d_used_percent":        24.0,
		"codex_7d_reset_at":            now.Add(4 * 24 * time.Hour).Format(time.RFC3339),
	}}

	got, err := newRadarSnapshotTestService(now, 7*24*time.Hour).GetRadarUsageSnapshot(context.Background(), account)

	require.NoError(t, err)
	require.Nil(t, got.FiveHour, "legacy 5h data must not survive an explicit upstream removal")
	require.NotNil(t, got.SevenDay)
	require.Equal(t, 24.0, got.SevenDay.Utilization)
}

func TestAccountUsageService_GetRadarUsageSnapshot_PropagatesContextErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
		"codex_usage_updated_at": now.Format(time.RFC3339), "codex_5h_used_percent": 10.0,
	}}
	svc := newRadarSnapshotTestService(now, 7*24*time.Hour)

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := svc.GetRadarUsageSnapshot(canceledCtx, account)
	require.Nil(t, got)
	require.ErrorIs(t, err, context.Canceled)

	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	got, err = svc.GetRadarUsageSnapshot(deadlineCtx, account)
	require.Nil(t, got)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestAccountUsageService_GetRadarUsageSnapshot_RejectsMalformedUtilization(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	sampledAt := now.Add(-time.Minute).Format(time.RFC3339)
	resetAt := now.Add(time.Hour)

	tests := []struct {
		name    string
		account func(any) *Account
	}{
		{
			name: "Anthropic",
			account: func(util any) *Account {
				return &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowEnd: &resetAt, Extra: map[string]any{
					"passive_usage_sampled_at": sampledAt, "session_window_utilization": util,
				}}
			},
		},
		{
			name: "OpenAI",
			account: func(util any) *Account {
				return &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
					"codex_usage_updated_at": sampledAt, "codex_5h_used_percent": util, "codex_5h_reset_at": resetAt.Format(time.RFC3339),
				}}
			},
		},
		{
			name: "Antigravity",
			account: func(util any) *Account {
				return &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth, Extra: map[string]any{
					antigravityRadarSampledAtExtraKey: sampledAt, antigravityRadarFiveHourUtilExtraKey: util,
					antigravityRadarFiveHourResetAtExtraKey:  resetAt.Format(time.RFC3339),
					antigravityRadarSubscriptionTierExtraKey: "PRO",
				}}
			},
		},
	}

	badValues := []any{"broken", math.NaN(), math.Inf(1), -0.01, 100.01}
	for _, tt := range tests {
		for _, bad := range badValues {
			bad := bad
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				account := tt.account(bad)
				if tt.name == "Antigravity" {
					require.Equal(t, "PRO", account.Extra[antigravityRadarSubscriptionTierExtraKey])
					require.NotNil(t, account.Extra[antigravityRadarFiveHourResetAtExtraKey])
				}
				if tt.name == "Anthropic" && finiteFloat(bad) > 1 {
					// Anthropic stores utilization as a 0..1 fraction.
					account.Extra["session_window_utilization"] = 1.01
				}
				got, err := newRadarSnapshotTestService(now, 7*24*time.Hour).GetRadarUsageSnapshot(context.Background(), account)
				require.Nil(t, got)
				require.ErrorIs(t, err, ErrRadarUsageSnapshotUnavailable)
			})
		}
	}
}

func finiteFloat(value any) float64 {
	valueFloat, _ := value.(float64)
	if math.IsNaN(valueFloat) || math.IsInf(valueFloat, 0) {
		return 0
	}
	return valueFloat
}

type radarAntigravityFetcherStub struct {
	result *QuotaResult
	err    error
	calls  atomic.Int32
}

func (f *radarAntigravityFetcherStub) CanFetch(*Account) bool { return true }
func (f *radarAntigravityFetcherStub) GetProxyURL(context.Context, *Account) string {
	return ""
}
func (f *radarAntigravityFetcherStub) FetchQuota(context.Context, *Account, string) (*QuotaResult, error) {
	f.calls.Add(1)
	return f.result, f.err
}

type radarSnapshotPersistRepo struct {
	AccountRepository

	mu        sync.Mutex
	account   *Account
	updatesCh chan map[string]any
	err       error
	deadline  time.Time
}

func (r *radarSnapshotPersistRepo) UpdateExtra(ctx context.Context, accountID int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		r.deadline = deadline
	}
	copyUpdates := make(map[string]any, len(updates))
	for key, value := range updates {
		copyUpdates[key] = value
	}
	if r.account != nil && r.account.ID == accountID && r.err == nil {
		if r.account.Extra == nil {
			r.account.Extra = make(map[string]any)
		}
		for key, value := range copyUpdates {
			r.account.Extra[key] = value
		}
	}
	if r.updatesCh != nil {
		r.updatesCh <- copyUpdates
	}
	return r.err
}

func TestAccountUsageService_AntigravitySuccessPersistsMinimalRadarSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	account := &Account{ID: 77, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "ag-secret-token", "project_id": "raw-user-project",
	}}
	repo := &radarSnapshotPersistRepo{account: account, updatesCh: make(chan map[string]any, 1)}
	fetcher := &radarAntigravityFetcherStub{result: &QuotaResult{UsageInfo: &UsageInfo{
		UpdatedAt:           &now,
		FiveHour:            &UsageProgress{Utilization: 37.5, ResetsAt: &resetAt},
		SubscriptionTier:    "PRO",
		SubscriptionTierRaw: "g1-ultra-secret-raw-tier",
		AntigravityQuota:    map[string]*AntigravityModelQuota{"raw-model-id": {Utilization: 1}},
	}}}
	svc := &AccountUsageService{
		accountRepo:                       repo,
		antigravityQuotaFetcher:           fetcher,
		cache:                             NewUsageCache(),
		radarSnapshotHardRetention:        7 * 24 * time.Hour,
		radarNow:                          func() time.Time { return now },
		antigravitySnapshotPersistTimeout: time.Second,
	}

	active, err := svc.getAntigravityUsage(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 37.5, active.FiveHour.Utilization)

	var updates map[string]any
	select {
	case updates = <-repo.updatesCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Antigravity Radar snapshot persistence")
	}
	require.Len(t, updates, 4)
	require.Equal(t, now.Format(time.RFC3339), updates[antigravityRadarSampledAtExtraKey])
	require.Equal(t, 37.5, updates[antigravityRadarFiveHourUtilExtraKey])
	require.Equal(t, resetAt.Format(time.RFC3339), updates[antigravityRadarFiveHourResetAtExtraKey])
	require.Equal(t, "PRO", updates[antigravityRadarSubscriptionTierExtraKey])
	encoded, marshalErr := json.Marshal(updates)
	require.NoError(t, marshalErr)
	serialized := string(encoded)
	for _, sensitive := range []string{"ag-secret-token", "raw-user-project", "raw-model-id", "g1-ultra-secret-raw-tier", "account_id", "user_id", "error"} {
		require.NotContains(t, serialized, sensitive)
	}
	repo.mu.Lock()
	require.False(t, repo.deadline.IsZero(), "persistence context must have a deadline")
	require.LessOrEqual(t, repo.deadline.Sub(time.Now()), time.Second)
	repo.mu.Unlock()

	passive, err := svc.GetRadarUsageSnapshot(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "passive", passive.Source)
	require.Equal(t, 37.5, passive.FiveHour.Utilization)
	require.Equal(t, "PRO", passive.SubscriptionTier)
}

func TestAccountUsageService_AntigravityPersistenceFailureDoesNotFailActiveUsage(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	resetAt := now.Add(time.Hour)
	repo := &radarSnapshotPersistRepo{err: errors.New("write failed"), updatesCh: make(chan map[string]any, 1)}
	fetcher := &radarAntigravityFetcherStub{result: &QuotaResult{UsageInfo: &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 12, ResetsAt: &resetAt}, SubscriptionTier: "FREE",
	}}}
	svc := &AccountUsageService{
		accountRepo: repo, antigravityQuotaFetcher: fetcher, cache: NewUsageCache(),
		radarNow: func() time.Time { return now }, antigravitySnapshotPersistTimeout: time.Second,
	}

	active, err := svc.getAntigravityUsage(context.Background(), &Account{ID: 88, Platform: PlatformAntigravity})
	require.NoError(t, err)
	require.Equal(t, 12.0, active.FiveHour.Utilization)
	select {
	case <-repo.updatesCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected best-effort persistence attempt")
	}
}

type radarBlockingSnapshotPersistRepo struct {
	AccountRepository
	started chan struct{}
	done    chan error
}

func (r *radarBlockingSnapshotPersistRepo) UpdateExtra(ctx context.Context, _ int64, _ map[string]any) error {
	close(r.started)
	<-ctx.Done()
	err := ctx.Err()
	r.done <- err
	return err
}

func TestAccountUsageService_AntigravityPersistenceIsActuallyTimeBounded(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	resetAt := now.Add(time.Hour)
	repo := &radarBlockingSnapshotPersistRepo{started: make(chan struct{}), done: make(chan error, 1)}
	fetcher := &radarAntigravityFetcherStub{result: &QuotaResult{UsageInfo: &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 12, ResetsAt: &resetAt}, SubscriptionTier: "FREE",
	}}}
	svc := &AccountUsageService{
		accountRepo: repo, antigravityQuotaFetcher: fetcher, cache: NewUsageCache(),
		radarNow: func() time.Time { return now }, antigravitySnapshotPersistTimeout: 200 * time.Millisecond,
	}

	active, err := svc.getAntigravityUsage(context.Background(), &Account{ID: 89, Platform: PlatformAntigravity})
	require.NoError(t, err)
	require.Equal(t, 12.0, active.FiveHour.Utilization)
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("background UpdateExtra did not start")
	}
	select {
	case <-repo.done:
		t.Fatal("active response waited until persistence completed")
	default:
	}

	select {
	case persistErr := <-repo.done:
		require.ErrorIs(t, persistErr, context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("blocking UpdateExtra did not terminate at the configured persistence timeout")
	}
}

func TestAccountUsageService_AntigravityDoesNotOverwriteLastGoodSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name   string
		result *QuotaResult
		err    error
	}{
		{name: "degraded fetch", err: errors.New("network failed")},
		{name: "forbidden", result: &QuotaResult{UsageInfo: &UsageInfo{IsForbidden: true, ErrorCode: errorCodeForbidden}}},
		{name: "nil five hour", result: &QuotaResult{UsageInfo: &UsageInfo{SubscriptionTier: "PRO"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := &radarSnapshotPersistRepo{updatesCh: make(chan map[string]any, 1)}
			svc := &AccountUsageService{
				accountRepo:             repo,
				antigravityQuotaFetcher: &radarAntigravityFetcherStub{result: tt.result, err: tt.err},
				cache:                   NewUsageCache(), radarNow: func() time.Time { return now },
				antigravitySnapshotPersistTimeout: time.Second,
			}
			_, err := svc.getAntigravityUsage(context.Background(), &Account{ID: 90, Platform: PlatformAntigravity})
			require.NoError(t, err)
			select {
			case updates := <-repo.updatesCh:
				t.Fatalf("must not overwrite last successful snapshot, got updates: %#v", updates)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}

func TestAccountUsageService_AntigravityDoesNotPersistMissingOrInvalidTier(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	resetAt := now.Add(time.Hour)
	for _, tt := range []struct {
		name string
		tier string
	}{
		{name: "missing", tier: ""},
		{name: "invalid", tier: "enterprise"},
		{name: "pro substring", tier: "not-pro"},
		{name: "free substring", tier: "free-form"},
		{name: "ultra substring", tier: "invalid-ultra-value"},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := &radarSnapshotPersistRepo{updatesCh: make(chan map[string]any, 1)}
			svc := &AccountUsageService{
				accountRepo: repo,
				antigravityQuotaFetcher: &radarAntigravityFetcherStub{result: &QuotaResult{UsageInfo: &UsageInfo{
					FiveHour: &UsageProgress{Utilization: 15, ResetsAt: &resetAt}, SubscriptionTier: tt.tier,
				}}},
				cache: NewUsageCache(), radarNow: func() time.Time { return now },
				antigravitySnapshotPersistTimeout: time.Second,
			}

			active, err := svc.getAntigravityUsage(context.Background(), &Account{ID: 91, Platform: PlatformAntigravity})
			require.NoError(t, err)
			require.Equal(t, 15.0, active.FiveHour.Utilization)
			select {
			case updates := <-repo.updatesCh:
				t.Fatalf("missing/invalid tier must not overwrite the last snapshot, got %#v", updates)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}

func TestAccountUsageService_AntigravityPersistsExplicitUnknownTier(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	resetAt := now.Add(time.Hour)
	repo := &radarSnapshotPersistRepo{updatesCh: make(chan map[string]any, 1)}
	svc := &AccountUsageService{
		accountRepo: repo,
		antigravityQuotaFetcher: &radarAntigravityFetcherStub{result: &QuotaResult{UsageInfo: &UsageInfo{
			FiveHour: &UsageProgress{Utilization: 15, ResetsAt: &resetAt}, SubscriptionTier: "UNKNOWN",
		}}},
		cache: NewUsageCache(), radarNow: func() time.Time { return now },
		antigravitySnapshotPersistTimeout: time.Second,
	}

	_, err := svc.getAntigravityUsage(context.Background(), &Account{ID: 92, Platform: PlatformAntigravity})
	require.NoError(t, err)
	select {
	case updates := <-repo.updatesCh:
		require.Equal(t, "UNKNOWN", updates[antigravityRadarSubscriptionTierExtraKey])
	case <-time.After(time.Second):
		t.Fatal("explicit UNKNOWN tier should be persisted")
	}
}

func TestProvideAccountUsageService_UsesConfiguredRadarHardRetention(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Radar: config.RadarConfig{SourceHardRetentionDays: 11}}
	svc := ProvideAccountUsageService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg)
	require.Equal(t, 11*24*time.Hour, svc.radarSnapshotHardRetention)
	require.NotNil(t, svc.radarNow)
	var reader RadarUsageSnapshotReader = svc
	require.Same(t, svc, reader)
}

func TestAntigravityRadarExtraKeysDoNotContainSensitiveTerms(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		antigravityRadarSampledAtExtraKey,
		antigravityRadarFiveHourUtilExtraKey,
		antigravityRadarFiveHourResetAtExtraKey,
		antigravityRadarSubscriptionTierExtraKey,
	} {
		lower := strings.ToLower(key)
		for _, forbidden := range []string{"token", "account_id", "user_id", "raw", "error", "payload"} {
			require.NotContains(t, lower, forbidden)
		}
	}
}
