# Client Integration

This page provides configuration examples for common clients. Replace `https://your-sub2api.example.com` and `sk-your-key` with your real values, or use `$BASE_URL` and `$SUB2API_KEY`.

## curl

List available models:

```bash
curl "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $SUB2API_KEY"
```

Send a Chat Completions request:

```bash
curl "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $SUB2API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "Hello, Sub2API." }
    ]
  }'
```

## OpenAI SDK TypeScript

```ts
import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: process.env.BASE_URL ?? 'https://your-sub2api.example.com/v1',
  apiKey: process.env.SUB2API_KEY ?? 'sk-your-key',
})

const result = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  messages: [{ role: 'user', content: 'Introduce Sub2API in one sentence.' }],
})

console.log(result.choices[0]?.message?.content)
```

If `$BASE_URL` already includes `/v1`, do not append `/v1` again. Most OpenAI SDK configurations expect `baseURL` to point at the `/v1` level.

## Claude Code Environment Variables

Claude Code or Anthropic compatible clients usually need an Anthropic Base URL and API Key. Environment variable names vary by client version, so follow the client documentation. A common setup is:

```bash
export ANTHROPIC_BASE_URL="$BASE_URL"
export ANTHROPIC_AUTH_TOKEN="$SUB2API_KEY"
```

Then choose a Claude compatible model supported by the current deployment. Model names should come from `/v1/models` or an admin-provided mapping name.

## Antigravity Claude Environment Variables

If an admin provides an Antigravity Claude compatible entry point, point the client to the `/antigravity` path:

```bash
export ANTHROPIC_BASE_URL="$BASE_URL/antigravity"
export ANTHROPIC_AUTH_TOKEN="$SUB2API_KEY"
```

Some clients require `ANTHROPIC_API_KEY` instead of `ANTHROPIC_AUTH_TOKEN`:

```bash
export ANTHROPIC_API_KEY="$SUB2API_KEY"
```

If you see 404 or model unavailable errors, confirm that the deployment has enabled `/antigravity/v1/messages` and that your group has access to the corresponding model.

## Gemini Native Endpoints

Gemini native clients should use `/v1beta` paths and Gemini request body format. Example:

```bash
curl "$BASE_URL/v1beta/models/gemini-2.0-flash:generateContent" \
  -H "Authorization: Bearer $SUB2API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          { "text": "Introduce Sub2API in one sentence." }
        ]
      }
    ]
  }'
```

Streaming requests use:

```text
$BASE_URL/v1beta/models/{model}:streamGenerateContent?alt=sse
```

Gemini native endpoints do not use the OpenAI `messages` format. Use a Gemini model name or mapping name supported by the current deployment.

## Codex `/v1/responses` Example

Coding clients that support the Responses API can point to Sub2API's `/v1/responses`:

```bash
curl "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $SUB2API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": "Write a TypeScript function that returns the sum of two numbers."
  }'
```

If the model or Responses API is not enabled in the current deployment, use an available model from `/v1/models` or ask an admin to enable the corresponding channel.

## Configuration Tips

| Item | Recommendation |
| --- | --- |
| Base URL | Configure it centrally through an environment variable, such as `https://your-sub2api.example.com`. |
| API Key | Use `$SUB2API_KEY` and avoid writing keys into source code. |
| Model name | Use `/v1/models` or admin-provided mapping documentation. |
| Timeout | Set client timeouts high enough for long outputs and streaming requests. |
| Logs | Log request IDs, model names, and error details, but never log the full API Key. |
