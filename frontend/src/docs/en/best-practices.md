# Best Practices

This page is for users integrating {{SITE_NAME}} into business systems. The goal is stable requests, easier troubleshooting, controlled cost, and fewer key or configuration risks.

## Pre-Integration Checklist

| Check | Recommendation |
| --- | --- |
| Base URL | Use `{{BASE_URL}}` or the full address provided by an admin. |
| API Key | Use the `${API_KEY}` environment variable and do not write it into source code. |
| Model list | Call `/v1/models` first to confirm models available to the current key. |
| Endpoint format | Choose OpenAI, Anthropic, Gemini, or Antigravity format based on the client. |
| Minimal request | Run one non-streaming minimal request before adding business logic. |

## Configuration Management

Keep configuration in environment variables or a secure configuration system:

```bash
export API_KEY="replace-with-your-api-key"
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="${API_KEY}"
```

Use different API Keys for test, staging, and production. Log environment name, model name, and request path to simplify debugging.

## Model Selection

Do not choose only by model name. Consider task type, cost, context length, latency, and stability:

| Task | Recommendation |
| --- | --- |
| Classification, tagging, simple rewriting | Prefer lower-cost, low-latency models. |
| Long summarization or complex reasoning | Choose models with stronger reasoning and longer context. |
| Code generation or repair | Prefer models and clients that work well with Responses or coding workflows. |
| High concurrency | Choose stable channels and prepare fallback models. |
| Strict structured output | Use lower temperature, clear schemas, or post-processing validation. |

Before launch, ask an admin to confirm model mappings and fallback channels so upstream changes do not break production.

## Error Handling and Retries

| Status | Recommendation |
| --- | --- |
| 401 | Do not retry. Check API Key and headers. |
| 403 | Do not retry blindly. Check group permission, quota, and model access. |
| 404 | Check path, Base URL joining, and model name. |
| 429 | Use exponential backoff, lower concurrency, and cap maximum retries. |
| 5xx | Retry after a short backoff, and log request time, model, and error details. |

For workflows with side effects, do not replay requests unconditionally. When retrying, make the business operation idempotent.

## Streaming

Streaming is useful for long text, chat UIs, and fast first-token feedback. Before using it, confirm:

1. The client supports SSE or streaming reads.
2. Reverse proxies do not buffer responses.
3. Timeouts are long enough for large outputs.
4. The client closes connections correctly when the user cancels.
5. There is a non-streaming or retry fallback.

Streaming failures are not always model issues. Proxy behavior and client parsing are more common causes.

## Cost Control

| Practice | Notes |
| --- | --- |
| Limit `max_tokens` | Control output length and avoid runaway generation. |
| Trim context | Send only necessary history and materials. |
| Tier models | Route simple tasks to lower-cost models and upgrade only when needed. |
| Control concurrency | Avoid batch jobs or scheduled tasks exhausting quota instantly. |
| Monitor retries | Repeated failed requests can amplify cost. |

After releasing a new feature, watch usage curves and failure rates to catch loops or excessive retries.

## Logging and Troubleshooting

Recommended log fields:

1. Request time and duration.
2. Request path and model name.
3. Status code and error summary.
4. Client version or service name.
5. Request ID, business user ID, or task ID.

Do not log full API Keys, private user data, or long sensitive prompts. When asking an admin for help, provide time, model, path, status code, and a redacted request summary.

## Production Launch Checklist

Before launch, confirm:

- API Keys are isolated by environment and have a rotation plan.
- `/v1/models` returns the expected models.
- Critical endpoints pass minimal request tests.
- 429 and 5xx use backoff retries.
- 401, 403, and 404 produce clear user-facing errors.
- Logs are redacted and can correlate requests with business tasks.
- Cost, quota, or failure rate has a monitoring path.
- A fallback model or manual handling path exists.
