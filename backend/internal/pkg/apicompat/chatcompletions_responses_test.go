package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ChatCompletionsToResponses tests
// ---------------------------------------------------------------------------

func TestChatCompletionsToResponses_BasicText(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o", resp.Model)
	assert.True(t, resp.Stream) // always forced true
	assert.False(t, *resp.Store)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 1)
	assert.Equal(t, "user", items[0].Role)
}

func TestChatCompletionsToResponses_SystemMessage(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "system", Content: json.RawMessage(`"You are helpful."`)},
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 2)
	assert.Equal(t, "system", items[0].Role)
	assert.Equal(t, "user", items[1].Role)
}

func TestChatCompletionsToResponses_ToolCalls(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Call the function"`)},
			{
				Role: "assistant",
				ToolCalls: []ChatToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: ChatFunctionCall{
							Name:      "ping",
							Arguments: `{"host":"example.com"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: "call_1",
				Content:    json.RawMessage(`"pong"`),
			},
		},
		Tools: []ChatTool{
			{
				Type: "function",
				Function: &ChatFunction{
					Name:        "ping",
					Description: "Ping a host",
					Parameters:  json.RawMessage(`{"type":"object"}`),
				},
			},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	// user + function_call + function_call_output = 3
	// (assistant message with empty content + tool_calls → only function_call items emitted)
	require.Len(t, items, 3)

	// Check function_call item
	assert.Equal(t, "function_call", items[1].Type)
	assert.Equal(t, "call_1", items[1].CallID)
	assert.Empty(t, items[1].ID)
	assert.Equal(t, "ping", items[1].Name)

	// Check function_call_output item
	assert.Equal(t, "function_call_output", items[2].Type)
	assert.Equal(t, "call_1", items[2].CallID)
	assert.Equal(t, "pong", items[2].Output)

	// Check tools
	require.Len(t, resp.Tools, 1)
	assert.Equal(t, "function", resp.Tools[0].Type)
	assert.Equal(t, "ping", resp.Tools[0].Name)
}

func TestChatCompletionsToResponses_MaxTokens(t *testing.T) {
	t.Run("max_tokens", func(t *testing.T) {
		maxTokens := 100
		req := &ChatCompletionsRequest{
			Model:     "gpt-4o",
			MaxTokens: &maxTokens,
			Messages:  []ChatMessage{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		}
		resp, err := ChatCompletionsToResponses(req)
		require.NoError(t, err)
		require.NotNil(t, resp.MaxOutputTokens)
		// Below minMaxOutputTokens (128), should be clamped
		assert.Equal(t, minMaxOutputTokens, *resp.MaxOutputTokens)
	})

	t.Run("max_completion_tokens_preferred", func(t *testing.T) {
		maxTokens := 100
		maxCompletion := 500
		req := &ChatCompletionsRequest{
			Model:               "gpt-4o",
			MaxTokens:           &maxTokens,
			MaxCompletionTokens: &maxCompletion,
			Messages:            []ChatMessage{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		}
		resp, err := ChatCompletionsToResponses(req)
		require.NoError(t, err)
		require.NotNil(t, resp.MaxOutputTokens)
		assert.Equal(t, 500, *resp.MaxOutputTokens)
	})
}

func TestChatCompletionsToResponses_ReasoningEffort(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model:           "gpt-4o",
		ReasoningEffort: "high",
		Messages:        []ChatMessage{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
	}
	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)
	require.NotNil(t, resp.Reasoning)
	assert.Equal(t, "high", resp.Reasoning.Effort)
	assert.Equal(t, "auto", resp.Reasoning.Summary)
}

func TestChatCompletionsToResponses_ImageURL(t *testing.T) {
	content := `[{"type":"text","text":"Describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc123"}}]`
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(content)},
		},
	}
	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 1)

	var parts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[0].Content, &parts))
	require.Len(t, parts, 2)
	assert.Equal(t, "input_text", parts[0].Type)
	assert.Equal(t, "Describe this", parts[0].Text)
	assert.Equal(t, "input_image", parts[1].Type)
	assert.Equal(t, "data:image/png;base64,abc123", parts[1].ImageURL)
}

func TestChatCompletionsToResponses_EmptyBase64ImageURLSkipped(t *testing.T) {
	content := `[{"type":"text","text":"Describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,"}}]`
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(content)},
		},
	}
	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 1)

	var parts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[0].Content, &parts))
	require.Len(t, parts, 1)
	assert.Equal(t, "input_text", parts[0].Type)
	assert.Equal(t, "Describe this", parts[0].Text)
}

func TestChatCompletionsToResponses_WhitespaceOnlyBase64ImageURLSkipped(t *testing.T) {
	content := `[{"type":"text","text":"Describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,   "}}]`
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(content)},
		},
	}
	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 1)

	var parts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[0].Content, &parts))
	require.Len(t, parts, 1)
	assert.Equal(t, "input_text", parts[0].Type)
	assert.Equal(t, "Describe this", parts[0].Text)
}

func TestChatCompletionsToResponses_SystemArrayContent(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "system", Content: json.RawMessage(`[{"type":"text","text":"You are a careful visual assistant."}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"Describe this image"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc123"}}]`)},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 2)

	var systemParts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[0].Content, &systemParts))
	require.Len(t, systemParts, 1)
	assert.Equal(t, "input_text", systemParts[0].Type)
	assert.Equal(t, "You are a careful visual assistant.", systemParts[0].Text)

	var userParts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[1].Content, &userParts))
	require.Len(t, userParts, 2)
	assert.Equal(t, "input_image", userParts[1].Type)
	assert.Equal(t, "data:image/png;base64,abc123", userParts[1].ImageURL)
}

func TestChatCompletionsToResponses_LegacyFunctions(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
		},
		Functions: []ChatFunction{
			{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
		FunctionCall: json.RawMessage(`{"name":"get_weather"}`),
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)
	require.Len(t, resp.Tools, 1)
	assert.Equal(t, "function", resp.Tools[0].Type)
	assert.Equal(t, "get_weather", resp.Tools[0].Name)

	// tool_choice should be converted
	require.NotNil(t, resp.ToolChoice)
	var tc map[string]any
	require.NoError(t, json.Unmarshal(resp.ToolChoice, &tc))
	assert.Equal(t, "function", tc["type"])
	assert.Equal(t, "get_weather", tc["name"])
	assert.NotContains(t, tc, "function")
}

func TestChatCompletionsToResponses_ServiceTier(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model:       "gpt-4o",
		ServiceTier: "flex",
		Messages:    []ChatMessage{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
	}
	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)
	assert.Equal(t, "flex", resp.ServiceTier)
}

func TestChatCompletionsToResponses_AssistantWithTextAndToolCalls(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Do something"`)},
			{
				Role:    "assistant",
				Content: json.RawMessage(`"Let me call a function."`),
				ToolCalls: []ChatToolCall{
					{
						ID:   "call_abc",
						Type: "function",
						Function: ChatFunctionCall{
							Name:      "do_thing",
							Arguments: `{}`,
						},
					},
				},
			},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	// user + assistant message (with text) + function_call
	require.Len(t, items, 3)
	assert.Equal(t, "user", items[0].Role)
	assert.Equal(t, "assistant", items[1].Role)
	assert.Equal(t, "function_call", items[2].Type)
	assert.Empty(t, items[2].ID)
}

func TestChatCompletionsToResponses_AssistantArrayContentPreserved(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"A"},{"type":"text","text":"B"}]`)},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 2)
	assert.Equal(t, "assistant", items[1].Role)

	var parts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[1].Content, &parts))
	require.Len(t, parts, 1)
	assert.Equal(t, "output_text", parts[0].Type)
	assert.Equal(t, "AB", parts[0].Text)
}

func TestChatCompletionsToResponses_AssistantThinkingTagPreserved(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"thinking","thinking":"internal plan"},{"type":"text","text":"final answer"}]`)},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 2)

	var parts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[1].Content, &parts))
	require.Len(t, parts, 1)
	assert.Equal(t, "output_text", parts[0].Type)
	assert.Contains(t, parts[0].Text, "<thinking>internal plan</thinking>")
	assert.Contains(t, parts[0].Text, "final answer")
}

// ---------------------------------------------------------------------------
// ResponsesToChatCompletions tests
// ---------------------------------------------------------------------------

