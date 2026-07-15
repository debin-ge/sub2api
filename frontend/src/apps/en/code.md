# Code

> Zero-to-API examples per language. The tabs above switch curl / Python / TypeScript / Go across the whole page.

## Setup

Export your key once:

```bash
export API_KEY="paste your sk- key here"
```

> The variable dies when you close the terminal. On Windows PowerShell use `$env:API_KEY="..."`.

## OpenAI compatible · Chat Completions

```bash tab=curl group=openai-chat
curl "{{BASE_URL}}v1/chat/completions" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{ "role": "user", "content": "Describe {{SITE_NAME}} in one sentence." }]
  }'
```

```python tab=Python group=openai-chat
# pip3 install openai
import os
from openai import OpenAI

client = OpenAI(base_url="{{BASE_URL}}v1", api_key=os.environ["API_KEY"])

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Describe {{SITE_NAME}} in one sentence."}],
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
  messages: [{ role: 'user', content: 'Describe {{SITE_NAME}} in one sentence.' }],
})
console.log(response.choices[0]?.message?.content)
```

```go tab=Go group=openai-chat
// Uses stdlib, no third-party deps
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
			{"role": "user", "content": "Describe {{SITE_NAME}} in one sentence."},
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

## Anthropic compatible · Messages

```bash tab=curl group=anthropic-msg
curl "{{BASE_URL}}v1/messages" \
  -H "x-api-key: ${API_KEY}" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 1024,
    "messages": [{ "role": "user", "content": "Describe {{SITE_NAME}} in one sentence." }]
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
    messages=[{"role": "user", "content": "Describe {{SITE_NAME}} in one sentence."}],
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
  messages: [{ role: 'user', content: 'Describe {{SITE_NAME}} in one sentence.' }],
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
			{"role": "user", "content": "Describe {{SITE_NAME}} in one sentence."},
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

## Gemini compatible · generateContent

```bash tab=curl group=gemini-gen
curl "{{BASE_URL}}v1beta/models/gemini-2.5-flash:generateContent" \
  -H "x-goog-api-key: ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [{ "parts": [{ "text": "Describe {{SITE_NAME}} in one sentence." }] }]
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
    contents="Describe {{SITE_NAME}} in one sentence.",
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
  contents: 'Describe {{SITE_NAME}} in one sentence.',
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
			{"parts": []map[string]string{{"text": "Describe {{SITE_NAME}} in one sentence."}}},
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
<summary>Streaming (SSE)</summary>

Add `"stream": true` to OpenAI and Anthropic requests (Gemini uses `:streamGenerateContent`). Example with OpenAI Chat:

```bash tab=curl group=openai-stream
curl -N "{{BASE_URL}}v1/chat/completions" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"List three points about {{SITE_NAME}}."}]}'
```

```python tab=Python group=openai-stream
import os
from openai import OpenAI

client = OpenAI(base_url="{{BASE_URL}}v1", api_key=os.environ["API_KEY"])
stream = client.chat.completions.create(
    model="gpt-4o-mini",
    stream=True,
    messages=[{"role": "user", "content": "List three points about {{SITE_NAME}}."}],
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
  messages: [{ role: 'user', content: 'List three points about {{SITE_NAME}}.' }],
})
for await (const chunk of stream) {
  process.stdout.write(chunk.choices[0]?.delta?.content ?? '')
}
```

```go tab=Go group=openai-stream
// The stdlib requires manual SSE parsing. Community libs
// (e.g. github.com/sashabaranov/go-openai) support stream: true out of the box.
```

</details>

<details>
<summary>Responses / tool calls / Antigravity channel</summary>

**Responses endpoint**: `POST /v1/responses` (used by Codex). Same shape as Chat Completions but the payload uses `input` instead of `messages`.

**Tool calls**: OpenAI uses `tools` + `tool_choice`; Anthropic uses `tools`; Gemini uses `tools[].functionDeclarations`. The wire format matches the upstream provider — just swap the Base URL.

**Antigravity channel** (Anthropic variant): change the Base URL to `{{BASE_URL}}antigravity`, everything else stays the same. Do not share a session with a standard Anthropic channel.

</details>

<details>
<summary>Troubleshooting</summary>

- **401** — check `echo ${API_KEY}` shows your key; header name matches (Anthropic `x-api-key`, OpenAI `Authorization: Bearer`, Gemini `x-goog-api-key`).
- **404** — endpoint paths: OpenAI `/v1/chat/completions`, Anthropic `/v1/messages` (**no** `v1beta`), Gemini `/v1beta/models/<name>:generateContent`.
- **Model unavailable** — `curl {{BASE_URL}}v1/models -H "Authorization: Bearer ${API_KEY}"` to list what's enabled for your group.
- More: [Troubleshooting](/docs/errors), [Endpoint Selection](/docs/endpoint-selection).

</details>
