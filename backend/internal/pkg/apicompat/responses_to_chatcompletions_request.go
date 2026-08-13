package apicompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ResponsesToChatCompletionsRequest maps supported Responses API request fields
// into a Chat Completions request. Unsupported stateful and selector shapes
// that would change request semantics are rejected instead of silently dropped.
func ResponsesToChatCompletionsRequest(req *ResponsesRequest) (*ChatCompletionsRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("responses request is nil")
	}
	if strings.TrimSpace(req.PreviousResponseID) != "" {
		return nil, fmt.Errorf("unsupported responses previous_response_id")
	}

	messages, err := convertResponsesInputToChatMessages(req.Input)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(req.Instructions) != "" {
		content, err := json.Marshal(req.Instructions)
		if err != nil {
			return nil, fmt.Errorf("marshal instructions: %w", err)
		}
		messages = append([]ChatMessage{{Role: "system", Content: content}}, messages...)
	}

	out := &ChatCompletionsRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		ServiceTier: req.ServiceTier,
	}

	if req.MaxOutputTokens != nil {
		maxCompletionTokens := *req.MaxOutputTokens
		out.MaxCompletionTokens = &maxCompletionTokens
	}

	if req.Reasoning != nil {
		out.ReasoningEffort = strings.TrimSpace(req.Reasoning.Effort)
	}

	if len(req.Tools) > 0 {
		tools, err := convertResponsesToolsToChatTools(req.Tools)
		if err != nil {
			return nil, err
		}
		out.Tools = tools
	}

	if len(bytes.TrimSpace(req.ToolChoice)) > 0 {
		toolChoice, err := convertResponsesToolChoiceToChat(req.ToolChoice)
		if err != nil {
			return nil, fmt.Errorf("convert tool_choice: %w", err)
		}
		out.ToolChoice = toolChoice
	}

	return out, nil
}

func convertResponsesInputToChatMessages(input json.RawMessage) ([]ChatMessage, error) {
	raw := bytes.TrimSpace(input)
	if len(raw) == 0 {
		return nil, fmt.Errorf("responses input is empty")
	}

	switch raw[0] {
	case '"':
		var inputText string
		if err := json.Unmarshal(raw, &inputText); err != nil {
			return nil, fmt.Errorf("parse responses string input: %w", err)
		}
		content, err := json.Marshal(inputText)
		if err != nil {
			return nil, fmt.Errorf("marshal responses string input: %w", err)
		}
		return []ChatMessage{{Role: "user", Content: content}}, nil

	case '[':
		var items []ResponsesInputItem
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("parse responses input items: %w", err)
		}

		messages := make([]ChatMessage, 0, len(items))
		mediaByCallID := make(toolOutputMediaByCallID)
		for i, item := range items {
			if isResponsesToolOutputItemType(item.Type) {
				outputRaw := item.outputRaw
				if len(outputRaw) == 0 {
					var err error
					outputRaw, err = json.Marshal(item.Output)
					if err != nil {
						return nil, fmt.Errorf("marshal responses tool output %d: %w", i, err)
					}
				}
				outputText, media, rewritten := extractToolOutputMedia(outputRaw)
				if rewritten {
					item.Output = outputText
					if strings.TrimSpace(item.CallID) != "" {
						mediaByCallID[item.CallID] = media
					}
				} else {
					// Duplicate outputs are last-wins; a later text output must clear
					// media extracted from an earlier output for the same call.
					delete(mediaByCallID, item.CallID)
				}
			}

			message, err := convertResponsesInputItemToChatMessage(item)
			if err != nil {
				return nil, fmt.Errorf("convert responses input item %d: %w", i, err)
			}
			if isResponsesToolCallItemType(item.Type) && i > 0 && isResponsesToolCallItemType(items[i-1].Type) {
				messages[len(messages)-1].ToolCalls = append(messages[len(messages)-1].ToolCalls, message.ToolCalls...)
				continue
			}
			messages = append(messages, message)
		}
		return normalizeChatMessagesWithToolOutputMedia(messages, mediaByCallID), nil

	default:
		return nil, fmt.Errorf("responses input must be a JSON string or array")
	}
}

func convertResponsesInputItemToChatMessage(item ResponsesInputItem) (ChatMessage, error) {
	switch item.Type {
	case "function_call", "custom_tool_call", "tool_search_call":
		if strings.TrimSpace(item.CallID) == "" {
			return ChatMessage{}, fmt.Errorf("%s is missing call_id", item.Type)
		}
		if item.Type != "tool_search_call" && strings.TrimSpace(item.Name) == "" {
			return ChatMessage{}, fmt.Errorf("%s is missing name", item.Type)
		}

		name := item.Name
		arguments := item.Arguments
		switch item.Type {
		case "custom_tool_call":
			encoded, err := json.Marshal(map[string]string{"input": item.Input})
			if err != nil {
				return ChatMessage{}, fmt.Errorf("marshal custom_tool_call input: %w", err)
			}
			arguments = string(encoded)
		case "tool_search_call":
			name = toolSearchProxyName
		}
		if arguments == "" {
			arguments = "{}"
		}
		return ChatMessage{
			Role: "assistant",
			ToolCalls: []ChatToolCall{
				{
					ID:   item.CallID,
					Type: "function",
					Function: ChatFunctionCall{
						Name:      name,
						Arguments: arguments,
					},
				},
			},
		}, nil

	case "function_call_output", "custom_tool_call_output", "tool_search_output":
		if strings.TrimSpace(item.CallID) == "" {
			return ChatMessage{}, fmt.Errorf("%s is missing call_id", item.Type)
		}
		content, err := json.Marshal(item.Output)
		if err != nil {
			return ChatMessage{}, fmt.Errorf("marshal function_call_output: %w", err)
		}
		return ChatMessage{
			Role:       "tool",
			Content:    content,
			ToolCallID: item.CallID,
		}, nil

	case "", "message":
		return convertResponsesRoleMessageToChat(item)

	default:
		return ChatMessage{}, fmt.Errorf("unsupported responses input item type %q", item.Type)
	}
}

