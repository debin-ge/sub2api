# Domestic Provider Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` for parallel implementation, or `superpowers:executing-plans` for inline execution. Track progress by changing each `- [ ]` checkbox to `- [x]` immediately after the step is completed and verified.

**Goal:** Implement phase two operating capabilities for MiniMax, GLM, Kimi, and DeepSeek gateways: model discovery, channel health monitoring, quota/balance synchronization, and model alias compatibility.

**Architecture:** Add a shared domestic-provider capability layer that owns default models, model aliases, monitor request metadata, and model-list exposure. Gateway request validation and upstream rewrite call the same resolver, `/v1/models` reads the same provider model list, Channel Monitor uses provider adapters, MiniMax quota synchronization calibrates Redis against official remains, and DeepSeek health records official balance availability without inventing remains for GLM/Kimi.

**Tech Stack:** Go 1.23, Gin, Ent, Redis Lua scripts, Wire provider assembly, Vue 3, TypeScript, Vitest, pnpm.

---

## Starting Point

Branch and workspace:

- Branch: `feature/domestic-provider-phase2`
- Worktree: `/Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2`
- Base commit: `8c4bc471 docs(gateway): add domestic provider phase two design`

Baseline checks already run from the worktree:

```bash
cd /Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2/backend
go test ./internal/service ./internal/handler ./internal/server/routes
```

Result:

```text
ok github.com/Wei-Shaw/sub2api/internal/service 43.453s
ok github.com/Wei-Shaw/sub2api/internal/handler 22.534s
ok github.com/Wei-Shaw/sub2api/internal/server/routes 2.548s
```

```bash
cd /Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2
pnpm --dir frontend run typecheck
```

Result: exit code `0`.

Primary design input:

- `docs/superpowers/specs/2026-05-08-domestic-provider-phase2-design.md`

Non-goals for this phase:

- Do not create fake remains or fake balance for GLM or Kimi.
- Do not delete real MiniMax Redis request records during calibration.
- Do not change unrelated OpenAI, Anthropic, Gemini behavior.
- Do not broaden account routing semantics beyond model listing, alias resolution, health monitoring, MiniMax remains, and DeepSeek balance health.

---

## Shared Contracts

### Provider default models

Use the exact public model ids below unless the implementation reads a stricter configured list from an existing account model mapping.

```go
var domesticProviderDefaultModels = map[string][]string{
    PlatformMiniMax: []string{"MiniMax-M2.7", "MiniMax-M2.7-highspeed"},
    PlatformGLM:     []string{"GLM-5.1", "GLM-4.7", "GLM-4.5-air"},
    PlatformKimi:    []string{"kimi-for-coding"},
    PlatformDeepSeek: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
}
```

### Alias table

Alias resolution is provider-scoped. Account `model_mapping` remains the highest priority. Default aliases are applied only when account mapping does not produce a different upstream model.

| Provider | External alias | Upstream model |
| --- | --- | --- |
| MiniMax | `claude-sonnet-4-5` | `MiniMax-M2.7` |
| MiniMax | `claude-3-5-sonnet-latest` | `MiniMax-M2.7` |
| MiniMax | `claude-sonnet-*` | `MiniMax-M2.7` |
| MiniMax | `claude-haiku-*` | `MiniMax-M2.7-highspeed` |
| GLM | `claude-sonnet-*` | `GLM-5.1` |
| GLM | `claude-opus-*` | `GLM-5.1` |
| GLM | `claude-haiku-*` | `GLM-4.5-air` |
| Kimi | `claude-sonnet-4-5` | `kimi-for-coding` |
| Kimi | `claude-3-5-sonnet-latest` | `kimi-for-coding` |
| Kimi | `claude-sonnet-*` | `kimi-for-coding` |
| DeepSeek | `deepseek-chat` | `deepseek-v4-flash` |
| DeepSeek | `deepseek-v3` | `deepseek-v4-flash` |
| DeepSeek | `deepseek-reasoner` | `deepseek-v4-pro` |
| DeepSeek | `deepseek-r1` | `deepseek-v4-pro` |

