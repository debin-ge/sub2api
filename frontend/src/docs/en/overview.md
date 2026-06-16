# Product Overview

{{SITE_NAME}} is a unified API gateway for multi-model services. It brings model routing, authentication, quotas, groups, and billing configuration from different upstream platforms into one entry point, so applications can integrate through a stable Base URL and API Key.

The features available in a specific deployment depend on its admin configuration, including the user's group, upstream account status, model mappings, channel pricing, quota policy, and enabled endpoints. Do not assume every {{SITE_NAME}} deployment supports every model or API.

## Use Cases

| Scenario | Description |
| --- | --- |
| Unified multi-model access | Applications keep one Base URL and access different upstreams through model names or mappings. |
| Team key management | Assign separate API Keys to users, projects, or groups, then control available models and quotas. |
| Existing client compatibility | Reuse common integration patterns for OpenAI, Anthropic, Gemini, Codex, Claude Code, and compatible clients. |
| Usage and cost governance | Manage request cost through channel pricing, group policies, and dashboard statistics. |
| Incremental migration | Switch upstream accounts, model mappings, or channel priorities without large application code changes. |

## Core Concepts

| Concept | Description |
| --- | --- |
| Base URL | The access address of the current {{SITE_NAME}} deployment, for example `{{BASE_URL}}`. |
| API Key | The access key issued by {{SITE_NAME}}, usually sent as a `Bearer` token. |
| Group | A configuration unit admins use to control accessible models, channels, prices, and quotas. |
| Channel | A {{SITE_NAME}} connection to an upstream platform or account. Different channels may support different models and APIs. |
| Model mapping | Admins can map the model name used in a request to a different upstream model. |
| Endpoint compatibility layer | {{SITE_NAME}} provides OpenAI, Anthropic, Gemini, Antigravity, and similar API-style entry points. |

## Basic Request Flow

1. Get the Base URL and API Key from an admin or from the console.
2. Set environment variables:

```bash
export BASE_URL="{{BASE_URL}}"
export YOUR_KEY="your-api-key"
```

3. Call `/v1/models` to check the models actually available to the current key.
4. Choose the request format required by your client or API, such as `/v1/chat/completions`, `/v1/messages`, `/v1/responses`, or Gemini native endpoints.
5. Adjust the model, group, or upstream configuration based on responses, errors, and usage data.

## Recommended Integration Order

| Step | Recommendation |
| --- | --- |
| Verify the key | Call `${BASE_URL}v1/models` first to confirm authentication and model visibility. |
| Choose an API | OpenAI SDK clients usually use `/v1/chat/completions` or `/v1/responses`; Claude-style clients use `/v1/messages`; Gemini native clients use `/v1beta` paths. |
| Pin model names | Use model names returned by `/v1/models`, or mapping names explicitly provided by an admin. |
| Enable streaming | Use `stream: true` or SSE endpoints only after confirming your client supports streaming responses. |
| Troubleshoot failures | For 401, 403, 404, 429, or upstream 5xx responses, first check whether the key, group, endpoint, and model match the current deployment. |

## Documentation Path

| Goal | Read |
| --- | --- |
| Make your first API call | Start with Quick Start, then API Keys and Accounts. |
| Configure a concrete client | Read Client Integration and Copy-Ready Configuration Snippets. |
| Choose the right API endpoint | Read Endpoint Selection Guide and API Reference. |
| Choose the right model | Read the selection method in Models and Platforms. |
| Understand balance and cost | Read Billing and Usage. |
| Fix request failures or unexpected behavior | Read Troubleshooting and FAQ. |
| Prepare for production | Read the launch checklist in Best Practices. |
