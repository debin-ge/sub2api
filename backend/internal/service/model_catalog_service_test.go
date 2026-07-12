package service

import (
	"context"
	"errors"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type modelDiscovererFunc func(context.Context, *Account) ([]string, error)

func (f modelDiscovererFunc) Discover(ctx context.Context, account *Account) ([]string, error) {
	return f(ctx, account)
}

type recordingModelDiscoverer struct {
	mu     sync.Mutex
	models map[int64][]string
	errs   map[int64]error
	calls  map[int64]int
}

func (d *recordingModelDiscoverer) Discover(_ context.Context, account *Account) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.calls == nil {
		d.calls = make(map[int64]int)
	}
	d.calls[account.ID]++
	return append([]string(nil), d.models[account.ID]...), d.errs[account.ID]
}

type modelCatalogAccountRepoStub struct {
	AccountRepository
	byGroup map[int64][]Account
	all     []Account
}

type modelCatalogGroupRepoStub struct {
	GroupRepository
	groups []Group
}

type modelCatalogChannelRepoStub struct {
	ChannelRepository
	channels  []Channel
	platforms map[int64]string
}

func (s *modelCatalogAccountRepoStub) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]Account, error) {
	accounts := s.byGroup[groupID]
	result := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account.Platform == platform {
			result = append(result, account)
		}
	}
	return result, nil
}

func (s *modelCatalogAccountRepoStub) ListSchedulableByPlatform(_ context.Context, platform string) ([]Account, error) {
	result := make([]Account, 0, len(s.all))
	for _, account := range s.all {
		if account.Platform == platform {
			result = append(result, account)
		}
	}
	return result, nil
}

func (s *modelCatalogGroupRepoStub) ListActive(context.Context) ([]Group, error) {
	return append([]Group(nil), s.groups...), nil
}

func (s *modelCatalogGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	for i := range s.groups {
		if s.groups[i].ID == id {
			group := s.groups[i]
			return &group, nil
		}
	}
	return nil, ErrGroupNotFound
}

func (s *modelCatalogChannelRepoStub) ListAll(context.Context) ([]Channel, error) {
	return append([]Channel(nil), s.channels...), nil
}

func (s *modelCatalogChannelRepoStub) GetGroupPlatforms(_ context.Context, _ []int64) (map[int64]string, error) {
	return maps.Clone(s.platforms), nil
}

func TestModelCatalogStatsLowCardinality(t *testing.T) {
	resetModelCatalogStatsForTest()

	recordModelCatalogRefresh(PlatformOpenAI, true)
	recordModelCatalogRefresh("custom-platform-1", true)
	recordModelCatalogRefresh("custom-platform-2", false)
	recordModelCatalogCache(catalogCacheFresh)
	recordModelCatalogCache(catalogCacheStale)
	recordModelCatalogCache(catalogCacheMiss)
	recordModelCatalogFallback(PlatformOpenAI, modelCatalogFallbackUpstreamError)
	recordModelCatalogFallback("custom-platform-3", "custom-reason-1")
	recordModelCatalogFallback("custom-platform-4", "custom-reason-2")

	stats := ModelCatalogStats()
	require.Equal(t, int64(1), stats.CacheFresh)
	require.Equal(t, int64(1), stats.CacheStale)
	require.Equal(t, int64(1), stats.CacheMiss)
	require.Len(t, stats.ByPlatform, 12)
	require.Equal(t, int64(1), stats.ByPlatform[PlatformOpenAI].RefreshSuccess)
	require.Equal(t, int64(1), stats.ByPlatform[PlatformOpenAI].FallbackByReason[modelCatalogFallbackUpstreamError])
	require.Equal(t, int64(1), stats.ByPlatform[modelCatalogPlatformUnknown].RefreshSuccess)
	require.Equal(t, int64(1), stats.ByPlatform[modelCatalogPlatformUnknown].RefreshFailure)
	require.Equal(t, int64(2), stats.ByPlatform[modelCatalogPlatformUnknown].FallbackByReason[modelCatalogFallbackOther])
	require.NotContains(t, stats.ByPlatform, "custom-platform-1")
	require.NotContains(t, stats.ByPlatform, "custom-platform-4")
}