### Account extra keys

MiniMax remains keys:

- `minimax_text_5h_limit`
- `minimax_text_5h_remaining`
- `minimax_remains_synced_at`
- `minimax_remains_sync_status`
- `minimax_remains_sync_error`
- `minimax_remains_local_used`
- `minimax_remains_synthetic_added`
- `minimax_remains_synthetic_removed`
- `minimax_remains_calibrated_at`

DeepSeek balance keys:

- `deepseek_balance_available`
- `deepseek_balance_amount`
- `deepseek_balance_currency`
- `deepseek_balance_checked_at`
- `deepseek_balance_status`
- `deepseek_balance_error`

These metadata keys must be scheduler-neutral in `backend/internal/repository/account_repo.go`, so periodic health updates do not churn account scheduling order.

### Config additions

Add fields under `GatewayConfig` in `backend/internal/config/config.go`:

```go
type GatewayModelAliasConfig struct {
    Enabled         bool `mapstructure:"enabled" json:"enabled"`
    IncludeInModels bool `mapstructure:"include_in_models" json:"include_in_models"`
}

type GatewayMiniMaxRemainsConfig struct {
    SyncEnabled       bool `mapstructure:"sync_enabled" json:"sync_enabled"`
    SyncIntervalSeconds int `mapstructure:"sync_interval_seconds" json:"sync_interval_seconds"`
    SyncJitterSeconds   int `mapstructure:"sync_jitter_seconds" json:"sync_jitter_seconds"`
    BatchSize           int `mapstructure:"batch_size" json:"batch_size"`
    StaleAfterSeconds   int `mapstructure:"stale_after_seconds" json:"stale_after_seconds"`
}

type GatewayDeepSeekBalanceConfig struct {
    CheckEnabled        bool `mapstructure:"check_enabled" json:"check_enabled"`
    CheckIntervalSeconds int `mapstructure:"check_interval_seconds" json:"check_interval_seconds"`
    CheckJitterSeconds   int `mapstructure:"check_jitter_seconds" json:"check_jitter_seconds"`
    BatchSize            int `mapstructure:"batch_size" json:"batch_size"`
    StaleAfterSeconds    int `mapstructure:"stale_after_seconds" json:"stale_after_seconds"`
}
```

Defaults:

- `gateway.model_aliases.enabled = true`
- `gateway.model_aliases.include_in_models = false`
- `gateway.minimax_remains.sync_enabled = true`
- `gateway.minimax_remains.sync_interval_seconds = 300`
- `gateway.minimax_remains.sync_jitter_seconds = 30`
- `gateway.minimax_remains.batch_size = 50`
- `gateway.minimax_remains.stale_after_seconds = 900`
- `gateway.deepseek_balance.check_enabled = true`
- `gateway.deepseek_balance.check_interval_seconds = 300`
- `gateway.deepseek_balance.check_jitter_seconds = 30`
- `gateway.deepseek_balance.batch_size = 50`
- `gateway.deepseek_balance.stale_after_seconds = 900`

Validation:

- intervals: `60..3600`
- jitter: `0..300`, and less than interval
- batch size: `1..200`
- stale seconds: greater than interval and less than or equal to `86400`

---

## Task 1: Add Shared Domestic Provider Capability Layer

Files to create:

- `backend/internal/service/provider_gateway_capabilities.go`
- `backend/internal/service/model_alias_resolver.go`
- `backend/internal/service/gateway_model_list_provider.go`
- `backend/internal/service/provider_gateway_capabilities_test.go`
- `backend/internal/service/model_alias_resolver_test.go`
- `backend/internal/service/gateway_model_list_provider_test.go`

Files to modify:

- `backend/internal/service/account.go`
- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`

Steps:

- [x] Add `ProviderGatewayCapabilities` with provider id, default model ids, public model ids, alias rules, and monitor defaults for MiniMax, GLM, Kimi, and DeepSeek.
- [x] Move GLM/Kimi/DeepSeek default model list ownership into the shared capability layer while preserving existing exported helper functions such as `DefaultGLMModelIDs`, `DefaultKimiModelIDs`, and `DefaultDeepSeekModelIDs`.
- [x] Add `DefaultMiniMaxModelIDs` with `MiniMax-M2.7` and `MiniMax-M2.7-highspeed`.
- [x] Add a provider-scoped alias resolver that supports exact match and trailing wildcard patterns.
- [x] Make account-level `model_mapping` higher priority than default alias rules.
- [x] Add config structs and defaults for `gateway.model_aliases`, `gateway.minimax_remains`, and `gateway.deepseek_balance`.
- [x] Add config validation tests for default values and invalid interval, jitter, batch, and stale values.
- [x] Run service and config tests for this task.

Resolver shape:

```go
type ModelAliasRule struct {
    AliasPattern string
    TargetModel  string
}

type ModelAliasResolution struct {
    RequestedModel string
    UpstreamModel  string
    Provider       string
    Source         string
    MatchedPattern string
}

func ResolveProviderModelAlias(provider, requested string) (ModelAliasResolution, bool) {
    caps, ok := GetProviderGatewayCapabilities(provider)
    if !ok {
        return ModelAliasResolution{}, false
    }
    for _, rule := range caps.AliasRules {
        if matchModelPattern(rule.AliasPattern, requested) {
            return ModelAliasResolution{
                RequestedModel: requested,
                UpstreamModel:  rule.TargetModel,
                Provider:       provider,
                Source:         "provider_default_alias",
                MatchedPattern: rule.AliasPattern,
            }, true
        }
    }
    return ModelAliasResolution{}, false
}
```

Acceptance checks:

```bash
cd /Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2/backend
go test ./internal/service -run 'TestProviderGatewayCapabilities|TestResolveProviderModelAlias|TestGatewayModelListProvider'
go test ./internal/config -run 'TestGateway'
```

---

## Task 2: Implement Domestic `/v1/models`

Files to modify:

- `backend/internal/handler/gateway_handler.go`
- `backend/internal/server/routes/gateway.go`
- `backend/internal/service/gateway_service.go`
- `backend/internal/service/account.go`

Files to create or extend:

- `backend/internal/handler/gateway_handler_models_test.go`
- `backend/internal/server/routes/gateway_test.go`
- `backend/internal/service/gateway_service_models_test.go`

Steps:

- [x] Remove the MiniMax `/v1/models` unsupported block from `backend/internal/server/routes/gateway.go`.
- [x] Keep MiniMax unsupported only for endpoints that are genuinely unsupported.
- [x] Route `/v1/models` for MiniMax, GLM, Kimi, and DeepSeek through `GatewayModelListProvider`.
- [x] Return Claude-style `list` model payloads consistently with current `writeClaudeModelList`.
- [x] Include default public model ids even when no account has explicit model mapping.
- [x] Merge account model mapping keys when schedulable accounts exist and the mapped values are provider-supported.
- [x] Exclude wildcard mapping keys from `/v1/models`.
- [x] Include aliases in `/v1/models` only when `gateway.model_aliases.include_in_models` is true.
- [x] Preserve existing GLM `/v1/models` behavior for normalized `GLM-*` model ids.
- [x] Add tests covering MiniMax, GLM, Kimi, DeepSeek, empty account list fallback, account mapping merge, wildcard exclusion, and alias inclusion config.

Response contract:

```json
{
  "type": "list",
  "data": [
    {
      "id": "MiniMax-M2.7",
      "type": "model",
      "display_name": "MiniMax-M2.7",
      "created_at": "2025-01-01T00:00:00Z"
    }
  ]
}
```

Acceptance checks:

```bash
cd /Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2/backend
go test ./internal/handler -run 'TestGatewayModels'
go test ./internal/server/routes -run 'TestGatewayRoutesModels'
go test ./internal/service -run 'TestGatewayModelList'
```

---

## Task 3: Wire Alias Resolution Into Gateway Request Flow

Files to modify:

- `backend/internal/service/account.go`
- `backend/internal/service/gateway_service.go`
- `backend/internal/service/minimax_gateway_service.go`
- `backend/internal/service/glm_gateway_service.go`
- `backend/internal/service/kimi_gateway_service.go`
- `backend/internal/service/deepseek_gateway_service.go`
- `backend/internal/service/channel.go`

Files to create or extend:

- `backend/internal/service/account_model_alias_test.go`
- `backend/internal/service/minimax_gateway_service_test.go`
- `backend/internal/service/glm_gateway_service_test.go`
- `backend/internal/service/kimi_gateway_service_test.go`
- `backend/internal/service/deepseek_gateway_service_test.go`

Steps:

- [x] Update MiniMax model mapping so account `model_mapping` is checked first and default aliases are checked second.
- [x] Add Kimi and DeepSeek provider alias mapping helpers with the same priority rule.
- [x] Refactor GLM default alias behavior to call the shared resolver while keeping existing wildcard outcomes unchanged.
- [x] Update provider-specific payload validation so alias requests are accepted when they resolve to a supported upstream model.
- [x] Rewrite upstream request payload model to the resolved provider model before forwarding.
- [x] Record requested model, channel mapped model, and model mapping chain using existing `ChannelUsageFields` fields.
- [x] Ensure billing model source remains provider/upstream-aware and does not bill under an alias when upstream rewrote it.
- [x] Add tests proving unsupported aliases still fail with current error semantics.

Mapping priority:

```text
client requested model
  -> account model_mapping exact or wildcard
  -> provider default alias exact or wildcard
  -> official provider model passthrough
  -> unsupported model error
