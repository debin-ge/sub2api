# 代码接入

本页提供用代码调用 {{SITE_NAME}} 的完整示例，覆盖 curl、Python、TypeScript、Go 四种方式。每个示例都可以直接复制运行。

开始前请确认你已完成 [接入总览](/docs/integration-overview) 的三个前提步骤：拿到 Base URL 和 API Key、用 curl 验证密钥、确认可用模型。

> 页面中的代码示例按语言分了标签页（Tab）。点一次某个语言的标签，整页会同步切换到该语言。

## 通用准备

### 第 1 步：设置密钥环境变量

在终端中执行（引号里换成你的真实密钥）：

```bash
export API_KEY="在这里粘贴你的API密钥"
```

设置后，本页所有示例中的 `${API_KEY}`（curl）、`os.environ["API_KEY"]`（Python）、`process.env.API_KEY`（TypeScript）、`os.Getenv("API_KEY")`（Go）都会自动读到它。

> 环境变量只在当前终端窗口有效，关掉窗口需要重新执行。Windows PowerShell 的等价写法是 `$env:API_KEY="在这里粘贴你的API密钥"`。

### 第 2 步：准备语言环境

选择你的语言，完成一次性的安装准备：

```bash tab=curl group=lang-setup
# curl 无需安装：
# - macOS / Linux 系统自带，直接使用。
# - Windows 10 及以上也自带，但在 PowerShell 里请使用 curl.exe（带 .exe），
#   否则会被 PowerShell 的同名别名拦截导致参数不兼容。
curl --version
```

```python tab=Python group=lang-setup
# 前提：已安装 Python 3.9+（macOS 自带 python3；Windows 到 python.org 下载安装）
# 在终端安装 SDK（三个都装上，后面示例会分别用到）：
#   pip3 install openai anthropic google-genai
#
# 运行方式：把示例代码保存为 main.py，然后在终端执行：
#   python3 main.py
```

```typescript tab=TypeScript group=lang-setup
// 前提：已安装 Node.js 20+（到 nodejs.org 下载安装，验证：node --version）
// 在终端初始化项目并安装 SDK：
//   mkdir demo && cd demo && npm init -y
//   npm install openai @anthropic-ai/sdk @google/genai
//
// 运行方式：把示例代码保存为 main.ts，然后在终端执行：
//   npx tsx main.ts
```

```go tab=Go group=lang-setup
// 前提：已安装 Go 1.22+（到 go.dev/dl 下载安装，验证：go version）
// 在终端初始化模块：
//   mkdir demo && cd demo && go mod init demo
//
// Go 示例使用标准库 net/http 直接调用，无需安装第三方依赖。
// 运行方式：把示例代码保存为 main.go，然后在终端执行：
//   go run main.go
```

## OpenAI 兼容接口

适用于所有支持 OpenAI 格式的场景。模型名请使用 `/v1/models` 返回列表中的名称，下面以 `gpt-4o-mini` 为例。

### Chat Completions 对话

最常用的对话端点 `POST /v1/chat/completions`：

```bash tab=curl group=openai-chat
curl "{{BASE_URL}}v1/chat/completions" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "用一句话介绍 {{SITE_NAME}}。" }
    ]
  }'
```

```python tab=Python group=openai-chat
# 保存为 main.py，运行：python3 main.py
import os
from openai import OpenAI

client = OpenAI(
    base_url="{{BASE_URL}}v1",
    api_key=os.environ["API_KEY"],
)

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[
        {"role": "user", "content": "用一句话介绍 {{SITE_NAME}}。"},
    ],
)

print(response.choices[0].message.content)
```

```typescript tab=TypeScript group=openai-chat
// 保存为 main.ts，运行：npx tsx main.ts
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
// 保存为 main.go，运行：go run main.go
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		panic(err)
	}
	fmt.Println(result.Choices[0].Message.Content)
}
```

**预期结果**：输出一句关于 {{SITE_NAME}} 的介绍文字。

**如果失败**：报 401 检查 `API_KEY` 是否已导出（`echo ${API_KEY}` 应显示你的密钥）；报模型不存在则把 `gpt-4o-mini` 换成 `/v1/models` 返回的模型名；其他见 [错误排查](/docs/errors)。

### Responses 接口（Codex 风格）

`POST /v1/responses` 是 OpenAI 的新一代接口，Codex 等编码客户端使用它：

