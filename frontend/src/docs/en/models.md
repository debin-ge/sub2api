# Models and Platforms

{{SITE_NAME}} can connect to multiple model platforms through different channels. Frontends and clients should not assume a deployment supports every platform, model, or endpoint.

> [!IMPORTANT]
> **Regarding Available Models**: The actually available model list should always be determined by the current API Key's response from `/v1/models`, `/v1beta/models`, or by admin guidance.

---

## Model Availability Factors

| Factor | Description & Impact |
| :--- | :--- |
| **API Key group** | Controls which channels, models, and pricing policies a user can access. |
| **Upstream account** | Determines whether an upstream account is valid, funded, and unrestricted. |
| **Model mapping** | Admins may map the requested model name to a different upstream model. |
| **Channel pricing** | Admins may expose models based on price, multiplier, or cost strategy. |
| **Runtime settings** | Routes, priorities, disabled items, concurrency, and rate limits all affect availability. |

---

## Platform Matrix

| Platform | Common API Family | Description |
| :--- | :--- | :--- |
| **`openai`** | `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, image APIs | Suitable for the OpenAI SDK and compatible ecosystem. Specific models depend on channels and mappings. |
| **`anthropic`** | `/v1/messages`, `/v1/messages/count_tokens` | Suitable for Claude Code, the Anthropic SDK, or Messages-compatible clients. |
| **`gemini`** | `/v1beta/models/*` | Suitable for Gemini native request bodies and SSE streaming generation. |
| **`antigravity`** | `/antigravity/*` | Antigravity compatible paths that may cover Claude-style and Gemini-style APIs. |
| **`minimax`** | Compatibility layer / mapping | Availability depends on the corresponding channel configuration. |
| **`glm`** | Compatibility layer / mapping | Useful when managing Zhipu GLM models through one gateway. |
| **`kimi`** | Compatibility layer / mapping | Model names and context capability depend on deployment settings. |
| **`deepseek`** | OpenAI compatible layer / mapping | Commonly used with Chat Completions compatible clients. |
| **`windsurf`** | Client-specific entry points | Requires the admin to configure the corresponding channel, model, and authentication. |
| **`opencode`** | `/v1/responses` / compatible paths | Suitable for coding clients that support the Responses API or OpenAI-style configuration. |

---

## Confirming Model Availability

Start with a check in your terminal:

```bash
# Query OpenAI/Claude style models
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

For Gemini native SDKs, query:

```bash
# Query Gemini style models
curl "${BASE_URL}v1beta/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

> [!WARNING]
> **Notice**: **Do not treat example model names in the docs as guaranteed availability.** Examples only explain request formats. Production clients should use model names returned by the API or mapping names explicitly provided by an admin.

---

## Model Selection Method

Choose models by task goal, not popularity:

| Task Type | Recommendation | Watch |
| :--- | :--- | :--- |
| **Classification, tagging, simple rewriting** | Prefer lower-cost, low-latency models. | Cost, stability, throughput. |
| **Support chat and general conversation** | Choose stable chat models with enough context. | First-token latency, answer quality, concurrency. |
| **Long summarization or knowledge base Q&A** | Choose models with longer context and good retrieval behavior. | Context length, citation accuracy, cost. |
| **Complex reasoning, planning, analysis** | Choose stronger reasoning models and cap output length. | Task success rate, output consistency. |
| **Code generation or review** | Prefer models that work well with coding workflows, Responses, or tool calls. | Code correctness, context window, tool support. |
| **Embeddings and retrieval** | Use dedicated embedding models instead of chat models. | Vector dimension, retrieval quality, cost. |
| **Image generation or editing** | Use image models and image endpoints exposed by the deployment. | Size, quality, response time. |

---

## Matching Models and Endpoints

> [!IMPORTANT]
> **Endpoint and Format Match**: Model availability does not mean every endpoint is available. Ensure that your payload structure matches the visited endpoint.

| Model/Client Type | Common Endpoint | Notes |
| :--- | :--- | :--- |
| **OpenAI Chat compatible model** | `/v1/chat/completions` | Request body uses `messages` array. |
| **Responses compatible model** | `/v1/responses` | Good for clients that support the OpenAI Responses API. |
| **Claude or Anthropic compatible model** | `/v1/messages` | Usually requires the `max_tokens` parameter. |
| **Gemini native model** | `/v1beta/models/{model}:generateContent` | Request body uses `contents` and `parts`. |
| **Embedding model** | `/v1/embeddings` | Input is text or an array of text. |

---

## Pre-Launch Validation

Before using a model in production:
1. Call `/v1/models` with the production API Key.
2. Send a minimal non-streaming request using the same Base URL as production.
3. If the business uses streaming, validate a streaming request too.
4. Test output quality, latency, and failure rate with real prompt samples.
5. Confirm pricing, quota, model mapping, and fallback channels with an admin.
6. Keep a quick rollback path for model name and endpoint configuration.

---

## Troubleshooting Model Issues

| Symptom | Possible Cause |
| :--- | :--- |
| **`/v1/models` does not return the expected model** | The current API Key group does not expose it, the model is disabled, the channel is unavailable, or no mapping exists. |
| **Request returns 404** | The path is not enabled, the model name does not exist, or the compatible endpoint does not match. |
| **Request returns 403** | The key is valid, but the current group lacks access. |
| **Request returns an upstream error** | Upstream quota, risk control, regional restrictions, model deprecation, or unsupported parameters. |
| **Streaming request disconnects** | Network, proxy, upstream rate limit, client timeout, or SSE parsing issue. |

---

## Fallback Model Strategy

High-availability workloads should prepare fallback models, but avoid automatic fallback to models with very different behavior.

| Strategy | Best For | Action |
| :--- | :--- | :--- |
| **Same-capability fallback** | Alternative models in the same API family for most production requests. | Swap model name parameter on 429/5xx and retry. |
| **Degraded model** | Return a simpler result with a lower-cost model when a stronger model is unavailable. | Accept lower reasoning depth to keep the service alive. |
| **Queue retry** | Non-real-time tasks. | Retrying with backoff is safer than immediate model switching. |
