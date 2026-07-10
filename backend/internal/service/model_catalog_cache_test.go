package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestModelCatalogCacheFreshStaleExpired(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	cache := newModelCatalogCache()
	cache.storeSuccess(7, []string{"model-a"}, now)
	require.Equal(t, catalogCacheFresh, cache.state(7, now.Add(4*time.Minute), 5*time.Minute, 24*time.Hour))
	require.Equal(t, catalogCacheStale, cache.state(7, now.Add(6*time.Minute), 5*time.Minute, 24*time.Hour))
	require.Equal(t, catalogCacheMiss, cache.state(7, now.Add(25*time.Hour), 5*time.Minute, 24*time.Hour))
	cache.invalidate(7)
	require.Equal(t, catalogCacheMiss, cache.state(7, now, 5*time.Minute, 24*time.Hour))
}

func TestModelCatalogCacheLoadReturnsClone(t *testing.T) {
	cache := newModelCatalogCache()
	cache.storeSuccess(7, []string{"model-a"}, time.Now())

	first, ok := cache.load(7)
	require.True(t, ok)
	first.models[0] = "mutated"

	second, ok := cache.load(7)
	require.True(t, ok)
	require.Equal(t, []string{"model-a"}, second.models)
}

func TestModelCatalogCacheLoadStateReturnsClassifiedSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	cache := newModelCatalogCache()
	cache.storeSuccess(7, []string{"model-a"}, now)

	entry, state := cache.loadState(7, now.Add(time.Minute), 5*time.Minute, 24*time.Hour)
	cache.invalidate(7)

	require.Equal(t, catalogCacheFresh, state)
	require.Equal(t, []string{"model-a"}, entry.models, "the classified snapshot must remain usable after invalidation")
}

func TestModelCatalogRefreshFailurePreservesSuccessAndBacksOff(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	cache := newModelCatalogCache()
	cache.storeSuccess(7, []string{"model-old"}, now.Add(-10*time.Minute))
	var calls atomic.Int64
	catalog := &ModelCatalogService{
		discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
			calls.Add(1)
			return nil, nil
		}),
		cfg: config.ModelCatalogConfig{
			RequestTimeoutSeconds: 10,
			FailureBackoffSeconds: 60,
			MaxConcurrency:        5,
		},
		cache:      cache,
		refreshSem: make(chan struct{}, 5),
		now:        func() time.Time { return now },
	}
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	_, err := catalog.RefreshAccount(context.Background(), account)
	require.Error(t, err)
	entry, ok := cache.load(7)
	require.True(t, ok)
	require.Equal(t, []string{"model-old"}, entry.models)

	_, _ = catalog.RefreshAccount(context.Background(), account)
	require.Equal(t, int64(1), calls.Load(), "second refresh must be suppressed by backoff")

	now = now.Add(61 * time.Second)
	_, _ = catalog.RefreshAccount(context.Background(), account)
	require.Equal(t, int64(2), calls.Load(), "refresh must resume after backoff")
}

func TestModelCatalogRefreshErrorIsReturnedAndPreservesSuccess(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	wantErr := errors.New("upstream unavailable")
	cache := newModelCatalogCache()
	cache.storeSuccess(9, []string{"model-old"}, now.Add(-10*time.Minute))
	catalog := &ModelCatalogService{
		discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
			return nil, wantErr
		}),
		cfg:        config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 1},
		cache:      cache,
		refreshSem: make(chan struct{}, 1),
		now:        func() time.Time { return now },
	}

	models, err := catalog.RefreshAccount(context.Background(), &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeOAuth})

	require.Nil(t, models)
	require.ErrorIs(t, err, wantErr)
	entry, ok := cache.load(9)
	require.True(t, ok)
	require.Equal(t, []string{"model-old"}, entry.models)
}

func TestModelCatalogRefreshSemaphoreTimeoutBacksOff(t *testing.T) {
	var calls atomic.Int64
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	catalog := &ModelCatalogService{
		discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
			calls.Add(1)
			return []string{"model-new"}, nil
		}),
		cfg:        config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 1},
		cache:      newModelCatalogCache(),
		refreshSem: sem,
		now:        time.Now,
	}
	account := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := catalog.RefreshAccount(ctx, account)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	<-sem

	_, err = catalog.RefreshAccount(context.Background(), account)
	require.ErrorIs(t, err, errModelCatalogRefreshBackoff)
	require.Zero(t, calls.Load())
}

