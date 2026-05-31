# Provider Responses Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add HTTP `POST /v1/responses` compatibility for MiniMax, GLM, Kimi, DeepSeek, and Windsurf provider groups, while keeping OpenCode's existing direct `/v1/responses` passthrough.

**Architecture:** Add shared Responses compatibility validation, add missing Responses-to-Chat request and Chat-to-Responses response converters, then wire provider-specific handlers and services through existing provider upstream paths. MiniMax, GLM, and Kimi convert Responses to Anthropic Messages upstreams; DeepSeek and Windsurf convert Responses to Chat Completions upstreams. Compact, WebSocket, images, and HTTP `previous_response_id` remain explicit failures.

**Tech Stack:** Go, Gin, `github.com/tidwall/gjson`, `github.com/tidwall/sjson`, existing `internal/pkg/apicompat`, existing provider gateway handlers and services, Go tests under `backend/`.

---

## File Structure

- Create `backend/internal/pkg/apicompat/responses_to_chatcompletions_request.go`: converts Responses request bodies into Chat Completions request bodies for DeepSeek and Windsurf.
- Modify `backend/internal/pkg/apicompat/chatcompletions_responses_test.go`: adds request conversion tests and Chat response to Responses conversion tests.
- Create `backend/internal/pkg/apicompat/chatcompletions_to_responses_response.go`: converts Chat Completions non-stream and stream chunks into Responses output.
- Create `backend/internal/service/provider_responses_validation.go`: shared validation for provider Responses compatibility requests.
- Create `backend/internal/service/provider_responses_validation_test.go`: verifies unsupported Responses features fail before provider quota is consumed.
- Create `backend/internal/service/provider_responses_anthropic.go`: shared service helpers for Responses -> Anthropic upstream -> Responses client output.
- Create `backend/internal/service/provider_responses_chat.go`: shared service helpers for Responses -> Chat upstream -> Responses client output.
- Modify `backend/internal/server/routes/gateway.go`: route `POST /v1/responses` and `POST /responses` to MiniMax, GLM, Kimi, DeepSeek, and Windsurf provider handlers; keep compact and WebSocket unsupported.
- Modify `backend/internal/server/routes/gateway_test.go`: update route expectations for the new dispatch contract.
- Modify `backend/internal/handler/glm_gateway_handler.go`, `backend/internal/handler/minimax_gateway_handler.go`, `backend/internal/handler/kimi_gateway_handler.go`, `backend/internal/handler/deepseek_gateway_handler.go`, and `backend/internal/handler/windsurf_gateway_handler.go`: add `ForwardResponses` to forwarder interfaces and add `Responses(c)` handlers.
- Modify `backend/internal/handler/*_gateway_handler_test.go`: add provider Responses handler tests.
- Modify `backend/internal/service/glm_gateway_service.go`, `backend/internal/service/minimax_gateway_service.go`, `backend/internal/service/kimi_gateway_service.go`, `backend/internal/service/deepseek_gateway_service.go`, and `backend/internal/service/windsurf_gateway_service.go`: add `ForwardResponses`.
- Modify `backend/internal/service/*_gateway_service_test.go`: add provider service Responses tests.

## Implementation Tasks

### Task 1: Add Responses Request -> Chat Completions Request Converter

**Files:**
- Create: `backend/internal/pkg/apicompat/responses_to_chatcompletions_request.go`
- Modify: `backend/internal/pkg/apicompat/chatcompletions_responses_test.go`

- [ ] **Step 1: Write failing converter tests**

Append these tests to `backend/internal/pkg/apicompat/chatcompletions_responses_test.go`:

```go
func TestResponsesToChatCompletionsRequest_StringInput(t *testing.T) {
	req := &ResponsesRequest{
		Model:        "deepseek-v4-flash",
		Instructions: "answer concisely",
		Input:        json.RawMessage(`"hello"`),
		Stream:       true,
		Reasoning:    &ResponsesReasoning{Effort: "medium"},
	}

	chat, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Equal(t, "deepseek-v4-flash", chat.Model)
	require.True(t, chat.Stream)
	require.Equal(t, "medium", chat.ReasoningEffort)
	require.Len(t, chat.Messages, 2)
	require.Equal(t, "system", chat.Messages[0].Role)
	require.JSONEq(t, `"answer concisely"`, string(chat.Messages[0].Content))
	require.Equal(t, "user", chat.Messages[1].Role)
	require.JSONEq(t, `"hello"`, string(chat.Messages[1].Content))
}

func TestResponsesToChatCompletionsRequest_MessageArrayWithFunctionTool(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-5",
		Input: json.RawMessage(`[
			{"role":"user","content":[{"type":"input_text","text":"lookup weather"}]},
			{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"city\":\"Paris\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"sunny"}
		]`),
		Tools: []ResponsesTool{{
			Type:        "function",
			Name:        "lookup",
			Description: "Lookup a value",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
		}},
		ToolChoice: json.RawMessage(`{"type":"function","name":"lookup"}`),
	}

	chat, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, chat.Messages, 3)
	require.Equal(t, "user", chat.Messages[0].Role)
	require.Equal(t, "assistant", chat.Messages[1].Role)
	require.Len(t, chat.Messages[1].ToolCalls, 1)
	require.Equal(t, "call_1", chat.Messages[1].ToolCalls[0].ID)
	require.Equal(t, "lookup", chat.Messages[1].ToolCalls[0].Function.Name)
	require.Equal(t, "tool", chat.Messages[2].Role)
	require.Equal(t, "call_1", chat.Messages[2].ToolCallID)
	require.Len(t, chat.Tools, 1)
	require.Equal(t, "function", chat.Tools[0].Type)
	require.Equal(t, "lookup", chat.Tools[0].Function.Name)
	require.JSONEq(t, `{"type":"function","function":{"name":"lookup"}}`, string(chat.ToolChoice))
}

func TestResponsesToChatCompletionsRequest_RejectsImageContent(t *testing.T) {
	req := &ResponsesRequest{
		Model: "deepseek-v4-flash",
		Input: json.RawMessage(`[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,aaa"}]}]`),
	}

	_, err := ResponsesToChatCompletionsRequest(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "input_image")
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
cd backend && go test ./internal/pkg/apicompat -run 'TestResponsesToChatCompletionsRequest' -count=1
```

Expected: FAIL with `undefined: ResponsesToChatCompletionsRequest`.

- [ ] **Step 3: Implement converter**

Create `backend/internal/pkg/apicompat/responses_to_chatcompletions_request.go` with these public entry points and helper responsibilities:

```go
package apicompat

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ResponsesToChatCompletionsRequest(req *ResponsesRequest) (*ChatCompletionsRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("responses request is nil")
	}
	out := &ChatCompletionsRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		ServiceTier: req.ServiceTier,
		ToolChoice:  convertResponsesToolChoiceToChat(req.ToolChoice),
	}
	if req.MaxOutputTokens != nil {
		v := *req.MaxOutputTokens
		out.MaxCompletionTokens = &v
	}
	if req.Reasoning != nil {
		out.ReasoningEffort = strings.TrimSpace(req.Reasoning.Effort)
	}
	if strings.TrimSpace(req.Instructions) != "" {
		raw, err := json.Marshal(req.Instructions)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, ChatMessage{Role: "system", Content: raw})
	}
	messages, err := responsesInputToChatMessages(req.Input)
	if err != nil {
		return nil, err
	}
	out.Messages = append(out.Messages, messages...)
	tools, err := responsesToolsToChatTools(req.Tools)
	if err != nil {
		return nil, err
	}
	out.Tools = tools
	return out, nil
}
```

The private helpers must cover these mappings:

- Responses string `input` -> one user Chat message with JSON string content.
- Responses role message with `input_text` parts -> Chat message with either JSON string content or Chat text parts.
- Responses `function_call` item -> Chat assistant message with one `tool_calls` entry.
- Responses `function_call_output` item -> Chat tool message with `tool_call_id`.
- Responses function tool -> Chat `{"type":"function","function":...}` tool.
- Responses `tool_choice` object `{"type":"function","name":"lookup"}` -> Chat `{"type":"function","function":{"name":"lookup"}}`.
- Any `input_image`, `image_generation`, `web_search`, `local_shell`, `mcp`, or unknown tool type -> error.

- [ ] **Step 4: Run converter tests**

Run:

```bash
cd backend && go test ./internal/pkg/apicompat -run 'TestResponsesToChatCompletionsRequest' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/pkg/apicompat/responses_to_chatcompletions_request.go backend/internal/pkg/apicompat/chatcompletions_responses_test.go
git commit -m "feat(apicompat): convert responses requests to chat completions"
```

### Task 2: Add Chat Completions Response -> Responses Output Converter

**Files:**
- Create: `backend/internal/pkg/apicompat/chatcompletions_to_responses_response.go`
- Modify: `backend/internal/pkg/apicompat/chatcompletions_responses_test.go`

- [ ] **Step 1: Write failing response converter tests**

Append these tests to `backend/internal/pkg/apicompat/chatcompletions_responses_test.go`:

```go
func TestChatCompletionsToResponsesResponse_TextAndUsage(t *testing.T) {
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl_1",
		Model: "deepseek-v4-flash",
		Choices: []ChatChoice{{
			Index: 0,
			Message: ChatMessage{
				Role:    "assistant",
				Content: json.RawMessage(`"hello back"`),
			},
			FinishReason: "stop",
		}},
		Usage: &ChatUsage{PromptTokens: 7, CompletionTokens: 5, TotalTokens: 12},
	}

	out := ChatCompletionsToResponsesResponse(resp, "client-model")
	require.Equal(t, "chatcmpl_1", out.ID)
	require.Equal(t, "client-model", out.Model)
	require.Equal(t, "completed", out.Status)
	require.Len(t, out.Output, 1)
	require.Equal(t, "message", out.Output[0].Type)
	require.Equal(t, "hello back", out.Output[0].Content[0].Text)
	require.Equal(t, 7, out.Usage.InputTokens)
	require.Equal(t, 5, out.Usage.OutputTokens)
	require.Equal(t, 12, out.Usage.TotalTokens)
}

