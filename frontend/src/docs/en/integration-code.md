# Code Integration

This page shows how to call {{SITE_NAME}} from your own code. It follows the Chinese documentation as the source of truth and covers OpenAI-compatible, Anthropic Messages, Gemini native, and Antigravity-compatible entry points.

## Common Setup

### Step 1: Set the API Key Environment Variable

```bash
export API_KEY="paste-your-api-key-here"
```

Windows PowerShell:

```powershell
$env:API_KEY="paste-your-api-key-here"
```

### Step 2: Prepare a Language Environment

```bash tab=curl group=lang-setup
# curl requires no extra installation on macOS, Linux, or modern Windows.
# In Windows PowerShell, use curl.exe if the curl alias causes parameter issues.
curl --version
```

```python tab=Python group=lang-setup
# Python 3.9+ recommended.
# Install all SDKs used by this page:
#   pip3 install openai anthropic google-genai
#
# Save examples as main.py and run:
#   python3 main.py
```

```typescript tab=TypeScript group=lang-setup
// Node.js 20+ recommended.
// Create a project and install SDKs:
//   npm init -y
//   npm install openai @anthropic-ai/sdk @google/genai
//
// Save examples as main.mjs and run:
//   node main.mjs
```

```go tab=Go group=lang-setup
// Go 1.21+ recommended.
// Create a module:
//   go mod init demo
//
// Examples below mostly use net/http so no SDK is required.
```

## OpenAI-Compatible APIs

Use OpenAI-compatible endpoints when your client expects OpenAI SDK shapes.

| API | Endpoint | Typical use |
| --- | --- | --- |
| Chat Completions | `{{BASE_URL}}v1/chat/completions` | Chat and most OpenAI-compatible clients |
| Responses | `{{BASE_URL}}v1/responses` | Codex-style tools and newer OpenAI clients |
| Embeddings | `{{BASE_URL}}v1/embeddings` | Vector search and retrieval |

### Chat Completions

```bash tab=curl group=openai-chat
curl "{{BASE_URL}}v1/chat/completions" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "Say hello in one sentence." }
    ]
  }'
```

```python tab=Python group=openai-chat
from openai import OpenAI

client = OpenAI(
    api_key="${API_KEY}",
    base_url="{{BASE_URL}}v1",
)

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Say hello in one sentence."}],
)

print(response.choices[0].message.content)
```

```typescript tab=TypeScript group=openai-chat
import OpenAI from "openai";

const client = new OpenAI({
  apiKey: process.env.API_KEY,
  baseURL: "{{BASE_URL}}v1",
});

const response = await client.chat.completions.create({
  model: "gpt-4o-mini",
  messages: [{ role: "user", content: "Say hello in one sentence." }],
});

console.log(response.choices[0]?.message?.content);
```

```go tab=Go group=openai-chat
package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	body := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Say hello in one sentence."}]}`)
	req, _ := http.NewRequest("POST", "{{BASE_URL}}v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	fmt.Println(string(data))
}
```

### Responses API

Use `/v1/responses` for tools or SDK code that expects OpenAI's Responses-style API.

```bash tab=curl group=openai-responses
curl "{{BASE_URL}}v1/responses" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.5",
    "input": "Summarize what an API gateway does in one sentence."
  }'
```

```python tab=Python group=openai-responses
from openai import OpenAI

client = OpenAI(api_key="${API_KEY}", base_url="{{BASE_URL}}v1")

response = client.responses.create(
    model="gpt-5.5",
    input="Summarize what an API gateway does in one sentence.",
)

print(response.output_text)
```

```typescript tab=TypeScript group=openai-responses
import OpenAI from "openai";

const client = new OpenAI({
  apiKey: process.env.API_KEY,
  baseURL: "{{BASE_URL}}v1",
});

const response = await client.responses.create({
  model: "gpt-5.5",
  input: "Summarize what an API gateway does in one sentence.",
});

console.log(response.output_text);
```

```go tab=Go group=openai-responses
package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
)

func main() {
	body := []byte(`{"model":"gpt-5.5","input":"Summarize what an API gateway does in one sentence."}`)
	req, _ := http.NewRequest("POST", "{{BASE_URL}}v1/responses", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	println(string(data))
}
```

### Streaming

```bash tab=curl group=openai-stream
curl "{{BASE_URL}}v1/chat/completions" \
  -N \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "stream": true,
    "messages": [{ "role": "user", "content": "Count from one to five." }]
  }'
```

```python tab=Python group=openai-stream
from openai import OpenAI

client = OpenAI(api_key="${API_KEY}", base_url="{{BASE_URL}}v1")

stream = client.chat.completions.create(
    model="gpt-4o-mini",
    stream=True,
    messages=[{"role": "user", "content": "Count from one to five."}],
)