```bash tab=curl group=openai-responses
curl "{{BASE_URL}}v1/responses" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": "写一个 TypeScript 函数，返回两个数字的和。"
  }'
```

```python tab=Python group=openai-responses
# 保存为 main.py，运行：python3 main.py
import os
from openai import OpenAI

client = OpenAI(
    base_url="{{BASE_URL}}v1",
    api_key=os.environ["API_KEY"],
)

response = client.responses.create(
    model="gpt-4.1",
    input="写一个 TypeScript 函数，返回两个数字的和。",
)

print(response.output_text)
```

```typescript tab=TypeScript group=openai-responses
// 保存为 main.ts，运行：npx tsx main.ts
import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: '{{BASE_URL}}v1',
  apiKey: process.env.API_KEY,
})

const response = await client.responses.create({
  model: 'gpt-4.1',
  input: '写一个 TypeScript 函数，返回两个数字的和。',
})

console.log(response.output_text)
```

```go tab=Go group=openai-responses
// 保存为 main.go，运行：go run main.go
package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	body := []byte(`{
		"model": "gpt-4.1",
		"input": "写一个 TypeScript 函数，返回两个数字的和。"
	}`)

	req, _ := http.NewRequest("POST", "{{BASE_URL}}v1/responses", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	fmt.Println(string(raw))
}
```

**预期结果**：返回包含代码的响应。如果报 404 或模型不可用，说明当前部署未开放 Responses 接口或该模型，请改用 Chat Completions 或联系管理员。

### 流式输出（SSE）

加上 `"stream": true` 后，回复会一个字一个字地实时返回（和网页聊天的打字机效果一样）：

```bash tab=curl group=openai-stream
curl -N "{{BASE_URL}}v1/chat/completions" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "stream": true,
    "messages": [
      { "role": "user", "content": "分三点介绍 {{SITE_NAME}}。" }
    ]
  }'
```

```python tab=Python group=openai-stream
# 保存为 main.py，运行：python3 main.py
import os
from openai import OpenAI

client = OpenAI(
    base_url="{{BASE_URL}}v1",
    api_key=os.environ["API_KEY"],
)

stream = client.chat.completions.create(
    model="gpt-4o-mini",
    stream=True,
    messages=[{"role": "user", "content": "分三点介绍 {{SITE_NAME}}。"}],
)

for chunk in stream:
    print(chunk.choices[0].delta.content or "", end="", flush=True)
print()
```

```typescript tab=TypeScript group=openai-stream
// 保存为 main.ts，运行：npx tsx main.ts
import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: '{{BASE_URL}}v1',
  apiKey: process.env.API_KEY,
})

const stream = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  stream: true,
  messages: [{ role: 'user', content: '分三点介绍 {{SITE_NAME}}。' }],
})

for await (const chunk of stream) {
  process.stdout.write(chunk.choices[0]?.delta?.content ?? '')
}
process.stdout.write('\n')
```

```go tab=Go group=openai-stream
// 保存为 main.go，运行：go run main.go
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func main() {
	body, _ := json.Marshal(map[string]any{
		"model":  "gpt-4o-mini",
		"stream": true,
		"messages": []map[string]string{
			{"role": "user", "content": "分三点介绍 {{SITE_NAME}}。"},
		},
	})

	req, _ := http.NewRequest("POST", "{{BASE_URL}}v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk) == nil && len(chunk.Choices) > 0 {
			fmt.Print(chunk.Choices[0].Delta.Content)
		}
	}
	fmt.Println()
}
```

**预期结果**：文字逐段实时出现，而不是等待数秒后一次性输出。

**如果没有流式效果**：curl 必须带 `-N` 参数；经过反向代理时代理需支持 SSE 并关闭缓冲。

### Embeddings 向量

用于文本检索、相似度计算，端点 `POST /v1/embeddings`：

```bash tab=curl group=openai-embed
curl "{{BASE_URL}}v1/embeddings" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "text-embedding-3-small",
    "input": "{{SITE_NAME}} 统一多模型 API 接入。"
  }'
```

```python tab=Python group=openai-embed
# 保存为 main.py，运行：python3 main.py
import os
from openai import OpenAI

client = OpenAI(
    base_url="{{BASE_URL}}v1",
    api_key=os.environ["API_KEY"],
)

response = client.embeddings.create(
    model="text-embedding-3-small",
    input="{{SITE_NAME}} 统一多模型 API 接入。",
)

print(len(response.data[0].embedding), "维向量")
```

