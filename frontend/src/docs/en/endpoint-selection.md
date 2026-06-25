# Endpoint Selection Guide

Choose endpoints based on your client and request format first, then the model. **API families use different request bodies, so similar model names do not make the formats interchangeable.**

---

## Quick Selection Table

| Integration Need | Recommended Endpoint | Request Format |
| :--- | :--- | :--- |
| **List models available to current key** | `GET /v1/models` | No request body. |
| **OpenAI Chat Completions client** | `POST /v1/chat/completions` | `messages` object array. |
| **OpenAI Responses or Codex-style client** | `POST /v1/responses` | `input` or Responses input structure. |
| **Anthropic Messages or Claude Code** | `POST /v1/messages` | `messages` and `max_tokens`. |
| **Anthropic token estimation** | `POST /v1/messages/count_tokens` | Messages request structure. |
| **Gemini native non-streaming** | `POST /v1beta/models/{model}:generateContent` | `contents` and `parts`. |
| **Gemini native streaming** | `POST /v1beta/models/{model}:streamGenerateContent?alt=sse` | `contents` and `parts` via SSE. |
| **Embeddings** | `POST /v1/embeddings` | `input` and an embedding model name. |
| **Image generation** | `POST /v1/images/generations` | Image parameters (dimensions, prompts). |
| **Antigravity compatible clients** | `/antigravity/...` | Follow admin-provided custom client guide. |

---

## Integration Flow

1. **Identify the Client**: Determine the SDK/framework you are using (e.g. OpenAI SDK, Anthropic SDK, Gemini native, Codex, etc.).
2. **Setup the Base URL**: Feed the endpoint configurations into your client instance.
3. **Confirm Model Names**: Run the model list query to fetch available names.
4. **Run a Minimal Check**: Dispatch a simple query to verify basic routing and authentication.
5. **Add Advanced Options**: Turn on streaming (SSE), tool calls, or multi-turn history.

---

## Base URL Patterns

Generic shell scripts expect the root address as:

```bash
export BASE_URL="{{BASE_URL}}"
export YOUR_KEY="replace-with-your-api-key"
```

Which allows appending the path using `curl`:

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

Official SDKs usually require more specific paths:

| Client Library | Common Base URL Configuration |
| :--- | :--- |
| **OpenAI SDK (JS/TS/Python)** | `{{BASE_URL}}v1` |
| **Anthropic SDK / Claude** | `{{BASE_URL}}` (SDKs usually append subpaths internally) |
| **Gemini Native SDK** | `{{BASE_URL}}` (SDKs append `/v1beta` internally) |
| **Antigravity Clients** | `{{BASE_URL}}antigravity` |

> [!WARNING]
> **Path Check**: If you see `404 Not Found` or path issues like `/v1/v1` or `//v1` in your request logs, it means the client's automatic path joining rules do not match the Base URL you configured. Check trailing slashes `/`.

---

## OpenAI Compatible or Responses

Choose by client capability:

| Endpoint Type | Best For | Advantages |
| :--- | :--- | :--- |
| `/v1/chat/completions` | Traditional chat, general Agents, function calling | Broadest ecosystem support and tooling. |
| `/v1/responses` | Coding assistants, structured input, or Responses API | Advanced agent-system interactions. |
| `/v1/embeddings` | Text embeddings, vector stores, semantic search | High performance vector index generation. |
| **Image Endpoints** | Image generation, edits, or multimodal flows | Direct DALL-E compatibility. |

> [!IMPORTANT]
> If your client only supports Chat Completions, **do not switch to Responses payload structure just to use a model**. Confirm first that the deployment exposes a Chat Completions compatible path for that model on the server side.

---

## Anthropic vs Gemini Payloads

#### Anthropic Messages Payload Example
```json
{
  "model": "claude-3-5-sonnet-latest",
  "max_tokens": 256,
  "messages": [
    { "role": "user", "content": "Hello" }
  ]
}
```

#### Gemini Native Payload Example
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

> [!NOTE]
> Even when models are mapped seamlessly behind the scenes, **the payloads are not interchangeable**. You must structure requests in the format defined by the endpoint.

---

## When to Use Antigravity

Use `/antigravity/...` only when an admin provides an Antigravity compatible entry point or your client explicitly requires Antigravity paths. Check:
1. Whether the deployment enables Antigravity routes.
2. Whether the API Key group exposes the related models.
3. Whether the client Base URL points to `{{BASE_URL}}antigravity`.

---

## Endpoint Self-Check

| Symptom | Cause | Action |
| :--- | :--- | :--- |
| **401 Unauthorized** | Credentials check failed. | Confirm the header includes `Authorization: Bearer $YOUR_KEY`. |
| **403 Forbidden** | Model is not enabled. | Ensure the current key group has permission for the model. |
| **404 Not Found** | Wrong path or model. | Verify the path exists and model name has no typos. |
| **Format Error** | Payload structure mismatch. | Check if the request body matches the target API family format. |
| **No Streaming Output** | SSE connection dropped. | Verify that proxies (like Nginx/Cloudflare) do not buffer chunks. |
