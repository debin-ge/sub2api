# Unified Model Catalog Design

## Background

Model discovery is currently split across several independent code paths:

- `GatewayService.GetAvailableModels` collects `model_mapping` keys from schedulable accounts.
- `/v1/models` falls back to provider defaults only when that collected mapping list is empty.
- The public Model Plaza reads `/api/v1/channels/public`, which combines channel configuration, group configuration, and the gateway model list.
- The admin account page and group candidate page each maintain separate provider-specific default and mapping logic.
- Some providers already have live upstream model discovery, but it is exposed through account-testing or provider-specific helpers rather than a shared flow.

This causes an important mixed-account failure: if any account on a platform has a non-empty mapping, the runtime model list becomes mapping-only. A newly released model can therefore be callable through an unrestricted OAuth or passthrough account and visible in hard-coded console presets, while remaining absent from the Model Plaza and `/v1/models`.

OpenAI exposed the problem first, but the defect is platform-independent. The fix must unify discovery for every supported platform and migrate every model-list consumer.

## Goals

1. Discover newly available upstream models without requiring a release when an account type supports live discovery.
2. Resolve models per account before aggregating them by platform or group.
3. Respect non-passthrough API-key model restrictions.
4. Keep account, group, API-key, channel, and public model views consistent for the same scope.
5. Prevent public views from including models available only through exclusive groups.
6. Remain usable when an upstream model endpoint is unavailable or failing.
7. Reuse existing authentication, proxy, URL validation, header override, and response parsing behavior.

## Non-goals

- Changing request routing, account selection, model mapping execution, billing, or usage recording.
- Changing public or admin API response shapes.
- Adding a database-backed model catalog.
- Guaranteeing that every model returned by a third-party upstream supports every sub2api endpoint.
- Removing the legacy gateway model-list API in the first implementation.

## Supported Platforms

The unified flow covers every platform currently declared by the domain:

- Anthropic
- OpenAI
- Gemini
- Antigravity
- Grok
- MiniMax
- GLM
- Kimi
- DeepSeek
- Windsurf
- OpenCode

Platform-specific differences are limited to building and parsing a live model-list request and providing a static fallback list.

## Authoritative Logic Flow

### 1. Determine the target platforms and scope

The caller determines the platform scope before model discovery begins:

- Public Model Plaza: obtain distinct platforms from active, non-exclusive groups.
- `/v1/models`: use the current API key's group and platform.
- Group model candidates: use the selected group's platform.
- Admin account model page: use the selected account's platform.
- Channel model view: use platforms from the channel's associated groups.

The service must not enumerate a hard-coded platform list for a scoped request.

### 2. Load eligible accounts

For aggregate views, load accounts within the caller's group scope and then retain only accounts that:

- match the target platform;
- have active status; and
- are schedulable.

For the public view, accounts must be reachable through an active, non-exclusive group. Accounts reachable only through exclusive groups must never contribute models to the public result.

When an account belongs to multiple selected groups, it is resolved once by account ID.

The admin account model page may inspect the explicitly selected account even when it is currently disabled or unschedulable. Disabled accounts do not contribute to aggregate views.

### 3. Resolve each account independently

Each eligible account is resolved separately. The presence of a mapping on one account must not change how another account is resolved.

#### OAuth and Setup Token

1. Request the provider's live model catalog using the account's valid token.
2. On success, normalize and cache the complete non-empty model list.
3. On failure, use the last successful cache entry if it is not older than the stale limit.
4. If no usable cache exists, use the platform's maintained default catalog.

#### API Key with passthrough enabled

1. Request the model-list endpoint derived from the account's effective base URL.
2. Apply the account API key, proxy, header overrides, TLS behavior, and URL allowlist rules used by normal upstream requests.
3. On success, normalize and cache the complete non-empty model list.
4. On failure, use stale cache and then the platform default catalog.

#### Upstream account type

`upstream` accounts are inherently passthrough and use the same live-discovery and fallback path as passthrough API-key accounts.

#### API Key without passthrough

1. Read the requested model names from the source keys of `model_mapping`.
2. Support legacy `model_whitelist` values during migration.
3. If restrictions contain wildcard suffixes, expand them against the current platform default catalog and any usable last-success catalog for that account.
4. Never return a wildcard pattern as a concrete model ID.
5. When no mapping or whitelist is configured, preserve the current "empty restriction means allow all" behavior by returning the platform default catalog.

The mapping target is not displayed because clients call the source model name.

#### Bedrock, Service Account, and unsupported live-list formats

1. Use configured mapping or whitelist models when present.
2. Expand wildcard restrictions against the provider fallback catalog.
3. When no restriction exists, use the platform/provider fallback catalog.