func TestModelCatalogStatsReturnsImmutableSnapshots(t *testing.T) {
	resetModelCatalogStatsForTest()
	recordModelCatalogRefresh(PlatformOpenAI, true)
	recordModelCatalogFallback(PlatformOpenAI, modelCatalogFallbackEmpty)

	first := ModelCatalogStats()
	first.ByPlatform[PlatformOpenAI].FallbackByReason[modelCatalogFallbackEmpty] = 99
	delete(first.ByPlatform, PlatformOpenAI)
	first.ByPlatform["injected"] = ModelCatalogPlatformRuntimeStats{RefreshSuccess: 99}

	second := ModelCatalogStats()
	require.Equal(t, int64(1), second.ByPlatform[PlatformOpenAI].RefreshSuccess)
	require.Equal(t, int64(1), second.ByPlatform[PlatformOpenAI].FallbackByReason[modelCatalogFallbackEmpty])
	require.NotContains(t, second.ByPlatform, "injected")
}

func TestModelCatalogStaticPolicyDoesNotRecordUnsupportedFallback(t *testing.T) {
	resetModelCatalogStatsForTest()
	catalog := NewModelCatalogService(nil, nil, nil, nil, config.ModelCatalogConfig{RequestTimeoutSeconds: 10})
	accounts := []*Account{
		{
			ID: 31, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Credentials: map[string]any{"model_mapping": map[string]any{"client-model": "upstream-model"}},
		},
		{ID: 32, Platform: PlatformAnthropic, Type: AccountTypeBedrock},
		{ID: 33, Platform: PlatformAnthropic, Type: AccountTypeServiceAccount},
		{ID: 34, Platform: PlatformGrok, Type: AccountTypeOAuth},
	}

	for _, account := range accounts {
		models, err := catalog.ListForAccount(context.Background(), account, true)
		require.NoError(t, err)
		require.NotEmpty(t, models)
	}

	stats := ModelCatalogStats()
	for platform, platformStats := range stats.ByPlatform {
		for reason, count := range platformStats.FallbackByReason {
			require.Zero(t, count, "static policy recorded fallback platform=%s reason=%s", platform, reason)
		}
	}
}

func TestModelCatalogOperationalPathsRecordStats(t *testing.T) {
	resetModelCatalogStatsForTest()
	discoverer := modelDiscovererFunc(func(_ context.Context, account *Account) ([]string, error) {
		if account.ID == 1 {
			return []string{"gpt-live-new"}, nil
		}
		return nil, newUpstreamModelSyncUpstreamError("upstream unavailable", errors.New("dial failed"))
	})
	catalog := &ModelCatalogService{
		discoverer: discoverer,
		cfg: config.ModelCatalogConfig{
			RefreshIntervalSeconds: 300, RequestTimeoutSeconds: 10,
			StaleTTLSeconds: 86400, FailureBackoffSeconds: 60, MaxConcurrency: 5,
		},
		cache: newModelCatalogCache(), refreshSem: make(chan struct{}, 5), now: time.Now,
	}
	for _, account := range []*Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
	} {
		models, err := catalog.ListForAccount(context.Background(), account, true)
		require.NoError(t, err)
		require.NotEmpty(t, models)
	}

	stats := ModelCatalogStats()
	require.Equal(t, int64(2), stats.CacheMiss)
	require.Equal(t, int64(1), stats.ByPlatform[PlatformOpenAI].RefreshSuccess)
	require.Equal(t, int64(1), stats.ByPlatform[PlatformOpenAI].RefreshFailure)
	require.Equal(t, int64(1), stats.ByPlatform[PlatformOpenAI].FallbackByReason[modelCatalogFallbackUpstreamError])
}

