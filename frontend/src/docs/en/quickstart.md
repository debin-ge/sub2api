# Quick Start

This page shows how to send your first request through {{SITE_NAME}}. Replace the placeholder address and key with your own deployment information.

```bash
export BASE_URL="{{BASE_URL}}"
export YOUR_KEY="your-api-key"
```

## 1. Confirm the Base URL

The Base URL is the address of your {{SITE_NAME}} deployment, for example:

```text
{{BASE_URL}}
```

The examples below use `$BASE_URL`. If your deployment is behind a reverse proxy path, use the full address provided by your admin.

## 2. Set the API Key

{{SITE_NAME}} API Keys are usually passed through HTTP Bearer authentication:

```http
Authorization: Bearer $YOUR_KEY
```

The command-line examples use `$YOUR_KEY`:

```bash
export YOUR_KEY="your-api-key"
```

Do not expose real keys in frontend code, logs, or public repositories.

## 3. List Available Models

Query `/v1/models` first to confirm that the key is valid and to see the models actually available to the current group.

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

The response depends on deployment configuration. The model list is affected by the API Key's group, upstream accounts, model mappings, channel pricing, and admin settings.

## 4. First Chat Completions Request

If your client uses the OpenAI Chat Completions format, call `/v1/chat/completions`:

```bash
curl "${BASE_URL}v1/chat/completions" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "Introduce {{SITE_NAME}} in one sentence." }
    ]
  }'
```

If the example model is not available, use a model name returned by `/v1/models`.

## 5. First Messages Request

If your client uses the Anthropic Messages format, call `/v1/messages`:

```bash
curl "${BASE_URL}v1/messages" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "max_tokens": 256,
    "messages": [
      { "role": "user", "content": "Introduce {{SITE_NAME}} in one sentence." }
    ]
  }'
```

If the deployment has not enabled the Anthropic compatible endpoint or model mapping for your group, ask an admin to adjust the configuration or choose an available model from `/v1/models`.

## 6. Common Checks

| Check | Description |
| --- | --- |
| Base URL | Make sure there are no extra paths, spaces, or typos. |
| API Key | Use `Authorization: Bearer $YOUR_KEY`. |
| Model name | Prefer names returned by `/v1/models`. |
| Endpoint format | OpenAI, Anthropic, Gemini, and other APIs use different request body formats. Do not mix them directly. |
| Permissions | 403, 404, or model unavailable errors are often related to groups, channels, or model mappings. |

## 7. Next Steps

| Next step | Document |
| --- | --- |
| Understand key permissions and security | API Keys and Accounts. |
| Integrate a specific SDK or command-line tool | Client Integration. |
| Copy complete configuration templates | Copy-Ready Configuration Snippets. |
| Choose models and endpoints | Models and Platforms, Endpoint Selection Guide. |
| Fix failed requests | Troubleshooting. |
