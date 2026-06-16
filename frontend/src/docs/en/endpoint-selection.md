# Endpoint Selection Guide

Choose endpoints based on your client and request format first, then the model. API families use different request bodies, so similar model names do not make the formats interchangeable.

## Quick Selection Table

| Need | Recommended endpoint | Request format |
| --- | --- | --- |
| List models available to the current key | `GET /v1/models` | No request body. |
| OpenAI Chat Completions client | `POST /v1/chat/completions` | `messages` array. |
| OpenAI Responses or Codex-style client | `POST /v1/responses` | `input` or Responses input structure. |
| Anthropic Messages or Claude Code | `POST /v1/messages` | `messages`, `max_tokens`. |
| Anthropic token estimation | `POST /v1/messages/count_tokens` | Messages request structure. |
| Gemini native non-streaming | `POST /v1beta/models/{model}:generateContent` | `contents`, `parts`. |
| Gemini native streaming | `POST /v1beta/models/{model}:streamGenerateContent?alt=sse` | `contents`, `parts`, SSE response. |
| Embeddings | `POST /v1/embeddings` | `input` and an embedding model. |
| Image generation | `POST /v1/images/generations` | Image generation parameters. |
| Antigravity compatible clients | `/antigravity/...` | Follow the admin-provided client configuration. |

## Selection Flow

1. Identify the client type: OpenAI SDK, Anthropic/Claude, Gemini native, Codex/Responses, or another compatible client.
2. Use the matching API family Base URL and request body.
3. Call the model list endpoint with the current API Key to confirm model names.
4. Send a minimal request to verify endpoint, model, and permission.
5. Then add business parameters, streaming, tool calls, or conversation history.

## Base URL Patterns

Command-line examples use the root address:

```bash
export BASE_URL="{{BASE_URL}}"
export YOUR_KEY="replace-with-your-api-key"
```

curl appends paths like this:

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

SDKs often need a more specific URL:

| Client | Common Base URL |
| --- | --- |
| OpenAI SDK | `{{BASE_URL}}v1` |
| Anthropic compatible clients | `{{BASE_URL}}` or the Anthropic Base URL required by the client. |
| Gemini native clients | `{{BASE_URL}}`, with `/v1beta` in request paths. |
| Antigravity clients | `{{BASE_URL}}antigravity` |

If you see `/v1/v1`, `//v1`, or a missing `/v1beta`, the Base URL and the client's automatic path joining probably do not match.

## OpenAI Compatible or Responses

Choose by client capability:

| Choice | Best for |
| --- | --- |
| `/v1/chat/completions` | Traditional chat, broad ecosystem compatibility, and function-calling compatible clients. |
| `/v1/responses` | Coding assistants, structured input, or newer clients that support the Responses API. |
| `/v1/embeddings` | Text embeddings, retrieval, and similarity search. |
| Image endpoints | Image generation, editing, or multimodal workflows. |

If your client only supports Chat Completions, do not switch to Responses just to use a model. First confirm that the deployment exposes a Chat Completions compatible path for that model.

## Anthropic vs Gemini

Anthropic Messages example:

```json
{
  "model": "claude-3-5-sonnet-latest",
  "max_tokens": 256,
  "messages": [
    { "role": "user", "content": "Hello" }
  ]
}
```

Gemini native example:

```json
{
  "contents": [
    {
      "parts": [
        { "text": "Hello" }
      ]
    }
  ]
}
```

The fields are different. Even when a model is reachable through mapping, you still need the request format that matches the endpoint.

## When to Use Antigravity

Use `/antigravity/...` only when an admin provides an Antigravity compatible entry point or your client explicitly requires Antigravity paths. Check:

1. Whether the deployment enables Antigravity routes.
2. Whether the API Key group exposes the related models.
3. Whether the client Base URL points to `{{BASE_URL}}antigravity`.
4. Whether request paths match the client's expectation.

## Endpoint Self-Check

| Problem | Confirm |
| --- | --- |
| 401 | The header includes `Authorization: Bearer $YOUR_KEY`. |
| 403 | The current key group has permission for the model and endpoint. |
| 404 | The path exists, Base URL is not duplicated, and the model name is correct. |
| Model format error | The request body belongs to the selected API family. |
| Streaming has no output | Client, proxy, and endpoint all support SSE or streaming reads. |
