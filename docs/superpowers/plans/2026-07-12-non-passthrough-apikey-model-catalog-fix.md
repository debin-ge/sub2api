# Non-Passthrough API-Key Model Catalog Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure Antigravity API-key accounts contribute only configured client-visible model restrictions to the unified catalog and Model Plaza, while preserving live discovery for Antigravity OAuth and `upstream` accounts.

**Architecture:** Keep `configuredAccountModelPatterns` as the single parser for current `model_mapping` source keys and legacy `model_whitelist` entries. Correct only the central live-catalog eligibility policy so Antigravity API-key accounts take the existing configured/default path before any live-cache read or refresh scheduling; retain the API-key request builder for explicit admin upstream sync.

**Tech Stack:** Go 1.24, `testing`, `testify/require`, process-local model catalog cache, Vue/Vitest regression gate.

## Global Constraints

- Current whitelist writes are identity entries in `credentials.model_mapping`; mapping targets are never displayed because clients request source names.
- Read `credentials.model_whitelist` only as a legacy fallback when no valid `model_mapping` source keys exist.
- Empty restrictions keep the existing platform-default catalog behavior.
- Antigravity OAuth and `AccountTypeUpstream` remain live-discovery accounts.
- Antigravity `AccountTypeAPIKey` must not synchronously discover, asynchronously refresh, enter the periodic runner, or consume a retained live-cache entry for catalog reads.
- Preserve the explicit Antigravity API-key admin upstream-sync/request-builder path.
- Preserve per-account union semantics: another OAuth or `upstream` account may still contribute live models to the same group or public platform.
- Do not change request routing, model mapping execution, page mode behavior, response DTOs, channel mappings, group custom-list filters, pricing, or billing.

---

### Task 1: Correct Antigravity API-Key Catalog Source Policy

**Files:**
- Modify: `backend/internal/service/model_catalog_service.go:667-690`
- Modify: `backend/internal/service/model_catalog_service_test.go:580-630`
- Modify: `backend/internal/service/model_catalog_refresh_runner_test.go:104-130`
- Modify: `backend/internal/service/upstream_models_test.go:391-438`

**Interfaces:**
- Consumes: `accountRequiresLiveCatalog(account *Account) bool`, `configuredOrDefaultAccountModels(account *Account, defaults []string) []string`, `ModelCatalogService.ListForAccount`, `ModelCatalogService.ListPublic`, and `ModelCatalogService.RefreshAll`.
- Produces: corrected Antigravity policy in `accountRequiresLiveCatalog`; no new production interface.

- [ ] **Step 1: Replace the incorrect real-discoverer expectations with configured restriction regressions**

In `backend/internal/service/upstream_models_test.go`, replace the two catalog-level Antigravity API-key tests with the following tests. Keep the lower-level `buildAntigravityAPIKeyModelsRequest` tests unchanged because explicit admin sync still uses that request path.

```go
func TestModelCatalogListForAccount_AntigravityAPIKeyUsesConfiguredMappingWithoutDiscovery(t *testing.T) {
	upstream := &antigravityUpstreamDiscoveryRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"gateway-live"}]}`)),
	}}
	discoverer := &UpstreamModelDiscoverer{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	catalog := NewModelCatalogService(nil, nil, nil, discoverer, config.ModelCatalogConfig{RequestTimeoutSeconds: 10})
	account := &Account{
		ID: 46, Platform: PlatformAntigravity, Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "gateway-key", "base_url": "https://gateway.example.com/antigravity",
			"model_mapping": map[string]any{
				"client-b": "gateway-b",
				"client-a": "gateway-a",
			},
		},
	}

	models, err := catalog.ListForAccount(context.Background(), account, true)

	require.NoError(t, err)
	require.Equal(t, []string{"client-a", "client-b"}, models)
	require.Nil(t, upstream.req)
	require.Zero(t, upstream.doCalls)
	require.Zero(t, upstream.doWithTLSCalls)
}

