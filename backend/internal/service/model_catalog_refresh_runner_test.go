package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type refreshRunnerAccountRepoStub struct {
	AccountRepository
	accounts  []Account
	listCalls chan struct{}
}

func (r *refreshRunnerAccountRepoStub) ListSchedulable(context.Context) ([]Account, error) {
	if r.listCalls != nil {
		select {
		case r.listCalls <- struct{}{}:
		default:
		}
	}
	return append([]Account(nil), r.accounts...), nil
}

func TestModelCatalogRefreshAllHonorsMaxConcurrency(t *testing.T) {
	release := make(chan struct{})
	var active atomic.Int64
	var maximum atomic.Int64
	discoverer := modelDiscovererFunc(func(ctx context.Context, _ *Account) ([]string, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		select {
		case <-release:
			return []string{"model-new"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	accounts := make([]Account, 8)
	for i := range accounts {
		accounts[i] = Account{ID: int64(i + 1), Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	}
	catalog := &ModelCatalogService{
		accountRepo: &refreshRunnerAccountRepoStub{accounts: accounts}, discoverer: discoverer,
		cfg:   config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 3},
		cache: newModelCatalogCache(), refreshSem: make(chan struct{}, 3), now: time.Now,
	}
	done := make(chan struct{})
	go func() { catalog.RefreshAll(context.Background()); close(done) }()
	require.Eventually(t, func() bool { return maximum.Load() == 3 }, time.Second, 10*time.Millisecond)
	close(release)
	<-done
	require.LessOrEqual(t, maximum.Load(), int64(3))
}

func TestModelCatalogRefreshAllFiltersAccountsAndIsolatesFailures(t *testing.T) {
	resetModelCatalogStatsForTest()
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true},
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true},
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true},
		{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true},
		{ID: 5, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusDisabled, Schedulable: true},
		{ID: 6, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: false},
	}
	var calls atomic.Int64
	catalog := &ModelCatalogService{
		accountRepo: &refreshRunnerAccountRepoStub{accounts: accounts},
		discoverer: modelDiscovererFunc(func(_ context.Context, account *Account) ([]string, error) {
			calls.Add(1)
			if account.ID == 2 {
				return nil, errors.New("account failure")
			}
			return []string{"model-new"}, nil
		}),
		cfg:   config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 2},
		cache: newModelCatalogCache(), refreshSem: make(chan struct{}, 2), now: time.Now,
	}

	summary := catalog.RefreshAll(context.Background())

	require.Equal(t, int64(3), calls.Load())
	require.Equal(t, ModelCatalogRefreshPlatformSummary{Scanned: 2, Succeeded: 2}, summary.ByPlatform[PlatformOpenAI])
	require.Equal(t, ModelCatalogRefreshPlatformSummary{Scanned: 1, Failed: 1}, summary.ByPlatform[PlatformAnthropic])
	stats := ModelCatalogStats()
	require.Equal(t, int64(2), stats.ByPlatform[PlatformOpenAI].RefreshSuccess)
	require.Equal(t, int64(1), stats.ByPlatform[PlatformAnthropic].RefreshFailure)
}

func TestModelCatalogRefreshAllIncludesAntigravityAPIKey(t *testing.T) {
	account := Account{
		ID: 7, Platform: PlatformAntigravity, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "gateway-key", "base_url": "https://gateway.example.com/antigravity"},
	}
	var calls atomic.Int64
	catalog := &ModelCatalogService{
		accountRepo: &refreshRunnerAccountRepoStub{accounts: []Account{account}},
		discoverer: modelDiscovererFunc(func(_ context.Context, got *Account) ([]string, error) {
			calls.Add(1)
			require.Equal(t, account.ID, got.ID)
			return []string{"gateway-live"}, nil
		}),
		cfg:   config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 1},
		cache: newModelCatalogCache(), refreshSem: make(chan struct{}, 1), now: time.Now,
	}

	summary := catalog.RefreshAll(context.Background())

	require.Equal(t, int64(1), calls.Load())
	require.Equal(t, ModelCatalogRefreshPlatformSummary{Scanned: 1, Succeeded: 1}, summary.ByPlatform[PlatformAntigravity])
}

