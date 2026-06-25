# API 参考

本页列出 {{SITE_NAME}} 常用兼容端点。实际支持范围取决于当前部署的路由、分组、上游账号、模型映射、渠道价格和管理员配置。

---

## 认证方式

大多数接口使用 Bearer Token：

```http
Authorization: Bearer $YOUR_KEY
```

### curl 调用示例

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

> [!NOTE]
> 部分兼容客户端可能使用 `api-key`、`x-api-key` 或 SDK 内置的其它字段传递密钥。除非管理员特别说明，推荐您在任何兼容场景中优先使用 `Authorization: Bearer $YOUR_KEY`。

---

## OpenAI 兼容端点

| HTTP 方法 | 路径 | 核心用途 | 备注说明 |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/models` | 查询当前密钥可用模型列表 | 结果随当前 Key 绑定的分组和管理员配置变化。 |
| **POST** | `/v1/chat/completions` | Chat Completions 聊天生成 | 适合大部分 OpenAI SDK 和大量的第三方客户端。 |
| **POST** | `/v1/responses` | Responses API 对话 | 适合 Codex CLI 或支持 OpenAI Responses API 的客户端。 |
| **POST** | `/v1/embeddings` | 文本向量化 (Embeddings) | 需要部署后台已启用并映射了向量模型。 |
| **POST** | `/v1/images/generations` | 图像生成 (Text-to-Image) | 需要部署并开放了 DALL-E 等图片生成渠道。 |
| **POST** | `/v1/images/edits` | 图像二次编辑与修改 | 需要客户端按照上游规格提交原图及相应参数。 |

---

## Anthropic 兼容端点

| HTTP 方法 | 路径 | 核心用途 | 备注说明 |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/messages` | Messages 消息对话生成 | 适合 Claude Code、Anthropic SDK 或兼容的命令行。 |
| **POST** | `/v1/messages/count_tokens` | 估算消息的 Token 数量 | 估算结果取决于上游引擎以及网关对应的解析规则。 |

---

## Gemini 原生端点

| HTTP 方法 | 路径 | 核心用途 | 备注说明 |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1beta/models` | 查询 Gemini 风格模型 | 返回的列表只覆盖当前 API Key 可以正常调用的范围。 |
| **GET** | `/v1beta/models/{model}` | 查询单个模型详细元数据 | `{model}` 使用部署支持的 Gemini 官方名或映射名。 |
| **POST** | `/v1beta/models/{model}:generateContent` | Gemini 非流式内容生成 | 传入的请求体结构必须遵循 Google 原生 JSON 格式。 |
| **POST** | `/v1beta/models/{model}:streamGenerateContent?alt=sse` | Gemini SSE 流式内容生成 | 客户端需要能够支持 SSE (`text/event-stream`) 协议。 |

---

## Antigravity 端点

| HTTP 方法 | 路径 | 核心用途 | 备注说明 |
| :--- | :--- | :--- | :--- |
| **GET** | `/antigravity/models` | 查询 Antigravity 可用模型 | 结果取决于 Antigravity 相关渠道配置与分组策略。 |
| **POST** | `/antigravity/v1/messages` | Antigravity Messages 聊天 | 通常用于 Antigravity 专用的 Claude 兼容配置。 |
| **POST** | `/antigravity/v1/messages/count_tokens` | Antigravity Token 估算 | 统计结果由网关内置估算器计算。 |
| **GET** | `/antigravity/v1/usage` | 查询当前 Key 的统计用量 | 具体返回的统计指标受密钥权限限制。 |
| **GET** | `/antigravity/v1beta/models` | Antigravity Gemini 模型列表 | 面向 Gemini 风格的 Antigravity 独立接入。 |
| **ANY** | `/antigravity/v1beta/models/*` | Antigravity Gemini 兼容转发 | 覆盖模型详情、非流式与流式生成等所有子路径。 |

---

## 请求体字段差异对比

| 接口兼容类型 | 核心消息字段 | 常见模型名来源 |
| :--- | :--- | :--- |
| **OpenAI Chat** | `messages`（每条包含 `role` 和 `content`） | `/v1/models` 返回的名称或映射名。 |
| **OpenAI Responses** | `input`（通常为单字符串输入） | `/v1/models` 或 Codex 客户端默认配置。 |
| **Anthropic Messages** | `messages`，需携带 `max_tokens` | Anthropic 兼容模型名。 |
| **Gemini Native** | `contents` 下嵌套 `parts` 数组 | `/v1beta/models` 返回的原生名称。 |

---

## 响应与错误

> [!IMPORTANT]
> **成功响应结构**通常严格遵循对应兼容接口族的原始协议格式。**错误响应**可能来自本中转网关本身，也可能由实际上游厂商直接透传返回。

在排查 API 调用失败时，建议优先通过以下顺序进行自检：
1. 确认请求头中配置的 API Key 拼写无误，且包含 `Bearer ` 前缀。
2. 确认当前 API Key 所在的分组已被授权调用该模型。
3. 确认请求的目标 URL 路径在当前的 {{SITE_NAME}} 部署中已启用。
4. 确认请求体（Payload）的字段设计完全符合对应兼容层协议。
5. 检查控制台以确认当前分组余额充足，没有达到速率上限。