```

Acceptance checks:

```bash
cd /Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2/backend
go test ./internal/service -run 'Test.*ModelAlias|TestMiniMax.*Model|TestGLM.*Model|TestKimi.*Model|TestDeepSeek.*Model'
```

---

## Task 4: Extend Channel Monitor To Four Domestic Providers

Files to modify:

- `backend/internal/service/channel_monitor_const.go`
- `backend/internal/service/channel_monitor_checker.go`
- `backend/internal/service/channel_monitor_validate.go`
- `backend/internal/service/channel_monitor_template_types.go`
- `backend/internal/handler/admin/channel_monitor_handler.go`
- `backend/internal/handler/admin/channel_monitor_template_handler.go`

Files to create or extend:

- `backend/internal/service/channel_monitor_checker_test.go`
- `backend/internal/service/channel_monitor_validate_test.go`
- `backend/internal/handler/admin/channel_monitor_handler_test.go`
- `backend/internal/handler/admin/channel_monitor_template_handler_test.go`

Steps:

- [x] Add provider constants: `minimax`, `glm`, `kimi`, `deepseek`.
- [x] Update validation messages and binding tags to allow `openai`, `anthropic`, `gemini`, `minimax`, `glm`, `kimi`, and `deepseek`.
- [x] Add monitor adapters for each domestic provider.
- [x] Keep endpoint validation origin-only and put provider-specific API prefixes in adapter paths.
- [x] Add provider body merge deny-list entries to prevent overriding `model`, `messages`, and provider-required request fields.
- [x] Add monitor template validation for the four providers.
- [x] Add checker tests for path, body, headers, and text extraction per provider.

Monitor adapter paths:

```go
var providerAdapters = map[string]providerAdapter{
    MonitorProviderMiniMax: {
        path:     "/anthropic/v1/messages",
        textPath: "content.0.text",
    },
    MonitorProviderGLM: {
        path:     "/api/anthropic/v1/messages",
        textPath: "content.0.text",
    },
    MonitorProviderKimi: {
        path:     "/coding/v1/messages",
        textPath: "content.0.text",
    },
    MonitorProviderDeepSeek: {
        path:     "/chat/completions",
        textPath: "choices.0.message.content",
    },
}
```

Headers:

- MiniMax: `Authorization: Bearer <key>`, Anthropic-compatible body
- GLM: `Authorization: Bearer <key>`, Anthropic-compatible body
- Kimi: `Authorization: Bearer <key>`, Anthropic-compatible body
- DeepSeek: `Authorization: Bearer <key>`, OpenAI-compatible body

Acceptance checks:

```bash
cd /Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2/backend
go test ./internal/service -run 'TestChannelMonitor'
go test ./internal/handler/admin -run 'TestChannelMonitor'
```

---

## Task 5: Implement MiniMax Remains Sync And Redis Calibration

Files to modify:

- `backend/internal/service/minimax_quota_port.go`
- `backend/internal/service/minimax_quota_service.go`
- `backend/internal/repository/minimax_quota_cache.go`
- `backend/internal/repository/account_repo.go`
- `backend/internal/service/wire.go`
- `backend/internal/service/minimax_token_plan_client.go`
- `backend/internal/config/config.go`

Files to create or extend:

- `backend/internal/service/minimax_remains_sync_service.go`
- `backend/internal/service/minimax_remains_sync_runner.go`
- `backend/internal/service/minimax_remains_sync_service_test.go`
- `backend/internal/service/minimax_quota_service_test.go`
- `backend/internal/repository/minimax_quota_cache_integration_test.go`

Steps:

- [x] Extend `MiniMaxQuotaCache` with `CalibrateTextRequests(ctx, accountID, targetUsed, windowSeconds)`.
- [x] Implement Redis calibration with synthetic ZSET members prefixed by `official:`.
- [x] During calibration, add synthetic members when official used is greater than local Redis count.
- [x] During calibration, remove only synthetic members when official used is less than local Redis count.
- [x] Never remove real request members created by `ReserveTextRequest`.
- [x] Add TTL refresh for the quota ZSET after calibration.
- [x] Add `MiniMaxRemainsSyncService` that fetches official remains, writes account extra keys, and calls quota calibration.
- [x] Add `MiniMaxRemainsSyncRunner` with interval, jitter, batch size, and graceful shutdown.
- [x] Wire the runner through `backend/internal/service/wire.go` only when config enables it.
- [x] Update `MiniMaxQuotaService` to honor fresh official remains by blocking when official remaining is zero and recording a clear rejection reason.
- [x] Add scheduler-neutral handling for the MiniMax metadata keys in `account_repo.go`.
- [x] Keep existing manual MiniMax remains sync behavior compatible with the new sync service.

Calibration algorithm:

```text
limit = official text_5h_limit
remaining = official text_5h_remaining
target_used = max(limit - remaining, 0)
local_total = count(real request members + synthetic official members)

