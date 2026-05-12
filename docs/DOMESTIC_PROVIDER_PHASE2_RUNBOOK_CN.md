# 国产供应商第二阶段运维手册

适用范围：MiniMax、GLM、Kimi、DeepSeek 网关的模型发现、模型 alias、Channel Monitor、MiniMax remains 同步与 DeepSeek 余额健康检测。

## 1. 模型 alias

默认配置：

```yaml
gateway:
  model_aliases:
    enabled: true
    include_in_models: false
```

- `enabled=true` 时，供应商默认 alias 会在请求转发前解析为真实上游模型。
- `include_in_models=false` 时，`GET /v1/models` 默认只暴露真实可调用模型；设为 `true` 后会额外暴露常用具体 alias，但不会暴露通配符 alias。
- 账号 `model_mapping` 优先级高于 provider 默认 alias，可用于覆盖默认映射。

默认 alias：

| 供应商 | 请求模型 | 上游模型 |
| --- | --- | --- |
| MiniMax | `claude-sonnet-4-5` / `claude-3-5-sonnet-latest` / `claude-sonnet-*` | `MiniMax-M2.7` |
| MiniMax | `claude-haiku-*` | `MiniMax-M2.7-highspeed` |
| GLM | `claude-sonnet-*` / `claude-opus-*` | `GLM-5.1` |
| GLM | `claude-haiku-*` | `GLM-4.5-air` |
| Kimi | `claude-sonnet-4-5` / `claude-3-5-sonnet-latest` / `claude-sonnet-*` | `kimi-for-coding` |
| DeepSeek | `deepseek-chat` / `deepseek-v3` | `deepseek-v4-flash` |
| DeepSeek | `deepseek-reasoner` / `deepseek-r1` | `deepseek-v4-pro` |

## 2. `/v1/models`

当前实现按 API Key 所属分组平台分发，同一个 `GET /v1/models` 路径会返回对应国产供应商的模型清单。下面用四个不同 API Key 表示四个 provider 分组。

```bash
curl -sS "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $MINIMAX_GROUP_TOKEN"

curl -sS "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $GLM_GROUP_TOKEN"

curl -sS "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $KIMI_GROUP_TOKEN"

curl -sS "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $DEEPSEEK_GROUP_TOKEN"
```

默认模型：

- MiniMax：`MiniMax-M2.7`、`MiniMax-M2.7-highspeed`
- GLM：`GLM-5.1`、`GLM-4.7`、`GLM-4.5-air`
- Kimi：`kimi-for-coding`
- DeepSeek：`deepseek-v4-flash`、`deepseek-v4-pro`

## 3. Channel Monitor

管理端 Channel Monitor 支持 provider：

- `minimax`
- `glm`
- `kimi`
- `deepseek`

探测请求由 provider adapter 生成：

| Provider | 请求路径 | 鉴权 |
| --- | --- | --- |
| MiniMax | `/anthropic/v1/messages` | `Authorization: Bearer <key>` |
| GLM | `/api/anthropic/v1/messages` | `Authorization: Bearer <key>` |
| Kimi | `/coding/v1/messages` | `Authorization: Bearer <key>` |
| DeepSeek | `/chat/completions` | `Authorization: Bearer <key>` |

GLM 和 Kimi 只做通用健康监控：endpoint 可达性、API key 是否有效、模型是否可调用、延迟和错误分类。不要为 GLM/Kimi 配置或展示伪 remains、伪 balance。

## 4. MiniMax Remains

配置：

```yaml
gateway:
  minimax_remains:
    sync_enabled: true
    sync_interval_seconds: 300
    sync_jitter_seconds: 30
    batch_size: 50
    stale_after_seconds: 900
```

行为：

- 后台任务定时同步官方 5 小时文本窗口 remains。
- 同步结果写入账号 `extra`，包括 `minimax_text_5h_limit`、`minimax_text_5h_remaining`、`minimax_remains_synced_at`、`minimax_remains_sync_status`。
- Redis 校准只增删 `official:` 前缀的 synthetic reservation，不删除真实请求记录。
- 官方 remains 新鲜且为 0 时，MiniMax 请求会被本地额度保护拦截。

手动同步单个账号：

```bash
curl -sS -X POST "$BASE_URL/api/v1/admin/accounts/$MINIMAX_ACCOUNT_ID/minimax/remains-sync" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## 5. DeepSeek Balance

配置：

```yaml
gateway:
  deepseek_balance:
    check_enabled: true
    check_interval_seconds: 300
    check_jitter_seconds: 30
    batch_size: 50
    stale_after_seconds: 900
```

行为：

- 后台任务定时调用 `GET <base_url>/user/balance`。
- 成功结果写入 `deepseek_balance_available`、`deepseek_balance_amount`、`deepseek_balance_currency`、`deepseek_balance_checked_at`、`deepseek_balance_status`。
- `deepseek_balance_status` 可为 `ok`、`unavailable`、`error`。
- DeepSeek balance 不作为每次请求前的同步远程检查；请求链路仍以真实上游响应和现有健康策略为准。

手动检测单个账号：

```bash
curl -sS -X POST "$BASE_URL/api/v1/admin/accounts/$DEEPSEEK_ACCOUNT_ID/deepseek/balance-check" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```