func TestModelCatalogRefreshRunnerLogsBoundedSanitizedFailure(t *testing.T) {
	const (
		responseSecret = "response-body-secret-9fda"
		tokenSecret    = "token-secret-3cbe"
		proxySecret    = "proxy-secret-52a1"
	)
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	account := Account{
		ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": tokenSecret},
		Proxy:       &Proxy{Protocol: "http", Host: proxySecret, Port: 8080, Username: "user", Password: "password"},
	}
	catalog := &ModelCatalogService{
		accountRepo: &refreshRunnerAccountRepoStub{accounts: []Account{account}},
		discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
			return nil, newUpstreamModelSyncUpstreamError(
				"Upstream model list request failed with HTTP 502",
				errors.New(responseSecret),
			)
		}),
		cfg:   config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 1},
		cache: newModelCatalogCache(), refreshSem: make(chan struct{}, 1), now: time.Now,
	}
	runner := NewModelCatalogRefreshRunner(catalog, catalog.cfg)

	runner.runOnce(context.Background())

	logs := output.String()
	require.Contains(t, logs, `"msg":"model_catalog_account_refresh_failed"`)
	require.Contains(t, logs, `"account_id":42`)
	require.Contains(t, logs, `"platform":"openai"`)
	require.Contains(t, logs, `"error_kind":"upstream"`)
	require.Contains(t, logs, `"http_status":502`)
	require.Contains(t, logs, `"msg":"model_catalog_refresh_pass_completed"`)
	require.Contains(t, logs, `"by_platform"`)
	require.Contains(t, logs, `"duration"`)
	for _, secret := range []string{responseSecret, tokenSecret, proxySecret, "password"} {
		require.False(t, strings.Contains(logs, secret), "logs contained secret %q: %s", secret, logs)
	}
}

func TestModelCatalogRefreshRunnerLogsRuntimeStatsSnapshot(t *testing.T) {
	resetModelCatalogStatsForTest()
	recordModelCatalogCache(catalogCacheFresh)
	recordModelCatalogFallback(PlatformOpenAI, modelCatalogFallbackUpstreamError)
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	runner := NewModelCatalogRefreshRunner(&ModelCatalogService{
		accountRepo: &refreshRunnerAccountRepoStub{}, now: time.Now,
	}, config.ModelCatalogConfig{})

	runner.runOnce(context.Background())

	logs := output.String()
	require.Contains(t, logs, `"runtime_stats"`)
	require.Contains(t, logs, `"CacheFresh":1`)
	require.Contains(t, logs, `"FallbackByReason"`)
}

func TestModelCatalogRefreshAllRecordsDuration(t *testing.T) {
	startedAt := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	var clockCalls atomic.Int64
	catalog := &ModelCatalogService{
		accountRepo: &refreshRunnerAccountRepoStub{},
		now: func() time.Time {
			if clockCalls.Add(1) == 1 {
				return startedAt
			}
			return startedAt.Add(2 * time.Second)
		},
	}

	summary := catalog.RefreshAll(context.Background())

	require.Equal(t, 2*time.Second, summary.Duration)
}

func TestModelCatalogRefreshRunnerStartsImmediatelyAndStops(t *testing.T) {
	listCalls := make(chan struct{}, 1)
	repo := &refreshRunnerAccountRepoStub{listCalls: listCalls}
	cfg := config.ModelCatalogConfig{RefreshIntervalSeconds: 300, RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 2}
	catalog := &ModelCatalogService{accountRepo: repo, cfg: cfg, cache: newModelCatalogCache(), refreshSem: make(chan struct{}, 2), now: time.Now}
	runner := NewModelCatalogRefreshRunner(catalog, cfg)
	runner.Start()
	select {
	case <-listCalls:
	case <-time.After(time.Second):
		t.Fatal("initial refresh scan did not start")
	}
	stopped := make(chan struct{})
	go func() { runner.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop")
	}
}

func TestModelCatalogRefreshRunnerStopWaitsForInFlightPass(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	repo := &refreshRunnerAccountRepoStub{accounts: []Account{{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
	}}}
	cfg := config.ModelCatalogConfig{RefreshIntervalSeconds: 300, RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 1}
	catalog := &ModelCatalogService{
		accountRepo: repo,
		discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
			close(started)
			<-release
			return []string{"model-new"}, nil
		}),
		cfg: cfg, cache: newModelCatalogCache(), refreshSem: make(chan struct{}, 1), now: time.Now,
	}
	runner := NewModelCatalogRefreshRunner(catalog, cfg)
	runner.Start()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("initial refresh did not start")
	}

	stopped := make(chan struct{})
	go func() { runner.Stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("runner stopped before its in-flight pass exited")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after its in-flight pass exited")
	}
}

func TestProvideModelCatalogConfig(t *testing.T) {
	want := config.ModelCatalogConfig{
		RefreshIntervalSeconds: 300,
		RequestTimeoutSeconds:  10,
		StaleTTLSeconds:        86400,
		FailureBackoffSeconds:  60,
		MaxConcurrency:         5,
	}

	require.Equal(t, want, ProvideModelCatalogConfig(&config.Config{ModelCatalog: want}))
}

func TestProvideModelCatalogRefreshRunnerStartsImmediately(t *testing.T) {
	listCalls := make(chan struct{}, 1)
	repo := &refreshRunnerAccountRepoStub{listCalls: listCalls}
	cfg := config.ModelCatalogConfig{RefreshIntervalSeconds: 300, RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 2}
	catalog := &ModelCatalogService{accountRepo: repo, cfg: cfg, cache: newModelCatalogCache(), refreshSem: make(chan struct{}, 2), now: time.Now}

	runner := ProvideModelCatalogRefreshRunner(catalog, cfg)
	t.Cleanup(runner.Stop)

	select {
	case <-listCalls:
	case <-time.After(time.Second):
		t.Fatal("provided runner did not start its initial refresh scan")
	}
}