func TestModelCatalogRefreshCancellationDoesNotBackOff(t *testing.T) {
	var calls atomic.Int64
	started := make(chan struct{})
	catalog := &ModelCatalogService{
		discoverer: modelDiscovererFunc(func(ctx context.Context, _ *Account) ([]string, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []string{"model-new"}, nil
		}),
		cfg:        config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 1},
		cache:      newModelCatalogCache(),
		refreshSem: make(chan struct{}, 1),
		now:        time.Now,
	}
	account := &Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := catalog.RefreshAccount(ctx, account)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not enter discovery")
	}
	cancel()

	err := <-result
	require.ErrorIs(t, err, context.Canceled)

	models, err := catalog.RefreshAccount(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"model-new"}, models)
	require.Equal(t, int64(2), calls.Load())
}

func TestModelCatalogRefreshCancellationWhileWaitingForSemaphoreDoesNotBackOff(t *testing.T) {
	var calls atomic.Int64
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	catalog := &ModelCatalogService{
		discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
			calls.Add(1)
			return []string{"model-new"}, nil
		}),
		cfg:        config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 1},
		cache:      newModelCatalogCache(),
		refreshSem: sem,
		now:        time.Now,
	}
	account := &Account{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := catalog.RefreshAccount(ctx, account)
	require.ErrorIs(t, err, context.Canceled)
	<-sem

	models, err := catalog.RefreshAccount(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"model-new"}, models)
	require.Equal(t, int64(1), calls.Load())
}

func TestModelCatalogRefreshSingleflight(t *testing.T) {
	const callerCount = 20
	const followerCount = callerCount - 1

	release := make(chan struct{})
	var releaseOnce sync.Once
	var calls atomic.Int64
	var active atomic.Int64
	var maximum atomic.Int64
	discoveryStarted := make(chan struct{}, callerCount)
	discoverer := modelDiscovererFunc(func(ctx context.Context, _ *Account) ([]string, error) {
		calls.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		discoveryStarted <- struct{}{}
		select {
		case <-release:
			return []string{"model-new"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	catalog := &ModelCatalogService{
		discoverer: discoverer,
		cfg:        config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 5},
		cache:      newModelCatalogCache(), refreshSem: make(chan struct{}, 5), now: time.Now,
	}
	account := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	var wg sync.WaitGroup
	type result struct {
		models []string
		err    error
	}
	results := make(chan result, callerCount)
	runRefresh := func() {
		models, err := catalog.RefreshAccount(context.Background(), account)
		results <- result{models: models, err: err}
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		runRefresh()
	}()
	select {
	case <-discoveryStarted:
	case <-time.After(time.Second):
		t.Fatal("leader discovery did not start")
	}

	startFollowers := make(chan struct{})
	followersInvoking := make(chan struct{}, followerCount)
	var followersReady sync.WaitGroup
	followersReady.Add(followerCount)
	for i := 0; i < followerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			followersReady.Done()
			<-startFollowers
			followersInvoking <- struct{}{}
			runRefresh()
		}()
	}
	followersReady.Wait()
	close(startFollowers)
	for i := 0; i < followerCount; i++ {
		<-followersInvoking
	}
	require.Never(t, func() bool { return maximum.Load() > 1 }, 50*time.Millisecond, time.Millisecond,
		"overlapping callers must not start a second active discovery")
	require.Equal(t, int64(1), calls.Load(), "the blocked leader must remain the only discovery before release")

	releaseOnce.Do(func() { close(release) })
	wg.Wait()
	close(results)
	allResults := make([]result, 0, callerCount)
	for result := range results {
		require.NoError(t, result.err)
		require.Equal(t, []string{"model-new"}, result.models)
		allResults = append(allResults, result)
	}
	require.Len(t, allResults, callerCount)
	require.Equal(t, int64(1), maximum.Load(), "same-account discoveries must never overlap")
	allResults[0].models[0] = "mutated"
	for _, result := range allResults[1:] {
		require.Equal(t, []string{"model-new"}, result.models, "singleflight callers must receive isolated slices")
	}
}

func TestModelCatalogRefreshAsyncDeduplicatesBeforeSpawning(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	catalog := &ModelCatalogService{
		discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
			calls.Add(1)
			close(started)
			<-release
			return []string{"model-new"}, nil
		}),
		cfg:        config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 1},
		cache:      newModelCatalogCache(),
		refreshSem: make(chan struct{}, 1),
		now:        time.Now,
	}
	account := &Account{ID: 20, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}

	for i := 0; i < 100; i++ {
		catalog.RefreshAccountAsync(account)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("async refresh did not start")
	}
	catalog.asyncRefreshMu.Lock()
	admitted := len(catalog.asyncRefreshes)
	catalog.asyncRefreshMu.Unlock()
	require.Equal(t, 1, admitted, "one account may have at most one admitted async worker")

	close(release)
	require.Eventually(t, func() bool {
		catalog.asyncRefreshMu.Lock()
		defer catalog.asyncRefreshMu.Unlock()
		return len(catalog.asyncRefreshes) == 0
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, int64(1), calls.Load())
}