```typescript tab=TypeScript group=openai-embed
// 保存为 main.ts，运行：npx tsx main.ts
import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: '{{BASE_URL}}v1',
  apiKey: process.env.API_KEY,
})

const response = await client.embeddings.create({
  model: 'text-embedding-3-small',
  input: '{{SITE_NAME}} 统一多模型 API 接入。',
})

console.log(response.data[0]?.embedding.length, '维向量')
```

```go tab=Go group=openai-embed
// 保存为 main.go，运行：go run main.go
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
		"model": "text-embedding-3-small",
		"input": "{{SITE_NAME}} 统一多模型 API 接入。",
	})

	req, _ := http.NewRequest("POST", "{{BASE_URL}}v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		panic(err)
	}
	fmt.Println(len(result.Data[0].Embedding), "维向量")
}
```

**预期结果**：输出向量维度（如 `1536 维向量`）。Embedding 模型需要当前部署开放，若报错请先查 `/v1/models`。

## Anthropic Messages 接口

适用于 Claude 系列模型，端点 `POST /v1/messages`。注意：Messages 请求**必须包含 `max_tokens`**。模型名以 `/v1/models` 为准，下面以 `claude-3-5-sonnet-latest` 为例。

### 基础请求

```bash tab=curl group=anthropic-messages
curl "{{BASE_URL}}v1/messages" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "max_tokens": 512,
    "messages": [
      { "role": "user", "content": "把这段需求整理成验收标准：用户可以重置密码。" }
    ]
  }'
```

```python tab=Python group=anthropic-messages
# 保存为 main.py，运行：python3 main.py
# 需要先安装：pip3 install anthropic
import os
from anthropic import Anthropic

client = Anthropic(
    base_url="{{BASE_URL}}",
    auth_token=os.environ["API_KEY"],
)

message = client.messages.create(
    model="claude-3-5-sonnet-latest",
    max_tokens=512,
    messages=[
        {"role": "user", "content": "把这段需求整理成验收标准：用户可以重置密码。"},
    ],
)

print(message.content[0].text)
```

```typescript tab=TypeScript group=anthropic-messages
// 保存为 main.ts，运行：npx tsx main.ts
// 需要先安装：npm install @anthropic-ai/sdk
import Anthropic from '@anthropic-ai/sdk'

const client = new Anthropic({
  baseURL: '{{BASE_URL}}',
  authToken: process.env.API_KEY,
})

const message = await client.messages.create({
  model: 'claude-3-5-sonnet-latest',
  max_tokens: 512,
  messages: [{ role: 'user', content: '把这段需求整理成验收标准：用户可以重置密码。' }],
})

const block = message.content[0]
console.log(block.type === 'text' ? block.text : block)
```

```go tab=Go group=anthropic-messages
// 保存为 main.go，运行：go run main.go
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
		"model":      "claude-3-5-sonnet-latest",
		"max_tokens": 512,
		"messages": []map[string]string{
			{"role": "user", "content": "把这段需求整理成验收标准：用户可以重置密码。"},
		},
	})

	req, _ := http.NewRequest("POST", "{{BASE_URL}}v1/messages", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		panic(err)
	}
	fmt.Println(result.Content[0].Text)
}
```

**预期结果**：输出整理好的验收标准列表。

**如果失败**：报错提示缺少 `max_tokens` 说明请求体不完整；SDK 报认证错误时确认使用的是 `auth_token`/`authToken` 参数（走 Bearer 认证）而不是 `api_key`。

### 流式输出

```bash tab=curl group=anthropic-stream
curl -N "{{BASE_URL}}v1/messages" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "max_tokens": 512,
    "stream": true,
    "messages": [
      { "role": "user", "content": "分三点介绍 {{SITE_NAME}}。" }
    ]
  }'
```

```python tab=Python group=anthropic-stream
# 保存为 main.py，运行：python3 main.py
import os
from anthropic import Anthropic

client = Anthropic(
    base_url="{{BASE_URL}}",
    auth_token=os.environ["API_KEY"],
)

with client.messages.stream(
    model="claude-3-5-sonnet-latest",
    max_tokens=512,
    messages=[{"role": "user", "content": "分三点介绍 {{SITE_NAME}}。"}],
) as stream:
    for text in stream.text_stream:
        print(text, end="", flush=True)
print()
```