Failure to support live discovery is a fallback condition, not a public request error.

### 4. Aggregate account results

For each target platform:

1. Union the model lists returned by all eligible accounts.
2. Trim whitespace and remove empty IDs.
3. Deduplicate case-insensitively while preserving the first original spelling.
4. Sort deterministically.
5. Exclude presentation-only internal routing IDs from the Model Plaza while preserving existing endpoint-specific behavior elsewhere.

A mixed account pool therefore behaves as a union. For example, a live OAuth catalog and a two-model restricted API-key account produce the union of both results.

### 5. Apply view-specific configuration

Discovery produces platform/account capabilities first. Existing configuration is applied afterward:

1. Add user-facing source aliases declared by channel model mappings.
2. Preserve the existing compatibility behavior in which an exact model declared by channel pricing can appear in channel/public display data.
3. Apply group `models_list_config` as the final group/API-key filter and ordering rule.
4. Attach pricing, group rate, recent-call, and channel metadata without changing discovery results.

Channel pricing remains presentation and billing metadata; it does not trigger live discovery.

## Service Shape

The implementation should remain compact rather than introducing a large provider framework.

### Model catalog service

Create one service responsible for:

- scope-to-account lookup;
- per-account resolution;
- model cleanup and union;
- wildcard expansion;
- group filtering; and
- provider fallback selection.

It exposes focused methods for the five consumers:

```go
ListForAccount(ctx context.Context, account *Account, waitForLive bool) ([]string, error)
ListForGroup(ctx context.Context, groupID int64, platform string) ([]string, error)
ListGroupCandidates(ctx context.Context, groupID int64, platform string) ([]string, error)
ListForAPIKey(ctx context.Context, apiKey *APIKey) ([]string, error)
ListPublic(ctx context.Context) (map[string][]string, error)
```

The exact public DTO remains in the handler. The catalog service returns model IDs, not pricing or page components.

### Reusable upstream discoverer

Extract the reusable request path currently owned by `AccountTestService.FetchUpstreamSupportedModels` into an upstream model discoverer. Both the admin sync action and the catalog service call it.

The discoverer retains provider-specific request construction but shares:

- token acquisition;
- proxy selection;
- base URL normalization and allowlist checks;
- header overrides;
- HTTP execution;
- the 8 MiB response limit;
- status validation;
- model ID extraction; and
- sanitized error classification.

Existing Antigravity, Windsurf, and OpenCode live-fetch helpers are reused from this layer. Providers without a working live endpoint return an unsupported error, which the catalog service converts to the documented fallback.

### Compatibility wrapper

Keep `GatewayService.GetAvailableModels` during the first migration. Change it to delegate to the unified service so remaining callers receive corrected per-account behavior. Delete it only after a later call-site audit.

## Cache and Refresh Design

### Cache entry

Use a process-local, account-level cache containing:

```text
account_id
models
fetched_at
source
last_refresh_error_at
```

Non-passthrough mappings and whitelists are read from the current account record and are not stored in the long-lived live-discovery cache.

No database migration is required.

### Configuration

Add:

```yaml
model_catalog:
  refresh_interval_seconds: 300
  request_timeout_seconds: 10
  stale_ttl_seconds: 86400
  failure_backoff_seconds: 60
  max_concurrency: 5
```

Validation rejects non-positive values. `max_concurrency` must be bounded to a safe operational range.

### Background refresh

- Start an asynchronous scan after service startup.
- Refresh active, schedulable accounts that require live discovery every five minutes by default.
- Limit total refresh concurrency to five by default.
- Apply a ten-second per-account timeout.
- Use `singleflight` per account so concurrent refresh triggers share one request.
- Stop the runner through context cancellation during service shutdown.

Account creation, credential changes, base URL changes, proxy changes, header override changes, passthrough mode changes, and re-enabling scheduling invalidate and enqueue that account. Disabling, deleting, or making an account unschedulable removes its cache entry.

### Read behavior

- Fresh cache: return immediately.
- Stale cache younger than 24 hours: return immediately and enqueue background refresh.
- Cache miss on the admin account page: allow one bounded synchronous discovery.
- Cache miss on `/v1/models`: allow a bounded discovery, then fall back.
- Cache miss on the public Model Plaza: return configured/provider fallback immediately and refresh asynchronously.

The anonymous public endpoint never waits for a fan-out across upstream accounts.

### Failure behavior

- Only a successful, non-empty model list replaces a cache entry.
- Empty, malformed, oversized, or non-2xx responses never erase the last successful entry.
- A failed account does not prevent other accounts from contributing models.
- Failure backoff prevents repeated requests for the same failing account.
- 401/403 handling may use the existing token refresh path once; repeated failure falls back without an unbounded retry loop.