func TestModelCatalogInvalidateForgetsOldFlightAndProtectsNewGeneration(t *testing.T) {
	oldStarted := make(chan struct{})
	newStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseOld) }) })
	var calls atomic.Int64
	catalog := &ModelCatalogService{
		discoverer: modelDiscovererFunc(func(_ context.Context, account *Account) ([]string, error) {
			calls.Add(1)
			if account.Name == "old" {
				close(oldStarted)
				<-releaseOld
				return []string{"model-old"}, nil
			}
			close(newStarted)
			return []string{"model-new"}, nil
		}),
		cfg:        config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 2},
		cache:      newModelCatalogCache(),
		refreshSem: make(chan struct{}, 2),
		now:        time.Now,
	}
	oldAccount := &Account{ID: 21, Name: "old", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	newAccount := &Account{ID: 21, Name: "new", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	catalog.RefreshAccountAsync(oldAccount)
	select {
	case <-oldStarted:
	case <-time.After(time.Second):
		t.Fatal("old refresh did not start")
	}

	catalog.InvalidateAccount(oldAccount.ID)
	catalog.RefreshAccountAsync(newAccount)
	select {
	case <-newStarted:
	case <-time.After(time.Second):
		releaseOnce.Do(func() { close(releaseOld) })
		t.Fatal("post-invalidation refresh joined the old flight")
	}
	require.Eventually(t, func() bool {
		entry, ok := catalog.cache.load(newAccount.ID)
		return ok && len(entry.models) == 1 && entry.models[0] == "model-new"
	}, time.Second, 10*time.Millisecond)

	releaseOnce.Do(func() { close(releaseOld) })
	require.Eventually(t, func() bool {
		entry, ok := catalog.cache.load(newAccount.ID)
		if !ok || len(entry.models) != 1 || entry.models[0] != "model-new" {
			return false
		}
		catalog.asyncRefreshMu.Lock()
		defer catalog.asyncRefreshMu.Unlock()
		return len(catalog.asyncRefreshes) == 0
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, int64(2), calls.Load())
}

func TestModelCatalogAsyncAdmissionOldCleanupPreservesNewGeneration(t *testing.T) {
	catalog := &ModelCatalogService{cache: newModelCatalogCache()}
	oldAdmission, _, admitted := catalog.beginAsyncRefresh(22)
	require.True(t, admitted)

	catalog.InvalidateAccount(22)
	newAdmission, _, admitted := catalog.beginAsyncRefresh(22)
	require.True(t, admitted)
	require.NotSame(t, oldAdmission, newAdmission)

	catalog.finishAsyncRefresh(22, oldAdmission)
	catalog.asyncRefreshMu.Lock()
	currentAdmission := catalog.asyncRefreshes[22]
	catalog.asyncRefreshMu.Unlock()
	require.Same(t, newAdmission, currentAdmission, "old cleanup must not delete the new generation's admission")
}

func TestModelCatalogListForAccountUsesFreshAndStaleCache(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	cfg := config.ModelCatalogConfig{
		RefreshIntervalSeconds: 300,
		RequestTimeoutSeconds:  10,
		StaleTTLSeconds:        86400,
		FailureBackoffSeconds:  60,
		MaxConcurrency:         2,
	}

	t.Run("fresh returns an isolated cached value", func(t *testing.T) {
		cache := newModelCatalogCache()
		cache.storeSuccess(10, []string{"model-fresh"}, now)
		var calls atomic.Int64
		catalog := &ModelCatalogService{
			discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
				calls.Add(1)
				return []string{"unexpected"}, nil
			}),
			cfg: cfg, cache: cache, refreshSem: make(chan struct{}, 2), now: func() time.Time { return now },
		}
		account := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}

		models, err := catalog.ListForAccount(context.Background(), account, true)
		require.NoError(t, err)
		require.Equal(t, []string{"model-fresh"}, models)
		require.Zero(t, calls.Load())

		models[0] = "mutated"
		entry, ok := cache.load(account.ID)
		require.True(t, ok)
		require.Equal(t, []string{"model-fresh"}, entry.models)
	})

	t.Run("stale returns immediately and refreshes asynchronously", func(t *testing.T) {
		cache := newModelCatalogCache()
		cache.storeSuccess(11, []string{"model-stale"}, now.Add(-6*time.Minute))
		var calls atomic.Int64
		catalog := &ModelCatalogService{
			discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
				calls.Add(1)
				return []string{"model-new"}, nil
			}),
			cfg: cfg, cache: cache, refreshSem: make(chan struct{}, 2), now: func() time.Time { return now },
		}
		account := &Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}

		models, err := catalog.ListForAccount(context.Background(), account, true)
		require.NoError(t, err)
		require.Equal(t, []string{"model-stale"}, models)
		require.Eventually(t, func() bool {
			entry, ok := cache.load(account.ID)
			return ok && calls.Load() == 1 && len(entry.models) == 1 && entry.models[0] == "model-new"
		}, time.Second, 10*time.Millisecond)
	})
}

