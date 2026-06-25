# 可复制配置模板

本页提供可以直接复制后修改的配置模板。除非管理员提供了其他地址，否则示例使用 `{{BASE_URL}}` 作为 Base URL，并统一用 `$YOUR_KEY` 传递 API Key。

## 通用环境变量

```bash
export BASE_URL="{{BASE_URL}}"
export YOUR_KEY="replace-with-your-api-key"
```

验证密钥：

```bash
curl -i "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

## OpenAI SDK

### TypeScript

```ts
import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: process.env.OPENAI_BASE_URL ?? '{{BASE_URL}}v1',
  apiKey: process.env.OPENAI_API_KEY ?? process.env.YOUR_KEY,
})

const response = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  messages: [
    { role: 'user', content: '用一句话介绍 {{SITE_NAME}}。' },
  ],
})

console.log(response.choices[0]?.message?.content)
```

推荐环境变量：

```bash
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="$YOUR_KEY"
```

### Python

```python
import os
from openai import OpenAI

client = OpenAI(
    base_url=os.getenv("OPENAI_BASE_URL", "{{BASE_URL}}v1"),
    api_key=os.getenv("OPENAI_API_KEY") or os.getenv("YOUR_KEY"),
)

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[
        {"role": "user", "content": "用一句话介绍 {{SITE_NAME}}。"},
    ],
)

print(response.choices[0].message.content)
```

## OpenAI Chat Completions curl

```bash
curl "${BASE_URL}v1/chat/completions" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "system", "content": "你是一个简洁的助手。" },
      { "role": "user", "content": "写一个三点行动清单。" }
    ],
    "temperature": 0.7,
    "max_tokens": 300
  }'
```

## OpenAI Responses curl

```bash
curl "${BASE_URL}v1/responses" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": "用 TypeScript 写一个 debounce 函数。"
  }'
```

## Anthropic Messages curl

```bash
curl "${BASE_URL}v1/messages" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "max_tokens": 512,
    "messages": [
      { "role": "user", "content": "帮我把这段需求整理成验收标准。" }
    ]
  }'
```

## Claude Code 或 Anthropic 兼容客户端

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}"
export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
```

如果客户端要求 `ANTHROPIC_API_KEY`：

```bash
export ANTHROPIC_API_KEY="$YOUR_KEY"
```

如果使用 Antigravity Claude 兼容入口：

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}antigravity"
export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
```

## Gemini 原生 curl

非流式：

```bash
curl "${BASE_URL}v1beta/models/gemini-2.0-flash:generateContent" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          { "text": "用一句话介绍 {{SITE_NAME}}。" }
        ]
      }
    ]
  }'
```

流式：

```bash
curl "${BASE_URL}v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          { "text": "持续输出一个简短大纲。" }
        ]
      }
    ]
  }'
```

## Embeddings

```bash
curl "${BASE_URL}v1/embeddings" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "text-embedding-3-small",
    "input": "{{SITE_NAME}} 统一多模型 API 接入。"
  }'
```

## 前端应用的安全代理

不要在浏览器中直接暴露 API Key。前端应用应请求你自己的服务端接口，再由服务端调用 {{SITE_NAME}}：

```ts
// server-side only
const response = await fetch('{{BASE_URL}}v1/chat/completions', {
  method: 'POST',
  headers: {
    Authorization: `Bearer ${process.env.YOUR_KEY}`,
    'Content-Type': 'application/json',
  },
  body: JSON.stringify({
    model: 'gpt-4o-mini',
    messages: [{ role: 'user', content: '你好' }],
  }),
})
```

## OpenCode CLI 环境配置

```bash
export OPENCODE_API_BASE="{{BASE_URL}}v1"
export OPENCODE_API_KEY="$YOUR_KEY"
```

## 常见替换项

| 占位项 | 替换为 |
| --- | --- |
| `$YOUR_KEY` | 你的 {{SITE_NAME}} API Key 环境变量。 |
| `gpt-4o-mini` | `/v1/models` 返回的 OpenAI 兼容模型。 |
| `claude-3-5-sonnet-latest` | `/v1/models` 返回或管理员提供的 Anthropic 兼容模型。 |
| `gemini-2.0-flash` | `/v1beta/models` 返回或管理员提供的 Gemini 模型。 |
| `{{BASE_URL}}` | 管理员提供的部署地址。 |
