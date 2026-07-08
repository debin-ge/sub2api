# Client Integration

This page gives a full integration path for common clients: verify the key and models first, configure an SDK or tool next, then handle streaming, timeouts, and errors. Replace `{{BASE_URL}}` with the address provided by an admin, and pass the API Key through `${API_KEY}`.

## Before You Configure a Client

Set common environment variables:

```bash
export API_KEY="replace-with-your-api-key"
```

List models with the current key:

```bash
curl "{{BASE_URL}}v1/models" \
  -H "Authorization: Bearer ${API_KEY}"
```

If this command fails, do not configure the SDK yet. Fix Base URL, API Key, group permission, or network connectivity first.

## Minimal curl Requests

Chat Completions:

```bash
curl "{{BASE_URL}}v1/chat/completions" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "Hello, {{SITE_NAME}}." }
    ]
  }'
```

Streaming Chat Completions:

```bash
curl -N "{{BASE_URL}}v1/chat/completions" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "stream": true,
    "messages": [
      { "role": "user", "content": "Explain {{SITE_NAME}} in three points." }
    ]
  }'
```

If the example model is unavailable, use a model returned by `/v1/models`.

## OpenAI SDK TypeScript

Most OpenAI SDK configurations expect `baseURL` at the `/v1` level:

```bash
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="${API_KEY}"
```

```ts
import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: process.env.OPENAI_BASE_URL,
  apiKey: process.env.OPENAI_API_KEY,
})

const result = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  messages: [{ role: 'user', content: 'Introduce {{SITE_NAME}} in one sentence.' }],
})

console.log(result.choices[0]?.message?.content)
```

Streaming:

```ts
const stream = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  stream: true,
  messages: [{ role: 'user', content: 'Return a short outline.' }],
})

for await (const chunk of stream) {
  process.stdout.write(chunk.choices[0]?.delta?.content ?? '')
}
```

If the environment variable already includes `/v1`, do not append `/v1` again in code.

## OpenAI SDK Python

```bash
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="${API_KEY}"
```

```python
from openai import OpenAI

client = OpenAI()

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[
        {"role": "user", "content": "Introduce {{SITE_NAME}} in one sentence."},
    ],
)

print(response.choices[0].message.content)
```

If you see `404`, first check whether `OPENAI_BASE_URL` became `{{BASE_URL}}v1/v1` or is missing `/v1`.

## Claude Code Environment Variables

Claude Code or Anthropic compatible clients usually need an Anthropic Base URL and API Key. Environment variable names vary by client version, so follow the client documentation. A common setup is:

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}"
export ANTHROPIC_AUTH_TOKEN="${API_KEY}"
```

Some clients require `ANTHROPIC_API_KEY`:

```bash
export ANTHROPIC_API_KEY="${API_KEY}"
```

Then choose a Claude compatible model supported by the current deployment. Model names should come from `/v1/models` or an admin-provided mapping name.

## Anthropic Messages Request

```bash
curl "{{BASE_URL}}v1/messages" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "max_tokens": 512,
    "messages": [
      { "role": "user", "content": "Turn these requirements into acceptance criteria." }
    ]
  }'
```

Messages requests usually require `max_tokens`. If you copy an OpenAI `messages` shape to an Anthropic endpoint, also confirm that the fields match the client or upstream requirements.

## Antigravity Claude Environment Variables

If an admin provides an Antigravity Claude compatible entry point, point the client to the `/antigravity` path:

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}antigravity"
export ANTHROPIC_AUTH_TOKEN="${API_KEY}"
```

If you see 404 or model unavailable errors, confirm that the deployment has enabled `/antigravity/v1/messages` and that your group has access to the corresponding model.

## Gemini Native Endpoints

Gemini native clients should use `/v1beta` paths and Gemini request body format. Non-streaming example:

```bash
curl "{{BASE_URL}}v1beta/models/gemini-2.0-flash:generateContent" \
  -H "Authorization: Bearer ${API_KEY}" \
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
curl -N "{{BASE_URL}}v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse" \
  -H "Authorization: Bearer ${API_KEY}" \
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

Gemini native endpoints do not use the OpenAI `messages` format. Use a Gemini model name or mapping name supported by the current deployment.

## Codex `/v1/responses` Example

Coding clients that support the Responses API can point to {{SITE_NAME}}'s `/v1/responses`:

```bash
curl "{{BASE_URL}}v1/responses" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": "Write a TypeScript function that returns the sum of two numbers."
  }'
```

If the model or Responses API is not enabled in the current deployment, use an available model from `/v1/models` or ask an admin to enable the corresponding channel.

## Client Configuration Tips

| Item | Recommendation |
| --- | --- |
| Base URL | Use the root address for curl path joining; OpenAI SDKs usually use `{{BASE_URL}}v1`. |
| API Key | Use `${API_KEY}` or the environment variable required by the client, and do not write keys into source code. |
| Model name | Use `/v1/models`, `/v1beta/models`, or admin-provided mapping documentation. |
| Timeout | Long outputs and streaming requests need longer HTTP, proxy, and load balancer timeouts. |
| Retry | Use exponential backoff for 429 and 5xx; do not blindly retry 401, 403, or 404. |
| Logs | Log request path, model, status code, and request ID, but never the full key. |

## Common Integration Issues

| Symptom | Possible cause | Action |
| --- | --- | --- |
| SDK returns 401 | API Key is not passed, variable name is wrong, or Bearer is missing. | Check whether the environment variable is empty and confirm the header format. |
| SDK returns 404 | Base URL duplicates `/v1` or the endpoint is not enabled. | Inspect the final request URL. |
| Model does not exist | The example model is not exposed to the current group. | Use a model returned by `/v1/models`. |
| Streaming has no output | Client or proxy does not support SSE, or timeout is too short. | Verify non-streaming first, then check proxy and timeout settings. |
| Local works but production fails | Production key, group, egress network, or proxy config differs. | Compare environment variables, model lists, and status codes. |