for chunk in stream:
    delta = chunk.choices[0].delta.content
    if delta:
        print(delta, end="")
```

```typescript tab=TypeScript group=openai-stream
import OpenAI from "openai";

const client = new OpenAI({
  apiKey: process.env.API_KEY,
  baseURL: "{{BASE_URL}}v1",
});

const stream = await client.chat.completions.create({
  model: "gpt-4o-mini",
  stream: true,
  messages: [{ role: "user", content: "Count from one to five." }],
});

for await (const chunk of stream) {
  process.stdout.write(chunk.choices[0]?.delta?.content ?? "");
}
```

```go tab=Go group=openai-stream
// Streaming responses are Server-Sent Events.
// Use an HTTP client that reads the response body incrementally instead of waiting for EOF.
```

### Embeddings

```bash tab=curl group=openai-embed
curl "{{BASE_URL}}v1/embeddings" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "text-embedding-3-small",
    "input": "The quick brown fox jumps over the lazy dog."
  }'
```

```python tab=Python group=openai-embed
from openai import OpenAI

client = OpenAI(api_key="${API_KEY}", base_url="{{BASE_URL}}v1")
response = client.embeddings.create(
    model="text-embedding-3-small",
    input="The quick brown fox jumps over the lazy dog.",
)
print(len(response.data[0].embedding))
```

```typescript tab=TypeScript group=openai-embed
import OpenAI from "openai";

const client = new OpenAI({
  apiKey: process.env.API_KEY,
  baseURL: "{{BASE_URL}}v1",
});

const response = await client.embeddings.create({
  model: "text-embedding-3-small",
  input: "The quick brown fox jumps over the lazy dog.",
});

console.log(response.data[0].embedding.length);
```

```go tab=Go group=openai-embed
// Use POST {{BASE_URL}}v1/embeddings with the same Bearer token header.
// The response contains data[0].embedding as a numeric vector.
```

## Anthropic Messages API

Use the root Base URL, not `v1`, when configuring Anthropic SDKs.

### Basic Request

```bash tab=curl group=anthropic-messages
curl "{{BASE_URL}}v1/messages" \
  -H "x-api-key: ${API_KEY}" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 256,
    "messages": [{ "role": "user", "content": "Say hello in one sentence." }]
  }'
```

```python tab=Python group=anthropic-messages
from anthropic import Anthropic

client = Anthropic(
    api_key="${API_KEY}",
    base_url="{{BASE_URL}}",
)

message = client.messages.create(
    model="claude-sonnet-4-5",
    max_tokens=256,
    messages=[{"role": "user", "content": "Say hello in one sentence."}],
)

print(message.content[0].text)
```

```typescript tab=TypeScript group=anthropic-messages
import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic({
  apiKey: process.env.API_KEY,
  baseURL: "{{BASE_URL}}",
});

const message = await client.messages.create({
  model: "claude-sonnet-4-5",
  max_tokens: 256,
  messages: [{ role: "user", content: "Say hello in one sentence." }],
});

console.log(message.content[0]);
```

```go tab=Go group=anthropic-messages
// POST {{BASE_URL}}v1/messages with x-api-key and anthropic-version headers.
// Use the same JSON body shown in the curl example.
```

### Streaming

```bash tab=curl group=anthropic-stream
curl "{{BASE_URL}}v1/messages" \
  -N \
  -H "x-api-key: ${API_KEY}" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 256,
    "stream": true,
    "messages": [{ "role": "user", "content": "Count from one to five." }]
  }'
```

```python tab=Python group=anthropic-stream
from anthropic import Anthropic

client = Anthropic(api_key="${API_KEY}", base_url="{{BASE_URL}}")

with client.messages.stream(
    model="claude-sonnet-4-5",
    max_tokens=256,
    messages=[{"role": "user", "content": "Count from one to five."}],
) as stream:
    for text in stream.text_stream:
        print(text, end="")
```

```typescript tab=TypeScript group=anthropic-stream
import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic({
  apiKey: process.env.API_KEY,
  baseURL: "{{BASE_URL}}",
});

const stream = await client.messages.create({
  model: "claude-sonnet-4-5",
  max_tokens: 256,
  stream: true,
  messages: [{ role: "user", content: "Count from one to five." }],
});

for await (const event of stream) {
  if (event.type === "content_block_delta") {
    process.stdout.write(event.delta.text ?? "");
  }
}
```

```go tab=Go group=anthropic-stream
// Anthropic streaming also uses SSE. Read the response body incrementally.
```

### Token Counting

```bash tab=curl group=anthropic-count
curl "{{BASE_URL}}v1/messages/count_tokens" \
  -H "x-api-key: ${API_KEY}" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-5",
    "messages": [{ "role": "user", "content": "How many tokens is this?" }]
  }'