func TestModelCatalogOperationalPathsRecordCacheStatesAndFallbackReasons(t *testing.T) {
	resetModelCatalogStatsForTest()
	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	cache := newModelCatalogCache()
	cache.storeSuccess(1, []string{"fresh"}, now)
	cache.storeSuccess(2, []string{"stale"}, now.Add(-10*time.Minute))
	cache.storeSuccess(3, []string{"expired"}, now.Add(-25*time.Hour))
	catalog := &ModelCatalogService{
		discoverer: modelDiscovererFunc(func(_ context.Context, account *Account) ([]string, error) {
			if account.ID == 4 {
				return []string{" ", ""}, nil
			}
			return []string{"live"}, nil
		}),
		cfg: config.ModelCatalogConfig{
			RefreshIntervalSeconds: 300, RequestTimeoutSeconds: 10,
			StaleTTLSeconds: 86400, FailureBackoffSeconds: 60, MaxConcurrency: 5,
		},
		cache: cache, refreshSem: make(chan struct{}, 5), now: func() time.Time { return now },
	}

	for _, account := range []*Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
	} {
		models, err := catalog.ListForAccount(context.Background(), account, true)
		require.NoError(t, err)
		require.NotEmpty(t, models)
	}
	models, err := catalog.ListForAccount(context.Background(), &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, false)
	require.NoError(t, err)
	require.NotEmpty(t, models)
	models, err = catalog.ListForAccount(context.Background(), &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, true)
	require.NoError(t, err)
	require.NotEmpty(t, models)
	models, err = catalog.ListForAccount(context.Background(), &Account{ID: 5, Platform: PlatformGrok, Type: AccountTypeOAuth}, true)
	require.NoError(t, err)
	require.NotEmpty(t, models)

	stats := ModelCatalogStats()
	require.Equal(t, int64(1), stats.CacheFresh)
	require.Equal(t, int64(1), stats.CacheStale)
	require.Equal(t, int64(2), stats.CacheMiss)
	require.Equal(t, int64(1), stats.ByPlatform[PlatformOpenAI].FallbackByReason[modelCatalogFallbackExpired])
	require.Equal(t, int64(1), stats.ByPlatform[PlatformOpenAI].FallbackByReason[modelCatalogFallbackEmpty])
	require.Zero(t, stats.ByPlatform[PlatformGrok].FallbackByReason[modelCatalogFallbackUnsupported])
}

func TestModelCatalogListForAccount(t *testing.T) {
	tests := []struct {
		name       string
		account    Account
		liveModels []string
		liveErr    error
		want       []string
		wantCalls  int
	}{
		{name: "oauth live", account: Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, liveModels: []string{"gpt-new"}, want: []string{"gpt-new"}, wantCalls: 1},
		{name: "apikey passthrough live", account: Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_passthrough": true}}, liveModels: []string{"gpt-new"}, want: []string{"gpt-new"}, wantCalls: 1},
		{name: "apikey mapping sources", account: Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"alias-b": "upstream-b", "alias-a": "upstream-a"}}}, want: []string{"alias-a", "alias-b"}},
		{name: "legacy whitelist", account: Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_whitelist": []any{"gpt-5.5"}}}, want: []string{"gpt-5.5"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discoverer := &recordingModelDiscoverer{
				models: map[int64][]string{tt.account.ID: tt.liveModels},
				errs:   map[int64]error{tt.account.ID: tt.liveErr},
			}
			catalog := &ModelCatalogService{
				discoverer: discoverer,
				cfg:        config.ModelCatalogConfig{RequestTimeoutSeconds: 10},
				now:        time.Now,
			}
			got, err := catalog.ListForAccount(context.Background(), &tt.account, true)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantCalls, discoverer.calls[tt.account.ID])
		})
	}
}

