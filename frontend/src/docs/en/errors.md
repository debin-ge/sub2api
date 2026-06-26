# Troubleshooting

{{SITE_NAME}} errors may come from the gateway itself or from upstream platforms. When troubleshooting, first check the Base URL, API Key, endpoint path, model name, and current group permissions, then decide whether an admin needs to inspect configuration.

---

## Troubleshooting Steps

We recommend using the following order to isolate issues:

1. Call `/v1/models` with the current environment variables to confirm the key and basic network path.
2. Send a minimal non-streaming request with the same model.
3. If non-streaming works, validate streaming or the full business request.
4. Compare Base URL, API Key, model name, and proxy settings across local, test, and production.
5. Use the status code to decide whether the problem is authentication, permission, path, rate limit, or upstream failure.

Minimal verification command:

```bash
curl -i "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

---

## HTTP Status Codes Reference

| Status Code | Meaning | Common Causes | Recommended Action |
| :--- | :--- | :--- | :--- |
| **401** | Unauthenticated | Missing API Key, wrong format, invalid key, or request header does not use Bearer format. | Check the `Authorization` header and confirm the key is not expired or disabled. |
| **403** | No Permission | The key is valid, but the group cannot access the model, channel, or endpoint. | Query `/v1/models` to check availability, and contact your admin to verify group permissions. |
| **404** | Not Found | Missing path, disabled endpoint, wrong model name, or missing mapping. | Verify endpoint paths, and ensure the model name matches one listed in the available models. |
| **429** | Too Many Requests | Hit a gateway, upstream account, group, or client-side rate limit or spending cap. | Lower concurrency, add retry backoff, or ask an admin to adjust limits and quotas. |
| **5xx** | Service Error | Upstream service failure, network issues, channel misconfiguration, or gateway internal error. | Record error logs, retry later, switch to fallback models, or contact your admin if needed. |

---

## 401 Unauthenticated

> [!CAUTION]
> **401 Unauthorized**: Indicates that the request failed authentication. When 401 occurs, **do not automatically retry from the client**. You should first fix the authentication configuration.

First confirm that the API Key is actually being sent in the request header:

```bash
echo "$YOUR_KEY" | wc -c
curl -i "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

### 401 Common Causes and Fixes

| Cause | Resolution |
| :--- | :--- |
| **Environment variable unset** | Run `export YOUR_KEY="..."` again in your current terminal session. |
| **Header misses Bearer prefix** | Ensure the header is formatted exactly as `Authorization: Bearer $YOUR_KEY`. |
| **Key deleted or disabled** | Check your console or ask your admin to confirm the status of the key. |
| **Confused environment keys** | Ensure development, staging, and production keys are matched correctly. |

---

## 403 No Permission

> [!IMPORTANT]
> **403 Forbidden**: Indicates that your API Key is valid, but the associated **group** lacks permission to access the target model, channel, or endpoint. It is not the same as a wrong Base URL, nor is it necessarily a typo in the key. Focus on group policies and permissions.

Common check items:
1. Verify if `/v1/models` returns the target model name.
2. Check if the current API Key is bound to the correct group.
3. Ensure the group enables the target endpoint and the corresponding channel.
4. Verify if group quota, balance, or expiration allows the request.
5. Check if an admin recently modified group permissions or pricing.

---

## 404 Not Found

> [!WARNING]
> **404 Not Found**: Usually caused by client-side URL path mismatches, disabled endpoints, or using invalid model names.

### 404 Checklist

| Check | Example Mismatch | Correct Approach / Explanation |
| :--- | :--- | :--- |
| **Duplicated `/v1`** | `{{BASE_URL}}v1/v1/chat/completions` | SDKs often append `/v1` automatically. Ensure your Base URL does not duplicate `/v1`. |
| **Missing `/v1beta`** | Mix native & compatible routes | Gemini native API requests typically require `/v1beta/models/...`. |
| **Missing `/antigravity`** | Standard OpenAI path used | Special clients or dedicated channels require the `/antigravity` path prefix. |
| **Model name exists** | Requests model not in available list | Use model names returned by `${BASE_URL}v1/models`. |
| **Endpoint enabled** | Requests image/embedding but gets 404 | Confirm whether the current deployment enables the target endpoint. |

---

## Model Unavailable