func TestModelCatalogListForAccountMissObeysWaitForLive(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	cfg := config.ModelCatalogConfig{
		RefreshIntervalSeconds: 300,
		RequestTimeoutSeconds:  10,
		StaleTTLSeconds:        86400,
		FailureBackoffSeconds:  60,
		MaxConcurrency:         2,
	}

	t.Run("expired waits for a synchronous refresh", func(t *testing.T) {
		cache := newModelCatalogCache()
		cache.storeSuccess(12, []string{"model-expired"}, now.Add(-25*time.Hour))
		catalog := &ModelCatalogService{
			discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
				return []string{"model-new"}, nil
			}),
			cfg: cfg, cache: cache, refreshSem: make(chan struct{}, 2), now: func() time.Time { return now },
		}
		account := &Account{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}

		models, err := catalog.ListForAccount(context.Background(), account, true)
		require.NoError(t, err)
		require.Equal(t, []string{"model-new"}, models)
	})

	t.Run("cold no-wait returns defaults and refreshes asynchronously", func(t *testing.T) {
		cache := newModelCatalogCache()
		var calls atomic.Int64
		catalog := &ModelCatalogService{
			discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
				calls.Add(1)
				return []string{"model-new"}, nil
			}),
			cfg: cfg, cache: cache, refreshSem: make(chan struct{}, 2), now: func() time.Time { return now },
		}
		account := &Account{ID: 13, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}

		models, err := catalog.ListForAccount(context.Background(), account, false)
		require.NoError(t, err)
		require.Equal(t, DefaultModelCatalogIDs(PlatformOpenAI), models)
		require.Eventually(t, func() bool {
			entry, ok := cache.load(account.ID)
			return ok && calls.Load() == 1 && len(entry.models) == 1 && entry.models[0] == "model-new"
		}, time.Second, 10*time.Millisecond)
	})
}