func TestModelCatalogConfiguredModels(t *testing.T) {
	discoverer := &recordingModelDiscoverer{
		models: map[int64][]string{},
		errs:   map[int64]error{},
	}
	resolve := func(account Account) []string {
		catalog := &ModelCatalogService{
			discoverer: discoverer,
			cfg: config.ModelCatalogConfig{
				RequestTimeoutSeconds: 10,
			},
			now: time.Now,
		}
		ids, err := catalog.ListForAccount(context.Background(), &account, true)
		require.NoError(t, err)
		return ids
	}

	require.Equal(t, DefaultModelCatalogIDs(PlatformOpenAI), resolve(Account{ID: 5, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}))
	require.Equal(t, []string{"client-name"}, resolve(Account{ID: 6, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{"client-name": "upstream-name"}}}))

	wildcard := resolve(Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeBedrock,
		Credentials: map[string]any{"model_whitelist": []any{"claude-sonnet-*"}}})
	require.NotEmpty(t, wildcard)
	for _, id := range wildcard {
		require.NotContains(t, id, "*")
	}

	require.Equal(t, DefaultModelCatalogIDs(PlatformAnthropic), resolve(Account{
		ID: 8, Platform: PlatformAnthropic, Type: AccountTypeServiceAccount,
	}))

	t.Run("mapping takes precedence over legacy whitelist", func(t *testing.T) {
		got := resolve(Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"model_mapping":   map[string]string{"client-model": "upstream-model"},
			"model_whitelist": []string{"legacy-model"},
		}})
		require.Equal(t, []string{"client-model"}, got)
	})

	t.Run("unsupported account type uses explicit restrictions", func(t *testing.T) {
		got := resolve(Account{ID: 10, Platform: PlatformGemini, Type: "unsupported", Credentials: map[string]any{
			"model_whitelist": []string{" gemini-custom ", "GEMINI-CUSTOM"},
		}})
		require.Equal(t, []string{"gemini-custom"}, got)
	})
}

func TestModelCatalogWildcard(t *testing.T) {
	candidates := []string{"claude-sonnet-4", "claude-opus-4", "Claude-Sonnet-3"}

	require.Equal(t,
		[]string{"Claude-Sonnet-3", "claude-sonnet-4", "custom-model"},
		expandModelPatterns([]string{" claude-sonnet-* ", "custom-model"}, candidates),
	)
	require.Empty(t, expandModelPatterns([]string{"missing-*"}, candidates))
}

func TestModelCatalogLivePolicies(t *testing.T) {
	tests := []struct {
		name    string
		account Account
	}{
		{name: "setup token", account: Account{ID: 11, Platform: PlatformAnthropic, Type: AccountTypeSetupToken}},
		{name: "antigravity upstream", account: Account{ID: 12, Platform: PlatformAntigravity, Type: AccountTypeUpstream}},
		{name: "anthropic passthrough", account: Account{ID: 13, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Extra: map[string]any{"anthropic_passthrough": true}}},
		{name: "windsurf", account: Account{ID: 14, Platform: PlatformWindsurf, Type: AccountTypeAPIKey}},
		{name: "opencode", account: Account{ID: 15, Platform: PlatformOpenCode, Type: AccountTypeAPIKey}},
		{name: "anthropic oauth", account: Account{ID: 20, Platform: PlatformAnthropic, Type: AccountTypeOAuth}},
		{name: "antigravity oauth", account: Account{ID: 21, Platform: PlatformAntigravity, Type: AccountTypeOAuth}},
		{name: "gemini ai studio oauth", account: Account{ID: 22, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"oauth_type": "ai_studio"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discoverer := &recordingModelDiscoverer{models: map[int64][]string{tt.account.ID: {"live-model"}}, errs: map[int64]error{}}
			catalog := NewModelCatalogService(nil, nil, nil, discoverer, config.ModelCatalogConfig{RequestTimeoutSeconds: 10})

			got, err := catalog.ListForAccount(context.Background(), &tt.account, true)

			require.NoError(t, err)
			require.Equal(t, []string{"live-model"}, got)
			require.Equal(t, 1, discoverer.calls[tt.account.ID])
		})
	}
}

