# FAQ

## What should I use as the Base URL?

Use the address of your {{SITE_NAME}} deployment, for example:

```text
{{BASE_URL}}
```

If an SDK asks for an OpenAI-style `baseURL`, it usually needs to point to `/v1`:

```text
{{BASE_URL}}v1
```

If your admin provides an address with a path, use that full address and avoid appending `/v1` twice.

## Which header should contain the API Key?

Prefer Bearer tokens:

```http
Authorization: Bearer $YOUR_KEY
```

With curl:

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

Some compatible clients may use `api-key`, `x-api-key`, or SDK configuration fields. Unless your admin or client requires another method, use the `Authorization` header.

## Why is my `/v1/models` result different from someone else's?

`/v1/models` returns the model view actually available to the current API Key. Different users or keys may belong to different groups, so the result is affected by group permissions, upstream accounts, model mappings, channel pricing, quotas, and admin settings.

If a model is missing, do not assume it is a frontend display issue. First confirm that the current key's group exposes the model and that an admin has configured an available channel.

## Which endpoint should I choose?

| Client or need | Recommended endpoint |
| --- | --- |
| OpenAI Chat Completions compatible client | `/v1/chat/completions` |
| OpenAI Responses or Codex-style client | `/v1/responses` |
| Claude Code or Anthropic Messages compatible client | `/v1/messages` |
| Gemini native client | `/v1beta/models/{model}:generateContent` |
| Antigravity compatible client | `/antigravity/...` |
| Embeddings | `/v1/embeddings` |
| Image generation or editing | `/v1/images/generations`, `/v1/images/edits` |

The endpoint and request body format must match. Do not send an OpenAI `messages` body directly to a Gemini native endpoint, and do not send a Gemini `contents` body to OpenAI Chat Completions.

## Why do I get a 404?

Common causes:

| Cause | Action |
| --- | --- |
| Base URL typo | Check whether `/v1` is duplicated or whether `/antigravity`, `/v1beta`, or another path is missing. |
| Endpoint not enabled | Ask an admin whether this deployment supports the API. |
| Model name does not exist | Use a model returned by `/v1/models` or a mapping name provided by an admin. |
| API family mismatch | Choose the endpoint again based on OpenAI, Anthropic, Gemini, or Antigravity format. |

## Why did the model request fail?

A model failure does not necessarily mean {{SITE_NAME}} is unavailable. Common causes include:

1. The current API Key group does not have permission for the model.
2. The upstream account has insufficient quota, an invalid key, or rate limiting.
3. The admin has not configured a model mapping or channel price.
4. Request parameters are not supported by the upstream model.
5. A streaming request was interrupted by a proxy or client timeout.

Start with:

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

Then send a minimal request with a returned model name. If the minimal request still fails, provide the status code, request path, model name, and request time to an admin. Do not send the full API Key.
