# Non-Passthrough API-Key Model Catalog Fix Design

## Background

The unified model catalog is intended to resolve every account independently:

- OAuth, setup-token, `upstream`, and explicitly passthrough API-key accounts may contribute a live upstream catalog.
- Non-passthrough API-key accounts contribute the client-visible model names declared by their model restrictions.

The current implementation violates this rule for Antigravity API-key accounts. Commit `77ebb829e` made every Antigravity `AccountTypeAPIKey` account live-catalog eligible. The periodic refresh runner can therefore cache the complete compatible-gateway model list. Once that cache is fresh, `ListForAccount` returns it before consulting the account's configured model restrictions, so public Model Plaza data can expose models outside that account's `model_mapping`.

This is not a frontend rendering defect. It is an account catalog-source policy defect.

## Existing Restriction Representation

The current account page presents “whitelist” and “mapping” editing modes, but both modes use `credentials.model_mapping` for current writes:

- A whitelist entry is encoded as an identity mapping, for example `model-a -> model-a`.
- A real mapping is encoded as a client source name mapped to an upstream target, for example `claude-sonnet-* -> claude-sonnet-4-6`.
- The client-visible/requestable model is always the source key.
- `credentials.model_whitelist` is a legacy compatibility field and is read only when no effective `model_mapping` source keys exist.

Historical or combined data may contain identity and non-identity entries together. This does not require separate catalog logic: every valid source key is a client-visible model pattern.

The gateway uses the same source-key semantics for account selection and request mapping. An empty mapping preserves the existing “unrestricted/default catalog” behavior.

## Root Cause

`accountRequiresLiveCatalog` currently classifies Antigravity API-key accounts as live:

```text
Antigravity OAuth      -> live
Antigravity API Key    -> live (incorrect for catalog aggregation)
Antigravity upstream   -> live
```

That classification affects all live-catalog paths:

1. `ListForAccount` reads and returns fresh cached live models.
2. cache misses may invoke the real discoverer.
3. public no-wait reads enqueue an asynchronous refresh.
4. the periodic runner refreshes the account.
5. group, API-key, channel, `/v1/models`, and Model Plaza aggregation consume that result.

The Antigravity API-key request builder itself is still useful for the explicit admin “sync upstream models” workflow. The bug is connecting that builder to the authoritative runtime catalog policy.

## Chosen Design

### 1. Correct the account catalog-source policy

Antigravity account policy becomes:

```text
OAuth account          -> live upstream discovery
API-key account        -> configured restriction/default catalog
upstream account       -> live upstream discovery
```

The Antigravity API-key type must return `false` from the live-catalog eligibility policy. Consequently it is excluded from synchronous discovery, asynchronous read refreshes, and periodic runner scans.

The existing Antigravity API-key discovery request builder remains available to the explicit admin sync action. This preserves the operator's ability to inspect upstream models while preventing that inspection result from becoming the Model Plaza source of truth.

### 2. Resolve configured restrictions consistently

For a non-live account, the catalog resolves models in this order:

1. Read non-empty source keys from `credentials.model_mapping`.
2. If there are no valid mapping source keys, read legacy `credentials.model_whitelist` entries.
3. Expand supported suffix wildcards against the maintained platform defaults.
4. If neither restriction contains a usable model, return the maintained platform default catalog.

Mapping targets are never displayed because clients request the source model name.

This is the existing `configuredAccountModelPatterns` behavior and should remain the single implementation path; no second Antigravity-specific parser is introduced.

### 3. Preserve aggregate union semantics

Each account contributes its own resolved list, and platform/group/public results remain a union.

Therefore, an Antigravity API-key account with two configured source names contributes only those two names. If the same public group also contains an OAuth or `upstream` account with a live catalog, models callable through that other account still appear in the Model Plaza. Account restrictions do not suppress capabilities supplied by another eligible account.

### 4. Ignore obsolete live cache entries by policy

`ListForAccount` already checks live eligibility before reading the live cache. After the policy correction, any previously cached Antigravity API-key catalog is ignored automatically. No cache schema or migration is needed.

Normal account mutation invalidation remains unchanged. A process restart or later cache invalidation removes obsolete entries, but correctness does not depend on immediate deletion.

## Platform Policy Boundaries

This fix does not redefine unrelated provider behavior:

- OpenAI API-key: live only when the existing OpenAI passthrough flag is enabled.
- Anthropic API-key: live only when the existing Anthropic passthrough flag is enabled.
- Gemini API-key: configured restriction/default catalog under the current policy.
- Antigravity API-key: configured restriction/default catalog.
- Windsurf and OpenCode API-key: retain their existing platform-native direct-upstream live policy; their routing treats maintained provider capabilities separately from account aliases.
- `AccountTypeUpstream`: remains inherently passthrough and live.
- OAuth/setup-token formats supported by the discoverer remain live.

## Error and Fallback Behavior

Configured Antigravity API-key resolution does not make a network request and therefore cannot fail due to the upstream model endpoint.

Malformed or empty restriction entries are ignored. If no usable source names remain, platform defaults are returned, matching the existing empty-restriction behavior. Legacy whitelist data remains readable during migration.

No public or admin response shape changes.

## Testing Strategy

Regression coverage must prove the behavior at the catalog boundary, not only helper functions:

1. An Antigravity API-key account with a real mapping returns mapping source names and makes zero discoverer calls.
2. An Antigravity API-key account with legacy whitelist data returns whitelist names and makes zero discoverer calls.
3. A pre-existing fresh live-cache entry for that account cannot override current restrictions.
4. `RefreshAll` does not scan or refresh Antigravity API-key accounts.
5. Public/group aggregation exposes only the configured contribution from that account.
6. Mixed-account aggregation still unions the restricted API-key contribution with OAuth/`upstream` live models.
7. Antigravity OAuth and `upstream` accounts remain live.
8. The explicit Antigravity API-key request-builder/admin-sync tests remain valid.

Tests must follow RED/GREEN: first update the catalog-level expectations so they fail against the current live classification, then make the smallest production policy change required to pass.

## Non-Goals

- Changing account-routing or model-mapping execution.
- Making the page modes destructive or strictly exclusive.
- Removing combined identity and non-identity entries from `model_mapping`.
- Removing legacy `model_whitelist` read compatibility.
- Removing the Antigravity admin upstream-sync action.
- Changing channel model mappings, group custom-list filtering, pricing, or public DTOs.
