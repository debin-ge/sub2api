# API Reference

This page lists common {{SITE_NAME}} compatible endpoints. Actual support depends on the current deployment's routes, groups, upstream accounts, model mappings, channel pricing, and admin settings.

## Authentication

Most APIs use Bearer tokens:

```http
Authorization: Bearer ${API_KEY}
```

curl example:

```bash
curl "{{BASE_URL}}v1/models" \
  -H "Authorization: Bearer ${API_KEY}"
```

Some compatible clients may send keys through `api-key`, `x-api-key`, or SDK-specific fields. Unless your admin says otherwise, prefer `Authorization: Bearer ${API_KEY}`.

## OpenAI Compatible Endpoints

| Method | Path | Purpose | Notes |
| --- | --- | --- | --- |
| GET | `/v1/models` | List models available to the current key | Results vary by group and admin configuration. |
| POST | `/v1/chat/completions` | Chat Completions | Suitable for the OpenAI SDK and many compatible clients. |
| POST | `/v1/responses` | Responses API | Suitable for Codex or clients that support the Responses API. |
| POST | `/v1/embeddings` | Text embeddings | Requires an enabled embedding model. |
| POST | `/v1/images/generations` | Image generation | Requires enabled image generation channels and models. |
| POST | `/v1/images/edits` | Image editing | The client must submit images and parameters as required by the upstream. |

## Anthropic Compatible Endpoints

| Method | Path | Purpose | Notes |
| --- | --- | --- | --- |
| POST | `/v1/messages` | Messages API | Suitable for Claude Code, the Anthropic SDK, or compatible clients. |
| POST | `/v1/messages/count_tokens` | Estimate Messages tokens | Availability depends on upstream and deployment configuration. |

## Gemini Native Endpoints

| Method | Path | Purpose | Notes |
| --- | --- | --- | --- |
| GET | `/v1beta/models` | List Gemini models | The response may only include models accessible under current settings. |
| GET | `/v1beta/models/{model}` | Get a single model | `{model}` uses a supported Gemini model name or mapping name. |
| POST | `/v1beta/models/{model}:generateContent` | Non-streaming Gemini generation | Request body follows the Gemini `generateContent` format. |
| POST | `/v1beta/models/{model}:streamGenerateContent?alt=sse` | Gemini SSE streaming generation | The client must support SSE streaming. |

## Antigravity Endpoints

| Method | Path | Purpose | Notes |
| --- | --- | --- | --- |
| GET | `/antigravity/models` | List Antigravity models | Results depend on Antigravity channels and group configuration. |
| POST | `/antigravity/v1/messages` | Antigravity Messages | Usually used by Antigravity Claude compatible setups. |
| POST | `/antigravity/v1/messages/count_tokens` | Antigravity token estimate | Support depends on upstream capabilities. |
| GET | `/antigravity/v1/usage` | Query Antigravity usage | Visibility depends on the key and deployment policy. |
| GET | `/antigravity/v1beta/models` | Antigravity Gemini model list | For Gemini-style Antigravity integration. |
| ANY | `/antigravity/v1beta/models/*` | Antigravity Gemini compatible forwarding | Covers model details, generation, streaming generation, and related subpaths. |

## Request Format Notes

| API family | Message fields | Common model source |
| --- | --- | --- |
| OpenAI Chat | `messages`, each with `role` and `content` | `/v1/models` or an admin-provided mapping name. |
| OpenAI Responses | `input` or another Responses API input structure | `/v1/models` or Codex client configuration. |
| Anthropic Messages | `messages`, `max_tokens`, and optional system or tool fields | Anthropic compatible models or mapping names. |
| Gemini Native | `contents`, `parts` | `/v1beta/models` or an admin-provided Gemini model name. |

## Responses and Errors

Successful responses usually follow the corresponding compatible API structure. Errors may come from {{SITE_NAME}} itself or from an upstream platform. When troubleshooting, check:

1. Whether the API Key is correct.
2. Whether the current group can access the model.
3. Whether the request path is enabled in the current deployment.
4. Whether the request body matches the target API family.
5. Whether upstream accounts, quotas, rate limits, or model mappings are healthy.
