# API 参考

本页列出 {{SITE_NAME}} 常用兼容端点。实际支持范围取决于当前部署的路由、分组、上游账号、模型映射、渠道价格和管理员配置。

## 认证

大多数接口使用 Bearer Token：

```http
Authorization: Bearer ${API_KEY}
```

curl 示例：

```bash
curl "{{BASE_URL}}v1/models" \
  -H "Authorization: Bearer ${API_KEY}"
```

部分兼容客户端可能使用 `api-key`、`x-api-key` 或 SDK 内置字段传递密钥。除非管理员特别说明，推荐优先使用 `Authorization: Bearer ${API_KEY}`。

## OpenAI 兼容端点

| 方法 | 路径 | 用途 | 备注 |
| --- | --- | --- | --- |
| GET | `/v1/models` | 查询当前密钥可用模型 | 结果会随分组和管理员配置变化。 |
| POST | `/v1/chat/completions` | Chat Completions | 适合 OpenAI SDK 和大量兼容客户端。 |
| POST | `/v1/responses` | Responses API | 适合 Codex 或支持 Responses API 的客户端。 |
| POST | `/v1/embeddings` | 文本向量 | 需要部署启用可用 embedding 模型。 |
| POST | `/v1/images/generations` | 图片生成 | 需要部署启用图片生成渠道和模型。 |
| POST | `/v1/images/edits` | 图片编辑 | 需要客户端按上游要求提交图片和参数。 |

## Anthropic 兼容端点

| 方法 | 路径 | 用途 | 备注 |
| --- | --- | --- | --- |
| POST | `/v1/messages` | Messages API | 适合 Claude Code、Anthropic SDK 或兼容客户端。 |
| POST | `/v1/messages/count_tokens` | 估算 Messages Token | 是否可用取决于上游和部署配置。 |

## Gemini 原生端点

| 方法 | 路径 | 用途 | 备注 |
| --- | --- | --- | --- |
| GET | `/v1beta/models` | 查询 Gemini 模型 | 返回内容可能只覆盖当前配置可访问范围。 |
| GET | `/v1beta/models/{model}` | 查询单个模型 | `{model}` 使用部署支持的 Gemini 模型名或映射名。 |
| POST | `/v1beta/models/{model}:generateContent` | Gemini 非流式生成 | 请求体遵循 Gemini `generateContent` 格式。 |
| POST | `/v1beta/models/{model}:streamGenerateContent?alt=sse` | Gemini SSE 流式生成 | 客户端需要支持 SSE 流式读取。 |

## Antigravity 端点

| 方法 | 路径 | 用途 | 备注 |
| --- | --- | --- | --- |
| GET | `/antigravity/models` | 查询 Antigravity 可用模型 | 结果取决于 Antigravity 相关渠道和分组配置。 |
| POST | `/antigravity/v1/messages` | Antigravity Messages | 通常用于 Antigravity Claude 兼容配置。 |
| POST | `/antigravity/v1/messages/count_tokens` | Antigravity Token 估算 | 是否支持取决于上游能力。 |
| GET | `/antigravity/v1/usage` | 查询 Antigravity 用量 | 可见范围取决于密钥和部署策略。 |
| GET | `/antigravity/v1beta/models` | Antigravity Gemini 模型列表 | 面向 Gemini 风格的 Antigravity 接入。 |
| ANY | `/antigravity/v1beta/models/*` | Antigravity Gemini 兼容转发 | 覆盖模型详情、生成和流式生成等子路径。 |

## 请求格式注意事项

| 接口族 | 消息字段 | 常见模型来源 |
| --- | --- | --- |
| OpenAI Chat | `messages`，每条包含 `role` 和 `content` | `/v1/models` 或管理员提供的映射名。 |
| OpenAI Responses | `input` 或 Responses API 支持的输入结构 | `/v1/models` 或 Codex 客户端配置。 |
| Anthropic Messages | `messages`、`max_tokens`，可包含系统提示和工具字段 | Anthropic 兼容模型或映射名。 |
| Gemini Native | `contents`、`parts` | `/v1beta/models` 或管理员提供的 Gemini 模型名。 |

## 响应和错误

成功响应通常遵循对应兼容接口的结构。错误响应可能来自 {{SITE_NAME}} 本身，也可能来自上游平台。排查时优先确认：

1. API Key 是否正确。
2. 当前分组是否允许访问该模型。
3. 请求路径是否属于当前部署启用的端点。
4. 请求体是否符合对应接口族格式。
5. 上游账号、额度、速率限制或模型映射是否正常。

