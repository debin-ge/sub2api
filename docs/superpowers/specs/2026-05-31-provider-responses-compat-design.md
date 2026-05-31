# Provider Responses Compatibility Design

## Goal

Add HTTP `POST /v1/responses` compatibility for MiniMax, GLM, Kimi, DeepSeek, and Windsurf provider groups, while preserving OpenCode's existing `/v1/responses` passthrough behavior.

The approved scope is "1 + 2": basic Responses compatibility plus Codex CLI common request compatibility. This means text and streaming Responses requests should work through the existing provider gateways, including common Codex request shapes such as `input`, `instructions`, function tools, and `reasoning.effort`. Unsupported Responses features must fail explicitly instead of being silently dropped.

## Current State

The route layer currently treats `POST /v1/responses` as supported only for OpenAI, generic Anthropic-compatible groups, and OpenCode. MiniMax, GLM, Kimi, DeepSeek, and Windsurf groups are routed to platform-specific unsupported handlers and return `404 not_found_error`.

The provider services already support stable upstream paths:

- MiniMax, GLM, and Kimi have Anthropic-compatible `/v1/messages` forwarding.
- DeepSeek and Windsurf have OpenAI-compatible `/v1/chat/completions` forwarding and Anthropic-compatible `/v1/messages` adapters.
- OpenCode already forwards `/v1/responses` directly to its upstream `/v1/responses` endpoint.
- `internal/pkg/apicompat` already contains conversion helpers between Responses, Anthropic Messages, and Chat Completions formats.

## Compatibility Contract

Supported for MiniMax, GLM, Kimi, DeepSeek, and Windsurf:

- `POST /v1/responses`
- `POST /responses`
- Non-streaming text responses
- Streaming SSE responses
- Responses `input` as a string or message array
- `instructions`
- Function tools that can be represented by the target upstream format
- `tool_choice` when convertible by existing compatibility helpers
- `reasoning.effort` extraction for usage records and best-effort upstream propagation where the provider path supports it
- Provider model alias and channel model mapping behavior consistent with existing `/v1/messages` and `/v1/chat/completions` paths

Explicitly unsupported for these providers:

- `GET /v1/responses` WebSocket ingress
- `POST /v1/responses/compact` and `/responses/compact`
- `/backend-api/codex/responses/compact`
- Image generation and image edit intents
- `previous_response_id` continuation over HTTP
- Built-in OpenAI tools that cannot be converted to Anthropic or Chat Completions safely, including image generation and remote MCP-style tools
- Response subresources other than the root create endpoint

Unsupported cases should return `400 invalid_request_error` when the request body asks for an unsupported Responses feature, and `404 not_found_error` when the route itself remains unsupported, such as `GET /v1/responses` for WebSocket.

## Architecture

Introduce provider-specific `Responses(c)` handlers for MiniMax, GLM, Kimi, DeepSeek, and Windsurf. Each handler should mirror the provider's existing auth, group validation, body reading, concurrency, billing, account selection, failover, and usage recording behavior from its established `Messages` or `ChatCompletions` handler. The new handler should differ only in request parsing, feature validation, and the service method it calls.

Add `ForwardResponses` to each provider service interface. The method should convert the incoming Responses request into the most stable existing provider path:

- MiniMax: `Responses -> Anthropic Messages -> MiniMax Anthropic-compatible upstream -> Responses`
- GLM: `Responses -> Anthropic Messages -> GLM Anthropic-compatible upstream -> Responses`
- Kimi: `Responses -> Anthropic Messages -> Kimi Anthropic-compatible upstream -> Responses`
- DeepSeek: `Responses -> Chat Completions -> DeepSeek chat upstream -> Responses`
- Windsurf: `Responses -> Chat Completions -> Windsurf chat upstream -> Responses`

OpenCode remains unchanged and continues to pass `/v1/responses` through to its upstream `/v1/responses` endpoint.

## Request Validation

Before converting a request, the provider `ForwardResponses` path should validate the Responses body with a shared helper so behavior is consistent across platforms. The helper should:

- Require valid JSON and a non-empty string `model`.
- Reject `previous_response_id` with a clear message that HTTP continuation is not supported for provider Responses compatibility.
- Reject image generation intent and image edit intent.
- Reject `/responses/compact` before service forwarding.
- Reject unsupported built-in tools with a message naming the unsupported tool type.
- Allow function tools and pass them through the existing compatibility conversion.

Validation should happen before account forwarding so unsupported requests do not consume provider quota.

## Data Flow

For non-streaming requests:

