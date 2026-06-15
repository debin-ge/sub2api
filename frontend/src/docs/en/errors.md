# Troubleshooting

Sub2API errors may come from the gateway itself or from upstream platforms. When troubleshooting, first check the Base URL, API Key, endpoint path, model name, and current group permissions.

## HTTP Status Codes

| Status | Meaning | Common causes | Recommended action |
| --- | --- | --- | --- |
| 401 | Unauthenticated | Missing API Key, wrong format, invalid key, or request header does not use Bearer. | Check `Authorization: Bearer $SUB2API_KEY` and confirm the key has not expired or been deleted. |
| 403 | No permission | The key is valid, but the group cannot access the model, channel, or endpoint. | Query `/v1/models` and ask an admin to check group, pricing, and model permissions. |
| 404 | Not found | Missing path, disabled endpoint, wrong model name, or missing mapping. | Check the API family and path, and make sure the model name comes from the current deployment. |
| 429 | Too many requests | Hit a Sub2API, upstream account, group, or client-side rate limit. | Lower concurrency, add retry backoff, or ask an admin to adjust limits and quotas. |
| 5xx | Service error | Upstream failure, network issue, channel misconfiguration, or internal service error. | Retry later, record the error and request time, and switch models or contact an admin if needed. |

## Model Unavailable

Common messages include model not found, model disabled, no available channel, or upstream rejected the request. Possible causes:

| Cause | Description |
| --- | --- |
| Group does not expose the model | The current API Key's group does not have permission for this model. |
| Missing model mapping | The requested model name does not map to an available upstream model. |
| Channel unavailable | The upstream account has insufficient quota, an invalid key, rate limiting, or is disabled. |
| Pricing mismatch | The admin has not configured pricing or multipliers for the model, so the request is rejected. |
| Wrong API family | An Anthropic model is called with an OpenAI request body, or a Gemini model is sent to the wrong endpoint. |

Recommended action: call `$BASE_URL/v1/models` first, retry with a model name from the returned list, then ask an admin to inspect groups, channels, and model mappings if it still fails.

## Unsupported Endpoint

If the response is 404 or explicitly says the endpoint is unsupported, the deployment usually has not enabled that path or the client built the Base URL incorrectly.

| Check | Example |
| --- | --- |
| OpenAI SDK | `baseURL` is usually `https://your-sub2api.example.com/v1`. |
| Anthropic compatible | Usually requests `$BASE_URL/v1/messages`; avoid duplicated `/v1/v1/messages`. |
| Gemini native | Use `$BASE_URL/v1beta/models/{model}:generateContent`. |
| Antigravity | Use `$BASE_URL/antigravity/...` and confirm that the admin enabled it. |

## Missing Group or Unbound Key

If an API Key is not associated with a valid group, you may see empty model lists, 403 responses, billing failures, or rejected requests. Ask an admin to confirm:

1. The API Key exists and is enabled.
2. The user is assigned to a valid group.
3. The group has models, channels, and pricing configured.
4. The group quota, balance, or expiration time allows the request.

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
