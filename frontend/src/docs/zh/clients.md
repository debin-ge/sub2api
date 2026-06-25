# 客户端接入

本页给出常见客户端的完整接入路径：先验证密钥和模型，再配置 SDK 或工具，最后处理流式、超时和错误。请将示例中的 `{{BASE_URL}}` 替换为管理员提供的地址，并统一通过 `$YOUR_KEY` 传递 API Key。

---

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

> [!IMPORTANT]
> 如果上述测试命令失败，先不要配置任何 SDK。请优先排查 Base URL、API Key 的正确性、分组权限以及网络连通性。

---

## curl 最小请求

### Chat Completions

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

### 流式 Chat Completions

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

> [!NOTE]
> 如果示例中的模型不可用，请改用前文 `/v1/models` 请求实际返回的可用模型名。

---

## OpenAI SDK TypeScript

多数 OpenAI SDK 需要把 `baseURL` 配置到 `/v1` 级别：

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

### 流式读取

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

> [!IMPORTANT]
> 如果环境变量中已经包含了末尾的 `/v1`（如 `{{BASE_URL}}v1`），请不要在代码实例化 SDK 时再次拼接 `/v1`。

---

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

> [!WARNING]
> 如果在调用中返回 `404` 错误，请优先检查您的 `OPENAI_BASE_URL` 是否被意外拼写成了 `{{BASE_URL}}v1/v1` 或缺少了结尾的 `/v1`。

---

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

> [!NOTE]
> Messages 请求通常需要携带 `max_tokens` 参数。如果您直接把 OpenAI 的 `messages` 数据结构复制给 Anthropic 端点，请确保字段格式符合上游或客户端的要求。

---

## Gemini 原生端点

Gemini 原生客户端必须使用 `/v1beta` 路径和 Gemini 的请求体格式。

#### 示例：非流式请求
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

#### 示例：流式请求
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

> [!IMPORTANT]
> Gemini 原生端点不兼容 OpenAI 的 `messages` 格式。请选用当前部署支持的 Gemini 模型名称或映射别名。

---

## CLI 编程客户端 (Coding Agents)

这些命令行工具直接在终端中运行。您只需配置相应的环境变量，即可在多个不同的 AI 编程助手之间轻松切换底层中转。

---

### 1. Claude Code CLI
> **官方命令行 Agent**

可通过指定 Anthropic API Base URL 将请求中转至本平台：

```bash
# 默认中转路径
export ANTHROPIC_BASE_URL="{{BASE_URL}}"
export ANTHROPIC_API_KEY="$YOUR_KEY"

# 如果使用特定的 Antigravity 兼容路径
export ANTHROPIC_BASE_URL="{{BASE_URL}}antigravity"
export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
```

> [!TIP]
> 配置完成后，直接在终端中运行 `claude` 即可拉起助手并开始对话。

---

### 2. Codex CLI
> **Responses API 编程工具**

Codex CLI 依赖 OpenAI 格式的 Responses API，可通过配置 OpenAI 环境变量进行中转：

```bash
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="$YOUR_KEY"
```

> [!WARNING]
> **Nginx 代理注意事项**：
> 若您使用 Nginx 作为反向代理，请务必在 Nginx 的 `http` 块中加入以下配置以保证会话粘性：
> ```nginx
> underscores_in_headers on;
> ```
> 否则 Nginx 默认会过滤掉请求头中含有下划线的字段（如 `session_id`），导致多账号环境下的粘性会话失效。

---

### 3. OpenCode CLI
> **OpenCode2API 兼容渠道**

配置对应的专属端点与密钥：

```bash
export OPENCODE_API_BASE="{{BASE_URL}}v1"
export OPENCODE_API_KEY="$YOUR_KEY"
```

> [!NOTE]
> 如果您使用的是通用的兼容客户端，直接将 OpenAI SDK 的 Base URL 指向本平台的 `{{BASE_URL}}v1` 即可。

---

## 第三方 GUI 与集成工具

这类图形化工具不仅能帮您存储 API 密钥，还能方便地在一处集中管理、配置并快速切换底层的不同 AI 客户端。

