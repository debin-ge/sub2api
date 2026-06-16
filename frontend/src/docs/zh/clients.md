# 客户端接入

本页给出常见客户端的完整接入路径：先验证密钥和模型，再配置 SDK 或工具，最后处理流式、超时和错误。请将示例中的 `{{BASE_URL}}` 替换为管理员提供的地址，并统一通过 `$YOUR_KEY` 传递 API Key。

## 接入前准备

先设置通用环境变量：

```bash
export BASE_URL="{{BASE_URL}}"
export YOUR_KEY="replace-with-your-api-key"
```

用当前密钥查询模型：

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

如果该命令失败，先不要配置 SDK。请先排查 Base URL、API Key、分组权限和网络连通性。

## curl 最小请求

Chat Completions：

```bash
curl "${BASE_URL}v1/chat/completions" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "你好，{{SITE_NAME}}。" }
    ]
  }'
```

流式 Chat Completions：

```bash
curl -N "${BASE_URL}v1/chat/completions" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "stream": true,
    "messages": [
      { "role": "user", "content": "分三点介绍 {{SITE_NAME}}。" }
    ]
  }'
```

如果示例模型不可用，请改用 `/v1/models` 返回的模型名。

## OpenAI SDK TypeScript

多数 OpenAI SDK 需要把 `baseURL` 配到 `/v1` 级别：

```bash
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="$YOUR_KEY"
```

```ts
import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: process.env.OPENAI_BASE_URL,
  apiKey: process.env.OPENAI_API_KEY,
})

const result = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  messages: [{ role: 'user', content: '用一句话介绍 {{SITE_NAME}}。' }],
})

console.log(result.choices[0]?.message?.content)
```

流式读取：

```ts
const stream = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  stream: true,
  messages: [{ role: 'user', content: '输出一个简短大纲。' }],
})

for await (const chunk of stream) {
  process.stdout.write(chunk.choices[0]?.delta?.content ?? '')
}
```

如果环境变量中已经包含 `/v1`，不要在代码里再拼接 `/v1`。

## OpenAI SDK Python

```bash
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="$YOUR_KEY"
```

```python
from openai import OpenAI

client = OpenAI()

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[
        {"role": "user", "content": "用一句话介绍 {{SITE_NAME}}。"},
    ],
)

print(response.choices[0].message.content)
```

如果出现 `404`，优先检查 `OPENAI_BASE_URL` 是否写成了 `{{BASE_URL}}v1/v1` 或缺少 `/v1`。

## Claude Code 环境变量

Claude Code 或 Anthropic 兼容客户端通常需要 Anthropic Base URL 和 API Key。不同版本的客户端变量名可能不同，请以客户端自身文档为准；常见配置如下：

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}"
export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
```

部分客户端需要 `ANTHROPIC_API_KEY`：

```bash
export ANTHROPIC_API_KEY="$YOUR_KEY"
```

然后选择当前部署支持的 Claude 兼容模型。模型名应来自 `/v1/models` 或管理员提供的映射名。

## Anthropic Messages 请求

```bash
curl "${BASE_URL}v1/messages" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "max_tokens": 512,
    "messages": [
      { "role": "user", "content": "把这段需求整理成验收标准。" }
    ]
  }'
```

Messages 请求通常需要 `max_tokens`。如果你把 OpenAI 的 `messages` 格式直接复制到 Anthropic 端点，也要确认字段是否符合该客户端或上游要求。

## Antigravity Claude 环境变量

如果管理员提供 Antigravity Claude 兼容入口，可将客户端指向 `/antigravity` 路径：

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}antigravity"
export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
```

如果出现 404 或模型不可用，请确认当前部署是否启用了 `/antigravity/v1/messages`，以及你的分组是否有对应模型权限。

## Gemini 原生端点

Gemini 原生客户端应使用 `/v1beta` 路径和 Gemini 请求体格式。非流式示例：

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

流式请求使用：

```bash
curl -N "${BASE_URL}v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          { "text": "连续输出一个简短大纲。" }
        ]
      }
    ]
  }'
```

Gemini 原生端点不使用 OpenAI `messages` 格式。请使用当前部署支持的 Gemini 模型名或映射名。

## Codex `/v1/responses` 示例

支持 Responses API 的编码客户端可指向 {{SITE_NAME}} 的 `/v1/responses`：

```bash
curl "${BASE_URL}v1/responses" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": "写一个 TypeScript 函数，返回两个数字的和。"
  }'
```

如果模型或 Responses API 未在当前部署开放，请改用 `/v1/models` 返回的可用模型，或联系管理员启用对应渠道。

## 客户端配置建议

| 项目 | 建议 |
| --- | --- |
| Base URL | 根地址用于 curl 拼接路径；OpenAI SDK 通常使用 `{{BASE_URL}}v1`。 |
| API Key | 使用 `$YOUR_KEY` 或客户端要求的环境变量，不要写入源码。 |
| 模型名 | 以 `/v1/models`、`/v1beta/models` 或管理员映射说明为准。 |
| 超时 | 长输出和流式请求需要更长的 HTTP、代理和负载均衡超时。 |
| 重试 | 对 429 和 5xx 使用指数退避；不要对 401、403、404 盲目重试。 |
| 日志 | 记录请求路径、模型、状态码和请求 ID，不要记录完整密钥。 |

## 常见接入问题

| 现象 | 可能原因 | 处理 |
| --- | --- | --- |
| SDK 返回 401 | API Key 没传入、变量名错误或缺少 Bearer。 | 打印环境变量是否为空，确认请求头格式。 |
| SDK 返回 404 | Base URL 重复拼接 `/v1` 或端点未启用。 | 核对最终请求 URL。 |
| 模型不存在 | 示例模型未在当前分组开放。 | 使用 `/v1/models` 返回的模型名。 |
| 流式没有输出 | 客户端或代理不支持 SSE，或超时过短。 | 先用非流式请求验证，再检查代理和超时。 |
| 本地正常生产失败 | 生产密钥、分组、网络出口或代理配置不同。 | 对比环境变量、模型列表和错误码。 |