```typescript tab=TypeScript group=anthropic-stream
// 保存为 main.ts，运行：npx tsx main.ts
import Anthropic from '@anthropic-ai/sdk'

const client = new Anthropic({
  baseURL: '{{BASE_URL}}',
  authToken: process.env.API_KEY,
})

const stream = client.messages.stream({
  model: 'claude-3-5-sonnet-latest',
  max_tokens: 512,
  messages: [{ role: 'user', content: '分三点介绍 {{SITE_NAME}}。' }],
})

stream.on('text', (text) => process.stdout.write(text))
await stream.finalMessage()
process.stdout.write('\n')
```

```go tab=Go group=anthropic-stream
// 保存为 main.go，运行：go run main.go
// Anthropic 的 SSE 事件格式与 OpenAI 不同：内容在 content_block_delta 事件里。
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func main() {
	body, _ := json.Marshal(map[string]any{
		"model":      "claude-3-5-sonnet-latest",
		"max_tokens": 512,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": "分三点介绍 {{SITE_NAME}}。"},
		},
	})

	req, _ := http.NewRequest("POST", "{{BASE_URL}}v1/messages", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) == nil &&
			event.Type == "content_block_delta" {
			fmt.Print(event.Delta.Text)
		}
	}
	fmt.Println()
}
```

### Token 估算

发送前估算请求会消耗多少 token，端点 `POST /v1/messages/count_tokens`：

```bash tab=curl group=anthropic-count
curl "{{BASE_URL}}v1/messages/count_tokens" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "messages": [
      { "role": "user", "content": "这段话会消耗多少 token？" }
    ]
  }'
```

```python tab=Python group=anthropic-count
# 保存为 main.py，运行：python3 main.py
import os
from anthropic import Anthropic

client = Anthropic(
    base_url="{{BASE_URL}}",
    auth_token=os.environ["API_KEY"],
)

result = client.messages.count_tokens(
    model="claude-3-5-sonnet-latest",
    messages=[{"role": "user", "content": "这段话会消耗多少 token？"}],
)

print(result.input_tokens, "input tokens")
```

```typescript tab=TypeScript group=anthropic-count
// 保存为 main.ts，运行：npx tsx main.ts
import Anthropic from '@anthropic-ai/sdk'

const client = new Anthropic({
  baseURL: '{{BASE_URL}}',
  authToken: process.env.API_KEY,
})

const result = await client.messages.countTokens({
  model: 'claude-3-5-sonnet-latest',
  messages: [{ role: 'user', content: '这段话会消耗多少 token？' }],
})

console.log(result.input_tokens, 'input tokens')
```

```go tab=Go group=anthropic-count
// 保存为 main.go，运行：go run main.go
package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	body := []byte(`{
		"model": "claude-3-5-sonnet-latest",
		"messages": [{ "role": "user", "content": "这段话会消耗多少 token？" }]
	}`)

	req, _ := http.NewRequest("POST", "{{BASE_URL}}v1/messages/count_tokens", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	fmt.Println(string(raw))
}
```

**预期结果**：返回 `{"input_tokens": <数字>}`。

## Gemini 原生接口

适用于 Gemini 系列模型，使用 `/v1beta` 路径和 Gemini 原生请求格式（`contents` + `parts`，与 OpenAI 的 `messages` 不兼容）。模型名以 `/v1beta/models` 或管理员说明为准，下面以 `gemini-2.0-flash` 为例。

### generateContent 非流式

```bash tab=curl group=gemini-generate
curl "{{BASE_URL}}v1beta/models/gemini-2.0-flash:generateContent" \
  -H "Authorization: Bearer ${API_KEY}" \
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

```python tab=Python group=gemini-generate
# 保存为 main.py，运行：python3 main.py
# 需要先安装：pip3 install google-genai
import os
from google import genai

client = genai.Client(
    api_key=os.environ["API_KEY"],
    http_options={"base_url": "{{BASE_URL}}".rstrip("/")},
)

response = client.models.generate_content(
    model="gemini-2.0-flash",
    contents="用一句话介绍 {{SITE_NAME}}。",
)

print(response.text)
```

```typescript tab=TypeScript group=gemini-generate
// 保存为 main.ts，运行：npx tsx main.ts
// 需要先安装：npm install @google/genai
import { GoogleGenAI } from '@google/genai'