```

```python tab=Python group=anthropic-count
from anthropic import Anthropic

client = Anthropic(api_key="${API_KEY}", base_url="{{BASE_URL}}")
result = client.messages.count_tokens(
    model="claude-sonnet-4-5",
    messages=[{"role": "user", "content": "How many tokens is this?"}],
)
print(result.input_tokens)
```

```typescript tab=TypeScript group=anthropic-count
import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic({
  apiKey: process.env.API_KEY,
  baseURL: "{{BASE_URL}}",
});

const result = await client.messages.countTokens({
  model: "claude-sonnet-4-5",
  messages: [{ role: "user", content: "How many tokens is this?" }],
});

console.log(result.input_tokens);
```

```go tab=Go group=anthropic-count
// POST {{BASE_URL}}v1/messages/count_tokens with Anthropic headers.
```

## Gemini Native APIs

Gemini native endpoints use `/v1beta` and Google-style request bodies.

### generateContent

```bash tab=curl group=gemini-generate
curl "{{BASE_URL}}v1beta/models/gemini-2.5-pro:generateContent?key=${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      { "parts": [{ "text": "Say hello in one sentence." }] }
    ]
  }'
```

```python tab=Python group=gemini-generate
from google import genai

client = genai.Client(
    api_key="${API_KEY}",
    http_options={"base_url": "{{BASE_URL}}v1beta"},
)

response = client.models.generate_content(
    model="gemini-2.5-pro",
    contents="Say hello in one sentence.",
)

print(response.text)
```

```typescript tab=TypeScript group=gemini-generate
import { GoogleGenAI } from "@google/genai";

const ai = new GoogleGenAI({
  apiKey: process.env.API_KEY,
  httpOptions: { baseUrl: "{{BASE_URL}}v1beta" },
});

const response = await ai.models.generateContent({
  model: "gemini-2.5-pro",
  contents: "Say hello in one sentence.",
});

console.log(response.text);
```

```go tab=Go group=gemini-generate
// POST {{BASE_URL}}v1beta/models/{model}:generateContent?key=${API_KEY}
// with a Gemini contents body.
```

### streamGenerateContent

```bash tab=curl group=gemini-stream
curl "{{BASE_URL}}v1beta/models/gemini-2.5-pro:streamGenerateContent?key=${API_KEY}&alt=sse" \
  -N \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      { "parts": [{ "text": "Count from one to five." }] }
    ]
  }'
```

```python tab=Python group=gemini-stream
from google import genai

client = genai.Client(
    api_key="${API_KEY}",
    http_options={"base_url": "{{BASE_URL}}v1beta"},
)

for chunk in client.models.generate_content_stream(
    model="gemini-2.5-pro",
    contents="Count from one to five.",
):
    print(chunk.text, end="")
```

```typescript tab=TypeScript group=gemini-stream
import { GoogleGenAI } from "@google/genai";

const ai = new GoogleGenAI({
  apiKey: process.env.API_KEY,
  httpOptions: { baseUrl: "{{BASE_URL}}v1beta" },
});

const stream = await ai.models.generateContentStream({
  model: "gemini-2.5-pro",
  contents: "Count from one to five.",
});

for await (const chunk of stream) {
  process.stdout.write(chunk.text ?? "");
}
```

```go tab=Go group=gemini-stream
// Streaming Gemini native calls use SSE with alt=sse.
```

## Antigravity-Compatible Entry

Antigravity-compatible Claude requests use the Anthropic Messages shape with an Antigravity-specific route.

```bash
# Claude models through /antigravity/v1/messages
curl "{{BASE_URL}}antigravity/v1/messages" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 256,
    "messages": [{ "role": "user", "content": "Say hello." }]
  }'
```

For SDK or client configuration, point the Anthropic Base URL to `{{BASE_URL}}antigravity`. Use the same request body as Anthropic Messages. See [Endpoint Selection Guide](/docs/endpoint-selection) for when to choose this entry.

## Production Recommendations

| Recommendation | Details |
| --- | --- |
| Verify with `/v1/models` first | Do not debug SDK code until the basic curl check succeeds. |
| Pin model names from the deployment | Examples explain request formats; production code should use real model IDs returned by your key. |
| Keep keys server-side | Never ship real API Keys in frontend code, mobile bundles, logs, or public repositories. |
| Add timeouts and retries | Retry transient 429 / 5xx responses with backoff. Do not retry non-idempotent operations blindly. |
| Handle streaming explicitly | SSE responses require clients and proxies that support long-lived connections. |
| Log request IDs | Keep request IDs or gateway trace fields when debugging billing, quota, or upstream failures. |

More production guidance is in [Best Practices](/docs/best-practices).
