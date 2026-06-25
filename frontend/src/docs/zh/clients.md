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

## CLI 编程客户端（Coding Agents）

这些命令行工具（如 Claude Code CLI、Codex CLI、OpenCode CLI 等）直接运行在终端中，通过环境变量可以方便地配置和切换底层的中转服务。

### Claude Code CLI

官方 Claude Code 客户端可以通过指定 Anthropic API Base URL 的环境变量，将流量中转至本平台：

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}"
export ANTHROPIC_API_KEY="$YOUR_KEY"
```

或者对于某些版本：

```bash
export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
```

配置完成后，在终端中直接运行 `claude` 即可拉起助手并调用对应的兼容模型。

如果管理员提供了特定的 `/antigravity` 路由，可以将 Base URL 更改为：

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}antigravity"
export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
```

### Codex CLI

Codex CLI 依赖 OpenAI 格式的 Responses API，需要配置如下环境变量将其中转至本平台：

```bash
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="$YOUR_KEY"
```

**注意事项**：若使用 Nginx 反向代理本平台服务，请确保 Nginx 配置的 `http` 块中加入下述配置以保证会话粘性：
```nginx
underscores_in_headers on;
```
如果不配置该项，Nginx 默认会过滤掉请求头中含有下划线的字段（如 `session_id`），导致多账号环境下的粘性会话失效。

### OpenCode CLI

OpenCode CLI 用于接入 OpenCode2API 渠道，需要配置对应的端点与密钥：

```bash
export OPENCODE_API_BASE="{{BASE_URL}}v1"
export OPENCODE_API_KEY="$YOUR_KEY"
```

若是使用通用兼容客户端，直接将 OpenAI SDK 的 Base URL 和 API Key 指向本平台的 `/v1` 路径即可。

## 第三方 GUI 与集成工具

这类图形界面工具不仅能帮您存储 API Key，还能方便地在一处管理、配置并快速切换底层的不同 AI 客户端。

### CC-Switch 客户端切换与导入

* **客户端管理与切换**：CC-Switch 是一个支持多客户端（Claude、Gemini、Codex 等）统一配置、管理与一键切换的本地快捷工具。
* **一键配置与导入**：为避免在不同 AI 客户端之间来回修改环境变量，本平台提供了内置的一键导入功能。在平台的**“密钥管理”**页面，点击 API Key 旁的 **“导入到 CC-Switch”**，根据需要选择要导入的客户端类型（如 Claude/Gemini/Codex），CC-Switch 将自动拉起并配置对应底层的 API Endpoint 以及 API Key，省去频繁手动切换的麻烦。
* **手动配置说明**：
  * 导入/配置 Claude 客户端：`app: "claude"`, `endpoint: "{{BASE_URL}}"`
  * 导入/配置 Codex 客户端：`app: "codex"`, `endpoint: "{{BASE_URL}}v1"`, 默认模型 `gpt-5.4`
  * 导入/配置 Gemini 客户端：`app: "gemini"`, `endpoint: "{{BASE_URL}}"`

### Cockpit 集中管理与切换

* **客户端集中控制台**：Cockpit 是一款本地 Agent 控制台 GUI 软件，能集中管理和一键拉起或切换本地的 **Claude Code CLI**、**Codex CLI**、**OpenCode CLI** 等不同 Agent 的会话。
* **配置与切换**：在 Cockpit 的配置面板中，可为不同的 AI 引擎独立设置 API 凭证，方便您根据需求一键切换所调用的 Agent 客户端。设置时请在 Cockpit 的设置面板中，将对应引擎的 API Base URL（或 Endpoint）修改为本平台的中转地址：
  * **Claude 引擎**：API Base URL / Endpoint 填入 `{{BASE_URL}}`，并填入本平台 API Key。
  * **Codex 引擎**：API Base URL / Endpoint 填入 `{{BASE_URL}}v1`，并填入本平台 API Key。
  * **OpenCode 引擎**：API Base URL / Endpoint 填入 `{{BASE_URL}}v1`，并填入本平台 API Key。

### Codex App

桌面端客户端 Codex App 可以直接在设置中将 API Base URL 替换为：
1. 将 OpenAI API Base URL 替换为：`{{BASE_URL}}v1`
2. 填入本平台生成的 API Key。


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