func TestModelCatalogListForAccountNoWaitUsesConfiguredFallback(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	cfg := config.ModelCatalogConfig{
		RefreshIntervalSeconds: 300,
		RequestTimeoutSeconds:  10,
		StaleTTLSeconds:        86400,
		FailureBackoffSeconds:  60,
		MaxConcurrency:         1,
	}
	tests := []struct {
		name        string
		accountID   int64
		credentials map[string]any
		expired     bool
		want        []string
	}{
		{
			name:      "cold mapping",
			accountID: 30,
			credentials: map[string]any{
				"model_mapping": map[string]any{"mapped-client": "upstream-model"},
			},
			want: []string{"mapped-client"},
		},
		{
			name:      "expired whitelist",
			accountID: 31,
			credentials: map[string]any{
				"model_whitelist": []any{"whitelist-only"},
			},
			expired: true,
			want:    []string{"whitelist-only"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := newModelCatalogCache()
			if tt.expired {
				cache.storeSuccess(tt.accountID, []string{"expired-live-model"}, now.Add(-25*time.Hour))
			}
			started := make(chan struct{})
			release := make(chan struct{})
			finished := make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(release) })
				select {
				case <-finished:
				case <-time.After(time.Second):
				}
			})
			catalog := &ModelCatalogService{
				discoverer: modelDiscovererFunc(func(ctx context.Context, _ *Account) ([]string, error) {
					close(started)
					defer close(finished)
					select {
					case <-release:
						return []string{"discovered-model"}, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}),
				cfg: cfg, cache: cache, refreshSem: make(chan struct{}, 1), now: func() time.Time { return now },
			}
			account := &Account{
				ID: tt.accountID, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Status: StatusActive, Schedulable: true, Credentials: tt.credentials,
			}

			models, err := catalog.ListForAccount(context.Background(), account, false)

			require.NoError(t, err)
			require.Equal(t, tt.want, models)
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("async discovery did not start")
			}
			releaseOnce.Do(func() { close(release) })
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("async discovery did not finish")
			}
			require.Eventually(t, func() bool {
				entry, ok := cache.load(account.ID)
				catalog.asyncRefreshMu.Lock()
				asyncRefreshes := len(catalog.asyncRefreshes)
				catalog.asyncRefreshMu.Unlock()
				return ok && len(entry.models) == 1 && entry.models[0] == "discovered-model" && asyncRefreshes == 0
			}, time.Second, 10*time.Millisecond)
		})
	}
}

func TestModelCatalogListForAccountRefreshFailureFallsBack(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	cfg := config.ModelCatalogConfig{
		RefreshIntervalSeconds: 300,
		RequestTimeoutSeconds:  10,
		StaleTTLSeconds:        86400,
		FailureBackoffSeconds:  60,
		MaxConcurrency:         1,
	}
	wantErr := errors.New("upstream unavailable")

	t.Run("expired previous success wins over configured fallback", func(t *testing.T) {
		cache := newModelCatalogCache()
		cache.storeSuccess(14, []string{"model-old"}, now.Add(-25*time.Hour))
		catalog := &ModelCatalogService{
			discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) { return nil, wantErr }),
			cfg:        cfg, cache: cache, refreshSem: make(chan struct{}, 1), now: func() time.Time { return now },
		}
		account := &Account{ID: 14, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
			"model_mapping": map[string]any{"configured-model": "upstream-model"},
		}}

		models, err := catalog.ListForAccount(context.Background(), account, true)
		require.NoError(t, err)
		require.Equal(t, []string{"model-old"}, models)
	})

	t.Run("cold failure uses configured fallback", func(t *testing.T) {
		catalog := &ModelCatalogService{
			discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) { return nil, wantErr }),
			cfg:        cfg, cache: newModelCatalogCache(), refreshSem: make(chan struct{}, 1), now: func() time.Time { return now },
		}
		account := &Account{ID: 15, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
			"model_mapping": map[string]any{"configured-model": "upstream-model"},
		}}

		models, err := catalog.ListForAccount(context.Background(), account, true)
		require.NoError(t, err)
		require.Equal(t, []string{"configured-model"}, models)
	})
}

func TestModelCatalogInvalidateAccount(t *testing.T) {
	cache := newModelCatalogCache()
	cache.storeSuccess(16, []string{"model-a"}, time.Now())
	catalog := &ModelCatalogService{cache: cache}

	catalog.InvalidateAccount(16)

	_, ok := cache.load(16)
	require.False(t, ok)
}