func TestChatCompletionsToResponsesResponse_ToolCall(t *testing.T) {
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl_2",
		Model: "deepseek-v4-flash",
		Choices: []ChatChoice{{
			Message: ChatMessage{
				Role: "assistant",
				ToolCalls: []ChatToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: ChatFunctionCall{Name: "lookup", Arguments: "{\"q\":\"x\"}"},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}

	out := ChatCompletionsToResponsesResponse(resp, "client-model")
	require.Equal(t, "completed", out.Status)
	require.Len(t, out.Output, 1)
	require.Equal(t, "function_call", out.Output[0].Type)
	require.Equal(t, "call_1", out.Output[0].CallID)
	require.Equal(t, "lookup", out.Output[0].Name)
	require.JSONEq(t, `{"q":"x"}`, out.Output[0].Arguments)
}

func TestChatChunkToResponsesEvents_TextAndDone(t *testing.T) {
	state := NewChatCompletionsToResponsesState("client-model")
	events := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		ID: "chatcmpl_stream",
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{Role: "assistant"},
		}},
	}, state)
	require.NotEmpty(t, events)
	require.Equal(t, "response.created", events[0].Type)

	text := "hello"
	events = ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		ID: "chatcmpl_stream",
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{Content: &text},
		}},
	}, state)
	require.Equal(t, "response.output_text.delta", events[0].Type)
	require.Equal(t, "hello", events[0].Delta)

	finish := "stop"
	events = ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		ID: "chatcmpl_stream",
		Choices: []ChatChunkChoice{{
			FinishReason: &finish,
		}},
		Usage: &ChatUsage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
	}, state)
	require.Equal(t, "response.completed", events[len(events)-1].Type)
	require.Equal(t, 5, events[len(events)-1].Response.Usage.TotalTokens)
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
cd backend && go test ./internal/pkg/apicompat -run 'TestChatCompletionsToResponsesResponse|TestChatChunkToResponsesEvents' -count=1
```

Expected: FAIL with undefined converter symbols.

- [ ] **Step 3: Implement response converter**

Create `backend/internal/pkg/apicompat/chatcompletions_to_responses_response.go` with:

```go
package apicompat

import (
	"encoding/json"
	"time"
)

type ChatCompletionsToResponsesState struct {
	ID             string
	Model          string
	CreatedMessage bool
	OutputIndex    int
	ToolIndexMap   map[int]int
	Usage          *ResponsesUsage
}

func NewChatCompletionsToResponsesState(model string) *ChatCompletionsToResponsesState {
	return &ChatCompletionsToResponsesState{
		Model:        model,
		ToolIndexMap: make(map[int]int),
	}
}

func ChatCompletionsToResponsesResponse(resp *ChatCompletionsResponse, model string) *ResponsesResponse {
	out := &ResponsesResponse{
		ID:     resp.ID,
		Object: "response",
		Model:  model,
		Status: "completed",
	}
	if resp == nil {
		return out
	}
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		out.Output = chatMessageToResponsesOutput(choice.Message)
		if choice.FinishReason == "length" {
			out.Status = "incomplete"
			out.IncompleteDetails = &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
		}
	}
	out.Usage = chatUsageToResponsesUsage(resp.Usage)
	return out
}

func ChatChunkToResponsesEvents(chunk *ChatCompletionsChunk, state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	if chunk == nil || state == nil {
		return nil
	}
	if state.ID == "" {
		state.ID = chunk.ID
	}
	if state.Model == "" {
		state.Model = chunk.Model
	}
	var events []ResponsesStreamEvent
	if !state.CreatedMessage {
		state.CreatedMessage = true
		events = append(events, ResponsesStreamEvent{
			Type: "response.created",
			Response: &ResponsesResponse{
				ID:     state.ID,
				Object: "response",
				Model:  state.Model,
				Status: "in_progress",
			},
		})
	}
	events = append(events, chatChunkChoicesToResponsesEvents(chunk.Choices, state)...)
	if chunk.Usage != nil {
		state.Usage = chatUsageToResponsesUsage(chunk.Usage)
	}
	return events
}

func FinalizeChatCompletionsResponsesStream(state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	if state == nil {
		return nil
	}
	return []ResponsesStreamEvent{{
		Type: "response.completed",
		Response: &ResponsesResponse{
			ID:     state.ID,
			Object: "response",
			Model:  state.Model,
			Status: "completed",
			Usage:  state.Usage,
		},
	}}
}
```

The helper implementation must map:

- Chat assistant text -> Responses `message` output with `output_text`.
- Chat `tool_calls` -> Responses `function_call`.
- Chat `reasoning_content` -> Responses `reasoning` summary.
- Chat streaming text delta -> `response.output_text.delta`.
- Chat streaming tool call deltas -> `response.output_item.added` and `response.function_call_arguments.delta`.
- Chat usage -> Responses usage, including cached prompt tokens when present.

- [ ] **Step 4: Run response converter tests**

Run:

```bash
cd backend && go test ./internal/pkg/apicompat -run 'TestChatCompletionsToResponsesResponse|TestChatChunkToResponsesEvents' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/pkg/apicompat/chatcompletions_to_responses_response.go backend/internal/pkg/apicompat/chatcompletions_responses_test.go
git commit -m "feat(apicompat): convert chat completions responses to responses"
```

### Task 3: Add Shared Provider Responses Validation

**Files:**
- Create: `backend/internal/service/provider_responses_validation.go`
- Create: `backend/internal/service/provider_responses_validation_test.go`

- [ ] **Step 1: Write failing validation tests**

Create `backend/internal/service/provider_responses_validation_test.go`:

```go
package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateProviderResponsesCompatibilityRequestAllowsTextAndFunctionTools(t *testing.T) {
	body := []byte(`{
		"model":"glm-5.1",
		"input":"hello",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],
		"reasoning":{"effort":"medium"}
	}`)

	err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", body)
	require.NoError(t, err)
}