func TestModelCatalogStaticallyUnsupportedFormatsUseConfiguredOrDefaultsWithoutDiscovery(t *testing.T) {
	tests := []struct {
		name    string
		account Account
		want    []string
	}{
		{
			name: "grok oauth mapping",
			account: Account{ID: 23, Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{
				"model_mapping": map[string]any{"grok-client": "grok-upstream"},
			}},
			want: []string{"grok-client"},
		},
		{
			name:    "grok oauth defaults",
			account: Account{ID: 24, Platform: PlatformGrok, Type: AccountTypeOAuth},
			want:    DefaultModelCatalogIDs(PlatformGrok),
		},
		{
			name: "gemini code assist whitelist",
			account: Account{ID: 25, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{
				"project_id":      "code-assist-project",
				"model_whitelist": []any{"gemini-client"},
			}},
			want: []string{"gemini-client"},
		},
		{
			name: "gemini code assist defaults",
			account: Account{ID: 26, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{
				"oauth_type": "code_assist",
				"project_id": "code-assist-project",
			}},
			want: DefaultModelCatalogIDs(PlatformGemini),
		},
		{
			name: "gemini google one project mapping",
			account: Account{ID: 29, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{
				"oauth_type":    "google_one",
				"project_id":    "google-one-project",
				"model_mapping": map[string]any{"google-one-client": "google-one-upstream"},
			}},
			want: []string{"google-one-client"},
		},
		{
			name: "gemini ai studio project whitelist",
			account: Account{ID: 30, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{
				"oauth_type":      "ai_studio",
				"project_id":      "ai-studio-project",
				"model_whitelist": []any{"ai-studio-client"},
			}},
			want: []string{"ai-studio-client"},
		},
	}
	waitCases := []struct {
		name        string
		waitForLive bool
	}{
		{name: "wait for live", waitForLive: true},
		{name: "no wait", waitForLive: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, waitCase := range waitCases {
				t.Run(waitCase.name, func(t *testing.T) {
					discoverer := &recordingModelDiscoverer{
						models: map[int64][]string{tt.account.ID: {"unexpected-live-model"}},
						errs:   map[int64]error{},
					}
					catalog := NewModelCatalogService(nil, nil, nil, discoverer, config.ModelCatalogConfig{RequestTimeoutSeconds: 10})

					got, err := catalog.ListForAccount(context.Background(), &tt.account, waitCase.waitForLive)

					require.NoError(t, err)
					require.Equal(t, tt.want, got)
					require.Zero(t, discoverer.calls[tt.account.ID])
				})
			}
		})
	}
}

func TestModelCatalogUnsupportedDiscoveryErrorFallsBackToConfiguredOrDefaults(t *testing.T) {
	tests := []struct {
		name    string
		account Account
		want    []string
	}{
		{
			name: "configured mapping",
			account: Account{ID: 27, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"model_mapping": map[string]any{"client-model": "upstream-model"},
			}},
			want: []string{"client-model"},
		},
		{
			name:    "provider defaults",
			account: Account{ID: 28, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			want:    DefaultModelCatalogIDs(PlatformOpenAI),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discoverer := &recordingModelDiscoverer{
				models: map[int64][]string{},
				errs: map[int64]error{tt.account.ID: &UpstreamModelSyncError{
					Kind:    UpstreamModelSyncErrorUnsupported,
					Message: "unsupported live model format",
				}},
			}
			catalog := NewModelCatalogService(nil, nil, nil, discoverer, config.ModelCatalogConfig{RequestTimeoutSeconds: 10})

			got, err := catalog.ListForAccount(context.Background(), &tt.account, true)

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Equal(t, 1, discoverer.calls[tt.account.ID])
		})
	}
}