func TestResponsesToChatCompletions_BasicText(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_123",
		Status: "completed",
		Output: []ResponsesOutput{
			{
				Type: "message",
				Content: []ResponsesContentPart{
					{Type: "output_text", Text: "Hello, world!"},
				},
			},
		},
		Usage: &ResponsesUsage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	assert.Equal(t, "chat.completion", chat.Object)
	assert.Equal(t, "gpt-4o", chat.Model)
	require.Len(t, chat.Choices, 1)
	assert.Equal(t, "stop", chat.Choices[0].FinishReason)

	var content string
	require.NoError(t, json.Unmarshal(chat.Choices[0].Message.Content, &content))
	assert.Equal(t, "Hello, world!", content)

	require.NotNil(t, chat.Usage)
	assert.Equal(t, 10, chat.Usage.PromptTokens)
	assert.Equal(t, 5, chat.Usage.CompletionTokens)
	assert.Equal(t, 15, chat.Usage.TotalTokens)
}

func TestResponsesToChatCompletions_ToolCalls(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_456",
		Status: "completed",
		Output: []ResponsesOutput{
			{
				Type:      "function_call",
				CallID:    "call_xyz",
				Name:      "get_weather",
				Arguments: `{"city":"NYC"}`,
			},
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	require.Len(t, chat.Choices, 1)
	assert.Equal(t, "tool_calls", chat.Choices[0].FinishReason)

	msg := chat.Choices[0].Message
	require.Len(t, msg.ToolCalls, 1)
	assert.Equal(t, "call_xyz", msg.ToolCalls[0].ID)
	assert.Equal(t, "function", msg.ToolCalls[0].Type)
	assert.Equal(t, "get_weather", msg.ToolCalls[0].Function.Name)
	assert.Equal(t, `{"city":"NYC"}`, msg.ToolCalls[0].Function.Arguments)
}

func TestResponsesToChatCompletions_Reasoning(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_789",
		Status: "completed",
		Output: []ResponsesOutput{
			{
				Type: "reasoning",
				Summary: []ResponsesSummary{
					{Type: "summary_text", Text: "I thought about it."},
				},
			},
			{
				Type: "message",
				Content: []ResponsesContentPart{
					{Type: "output_text", Text: "The answer is 42."},
				},
			},
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	require.Len(t, chat.Choices, 1)

	var content string
	require.NoError(t, json.Unmarshal(chat.Choices[0].Message.Content, &content))
	assert.Equal(t, "The answer is 42.", content)
	assert.Equal(t, "I thought about it.", chat.Choices[0].Message.ReasoningContent)
}

func TestChatCompletionsToResponses_ToolArrayContent(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Use the tool"`)},
			{
				Role: "assistant",
				ToolCalls: []ChatToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: ChatFunctionCall{
							Name:      "inspect_image",
							Arguments: `{}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: "call_1",
				Content: json.RawMessage(
					`[{"type":"text","text":"image width: 100"},{"type":"image_url","image_url":{"url":"data:image/png;base64,ignored"}},{"type":"text","text":"; image height: 200"}]`,
				),
			},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 3)
	assert.Equal(t, "function_call_output", items[2].Type)
	assert.Equal(t, "call_1", items[2].CallID)
	assert.Equal(t, "image width: 100; image height: 200", items[2].Output)
}

func TestResponsesToChatCompletions_Incomplete(t *testing.T) {
	resp := &ResponsesResponse{
		ID:                "resp_inc",
		Status:            "incomplete",
		IncompleteDetails: &ResponsesIncompleteDetails{Reason: "max_output_tokens"},
		Output: []ResponsesOutput{
			{
				Type: "message",
				Content: []ResponsesContentPart{
					{Type: "output_text", Text: "partial..."},
				},
			},
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	require.Len(t, chat.Choices, 1)
	assert.Equal(t, "length", chat.Choices[0].FinishReason)
}

func TestResponsesToChatCompletions_CachedTokens(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_cache",
		Status: "completed",
		Output: []ResponsesOutput{
			{
				Type:    "message",
				Content: []ResponsesContentPart{{Type: "output_text", Text: "cached"}},
			},
		},
		Usage: &ResponsesUsage{
			InputTokens:  100,
			OutputTokens: 10,
			TotalTokens:  110,
			InputTokensDetails: &ResponsesInputTokensDetails{
				CachedTokens: 80,
			},
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	require.NotNil(t, chat.Usage)
	require.NotNil(t, chat.Usage.PromptTokensDetails)
	assert.Equal(t, 80, chat.Usage.PromptTokensDetails.CachedTokens)
}

func TestResponsesToChatCompletions_WebSearch(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_ws",
		Status: "completed",
		Output: []ResponsesOutput{
			{
				Type:   "web_search_call",
				Action: &WebSearchAction{Type: "search", Query: "test"},
			},
			{
				Type:    "message",
				Content: []ResponsesContentPart{{Type: "output_text", Text: "search results"}},
			},
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	require.Len(t, chat.Choices, 1)
	assert.Equal(t, "stop", chat.Choices[0].FinishReason)

	var content string
	require.NoError(t, json.Unmarshal(chat.Choices[0].Message.Content, &content))
	assert.Equal(t, "search results", content)
}

// ---------------------------------------------------------------------------
// Streaming: ResponsesEventToChatChunks tests
// ---------------------------------------------------------------------------

func TestResponsesEventToChatChunks_TextDelta(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"

	// response.created → role chunk
	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.created",
		Response: &ResponsesResponse{
			ID: "resp_stream",
		},
	}, state)
	require.Len(t, chunks, 1)
	assert.Equal(t, "assistant", chunks[0].Choices[0].Delta.Role)
	assert.True(t, state.SentRole)

	// response.output_text.delta → content chunk
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:  "response.output_text.delta",
		Delta: "Hello",
	}, state)
	require.Len(t, chunks, 1)
	require.NotNil(t, chunks[0].Choices[0].Delta.Content)
	assert.Equal(t, "Hello", *chunks[0].Choices[0].Delta.Content)
}

func TestResponsesEventToChatChunks_ToolCallDelta(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.SentRole = true

	// response.output_item.added (function_call) — output_index=1 (e.g. after a message item at 0)
	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 1,
		Item: &ResponsesOutput{
			Type:   "function_call",
			CallID: "call_1",
			Name:   "get_weather",
		},
	}, state)
	require.Len(t, chunks, 1)
	require.Len(t, chunks[0].Choices[0].Delta.ToolCalls, 1)
	tc := chunks[0].Choices[0].Delta.ToolCalls[0]
	assert.Equal(t, "call_1", tc.ID)
	assert.Equal(t, "get_weather", tc.Function.Name)
	require.NotNil(t, tc.Index)
	assert.Equal(t, 0, *tc.Index)

	// response.function_call_arguments.delta — uses output_index (NOT call_id) to find tool
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 1, // matches the output_index from output_item.added above
		Delta:       `{"city":`,
	}, state)
	require.Len(t, chunks, 1)
	tc = chunks[0].Choices[0].Delta.ToolCalls[0]
	require.NotNil(t, tc.Index)
	assert.Equal(t, 0, *tc.Index, "argument delta must use same index as the tool call")
	assert.Equal(t, `{"city":`, tc.Function.Arguments)

	// Add a second function call at output_index=2
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 2,
		Item: &ResponsesOutput{
			Type:   "function_call",
			CallID: "call_2",
			Name:   "get_time",
		},
	}, state)
	require.Len(t, chunks, 1)
	tc = chunks[0].Choices[0].Delta.ToolCalls[0]
	require.NotNil(t, tc.Index)
	assert.Equal(t, 1, *tc.Index, "second tool call should get index 1")

	// Argument delta for second tool call
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 2,
		Delta:       `{"tz":"UTC"}`,
	}, state)
	require.Len(t, chunks, 1)
	tc = chunks[0].Choices[0].Delta.ToolCalls[0]
	require.NotNil(t, tc.Index)
	assert.Equal(t, 1, *tc.Index, "second tool arg delta must use index 1")

	// Argument delta for first tool call (interleaved)
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 1,
		Delta:       `"Tokyo"}`,
	}, state)
	require.Len(t, chunks, 1)
	tc = chunks[0].Choices[0].Delta.ToolCalls[0]
	require.NotNil(t, tc.Index)
	assert.Equal(t, 0, *tc.Index, "first tool arg delta must still use index 0")
}

func TestResponsesEventToChatChunks_Completed(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.IncludeUsage = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{
			Status: "completed",
			Usage: &ResponsesUsage{
				InputTokens:  50,
				OutputTokens: 20,
				TotalTokens:  70,
				InputTokensDetails: &ResponsesInputTokensDetails{
					CachedTokens: 30,
				},
			},
		},
	}, state)
	// finish chunk + usage chunk
	require.Len(t, chunks, 2)

	// First chunk: finish_reason
	require.NotNil(t, chunks[0].Choices[0].FinishReason)
	assert.Equal(t, "stop", *chunks[0].Choices[0].FinishReason)

	// Second chunk: usage
	require.NotNil(t, chunks[1].Usage)
	assert.Equal(t, 50, chunks[1].Usage.PromptTokens)
	assert.Equal(t, 20, chunks[1].Usage.CompletionTokens)
	assert.Equal(t, 70, chunks[1].Usage.TotalTokens)
	require.NotNil(t, chunks[1].Usage.PromptTokensDetails)
	assert.Equal(t, 30, chunks[1].Usage.PromptTokensDetails.CachedTokens)
}

func TestResponsesEventToChatChunks_ResponseDone(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.IncludeUsage = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.done",
		Response: &ResponsesResponse{
			Status: "completed",
			Usage:  &ResponsesUsage{InputTokens: 13, OutputTokens: 7},
		},
	}, state)
	require.Len(t, chunks, 2)
	require.NotNil(t, chunks[0].Choices[0].FinishReason)
	assert.Equal(t, "stop", *chunks[0].Choices[0].FinishReason)
	require.NotNil(t, chunks[1].Usage)
	assert.Equal(t, 13, chunks[1].Usage.PromptTokens)
	assert.Equal(t, 7, chunks[1].Usage.CompletionTokens)
	assert.Nil(t, FinalizeResponsesChatStream(state))
}

func TestResponsesEventToChatChunks_ResponseDoneIncomplete(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.IncludeUsage = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.done",
		Response: &ResponsesResponse{
			Status:            "incomplete",
			IncompleteDetails: &ResponsesIncompleteDetails{Reason: "max_output_tokens"},
			Usage:             &ResponsesUsage{InputTokens: 13, OutputTokens: 7},
		},
	}, state)
	require.Len(t, chunks, 2)
	require.NotNil(t, chunks[0].Choices[0].FinishReason)
	assert.Equal(t, "length", *chunks[0].Choices[0].FinishReason)
	require.NotNil(t, chunks[1].Usage)
	assert.Equal(t, 13, chunks[1].Usage.PromptTokens)
	assert.Equal(t, 7, chunks[1].Usage.CompletionTokens)
	assert.Nil(t, FinalizeResponsesChatStream(state))
}

func TestResponsesEventToChatChunks_CompletedWithToolCalls(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.SawToolCall = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{
			Status: "completed",
		},
	}, state)
	require.Len(t, chunks, 1)
	require.NotNil(t, chunks[0].Choices[0].FinishReason)
	assert.Equal(t, "tool_calls", *chunks[0].Choices[0].FinishReason)
}

func TestResponsesEventToChatChunks_ReasoningDelta(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.SentRole = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:  "response.reasoning_summary_text.delta",
		Delta: "Thinking...",
	}, state)
	require.Len(t, chunks, 1)
	require.NotNil(t, chunks[0].Choices[0].Delta.ReasoningContent)
	assert.Equal(t, "Thinking...", *chunks[0].Choices[0].Delta.ReasoningContent)

	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.reasoning_summary_text.done",
	}, state)
	require.Len(t, chunks, 0)
}

func TestResponsesEventToChatChunks_ReasoningThenTextAutoCloseTag(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.SentRole = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:  "response.reasoning_summary_text.delta",
		Delta: "plan",
	}, state)
	require.Len(t, chunks, 1)
	require.NotNil(t, chunks[0].Choices[0].Delta.ReasoningContent)
	assert.Equal(t, "plan", *chunks[0].Choices[0].Delta.ReasoningContent)

	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:  "response.output_text.delta",
		Delta: "answer",
	}, state)
	require.Len(t, chunks, 1)
	require.NotNil(t, chunks[0].Choices[0].Delta.Content)
	assert.Equal(t, "answer", *chunks[0].Choices[0].Delta.Content)
}

func TestFinalizeResponsesChatStream(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.IncludeUsage = true
	state.Usage = &ChatUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	chunks := FinalizeResponsesChatStream(state)
	require.Len(t, chunks, 2)

	// Finish chunk
	require.NotNil(t, chunks[0].Choices[0].FinishReason)
	assert.Equal(t, "stop", *chunks[0].Choices[0].FinishReason)

	// Usage chunk
	require.NotNil(t, chunks[1].Usage)
	assert.Equal(t, 100, chunks[1].Usage.PromptTokens)

	// Idempotent: second call returns nil
	assert.Nil(t, FinalizeResponsesChatStream(state))
}

func TestFinalizeResponsesChatStream_AfterCompleted(t *testing.T) {
	// If response.completed already emitted the finish chunk, FinalizeResponsesChatStream
	// must be a no-op (prevents double finish_reason being sent to the client).
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.IncludeUsage = true

	// Simulate response.completed
	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{
			Status: "completed",
			Usage: &ResponsesUsage{
				InputTokens:  10,
				OutputTokens: 5,
				TotalTokens:  15,
			},
		},
	}, state)
	require.NotEmpty(t, chunks) // finish + usage chunks

	// Now FinalizeResponsesChatStream should return nil — already finalized.
	assert.Nil(t, FinalizeResponsesChatStream(state))
}

func TestChatChunkToSSE(t *testing.T) {
	chunk := ChatCompletionsChunk{
		ID:      "chatcmpl-test",
		Object:  "chat.completion.chunk",
		Created: 1700000000,
		Model:   "gpt-4o",
		Choices: []ChatChunkChoice{
			{
				Index:        0,
				Delta:        ChatDelta{Role: "assistant"},
				FinishReason: nil,
			},
		},
	}

	sse, err := ChatChunkToSSE(chunk)
	require.NoError(t, err)
	assert.Contains(t, sse, "data: ")
	assert.Contains(t, sse, "chatcmpl-test")
	assert.Contains(t, sse, "assistant")
	assert.True(t, len(sse) > 10)
}

// ---------------------------------------------------------------------------
// Stream round-trip test
// ---------------------------------------------------------------------------

func TestChatCompletionsStreamRoundTrip(t *testing.T) {
	// Simulate: client sends chat completions request, upstream returns Responses SSE events.
	// Verify that the streaming state machine produces correct chat completions chunks.

	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.IncludeUsage = true

	var allChunks []ChatCompletionsChunk

	// 1. response.created
	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:     "response.created",
		Response: &ResponsesResponse{ID: "resp_rt"},
	}, state)
	allChunks = append(allChunks, chunks...)

	// 2. text deltas
	for _, text := range []string{"Hello", ", ", "world", "!"} {
		chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
			Type:  "response.output_text.delta",
			Delta: text,
		}, state)
		allChunks = append(allChunks, chunks...)
	}

	// 3. response.completed
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{
			Status: "completed",
			Usage: &ResponsesUsage{
				InputTokens:  10,
				OutputTokens: 4,
				TotalTokens:  14,
			},
		},
	}, state)
	allChunks = append(allChunks, chunks...)

	// Verify: role chunk + 4 text chunks + finish chunk + usage chunk = 7
	require.Len(t, allChunks, 7)

	// First chunk has role
	assert.Equal(t, "assistant", allChunks[0].Choices[0].Delta.Role)

	// Text chunks
	var fullText string
	for i := 1; i <= 4; i++ {
		require.NotNil(t, allChunks[i].Choices[0].Delta.Content)
		fullText += *allChunks[i].Choices[0].Delta.Content
	}
	assert.Equal(t, "Hello, world!", fullText)

	// Finish chunk
	require.NotNil(t, allChunks[5].Choices[0].FinishReason)
	assert.Equal(t, "stop", *allChunks[5].Choices[0].FinishReason)

	// Usage chunk
	require.NotNil(t, allChunks[6].Usage)
	assert.Equal(t, 10, allChunks[6].Usage.PromptTokens)
	assert.Equal(t, 4, allChunks[6].Usage.CompletionTokens)

	// All chunks share the same ID
	for _, c := range allChunks {
		assert.Equal(t, "resp_rt", c.ID)
	}
}

// ---------------------------------------------------------------------------
// BufferedResponseAccumulator tests
// ---------------------------------------------------------------------------

func TestBufferedResponseAccumulator_TextOnly(t *testing.T) {
	acc := NewBufferedResponseAccumulator()

	acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.output_text.delta", Delta: "Hello"})
	acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.output_text.delta", Delta: ", world!"})

	assert.True(t, acc.HasContent())

	output := acc.BuildOutput()
	require.Len(t, output, 1)
	assert.Equal(t, "message", output[0].Type)
	assert.Equal(t, "assistant", output[0].Role)
	require.Len(t, output[0].Content, 1)
	assert.Equal(t, "output_text", output[0].Content[0].Type)
	assert.Equal(t, "Hello, world!", output[0].Content[0].Text)
}

func TestBufferedResponseAccumulator_ToolCalls(t *testing.T) {
	acc := NewBufferedResponseAccumulator()

	// Add function call at output_index=1
	acc.ProcessEvent(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 1,
		Item: &ResponsesOutput{
			Type:   "function_call",
			CallID: "call_abc",
			Name:   "get_weather",
		},
	})
	acc.ProcessEvent(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 1,
		Delta:       `{"city":`,
	})
	acc.ProcessEvent(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 1,
		Delta:       `"NYC"}`,
	})

	assert.True(t, acc.HasContent())

	output := acc.BuildOutput()
	require.Len(t, output, 1)
	assert.Equal(t, "function_call", output[0].Type)
	assert.Equal(t, "call_abc", output[0].CallID)
	assert.Equal(t, "get_weather", output[0].Name)
	assert.Equal(t, `{"city":"NYC"}`, output[0].Arguments)
}

func TestBufferedResponseAccumulator_Reasoning(t *testing.T) {
	acc := NewBufferedResponseAccumulator()

	acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.reasoning_summary_text.delta", Delta: "Step 1: "})
	acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.reasoning_summary_text.delta", Delta: "think about it"})

	assert.True(t, acc.HasContent())

	output := acc.BuildOutput()
	require.Len(t, output, 1)
	assert.Equal(t, "reasoning", output[0].Type)
	require.Len(t, output[0].Summary, 1)
	assert.Equal(t, "summary_text", output[0].Summary[0].Type)
	assert.Equal(t, "Step 1: think about it", output[0].Summary[0].Text)
}

func TestBufferedResponseAccumulator_Mixed(t *testing.T) {
	acc := NewBufferedResponseAccumulator()

	// Reasoning first
	acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.reasoning_summary_text.delta", Delta: "I thought about it."})

	// Then text
	acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.output_text.delta", Delta: "The answer is 42."})

	// Then a tool call
	acc.ProcessEvent(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 2,
		Item: &ResponsesOutput{
			Type:   "function_call",
			CallID: "call_1",
			Name:   "verify",
		},
	})
	acc.ProcessEvent(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 2,
		Delta:       `{}`,
	})

	assert.True(t, acc.HasContent())

	output := acc.BuildOutput()
	// Order: reasoning → message → function_calls
	require.Len(t, output, 3)
	assert.Equal(t, "reasoning", output[0].Type)
	assert.Equal(t, "message", output[1].Type)
	assert.Equal(t, "function_call", output[2].Type)
	assert.Equal(t, "The answer is 42.", output[1].Content[0].Text)
	assert.Equal(t, "verify", output[2].Name)
}

func TestBufferedResponseAccumulator_SupplementEmptyOutput(t *testing.T) {
	acc := NewBufferedResponseAccumulator()
	acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.output_text.delta", Delta: "Hello"})

	resp := &ResponsesResponse{
		ID:     "resp_1",
		Status: "completed",
		Output: nil, // empty output
		Usage:  &ResponsesUsage{InputTokens: 10, OutputTokens: 5},
	}

	acc.SupplementResponseOutput(resp)

	require.Len(t, resp.Output, 1)
	assert.Equal(t, "message", resp.Output[0].Type)
	assert.Equal(t, "Hello", resp.Output[0].Content[0].Text)
	// Usage should be untouched
	assert.Equal(t, 10, resp.Usage.InputTokens)
}

func TestBufferedResponseAccumulator_NoSupplementWhenOutputExists(t *testing.T) {
	acc := NewBufferedResponseAccumulator()
	acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.output_text.delta", Delta: "from deltas"})

	resp := &ResponsesResponse{
		ID:     "resp_2",
		Status: "completed",
		Output: []ResponsesOutput{
			{
				Type: "message",
				Content: []ResponsesContentPart{
					{Type: "output_text", Text: "from terminal event"},
				},
			},
		},
	}

	acc.SupplementResponseOutput(resp)

	// Output should NOT be overwritten
	require.Len(t, resp.Output, 1)
	assert.Equal(t, "from terminal event", resp.Output[0].Content[0].Text)
}

func TestBufferedResponseAccumulator_EmptyDeltas(t *testing.T) {
	acc := NewBufferedResponseAccumulator()

	// Process events with empty delta — should not accumulate
	acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.output_text.delta", Delta: ""})
	acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.created"})

	assert.False(t, acc.HasContent())

	resp := &ResponsesResponse{ID: "resp_3", Status: "completed"}
	acc.SupplementResponseOutput(resp)
	assert.Nil(t, resp.Output)
}

func TestBufferedResponseAccumulator_IgnoresNonFunctionCallItems(t *testing.T) {
	acc := NewBufferedResponseAccumulator()

	// output_item.added with type "message" should be ignored
	acc.ProcessEvent(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item:        &ResponsesOutput{Type: "message"},
	})

	assert.False(t, acc.HasContent())
}

// ---------------------------------------------------------------------------
// ResponsesToChatCompletionsRequest tests
// ---------------------------------------------------------------------------

func TestResponsesToChatCompletionsRequest_StringInput(t *testing.T) {
	maxOutputTokens := 512
	temperature := 0.25
	topP := 0.9
	req := &ResponsesRequest{
		Model:           "gpt-5.2",
		Instructions:    "Answer precisely.",
		Input:           json.RawMessage(`"Hello"`),
		MaxOutputTokens: &maxOutputTokens,
		Temperature:     &temperature,
		TopP:            &topP,
		Stream:          true,
		Reasoning:       &ResponsesReasoning{Effort: " high "},
		ServiceTier:     "priority",
	}

	chat, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	assert.Equal(t, "gpt-5.2", chat.Model)
	assert.Equal(t, &maxOutputTokens, chat.MaxCompletionTokens)
	assert.Equal(t, &temperature, chat.Temperature)
	assert.Equal(t, &topP, chat.TopP)
	assert.True(t, chat.Stream)
	assert.Equal(t, "high", chat.ReasoningEffort)
	assert.Equal(t, "priority", chat.ServiceTier)

	require.Len(t, chat.Messages, 2)
	assert.Equal(t, "system", chat.Messages[0].Role)
	assert.JSONEq(t, `"Answer precisely."`, string(chat.Messages[0].Content))
	assert.Equal(t, "user", chat.Messages[1].Role)
	assert.JSONEq(t, `"Hello"`, string(chat.Messages[1].Content))
}

func TestResponsesToChatCompletionsRequest_MessageArrayWithTools(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-5.2",
		Input: json.RawMessage(`[
			{"role":"user","content":[{"type":"input_text","text":"Use the tool"}]},
			{"role":"user","content":[{"type":"input_text","text":"City: "},{"type":"input_text","text":"Paris"}]},
			{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"city\":\"Paris\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"sunny"}
		]`),
		Tools: []ResponsesTool{
			{
				Type:        "function",
				Name:        "lookup",
				Description: "Look up weather",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
		ToolChoice: json.RawMessage(`{"type":"function","name":"lookup"}`),
	}

	chat, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, chat.Messages, 4)

	assert.Equal(t, "user", chat.Messages[0].Role)
	assert.JSONEq(t, `"Use the tool"`, string(chat.Messages[0].Content))

	assert.Equal(t, "user", chat.Messages[1].Role)
	var parts []ChatContentPart
	require.NoError(t, json.Unmarshal(chat.Messages[1].Content, &parts))
	require.Len(t, parts, 2)
	assert.Equal(t, ChatContentPart{Type: "text", Text: "City: "}, parts[0])
	assert.Equal(t, ChatContentPart{Type: "text", Text: "Paris"}, parts[1])

	assert.Equal(t, "assistant", chat.Messages[2].Role)
	require.Len(t, chat.Messages[2].ToolCalls, 1)
	assert.Equal(t, "call_1", chat.Messages[2].ToolCalls[0].ID)
	assert.Equal(t, "function", chat.Messages[2].ToolCalls[0].Type)
	assert.Equal(t, "lookup", chat.Messages[2].ToolCalls[0].Function.Name)
	assert.Equal(t, `{"city":"Paris"}`, chat.Messages[2].ToolCalls[0].Function.Arguments)

	assert.Equal(t, "tool", chat.Messages[3].Role)
	assert.Equal(t, "call_1", chat.Messages[3].ToolCallID)
	assert.JSONEq(t, `"sunny"`, string(chat.Messages[3].Content))

	require.Len(t, chat.Tools, 1)
	assert.Equal(t, "function", chat.Tools[0].Type)
	require.NotNil(t, chat.Tools[0].Function)
	assert.Equal(t, "lookup", chat.Tools[0].Function.Name)
	assert.Equal(t, "Look up weather", chat.Tools[0].Function.Description)
	assert.JSONEq(t, `{"type":"object"}`, string(chat.Tools[0].Function.Parameters))

	var toolChoice struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	require.NoError(t, json.Unmarshal(chat.ToolChoice, &toolChoice))
	assert.Equal(t, "function", toolChoice.Type)
	assert.Equal(t, "lookup", toolChoice.Function.Name)
}

func TestResponsesToChatCompletionsRequest_RejectsInputImage(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-5.2",
		Input: json.RawMessage(`[
			{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,abc123"}]}
		]`),
	}

	_, err := ResponsesToChatCompletionsRequest(req)
	require.Error(t, err)
	assert.ErrorContains(t, err, "input_image")
}

func TestResponsesToChatCompletionsRequest_RejectsUnsupportedTool(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-5.2",
		Input: json.RawMessage(`"Hello"`),
		Tools: []ResponsesTool{{Type: "web_search"}},
	}

	_, err := ResponsesToChatCompletionsRequest(req)
	require.Error(t, err)
	assert.ErrorContains(t, err, "web_search")
}

func TestResponsesToChatCompletionsRequest_RejectsNilRequest(t *testing.T) {
	_, err := ResponsesToChatCompletionsRequest(nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "nil")
}

func TestResponsesToChatCompletionsRequest_RejectsPreviousResponseID(t *testing.T) {
	req := &ResponsesRequest{
		Model:              "gpt-5.2",
		Input:              json.RawMessage(`"Hello"`),
		PreviousResponseID: "resp_previous",
	}

	_, err := ResponsesToChatCompletionsRequest(req)
	require.Error(t, err)
	assert.ErrorContains(t, err, "previous_response_id")
}

func TestResponsesToChatCompletionsRequest_AssistantReplayOutputText(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-5.2",
		Input: json.RawMessage(`[
			{"role":"assistant","content":[{"type":"output_text","text":"Earlier answer"}]}
		]`),
	}

	chat, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, chat.Messages, 1)
	assert.Equal(t, "assistant", chat.Messages[0].Role)
	assert.JSONEq(t, `"Earlier answer"`, string(chat.Messages[0].Content))
}

func TestResponsesToChatCompletionsRequest_AssistantReplayRoundTrip(t *testing.T) {
	original := &ChatCompletionsRequest{
		Model: "gpt-5.2",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Question"`)},
			{Role: "assistant", Content: json.RawMessage(`"Earlier answer"`)},
		},
	}

	responses, err := ChatCompletionsToResponses(original)
	require.NoError(t, err)
	chat, err := ResponsesToChatCompletionsRequest(responses)
	require.NoError(t, err)

	require.Len(t, chat.Messages, 2)
	assert.Equal(t, "user", chat.Messages[0].Role)
	assert.JSONEq(t, `"Question"`, string(chat.Messages[0].Content))
	assert.Equal(t, "assistant", chat.Messages[1].Role)
	assert.JSONEq(t, `"Earlier answer"`, string(chat.Messages[1].Content))
}

func TestResponsesToChatCompletionsRequest_RejectsOutputTextForInputRole(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-5.2",
		Input: json.RawMessage(`[
			{"role":"user","content":[{"type":"output_text","text":"Wrong direction"}]}
		]`),
	}

	_, err := ResponsesToChatCompletionsRequest(req)
	require.Error(t, err)
	assert.ErrorContains(t, err, "output_text")
}

func TestResponsesToChatCompletionsRequest_RejectsInputTextForAssistant(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-5.2",
		Input: json.RawMessage(`[
			{"role":"assistant","content":[{"type":"input_text","text":"Wrong direction"}]}
		]`),
	}

	_, err := ResponsesToChatCompletionsRequest(req)
	require.Error(t, err)
	assert.ErrorContains(t, err, "input_text")
}

func TestResponsesToChatCompletionsRequest_CoalescesAdjacentFunctionCalls(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-5.2",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"lookup_weather","arguments":"{\"city\":\"Paris\"}"},
			{"type":"function_call","call_id":"call_2","name":"lookup_time","arguments":"{\"city\":\"Paris\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"sunny"},
			{"type":"function_call_output","call_id":"call_2","output":"14:00"}
		]`),
	}

	chat, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, chat.Messages, 3)

	assert.Equal(t, "assistant", chat.Messages[0].Role)
	require.Len(t, chat.Messages[0].ToolCalls, 2)
	assert.Equal(t, "call_1", chat.Messages[0].ToolCalls[0].ID)
	assert.Equal(t, "lookup_weather", chat.Messages[0].ToolCalls[0].Function.Name)
	assert.Equal(t, "call_2", chat.Messages[0].ToolCalls[1].ID)
	assert.Equal(t, "lookup_time", chat.Messages[0].ToolCalls[1].Function.Name)

	assert.Equal(t, "tool", chat.Messages[1].Role)
	assert.Equal(t, "call_1", chat.Messages[1].ToolCallID)
	assert.Equal(t, "tool", chat.Messages[2].Role)
	assert.Equal(t, "call_2", chat.Messages[2].ToolCallID)
}

func TestResponsesToChatCompletionsRequest_RejectsAllowedToolsToolChoice(t *testing.T) {
	req := &ResponsesRequest{
		Model:      "gpt-5.2",
		Input:      json.RawMessage(`"Hello"`),
		ToolChoice: json.RawMessage(`{"type":"allowed_tools","tools":[{"type":"function","name":"lookup"}]}`),
	}

	_, err := ResponsesToChatCompletionsRequest(req)
	require.Error(t, err)
	assert.ErrorContains(t, err, "allowed_tools")
}

func TestResponsesToChatCompletionsRequest_AcceptsHarmlessCompatibilityFields(t *testing.T) {
	store := false
	parallelToolCalls := true
	req := &ResponsesRequest{
		Model:             "gpt-5.2",
		Input:             json.RawMessage(`"Hello"`),
		Include:           []string{"reasoning.encrypted_content"},
		Store:             &store,
		ParallelToolCalls: &parallelToolCalls,
		Reasoning: &ResponsesReasoning{
			Effort:  "medium",
			Summary: "auto",
		},
		Text:           &ResponsesText{Verbosity: "high"},
		PromptCacheKey: "cache-key",
	}

	chat, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	assert.Equal(t, "medium", chat.ReasoningEffort)
}

// ---------------------------------------------------------------------------
// ChatCompletionsToResponsesResponse tests
// ---------------------------------------------------------------------------

func TestChatCompletionsToResponsesResponse_TextAndUsage(t *testing.T) {
	chat := &ChatCompletionsResponse{
		ID:    "chatcmpl_text",
		Model: "upstream-model",
		Choices: []ChatChoice{{
			Message: ChatMessage{
				Role:    "assistant",
				Content: json.RawMessage(`"Hello from chat"`),
			},
			FinishReason: "stop",
		}},
		Usage: &ChatUsage{
			PromptTokens:     12,
			CompletionTokens: 7,
			TotalTokens:      19,
		},
	}

	resp := ChatCompletionsToResponsesResponse(chat, "client-model")

	assert.Equal(t, "chatcmpl_text", resp.ID)
	assert.Equal(t, "response", resp.Object)
	assert.Equal(t, "client-model", resp.Model)
	assert.Equal(t, "completed", resp.Status)
	require.Len(t, resp.Output, 1)
	assert.Equal(t, "message", resp.Output[0].Type)
	assert.Equal(t, "assistant", resp.Output[0].Role)
	assert.Equal(t, "completed", resp.Output[0].Status)
	require.Len(t, resp.Output[0].Content, 1)
	assert.Equal(t, "output_text", resp.Output[0].Content[0].Type)
	assert.Equal(t, "Hello from chat", resp.Output[0].Content[0].Text)
	require.NotNil(t, resp.Usage)
	assert.Equal(t, 12, resp.Usage.InputTokens)
	assert.Equal(t, 7, resp.Usage.OutputTokens)
	assert.Equal(t, 19, resp.Usage.TotalTokens)
}

func TestChatCompletionsToResponsesResponse_ToolCall(t *testing.T) {
	chat := &ChatCompletionsResponse{
		Choices: []ChatChoice{{
			Message: ChatMessage{
				Role: "assistant",
				ToolCalls: []ChatToolCall{{
					ID:   "call_lookup",
					Type: "function",
					Function: ChatFunctionCall{
						Name:      "lookup",
						Arguments: `{"city":"Paris"}`,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}

	resp := ChatCompletionsToResponsesResponse(chat, "gpt-4o")

	assert.Equal(t, "response", resp.Object)
	assert.Equal(t, "gpt-4o", resp.Model)
	assert.Equal(t, "completed", resp.Status)
	assert.NotEmpty(t, resp.ID)
	assert.Contains(t, resp.ID, "resp_")
	require.Len(t, resp.Output, 1)
	assert.Equal(t, "function_call", resp.Output[0].Type)
	assert.Equal(t, "call_lookup", resp.Output[0].CallID)
	assert.Equal(t, "lookup", resp.Output[0].Name)
	assert.Equal(t, `{"city":"Paris"}`, resp.Output[0].Arguments)
	assert.Equal(t, "completed", resp.Output[0].Status)
}

func TestChatCompletionsToResponsesResponse_ToolCallPreservesEmptyArguments(t *testing.T) {
	chat := &ChatCompletionsResponse{
		Choices: []ChatChoice{{
			Message: ChatMessage{
				Role: "assistant",
				ToolCalls: []ChatToolCall{{
					ID:   "call_empty",
					Type: "function",
					Function: ChatFunctionCall{
						Name:      "empty_args",
						Arguments: "",
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}

	resp := ChatCompletionsToResponsesResponse(chat, "gpt-4o")

	require.Len(t, resp.Output, 1)
	assert.Equal(t, "function_call", resp.Output[0].Type)
	assert.Equal(t, "", resp.Output[0].Arguments)
}

func TestChatCompletionsToResponsesResponse_ReasoningCachedUsageAndLength(t *testing.T) {
	chat := &ChatCompletionsResponse{
		ID: "chatcmpl_reasoning",
		Choices: []ChatChoice{{
			Message: ChatMessage{
				Role:             "assistant",
				Content:          json.RawMessage(`"Partial answer"`),
				ReasoningContent: "Worked through the problem.",
			},
			FinishReason: "length",
		}},
		Usage: &ChatUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			PromptTokensDetails: &ChatTokenDetails{
				CachedTokens: 80,
			},
		},
	}

	resp := ChatCompletionsToResponsesResponse(chat, "client-model")

	assert.Equal(t, "incomplete", resp.Status)
	require.NotNil(t, resp.IncompleteDetails)
	assert.Equal(t, "max_output_tokens", resp.IncompleteDetails.Reason)
	require.Len(t, resp.Output, 2)
	assert.Equal(t, "reasoning", resp.Output[0].Type)
	require.Len(t, resp.Output[0].Summary, 1)
	assert.Equal(t, "summary_text", resp.Output[0].Summary[0].Type)
	assert.Equal(t, "Worked through the problem.", resp.Output[0].Summary[0].Text)
	assert.Equal(t, "message", resp.Output[1].Type)
	assert.Equal(t, "Partial answer", resp.Output[1].Content[0].Text)
	require.NotNil(t, resp.Usage)
	assert.Equal(t, 100, resp.Usage.InputTokens)
	assert.Equal(t, 50, resp.Usage.OutputTokens)
	assert.Equal(t, 150, resp.Usage.TotalTokens)
	require.NotNil(t, resp.Usage.InputTokensDetails)
	assert.Equal(t, 80, resp.Usage.InputTokensDetails.CachedTokens)
}

func TestChatCompletionsToResponsesResponse_ContentFilterIncomplete(t *testing.T) {
	chat := &ChatCompletionsResponse{
		ID: "chatcmpl_filtered",
		Choices: []ChatChoice{{
			Message: ChatMessage{
				Role:    "assistant",
				Content: json.RawMessage(`"filtered"`),
			},
			FinishReason: "content_filter",
		}},
	}

	resp := ChatCompletionsToResponsesResponse(chat, "gpt-4o")

	assert.Equal(t, "incomplete", resp.Status)
	require.NotNil(t, resp.IncompleteDetails)
	assert.Equal(t, "content_filter", resp.IncompleteDetails.Reason)
}

func TestChatCompletionsToResponsesResponse_NilResponseSafety(t *testing.T) {
	resp := ChatCompletionsToResponsesResponse(nil, "client-model")

	require.NotNil(t, resp)
	assert.Equal(t, "response", resp.Object)
	assert.Equal(t, "client-model", resp.Model)
	assert.Equal(t, "completed", resp.Status)
	assert.NotEmpty(t, resp.ID)
	assert.Contains(t, resp.ID, "resp_")
}

func TestChatCompletionsToResponsesResponse_EmptyChoiceMessage(t *testing.T) {
	chat := &ChatCompletionsResponse{
		Choices: []ChatChoice{{}},
	}

	resp := ChatCompletionsToResponsesResponse(chat, "gpt-4o")

	assert.Equal(t, "completed", resp.Status)
	require.Len(t, resp.Output, 1)
	assert.Equal(t, "message", resp.Output[0].Type)
	assert.Equal(t, "assistant", resp.Output[0].Role)
	assert.Equal(t, "completed", resp.Output[0].Status)
	require.Len(t, resp.Output[0].Content, 1)
	assert.Equal(t, "output_text", resp.Output[0].Content[0].Type)
	assert.Equal(t, "", resp.Output[0].Content[0].Text)
}

func TestChatChunkToResponsesEvents_StreamingTextCreatedDeltaAndCompletionUsage(t *testing.T) {
	state := NewChatCompletionsToResponsesState("client-model")

	first := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		ID:    "chatcmpl_stream",
		Model: "upstream-model",
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{Content: chatTestStringPtr("Hello")},
		}},
	}, state)

	require.Len(t, first, 3)
	assert.Equal(t, "response.created", first[0].Type)
	require.NotNil(t, first[0].Response)
	assert.Equal(t, "chatcmpl_stream", first[0].Response.ID)
	assert.Equal(t, "client-model", first[0].Response.Model)
	assert.Equal(t, "in_progress", first[0].Response.Status)
	assert.Equal(t, "response.output_item.added", first[1].Type)
	require.NotNil(t, first[1].Item)
	assert.Equal(t, "message", first[1].Item.Type)
	assert.Equal(t, "assistant", first[1].Item.Role)
	assert.Equal(t, "in_progress", first[1].Item.Status)
	assert.Equal(t, "response.output_text.delta", first[2].Type)
	assert.Equal(t, "Hello", first[2].Delta)
	assert.Equal(t, first[1].Item.ID, first[2].ItemID)

	second := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Usage: &ChatUsage{
			PromptTokens:     10,
			CompletionTokens: 4,
			PromptTokensDetails: &ChatTokenDetails{
				CachedTokens: 6,
			},
		},
		Choices: []ChatChunkChoice{{
			FinishReason: chatTestStringPtr("stop"),
		}},
	}, state)

	require.Len(t, second, 2)
	assert.Equal(t, "response.output_text.done", second[0].Type)
	assert.Equal(t, "response.output_item.done", second[1].Type)

	final := FinalizeChatCompletionsResponsesStream(state)
	require.Len(t, final, 1)
	assert.Equal(t, "response.completed", final[0].Type)
	require.NotNil(t, final[0].Response)
	assert.Equal(t, "completed", final[0].Response.Status)
	require.NotNil(t, final[0].Response.Usage)
	assert.Equal(t, 10, final[0].Response.Usage.InputTokens)
	assert.Equal(t, 4, final[0].Response.Usage.OutputTokens)
	assert.Equal(t, 14, final[0].Response.Usage.TotalTokens)
	require.NotNil(t, final[0].Response.Usage.InputTokensDetails)
	assert.Equal(t, 6, final[0].Response.Usage.InputTokensDetails.CachedTokens)
	chatAssertMonotonicSequence(t, append(append(first, second...), final...))
}

func TestChatChunkToResponsesEvents_ToolCallArgumentFragments(t *testing.T) {
	state := NewChatCompletionsToResponsesState("gpt-4o")
	toolIndex := 0

	events := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		ID: "chatcmpl_tool",
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ToolCalls: []ChatToolCall{{
				Index: &toolIndex,
				ID:    "call_lookup",
				Type:  "function",
				Function: ChatFunctionCall{
					Name:      "lookup",
					Arguments: `{"city":`,
				},
			}}},
		}},
	}, state)

	require.Len(t, events, 3)
	assert.Equal(t, "response.created", events[0].Type)
	assert.Equal(t, "response.output_item.added", events[1].Type)
	require.NotNil(t, events[1].Item)
	assert.Equal(t, "function_call", events[1].Item.Type)
	assert.Equal(t, "call_lookup", events[1].Item.CallID)
	assert.Equal(t, "lookup", events[1].Item.Name)
	assert.Equal(t, "in_progress", events[1].Item.Status)
	assert.Equal(t, "response.function_call_arguments.delta", events[2].Type)
	assert.Equal(t, `{"city":`, events[2].Delta)
	assert.Equal(t, "call_lookup", events[2].CallID)
	assert.Equal(t, "lookup", events[2].Name)
	assert.Equal(t, events[1].Item.ID, events[2].ItemID)

	events = ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ToolCalls: []ChatToolCall{{
				Index: &toolIndex,
				Function: ChatFunctionCall{
					Arguments: `"Paris"}`,
				},
			}}},
		}},
	}, state)
	require.Len(t, events, 1)
	assert.Equal(t, "response.function_call_arguments.delta", events[0].Type)
	assert.Equal(t, `"Paris"}`, events[0].Delta)
	assert.Equal(t, "call_lookup", events[0].CallID)
	assert.Equal(t, "lookup", events[0].Name)

	events = ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			FinishReason: chatTestStringPtr("tool_calls"),
		}},
	}, state)
	require.Len(t, events, 2)
	assert.Equal(t, "response.function_call_arguments.done", events[0].Type)
	assert.Equal(t, "call_lookup", events[0].CallID)
	assert.Equal(t, "lookup", events[0].Name)
	assert.Equal(t, "response.output_item.done", events[1].Type)

	events = FinalizeChatCompletionsResponsesStream(state)
	require.Len(t, events, 1)
	assert.Equal(t, "response.completed", events[0].Type)
}

func TestChatChunkToResponsesEvents_InterleavedToolIndicesRemainOpenUntilFinish(t *testing.T) {
	state := NewChatCompletionsToResponsesState("gpt-4o")
	firstToolIndex := 0
	secondToolIndex := 1

	first := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		ID: "chatcmpl_tools",
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ToolCalls: []ChatToolCall{{
				Index: &firstToolIndex,
				ID:    "call_first",
				Type:  "function",
				Function: ChatFunctionCall{
					Name:      "first",
					Arguments: `{"one":`,
				},
			}}},
		}},
	}, state)
	require.Len(t, first, 3)
	assert.Equal(t, "response.output_item.added", first[1].Type)
	require.NotNil(t, first[1].Item)
	firstItemID := first[1].Item.ID

	second := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ToolCalls: []ChatToolCall{{
				Index: &secondToolIndex,
				ID:    "call_second",
				Type:  "function",
				Function: ChatFunctionCall{
					Name:      "second",
					Arguments: `{"two":2}`,
				},
			}}},
		}},
	}, state)

	require.Len(t, second, 2)
	assert.Equal(t, "response.output_item.added", second[0].Type)
	require.NotNil(t, second[0].Item)
	assert.Equal(t, "call_second", second[0].Item.CallID)
	assert.Equal(t, "second", second[0].Item.Name)
	assert.Equal(t, "response.function_call_arguments.delta", second[1].Type)
	assert.Equal(t, second[0].Item.ID, second[1].ItemID)
	assert.Equal(t, "call_second", second[1].CallID)
	assert.Equal(t, `{"two":2}`, second[1].Delta)

	third := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ToolCalls: []ChatToolCall{{
				Index: &firstToolIndex,
				Function: ChatFunctionCall{
					Arguments: `"continued"}`,
				},
			}}},
		}},
	}, state)

	require.Len(t, third, 1)
	assert.Equal(t, "response.function_call_arguments.delta", third[0].Type)
	assert.Equal(t, firstItemID, third[0].ItemID)
	assert.Equal(t, "call_first", third[0].CallID)
	assert.Equal(t, `"continued"}`, third[0].Delta)

	finish := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			FinishReason: chatTestStringPtr("tool_calls"),
		}},
	}, state)

	require.Len(t, finish, 4)
	assert.Equal(t, "response.function_call_arguments.done", finish[0].Type)
	assert.Equal(t, firstItemID, finish[0].ItemID)
	assert.Equal(t, "call_first", finish[0].CallID)
	assert.Equal(t, "response.output_item.done", finish[1].Type)
	assert.Equal(t, "response.function_call_arguments.done", finish[2].Type)
	assert.Equal(t, "call_second", finish[2].CallID)
	assert.Equal(t, "response.output_item.done", finish[3].Type)
}

func TestChatChunkToResponsesEvents_ZeroValueStateInitializesToolItems(t *testing.T) {
	state := &ChatCompletionsToResponsesState{}
	toolIndex := 0

	require.NotPanics(t, func() {
		events := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
			ID: "chatcmpl_zero_state",
			Choices: []ChatChunkChoice{{
				Delta: ChatDelta{ToolCalls: []ChatToolCall{{
					Index: &toolIndex,
					ID:    "call_zero",
					Type:  "function",
					Function: ChatFunctionCall{
						Name:      "zero",
						Arguments: `{}`,
					},
				}}},
			}},
		}, state)

		require.Len(t, events, 3)
		assert.Equal(t, "response.output_item.added", events[1].Type)
		require.NotNil(t, events[1].Item)
		assert.Equal(t, "call_zero", events[1].Item.CallID)
		assert.Equal(t, "response.function_call_arguments.delta", events[2].Type)
	})
}

func TestChatChunkToResponsesEvents_MetadataOnlyToolDeltaWaitsForRealCallID(t *testing.T) {
	state := NewChatCompletionsToResponsesState("gpt-4o")
	toolIndex := 0

	first := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		ID: "chatcmpl_delayed_tool_id",
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ToolCalls: []ChatToolCall{{
				Index: &toolIndex,
				Type:  "function",
				Function: ChatFunctionCall{
					Name: "lookup",
				},
			}}},
		}},
	}, state)

	require.Len(t, first, 1)
	assert.Equal(t, "response.created", first[0].Type)

	second := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ToolCalls: []ChatToolCall{{
				Index: &toolIndex,
				ID:    "call_real",
				Function: ChatFunctionCall{
					Arguments: `{"city":"Paris"}`,
				},
			}}},
		}},
	}, state)

	require.Len(t, second, 2)
	assert.Equal(t, "response.output_item.added", second[0].Type)
	require.NotNil(t, second[0].Item)
	assert.Equal(t, "call_real", second[0].Item.CallID)
	assert.Equal(t, "lookup", second[0].Item.Name)
	assert.Equal(t, "response.function_call_arguments.delta", second[1].Type)
	assert.Equal(t, "call_real", second[1].CallID)
	assert.Equal(t, "lookup", second[1].Name)
	assert.Equal(t, `{"city":"Paris"}`, second[1].Delta)
}

func TestChatChunkToResponsesEvents_UsageOnlyChunkAfterFinishEmitsTerminalWithUsage(t *testing.T) {
	state := NewChatCompletionsToResponsesState("gpt-4o")

	first := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		ID: "chatcmpl_usage_after_finish",
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{Content: chatTestStringPtr("Hello")},
		}},
	}, state)
	require.NotEmpty(t, first)

	finish := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			FinishReason: chatTestStringPtr("stop"),
		}},
	}, state)
	require.Len(t, finish, 2)
	assert.Equal(t, "response.output_text.done", finish[0].Type)
	assert.Equal(t, "response.output_item.done", finish[1].Type)

	usage := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Usage: &ChatUsage{
			PromptTokens:     11,
			CompletionTokens: 5,
			TotalTokens:      16,
			PromptTokensDetails: &ChatTokenDetails{
				CachedTokens: 7,
			},
		},
		Choices: []ChatChunkChoice{},
	}, state)
	require.Len(t, usage, 1)
	assert.Equal(t, "response.completed", usage[0].Type)
	require.NotNil(t, usage[0].Response)
	require.NotNil(t, usage[0].Response.Usage)
	assert.Equal(t, 11, usage[0].Response.Usage.InputTokens)
	assert.Equal(t, 5, usage[0].Response.Usage.OutputTokens)
	assert.Equal(t, 16, usage[0].Response.Usage.TotalTokens)
	require.NotNil(t, usage[0].Response.Usage.InputTokensDetails)
	assert.Equal(t, 7, usage[0].Response.Usage.InputTokensDetails.CachedTokens)

	assert.Nil(t, FinalizeChatCompletionsResponsesStream(state))
	assert.Nil(t, ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{Content: chatTestStringPtr("ignored")},
		}},
	}, state))
}

func TestFinalizeChatCompletionsResponsesStream_EmitsPendingFinishWhenNoUsageChunkArrives(t *testing.T) {
	state := NewChatCompletionsToResponsesState("gpt-4o")

	first := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		ID: "chatcmpl_pending_finish",
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{Content: chatTestStringPtr("partial")},
		}},
	}, state)
	require.NotEmpty(t, first)

	finish := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			FinishReason: chatTestStringPtr("stop"),
		}},
	}, state)
	require.Len(t, finish, 2)
	assert.Equal(t, "response.output_text.done", finish[0].Type)
	assert.Equal(t, "response.output_item.done", finish[1].Type)

	events := FinalizeChatCompletionsResponsesStream(state)
	require.Len(t, events, 1)
	assert.Equal(t, "response.completed", events[0].Type)
	require.NotNil(t, events[0].Response)
	assert.Equal(t, "completed", events[0].Response.Status)
	assert.Nil(t, FinalizeChatCompletionsResponsesStream(state))
}

func TestChatChunkToResponsesEvents_ReasoningDeltaAndClose(t *testing.T) {
	state := NewChatCompletionsToResponsesState("")

	events := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Model: "chunk-model",
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ReasoningContent: chatTestStringPtr("Think")},
		}},
	}, state)

	require.Len(t, events, 3)
	assert.Equal(t, "response.created", events[0].Type)
	require.NotNil(t, events[0].Response)
	assert.Equal(t, "chunk-model", events[0].Response.Model)
	assert.Contains(t, events[0].Response.ID, "resp_")
	assert.Equal(t, "response.output_item.added", events[1].Type)
	require.NotNil(t, events[1].Item)
	assert.Equal(t, "reasoning", events[1].Item.Type)
	assert.Equal(t, "response.reasoning_summary_text.delta", events[2].Type)
	assert.Equal(t, "Think", events[2].Delta)
	assert.Equal(t, events[1].Item.ID, events[2].ItemID)

	events = ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			FinishReason: chatTestStringPtr("stop"),
		}},
	}, state)
	require.Len(t, events, 2)
	assert.Equal(t, "response.reasoning_summary_text.done", events[0].Type)
	assert.Equal(t, "response.output_item.done", events[1].Type)

	events = FinalizeChatCompletionsResponsesStream(state)
	require.Len(t, events, 1)
	assert.Equal(t, "response.completed", events[0].Type)
}

func TestFinalizeChatCompletionsResponsesStream_IdempotentAndNoDuplicateAfterTerminal(t *testing.T) {
	state := NewChatCompletionsToResponsesState("gpt-4o")
	events := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		ID: "chatcmpl_finalize",
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{Content: chatTestStringPtr("partial")},
		}},
	}, state)
	require.NotEmpty(t, events)

	events = FinalizeChatCompletionsResponsesStream(state)
	require.Len(t, events, 3)
	assert.Equal(t, "response.output_text.done", events[0].Type)
	assert.Equal(t, "response.output_item.done", events[1].Type)
	assert.Equal(t, "response.completed", events[2].Type)
	assert.Nil(t, FinalizeChatCompletionsResponsesStream(state))

	state = NewChatCompletionsToResponsesState("gpt-4o")
	_ = ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{Content: chatTestStringPtr("done")},
		}},
	}, state)
	events = ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			FinishReason: chatTestStringPtr("stop"),
		}},
	}, state)
	require.NotEmpty(t, events)
	events = FinalizeChatCompletionsResponsesStream(state)
	require.Len(t, events, 1)
	assert.Equal(t, "response.completed", events[0].Type)
	assert.Nil(t, FinalizeChatCompletionsResponsesStream(state))
}

func TestChatChunkToResponsesEvents_MonotonicSequenceNumbersAcrossMixedEvents(t *testing.T) {
	state := NewChatCompletionsToResponsesState("gpt-4o")
	toolIndex := 0

	var all []ResponsesStreamEvent
	all = append(all, ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ReasoningContent: chatTestStringPtr("plan")},
		}},
	}, state)...)
	all = append(all, ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{Content: chatTestStringPtr("answer")},
		}},
	}, state)...)
	all = append(all, ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ToolCalls: []ChatToolCall{{
				Index: &toolIndex,
				ID:    "call_1",
				Type:  "function",
				Function: ChatFunctionCall{
					Name:      "lookup",
					Arguments: "{}",
				},
			}}},
		}},
	}, state)...)
	all = append(all, ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			FinishReason: chatTestStringPtr("tool_calls"),
		}},
	}, state)...)

	chatAssertMonotonicSequence(t, all)
}

func TestChatChunkToResponsesEvents_LengthFinishReasonIncomplete(t *testing.T) {
	state := NewChatCompletionsToResponsesState("gpt-4o")

	events := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			FinishReason: chatTestStringPtr("length"),
		}},
	}, state)

	require.Len(t, events, 1)
	assert.Equal(t, "response.created", events[0].Type)

	events = FinalizeChatCompletionsResponsesStream(state)
	require.Len(t, events, 1)
	assert.Equal(t, "response.incomplete", events[0].Type)
	require.NotNil(t, events[0].Response)
	assert.Equal(t, "incomplete", events[0].Response.Status)
	require.NotNil(t, events[0].Response.IncompleteDetails)
	assert.Equal(t, "max_output_tokens", events[0].Response.IncompleteDetails.Reason)
}

func TestChatChunkToResponsesEvents_ContentFilterFinishReasonIncomplete(t *testing.T) {
	state := NewChatCompletionsToResponsesState("gpt-4o")

	events := ChatChunkToResponsesEvents(&ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			FinishReason: chatTestStringPtr("content_filter"),
		}},
	}, state)

	require.Len(t, events, 1)
	assert.Equal(t, "response.created", events[0].Type)

	events = FinalizeChatCompletionsResponsesStream(state)
	require.Len(t, events, 1)
	assert.Equal(t, "response.incomplete", events[0].Type)
	require.NotNil(t, events[0].Response)
	assert.Equal(t, "incomplete", events[0].Response.Status)
	require.NotNil(t, events[0].Response.IncompleteDetails)
	assert.Equal(t, "content_filter", events[0].Response.IncompleteDetails.Reason)
}

func TestChatChunkToResponsesEvents_NilInputsReturnNil(t *testing.T) {
	assert.Nil(t, ChatChunkToResponsesEvents(nil, NewChatCompletionsToResponsesState("gpt-4o")))
	assert.Nil(t, ChatChunkToResponsesEvents(&ChatCompletionsChunk{}, nil))
	assert.Nil(t, FinalizeChatCompletionsResponsesStream(nil))
}

func chatTestStringPtr(s string) *string {
	return &s
}

func chatAssertMonotonicSequence(t *testing.T, events []ResponsesStreamEvent) {
	t.Helper()
	for i, event := range events {
		assert.Equal(t, i, event.SequenceNumber, "event %d (%s)", i, event.Type)
	}
}
