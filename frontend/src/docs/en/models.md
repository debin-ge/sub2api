# Models and Platforms

Sub2API can connect to multiple model platforms through different channels. Frontends and clients should not assume a deployment supports every platform, model, or endpoint. The available models are defined by the current API Key's response from `/v1/models`, `/v1beta/models`, or by admin guidance.

Model availability is usually determined by these factors:

| Factor | Impact |
| --- | --- |
| API Key group | Controls which channels, models, and pricing policies a user can access. |
| Upstream account | Determines whether an upstream account is valid, funded, and unrestricted. |
| Model mapping | Admins may map the requested model name to a different upstream model. |
| Channel pricing | Admins may expose models based on price, multiplier, or cost strategy. |
| Admin settings | Routes, priorities, disabled items, concurrency, and rate limits all affect availability. |

## Platform Matrix

| Platform | Common API family | Description |
| --- | --- | --- |
| `openai` | `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, image APIs | Suitable for the OpenAI SDK and compatible ecosystem. Specific models depend on channels and mappings. |
| `anthropic` | `/v1/messages`, `/v1/messages/count_tokens` | Suitable for Claude Code, the Anthropic SDK, or Messages-compatible clients. |
| `gemini` | `/v1beta/models/*` | Suitable for Gemini native request bodies and SSE streaming generation. |
| `antigravity` | `/antigravity/*` | Antigravity compatible paths that may cover Claude-style and Gemini-style APIs. |
| `minimax` | Usually accessed through compatibility layers or model mappings | Availability depends on the corresponding channel configuration. |
| `glm` | Usually accessed through compatibility layers or model mappings | Useful when managing Zhipu GLM models through one gateway. |
| `kimi` | Usually accessed through compatibility layers or model mappings | Model names and context capability depend on deployment settings. |
| `deepseek` | Usually accessed through OpenAI compatible paths or mappings | Commonly used with Chat Completions compatible clients. |
| `windsurf` | Usually for specific clients or compatible entry points | Requires the admin to configure the corresponding channel, model, and authentication. |
| `opencode` | Usually through `/v1/responses` or OpenAI compatible paths | Suitable for coding clients that support the Responses API or OpenAI-style configuration. |

## Confirming Model Availability

Start with:

```bash
curl "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $SUB2API_KEY"
```

For Gemini native clients, you can also check:

```bash
curl "$BASE_URL/v1beta/models" \
  -H "Authorization: Bearer $SUB2API_KEY"
```

Do not treat example model names in the docs as guaranteed availability. Examples only explain request formats. Production clients should use model names returned by the API or mapping names explicitly provided by an admin.

## When a Model Is Unavailable

| Symptom | Possible cause |
| --- | --- |
| `/v1/models` does not return the expected model | The current API Key group does not expose it, the model is disabled, the channel is unavailable, or no mapping exists. |
| Request returns 404 | The path is not enabled, the model name does not exist, or the compatible endpoint does not match. |
| Request returns 403 | The key is valid, but the current group lacks access. |
| Request returns an upstream error | Upstream quota, risk control, regional restrictions, model deprecation, or unsupported parameters. |
| Streaming request disconnects | Network, proxy, upstream rate limit, client timeout, or SSE parsing issue. |

If your business needs a fixed model, ask an admin to confirm the model mapping, channel priority, pricing, and fallback channels instead of relying only on a hard-coded client-side model name.