func TestValidateProviderResponsesCompatibilityRequestRejectsPreviousResponseID(t *testing.T) {
	err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{
		"model":"glm-5.1",
		"previous_response_id":"resp_1",
		"input":"continue"
	}`))

	require.Error(t, err)
	var compatErr *ProviderResponsesCompatibilityError
	require.ErrorAs(t, err, &compatErr)
	require.Equal(t, http.StatusBadRequest, compatErr.StatusCode)
	require.Equal(t, "invalid_request_error", compatErr.Type)
	require.Contains(t, compatErr.Message, "previous_response_id")
}

func TestValidateProviderResponsesCompatibilityRequestRejectsCompactPath(t *testing.T) {
	err := ValidateProviderResponsesCompatibilityRequest("/v1/responses/compact", []byte(`{"model":"glm-5.1","input":"hello"}`))

	require.Error(t, err)
	require.Contains(t, err.Error(), "compact")
}

func TestValidateProviderResponsesCompatibilityRequestRejectsImageIntent(t *testing.T) {
	err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{
		"model":"glm-5.1",
		"input":"draw",
		"tools":[{"type":"image_generation"}],
		"tool_choice":{"type":"image_generation"}
	}`))

	require.Error(t, err)
	require.Contains(t, err.Error(), "image_generation")
}

func TestValidateProviderResponsesCompatibilityRequestRejectsBuiltinTools(t *testing.T) {
	for _, toolType := range []string{"web_search", "local_shell", "mcp"} {
		t.Run(toolType, func(t *testing.T) {
			err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{
				"model":"glm-5.1",
				"input":"hello",
				"tools":[{"type":"`+toolType+`"}]
			}`))
			require.Error(t, err)
			require.Contains(t, err.Error(), toolType)
		})
	}
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
cd backend && go test ./internal/service -run 'TestValidateProviderResponsesCompatibilityRequest' -count=1
```

Expected: FAIL with `undefined: ValidateProviderResponsesCompatibilityRequest`.

- [ ] **Step 3: Implement validator**

Create `backend/internal/service/provider_responses_validation.go`:

```go
package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

type ProviderResponsesCompatibilityError struct {
	StatusCode int
	Type       string
	Message    string
}

func (e *ProviderResponsesCompatibilityError) Error() string {
	return e.Message
}

func ValidateProviderResponsesCompatibilityRequest(path string, body []byte) error {
	trimmedPath := strings.TrimRight(strings.TrimSpace(path), "/")
	if strings.Contains(trimmedPath, "/responses/compact") {
		return providerResponsesInvalid("provider Responses compatibility does not support /responses/compact")
	}
	if !gjson.ValidBytes(body) {
		return providerResponsesInvalid("Failed to parse request body")
	}
	if strings.TrimSpace(gjson.GetBytes(body, "model").String()) == "" {
		return providerResponsesInvalid("model is required")
	}
	if strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()) != "" {
		return providerResponsesInvalid("previous_response_id is not supported for this provider Responses compatibility path")
	}
	if IsImageGenerationIntent("/v1/responses", gjson.GetBytes(body, "model").String(), body) {
		return providerResponsesInvalid("image_generation is not supported for this provider Responses compatibility path")
	}
	if err := rejectUnsupportedProviderResponsesTools(body); err != nil {
		return err
	}
	if gjson.GetBytes(body, `input.#(content.#(type=="input_image"))`).Exists() {
		return providerResponsesInvalid("input_image is not supported for this provider Responses compatibility path")
	}
	return nil
}

func providerResponsesInvalid(message string) *ProviderResponsesCompatibilityError {
	return &ProviderResponsesCompatibilityError{
		StatusCode: http.StatusBadRequest,
		Type:       "invalid_request_error",
		Message:    message,
	}
}

func rejectUnsupportedProviderResponsesTools(body []byte) error {
	for _, tool := range gjson.GetBytes(body, "tools").Array() {
		toolType := strings.TrimSpace(tool.Get("type").String())
		if toolType == "" || toolType == "function" {
			continue
		}
		return providerResponsesInvalid(fmt.Sprintf("%s is not supported for this provider Responses compatibility path", toolType))
	}
	return nil
}
```

- [ ] **Step 4: Run validation tests**

Run:

```bash
cd backend && go test ./internal/service -run 'TestValidateProviderResponsesCompatibilityRequest' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/provider_responses_validation.go backend/internal/service/provider_responses_validation_test.go
git commit -m "feat(gateway): validate provider responses compatibility requests"
```

### Task 4: Add Route Dispatch for Provider Responses

**Files:**
- Modify: `backend/internal/server/routes/gateway.go`
- Modify: `backend/internal/server/routes/gateway_test.go`

- [ ] **Step 1: Update route tests first**

In `backend/internal/server/routes/gateway_test.go`, change provider unsupported endpoint tests so root `/v1/responses`, `/responses`, and `/backend-api/codex/responses` are not in the unsupported path lists. Add dispatch tests for each provider:

```go
func TestGatewayRoutesGLMResponsesDispatchesToGLMHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformGLM)

	for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"glm-5.1","input":"hello"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusServiceUnavailable, w.Code, "path=%s", path)
		require.Contains(t, w.Body.String(), "glm gateway service unavailable", "path=%s", path)
	}
}
```

Add these dispatch tests with the same request loop used by `TestGatewayRoutesGLMResponsesDispatchesToGLMHandler` and the listed request body and expected service-unavailable text:

- `TestGatewayRoutesMiniMaxResponsesDispatchesToMiniMaxHandler`: body `{"model":"MiniMax-M2.7","input":"hello"}`, expected text `minimax gateway service unavailable`.
- `TestGatewayRoutesKimiResponsesDispatchesToKimiHandler`: body `{"model":"kimi-for-coding","input":"hello"}`, expected text `kimi gateway service unavailable`.
- `TestGatewayRoutesDeepSeekResponsesDispatchesToDeepSeekHandler`: body `{"model":"deepseek-chat","input":"hello"}`, expected text `deepseek gateway service unavailable`.
- `TestGatewayRoutesWindsurfResponsesDispatchesToWindsurfHandler`: body `{"model":"claude-sonnet-4.6","input":"hello"}`, expected text `windsurf gateway service unavailable`.

Keep compact and WebSocket tests expecting `404 not_found_error`.

- [ ] **Step 2: Run route tests and verify failure**

Run:

```bash
cd backend && go test ./internal/server/routes -run 'TestGatewayRoutes.*Responses|TestGatewayRoutes.*Unsupported' -count=1
```

Expected: FAIL because root provider Responses routes still call unsupported handlers.

- [ ] **Step 3: Update route dispatch**

In `backend/internal/server/routes/gateway.go`, update every `POST /responses` dispatch switch to call provider `Responses(c)` writers:

```go
case service.PlatformMiniMax:
	writeMiniMaxResponses(c, h)
	return