if target_used > local_total:
  add target_used - local_total synthetic members with score now

if target_used < local_total:
  remove min(local_total - target_used, synthetic_count) synthetic members ordered by score

real request members are retained
```

Runner behavior:

- `SyncAll` lists MiniMax accounts with `ListByPlatform(PlatformMiniMax)`.
- It processes only accounts where `IsMiniMaxTokenPlan(account)` is true.
- It updates success keys and clears `minimax_remains_sync_error` on success.
- It writes `minimax_remains_sync_status = "error"` and `minimax_remains_sync_error` on failure.
- It continues with the next account when one account fails.

Acceptance checks:

```bash
cd /Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2/backend
go test ./internal/service -run 'TestMiniMaxRemainsSync|TestMiniMaxQuota'
go test -tags=integration ./internal/repository -run 'TestMiniMaxQuotaCache'
```

---

## Task 6: Implement DeepSeek Balance Health Check

Files to modify:

- `backend/internal/service/account_service.go`
- `backend/internal/repository/account_repo.go`
- `backend/internal/handler/admin/account_handler.go`
- `backend/internal/server/routes/admin.go`
- `backend/internal/service/wire.go`
- `backend/internal/config/config.go`

Files to create or extend:

- `backend/internal/service/deepseek_balance_client.go`
- `backend/internal/service/deepseek_balance_health_service.go`
- `backend/internal/service/deepseek_balance_health_runner.go`
- `backend/internal/service/deepseek_balance_client_test.go`
- `backend/internal/service/deepseek_balance_health_service_test.go`
- `backend/internal/handler/admin/account_handler_deepseek_test.go`

Steps:

- [x] Add a DeepSeek balance client that calls `GET <base_url>/user/balance` with bearer auth.
- [x] Parse `is_available`, `balance_infos[].currency`, and `balance_infos[].total_balance`.
- [x] Prefer CNY when multiple balances exist; otherwise use the first balance entry.
- [x] Add `DeepSeekBalanceHealthService` that updates account extra keys and marks unavailable balances as unhealthy.
- [x] Add `DeepSeekBalanceHealthRunner` with interval, jitter, batch size, and graceful shutdown.
- [x] Add an admin manual check route for a single DeepSeek account.
- [x] Add scheduler-neutral handling for DeepSeek balance metadata keys in `account_repo.go`.
- [x] Do not gate request routing solely on DeepSeek balance unless the existing health policy already rejects unhealthy accounts.

Expected upstream response:

```json
{
  "is_available": true,
  "balance_infos": [
    {
      "currency": "CNY",
      "total_balance": "10.00",
      "granted_balance": "0.00",
      "topped_up_balance": "10.00"
    }
  ]
}
```

Stored success metadata:

```json
{
  "deepseek_balance_available": true,
  "deepseek_balance_amount": "10.00",
  "deepseek_balance_currency": "CNY",
  "deepseek_balance_checked_at": "2026-05-12T00:00:00Z",
  "deepseek_balance_status": "ok"
}
```

Acceptance checks:

```bash
cd /Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2/backend
go test ./internal/service -run 'TestDeepSeekBalance'
go test ./internal/handler/admin -run 'Test.*DeepSeek.*Balance'
```

---

## Task 7: Update Frontend Admin UI

Files to modify:

- `frontend/src/api/admin/channelMonitor.ts`
- `frontend/src/constants/channelMonitor.ts`
- `frontend/src/components/admin/monitor/MonitorFormDialog.vue`
- `frontend/src/components/admin/monitor/MonitorFiltersBar.vue`
- `frontend/src/components/admin/monitor/MonitorTemplateManagerDialog.vue`
- `frontend/src/composables/useChannelMonitorFormat.ts`
- `frontend/src/i18n/locales/zh.ts`
- `frontend/src/i18n/locales/en.ts`
- `frontend/src/composables/useModelWhitelist.ts`
- `frontend/src/types/index.ts`
- `frontend/src/components/account/AccountCapacityCell.vue`
- `frontend/src/api/admin/accounts.ts`

Files to create or extend:

- `frontend/src/composables/__tests__/useModelWhitelist.spec.ts`
- `frontend/src/components/account/__tests__/AccountCapacityCell.spec.ts`
- `frontend/src/components/admin/monitor/__tests__/MonitorFormDialog.spec.ts`
- `frontend/src/components/admin/monitor/__tests__/MonitorTemplateManagerDialog.spec.ts`

Steps:

- [x] Add provider union values `minimax`, `glm`, `kimi`, and `deepseek` to admin channel monitor API types.
- [x] Add provider constants, provider list entries, labels, badges, and picker styles for all four providers.
- [x] Update monitor create/update forms to offer all supported providers.
- [x] Update monitor filters and template manager tabs to include all supported providers.
- [x] Add i18n labels for `minimax` and `kimi`; keep existing `glm` and `deepseek` labels.
- [x] Update Kimi model presets to include Claude Sonnet aliases that map to `kimi-for-coding`.
- [x] Update DeepSeek model presets to include `deepseek-chat`, `deepseek-v3`, `deepseek-reasoner`, and `deepseek-r1`.
- [x] Preserve MiniMax and GLM presets already present in the UI.
- [x] Extend `Account.extra` typing with DeepSeek balance keys.
- [x] Add DeepSeek balance display to `AccountCapacityCell`.
- [x] Confirm GLM/Kimi account rows do not display fake remains or fake balance.
- [x] Add an account API helper for the DeepSeek manual balance check route.

Frontend UI constraints:

- Keep monitor provider controls dense and utilitarian.
- Do not add a marketing page or explanatory panel.
- Use existing component patterns and existing badge visual language.
- Ensure provider text fits in compact filter controls.

Acceptance checks:

```bash
cd /Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2
pnpm --dir frontend run typecheck
pnpm --dir frontend exec vitest run src/composables/__tests__/useModelWhitelist.spec.ts src/components/account/__tests__/AccountCapacityCell.spec.ts
pnpm --dir frontend exec vitest run src/components/admin/monitor/__tests__/MonitorFormDialog.spec.ts src/components/admin/monitor/__tests__/MonitorTemplateManagerDialog.spec.ts
```

---

## Task 8: Add Documentation And Operational Smoke Tests

Files to create or modify:

- `docs/superpowers/specs/2026-05-08-domestic-provider-phase2-design.md`
- `docs/superpowers/plans/2026-05-12-domestic-provider-phase2-execution.md`
- `docs/DOMESTIC_PROVIDER_PHASE2_RUNBOOK_CN.md`

Steps:

- [x] Update the existing phase two design document only where implementation details have become more precise.
- [x] Add a Chinese runbook for enabling aliases, reading `/v1/models`, interpreting MiniMax remains, interpreting DeepSeek balance, and configuring Channel Monitor providers.
- [x] Add curl examples for each domestic `/v1/models` route.
- [x] Add curl examples for MiniMax remains manual sync and DeepSeek balance manual check.
- [x] Add an operator note that GLM and Kimi use generic health monitoring only.

Smoke examples:

```bash
curl -sS "$BASE_URL/v1/models" -H "Authorization: Bearer $MINIMAX_GROUP_TOKEN"
curl -sS "$BASE_URL/v1/models" -H "Authorization: Bearer $GLM_GROUP_TOKEN"
curl -sS "$BASE_URL/v1/models" -H "Authorization: Bearer $KIMI_GROUP_TOKEN"
curl -sS "$BASE_URL/v1/models" -H "Authorization: Bearer $DEEPSEEK_GROUP_TOKEN"
```

Final full verification:

```bash
cd /Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2/backend
go test ./internal/service ./internal/handler ./internal/server/routes
go test -tags=integration ./internal/repository -run 'TestMiniMaxQuotaCache'
```

```bash
cd /Users/gedebin/Documents/Code/sub2api/.worktrees/feature-domestic-provider-phase2
pnpm --dir frontend run typecheck
pnpm --dir frontend exec vitest run src/composables/__tests__/useModelWhitelist.spec.ts src/components/account/__tests__/AccountCapacityCell.spec.ts src/components/admin/monitor/__tests__/MonitorFormDialog.spec.ts src/components/admin/monitor/__tests__/MonitorTemplateManagerDialog.spec.ts
```

---

## Implementation Order

Recommended order:

- [x] Task 1 first, because all later work should depend on one provider capability source.
- [x] Task 2 second, because `/v1/models` is externally visible and validates capability list shape early.
- [x] Task 3 third, because request flow alias support should share the resolver already tested in Task 1.
- [x] Task 4 fourth, because Channel Monitor is mostly independent after provider constants are in place.
- [x] Task 5 fifth, because MiniMax Redis calibration touches cache scripts and quota service behavior.
- [x] Task 6 sixth, because DeepSeek balance health shares runner/config/repository patterns with Task 5.
- [x] Task 7 seventh, after backend API shapes are stable.
- [x] Task 8 last, after behavior and names are final.

Parallelization guidance:

- Backend Task 1 and frontend Task 7 should not run in parallel because frontend model presets depend on final alias names from Task 1.
- Task 4 can run in parallel with Task 5 after provider constants are added.
- Task 6 can run in parallel with Task 5 if the workers coordinate changes to `config.go`, `wire.go`, `account_repo.go`, and account admin routes.
- Task 8 should run after all behavior tasks finish.

Merge discipline:

- Keep generated Wire output consistent with the repository’s existing generation pattern.
- Re-run targeted tests after each task.
- Re-run final full verification before marking this plan complete.