func TestModelCatalogWaitForLiveFalseReturnsDefaultsWithoutDiscovery(t *testing.T) {
	account := &Account{ID: 16, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	discoverer := &recordingModelDiscoverer{models: map[int64][]string{account.ID: {"live-model"}}, errs: map[int64]error{}}
	catalog := NewModelCatalogService(nil, nil, nil, discoverer, config.ModelCatalogConfig{RequestTimeoutSeconds: 10})

	got, err := catalog.ListForAccount(context.Background(), account, false)

	require.NoError(t, err)
	require.Equal(t, DefaultModelCatalogIDs(PlatformOpenAI), got)
	require.Zero(t, discoverer.calls[account.ID])
}

func TestModelCatalogLiveDiscoveryUsesTimeoutAndNormalizes(t *testing.T) {
	account := &Account{ID: 17, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	discoverer := modelDiscovererFunc(func(ctx context.Context, gotAccount *Account) ([]string, error) {
		require.Same(t, account, gotAccount)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(2*time.Second), deadline, time.Second)
		return []string{" gpt-b ", "GPT-A", "gpt-a", ""}, nil
	})
	catalog := NewModelCatalogService(nil, nil, nil, discoverer, config.ModelCatalogConfig{RequestTimeoutSeconds: 2})

	got, err := catalog.ListForAccount(context.Background(), account, true)

	require.NoError(t, err)
	require.Equal(t, []string{"GPT-A", "gpt-b"}, got)
}

func TestModelCatalogEmptyLiveResultFallsBackToProviderDefaults(t *testing.T) {
	account := &Account{ID: 18, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	catalog := NewModelCatalogService(nil, nil, nil, modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
		return []string{" ", ""}, nil
	}), config.ModelCatalogConfig{RequestTimeoutSeconds: 10})

	got, err := catalog.ListForAccount(context.Background(), account, true)

	require.NoError(t, err)
	require.Equal(t, DefaultModelCatalogIDs(PlatformOpenAI), got)
}

func TestModelCatalogNonUnsupportedLiveDiscoveryErrorFallsBackToProviderDefaults(t *testing.T) {
	wantErr := &UpstreamModelSyncError{
		Kind:    UpstreamModelSyncErrorUpstream,
		Message: "discovery failed",
		Err:     errors.New("upstream detail"),
	}
	account := &Account{ID: 19, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	catalog := NewModelCatalogService(nil, nil, nil, modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
		return nil, wantErr
	}), config.ModelCatalogConfig{RequestTimeoutSeconds: 10})

	got, err := catalog.ListForAccount(context.Background(), account, true)

	require.NoError(t, err)
	require.Equal(t, DefaultModelCatalogIDs(PlatformOpenAI), got)
}

func TestModelCatalogMixedAccountsUnionsLiveAndRestricted(t *testing.T) {
	discoverer := &recordingModelDiscoverer{
		models: map[int64][]string{1: {"gpt-live-new"}},
		errs:   map[int64]error{},
	}
	catalog := &ModelCatalogService{
		discoverer: discoverer,
		cfg:        config.ModelCatalogConfig{RequestTimeoutSeconds: 10},
		now:        time.Now,
	}
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"alias-old": "upstream-old"}}},
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusDisabled, Schedulable: true},
		{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: false},
		{ID: 5, Platform: PlatformGemini, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true},
	}

	got, err := catalog.listFromAccounts(context.Background(), accounts, PlatformOpenAI, true)
	require.NoError(t, err)
	require.Equal(t, []string{"alias-old", "gpt-live-new"}, got)
	require.Equal(t, 1, discoverer.calls[1])
	require.Zero(t, discoverer.calls[3])
	require.Zero(t, discoverer.calls[4])
	require.Zero(t, discoverer.calls[5])
}

