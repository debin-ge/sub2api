// Local second-development messages that do not exist in the upstream modular locale.
// Keep this file additive: upstream messages remain authoritative for overlapping keys.
export default {
  "radar": {
    "pageTitle": "Model Radar",
    "common": {
      "date": "Date",
      "never": "Not yet",
      "stale": "Data may be outdated",
      "unknownTime": "Unknown time"
    },
    "header": {
      "lightTheme": "Switch to light theme",
      "darkTheme": "Switch to dark theme",
      "dashboard": "Dashboard",
      "login": "Log in",
      "sectionNavigation": "Radar sections",
      "health": "Service health",
      "quota": "Quota radar",
      "degradation": "Benchmark radar"
    },
    "hero": {
      "eyebrow": "Public data dashboard",
      "title": "Model Radar",
      "description": "Track service health, anonymous quota aggregates, and changes in public model benchmarks.",
      "clientUpdated": "Page data fetched"
    },
    "state": {
      "loading": "Loading data",
      "empty": "No data available",
      "retry": "Retry"
    },
    "error": {
      "title": "Unable to load radar data",
      "safeReason": "The public data sources are temporarily unavailable. Please try again.",
      "loadFailed": "Unable to load this section",
      "generic": "Unable to load this section"
    },
    "health": {
      "title": "Service health",
      "subtitle": "Current service status for added model platforms and vendors.",
      "empty": "No added model platforms are currently available.",
      "updated": "Updated",
      "uptime": "90-day uptime",
      "dayUptime": "{days}-day uptime",
      "lastIncident": "Latest incident",
      "history30d": "30-day history",
      "dayHistory": "{days}-day history",
      "dailyStatus": "Daily status",
      "statusLegend": "Status key",
      "incidents30d": "incidents",
      "incidents": "incidents",
      "closeHistory": "Close history details",
      "moreIncidents": "more incidents",
      "historyUnavailable": "Historical coverage is unavailable for this day.",
      "noIncidents": "No incidents reported for this day.",
      "status": {
        "operational": "Operational",
        "degraded": "Degraded performance",
        "partialOutage": "Partial outage",
        "majorOutage": "Major outage",
        "maintenance": "Under maintenance",
        "unknown": "Status unknown"
      }
    },
    "quota": {
      "title": "Quota radar",
      "subtitle": "API-equivalent value estimates with sample sizes for each available quota window.",
      "emptyPending": "No quota data yet. Aggregation runs after service startup; try again shortly.",
      "emptyNoPublishable": "No publishable quota data. Supported plan buckets require recent passive quota snapshots and their configured minimum sample.",
      "emptyFailed": "Quota aggregation is temporarily unavailable. Please try again later.",
      "emptyDisabled": "Quota aggregation is currently disabled.",
      "accounts": "Accounts",
      "snapshotStale": "Snapshot data may be outdated",
      "smallSample": "Small sample",
      "singleSampleLowConfidence": "Single-sample estimate · Low confidence",
      "fiveHourUtilization": "5-hour utilization",
      "sevenDayUtilization": "7-day utilization",
      "inferredLimit": "Estimated API-equivalent value",
      "inference": {
        "insufficient": "Insufficient samples",
        "dispersed": "Data is too dispersed",
        "invalid": "Invalid aggregate",
        "unknownPlan": "Plan is unknown; estimation is unavailable",
        "unavailable": "No trusted result"
      },
      "noWindow": "No data for this quota window",
      "sevenDayTrend": "7-day 5-hour utilization trend",
      "sevenDayQuotaTrend": "7-day quota utilization trend",
      "trendStale": "Trend data may be outdated",
      "sparklineLabel": "Quota utilization trend",
      "trendRange": "Time range",
      "trendPoints": "points",
      "trendLatest": "Latest",
      "trendMinimum": "Minimum",
      "trendMaximum": "Maximum",
      "noTrend": "No trend data",
      "openDetails": "View details",
      "capturedAt": "Snapshot",
      "closeDetails": "Close quota details",
      "windowTabs": "Quota window",
      "window5h": "5 hours",
      "window7d": "7 days",
      "averageUtilization": "Average utilization",
      "range": "Utilization range",
      "averageCost": "Average cost",
      "sampleSize": "Sample size",
      "modelUtilization": "Model utilization",
      "breakdown": "Model cost breakdown",
      "model": "Model",
      "averageRequests": "Average requests",
      "share": "Cost share",
      "noBreakdown": "No model breakdown data",
      "trend": "Trend"
    },
    "trend": {
      "loading": "Loading trend",
      "error": "Unable to load trend"
    },
    "degradation": {
      "title": "Benchmark radar",
      "subtitle": "Current Artificial Analysis indices intersected with Model Plaza, plus model leaderboard rankings.",
      "tabs": "Benchmark views",
      "overview": "Index overview",
      "lmarena": "Model leaderboard",
      "loading": "Loading benchmark data",
      "error": "Unable to load benchmark data",
      "chartTitle": "Benchmark comparison",
      "scaleHint": "Index scale",
      "radarLabel": "Model benchmark comparison chart",
      "empty": "Waiting for benchmark data",
      "emptyIntersection": "No complete Artificial Analysis models currently match the Model Plaza catalog.",
      "stale": "Data may be outdated",
      "projectFetchedAt": "Project fetched at",
      "selectModels": "Select models",
      "searchModels": "Search AA or Model Plaza models",
      "selectionLimit": "Compare up to",
      "models": "models",
      "removeModel": "Remove model",
      "noSearchResults": "No matching models",
      "intelligence": "Intelligence index",
      "intelligenceShort": "Intelligence",
      "intelligenceDescription": "Composite performance across broad reasoning and knowledge evaluations.",
      "coding": "Coding index",
      "codingShort": "Coding",
      "codingDescription": "Performance on software-development and code-generation evaluations.",
      "agentic": "Agentic index",
      "agenticShort": "Agentic",
      "agenticDescription": "Performance on multi-step agent and tool-use evaluations."
    },
    "lmarena": {
      "loading": "Loading model leaderboard",
      "error": "Unable to load model leaderboard",
      "totalVotes": "Leaderboard model vote sum",
      "fetchedAt": "Fetched at",
      "rank": "Rank",
      "model": "Model",
      "vendor": "Vendor",
      "elo": "Elo",
      "confidence": "Confidence interval",
      "votes": "Votes",
      "empty": "No leaderboard models match the current model catalog."
    },
    "sources": {
      "title": "Data sources",
      "every": "Every",
      "lastAttempt": "Last attempt",
      "lastSuccess": "Last success",
      "nextRun": "Next scheduled run",
      "aggregatedUsage": "Sub2API Aggregated Usage",
      "empty": "No source metadata available",
      "healthy": "Healthy",
      "failed": "Failed",
      "notConfigured": "Not configured",
      "neverAttempted": "Not attempted",
      "stale": "Stale",
      "error": {
        "network": "Network error",
        "unauthorized": "Source authorization failed",
        "rateLimited": "Source rate limited",
        "invalidResponse": "Invalid source response",
        "upstream": "Upstream source error",
        "aggregation": "Usage aggregation failed",
        "generic": "Source unavailable"
      },
      "disclaimer": "Data is aggregated from anonymous on-site statistics and public third-party sources. Model benchmark results depend on evaluation methodology; API-equivalent values are statistical estimates, not official vendor quota limits or commitments, and should not be the sole basis for critical business decisions."
    }
  },
  "plaza": {
    "title": "Model Plaza",
    "header": {
      "label": "Model Plaza",
      "home": "Home",
      "docs": "Docs",
      "dashboard": "Dashboard",
      "register": "Register"
    },
    "hero": {
      "eyebrow": "Public model catalog",
      "title": "Model Plaza",
      "subtitle": "Token prices are displayed per 1M tokens unless otherwise noted. CNY amounts are reference estimates based on public settings and do not change backend billing.",
      "rateTag": "¥1 = ${rate}",
      "boostValue": "{boost}x"
    },
    "metrics": {
      "models": "Models",
      "platforms": "Providers",
      "boost": "Recharge boost"
    },
    "card": {
      "input": "Input",
      "output": "Output",
      "cacheWrite": "Cache write",
      "cacheRead": "Cache read",
      "imageOutput": "Image output",
      "perRequest": "Per request",
      "supportedChannels": "{n} channels",
      "viewDetails": "View details",
      "billingPerToken": "Pay per token",
      "billingPerRequest": "Pay per request",
      "discountBadge": "≈ {percent}% of reference",
      "standardDiscountBadge": "Standard ≈ {percent}% of reference",
      "vipDiscountBadge": "VIP ≈ {percent}% of reference",
      "peakPricing": "Peak pricing applies",
      "notAvailable": "N/A",
      "recentCalls": "{count} calls in 7d"
    },
    "searchBar": {
      "total": "{total} models",
      "filtered": "{visible}/{total} models"
    },
    "infoBanner": {
      "text": "Each model shows the lowest usable base price for standard public groups and VIP groups separately. The two price classes never overwrite each other; peak windows are called out in details."
    },
    "modal": {
      "close": "Close",
      "fullPricing": "Full pricing",
      "input": "Input",
      "output": "Output",
      "cacheWrite": "Cache write",
      "cacheRead": "Cache read",
      "imageOutput": "Image output",
      "perRequest": "Per request",
      "supportedChannels": "Channels supporting this model",
      "channelsCount": "{n} channels",
      "tieredPricing": "Tiered pricing",
      "basePricePeakNote": "Prices above use the base multipliers for standard public and VIP groups separately. Token billing also applies the configured peak multiplier during peak windows.",
      "tierRange": "{min} - {max} tokens",
      "tierRangeOpenEnded": "{min}+ tokens"
    },
    "price": {
      "standardLabel": "Standard",
      "vipLabel": "VIP",
      "standardPricing": "Standard group pricing",
      "vipPricing": "VIP group pricing",
      "unitPerMillion": "/1M",
      "unitPerRequest": "/request"
    },
    "platform": {
      "modelCount": "{n} models"
    },
    "loading": "Loading models...",
    "filters": {
      "title": "Filters",
      "subtitle": "Filter models by provider and sort order",
      "search": "Search models",
      "searchPlaceholder": "Search model name...",
      "platform": "Providers",
      "allPlatforms": "All",
      "sort": "Sort",
      "sortPopularity": "Trending (7 days)",
      "sortDefault": "Name",
      "sortInputAsc": "Input price low to high",
      "sortInputDesc": "Input price high to low"
    },
    "footer": {
      "scaleNote": "Token prices are displayed per 1M tokens unless otherwise noted.",
      "referenceRateDisclaimer": "CNY amounts are reference estimates based on public settings and do not change backend billing.",
      "registerCta": "Create account",
      "rechargeCta": "Recharge"
    },
    "empty": {
      "title": "No public models yet",
      "subtitle": "Public model data will appear here after available channels are published.",
      "filteredTitle": "No models match your filters",
      "filteredSubtitle": "Try another platform, search term, or sort option."
    },
    "unavailable": {
      "title": "Model Plaza is unavailable",
      "subtitle": "Public available-channel browsing is not enabled for this deployment."
    },
    "error": {
      "title": "Could not load Model Plaza",
      "subtitle": "Refresh the page or try again later."
    }
  },
  "home": {
    "apps": "Apps",
    "viewApps": "View Apps integration",
    "providers": {
      "grok": "Grok",
      "minimax": "MiniMax",
      "glm": "GLM",
      "kimi": "Kimi",
      "zhipu": "Zhipu GLM",
      "deepseek": "DeepSeek",
      "windsurf": "Windsurf",
      "opencode": "OpenCode"
    }
  },
  "common": {
    "apply": "Apply",
    "clear": "Clear",
    "creating": "Creating...",
    "login": "Log in",
    "previous": "Previous",
    "required": "Required",
    "retry": "Retry",
    "sending": "Sending...",
    "tryAgain": "Try again"
  },
  "nav": {
    "apps": "Apps",
    "vipReconcile": "VIP Reconciliation"
  },
  "monitorCommon": {
    "providers": {
      "minimax": "MiniMax",
      "glm": "GLM",
      "kimi": "Kimi",
      "zhipu": "Zhipu GLM",
      "deepseek": "DeepSeek",
      "windsurf": "Windsurf",
      "opencode": "OpenCode"
    }
  },
  "admin": {
    "dashboard": {
      "upstreamBalance": "Upstream Account Balance",
      "upstreamBalanceConnected": "Connected",
      "upstreamBalanceDisabled": "Reseller mode is disabled",
      "upstreamBalanceNotConfigured": "Missing upstream endpoint or API key",
      "upstreamBalanceAuthFailed": "Invalid upstream API key",
      "upstreamBalanceUnreachable": "Upstream is unreachable",
      "upstreamBalanceInvalidResponse": "Invalid upstream balance response",
      "upstreamBalanceError": "Upstream returned an error"
    },
    "vipReconcile": {
      "title": "VIP Reconciliation",
      "description": "Preview historical paid-eligibility drift and repair it safely with a resumable background job.",
      "preview": {
        "title": "Reconciliation preview",
        "description": "The preview uses a fixed database as-of time. Pagination passes the server cursor through unchanged so every page stays on the same snapshot.",
        "pageSize": "Page size",
        "newSnapshot": "Refresh snapshot",
        "asOf": "Fixed as-of",
        "total": "Total requiring action",
        "page": "Page {page}",
        "empty": "This snapshot contains no records requiring repair or manual review.",
        "loadFailed": "Unable to load the VIP reconciliation preview.",
        "stats": {
          "eligibilityRepair": "Eligibility repairs",
          "eligibilityRepairHint": "Paid eligibility must be materialized",
          "effectiveChange": "Effective state changes",
          "effectiveChangeHint": "Final VIP state will change",
          "forceOffUnchanged": "Force-off preserved",
          "forceOffUnchangedHint": "Eligibility repaired; final VIP stays off",
          "invalidOrder": "Invalid orders",
          "invalidOrderHint": "Missing completed_at; cannot auto-repair",
          "deletedUser": "Deleted users",
          "deletedUserHint": "Order owner is missing or deleted"
        },
        "columns": {
          "category": "Category",
          "user": "User ID",
          "order": "Order ID",
          "completedAt": "Completed at",
          "currentMode": "Current mode",
          "currentEffective": "Current effective VIP",
          "willChange": "Effective state changes"
        },
        "categories": {
          "eligibilityRepair": "Eligibility repair",
          "effectiveChange": "Effective state change",
          "forceOffUnchanged": "Force-off preserved",
          "invalidOrder": "Invalid order",
          "deletedUser": "Deleted user"
        }
      },
      "execute": {
        "title": "Run full reconciliation",
        "description": "The background job recalculates from a fresh as-of time. The preview is for assessment only and is not used as execution input.",
        "warning": "This operation repairs historical VIP paid eligibility and writes immutable audit events. FORCE_OFF always wins; later payments and reconciliation cannot bypass an administrator override.",
        "requestId": "Idempotency request ID",
        "requestIdHint": "A failed retry must reuse the same request ID and reason. The server enforces idempotency and detects conflicts.",
        "newRequest": "Generate new request",
        "reason": "Execution reason",
        "reasonPlaceholder": "Describe the business context, ticket, or change reference for this full reconciliation",
        "reasonRequired": "A reason is required to run full reconciliation.",
        "invalidRequestId": "The idempotency request ID is invalid. Generate a new request ID.",
        "stepUpHint": "Starting and resuming a job both require administrator TOTP step-up verification.",
        "submit": "Start full reconciliation",
        "submitting": "Submitting...",
        "accepted": "VIP reconciliation job #{id} was accepted.",
        "failed": "Unable to submit the VIP reconciliation job."
      },
      "job": {
        "title": "Latest reconciliation job",
        "requestId": "Request ID",
        "restoring": "Restoring the latest job status...",
        "restoreFailed": "Unable to restore the latest VIP reconciliation job.",
        "loadFailed": "Unable to load VIP reconciliation job status.",
        "unknownStatus": "The server returned unknown job status “{status}”. Automatic polling and actions have stopped; verify the job manually before continuing.",
        "inProgress": "Background job in progress",
        "indeterminateHint": "The backend does not expose a total scan count, so this shows scanned records without estimating a percentage.",
        "scannedCount": "{count} records scanned",
        "resume": "Resume from failure",
        "lastError": "Latest error",
        "reason": "Execution reason",
        "actor": "Administrator",
        "cursor": "Resume cursor (completed time / order)",
        "startedAt": "Started at",
        "finishedAt": "Finished at",
        "updatedAt": "Updated at",
        "attempts": "Attempts",
        "scanned": "Scanned",
        "eligibilityRepaired": "Eligibility repaired",
        "effectiveChanged": "Effective state changed",
        "forceOffUnchanged": "Force-off preserved",
        "alreadyCorrect": "Already correct",
        "deleted": "Deleted users",
        "invalidOrder": "Invalid orders",
        "failedCount": "Failed records",
        "status": {
          "queued": "Queued",
          "running": "Running",
          "succeeded": "Succeeded",
          "failed": "Failed",
          "unknown": "Unknown: {status}"
        }
      },
      "errors": {
        "VIP_RECONCILE_IDEMPOTENCY_CONFLICT": "This request ID was used by a different administrator or with a different reason. Verify the input and generate a new request.",
        "VIP_RECONCILE_ACTIVE_JOB": "Another full VIP reconciliation job is already active. Wait for it to finish.",
        "VIP_RECONCILE_JOB_NOT_FOUND": "The VIP reconciliation job does not exist or is no longer accessible.",
        "VIP_RECONCILE_JOB_NOT_RUNNABLE": "This VIP reconciliation job cannot currently run or resume."
      }
    },
    "users": {
      "passwordCopied": "Password copied",
      "replaceGroupFailed": "Failed to replace group",
      "vip": {
        "column": "Effective VIP",
        "editTitle": "VIP entitlement mode",
        "editHint": "This controls the target user's final VIP entitlement only. Administrator binding is still rechecked against the target user's effective state.",
        "modeLabel": "Management mode",
        "modes": {
          "AUTO": "Automatic (follow paid eligibility)",
          "FORCE_ON": "Force on",
          "FORCE_OFF": "Force off"
        },
        "modeHints": {
          "AUTO": "Clears the manual override and derives the final state from completed qualifying payments.",
          "FORCE_ON": "Keeps final VIP access enabled regardless of automatic paid eligibility.",
          "FORCE_OFF": "Highest priority; later payments may record eligibility but cannot re-enable final VIP access.",
          "UNKNOWN": "The server returned an unknown or missing mode. Select an explicit mode before submitting."
        },
        "unknownMode": "Unknown mode: {mode}",
        "unknownValue": "Unknown",
        "notAvailable": "None",
        "reasonLabel": "Change reason",
        "reasonPlaceholder": "Explain the business reason for changing this VIP mode",
        "reasonRequiredHint": "The mode changed. A reason is required and will be written to immutable audit history.",
        "reasonUnchangedHint": "A reason is required only when changing the VIP mode.",
        "reasonRequired": "A reason is required when changing the VIP mode.",
        "effectiveActive": "VIP active",
        "effectiveInactive": "Not VIP",
        "effectiveUnknown": "State unknown",
        "filters": {
          "allEffective": "All VIP states",
          "effective": "Effective VIP only",
          "notEffective": "Non-VIP only",
          "allModes": "All VIP modes"
        },
        "tooltip": {
          "effective": "Final state",
          "mode": "Management mode",
          "paidEligible": "Automatic paid eligibility",
          "paidSource": "Eligibility source",
          "paidAt": "Eligibility time",
          "effectiveSource": "Effective source",
          "grantedAt": "Effective time",
          "overrideAt": "Override time",
          "overrideBy": "Administrator ID",
          "overrideReason": "Override reason"
        },
        "auditMenu": "VIP audit",
        "auditTitle": "VIP entitlement audit",
        "auditEmpty": "No VIP entitlement changes yet.",
        "auditLoadFailed": "Failed to load VIP audit history.",
        "auditUnknownAction": "Unknown change",
        "auditBefore": "Before",
        "auditAfter": "After",
        "auditActor": "Actor",
        "auditReason": "Reason",
        "auditOrder": "Order",
        "auditRequest": "Request ID",
        "willGrantExclusive": "Submitting will atomically grant access to this exclusive group",
        "catalogLoadFailed": "Failed to load the target user's group catalog.",
        "catalogDenied": "This group cannot currently be bound for the target user."
      }
    },
    "groups": {
      "platforms": {
        "minimax": "MiniMax",
        "glm": "GLM",
        "kimi": "Kimi",
        "zhipu": "Zhipu GLM",
        "deepseek": "DeepSeek",
        "windsurf": "Windsurf",
        "opencode": "OpenCode"
      },
      "vipOnly": {
        "badge": "VIP only",
        "label": "Effective VIP only",
        "hint": "When enabled, only users with effective VIP access can bind to and run this standard group.",
        "subscriptionDisabled": "Subscription groups cannot also be VIP-only; use subscription entitlement checks instead."
      },
      "fallbackSubscriptionExcluded": "Subscription groups are excluded from fallback targets; both fallback paths prohibit entering a subscription group."
    },
    "channels": {
      "emptyModelsInPricing": "Configure at least one model price.",
      "noGroupsSelected": "Select at least one group."
    },
    "accounts": {
      "platforms": {
        "minimax": "MiniMax",
        "glm": "GLM",
        "kimi": "Kimi",
        "zhipu": "Zhipu GLM",
        "deepseek": "DeepSeek",
        "windsurf": "Windsurf",
        "opencode": "OpenCode"
      },
      "capacity": {
        "minimax": {
          "exhausted": "MiniMax official 5h request remains exhausted",
          "warning": "MiniMax official 5h request remains low",
          "normal": "MiniMax official 5h request remains normal"
        },
        "deepseek": {
          "normal": "DeepSeek official balance available",
          "unavailable": "DeepSeek official balance unavailable",
          "error": "DeepSeek official balance check failed"
        }
      },
      "syncMiniMaxRemains": "Sync MiniMax Remains",
      "syncMiniMaxRemainsSuccess": "MiniMax remains synced successfully",
      "syncMiniMaxRemainsFailed": "Failed to sync MiniMax remains",
      "openai": {
        "codexCLIOnlyAllowClaudeCode": "Also allow Claude Code's Codex plugin",
        "codexCLIOnlyAllowClaudeCodeDesc": "Only takes effect when the switch above is on. Additionally allows requests from the Claude Code Codex plugin (exact match on originator=Claude Code) without weakening blocking of other non-official clients.",
        "codexImageGenerationBridge": "Codex image-generation bridge",
        "codexImageGenerationBridgeDesc": "Account policy takes precedence over channel and global settings. Only controls whether Codex requests through the /responses text endpoint receive the image_generation tool; standalone image-generation endpoints are unaffected.",
        "codexImageGenerationBridgeInherit": "Follow channel",
        "codexImageGenerationBridgeInheritDesc": "Do not write an account override; use the channel or global policy.",
        "codexImageGenerationBridgeEnabled": "Force on",
        "codexImageGenerationBridgeEnabledDesc": "Allow image tool injection for Codex /responses requests.",
        "codexImageGenerationBridgeDisabled": "Force off",
        "codexImageGenerationBridgeDisabledDesc": "Block image tool injection for Codex /responses requests.",
        "codexImageGenerationBridgeBadgeInherit": "Channel policy",
        "codexImageGenerationBridgeBadgeEnabled": "Account on",
        "codexImageGenerationBridgeBadgeDisabled": "Account off"
      },
      "glm": {
        "apiKeyHint": "Your GLM Coding Plan API Key"
      },
      "kimi": {
        "apiKeyHint": "Your Kimi Coding Plan API Key"
      },
      "deepseek": {
        "apiKeyHint": "Your DeepSeek API Key"
      },
      "windsurf": {
        "apiKeyHint": "Your Windsurf reverse proxy API Key"
      },
      "opencode": {
        "apiKeyHint": "Your OpenCode2API API Key"
      },
      "failedToLoadModels": "Failed to load latest supported models",
      "glmAccount": "GLM Account",
      "fromModel": "Source model",
      "headerOverride": {
        "fillTemplate": "Fill example"
      },
      "toModel": "Target model"
    },
    "ops": {
      "errorDetail": {
        "attemptedKeyPrefix": "Attempted Key Prefix",
        "deletedKeyOwner": "Deleted Key Owner"
      },
      "runtime": {
        "metricThresholds": "Metric thresholds",
        "metricThresholdsHint": "Configure thresholds used for runtime health scoring and alert highlighting.",
        "requestErrorRateMaxPercent": "Request error rate max (%)",
        "requestErrorRateMaxPercentHint": "Request error rate is marked unhealthy when it exceeds this percentage.",
        "slaMinPercent": "SLA minimum percent",
        "slaMinPercentHint": "SLA is marked unhealthy when it falls below this value.",
        "ttftP99MaxMs": "TTFT P99 max (ms)",
        "ttftP99MaxMsHint": "First-token latency is marked unhealthy when P99 exceeds this value.",
        "upstreamErrorRateMaxPercent": "Upstream error rate max (%)",
        "upstreamErrorRateMaxPercentHint": "Upstream error rate is marked unhealthy when it exceeds this percentage."
      },
      "settings": {
        "ignoreInvalidApiKeyErrors": "Ignore invalid API key errors",
        "ignoreInvalidApiKeyErrorsHint": "When enabled, invalid or missing API key errors (INVALID_API_KEY, API_KEY_REQUIRED) will not be written to the error log."
      }
    },
    "settings": {
      "tabs": {
        "reseller": "Reseller"
      },
      "reseller": {
        "title": "Reseller Upstream Configuration",
        "description": "View this sub-site account balance on the parent provider.",
        "enabledLabel": "Enable reseller mode",
        "enabledHint": "Connect this deployment to an upstream parent provider as a sub-site.",
        "endpointLabel": "Upstream Endpoint",
        "endpointPlaceholder": "https://parent.example.com",
        "endpointHint": "API base URL of the upstream parent provider.",
        "apiKeyLabel": "Upstream API Key",
        "apiKeyPlaceholder": "Enter upstream API key",
        "mode": "Reseller Mode",
        "endpoint": "Upstream Endpoint",
        "apiKey": "Upstream API Key",
        "apiKeyConfigured": "Configured",
        "apiKeyConfiguredHint": "An API key is already configured. Leave this blank to keep the current key.",
        "apiKeyHint": "Entering a value replaces the current upstream API key.",
        "apiKeyNotConfigured": "Not configured",
        "testConnection": "Test Connection",
        "upstreamBalance": "Upstream Account Balance",
        "localBalanceHint": "This balance belongs to the sub-site account on the parent provider. It is not a local user balance.",
        "status": {
          "ok": "Connected",
          "disabled": "Reseller mode is disabled",
          "notConfigured": "Missing upstream endpoint or API key",
          "authFailed": "Invalid upstream API key",
          "unreachable": "Upstream is unreachable",
          "invalidResponse": "Invalid upstream balance response",
          "upstreamError": "Upstream returned an error"
        }
      },
      "features": {
        "radar": {
          "title": "Model Radar Operations",
          "description": "Control background Radar scheduling, trigger a safe manual refresh, and inspect source execution state.",
          "loading": "Loading Radar status…",
          "loadError": "Radar status is temporarily unavailable.",
          "retry": "Retry",
          "enabled": "Enable scheduled Radar updates",
          "enabledHint": "Disabling stops future scheduled runs while preserving existing data. Manual refresh remains available.",
          "updateError": "The Radar runtime setting could not be updated. The previous value has been restored.",
          "sourcesTitle": "Source execution status",
          "never": "Never",
          "unavailable": "Unavailable",
          "none": "None",
          "status": {
            "healthy": "Healthy",
            "failed": "Failed",
            "never_attempted": "Never attempted",
            "stale": "Stale snapshot"
          },
          "fields": {
            "lastSuccess": "Last success",
            "lastFailure": "Last failure",
            "nextFire": "Next scheduled run",
            "error": "Safe error"
          },
          "sources": {
            "aa": "Artificial Analysis Models",
            "lmarena": "LMArena",
            "status_claude": "Claude Statuspage",
            "status_openai": "OpenAI Statuspage",
            "quota_aggregator": "Sub2API Aggregated Usage"
          },
          "errors": {
            "network_error": "Network error",
            "unauthorized": "Authorization rejected",
            "rate_limited": "Rate limited",
            "invalid_response": "Invalid response",
            "upstream_error": "Upstream error",
            "aggregation_error": "Aggregation error"
          },
          "refresh": {
            "action": "Refresh now",
            "pending": "Refresh requested…",
            "triggered": "Manual refresh started.",
            "coalesced": "A refresh is already running; this request was coalesced.",
            "error": "The manual refresh could not be requested.",
            "id": "Refresh ID: {id}",
            "tasks": "Tasks: {tasks}"
          }
        }
      },
      "registration": {
        "rateLimit": "Registration Rate Limit",
        "rateLimitHint": "Limit registration/verification-code frequency per IP, per email address, and per email domain to prevent bulk sign-up abuse. The domain-level limit is a high-threshold backstop; do not set it too low or it will affect normal users",
        "rateLimitPerIp": "Max registrations per IP",
        "rateLimitWindowIp": "Per-IP time window (seconds)",
        "rateLimitPerEmail": "Max requests per email address",
        "rateLimitWindowEmail": "Per-email-address time window (seconds)",
        "rateLimitPerEmailDomain": "Max requests per email domain (anti-bulk)",
        "rateLimitWindowEmailDomain": "Per-email-domain time window (seconds)"
      },
      "gatewayForwarding": {
        "openaiAllowClaudeCodeCodexPlugin": "Allow using the Codex plugin in Claude Code",
        "openaiAllowClaudeCodeCodexPluginDesc": "Global switch; only affects OpenAI OAuth accounts that have 'Codex official clients only' enabled. When on, all such accounts additionally allow requests from the Claude Code Codex plugin (exact match on originator=Claude Code) without per-account config; upstream requests remain pass-through."
      },
      "payment": {
        "providerWise": "Wise",
        "field_quickPayBaseUrl": "Quick Pay Base URL",
        "field_apiToken": "API Token",
        "field_profileId": "Profile ID",
        "field_balanceId": "Balance ID",
        "field_environment": "Environment",
        "field_webhookPublicKey": "Webhook Public Key",
        "field_settlementStrategy": "Settlement Strategy",
        "field_allowedMethodsNote": "Allowed Methods Note",
        "field_reconcileWindowHours": "Reconcile Window Hours",
        "field_autoFulfillFeePayments": "Auto-fulfill Fee Payments",
        "field_wiseQuickPayBaseUrlHint": "Wise Quick Pay / payment link base URL. The system appends amount, currency, and an alphanumeric order reference as description.",
        "field_wiseApiBaseHint": "Wise API base URL. Production usually uses https://api.wise.com.",
        "field_wiseWebhookPublicKeyHint": "Optional override for the Wise webhook RSA public key. Leave empty to use the built-in public key configuration; this is not a required secret.",
        "field_wiseSettlementStrategyHint": "v1 is fixed to exact_only and only auto-fulfills when settled amount equals order amount.",
        "field_wiseAllowedMethodsNoteHint": "Operational note only. v1 should enable Wise balance / bank transfer methods where settled amount equals order amount.",
        "field_wiseReconcileWindowHoursHint": "Statement lookup window in hours. Default is 72.",
        "field_wiseAutoFulfillFeePaymentsHint": "Reserved for future card / Apple Pay / Google Pay fee strategies. v1 must stay false; true is rejected by the backend.",
        "wiseWebhookHint": "After saving and enabling this Wise provider, the system automatically creates a webhook subscription for this URL.",
        "wiseWebhookSubscriptionTitle": "Wise webhook subscription",
        "wiseWebhookSubscriptionActive": "Automatically subscribed",
        "wiseWebhookSubscriptionFailed": "Subscription failed",
        "wiseWebhookSubscriptionUnknown": "Not synced yet",
        "wiseWebhookSubscriptionId": "Subscription ID",
        "wiseWebhookSubscriptionError": "Error",
        "wiseWebhookDeliveryUrl": "Delivery URL",
        "wiseGuideSummary": "Wise v1 uses hosted redirect plus automatic reconciliation. Enable Wise balance / bank transfer only.",
        "wiseGuideNote": "Do not enable card / Apple Pay / Google Pay auto-fulfillment in v1. Fee-deducted transactions require manual review."
      }
    }
  },
  "vip": {
    "access": {
      "label": {
        "ACTIVE": "VIP active",
        "PAYMENT_REQUIRED": "VIP payment required",
        "ACTIVATION_PENDING": "VIP activation pending",
        "ACTIVATION_FAILED": "VIP activation needs support",
        "RESTRICTED": "VIP access restricted",
        "UNKNOWN": "VIP status unavailable"
      },
      "description": {
        "ACTIVE": "Your VIP benefits are active. You can use VIP-only groups you are authorized for.",
        "PAYMENT_REQUIRED": "Complete a real payment to become eligible for VIP access.",
        "ACTIVATION_PENDING": "Your payment is complete and VIP benefits are syncing. Refresh later and do not pay again.",
        "ACTIVATION_FAILED": "Your payment is complete, but VIP activation exceeded the expected time. Do not pay again; contact an administrator.",
        "RESTRICTED": "VIP access is restricted by your account status. Contact an administrator if you need help.",
        "UNKNOWN": "VIP access cannot be confirmed. Related actions and purchase links are hidden for safety; refresh later or contact an administrator."
      },
      "purchaseAction": "Go to purchase"
    },
    "group": {
      "badge": "VIP only",
      "denied": {
        "GROUP_VIP_ONLY": "This group is available only to users with active VIP access.",
        "GROUP_NOT_ALLOWED": "Your account is not allowed to bind this group.",
        "GROUP_NOT_ACTIVE": "This group is not active.",
        "SUBSCRIPTION_REQUIRED": "An active subscription is required for this group.",
        "GROUP_ACCESS_PROFILE_MISSING": "Group access cannot be confirmed right now. Try again later.",
        "INVALID_CONFIGURATION": "This group is unavailable because its configuration is invalid. Contact an administrator.",
        "UNKNOWN": "Access to this group cannot be confirmed right now."
      },
      "actions": {
        "PAYMENT": "Purchase to unlock",
        "CONTACT_SUPPORT": "Contact an administrator for help"
      },
      "errors": {
        "GROUP_NOT_ALLOWED": "Your account is not allowed to bind this group.",
        "GROUP_VIP_ONLY": "This group is available only to users with active VIP access.",
        "GROUP_NOT_ACTIVE": "This group is not active.",
        "SUBSCRIPTION_REQUIRED": "An active subscription is required for this group.",
        "GROUP_ACCESS_PROFILE_MISSING": "Group access cannot be confirmed right now. Try again later.",
        "GROUP_FALLBACK_SUBSCRIPTION_FORBIDDEN": "A subscription group cannot be used as a fallback target.",
        "GROUP_FALLBACK_EXCLUSIVE_ESCALATION": "The fallback would escalate exclusive access and cannot be saved.",
        "GROUP_FALLBACK_VIP_ESCALATION": "The fallback would escalate VIP access and cannot be saved.",
        "GROUP_FALLBACK_INVALID_CONFIG": "The group fallback configuration is invalid."
      }
    },
    "paymentResult": {
      "ACTIVE": "Payment and fulfillment succeeded. Your VIP benefits are active.",
      "PAYMENT_REQUIRED": "Payment is complete, but VIP status is not confirmed. Do not pay again; refresh later or contact an administrator.",
      "ACTIVATION_PENDING": "Payment is complete and your VIP benefits are syncing. Refresh later and do not pay again.",
      "ACTIVATION_FAILED": "Payment is complete, but VIP activation needs administrator support. Do not pay again.",
      "RESTRICTED": "Payment is complete, but VIP access remains restricted by your account status. Contact an administrator if you need help.",
      "UNKNOWN": "Payment is complete, but VIP status cannot be confirmed. Do not pay again; refresh later or contact an administrator."
    }
  },
  "payment": {
    "wisePaymentNoticeTitle": "Wise Payment Notice",
    "wisePaymentNoticeBody": "Confirm the amount and currency on the Wise page and do not edit the amount. v1 auto-fulfills only Wise balance / bank transfer payments; card, Apple Pay, or Google Pay may deduct fees and will not be auto-fulfilled.",
    "methods": {
      "wise": "Wise"
    },
    "admin": {
      "wiseReconcile": {
        "title": "Wise Reconciliation Detail",
        "manualReviewWarning": "This Wise transaction requires manual review. Fee-deducted, amount-mismatched, or metadata-mismatched transactions are not auto-fulfilled.",
        "decision": "Decision",
        "reason": "Reason",
        "transactionId": "Wise Transaction ID",
        "grossAmount": "Gross Amount",
        "feeAmount": "Fee",
        "netAmount": "Net Amount",
        "currency": "Currency",
        "status": "Transaction Status",
        "description": "Description",
        "reference": "Payment Reference",
        "occurredAt": "Occurred At"
      },
      "validityDays": "Validity (days)",
      "validityDaysRequired": "Validity days must be greater than 0"
    }
  }
} as const
