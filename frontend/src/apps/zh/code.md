# 代码接入

> 从零调用 API 的语言示例。上方标签切换 curl / Python / TypeScript / Go，全页同步。

## 准备

先把密钥写入环境变量（引号里换成你的真实密钥）：

```bash
export API_KEY="在此粘贴 sk- 开头的密钥"
```

> 关掉终端窗口会失效；Windows PowerShell 用 `$env:API_KEY="..."`。

## OpenAI 兼容 · Chat Completions

```bash tab=curl group=openai-chat
curl "{{BASE_URL}}v1/chat/completions" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{ "role": "user", "content": "用一句话介绍 {{SITE_NAME}}。" }]
  }'
```

```python tab=Python group=openai-chat
# pip3 install openai
import os
from openai import OpenAI

client = OpenAI(base_url="{{BASE_URL}}v1", api_key=os.environ["API_KEY"])

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "用一句话介绍 {{SITE_NAME}}。"}],
)
print(response.choices[0].message.content)
```

```typescript tab=TypeScript group=openai-chat
// npm i openai
import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: '{{BASE_URL}}v1',
  apiKey: process.env.API_KEY,
})

const response = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  messages: [{ role: 'user', content: '用一句话介绍 {{SITE_NAME}}。' }],
})
console.log(response.choices[0]?.message?.content)
```

```go tab=Go group=openai-chat
// 标准库调用，无需第三方依赖
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

func main() {
	body, _ := json.Marshal(map[string]any{
		"model": "gpt-4o-mini",
		"messages": []map[string]string{
			{"role": "user", "content": "用一句话介绍 {{SITE_NAME}}。"},
		},
	})
	req, _ := http.NewRequest("POST", "{{BASE_URL}}v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	fmt.Println(result.Choices[0].Message.Content)
}
```

## Anthropic 兼容 · Messages

```bash tab=curl group=anthropic-msg
curl "{{BASE_URL}}v1/messages" \
  -H "x-api-key: ${API_KEY}" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 1024,
    "messages": [{ "role": "user", "content": "用一句话介绍 {{SITE_NAME}}。" }]
  }'
```

```python tab=Python group=anthropic-msg
# pip3 install anthropic
import os
from anthropic import Anthropic

client = Anthropic(base_url="{{BASE_URL}}", api_key=os.environ["API_KEY"])

message = client.messages.create(
    model="claude-sonnet-4-5",
    max_tokens=1024,
    messages=[{"role": "user", "content": "用一句话介绍 {{SITE_NAME}}。"}],
)
print(message.content[0].text)
```

```typescript tab=TypeScript group=anthropic-msg
// npm i @anthropic-ai/sdk
import Anthropic from '@anthropic-ai/sdk'

const client = new Anthropic({
  baseURL: '{{BASE_URL}}',
  apiKey: process.env.API_KEY,
})

const message = await client.messages.create({
  model: 'claude-sonnet-4-5',
  max_tokens: 1024,
  messages: [{ role: 'user', content: '用一句话介绍 {{SITE_NAME}}。' }],
})
console.log(message.content[0].type === 'text' ? message.content[0].text : '')
```

```go tab=Go group=anthropic-msg
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

func main() {
	body, _ := json.Marshal(map[string]any{
		"model":      "claude-sonnet-4-5",
		"max_tokens": 1024,
		"messages": []map[string]string{
			{"role": "user", "content": "用一句话介绍 {{SITE_NAME}}。"},
		},
	})
	req, _ := http.NewRequest("POST", "{{BASE_URL}}v1/messages", bytes.NewReader(body))
	req.Header.Set("x-api-key", os.Getenv("API_KEY"))
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	var result struct {
		Content []struct{ Text string } `json:"content"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	fmt.Println(result.Content[0].Text)
}
```

## Gemini 兼容 · generateContent

```bash tab=curl group=gemini-gen
curl "{{BASE_URL}}v1beta/models/gemini-2.5-flash:generateContent" \
  -H "x-goog-api-key: ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [{ "parts": [{ "text": "用一句话介绍 {{SITE_NAME}}。" }] }]
  }'
```

```python tab=Python group=gemini-gen
# pip3 install google-genai
import os
from google import genai