### Public HTTP caching

Change the public catalog header from:

```http
Cache-Control: public, max-age=300
```

to:

```http
Cache-Control: public, max-age=60, stale-while-revalidate=300
```

With a five-minute account refresh interval, a newly released upstream model should normally appear in the Model Plaza within approximately six minutes.

## Consumer Migration

### Admin account models

Replace provider-specific default/mapping branches with `ListForAccount`. Preserve the response DTO and display metadata lookup.

### Group model candidates

Use `ListGroupCandidates` to return the unfiltered union available to the group, plus required configured aliases. The admin can then select the final `models_list_config` ordering.

### `/v1/models`

Use `ListForAPIKey`, then write the existing OpenAI- or Claude-shaped response. Preserve current API-key group scoping and custom list semantics.

### Public Model Plaza

Use `ListPublic` instead of `GatewayService.GetAvailableModels(ctx, nil, platform)`. Continue joining the returned IDs with channel pricing, public groups, recent call counts, and display-only routing filters.

### Channel support view

Use the unified group/platform catalog for each channel-associated group, then attach channel mapping, pricing, and multiplier information.

No frontend API contract change is required for any consumer.

## Security and Privacy

- Never expose account IDs, credentials, tokens, proxy URLs, or raw upstream errors in public responses.
- Log account ID, platform, error category, and status code only.
- Do not log full upstream response bodies.
- Reuse existing URL allowlist and private-host policy checks.
- Reuse account proxy and header overrides so discovery observes the same upstream path as inference.
- Keep response-size limits and context cancellation mandatory.

## Observability

Add counters and structured summaries for:

- refresh attempts by platform and result;
- cache hit, stale hit, and miss;
- fallback usage by platform and reason;
- accounts scanned, refreshed, succeeded, and failed per refresh pass; and
- refresh pass duration.

Metrics must not use account ID as a label to avoid high cardinality. Account IDs may appear in sanitized logs.

## Test Strategy

### Per-account resolution

Cover:

- OAuth live success, stale fallback, and provider fallback;
- Setup Token behavior;
- passthrough API-key live success and fallback;
- upstream account live behavior;
- non-passthrough whitelist-only behavior;
- non-passthrough mapping source-name behavior;
- empty restriction returning provider defaults;
- legacy `model_whitelist` compatibility;
- wildcard expansion without returning `*`;
- Bedrock, Service Account, and unsupported discovery fallbacks; and
- empty, duplicate, and case-variant cleanup.

### Aggregation and scoping

Cover:

- union of live OAuth and restricted API-key accounts;
- union of different passthrough upstream lists;
- isolation between platforms;
- exclusion of disabled, error, and unschedulable accounts;
- exclusion of exclusive-only accounts from public results;
- account deduplication across multiple public groups;
- group custom model filtering and ordering; and
- channel aliases and configured display models.

### Cache and runner

Cover:

- fresh hits avoiding upstream requests;
- stale-while-refresh behavior;
- cache misses and bounded synchronous refresh;
- empty responses preserving successful cache;
- per-account `singleflight`;
- failure backoff;
- account invalidation;
- concurrency limits;
- timeouts; and
- clean runner shutdown.

### HTTP regression

Cover:

- unchanged `/v1/models` response shapes;
- live-discovered models appearing in `/api/v1/channels/public`;
- admin account model consistency;
- group candidate consistency;
- channel model-view consistency;
- public cache headers; and
- existing Model Plaza frontend rendering without API changes.

## Acceptance Criteria

1. Any platform with working live discovery can expose a new upstream model without a sub2api release.
2. The new model appears in all applicable account, group, API-key, channel, and public views.
3. The normal public discovery delay is no more than approximately six minutes with default configuration.
4. A non-passthrough API-key account never contributes models outside its explicit restrictions, except that an empty restriction intentionally means the platform default catalog.
5. One mapped account cannot suppress models discovered from another account.
6. Upstream errors never replace a valid cached catalog with an empty list.
7. Exclusive-group-only accounts cannot affect the public catalog.
8. Existing endpoint paths, JSON shapes, routing, mapping execution, and billing behavior remain unchanged.
9. All existing tests and the new account, aggregation, cache, and HTTP regression tests pass.

## Implementation Sequence

1. Extract and test the reusable upstream discoverer.
2. Implement and test per-account resolution and aggregation.
3. Add cache, invalidation, and refresh runner behavior.
4. Migrate the admin account model page.
5. Migrate group candidates.
6. Migrate `/v1/models` and make the old gateway method a compatibility wrapper.
7. Migrate the Model Plaza and channel view.
8. Add observability, configuration validation, and full regression verification.