func TestModelCatalogListForAccount_AntigravityAPIKeyLegacyWhitelistIgnoresFreshLiveCache(t *testing.T) {
	discoverer := &recordingModelDiscoverer{
		models: map[int64][]string{47: {"unexpected-live"}},
		errs:   map[int64]error{},
	}
	catalog := NewModelCatalogService(nil, nil, nil, discoverer, config.ModelCatalogConfig{RequestTimeoutSeconds: 10})
	catalog.cache.storeSuccess(47, []string{"cached-live"}, catalog.currentTime())
	account := &Account{
		ID: 47, Platform: PlatformAntigravity, Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "gateway-key", "base_url": "https://gateway.example.com/antigravity",
			"model_whitelist": []any{"legacy-b", "legacy-a"},
		},
	}

	models, err := catalog.ListForAccount(context.Background(), account, true)

	require.NoError(t, err)
	require.Equal(t, []string{"legacy-a", "legacy-b"}, models)
	require.Zero(t, discoverer.calls[account.ID])
}
```

- [ ] **Step 2: Add the public mixed-account regression**

Add this test next to `TestModelCatalogPublicScope` in `backend/internal/service/model_catalog_service_test.go`. It proves that a retained API-key live cache cannot leak into Model Plaza, while an OAuth account in the same group still contributes its own live cache.

```go
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
```

- [ ] **Step 3: Change the runner regression to require exclusion**

Replace `TestModelCatalogRefreshAllIncludesAntigravityAPIKey` in `backend/internal/service/model_catalog_refresh_runner_test.go` with:

```go
func TestModelCatalogRefreshAllExcludesAntigravityAPIKey(t *testing.T) {
	account := Account{
		ID: 7, Platform: PlatformAntigravity, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{
			"api_key": "gateway-key",
			"base_url": "https://gateway.example.com/antigravity",
			"model_mapping": map[string]any{"client-model": "gateway-model"},
		},
	}
	var calls atomic.Int64
	catalog := &ModelCatalogService{
		accountRepo: &refreshRunnerAccountRepoStub{accounts: []Account{account}},
		discoverer: modelDiscovererFunc(func(context.Context, *Account) ([]string, error) {
			calls.Add(1)
			return []string{"gateway-live"}, nil
		}),
		cfg:   config.ModelCatalogConfig{RequestTimeoutSeconds: 10, FailureBackoffSeconds: 60, MaxConcurrency: 1},
		cache: newModelCatalogCache(), refreshSem: make(chan struct{}, 1), now: time.Now,
	}

	summary := catalog.RefreshAll(context.Background())

	require.Zero(t, calls.Load())
	require.NotContains(t, summary.ByPlatform, PlatformAntigravity)
}
```

- [ ] **Step 4: Run the new regressions and verify RED**

Run:

```bash
cd backend
go test ./internal/service -run 'Test(ModelCatalogListForAccount_AntigravityAPIKey|ModelCatalogPublicAntigravityAPIKeyUsesConfiguredRestrictionDespiteLiveCache|ModelCatalogRefreshAllExcludesAntigravityAPIKey)' -count=1 -v
```

Expected: FAIL against the current code because:

- the direct account test returns `gateway-live` and records an upstream call;
- the legacy-whitelist test returns `cached-live`;
- the public test contains `api-key-cached-live` instead of `client-visible`;
- the runner test records one discovery call and one scanned Antigravity account.

- [ ] **Step 5: Implement the minimal catalog policy correction**

In `backend/internal/service/model_catalog_service.go`, change only the Antigravity case in `accountRequiresLiveCatalog`:

```go
case PlatformAntigravity:
	return account.Type == AccountTypeOAuth ||
		account.Type == AccountTypeUpstream
```

Do not remove `buildAntigravityAPIKeyModelsRequest`; it remains part of explicit admin upstream sync.

- [ ] **Step 6: Run the focused tests and verify GREEN**

Run:

```bash
cd backend
go test ./internal/service -run 'Test(ModelCatalogListForAccount_AntigravityAPIKey|ModelCatalogPublicAntigravityAPIKeyUsesConfiguredRestrictionDespiteLiveCache|ModelCatalogRefreshAllExcludesAntigravityAPIKey|ModelCatalogLivePolicies|BuildAntigravityAPIKeyModelsRequest)' -count=1 -v
```

Expected: PASS. The configured mapping/legacy whitelist tests make zero discovery calls; OAuth and `upstream` live-policy tests and the explicit API-key request-builder test remain green.

- [ ] **Step 7: Run package and race verification**

Run:

```bash
cd backend
go test ./internal/service -count=1
go test -race ./internal/service -run 'Test(ModelCatalogListForAccount_AntigravityAPIKey|ModelCatalogPublicAntigravityAPIKeyUsesConfiguredRestrictionDespiteLiveCache|ModelCatalogRefreshAllExcludesAntigravityAPIKey|ModelCatalogLivePolicies)' -count=1
```

Expected: both commands exit `0`.

- [ ] **Step 8: Commit the behavior fix**

```bash
git add \
  backend/internal/service/model_catalog_service.go \
  backend/internal/service/model_catalog_service_test.go \
  backend/internal/service/model_catalog_refresh_runner_test.go \
  backend/internal/service/upstream_models_test.go
git commit -m "fix(model-catalog): honor antigravity api key restrictions"
```

- [ ] **Step 9: Run final branch gates**

Run:

```bash
cd backend
go test ./... -count=1
go vet ./internal/service ./internal/handler/... ./cmd/server
go generate ./cmd/server
git diff --exit-code -- cmd/server/wire_gen.go
go build -o /tmp/sub2api-model-catalog-fix ./cmd/server
rm -f /tmp/sub2api-model-catalog-fix

cd ../frontend
pnpm typecheck
pnpm test:run src/views/__tests__/PlazaView.spec.ts
pnpm build

cd ..
git diff --check
git status --short --branch
```

Expected:

- all backend tests pass;
- vet, Wire consistency, and server build exit `0`;
- frontend typecheck passes;
- PlazaView reports 6 passing tests;
- frontend production build exits `0` with only known non-blocking bundling/Browserslist warnings;
- no `backend/server` artifact exists;
- the branch worktree is clean.