client = genai.Client(
    api_key=os.environ["API_KEY"],
    http_options={"base_url": "{{BASE_URL}}"},
)

response = client.models.generate_content(
    model="gemini-2.5-flash",
    contents="用一句话介绍 {{SITE_NAME}}。",
)
print(response.text)
```

```typescript tab=TypeScript group=gemini-gen
// npm i @google/genai
import { GoogleGenAI } from '@google/genai'

const client = new GoogleGenAI({
  apiKey: process.env.API_KEY!,
  httpOptions: { baseUrl: '{{BASE_URL}}' },
})

const response = await client.models.generateContent({
  model: 'gemini-2.5-flash',
  contents: '用一句话介绍 {{SITE_NAME}}。',
})
console.log(response.text)
```

```go tab=Go group=gemini-gen
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

func main() {
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": "用一句话介绍 {{SITE_NAME}}。"}}},
		},
	})
	req, _ := http.NewRequest("POST",
		"{{BASE_URL}}v1beta/models/gemini-2.5-flash:generateContent",
		bytes.NewReader(body))
	req.Header.Set("x-goog-api-key", os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	fmt.Printf("%+v\n", result)
}
```

<details>
<summary>流式输出（SSE）</summary>

在 OpenAI 与 Anthropic 请求体里加 `"stream": true`，客户端会按 SSE 事件逐字接收（Gemini 用 `:streamGenerateContent`）。以下以 OpenAI Chat 为例：

```bash tab=curl group=openai-stream
curl -N "{{BASE_URL}}v1/chat/completions" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"分三点介绍 {{SITE_NAME}}。"}]}'
```

```python tab=Python group=openai-stream
import os
from openai import OpenAI

client = OpenAI(base_url="{{BASE_URL}}v1", api_key=os.environ["API_KEY"])
stream = client.chat.completions.create(
    model="gpt-4o-mini",
    stream=True,
    messages=[{"role": "user", "content": "分三点介绍 {{SITE_NAME}}。"}],
)
for chunk in stream:
    print(chunk.choices[0].delta.content or "", end="", flush=True)
```

```typescript tab=TypeScript group=openai-stream
import OpenAI from 'openai'

const client = new OpenAI({ baseURL: '{{BASE_URL}}v1', apiKey: process.env.API_KEY })
const stream = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  stream: true,
  messages: [{ role: 'user', content: '分三点介绍 {{SITE_NAME}}。' }],
})
for await (const chunk of stream) {
  process.stdout.write(chunk.choices[0]?.delta?.content ?? '')
}
```

```go tab=Go group=openai-stream
// Go 标准库需要手动解析 data: 行，示例请参考社区库
// （github.com/sashabaranov/go-openai 已支持 stream: true）
```

</details>

<details>
<summary>Responses / 工具调用 / Antigravity 渠道</summary>

**Responses 接口**：`POST /v1/responses`（Codex 用的新接口）。用法与 Chat Completions 相同，但请求体字段为 `input` 而不是 `messages`。

**工具调用**：OpenAI 用 `tools` + `tool_choice` 字段；Anthropic 用 `tools`；Gemini 用 `tools[].functionDeclarations`。协议与官方一致，替换 Base URL 即可。

**Antigravity 渠道**（Anthropic 协议变体）：Base URL 改为 `{{BASE_URL}}antigravity`，其他不变。不能与标准 Anthropic 渠道混用会话上下文。

</details>

<details>
<summary>失败时看这里</summary>

- **401** — `echo ${API_KEY}` 检查环境变量是否设成功；密钥格式（Anthropic 用 `x-api-key`，OpenAI 用 `Authorization: Bearer`，Gemini 用 `x-goog-api-key`）。
- **404** — 端点路径核对：OpenAI `/v1/chat/completions`、Anthropic `/v1/messages`（**无** `v1beta`）、Gemini `/v1beta/models/<name>:generateContent`。
- **模型不可用** — 用 `curl {{BASE_URL}}v1/models -H "Authorization: Bearer ${API_KEY}"` 查当前分组开放的模型名。
- 更多详见 [错误排查](/docs/errors) 与 [端点选择指南](/docs/endpoint-selection)。

</details>