1. Route selects provider-specific `Responses(c)` based on the API key group platform.
2. Handler reads and validates the request body.
3. Handler parses request metadata for model, stream flag, session hash, and usage context.
4. Handler performs billing, concurrency, account selection, and failover with the same semantics as the provider's current handler.
5. Service converts the Responses request to the upstream format.
6. Service forwards to the provider's existing upstream endpoint.
7. Service converts the upstream response back to Responses format.
8. Handler records usage with inbound endpoint `/v1/responses` and upstream endpoint set to the concrete provider path used by that service.

For streaming requests:

1. The same route, validation, billing, concurrency, and account selection flow runs.
2. Service converts the Responses request to upstream streaming format.
3. Service reads upstream SSE events.
4. Service emits Responses SSE events to the client.
5. Usage is accumulated from terminal events or provider usage chunks and recorded after stream completion.

## Error Handling

Unsupported feature errors should use OpenAI-compatible error envelopes for Responses callers:

```json
{
  "error": {
    "message": "previous_response_id is not supported for this provider Responses compatibility path",
    "type": "invalid_request_error"
  }
}
```

Provider upstream errors should preserve current retry and failover semantics:

- Retryable upstream status codes should fail over to another account when no stream bytes have been written.
- Non-retryable upstream errors should return mapped client errors.
- Once streaming output has started, failover should stop and the stream should finish with the existing streaming-aware error behavior.

Route-level unsupported features should remain `404 not_found_error`, including WebSocket `GET /v1/responses` and compact subpaths.

## Usage And Observability

Usage records should preserve existing accounting fields and add accurate endpoint information:

- `InboundEndpoint`: `/v1/responses`
- `UpstreamEndpoint`: provider-specific concrete route, such as `/v1/messages`, `/v1/chat/completions`, `/anthropic/v1/messages`, or the equivalent internal canonical endpoint already used by the handler helpers
- `Model`: original requested model
- `UpstreamModel`: mapped provider model
- `ReasoningEffort`: value from `reasoning.effort` when present
- `RequestPayloadHash`: hash of the original client Responses body

Ops logging should use provider-specific components such as `handler.minimax_gateway.responses` and include request model, stream flag, selected account, and upstream endpoint.

## Testing Strategy

Route tests:

- Update MiniMax, GLM, Kimi, DeepSeek, and Windsurf route tests so `POST /v1/responses` and `POST /responses` dispatch to the provider handler instead of returning `404`.
- Keep `GET /v1/responses`, `/v1/responses/compact`, `/responses/compact`, image routes, and token counting unsupported for these providers.
- Keep OpenCode `/v1/responses` behavior unchanged.

Handler tests:

- For each provider, add tests that `Responses(c)` rejects invalid platform, empty body, missing model, unsupported model, `previous_response_id`, unsupported built-in tools, compact paths, and unsupported image intent.
- For each provider, add tests that successful non-streaming and streaming calls record usage with inbound `/v1/responses`.
- Verify account release behavior on forward errors and panics matches existing handlers.

Service tests:

- For MiniMax, GLM, and Kimi, assert `ForwardResponses` sends Anthropic Messages-shaped JSON to the provider upstream and writes Responses-shaped output.
- For DeepSeek and Windsurf, assert `ForwardResponses` sends Chat Completions-shaped JSON to the provider upstream and writes Responses-shaped output.
- Cover streaming conversion for at least one Anthropic-backed provider and one Chat-backed provider, then add provider-specific tests for model rewriting and usage parsing.
- Verify `reasoning.effort` survives into `ForwardResult.ReasoningEffort`.

Compatibility tests:

- Add cross-provider tests for common Codex request shapes:
  - string `input`
  - message-array `input`
  - `instructions`
  - function tool declaration
  - streaming function call output from upstream converted into Responses events
  - unsupported `previous_response_id` returning `400 invalid_request_error`

## Rollout Plan

Implement the feature in small provider slices:

1. Add shared Responses validation helpers and tests.
2. Add route dispatch for provider Responses handlers while keeping unsupported subpaths unchanged.
3. Implement one Anthropic-backed provider first, preferably GLM because it has a simple model set.
4. Generalize to MiniMax and Kimi.
5. Implement one Chat-backed provider first, preferably DeepSeek.
6. Generalize to Windsurf.
7. Run focused backend tests for routes, handlers, services, and compatibility conversion.
8. Update any user-facing endpoint documentation if the repository currently lists provider endpoint support.

## Non-Goals

This design does not add provider support for Responses WebSocket v2, `/responses/compact`, image generation, image edits, remote MCP tools, or HTTP `previous_response_id` continuation. Those features require stateful protocol handling or native upstream support and should be designed separately.
