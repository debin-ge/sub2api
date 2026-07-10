package service

import (
	"context"
	"errors"
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
		{name: "upstream", account: Account{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeUpstream}},
		{name: "anthropic passthrough", account: Account{ID: 13, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Extra: map[string]any{"anthropic_passthrough": true}}},
		{name: "windsurf", account: Account{ID: 14, Platform: PlatformWindsurf, Type: AccountTypeAPIKey}},
		{name: "opencode", account: Account{ID: 15, Platform: PlatformOpenCode, Type: AccountTypeAPIKey}},
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

func TestModelCatalogEmptyLiveResultIsUpstreamError(t *testing.T) {
	account := &Account{ID: 18, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	catalog := NewModelCatalogService(nil, nil, nil, modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
		return []string{" ", ""}, nil
	}), config.ModelCatalogConfig{RequestTimeoutSeconds: 10})

	got, err := catalog.ListForAccount(context.Background(), account, true)

	require.Nil(t, got)
	var syncErr *UpstreamModelSyncError
	require.ErrorAs(t, err, &syncErr)
	require.Equal(t, UpstreamModelSyncErrorUpstream, syncErr.Kind)
}

func TestModelCatalogPropagatesLiveDiscoveryError(t *testing.T) {
	wantErr := errors.New("discovery failed")
	account := &Account{ID: 19, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	catalog := NewModelCatalogService(nil, nil, nil, modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
		return nil, wantErr
	}), config.ModelCatalogConfig{RequestTimeoutSeconds: 10})

	got, err := catalog.ListForAccount(context.Background(), account, true)

	require.Nil(t, got)
	require.ErrorIs(t, err, wantErr)
}