func TestModelCatalogPublicScope(t *testing.T) {
	// Groups 10 and 11 are public and both reference account 1; group 12 is exclusive
	// and references account 9. ListPublic must return account 1's model once and no
	// model from account 9.
	groups := []Group{
		{ID: 10, Platform: PlatformOpenAI, Status: StatusActive},
		{ID: 11, Platform: PlatformOpenAI, Status: StatusActive},
		{ID: 12, Platform: PlatformOpenAI, Status: StatusActive, IsExclusive: true},
	}
	publicAccount := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-public": "upstream-public"}}}
	exclusiveAccount := Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-exclusive": "upstream-exclusive"}}}
	discoverer := &recordingModelDiscoverer{
		models: map[int64][]string{},
		errs:   map[int64]error{},
	}
	catalog := &ModelCatalogService{
		accountRepo: &modelCatalogAccountRepoStub{byGroup: map[int64][]Account{
			10: {publicAccount}, 11: {publicAccount}, 12: {exclusiveAccount},
		}},
		groupRepo:  &modelCatalogGroupRepoStub{groups: groups},
		discoverer: discoverer,
		cfg:        config.ModelCatalogConfig{RequestTimeoutSeconds: 10},
		now:        time.Now,
	}
	got, err := catalog.ListPublic(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-public"}, got[PlatformOpenAI])
	require.Empty(t, discoverer.calls)
}

func TestModelCatalogPublicAntigravityAPIKeyUsesConfiguredRestrictionDespiteLiveCache(t *testing.T) {
	now := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	group := Group{ID: 40, Platform: PlatformAntigravity, Status: StatusActive}
	restrictedAPIKey := Account{
		ID: 41, Platform: PlatformAntigravity, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"client-visible": "gateway-target"},
		},
	}
	liveOAuth := Account{
		ID: 42, Platform: PlatformAntigravity, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
	}
	discoverer := &recordingModelDiscoverer{models: map[int64][]string{}, errs: map[int64]error{}}
	catalog := NewModelCatalogService(
		&modelCatalogAccountRepoStub{byGroup: map[int64][]Account{40: {restrictedAPIKey, liveOAuth}}},
		&modelCatalogGroupRepoStub{groups: []Group{group}},
		nil,
		discoverer,
		config.ModelCatalogConfig{RefreshIntervalSeconds: 300, RequestTimeoutSeconds: 10},
	)
	catalog.now = func() time.Time { return now }
	catalog.cache.storeSuccess(restrictedAPIKey.ID, []string{"api-key-cached-live"}, now)
	catalog.cache.storeSuccess(liveOAuth.ID, []string{"oauth-live"}, now)

	models, err := catalog.ListPublic(context.Background())

	require.NoError(t, err)
	require.Equal(t, []string{"client-visible", "oauth-live"}, models[PlatformAntigravity])
	require.Empty(t, discoverer.calls)
}

func TestModelCatalogRequestMemoResolvesAccountOnce(t *testing.T) {
	discoverer := &recordingModelDiscoverer{
		models: map[int64][]string{1: {"gpt-public"}},
		errs:   map[int64]error{},
	}
	catalog := &ModelCatalogService{
		discoverer: discoverer,
		cfg:        config.ModelCatalogConfig{RequestTimeoutSeconds: 10},
		now:        time.Now,
	}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	memo := make(map[int64][]string)
	first, err := catalog.resolveAccountOnce(context.Background(), memo, account, true)
	require.NoError(t, err)
	second, err := catalog.resolveAccountOnce(context.Background(), memo, account, true)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-public"}, first)
	require.Equal(t, first, second)
	require.Equal(t, 1, discoverer.calls[1])
}

func TestModelCatalogGroupConfigAndCandidates(t *testing.T) {
	group := Group{ID: 20, Platform: PlatformOpenAI, Status: StatusActive,
		ModelsListConfig: GroupModelsListConfig{Enabled: true, Models: []string{"priced-d", "gpt-b"}}}
	account := Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	discoverer := &recordingModelDiscoverer{
		models: map[int64][]string{2: {"gpt-a", "gpt-b"}},
		errs:   map[int64]error{},
	}
	channelRepo := &modelCatalogChannelRepoStub{
		channels: []Channel{{
			ID: 30, Status: StatusActive, GroupIDs: []int64{20},
			ModelMapping: map[string]map[string]string{PlatformOpenAI: {"alias-c": "upstream-c"}},
			ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"priced-d"}}},
		}},
		platforms: map[int64]string{20: PlatformOpenAI},
	}
	catalog := &ModelCatalogService{
		accountRepo:    &modelCatalogAccountRepoStub{byGroup: map[int64][]Account{20: {account}}},
		groupRepo:      &modelCatalogGroupRepoStub{groups: []Group{group}},
		channelService: NewChannelService(channelRepo, nil, nil, nil),
		discoverer:     discoverer,
		cfg:            config.ModelCatalogConfig{RequestTimeoutSeconds: 10},
		now:            time.Now,
	}
	candidates, err := catalog.ListGroupCandidates(context.Background(), 20, PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, []string{"alias-c", "gpt-a", "gpt-b", "priced-d"}, candidates)

	final, err := catalog.ListForGroup(context.Background(), 20, PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, []string{"priced-d", "gpt-b"}, final)
}

