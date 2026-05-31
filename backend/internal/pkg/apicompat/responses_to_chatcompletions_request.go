package apicompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ResponsesToChatCompletionsRequest converts a Responses API request into a
// Chat Completions request. Responses features without a Chat Completions
// equivalent are rejected instead of being silently dropped.
func ResponsesToChatCompletionsRequest(req *ResponsesRequest) (*ChatCompletionsRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("responses request is nil")
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
		for i, item := range items {
			message, err := convertResponsesInputItemToChatMessage(item)
			if err != nil {
				return nil, fmt.Errorf("convert responses input item %d: %w", i, err)
			}
			messages = append(messages, message)
		}
		return messages, nil

	default:
		return nil, fmt.Errorf("responses input must be a JSON string or array")
	}
}

func convertResponsesInputItemToChatMessage(item ResponsesInputItem) (ChatMessage, error) {
	switch item.Type {
	case "function_call":
		if strings.TrimSpace(item.CallID) == "" {
			return ChatMessage{}, fmt.Errorf("function_call is missing call_id")
		}
		if strings.TrimSpace(item.Name) == "" {
			return ChatMessage{}, fmt.Errorf("function_call is missing name")
		}
		arguments := item.Arguments
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
						Name:      item.Name,
						Arguments: arguments,
					},
				},
			},
		}, nil

	case "function_call_output":
		if strings.TrimSpace(item.CallID) == "" {
			return ChatMessage{}, fmt.Errorf("function_call_output is missing call_id")
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

func convertResponsesRoleMessageToChat(item ResponsesInputItem) (ChatMessage, error) {
	switch item.Role {
	case "developer", "system", "user", "assistant":
	default:
		if strings.TrimSpace(item.Role) == "" {
			return ChatMessage{}, fmt.Errorf("message is missing role")
		}
		return ChatMessage{}, fmt.Errorf("unsupported responses message role %q", item.Role)
	}

	content, err := convertResponsesMessageContentToChat(item.Content)
	if err != nil {
		return ChatMessage{}, err
	}
	return ChatMessage{Role: item.Role, Content: content}, nil
}

func convertResponsesMessageContentToChat(content json.RawMessage) (json.RawMessage, error) {
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

		chatParts := make([]ChatContentPart, 0, len(parts))
		for i, part := range parts {
			switch part.Type {
			case "input_text":
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
		if tool.Type != "function" {
			return nil, fmt.Errorf("unsupported responses tool type %q at index %d", tool.Type, i)
		}
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
	}
	return chatTools, nil
}

func convertResponsesToolChoiceToChat(raw json.RawMessage) (json.RawMessage, error) {
	var choice string
	if err := json.Unmarshal(raw, &choice); err == nil {
		switch choice {
		case "auto", "none", "required":
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
	if choiceObject.Type != "function" {
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
