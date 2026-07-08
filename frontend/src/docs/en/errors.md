# Troubleshooting

{{SITE_NAME}} errors may come from the gateway itself or from upstream platforms. When troubleshooting, first check the Base URL, API Key, endpoint path, model name, and current group permissions, then decide whether an admin needs to inspect configuration.

## Troubleshooting Order

Use this order:

1. Call `/v1/models` with the current environment variables to confirm the key and basic network path.
2. Send a minimal non-streaming request with the same model.
3. If non-streaming works, validate streaming or the full business request.
4. Compare Base URL, API Key, model name, and proxy settings across local, test, and production.
5. Use the status code to decide whether the problem is authentication, permission, path, rate limit, or upstream failure.

Minimal verification command:

```bash
curl -i "{{BASE_URL}}v1/models" \
  -H "Authorization: Bearer ${API_KEY}"
```

## HTTP Status Codes

| Status | Meaning | Common causes | Recommended action |
| --- | --- | --- | --- |
| 401 | Unauthenticated | Missing API Key, wrong format, invalid key, or request header does not use Bearer. | Check `Authorization: Bearer ${API_KEY}` and confirm the key has not expired or been deleted. |
| 403 | No permission | The key is valid, but the group cannot access the model, channel, or endpoint. | Query `/v1/models` and ask an admin to check group, pricing, and model permissions. |
| 404 | Not found | Missing path, disabled endpoint, wrong model name, or missing mapping. | Check the API family and path, and make sure the model name comes from the current deployment. |
| 429 | Too many requests | Hit a {{SITE_NAME}}, upstream account, group, or client-side rate limit. | Lower concurrency, add retry backoff, or ask an admin to adjust limits and quotas. |
| 5xx | Service error | Upstream failure, network issue, channel misconfiguration, or internal service error. | Retry later, record the error and request time, and switch models or contact an admin if needed. |

## 401 Unauthenticated

First confirm that the API Key is actually sent:

```bash
echo "${API_KEY}" | wc -c
curl -i "{{BASE_URL}}v1/models" \
  -H "Authorization: Bearer ${API_KEY}"
```

Common causes:

| Cause | Action |
| --- | --- |
| Environment variable is unset | Run `export API_KEY="..."` again. |
| Header misses Bearer | Use `Authorization: Bearer ${API_KEY}`. |
| Key was deleted or disabled | Check the dashboard or ask an admin. |
| Wrong environment key | Separate test, staging, and production API Keys. |

401 usually should not be retried automatically. Fix authentication first.

## 403 No Permission

403 means the key may be valid, but the current group lacks access. Check:

1. Whether `/v1/models` returns the target model.
2. Whether the current user or API Key is bound to the right group.
3. Whether the group enables the target endpoint and channel.
4. Whether quota, balance, or expiration allows the request.
5. Whether an admin recently changed permissions or pricing.

403 is not the same as a wrong Base URL, and it is not always a typo in the key. Focus on permissions and group policy.

## 404 Not Found

404 often means path, endpoint, or model name mismatch:

| Check | Example |
| --- | --- |
| Duplicated `/v1` | `{{BASE_URL}}v1/v1/chat/completions` is wrong. |
| Missing `/v1beta` | Gemini native requests need `/v1beta/models/...`. |
| Missing `/antigravity` | Antigravity clients need the corresponding path. |
| Model name exists | Use results from `/v1/models` or `/v1beta/models`. |
| Endpoint is enabled | Some deployments may not expose images, embeddings, or Responses. |

## Model Unavailable

Common messages include model not found, model disabled, no available channel, or upstream rejected the request. Possible causes:

| Cause | Description |
| --- | --- |
| Group does not expose the model | The current API Key's group does not have permission for this model. |
| Missing model mapping | The requested model name does not map to an available upstream model. |
| Channel unavailable | The upstream account has insufficient quota, an invalid key, rate limiting, or is disabled. |
| Pricing mismatch | The admin has not configured pricing or multipliers for the model, so the request is rejected. |
| Wrong API family | An Anthropic model is called with an OpenAI request body, or a Gemini model is sent to the wrong endpoint. |

Recommended action: call `{{BASE_URL}}v1/models` first, retry with a model name from the returned list, then ask an admin to inspect groups, channels, and model mappings if it still fails.

## Unsupported Endpoint

If the response is 404 or explicitly says the endpoint is unsupported, the deployment usually has not enabled that path or the client built the Base URL incorrectly.

| Check | Example |
| --- | --- |
| OpenAI SDK | `baseURL` is usually `{{BASE_URL}}v1`. |
| Anthropic compatible | Usually requests `{{BASE_URL}}v1/messages`; avoid duplicated `/v1/v1/messages`. |
| Gemini native | Use `{{BASE_URL}}v1beta/models/{model}:generateContent`. |
| Antigravity | Use `{{BASE_URL}}antigravity/...` and confirm that the admin enabled it. |

## Missing Group or Unbound Key

If an API Key is not associated with a valid group, you may see empty model lists, 403 responses, billing failures, or rejected requests. Ask an admin to confirm:

1. The API Key exists and is enabled.
2. The user is assigned to a valid group.
3. The group has models, channels, and pricing configured.
4. The group quota, balance, or expiration time allows the request.

## 429 Rate Limited

429 means requests are too fast or quota is exceeded. Recommended actions:

| Scenario | Recommendation |
| --- | --- |
| One user sends high-frequency requests | Lower concurrency and add client-side throttling. |
| Batch job | Split batches and limit batch size and interval. |
| Too many automatic retries | Use exponential backoff and cap maximum retries. |
| Upstream account rate limit | Ask an admin to check channel status or switch fallback channels. |
| Group quota is insufficient | Check usage or ask an admin to adjust quota. |

Do not retry 429 immediately in an infinite loop. That amplifies rate limiting and cost.

## Streaming Interruptions

Streaming requests may disconnect mid-generation, appearing as a closed SSE connection, client timeout, or incomplete output.

| Possible cause | Recommendation |
| --- | --- |
| Client timeout too short | Increase HTTP client, proxy, and load balancer timeouts. |
| Proxy buffering | Disable reverse proxy buffering so SSE chunks are forwarded promptly. |
| Upstream rate limit or error | Lower concurrency and check upstream account status and error logs. |
| Unstable network | Add retry logic, but avoid blindly replaying non-idempotent requests. |
| Output too long | Control `max_tokens` or split the task. |

If non-streaming requests work for the same model but streaming keeps failing, check proxy behavior, SSE support, and client parsing first.

## Minimal Reproduction Template

When reporting a problem to an admin or support contact, provide the following without sharing the full API Key:

```text
Time range:
Request path:
Model name:
Status code:
Error summary:
API Key identifier: name or last characters only
Client type: curl / OpenAI SDK / Claude Code / Gemini / other
Streaming:
Local or production environment:
```

Minimal reproducible curl:

```bash
curl -i "{{BASE_URL}}v1/chat/completions" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "ping" }
    ],
    "max_tokens": 20
  }'
```

If the minimal request succeeds but the business request fails, the issue is more likely business parameters, context length, streaming handling, tool calls, or client timeout.