func TestModelCatalogGroupCandidatesSupportsCreateFlowAndInferredPlatform(t *testing.T) {
	group := Group{ID: 20, Platform: PlatformOpenAI, Status: StatusActive}
	account := Account{
		ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"group-model": "upstream-group"}},
	}
	platformAccount := Account{
		ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"platform-model": "upstream-platform"}},
	}
	defaultPlatformAccount := Account{
		ID: 4, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"anthropic-model": "upstream-anthropic"}},
	}
	catalog := &ModelCatalogService{
		accountRepo: &modelCatalogAccountRepoStub{
			byGroup: map[int64][]Account{20: {account}},
			all:     []Account{platformAccount, defaultPlatformAccount},
		},
		groupRepo: &modelCatalogGroupRepoStub{groups: []Group{group}},
	}

	createModels, err := catalog.ListGroupCandidates(context.Background(), 0, PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, []string{"platform-model"}, createModels)

	emptyModels, err := catalog.ListGroupCandidates(context.Background(), 0, PlatformGemini)
	require.NoError(t, err)
	require.Empty(t, emptyModels)

	defaultCreateModels, err := catalog.ListGroupCandidates(context.Background(), 0, "")
	require.NoError(t, err)
	require.Equal(t, []string{"anthropic-model"}, defaultCreateModels)

	inferredModels, err := catalog.ListGroupCandidates(context.Background(), 20, "")
	require.NoError(t, err)
	require.Equal(t, []string{"group-model"}, inferredModels)
}

func TestModelCatalogPlatformScopeDoesNotInjectDefaultsWithoutAccounts(t *testing.T) {
	groupID := int64(20)
	groupAccount := Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"group-model": "upstream-group"}}}
	platformAccount := Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"platform-model": "upstream-platform"}}}
	catalog := &ModelCatalogService{
		accountRepo: &modelCatalogAccountRepoStub{
			byGroup: map[int64][]Account{groupID: {groupAccount}},
			all:     []Account{platformAccount},
		},
	}

	groupModels, err := catalog.ListForPlatform(context.Background(), &groupID, PlatformOpenAI, true)
	require.NoError(t, err)
	require.Equal(t, []string{"group-model"}, groupModels)

	platformModels, err := catalog.ListForPlatform(context.Background(), nil, PlatformOpenAI, true)
	require.NoError(t, err)
	require.Equal(t, []string{"platform-model"}, platformModels)

	emptyModels, err := catalog.ListForPlatform(context.Background(), nil, PlatformGemini, true)
	require.NoError(t, err)
	require.Empty(t, emptyModels)
}

func TestModelCatalogAPIKeyScopeRequiresLoadedGroup(t *testing.T) {
	groupID := int64(20)
	group := &Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive}
	account := Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"group-model": "upstream-group"}}}
	catalog := &ModelCatalogService{
		accountRepo: &modelCatalogAccountRepoStub{byGroup: map[int64][]Account{groupID: {account}}},
		groupRepo:   &modelCatalogGroupRepoStub{groups: []Group{*group}},
	}

	groupModels, err := catalog.ListForAPIKey(context.Background(), &APIKey{GroupID: &groupID, Group: group})
	require.NoError(t, err)
	require.Equal(t, []string{"group-model"}, groupModels)

	legacyModels, err := catalog.ListForAPIKey(context.Background(), &APIKey{})
	require.NoError(t, err)
	require.Equal(t, DefaultModelCatalogIDs(PlatformAnthropic), legacyModels)

	_, err = catalog.ListForAPIKey(context.Background(), &APIKey{GroupID: &groupID})
	require.Error(t, err)
}