---

### CC-Switch
> **多客户端一键切换器**

CC-Switch 是一个支持多客户端（Claude、Gemini、Codex 等）统一配置、管理与一键切换的本地快捷工具。

#### 💡 极速配置：一键导入
本平台已深度集成 CC-Switch 一键导入协议，您无需手动复制繁琐的环境变量：
1. 访问平台的 **“密钥管理”** 页面。
2. 点击 API Key 旁的 **“导入到 CC-Switch”** 按钮。
3. 在弹窗中选择要导入的目标客户端类型（如 Claude/Gemini/Codex）。
4. 系统将自动唤起 CC-Switch 并完成代理地址与 API 密钥的配置。

#### ⚙️ 底层配置参数对照表
如果您需要手动在 CC-Switch 中配置，请参考以下参数：

| 客户端类型 (App) | 代理端点 (Endpoint) | 额外参数 (Model) |
| :--- | :--- | :--- |
| **Claude** | `{{BASE_URL}}` | - |
| **Codex** | `{{BASE_URL}}v1` | `gpt-5.4` |
| **Gemini** | `{{BASE_URL}}` | - |

---

### Cockpit
> **本地 Agent 集中控制台**

Cockpit 是一款本地 Agent 控制台 GUI 软件，能集中管理和一键拉起或切换本地的 **Claude Code CLI**、**Codex CLI**、**OpenCode CLI** 等不同 Agent 的会话。

#### 🔄 多引擎切换与配置
您可以在 Cockpit 的配置面板中，分别为各个 AI 引擎独立设置中转凭证，以实现一键切换所调用的 Agent 客户端。

* **Claude 引擎**
  - **Endpoint / API Base URL**: `{{BASE_URL}}`
  - **API Key**: 填入本平台 API Key
* **Codex 引擎**
  - **Endpoint / API Base URL**: `{{BASE_URL}}v1`
  - **API Key**: 填入本平台 API Key
* **OpenCode 引擎**
  - **Endpoint / API Base URL**: `{{BASE_URL}}v1`
  - **API Key**: 填入本平台 API Key

---

### Codex App
> **桌面客户端**

在 Codex App 客户端设置项中，替换以下配置即可完成中转：
1. **OpenAI API Base URL** 替换为：`{{BASE_URL}}v1`
2. **API Key** 填入本平台生成的 API Key。

---

## 客户端配置建议

| 项目 | 建议 |
| :--- | :--- |
| **Base URL** | 根地址用于 curl 拼接路径；OpenAI SDK 通常使用 `{{BASE_URL}}v1`。 |
| **API Key** | 使用 `$YOUR_KEY` 或客户端要求的环境变量，不要写入源码。 |
| **模型名** | 以 `/v1/models`、`/v1beta/models` 或管理员映射说明为准。 |
| **超时** | 长输出和流式请求需要更长的 HTTP、代理和负载均衡超时。 |
| **重试** | 对 429 和 5xx 使用指数退避；不要对 401、403、404 盲目重试。 |
| **日志** | 记录请求路径、模型、状态码和请求 ID，不要记录完整密钥。 |

---

## 常见接入问题

| 现象 | 可能原因 | 处理方法 |
| :--- | :--- | :--- |
| **SDK 返回 401** | API Key 没传入、变量名错误或缺少 Bearer 前缀。 | 打印环境变量是否为空，确认请求头格式。 |
| **SDK 返回 404** | Base URL 重复拼接 `/v1` 或对应端点未在服务端启用。 | 核对最终拼装出的请求 URL。 |
| **模型不存在** | 示例模型未在您当前分组下开放访问。 | 使用 `/v1/models` 返回的可用模型名。 |
| **流式没有输出** | 客户端或反向代理不支持 SSE，或超时设置过短。 | 先用非流式请求验证连接，再排查代理和超时。 |
| **本地正常生产失败** | 生产与本地 of 密钥、分组、网络出口或代理配置有差异。 | 对比分析两边的环境变量、可用模型列表和返回错误码。 |
