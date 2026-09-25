# Billing and Usage

{{SITE_NAME}} billing and usage views depend on the current deployment's groups, channel pricing, multipliers, quotas, and admin policies. Different deployments may use different rules, so this page explains the general concepts users should understand while integrating.

## Billing Overview

{{SITE_NAME}} charges by actual usage, and each model uses one billing type. Every card in the [Model Plaza](/plaza) is labeled with its billing type, and displayed prices already include your group multiplier.

| Billing type | How it is priced | Unit |
| --- | --- | --- |
| Text | Input, output, cache write, and cache read are priced separately | per 1M tokens |
| Image | Image tokens first; per image by 1K / 2K / 4K size when no token usage is returned | per 1M tokens / per image |
| Video | Per-second price of the resolution tier (480p / 720p / 1080p) × duration | per second |
| Per request | Fixed charge per successful request, regardless of tokens | per request |

## Text Models

```text
cost = (input tokens × input price + output tokens × output price
        + cache write tokens × cache write price + cache read tokens × cache read price)
       ÷ 1,000,000 × group multiplier
```

- Input includes system prompts, conversation history, user input, and tool definitions. Output includes generated text, tool call arguments, and structured output.
- Some models are tiered by context length (for example, a long-context price above a token threshold). The model detail's tiered pricing lists every tier.
- Models such as DeepSeek have official time-of-day pricing with different peak and off-peak prices, explained in the model detail.
- Streaming only changes how responses are delivered. Content already generated is still counted.

## Image Models

An image model may have both kinds of prices. They are applied in this order:

1. **Image tokens (preferred)**: when the upstream returns image token usage, text input, image input, and image output tokens are billed at their token prices.
2. **Per image (fallback)**: when no image token usage is returned, cost = per-image price of the size tier × number of images.

The size tier is decided by the long edge of the image. The actual output size returned by the upstream takes priority over the `size` request parameter:

| Size tier | Rule | Examples |
| --- | --- | --- |
| 1K | long edge ≤ 1024px | `1024x1024` |
| 2K | 1024px < long edge ≤ 2048px | `1536x1024`, `2048x2048` |
| 4K | 2048px < long edge ≤ 4096px | `3840x2160`, `4096x4096` |

When no size is given or `size` is `auto`, the 2K price is used. Sizes with a long edge above 4096px have no price and the request is rejected.

## Video Models

```text
cost = per-second price of the resolution tier × duration (seconds) × count × group multiplier
```

For example, if a video model costs $0.10 per second at 720p, a 6-second 720p video costs $0.60 before your group multiplier is applied.

Video generation is asynchronous and settles as follows:

| Task result | Charge |
| --- | --- |
| Submitted | The estimated amount is held from your balance |
| Completed | Settled by actual duration and resolution; if the upstream returns no usage, the held amount is charged |
| Failed, cancelled, or expired | The full hold is released and nothing is charged |

## Per-Request Models

Some models charge a fixed price per successful request, regardless of input or output length. Their cards show the price per call.

## Multipliers and VIP

| Item | Description |
| --- | --- |
| Group multiplier | Groups can have different multipliers. Plaza prices already include it. |
| Peak multiplier | Some groups apply a higher multiplier to token billing during peak hours; cards show a peak badge. |
| Image multiplier | Some groups use a separate multiplier for per-image pricing, shown in the model's available groups. |
| Video multiplier | Some groups use a separate multiplier for per-second video pricing, also shown in the available groups. |
| VIP price | VIP group prices are shown separately from regular groups. |

## Other Factors

| Factor | Impact |
| --- | --- |
| Channel | The same model may be routed through different upstream channels with different prices and availability. |
| Model mapping | The requested model name may be mapped to another upstream model. |
| Request parameters | `max_tokens`, image size, video duration and resolution, batch input, and tool calls affect usage. |
| Retries | Automatic client retries create multiple requests and multiple charges. |

For reconciliation, follow the current deployment dashboard or the billing rules provided by an admin.

## Estimating Cost Before Requests

When exact estimation is unavailable, reduce uncertainty with these steps:

1. Choose a model that matches the task instead of defaulting to the largest model.
2. Keep only necessary history in the context.
3. Set a reasonable `max_tokens` or output limit.
4. Summarize or split long documents before sending them.
5. Test with low traffic and observe actual usage.

If the Anthropic-compatible token counting endpoint is enabled, you can try:

```bash
curl "{{BASE_URL}}v1/messages/count_tokens" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "messages": [
      { "role": "user", "content": "Estimate the tokens for this request." }
    ]
  }'
```

Availability depends on the upstream provider and admin configuration. For OpenAI, Gemini, or other API families, follow the capabilities exposed by the current deployment.

## Cost Control Practices

| Practice | Notes |
| --- | --- |
| Confirm models with `/v1/models` | Avoid requesting the wrong model or an unauthorized model. |
| Choose the right model tier | Use lower-cost models for simple classification, rewriting, or extraction tasks. |
| Limit output length | Set `max_tokens` to avoid unbounded generation. |
| Reduce repeated context | Do not resend full history or irrelevant materials on every request. |
| Retry carefully | Use exponential backoff for 429 and 5xx, and limit maximum retries. |
| Monitor spikes | Sudden request increases may indicate loops, batch jobs, or key leakage. |

## Viewing Usage

If the deployment provides a usage page in the dashboard, you may see request volume, model, time range, cost, or balance information. Fields vary by deployment, but common ones include:

| Field | Meaning |
| --- | --- |
| Time | Request time or reporting window. |
| Model | Requested model name or mapped model name. |
| Input/output | Input and output tokens or other measurement units. |
| Status | Success, failure, upstream error, or rate limited. |
| Cost | Consumption calculated from group and channel pricing. |

If there is no usage entry in the dashboard, ask an admin how usage should be viewed.

## Reconciliation and Anomaly Checks

When usage looks abnormal, collect the following without sending the full API Key:

1. Request time range.
2. API Key name or last characters.
3. Request path and model name.
4. Client or service name.
5. Error codes, retry count, and concurrency.
6. Recent changes to model, prompt, batch jobs, or scheduled tasks.

Common causes include excessive automatic retries, runaway loops, long-context requests, high-concurrency test scripts, key leakage, or model mapping changes.

## Budget Recommendations

Before production integration, configure:

| Item | Recommendation |
| --- | --- |
| Environment isolation | Use different API Keys for test, staging, and production. |
| Quota limits | Set quota or rate limits for high-risk projects. |
| Alert thresholds | Notify owners when request volume, cost, or failure rate is abnormal. |
| Model allowlist | Expose only the models the business needs. |
| Release observation | Watch request volume and error rate for 30 to 60 minutes after deployment. |
