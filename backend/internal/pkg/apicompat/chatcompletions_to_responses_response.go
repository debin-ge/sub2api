package apicompat

// ChatCompletionsToResponsesResponse converts a non-streaming Chat Completions
// response into a non-streaming Responses API response.
func ChatCompletionsToResponsesResponse(resp *ChatCompletionsResponse, model string) *ResponsesResponse {
	out := &ResponsesResponse{
		ID:     generateResponsesID(),
		Object: "response",
		Model:  model,
		Status: "completed",
	}
	if resp == nil {
		out.Output = []ResponsesOutput{emptyAssistantResponsesMessage()}
		return out
	}
	if resp.ID != "" {
		out.ID = resp.ID
	}

	var choice ChatChoice
	if len(resp.Choices) > 0 {
		choice = resp.Choices[0]
	}
	if choice.FinishReason == "length" {
		out.Status = "incomplete"
		out.IncompleteDetails = &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
	}

	msg := choice.Message
	var outputs []ResponsesOutput
	if msg.ReasoningContent != "" {
		outputs = append(outputs, ResponsesOutput{
			Type: "reasoning",
			ID:   generateItemID(),
			Summary: []ResponsesSummary{{
				Type: "summary_text",
				Text: msg.ReasoningContent,
			}},
		})
	}

	text := ""
	if len(msg.Content) > 0 {
		text, _ = parseAssistantContent(msg.Content)
	}
	if text != "" {
		outputs = append(outputs, ResponsesOutput{
			Type:    "message",
			ID:      generateItemID(),
			Role:    "assistant",
			Content: []ResponsesContentPart{{Type: "output_text", Text: text}},
			Status:  "completed",
		})
	}

	for _, tc := range msg.ToolCalls {
		outputs = append(outputs, ResponsesOutput{
			Type:      "function_call",
			ID:        generateItemID(),
			CallID:    tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
			Status:    "completed",
		})
	}

	if len(outputs) == 0 {
		outputs = append(outputs, emptyAssistantResponsesMessage())
	}
	out.Output = outputs
	out.Usage = chatUsageToResponsesUsage(resp.Usage)
	return out
}

func emptyAssistantResponsesMessage() ResponsesOutput {
	return ResponsesOutput{
		Type:    "message",
		ID:      generateItemID(),
		Role:    "assistant",
		Content: []ResponsesContentPart{{Type: "output_text", Text: ""}},
		Status:  "completed",
	}
}

func chatUsageToResponsesUsage(usage *ChatUsage) *ResponsesUsage {
	if usage == nil {
		return nil
	}

	total := usage.TotalTokens
	if total == 0 {
		total = usage.PromptTokens + usage.CompletionTokens
	}
	out := &ResponsesUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		TotalTokens:  total,
	}
	if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens > 0 {
		out.InputTokensDetails = &ResponsesInputTokensDetails{
			CachedTokens: usage.PromptTokensDetails.CachedTokens,
		}
	}
	return out
}
