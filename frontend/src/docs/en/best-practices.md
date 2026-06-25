# Best Practices

This guide is for developers integrating {{SITE_NAME}} into production and business systems. The goal is to achieve high availability, fast troubleshooting, controlled costs, and secure credential handling.

---

## Pre-Integration Checklist

| Check | Best Practice Recommendation |
| :--- | :--- |
| **Base URL** | Configure to `{{BASE_URL}}`, appending `/v1` depending on the client library. |
| **API Key** | Must be passed via environment variables; never hardcode it as a literal string in your code. |
| **Model Whitelist** | Call `/v1/models` first to verify that the key actually has permission for the model. |
| **Endpoint Match** | Choose the compatible API family (OpenAI/Claude/Gemini/Antigravity) that matches your client. |
| **Minimal Check** | Ensure one non-streaming request succeeds before writing any complex business logic. |

---

## Configuration & Environment Management

Keep configuration in environment variables or a secure configuration system:

```bash
export BASE_URL="{{BASE_URL}}"
export YOUR_KEY="replace-with-your-api-key"
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="$YOUR_KEY"
```

> [!TIP]
> **Environment Isolation**: Always use different API Keys for test, staging, and production environments. Log the environment name, the requested model, and the request path to dramatically simplify debugging in production.

---

## Model Selection Strategy

Do not choose only by model name. Consider task type, cost, context length, latency, and stability:

| Workload Scenario | Model Selection Recommendation |
| :--- | :--- |
| **Classification, tagging, simple text rewriting** | Prefer lower-cost, low-latency models. |
| **Long summarization or complex reasoning** | Choose models with stronger reasoning and longer context. |
| **Code generation or repair (IDE)** | Prefer models that work well with Responses API or coding workflows. |
| **High concurrency workloads** | Ensure the backend has stable fallback channels, and prepare fallback models. |
| **Strict structured output** | Use lower temperatures (`temperature <= 0.2`), and enforce post-processing validation. |

---

## Error Handling and Retries

Implement differential handling logic based on HTTP status codes returned by the gateway:

| HTTP Status Code | Client Retry Strategy | Notes & Action |
| :--- | :--- | :--- |
| **401 Unauthorized** | 🚫 **Never retry** | Check if the key variable is configured and if the Bearer prefix is present. |
| **403 Forbidden** | 🚫 **Do not retry blindly** | The key is valid, but the associated group lacks permissions for the model. |
| **404 Not Found** | 🚫 **Never retry** | Confirm the path is correct and the model name contains no typos. |
| **429 Too Many Requests** | ✅ **Exponential backoff** | Lower concurrency, add jitter (random delays), and cap maximum retries. |
| **5xx Server Error** | ✅ **Retry after short delay** | Retry 2-3 times with exponential backoff and log the error details. |

> [!IMPORTANT]
> **Retry Idempotency**: For workflows that perform write operations or have billing side-effects, do not replay requests unconditionally. Ensure idempotency at the business layer by introducing unique request identifiers.

---

## Streaming (SSE) Specifications

Streaming responses provide a great first-token experience. Before using it, confirm:
1. The client supports SSE and parses the `data: [DONE]` marker properly.
2. Reverse proxies (such as Nginx, Cloudflare) **do not buffer responses** and support chunked streaming.
3. Timeouts (e.g. read timeouts) are configured long enough to cover large outputs.
4. When a user cancels a request, the client closes the connection properly to prevent backend billing waste.

> [!NOTE]
> Streaming failures are not always model issues. Proxy timeouts and client-side parsing logic are more common causes of broken connections.

---

## Cost Governance

* **Limit `max_tokens`**: Always specify output length limits to prevent runaway token consumption due to dead loops or prompt injection.
* **Trim Context**: Discard redundant chat history and send only necessary context to the model.
* **Tiered Models**: Route simple tasks to lower-cost models first, and upgrade only when complex reasoning is required.
* **Smooth Concurrency**: Avoid scheduled jobs or Cron tasks hitting the gateway at the same moment to prevent instant quota exhaustion.

> [!WARNING]
> After releasing a new feature, monitor usage curves and failure rates closely. Protect your account against loops or excessive retries.

---

## Production Launch Checklist

- [ ] **Environment Isolation**: API Keys are isolated by environment and have a rotation plan.
- [ ] **Model Whitelist**: `/v1/models` returns the expected models in the production environment.
- [ ] **Endpoint Validation**: Critical endpoints pass minimal non-streaming and streaming tests.
- [ ] **Retry Jitter**: 429 and 5xx errors use exponential backoff retries with random jitter.
- [ ] **Error Friendliness**: 401, 403, and 404 produce clear user-facing errors rather than stack traces.
- [ ] **Logs Redacted**: Logs are redacted (no raw keys) and can correlate requests with business tasks.
- [ ] **Fallback Plan**: A fallback model or manual handling path exists for critical transactions.