const client = new GoogleGenAI({
  apiKey: process.env.API_KEY,
  httpOptions: { baseUrl: '{{BASE_URL}}'.replace(/\/$/, '') },
})

const response = await client.models.generateContent({
  model: 'gemini-2.0-flash',
  contents: '用一句话介绍 {{SITE_NAME}}。',
})

console.log(response.text)
```

```go tab=Go group=gemini-generate
// 保存为 main.go，运行：go run main.go
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

	url := "{{BASE_URL}}v1beta/models/gemini-2.0-flash:generateContent"
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		panic(err)
	}
	fmt.Println(result.Candidates[0].Content.Parts[0].Text)
}
```

**预期结果**：输出一句介绍文字。官方 SDK 会自动使用 `x-goog-api-key` 请求头，{{SITE_NAME}} 同时支持它和 `Authorization: Bearer`，两种都可以。

### streamGenerateContent 流式

```bash tab=curl group=gemini-stream
curl -N "{{BASE_URL}}v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          { "text": "分三点介绍 {{SITE_NAME}}。" }
        ]
      }
    ]
  }'
```

```python tab=Python group=gemini-stream
# 保存为 main.py，运行：python3 main.py
import os
from google import genai

client = genai.Client(
    api_key=os.environ["API_KEY"],
    http_options={"base_url": "{{BASE_URL}}".rstrip("/")},
)

for chunk in client.models.generate_content_stream(
    model="gemini-2.0-flash",
    contents="分三点介绍 {{SITE_NAME}}。",
):
    print(chunk.text or "", end="", flush=True)
print()
```

```typescript tab=TypeScript group=gemini-stream
// 保存为 main.ts，运行：npx tsx main.ts
import { GoogleGenAI } from '@google/genai'

const client = new GoogleGenAI({
  apiKey: process.env.API_KEY,
  httpOptions: { baseUrl: '{{BASE_URL}}'.replace(/\/$/, '') },
})

const stream = await client.models.generateContentStream({
  model: 'gemini-2.0-flash',
  contents: '分三点介绍 {{SITE_NAME}}。',
})

for await (const chunk of stream) {
  process.stdout.write(chunk.text ?? '')
}
process.stdout.write('\n')
```

```go tab=Go group=gemini-stream
// 保存为 main.go，运行：go run main.go
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func main() {
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": "分三点介绍 {{SITE_NAME}}。"}}},
		},
	})

	url := "{{BASE_URL}}v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse"
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var chunk struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk) == nil &&
			len(chunk.Candidates) > 0 && len(chunk.Candidates[0].Content.Parts) > 0 {
			fmt.Print(chunk.Candidates[0].Content.Parts[0].Text)
		}
	}
	fmt.Println()
}
```

## Antigravity 兼容入口

如果管理员启用了 Antigravity 渠道，可通过 `/antigravity` 前缀路径访问对应的 Claude 和 Gemini 模型：

```bash
# Claude 模型走 /antigravity/v1/messages
curl "{{BASE_URL}}antigravity/v1/messages" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "max_tokens": 256,
    "messages": [
      { "role": "user", "content": "你好" }
    ]
  }'
```

SDK 或客户端接入时，把 Anthropic Base URL 指向 `{{BASE_URL}}antigravity` 即可，代码写法与上面 Anthropic 小节完全相同。何时应该选择 Antigravity 入口，见 [端点选择指南](/docs/endpoint-selection)。

## 生产接入建议

| 项目 | 建议 |
| --- | --- |
| 密钥管理 | 密钥只放环境变量或密钥管理服务，**绝不写进源码或提交到 Git**。 |
| 重试 | 对 429（限流）和 5xx 使用指数退避重试；401/403/404 重试无意义，先修配置。 |
| 超时 | 长输出和流式请求需要更长的 HTTP 超时（建议 ≥ 120 秒），反向代理和负载均衡也要同步调大。 |
| 前端安全 | 浏览器端**不要直接携带 API Key** 调用；应由你自己的服务端转发请求。 |
| 日志 | 记录请求路径、模型、状态码和请求 ID；**不要记录完整密钥**。 |
| 模型名 | 定期核对 `/v1/models`，模型可用性会随分组和渠道配置变化。 |

更多实践见 [最佳实践](/docs/best-practices)。
