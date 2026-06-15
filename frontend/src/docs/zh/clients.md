# 客户端接入

本页给出常见客户端配置示例。请将 `https://your-sub2api.example.com` 和 `sk-your-key` 替换为你的实际信息，或使用环境变量 `$BASE_URL`、`$SUB2API_KEY`。

## curl

查询可用模型：

```bash
curl "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $SUB2API_KEY"
```

发起 Chat Completions 请求：

```bash
curl "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $SUB2API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "你好，Sub2API。" }
    ]
  }'
```

## OpenAI SDK TypeScript

```ts
import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: process.env.BASE_URL ?? 'https://your-sub2api.example.com/v1',
  apiKey: process.env.SUB2API_KEY ?? 'sk-your-key',
})

const result = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  messages: [{ role: 'user', content: '用一句话介绍 Sub2API。' }],
})

console.log(result.choices[0]?.message?.content)
```

如果你的 `$BASE_URL` 已经包含 `/v1`，不要重复拼接。多数 OpenAI SDK 需要 `baseURL` 指向 `/v1` 级别路径。

## Claude Code 环境变量

Claude Code 或 Anthropic 兼容客户端通常需要 Anthropic Base URL 和 API Key。不同版本的客户端变量名可能不同，请以客户端文档为准；常见配置如下：

```bash
export ANTHROPIC_BASE_URL="$BASE_URL"
export ANTHROPIC_AUTH_TOKEN="$SUB2API_KEY"
```

然后选择当前部署支持的 Claude 兼容模型。模型名应来自 `/v1/models` 或管理员提供的映射名。

## Antigravity Claude 环境变量

如果管理员提供 Antigravity Claude 兼容入口，可将客户端指向 `/antigravity` 路径：

```bash
export ANTHROPIC_BASE_URL="$BASE_URL/antigravity"
export ANTHROPIC_AUTH_TOKEN="$SUB2API_KEY"
```

部分客户端需要 `ANTHROPIC_API_KEY` 而不是 `ANTHROPIC_AUTH_TOKEN`：

```bash
export ANTHROPIC_API_KEY="$SUB2API_KEY"
```

如果出现 404 或模型不可用，请确认该部署是否启用了 `/antigravity/v1/messages`，以及你的分组是否有对应模型权限。

## Gemini 原生端点

Gemini 原生客户端应使用 `/v1beta` 路径和 Gemini 请求体格式。例如：

```bash
curl "$BASE_URL/v1beta/models/gemini-2.0-flash:generateContent" \
  -H "Authorization: Bearer $SUB2API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          { "text": "用一句话介绍 Sub2API。" }
        ]
      }
    ]
  }'
```

流式请求使用：

```text
$BASE_URL/v1beta/models/{model}:streamGenerateContent?alt=sse
```

Gemini 原生端点不使用 OpenAI `messages` 格式。请使用当前部署支持的 Gemini 模型名或映射名。

## Codex `/v1/responses` 示例

支持 Responses API 的编码客户端可指向 Sub2API 的 `/v1/responses`：

```bash
curl "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $SUB2API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": "写一个 TypeScript 函数，返回两个数字的和。"
  }'
```

如果模型或 Responses API 未在当前部署开放，请改用 `/v1/models` 返回的可用模型，或联系管理员启用对应渠道。

## 配置建议

| 项目 | 建议 |
| --- | --- |
| Base URL | 在环境变量中集中配置，例如 `https://your-sub2api.example.com`。 |
| API Key | 使用 `$SUB2API_KEY`，避免写入源码。 |
| 模型名 | 以 `/v1/models` 或管理员映射说明为准。 |
| 超时 | 对长输出和流式请求设置足够的客户端超时。 |
| 日志 | 记录 request id、模型名和错误信息，但不要记录完整密钥。 |

