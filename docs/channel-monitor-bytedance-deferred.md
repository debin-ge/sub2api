# ByteDance 渠道监控：暂缓（Deferred）

**Date:** 2026-09-12
**Status:** 暂缓，本次不实施
**来源:** `main → test` 合并后代码审查的遗留项（第 5 项）

## 结论

ByteDance 目前**不在**渠道监控的 provider 白名单里，这是有意为之，不是漏掉的。

原始想法是"只开 quota 模式"，把它当成一次配置项改动（往几张白名单里加个字符串）。
结论是这条路走不通：ByteDance 账号**没有任何可用的用量/余额数据源**，加进去得到的不是
"功能少一半的监控项"，而是"一个每次必然失败的永红告警源"。

## 为什么"只开 quota 模式"不是配置项改动

渠道监控的 quota 快照只有一个入口，`ChannelMonitorQuotaFetcher.fetchUncached`
（`backend/internal/service/channel_monitor_quota_fetcher.go:218-226`），它按
`account.Platform` 二分：

- `case domain.PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax`（`:219`）
  → `fetchCNQuota`（coding plan）或 `fetchCNBalance`（余额）；
- `default:`（`:225`）→ `fetchUsage`。

ByteDance 会落进 `default`，也就是 `fetchUsage` → `AccountUsageService.GetUsageForAccount`。
而后者的分支只覆盖 OpenAI OAuth / Gemini / Antigravity / Grok / `CanGetUsage()` / SetupToken，
最后兜底返回错误：

```go
// backend/internal/service/account_usage_service.go:570
return nil, fmt.Errorf("account type %s does not support usage query", account.Type)
```

`CanGetUsage()` 只对 OAuth 账号为真（`backend/internal/service/account.go:412`），而 ByteDance
账号是 API Key 型的——`ByteDanceVideoProvider.apiKey()` 只读 `api_key` 凭据
（`backend/internal/service/bytedance_video_provider.go:114`），基址是 Ark 推理端点
`https://ark.cn-beijing.volces.com/api/v3`（`:25`）。Ark 的 API Key 只能调推理，查不到账户额度。

所以每一次 quota 检查都会稳定落到那句兜底错误上。

probe 模式同样不可行：`VideoCapabilityProber.ProbeCapability`
（`backend/internal/service/video_provider.go:258`）目前只有 `OpenAIVideoProvider` 实现
（`backend/internal/service/openai_video_provider.go:114`），`probeCapableProviders`
（`backend/internal/service/channel_monitor_validate.go:35`）与之对应，`validateCheckMode`
（`:82`）在非 quota 模式下要求 `providerSupportsProbe`。

## 复工时的三条路线

**A. 维持暂缓**（本次选择）。不加白名单，不产生假告警。ByteDance 渠道的健康状况暂时依赖
业务侧的失败率/错误日志，而非主动监控。

**B. 接火山引擎 OpenAPI 查余额**，让 quota 模式名副其实。需要三件新东西，不是配置改动：
1. 新增凭据字段 AccessKey / SecretKey（账户额度查询走火山 OpenAPI，不是 Ark 推理端点）；
2. 实现火山引擎 V4 请求签名器；
3. 新增 ByteDance 专用的 balance fetcher，并在 `fetchUncached` 的 switch 里给它一个 case。

还要先定清楚 Ark API Key 与 AK/SK 的并存关系（是否要求两者都填、只填 API Key 的存量账号如何降级）。

**C. 做"凭据有效性探测"而非用量查询**。用一次轻量的 Ark 调用（例如 `SearchByClientToken`
形态的查询）只回答"这把 key 还能用吗"，不产生用量数据。需要为 ByteDance 实现
`ProbeCapability` 并加入 `probeCapableProviders`；语义上它属于 **probe** 模式，不是 quota。

## 复工清单：平台白名单必须三处同步

任选 B 或 C 复工时，下面这些位置必须一起改，漏一处会在运行期或建表期报错：

| 位置 | 内容 |
|---|---|
| `backend/internal/service/channel_monitor_validate.go:16` | `monitorProviders`（走 C 还需改 `:35` 的 `probeCapableProviders`） |
| `backend/internal/service/channel_monitor_const.go:77-89` | `MonitorProvider*` 字符串常量（同时是 ent enum 的实际值） |
| 新编号迁移 | `channel_monitors_provider_check` 与 `channel_monitor_request_templates_provider_check` 加 `'bytedance'`。当前 12 个取值由迁移 226 设定；**不要改 226 或 237**，另建新编号 |
| `frontend/src/constants/channelMonitor.ts:41` | `PROVIDERS`；若只支持 quota 还需加进 `:57` 的 `QUOTA_ONLY_PROVIDERS` |
| `frontend/src/api/admin/channelMonitor.ts:8` | `Provider` 联合类型 |
| `backend/internal/service/channel_monitor_quota_mode_test.go:367` | `TestProviderProbeCapabilityMatrix` 会要求同步 |

## Out of scope

- 不为 ByteDance 引入"允许 quota 检查恒定失败"的宽容模式——那等于把监控变成噪声源。
- 不改动迁移 226 / 237 的 provider CHECK（已应用，不可原地修改）。
- 不在本次改动里调整 `QUOTA_ONLY_PROVIDERS` 的现有成员（目前仅 `antigravity`）。
