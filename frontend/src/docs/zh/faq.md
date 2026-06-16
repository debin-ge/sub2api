# 常见问题

## Base URL 应该填什么？

填写你的 {{SITE_NAME}} 部署地址，例如：

```text
{{BASE_URL}}
```

如果 SDK 要求填写 OpenAI 风格 `baseURL`，通常需要指向 `/v1`：

```text
{{BASE_URL}}v1
```

如果管理员提供了带路径的地址，请以管理员给出的完整地址为准，避免重复拼接 `/v1`。

## API Key 放在哪个请求头？

推荐使用 Bearer Token：

```http
Authorization: Bearer $YOUR_KEY
```

curl 中可以写成：

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

部分兼容客户端可能使用 `api-key`、`x-api-key` 或 SDK 配置项。除非管理员或客户端要求其他方式，优先使用 `Authorization` 请求头。

## 为什么 `/v1/models` 的结果和别人不一样？

`/v1/models` 返回的是当前 API Key 实际可用的模型视图。不同用户或密钥可能属于不同分组，因此会受到分组权限、上游账号、模型映射、渠道价格、额度和管理员配置影响。

如果缺少某个模型，请不要假设是前端显示问题。先确认当前密钥所属分组是否开放该模型，以及管理员是否配置了可用渠道。

## 应该选择哪个端点？

| 客户端或需求 | 推荐端点 |
| --- | --- |
| OpenAI Chat Completions 兼容客户端 | `/v1/chat/completions` |
| OpenAI Responses 或 Codex 类客户端 | `/v1/responses` |
| Claude Code 或 Anthropic Messages 兼容客户端 | `/v1/messages` |
| Gemini 原生客户端 | `/v1beta/models/{model}:generateContent` |
| Antigravity 兼容客户端 | `/antigravity/...` |
| Embedding | `/v1/embeddings` |
| 图片生成或编辑 | `/v1/images/generations`、`/v1/images/edits` |

端点和请求体格式需要匹配。不要把 OpenAI `messages` 请求体直接发到 Gemini 原生端点，也不要把 Gemini `contents` 请求体发到 OpenAI Chat Completions 端点。

## 为什么会返回 404？

常见原因：

| 原因 | 处理 |
| --- | --- |
| Base URL 拼错 | 检查是否重复 `/v1`，或缺少 `/antigravity`、`/v1beta` 等路径。 |
| 端点未启用 | 联系管理员确认当前部署是否支持该接口。 |
| 模型名不存在 | 使用 `/v1/models` 返回的模型名，或管理员提供的映射名。 |
| 接口族不匹配 | 按 OpenAI、Anthropic、Gemini 或 Antigravity 的格式重新选择端点。 |

## 为什么模型调用失败？

模型失败不一定表示 {{SITE_NAME}} 不可用。常见原因包括：

1. 当前 API Key 分组没有该模型权限。
2. 上游账号额度不足、密钥失效或被限流。
3. 管理员未配置模型映射或渠道价格。
4. 请求体参数不被该上游模型支持。
5. 流式请求被代理或客户端超时中断。

建议先调用：

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

然后用返回的模型名发起最小请求。如果最小请求仍失败，请把错误码、请求路径、模型名和时间点提供给管理员排查，注意不要发送完整 API Key。

