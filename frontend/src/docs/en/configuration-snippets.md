# Copy-Ready Configuration Snippets

This page provides templates you can copy and adjust. Unless an admin provides another address, examples use `{{BASE_URL}}` as the Base URL and pass the API Key through `$YOUR_KEY`.

## Common Environment Variables

```bash
export BASE_URL="{{BASE_URL}}"
export YOUR_KEY="replace-with-your-api-key"
```

Verify the key:

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
    { role: 'user', content: 'Introduce {{SITE_NAME}} in one sentence.' },
  ],
})

console.log(response.choices[0]?.message?.content)
```

Recommended environment variables:

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
        {"role": "user", "content": "Introduce {{SITE_NAME}} in one sentence."},
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
      { "role": "system", "content": "You are a concise assistant." },
      { "role": "user", "content": "Write a three-point action list." }
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
    "input": "Write a debounce function in TypeScript."
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
      { "role": "user", "content": "Turn these requirements into acceptance criteria." }
    ]
  }'
```

## Claude Code or Anthropic Compatible Clients

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}"
export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
```

If the client requires `ANTHROPIC_API_KEY`:

```bash
export ANTHROPIC_API_KEY="$YOUR_KEY"
```

If you use an Antigravity Claude compatible entry point:

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}antigravity"
export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
```

## Gemini Native curl

Non-streaming:

```bash
curl "${BASE_URL}v1beta/models/gemini-2.0-flash:generateContent" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          { "text": "Introduce {{SITE_NAME}} in one sentence." }
        ]
      }
    ]
  }'
```

Streaming:

```bash
curl "${BASE_URL}v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          { "text": "Stream a short outline." }
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
    "input": "{{SITE_NAME}} unifies multi-model API access."
  }'
```

## Safe Proxy for Frontend Apps

Do not expose API Keys directly in browsers. Frontend apps should call your own server endpoint, and the server should call {{SITE_NAME}}:

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
    messages: [{ role: 'user', content: 'Hello' }],
  }),
})
```

## Common Replacements

| Placeholder | Replace with |
| --- | --- |
| `$YOUR_KEY` | Your {{SITE_NAME}} API Key environment variable. |
| `gpt-4o-mini` | An OpenAI compatible model returned by `/v1/models`. |
| `claude-3-5-sonnet-latest` | An Anthropic compatible model returned by `/v1/models` or provided by an admin. |
| `gemini-2.0-flash` | A Gemini model returned by `/v1beta/models` or provided by an admin. |
| `{{BASE_URL}}` | The deployment address provided by an admin. |