func isResponsesToolCallItemType(itemType string) bool {
	switch itemType {
	case "function_call", "custom_tool_call", "tool_search_call":
		return true
	default:
		return false
	}
}

func isResponsesToolOutputItemType(itemType string) bool {
	switch itemType {
	case "function_call_output", "custom_tool_call_output", "tool_search_output":
		return true
	default:
		return false
	}
}

func convertResponsesRoleMessageToChat(item ResponsesInputItem) (ChatMessage, error) {
	switch item.Role {
	case "developer", "system", "user", "assistant":
	default:
		if strings.TrimSpace(item.Role) == "" {
			return ChatMessage{}, fmt.Errorf("message is missing role")
		}
		return ChatMessage{}, fmt.Errorf("unsupported responses message role %q", item.Role)
	}

	content, err := convertResponsesMessageContentToChat(item.Role, item.Content)
	if err != nil {
		return ChatMessage{}, err
	}
	return ChatMessage{Role: item.Role, Content: content}, nil
}

func convertResponsesMessageContentToChat(role string, content json.RawMessage) (json.RawMessage, error) {
	raw := bytes.TrimSpace(content)
	if len(raw) == 0 {
		return nil, fmt.Errorf("message is missing content")
	}

	switch raw[0] {
	case '"':
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, fmt.Errorf("parse message string content: %w", err)
		}
		return json.Marshal(text)

	case '[':
		var parts []ResponsesContentPart
		if err := json.Unmarshal(raw, &parts); err != nil {
			return nil, fmt.Errorf("parse message content parts: %w", err)
		}

		textPartType := "input_text"
		if role == "assistant" {
			textPartType = "output_text"
		}

		chatParts := make([]ChatContentPart, 0, len(parts))
		for i, part := range parts {
			switch part.Type {
			case textPartType:
				chatParts = append(chatParts, ChatContentPart{
					Type: "text",
					Text: part.Text,
				})
			case "input_image":
				return nil, fmt.Errorf("unsupported responses content part type %q at index %d", part.Type, i)
			default:
				return nil, fmt.Errorf("unsupported responses content part type %q at index %d", part.Type, i)
			}
		}

		if len(chatParts) == 1 {
			return json.Marshal(chatParts[0].Text)
		}
		return json.Marshal(chatParts)

	default:
		return nil, fmt.Errorf("message content must be a JSON string or array")
	}
}

func convertResponsesToolsToChatTools(tools []ResponsesTool) ([]ChatTool, error) {
	chatTools := make([]ChatTool, 0, len(tools))
	for i, tool := range tools {
		switch strings.ToLower(strings.TrimSpace(tool.Type)) {
		case "function", "custom":
			if strings.TrimSpace(tool.Name) == "" {
				return nil, fmt.Errorf("function tool at index %d is missing name", i)
			}
			chatTools = append(chatTools, ChatTool{
				Type: "function",
				Function: &ChatFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.Parameters,
					Strict:      tool.Strict,
				},
			})
		case "x_search":
			chatTools = append(chatTools, ChatTool{
				Type:                     "x_search",
				AllowedXHandles:          tool.AllowedXHandles,
				ExcludedXHandles:         tool.ExcludedXHandles,
				FromDate:                 tool.FromDate,
				ToDate:                   tool.ToDate,
				EnableImageUnderstanding: tool.EnableImageUnderstanding,
				EnableVideoUnderstanding: tool.EnableVideoUnderstanding,
			})
		case "web_search", "image_generation", "file_search", "computer_use", "tool_search", "namespace", "":
			// This converter is the strict compatibility path. Silently dropping a
			// server-side tool changes request semantics, so callers must explicitly
			// use LegacyResponsesToChatCompletionsRequest when lossy fallback is
			// acceptable.
			return nil, fmt.Errorf("unsupported responses tool type %q at index %d", tool.Type, i)
		default:
			return nil, fmt.Errorf("unsupported responses tool type %q at index %d", tool.Type, i)
		}
	}
	return chatTools, nil
}

func convertResponsesToolChoiceToChat(raw json.RawMessage) (json.RawMessage, error) {
	var choice string
	if err := json.Unmarshal(raw, &choice); err == nil {
		switch choice {
		case "auto", "none", "required", "x_search":
			return json.Marshal(choice)
		default:
			return nil, fmt.Errorf("unsupported responses tool_choice %q", choice)
		}
	}

	var choiceObject struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &choiceObject); err != nil {
		return nil, fmt.Errorf("parse responses tool_choice: %w", err)
	}
	switch choiceObject.Type {
	case "function":
	case "x_search":
		return json.Marshal(map[string]string{"type": "x_search"})
	case "allowed_tools":
		return nil, fmt.Errorf("unsupported responses tool_choice type %q", choiceObject.Type)
	default:
		return nil, fmt.Errorf("unsupported responses tool_choice type %q", choiceObject.Type)
	}
	if strings.TrimSpace(choiceObject.Name) == "" {
		return nil, fmt.Errorf("function tool_choice is missing name")
	}

	return json.Marshal(map[string]any{
		"type": "function",
		"function": map[string]string{
			"name": choiceObject.Name,
		},
	})
}