> [!NOTE]
> Common error responses include: `model_not_found`, `model_not_enabled`, `no_available_channel`, or direct upstream rejection.

### Common Root Causes

| Cause | Detailed Description |
| :--- | :--- |
| **Group does not expose model** | The group associated with the API Key does not have access permissions. |
| **Missing model mapping** | The requested model name is not mapped to any active upstream model. |
| **Channel unavailable** | The underlying account has insufficient quota, an invalid key, or is rate-limited/disabled. |
| **Pricing mismatch** | The admin has not configured pricing or multipliers for the model; requests are blocked to prevent billing errors. |
| **Wrong API family** | For example, sending an OpenAI request body format to an Anthropic model. |

---

## Unsupported Endpoint

If the response returns 404 along with an `unsupported endpoint` message, it usually means the gateway does not enable that route, or the client built the Base URL incorrectly.

| Client SDK | Recommended baseURL Config |
| :--- | :--- |
| **OpenAI SDK** | `baseURL` is usually `{{BASE_URL}}v1` |
| **Anthropic Compatible** | Request endpoint is `${BASE_URL}v1/messages`; avoid duplicating `/v1/v1/messages` |
| **Gemini Native** | Request endpoint is `${BASE_URL}v1beta/models/{model}:generateContent` |
| **Antigravity Route** | Request endpoint is `${BASE_URL}antigravity/...`, and confirm that the component is enabled |

---

## 429 Rate Limited

> [!WARNING]
> **429 Too Many Requests**: Indicates that the client requests are too fast or have exceeded the quota limit. **We strongly recommend implementing an Exponential Backoff retry strategy in your client, and avoid instant infinite retry loops.**

### 429 Scenarios and Optimization Suggestions

| Scenario | Optimization Suggestion |
| :--- | :--- |
| **Single user high concurrency** | Reduce concurrency, and introduce rate limiting or queues on the client. |
| **Batch job triggers** | Execute tasks in smaller batches with pauses between iterations. |
| **Out-of-control auto retries** | Use exponential backoff with jitter, and cap the maximum retry attempts. |
| **Upstream rate limits** | Contact your admin to configure additional channels or switch fallbacks. |
| **Group quota exceeded** | Check consumption, and contact your admin to adjust quotas or top up. |

---

## Streaming Interruptions

> [!NOTE]
> Streaming requests may disconnect mid-generation, appearing as a closed SSE (Server-Sent Events) connection, client timeout, or truncated content.

### Streaming Issues Checklist

| Possible Cause | Recommended Countermeasure |
| :--- | :--- |
| **Client timeout too short** | Increase timeouts on the HTTP client, gateway proxy, reverse proxy, and load balancer. |
| **Proxy buffering** | Disable reverse proxy buffering (e.g., in Nginx) to ensure SSE chunks are forwarded instantly. |
| **Upstream rate limit/error** | Lower concurrency and verify upstream account status. |
| **Unstable network paths** | Introduce a robust retry strategy (avoid blindly re-sending non-idempotent requests). |
| **Output too long** | Configure `max_tokens` appropriately, or break large tasks into multi-step prompts. |

> [!TIP]
> If **non-streaming** requests work fine for the same model but **streaming** requests keep failing, focus your investigation on **reverse proxy buffering configurations** and **client-side SSE parsing logic**.

---

## Minimal Reproduction & Feedback Template

To help admins isolate issues quickly, please provide the following masked information when reporting issues. **Never share your full API Key.**

```text
Investigation Time Range: YYYY-MM-DD HH:MM:SS
Request Path: e.g., /v1/chat/completions
Model Name: e.g., gpt-4o-mini
HTTP Status Code: e.g., 403
Error Summary: e.g., "model_not_enabled"
API Key Identifier: e.g., "my_dev_key" or only the last 4 characters
Client Type: e.g., OpenAI SDK / Claude Code / curl
Is Streaming: Yes / No
Deployment Environment: Local test / Staging / Production
```

### Minimal Reproducible curl Script

```bash
curl -i "${BASE_URL}v1/chat/completions" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "ping" }
    ],
    "max_tokens": 20
  }'
```

> [!TIP]
> If the minimal curl script succeeds but your application code fails, the issue is likely due to **invalid parameters**, **excessive context length**, **improper streaming/multimodal handling**, or **low timeout limits**.
