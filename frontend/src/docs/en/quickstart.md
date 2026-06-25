# Quick Start

This page shows how to send your first request through {{SITE_NAME}}. Replace the placeholder address and key with your own deployment information.

> [!TIP]
> Make sure you have obtained or generated your **Base URL** and **API Key** from the platform dashboard before proceeding.

---

## 1. Confirm the Base URL

The Base URL is the address of your {{SITE_NAME}} deployment, for example:

```text
{{BASE_URL}}
```

The examples below use `$BASE_URL`. If your deployment is behind a custom gateway or reverse proxy path, use the full address provided by your admin.

---

## 2. Set the API Key

{{SITE_NAME}} API Keys are usually passed through HTTP Bearer authentication:

```http
Authorization: Bearer $YOUR_KEY
```

To set the API Key as an environment variable in your terminal, run:

```bash
export YOUR_KEY="your-api-key"
```

> [!WARNING]
> **Security Notice**: Do not expose real API keys in frontend code, client-side console logs, or public Git repositories to prevent credential abuse.

---

## 3. List Available Models

Query the `/v1/models` endpoint first to confirm that the key is valid and to see the models actually available to the current key group.

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

> [!NOTE]
> The model list returned depends on deployment configurations. It is affected by the API Key's group parameters, upstream account statuses, model mappings, and channel allocations.

---

## 4. First Chat Completions Request

If your client or SDK uses the standard OpenAI Chat Completions format, call the `/v1/chat/completions` endpoint:

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

> [!TIP]
> If the example model is not available, replace it with a model name returned by the `/v1/models` endpoint in Step 3.

---

## 5. First Messages Request

If your client or SDK uses the Anthropic Messages format, call the `/v1/messages` compatibility endpoint:

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

> [!IMPORTANT]
> If the deployment has not enabled the Anthropic compatible endpoint or model mapping for your group, you may receive a 403 or 404 error. Contact your admin or choose an available model from `/v1/models`.

---

## 6. Common Checks

| Check | Description and Troubleshooting |
| :--- | :--- |
| **Base URL** | Make sure there are no extra trailing paths, spaces, or typos. |
| **API Key** | Confirm that the request header is passed as `Authorization: Bearer $YOUR_KEY`. |
| **Model name** | Prefer names returned by `/v1/models`. |
| **Endpoint format** | OpenAI, Anthropic, Gemini, and other APIs use different payload structures. Do not mix them. |
| **Permissions** | 403 or 404 errors usually mean your API Key group lacks access to the requested model. |

---

## 7. Next Steps

| Goal | Documentation |
| :--- | :--- |
| **Understand key permissions and security** | [API Keys and Accounts](api-keys) |
| **Integrate a specific SDK or CLI agent** | [Client Integration](clients) |
| **Copy ready-made configuration templates** | [Copy-Ready Configuration Snippets](configuration-snippets) |
| **Find endpoint details and model lists** | [Endpoint Selection Guide](endpoint-selection) |
| **Fix request failures or routing errors** | [Troubleshooting](errors) |