case service.PlatformGLM:
	writeGLMResponses(c, h)
	return
case service.PlatformKimi:
	writeKimiResponses(c, h)
	return
case service.PlatformDeepSeek:
	writeDeepSeekResponses(c, h)
	return
case service.PlatformWindsurf:
	writeWindsurfResponses(c, h)
	return
```

Add writer helpers next to the existing chat helpers:

```go
func writeGLMResponses(c *gin.Context, h *handler.Handlers) {
	if h != nil && h.GLMGateway != nil {
		h.GLMGateway.Responses(c)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "api_error",
			"message": "glm gateway service unavailable",
		},
	})
}
```

Add four more writer helpers with the same structure and these concrete method calls and fallback messages:

- `writeMiniMaxResponses`: call `h.MiniMaxGateway.Responses(c)`, fallback `minimax gateway service unavailable`.
- `writeKimiResponses`: call `h.KimiGateway.Responses(c)`, fallback `kimi gateway service unavailable`.
- `writeDeepSeekResponses`: call `h.DeepSeekGateway.Responses(c)`, fallback `deepseek gateway service unavailable`.
- `writeWindsurfResponses`: call `h.WindsurfGateway.Responses(c)`, fallback `windsurf gateway service unavailable`.

- [ ] **Step 4: Keep compact and GET unsupported**

For `POST /responses/*subpath`, keep provider compact paths routed to existing unsupported writers. Do not dispatch subpaths to provider `Responses(c)`.

- [ ] **Step 5: Run route tests**

Run:

```bash
cd backend && go test ./internal/server/routes -run 'TestGatewayRoutes.*Responses|TestGatewayRoutes.*Unsupported' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/server/routes/gateway.go backend/internal/server/routes/gateway_test.go
git commit -m "feat(gateway): route provider responses requests"
```

### Task 5: Add Shared Anthropic-Backed Provider Responses Service Helper

**Files:**
- Create: `backend/internal/service/provider_responses_anthropic.go`
- Create: `backend/internal/service/provider_responses_anthropic_test.go`

- [ ] **Step 1: Write helper tests**

Create `backend/internal/service/provider_responses_anthropic_test.go` with a narrow fake upstream test:

```go
package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardProviderResponsesViaAnthropicBuildsMessagesAndWritesResponses(t *testing.T) {
	var capturedBody []byte
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": {"text/event-stream"},
				"X-Request-Id": {"anthropic-resp-1"},
			},
			Body: io.NopCloser(strings.NewReader(
				"event: message_start\n"+
					"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"GLM-5.1\",\"content\":[],\"usage\":{\"input_tokens\":4,\"output_tokens\":0}}}\n\n"+
					"event: content_block_start\n"+
					"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
					"event: content_block_delta\n"+
					"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n"+
					"event: message_delta\n"+
					"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n",
			)),
		}, nil
	})}
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	result, err := forwardProviderResponsesViaAnthropic(context.Background(), c, providerResponsesAnthropicConfig{
		ServiceName: "test",
		HTTPClient:  client,
		Account:     &Account{ID: 1, Platform: PlatformGLM},
		Body:        []byte(`{"model":"glm-5.1","input":"hello","reasoning":{"effort":"medium"}}`),
		BuildRequest: func(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://upstream.example/v1/messages", strings.NewReader(string(body)))
			return req, "glm-5.1", "GLM-5.1", err
		},
		ShouldReturnUpstreamError: func(status int) bool { return status >= http.StatusBadRequest },
		ReadErrorBody:             readGLMNonStreamResponseBody,
	})

	require.NoError(t, err)
	require.Equal(t, "anthropic-resp-1", result.RequestID)
	require.Equal(t, "medium", *result.ReasoningEffort)
	require.Equal(t, "GLM-5.1", gjson.GetBytes(capturedBody, "model").String())
	require.Equal(t, "user", gjson.GetBytes(capturedBody, "messages.0.role").String())
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
	require.Equal(t, "hello", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
}
```

- [ ] **Step 2: Run helper test and verify failure**

Run:

```bash
cd backend && go test ./internal/service -run 'TestForwardProviderResponsesViaAnthropic' -count=1
```

Expected: FAIL with undefined helper symbols.

- [ ] **Step 3: Implement helper**

Create `backend/internal/service/provider_responses_anthropic.go` with:

```go
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

type providerResponsesAnthropicConfig struct {
	ServiceName               string
	HTTPClient                *http.Client
	Account                   *Account
	Body                      []byte
	BuildRequest              func(context.Context, *gin.Context, *Account, []byte) (*http.Request, string, string, error)
	ShouldReturnUpstreamError func(int) bool
	ReadErrorBody             func(io.Reader) ([]byte, error)
}

func forwardProviderResponsesViaAnthropic(ctx context.Context, c *gin.Context, cfg providerResponsesAnthropicConfig) (*ForwardResult, error) {
	start := time.Now()
	if err := ValidateProviderResponsesCompatibilityRequest(requestPathForResponsesValidation(c), cfg.Body); err != nil {
		writeProviderResponsesCompatibilityError(c, err)
		return nil, err
	}
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(cfg.Body, &responsesReq); err != nil {
		return nil, fmt.Errorf("parse responses request: %w", err)
	}
	anthropicReq, err := apicompat.ResponsesToAnthropicRequest(&responsesReq)
	if err != nil {
		return nil, fmt.Errorf("convert responses to anthropic: %w", err)
	}
	anthropicReq.Stream = true
	anthropicBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}
	upstreamReq, originalModel, upstreamModel, err := cfg.BuildRequest(ctx, c, cfg.Account, anthropicBody)
	if err != nil {
		return nil, err
	}
	resp, err := cfg.HTTPClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("%s upstream request failed: %w", cfg.ServiceName, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if cfg.ShouldReturnUpstreamError(resp.StatusCode) {
		body, readErr := cfg.ReadErrorBody(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: body, ResponseHeaders: resp.Header.Clone()}
	}
	reasoningEffort := ExtractResponsesReasoningEffortFromBody(cfg.Body)
	if responsesReq.Stream {
		return handleProviderResponsesAnthropicStreaming(resp, c, originalModel, upstreamModel, reasoningEffort, start)
	}
	return handleProviderResponsesAnthropicBuffered(resp, c, originalModel, upstreamModel, reasoningEffort, start)
}
```

Move reusable buffered and streaming Anthropic-to-Responses logic from `backend/internal/service/gateway_forward_as_responses.go` into private helper functions in this file, keeping the existing `GatewayService` behavior intact by having it call the shared functions or by keeping its current implementation unchanged and copying only the provider-safe logic.

- [ ] **Step 4: Run helper test**

Run:

```bash
cd backend && go test ./internal/service -run 'TestForwardProviderResponsesViaAnthropic|TestValidateProviderResponsesCompatibilityRequest' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/provider_responses_anthropic.go backend/internal/service/provider_responses_anthropic_test.go
git commit -m "feat(gateway): add anthropic provider responses bridge"
```

### Task 6: Implement GLM Responses End-to-End

**Files:**
- Modify: `backend/internal/handler/glm_gateway_handler.go`
- Modify: `backend/internal/handler/glm_gateway_handler_test.go`
- Modify: `backend/internal/service/glm_gateway_service.go`
- Modify: `backend/internal/service/glm_gateway_service_test.go`

- [ ] **Step 1: Add GLM service tests**

Append to `backend/internal/service/glm_gateway_service_test.go`:

```go
func TestGLMGatewayServiceForwardResponsesBuildsAnthropicRequestAndWritesResponses(t *testing.T) {
	var captured *http.Request
	var capturedBody []byte
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"glm-resp-1"}},
			Body: io.NopCloser(strings.NewReader(
				"event: message_start\n"+
					"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"GLM-5.1\",\"content\":[],\"usage\":{\"input_tokens\":4,\"output_tokens\":0}}}\n\n"+
					"event: content_block_start\n"+
					"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
					"event: content_block_delta\n"+
					"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n"+
					"event: message_delta\n"+
					"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n",
			)),
		}, nil
	})}
	svc := NewGLMGatewayService(client, nil)
	c, rec := newGLMGatewayTestContext("/v1/responses")
	body := []byte(`{"model":"claude-sonnet-4-5","input":"hello","reasoning":{"effort":"medium"}}`)

	result, err := svc.ForwardResponses(context.Background(), c, glmGatewayTestAccount(), body, "req-responses")
	require.NoError(t, err)
	require.Equal(t, "https://open.bigmodel.cn/api/anthropic/v1/messages", captured.URL.String())
	require.Equal(t, "GLM-5.1", gjson.GetBytes(capturedBody, "model").String())
	require.Equal(t, "user", gjson.GetBytes(capturedBody, "messages.0.role").String())
	require.Equal(t, "glm-resp-1", result.RequestID)
	require.Equal(t, "claude-sonnet-4-5", result.Model)
	require.Equal(t, "GLM-5.1", result.UpstreamModel)
	require.Equal(t, "medium", *result.ReasoningEffort)
	require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
	require.Equal(t, "hello", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
}
```

- [ ] **Step 2: Add GLM handler tests**

Update `fakeGLMForwarder` in `backend/internal/handler/glm_gateway_handler_test.go` with:

```go
responsesCalled int

func (f *fakeGLMForwarder) ForwardResponses(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	f.responsesCalled++
	f.account = account
	f.body = append([]byte(nil), body...)
	f.requestID = requestID
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	c.JSON(http.StatusOK, gin.H{"id": "resp_1", "object": "response", "model": "glm-5.1"})
	effort := "medium"
	return &service.ForwardResult{
		RequestID:       "glm-upstream-resp-req-1",
		Model:           "glm-5.1",
		UpstreamModel:   "GLM-5.1",
		ReasoningEffort: &effort,
		Usage: service.ClaudeUsage{InputTokens: 9, OutputTokens: 4},
		Duration: time.Millisecond,
	}, nil
}
```

Add:

```go
func TestGLMGatewayHandlerResponsesSuccessForwardsAndRecordsUsage(t *testing.T) {
	forwarder := &fakeGLMForwarder{}
	gateway := &fakeGLMGatewayService{
		selections: []*service.AccountSelectionResult{{Account: glmTestAccount(101), Acquired: true, ReleaseFunc: func() {}}},
	}
	h := &GLMGatewayHandler{
		glmService:          forwarder,
		gatewayService:      gateway,
		billingCacheService: &fakeGLMBillingChecker{},
		concurrencyHelper:   &fakeGLMConcurrencyController{allowWait: true},
		maxAccountSwitches:  3,
	}
	c, rec, apiKey := newGLMHandlerTestContextForPath(t, "/v1/responses", service.PlatformGLM, `{"model":"glm-5.1","input":"hello","reasoning":{"effort":"medium"}}`)

	h.Responses(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, forwarder.responsesCalled)
	require.Equal(t, 0, forwarder.messagesCalled)
	require.Equal(t, 0, forwarder.chatCalled)
	require.JSONEq(t, `{"model":"glm-5.1","input":"hello","reasoning":{"effort":"medium"}}`, string(forwarder.body))
	require.NotNil(t, gateway.recorded)
	require.Equal(t, apiKey, gateway.recorded.APIKey)
	require.Equal(t, "/v1/responses", gateway.recorded.InboundEndpoint)
	require.Equal(t, "/v1/responses", gateway.recorded.UpstreamEndpoint)
	require.NotNil(t, gateway.recorded.Result.ReasoningEffort)
	require.Equal(t, "medium", *gateway.recorded.Result.ReasoningEffort)
}
```

Add a rejection test for `previous_response_id` returning `400 invalid_request_error`.

- [ ] **Step 3: Run GLM tests and verify failure**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler -run 'TestGLM.*Responses|TestGLMGatewayServiceForwardResponses' -count=1
```

Expected: FAIL with missing `ForwardResponses` and `Responses` methods.

- [ ] **Step 4: Implement GLM service and handler**

In `backend/internal/service/glm_gateway_service.go`, add:

```go
func (s *GLMGatewayService) ForwardResponses(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("glm gateway service unavailable")
	}
	if _, err := validateGLMAccount(account); err != nil {
		return nil, err
	}
	return forwardProviderResponsesViaAnthropic(ctx, c, providerResponsesAnthropicConfig{
		ServiceName:               "glm",
		HTTPClient:                s.httpClient,
		Account:                   account,
		Body:                      body,
		BuildRequest:              s.buildMessagesRequest,
		ShouldReturnUpstreamError: shouldReturnGLMUpstreamError,
		ReadErrorBody:             readGLMNonStreamResponseBody,
	})
}
```

In `backend/internal/handler/glm_gateway_handler.go`:

- Add `ForwardResponses` to `glmMessagesForwarder`.
- Add `func (h *GLMGatewayHandler) Responses(c *gin.Context)` by following the existing `ChatCompletions` body with `service.ParseGatewayRequest(body, "responses")` and `h.glmService.ForwardResponses(...)`.
- Use `ValidateProviderResponsesCompatibilityRequest` before account selection and return its status/type/message via `h.errorResponse`.

- [ ] **Step 5: Run GLM tests**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler -run 'TestGLM.*Responses|TestGLMGatewayServiceForwardResponses' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/glm_gateway_service.go backend/internal/service/glm_gateway_service_test.go backend/internal/handler/glm_gateway_handler.go backend/internal/handler/glm_gateway_handler_test.go
git commit -m "feat(glm): support responses compatibility"
```

### Task 7: Implement MiniMax and Kimi Responses

**Files:**
- Modify: `backend/internal/handler/minimax_gateway_handler.go`
- Modify: `backend/internal/handler/minimax_gateway_handler_test.go`
- Modify: `backend/internal/service/minimax_gateway_service.go`
- Modify: `backend/internal/service/minimax_chat_completions_test.go`
- Modify: `backend/internal/handler/kimi_gateway_handler.go`
- Modify: `backend/internal/handler/kimi_gateway_handler_test.go`
- Modify: `backend/internal/service/kimi_gateway_service.go`
- Modify: `backend/internal/service/kimi_gateway_service_test.go`

- [ ] **Step 1: Add MiniMax tests**

Add `TestMiniMaxGatewayServiceForwardResponsesBuildsAnthropicRequestAndWritesResponses` to the MiniMax service test file. Use the same SSE fixture as the GLM service test, capture the upstream request in `captured *http.Request`, and assert:

```go
require.Equal(t, "https://api.minimaxi.com/anthropic/v1/messages", captured.URL.String())
require.Equal(t, "MiniMax-M2.7", gjson.GetBytes(capturedBody, "model").String())
require.Equal(t, "MiniMax-M2.7", result.UpstreamModel)
require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
```

Add `TestMiniMaxGatewayHandlerResponsesSuccessForwardsAndRecordsUsage` to the MiniMax handler test file. Extend the fake forwarder with `responsesCalled int` and `ForwardResponses(...)`. Assert:

```go
require.Equal(t, 1, forwarder.responsesCalled)
require.Equal(t, "/v1/responses", gateway.recorded.InboundEndpoint)
require.Equal(t, "/v1/responses", gateway.recorded.UpstreamEndpoint)
```

- [ ] **Step 2: Add Kimi tests**

Add `TestKimiGatewayServiceForwardResponsesBuildsAnthropicRequestAndWritesResponses` to the Kimi service test file. Use the same SSE fixture as the GLM service test, capture the upstream request in `captured *http.Request`, and assert:

```go
require.Equal(t, "https://api.kimi.com/coding/v1/messages", captured.URL.String())
require.Equal(t, "kimi-for-coding", gjson.GetBytes(capturedBody, "model").String())
require.Equal(t, "kimi-for-coding", result.UpstreamModel)
require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
```

Add `TestKimiGatewayHandlerResponsesSuccessForwardsAndRecordsUsage` to the Kimi handler test file. Extend the fake forwarder with `responsesCalled int` and `ForwardResponses(...)`, then assert `responsesCalled == 1`, `gateway.recorded.InboundEndpoint == "/v1/responses"`, and `gateway.recorded.UpstreamEndpoint == "/v1/responses"`.

Add `TestKimiGatewayHandlerResponsesRejectsNonKimiModel` with body `{"model":"claude-sonnet-4-5","input":"hello"}` and assert `http.StatusBadRequest` plus response text `Kimi gateway only supports model kimi-for-coding`.

- [ ] **Step 3: Run MiniMax/Kimi tests and verify failure**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler -run 'Test(MiniMax|Kimi).*Responses|Test(MiniMax|Kimi)GatewayServiceForwardResponses' -count=1
```

Expected: FAIL with missing methods and fake forwarder interface errors.

- [ ] **Step 4: Implement MiniMax**

In `backend/internal/service/minimax_gateway_service.go`, add `ForwardResponses` using `forwardProviderResponsesViaAnthropic`, `s.buildMessagesRequest`, `shouldReturnMiniMaxUpstreamError`, and `readMiniMaxNonStreamResponseBody`. Preserve MiniMax text quota semantics from `ForwardMessages`: trim `requestID`, require `s.quotaService`, call `s.quotaService.ReserveTextRequest(ctx, account, requestID)` before the upstream request, call `RollbackTextRequest` on upstream request failure, upstream error status, or response handling error, and return `minimax quota exhausted` when the reservation is denied.

In `backend/internal/handler/minimax_gateway_handler.go`, add `ForwardResponses` to the forwarder interface. Add `Responses(c)` by copying the existing `ChatCompletions(c)` control flow, changing the parser call to `service.ParseGatewayRequest(body, "responses")`, calling `ValidateProviderResponsesCompatibilityRequest(c.Request.URL.Path, body)` before concurrency acquisition, and calling `h.minimaxService.ForwardResponses(...)` inside the failover loop.

- [ ] **Step 5: Implement Kimi**

In `backend/internal/service/kimi_gateway_service.go`, add `ForwardResponses` using `forwardProviderResponsesViaAnthropic`, `s.buildMessagesRequest`, `shouldReturnKimiUpstreamError`, and `readGLMNonStreamResponseBody`.

In `backend/internal/handler/kimi_gateway_handler.go`, add `ForwardResponses` to the forwarder interface. Add `Responses(c)` by copying the existing `ChatCompletions(c)` control flow, changing the parser call to `service.ParseGatewayRequest(body, "responses")`, calling `ValidateProviderResponsesCompatibilityRequest(c.Request.URL.Path, body)` before concurrency acquisition, preserving the strict `kimi-for-coding` model check, and calling `h.kimiService.ForwardResponses(...)` inside the failover loop.

- [ ] **Step 6: Run MiniMax/Kimi tests**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler -run 'Test(MiniMax|Kimi).*Responses|Test(MiniMax|Kimi)GatewayServiceForwardResponses' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/service/minimax_gateway_service.go backend/internal/service/minimax_chat_completions_test.go backend/internal/handler/minimax_gateway_handler.go backend/internal/handler/minimax_gateway_handler_test.go backend/internal/service/kimi_gateway_service.go backend/internal/service/kimi_gateway_service_test.go backend/internal/handler/kimi_gateway_handler.go backend/internal/handler/kimi_gateway_handler_test.go
git commit -m "feat(gateway): support responses for minimax and kimi"
```

### Task 8: Add Shared Chat-Backed Provider Responses Service Helper

**Files:**
- Create: `backend/internal/service/provider_responses_chat.go`
- Create: `backend/internal/service/provider_responses_chat_test.go`

- [ ] **Step 1: Write helper tests**

Create `backend/internal/service/provider_responses_chat_test.go`:

```go
package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardProviderResponsesViaChatBuildsChatAndWritesResponses(t *testing.T) {
	var capturedBody []byte
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"chat-resp-1"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chat_1","model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`)),
		}, nil
	})}
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	result, err := forwardProviderResponsesViaChat(context.Background(), c, providerResponsesChatConfig{
		ServiceName: "test-chat",
		HTTPClient:  client,
		Account:     &Account{ID: 1, Platform: PlatformDeepSeek},
		Body:        []byte(`{"model":"deepseek-chat","input":"hello","reasoning":{"effort":"medium"}}`),
		BuildRequest: func(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://upstream.example/chat/completions", strings.NewReader(string(body)))
			return req, "deepseek-chat", "deepseek-v4-flash", err
		},
		ShouldReturnUpstreamError: func(status int) bool { return status >= http.StatusBadRequest },
		ReadErrorBody:             readGLMNonStreamResponseBody,
		ParseUsage:                parseDeepSeekOpenAIUsage,
		ParseStreamingUsage:       parseDeepSeekOpenAIStreamingUsage,
	})

	require.NoError(t, err)
	require.Equal(t, "chat-resp-1", result.RequestID)
	require.Equal(t, "medium", *result.ReasoningEffort)
	require.Equal(t, "deepseek-v4-flash", gjson.GetBytes(capturedBody, "model").String())
	require.Equal(t, "user", gjson.GetBytes(capturedBody, "messages.0.role").String())
	require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
	require.Equal(t, "hello", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
}
```

- [ ] **Step 2: Run helper test and verify failure**

Run:

```bash
cd backend && go test ./internal/service -run 'TestForwardProviderResponsesViaChat' -count=1
```

Expected: FAIL with undefined helper symbols.

- [ ] **Step 3: Implement helper**

Create `backend/internal/service/provider_responses_chat.go`:

```go
package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

type providerResponsesChatConfig struct {
	ServiceName               string
	HTTPClient                *http.Client
	Account                   *Account
	Body                      []byte
	BuildRequest              func(context.Context, *gin.Context, *Account, []byte) (*http.Request, string, string, error)
	ShouldReturnUpstreamError func(int) bool
	ReadErrorBody             func(io.Reader) ([]byte, error)
	ParseUsage                func([]byte) *ClaudeUsage
	ParseStreamingUsage       func(string, *ClaudeUsage)
}

func forwardProviderResponsesViaChat(ctx context.Context, c *gin.Context, cfg providerResponsesChatConfig) (*ForwardResult, error) {
	start := time.Now()
	if err := ValidateProviderResponsesCompatibilityRequest(requestPathForResponsesValidation(c), cfg.Body); err != nil {
		writeProviderResponsesCompatibilityError(c, err)
		return nil, err
	}
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(cfg.Body, &responsesReq); err != nil {
		return nil, fmt.Errorf("parse responses request: %w", err)
	}
	chatReq, err := apicompat.ResponsesToChatCompletionsRequest(&responsesReq)
	if err != nil {
		return nil, fmt.Errorf("convert responses to chat completions: %w", err)
	}
	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions request: %w", err)
	}
	upstreamReq, originalModel, upstreamModel, err := cfg.BuildRequest(ctx, c, cfg.Account, chatBody)
	if err != nil {
		return nil, err
	}
	resp, err := cfg.HTTPClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("%s upstream request failed: %w", cfg.ServiceName, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if cfg.ShouldReturnUpstreamError(resp.StatusCode) {
		body, readErr := cfg.ReadErrorBody(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: body, ResponseHeaders: resp.Header.Clone()}
	}
	reasoningEffort := ExtractResponsesReasoningEffortFromBody(cfg.Body)
	if responsesReq.Stream {
		return handleProviderResponsesChatStreaming(resp, c, originalModel, upstreamModel, reasoningEffort, start, cfg.ParseStreamingUsage)
	}
	return handleProviderResponsesChatBuffered(resp, c, originalModel, upstreamModel, reasoningEffort, start, cfg.ParseUsage)
}
```

Implement `handleProviderResponsesChatBuffered` by reading the Chat JSON body, parsing usage with `cfg.ParseUsage`, converting to `apicompat.ChatCompletionsToResponsesResponse`, and writing JSON with `object:"response"`. Implement `handleProviderResponsesChatStreaming` by reading SSE `data:` chunks, parsing Chat chunks, converting each chunk with `apicompat.ChatChunkToResponsesEvents`, emitting `apicompat.ResponsesEventToSSE`, and finalizing with `apicompat.FinalizeChatCompletionsResponsesStream`.

- [ ] **Step 4: Run helper test**

Run:

```bash
cd backend && go test ./internal/service -run 'TestForwardProviderResponsesViaChat' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/provider_responses_chat.go backend/internal/service/provider_responses_chat_test.go
git commit -m "feat(gateway): add chat provider responses bridge"
```

### Task 9: Implement DeepSeek Responses

**Files:**
- Modify: `backend/internal/handler/deepseek_gateway_handler.go`
- Modify: `backend/internal/handler/deepseek_gateway_handler_test.go`
- Modify: `backend/internal/service/deepseek_gateway_service.go`
- Modify: `backend/internal/service/deepseek_gateway_service_test.go`

- [ ] **Step 1: Add DeepSeek service tests**

Append to `backend/internal/service/deepseek_gateway_service_test.go`:

```go
func TestDeepSeekGatewayServiceForwardResponsesUsesChatEndpointAndWritesResponses(t *testing.T) {
	var captured *http.Request
	var capturedBody []byte
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"deepseek-resp-1"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chat_1","model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13}}`)),
		}, nil
	})}
	svc := NewDeepSeekGatewayService(client, nil)
	c, rec := newDeepSeekGatewayTestContext("/v1/responses")
	body := []byte(`{"model":"deepseek-chat","input":"hello","reasoning":{"effort":"medium"}}`)

	result, err := svc.ForwardResponses(context.Background(), c, deepSeekGatewayTestAccount(), body, "req-responses")

	require.NoError(t, err)
	require.Equal(t, "https://api.deepseek.com/chat/completions", captured.URL.String())
	require.Equal(t, "deepseek-v4-flash", gjson.GetBytes(capturedBody, "model").String())
	require.Equal(t, "medium", gjson.GetBytes(capturedBody, "reasoning_effort").String())
	require.Equal(t, "deepseek-resp-1", result.RequestID)
	require.Equal(t, "deepseek-chat", result.Model)
	require.Equal(t, "deepseek-v4-flash", result.UpstreamModel)
	require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
	require.Equal(t, "hello", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
}
```

- [ ] **Step 2: Add DeepSeek handler tests**

Update `fakeDeepSeekGatewayService` and `fakeDeepSeekForwarder` in `backend/internal/handler/deepseek_gateway_handler_test.go` to include `ForwardResponses`, then add:

```go
func TestDeepSeekGatewayHandlerResponsesSuccessForwardsAndRecordsUsage(t *testing.T) {
	forwarder := &fakeDeepSeekForwarder{}
	gateway := &fakeDeepSeekGatewayService{
		selections: []*service.AccountSelectionResult{{Account: deepSeekTestAccount(101), Acquired: true, ReleaseFunc: func() {}}},
	}
	h := &DeepSeekGatewayHandler{
		deepSeekService:     forwarder,
		gatewayService:      gateway,
		billingCacheService: &fakeDeepSeekBillingChecker{},
		concurrencyHelper:   &fakeDeepSeekConcurrencyController{allowWait: true},
		maxAccountSwitches:  3,
	}
	c, rec, _ := newDeepSeekHandlerTestContextForPath(t, "/v1/responses", service.PlatformDeepSeek, `{"model":"deepseek-chat","input":"hello"}`)

	h.Responses(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, forwarder.responsesCalled)
	require.Equal(t, "/v1/responses", gateway.recorded.InboundEndpoint)
	require.Equal(t, "/v1/responses", gateway.recorded.UpstreamEndpoint)
}
```

- [ ] **Step 3: Run DeepSeek tests and verify failure**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler -run 'TestDeepSeek.*Responses|TestDeepSeekGatewayServiceForwardResponses' -count=1
```

Expected: FAIL with missing `ForwardResponses` and `Responses`.

- [ ] **Step 4: Implement DeepSeek service and handler**

In `backend/internal/service/deepseek_gateway_service.go`, add:

```go
func (s *DeepSeekGatewayService) ForwardResponses(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("deepseek gateway service unavailable")
	}
	if _, err := validateDeepSeekAccount(account); err != nil {
		return nil, err
	}
	return forwardProviderResponsesViaChat(ctx, c, providerResponsesChatConfig{
		ServiceName:               "deepseek",
		HTTPClient:                s.httpClient,
		Account:                   account,
		Body:                      body,
		BuildRequest:              s.buildChatCompletionsRequest,
		ShouldReturnUpstreamError: shouldReturnDeepSeekUpstreamError,
		ReadErrorBody:             readGLMNonStreamResponseBody,
		ParseUsage:                parseDeepSeekOpenAIUsage,
		ParseStreamingUsage:       parseDeepSeekOpenAIStreamingUsage,
	})
}
```

In `backend/internal/handler/deepseek_gateway_handler.go`, add `ForwardResponses` to the forwarder interface. Add `Responses(c)` by copying the existing `ChatCompletions(c)` control flow, changing the parser call to `service.ParseGatewayRequest(body, "responses")`, calling `ValidateProviderResponsesCompatibilityRequest(c.Request.URL.Path, body)` before concurrency acquisition, and calling `h.deepSeekService.ForwardResponses(...)` inside the failover loop.

- [ ] **Step 5: Run DeepSeek tests**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler -run 'TestDeepSeek.*Responses|TestDeepSeekGatewayServiceForwardResponses' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/deepseek_gateway_service.go backend/internal/service/deepseek_gateway_service_test.go backend/internal/handler/deepseek_gateway_handler.go backend/internal/handler/deepseek_gateway_handler_test.go
git commit -m "feat(deepseek): support responses compatibility"
```

### Task 10: Implement Windsurf Responses

**Files:**
- Modify: `backend/internal/handler/windsurf_gateway_handler.go`
- Modify: `backend/internal/handler/windsurf_gateway_handler_test.go`
- Modify: `backend/internal/service/windsurf_gateway_service.go`
- Modify: `backend/internal/service/windsurf_gateway_service_test.go`

- [ ] **Step 1: Add Windsurf tests**

Add `TestWindsurfGatewayServiceForwardResponsesUsesChatEndpointAndWritesResponses` to the Windsurf service test file. Use a non-streaming Chat response body with `choices[0].message.content:"hello"`, capture the upstream request in `captured *http.Request`, and assert:

```go
require.Equal(t, "https://windsurf.example/v1/chat/completions", captured.URL.String())
require.Equal(t, "claude-sonnet-4-6", gjson.GetBytes(capturedBody, "model").String())
require.Equal(t, "claude-sonnet-4.6", result.Model)
require.Equal(t, "claude-sonnet-4-6", result.UpstreamModel)
require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
```

Use a Windsurf test account with `base_url` set to `https://windsurf.example/v1`.

Add `TestWindsurfGatewayHandlerResponsesSuccessForwardsAndRecordsUsage` to the Windsurf handler test file. Extend the fake forwarder with `responsesCalled int` and `ForwardResponses(...)`, then assert `responsesCalled == 1`, `gateway.recorded.InboundEndpoint == "/v1/responses"`, and `gateway.recorded.UpstreamEndpoint == "/v1/responses"`.

- [ ] **Step 2: Run Windsurf tests and verify failure**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler -run 'TestWindsurf.*Responses|TestWindsurfGatewayServiceForwardResponses' -count=1
```

Expected: FAIL with missing methods.

- [ ] **Step 3: Implement Windsurf service and handler**

In `backend/internal/service/windsurf_gateway_service.go`, add `ForwardResponses` using `forwardProviderResponsesViaChat`, `s.buildChatCompletionsRequest`, `shouldReturnWindsurfUpstreamError`, `readGLMNonStreamResponseBody`, `parseWindsurfOpenAIUsage`, and `parseWindsurfOpenAIStreamingUsage`.

In `backend/internal/handler/windsurf_gateway_handler.go`, add `ForwardResponses` to the forwarder interface. Add `Responses(c)` by copying the existing `ChatCompletions(c)` control flow, changing the parser call to `service.ParseGatewayRequest(body, "responses")`, calling `ValidateProviderResponsesCompatibilityRequest(c.Request.URL.Path, body)` before concurrency acquisition, and calling `h.windsurfService.ForwardResponses(...)` inside the failover loop.

- [ ] **Step 4: Run Windsurf tests**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler -run 'TestWindsurf.*Responses|TestWindsurfGatewayServiceForwardResponses' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/windsurf_gateway_service.go backend/internal/service/windsurf_gateway_service_test.go backend/internal/handler/windsurf_gateway_handler.go backend/internal/handler/windsurf_gateway_handler_test.go
git commit -m "feat(windsurf): support responses compatibility"
```

### Task 11: Add Cross-Provider Codex Compatibility Coverage

**Files:**
- Create: `backend/internal/service/provider_responses_codex_compat_test.go`

- [ ] **Step 1: Add Codex request-shape tests**

Create `backend/internal/service/provider_responses_codex_compat_test.go`:

```go
package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderResponsesCompatibilityAllowsCodexCommonShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "string_input", body: `{"model":"glm-5.1","input":"hello"}`},
		{name: "message_array_input", body: `{"model":"glm-5.1","input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]}`},
		{name: "instructions", body: `{"model":"glm-5.1","instructions":"be brief","input":"hello"}`},
		{name: "function_tool", body: `{"model":"glm-5.1","input":"hello","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"tool_choice":"auto"}`},
		{name: "reasoning_effort", body: `{"model":"glm-5.1","input":"hello","reasoning":{"effort":"high","summary":"auto"}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(tc.body))
			require.NoError(t, err)
		})
	}
}

func TestProviderResponsesCompatibilityRejectsUnsupportedCodexShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "previous_response_id", body: `{"model":"glm-5.1","previous_response_id":"resp_1","input":[{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`, want: "previous_response_id"},
		{name: "image_tool", body: `{"model":"glm-5.1","input":"draw","tools":[{"type":"image_generation"}]}`, want: "image_generation"},
		{name: "web_search", body: `{"model":"glm-5.1","input":"search","tools":[{"type":"web_search"}]}`, want: "web_search"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(tc.body))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
```

- [ ] **Step 2: Run compatibility tests**

Run:

```bash
cd backend && go test ./internal/service -run 'TestProviderResponsesCompatibility.*Codex' -count=1
```

Expected: PASS.

- [ ] **Step 3: Run focused provider test suite**

Run:

```bash
cd backend && go test ./internal/pkg/apicompat ./internal/service ./internal/handler ./internal/server/routes -run 'Responses|responses|ProviderResponses|ChatCompletionsToResponses|ResponsesToChatCompletions' -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/service/provider_responses_codex_compat_test.go
git commit -m "test(gateway): cover codex provider responses compatibility"
```

### Task 12: Final Verification And Documentation

**Files:**
- Inspect: `README.md`
- Inspect: `README_CN.md`
- Inspect: `deploy/config.example.yaml`
- Inspect: `frontend/src/components/keys/UseKeyModal.vue`

- [ ] **Step 1: Search for endpoint support docs**

Run:

```bash
rg -n "responses|/v1/responses|/v1/chat/completions|/v1/messages|MiniMax|GLM|Kimi|DeepSeek|Windsurf|OpenCode" README.md README_CN.md deploy docs frontend/src -g '*.{md,yaml,ts,vue}'
```

Expected: no provider endpoint support table in `README.md`, `README_CN.md`, or `deploy/config.example.yaml`. `frontend/src/components/keys/UseKeyModal.vue` may contain client configuration snippets, but it does not need a change unless implementation changes generated endpoint URLs.

- [ ] **Step 2: Record documentation decision**

When the search output matches the expected result, leave README and deploy files unchanged and note this in the implementation summary:

```text
No endpoint support table exists in README/deploy docs, so no documentation file was changed.
```

When the search output shows an existing provider endpoint support table, pause execution and update this plan with the exact file and line-level documentation change before editing docs.

- [ ] **Step 3: Run full backend tests for touched packages**

Run:

```bash
cd backend && go test ./internal/pkg/apicompat ./internal/service ./internal/handler ./internal/server/routes -count=1
```

Expected: PASS.

- [ ] **Step 4: Run frontend tests when frontend files changed**

Run this command when `git status --short` shows any `frontend/` file:

```bash
cd frontend && pnpm test -- --run
```

Expected: PASS.

- [ ] **Step 5: Inspect git diff**

Run:

```bash
git status --short
git diff --stat
git diff --check
```

Expected: only planned files changed; `git diff --check` produces no whitespace errors.

- [ ] **Step 6: Commit final docs or cleanup**

If docs changed:

```bash
git add README.md README_CN.md deploy/config.example.yaml docs frontend/src
git commit -m "docs: document provider responses compatibility"
```

If no docs changed, do not create an empty commit.

## Self-Review Checklist

- Spec coverage: Tasks 1-2 cover missing conversion primitives; Task 3 covers shared validation and unsupported features; Task 4 covers route contract; Tasks 5-10 cover all five provider services and handlers; Task 11 covers Codex common request shapes; Task 12 covers docs and verification.
- Unsupported features remain explicit: compact routes, WebSocket GET, images, built-in tools, and HTTP `previous_response_id` all have validation or route-test coverage.
- OpenCode remains unchanged: no task modifies `backend/internal/service/opencode_gateway_service.go` or `backend/internal/handler/opencode_gateway_handler.go`.
- Test commands use the `backend/` Go module path.
