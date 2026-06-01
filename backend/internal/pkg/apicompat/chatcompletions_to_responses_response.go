package apicompat

import "fmt"

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
	switch choice.FinishReason {
	case "length":
		out.Status = "incomplete"
		out.IncompleteDetails = &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
	case "content_filter":
		out.Status = "incomplete"
		out.IncompleteDetails = &ResponsesIncompleteDetails{Reason: "content_filter"}
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

// ChatCompletionsToResponsesState tracks a Chat Completions stream while it is
// converted into Responses stream events.
type ChatCompletionsToResponsesState struct {
	ResponseID     string
	Model          string
	SequenceNumber int
	CreatedSent    bool
	CompletedSent  bool

	NextOutputIndex int
	CurrentCategory string
	MessageItem     *chatResponsesStreamItem
	ReasoningItem   *chatResponsesStreamItem
	ToolItems       map[int]*chatResponsesStreamItem
	ToolOrder       []int
	Usage           *ChatUsage
}

type chatResponsesStreamItem struct {
	OutputIndex int
	ItemID      string
	Type        string
	CallID      string
	Name        string
	Closed      bool
}

// NewChatCompletionsToResponsesState returns an initialized streaming
// conversion state. A non-empty model here takes precedence over chunk.Model.
func NewChatCompletionsToResponsesState(model string) *ChatCompletionsToResponsesState {
	return &ChatCompletionsToResponsesState{
		Model:     model,
		ToolItems: make(map[int]*chatResponsesStreamItem),
	}
}

// ChatChunkToResponsesEvents converts one Chat Completions streaming chunk into
// zero or more Responses streaming events.
func ChatChunkToResponsesEvents(chunk *ChatCompletionsChunk, state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	if chunk == nil || state == nil || state.CompletedSent {
		return nil
	}

	meaningful := chatChunkIsMeaningful(chunk)
	chatResponsesUpdateStateFromChunk(chunk, state, meaningful)
	if chunk.Usage != nil {
		state.Usage = chunk.Usage
	}

	var events []ResponsesStreamEvent
	if meaningful {
		events = append(events, chatResponsesEnsureCreated(state)...)
	}

	for _, choice := range chunk.Choices {
		events = append(events, chatResponsesHandleDelta(choice.Delta, state)...)
		if choice.FinishReason != nil {
			events = append(events, chatResponsesEnsureCreated(state)...)
			events = append(events, chatResponsesCloseCurrentCategory(state)...)
			events = append(events, chatResponsesTerminalEvent(*choice.FinishReason, state))
			state.CompletedSent = true
			return events
		}
	}

	return events
}

// FinalizeChatCompletionsResponsesStream emits synthetic closure/completion
// when the upstream stream ends without a terminal chunk.
func FinalizeChatCompletionsResponsesStream(state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	if state == nil || !state.CreatedSent || state.CompletedSent {
		return nil
	}

	var events []ResponsesStreamEvent
	events = append(events, chatResponsesCloseCurrentCategory(state)...)
	events = append(events, chatResponsesTerminalEvent("stop", state))
	state.CompletedSent = true
	return events
}

func chatResponsesUpdateStateFromChunk(chunk *ChatCompletionsChunk, state *ChatCompletionsToResponsesState, meaningful bool) {
	if state.ResponseID == "" {
		if chunk.ID != "" {
			state.ResponseID = chunk.ID
		} else if meaningful {
			state.ResponseID = generateResponsesID()
		}
	}
	if state.Model == "" {
		state.Model = chunk.Model
	}
}

func chatChunkIsMeaningful(chunk *ChatCompletionsChunk) bool {
	if chunk.Usage != nil {
		return true
	}
	for _, choice := range chunk.Choices {
		if choice.FinishReason != nil {
			return true
		}
		delta := choice.Delta
		if delta.Content != nil || delta.ReasoningContent != nil || len(delta.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func chatResponsesEnsureCreated(state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	if state.CreatedSent {
		return nil
	}
	state.CreatedSent = true
	return []ResponsesStreamEvent{chatResponsesEvent(state, "response.created", &ResponsesStreamEvent{
		Response: &ResponsesResponse{
			ID:     state.ResponseID,
			Object: "response",
			Model:  state.Model,
			Status: "in_progress",
			Output: []ResponsesOutput{},
		},
	})}
}

func chatResponsesHandleDelta(delta ChatDelta, state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	var events []ResponsesStreamEvent
	if delta.ReasoningContent != nil {
		events = append(events, chatResponsesReasoningDelta(*delta.ReasoningContent, state)...)
	}
	if delta.Content != nil {
		events = append(events, chatResponsesTextDelta(*delta.Content, state)...)
	}
	for _, toolCall := range delta.ToolCalls {
		events = append(events, chatResponsesToolDelta(toolCall, state)...)
	}
	return events
}

func chatResponsesTextDelta(delta string, state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	var events []ResponsesStreamEvent
	if state.CurrentCategory != "message" {
		events = append(events, chatResponsesCloseCurrentCategory(state)...)
		events = append(events, chatResponsesOpenMessage(state))
		state.CurrentCategory = "message"
	}
	events = append(events, chatResponsesEvent(state, "response.output_text.delta", &ResponsesStreamEvent{
		OutputIndex:  state.MessageItem.OutputIndex,
		ContentIndex: 0,
		Delta:        delta,
		ItemID:       state.MessageItem.ItemID,
	}))
	return events
}

func chatResponsesReasoningDelta(delta string, state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	var events []ResponsesStreamEvent
	if state.CurrentCategory != "reasoning" {
		events = append(events, chatResponsesCloseCurrentCategory(state)...)
		events = append(events, chatResponsesOpenReasoning(state))
		state.CurrentCategory = "reasoning"
	}
	events = append(events, chatResponsesEvent(state, "response.reasoning_summary_text.delta", &ResponsesStreamEvent{
		OutputIndex:  state.ReasoningItem.OutputIndex,
		SummaryIndex: 0,
		Delta:        delta,
		ItemID:       state.ReasoningItem.ItemID,
	}))
	return events
}

func chatResponsesToolDelta(toolCall ChatToolCall, state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	index := 0
	if toolCall.Index != nil {
		index = *toolCall.Index
	}

	var events []ResponsesStreamEvent
	if state.CurrentCategory != "function_call" {
		events = append(events, chatResponsesCloseCurrentCategory(state)...)
		state.CurrentCategory = "function_call"
	}

	item := state.ToolItems[index]
	if item == nil {
		item = &chatResponsesStreamItem{
			OutputIndex: state.NextOutputIndex,
			ItemID:      generateItemID(),
			Type:        "function_call",
			CallID:      toolCall.ID,
			Name:        toolCall.Function.Name,
		}
		if item.CallID == "" {
			item.CallID = fmt.Sprintf("call_%d", index)
		}
		state.NextOutputIndex++
		state.ToolItems[index] = item
		state.ToolOrder = append(state.ToolOrder, index)
		events = append(events, chatResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
			OutputIndex: item.OutputIndex,
			Item: &ResponsesOutput{
				Type:   "function_call",
				ID:     item.ItemID,
				CallID: item.CallID,
				Name:   item.Name,
				Status: "in_progress",
			},
		}))
	} else {
		if item.CallID == "" && toolCall.ID != "" {
			item.CallID = toolCall.ID
		}
		if item.Name == "" && toolCall.Function.Name != "" {
			item.Name = toolCall.Function.Name
		}
	}

	if toolCall.Function.Arguments != "" {
		events = append(events, chatResponsesEvent(state, "response.function_call_arguments.delta", &ResponsesStreamEvent{
			OutputIndex: item.OutputIndex,
			Delta:       toolCall.Function.Arguments,
			ItemID:      item.ItemID,
			CallID:      item.CallID,
			Name:        item.Name,
		}))
	}
	return events
}

func chatResponsesOpenMessage(state *ChatCompletionsToResponsesState) ResponsesStreamEvent {
	state.MessageItem = &chatResponsesStreamItem{
		OutputIndex: state.NextOutputIndex,
		ItemID:      generateItemID(),
		Type:        "message",
	}
	state.NextOutputIndex++
	return chatResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
		OutputIndex: state.MessageItem.OutputIndex,
		Item: &ResponsesOutput{
			Type:   "message",
			ID:     state.MessageItem.ItemID,
			Role:   "assistant",
			Status: "in_progress",
		},
	})
}

func chatResponsesOpenReasoning(state *ChatCompletionsToResponsesState) ResponsesStreamEvent {
	state.ReasoningItem = &chatResponsesStreamItem{
		OutputIndex: state.NextOutputIndex,
		ItemID:      generateItemID(),
		Type:        "reasoning",
	}
	state.NextOutputIndex++
	return chatResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
		OutputIndex: state.ReasoningItem.OutputIndex,
		Item: &ResponsesOutput{
			Type: "reasoning",
			ID:   state.ReasoningItem.ItemID,
		},
	})
}

func chatResponsesCloseCurrentCategory(state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	switch state.CurrentCategory {
	case "message":
		return chatResponsesCloseMessage(state)
	case "reasoning":
		return chatResponsesCloseReasoning(state)
	case "function_call":
		return chatResponsesCloseToolCalls(state)
	default:
		return nil
	}
}

func chatResponsesCloseMessage(state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	if state.MessageItem == nil || state.MessageItem.Closed {
		state.CurrentCategory = ""
		return nil
	}
	item := state.MessageItem
	item.Closed = true
	state.CurrentCategory = ""
	return []ResponsesStreamEvent{
		chatResponsesEvent(state, "response.output_text.done", &ResponsesStreamEvent{
			OutputIndex:  item.OutputIndex,
			ContentIndex: 0,
			ItemID:       item.ItemID,
		}),
		chatResponsesEvent(state, "response.output_item.done", &ResponsesStreamEvent{
			OutputIndex: item.OutputIndex,
			Item: &ResponsesOutput{
				Type:   "message",
				ID:     item.ItemID,
				Status: "completed",
			},
		}),
	}
}

func chatResponsesCloseReasoning(state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	if state.ReasoningItem == nil || state.ReasoningItem.Closed {
		state.CurrentCategory = ""
		return nil
	}
	item := state.ReasoningItem
	item.Closed = true
	state.CurrentCategory = ""
	return []ResponsesStreamEvent{
		chatResponsesEvent(state, "response.reasoning_summary_text.done", &ResponsesStreamEvent{
			OutputIndex:  item.OutputIndex,
			SummaryIndex: 0,
			ItemID:       item.ItemID,
		}),
		chatResponsesEvent(state, "response.output_item.done", &ResponsesStreamEvent{
			OutputIndex: item.OutputIndex,
			Item: &ResponsesOutput{
				Type:   "reasoning",
				ID:     item.ItemID,
				Status: "completed",
			},
		}),
	}
}

func chatResponsesCloseToolCalls(state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	var events []ResponsesStreamEvent
	for _, index := range state.ToolOrder {
		item := state.ToolItems[index]
		if item == nil || item.Closed {
			continue
		}
		item.Closed = true
		events = append(events,
			chatResponsesEvent(state, "response.function_call_arguments.done", &ResponsesStreamEvent{
				OutputIndex: item.OutputIndex,
				ItemID:      item.ItemID,
				CallID:      item.CallID,
				Name:        item.Name,
			}),
			chatResponsesEvent(state, "response.output_item.done", &ResponsesStreamEvent{
				OutputIndex: item.OutputIndex,
				Item: &ResponsesOutput{
					Type:   "function_call",
					ID:     item.ItemID,
					CallID: item.CallID,
					Name:   item.Name,
					Status: "completed",
				},
			}),
		)
	}
	state.CurrentCategory = ""
	return events
}

func chatResponsesTerminalEvent(finishReason string, state *ChatCompletionsToResponsesState) ResponsesStreamEvent {
	status := "completed"
	eventType := "response.completed"
	var incompleteDetails *ResponsesIncompleteDetails
	switch finishReason {
	case "length":
		status = "incomplete"
		eventType = "response.incomplete"
		incompleteDetails = &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
	case "content_filter":
		status = "incomplete"
		eventType = "response.incomplete"
		incompleteDetails = &ResponsesIncompleteDetails{Reason: "content_filter"}
	}

	return chatResponsesEvent(state, eventType, &ResponsesStreamEvent{
		Response: &ResponsesResponse{
			ID:                state.ResponseID,
			Object:            "response",
			Model:             state.Model,
			Status:            status,
			Output:            []ResponsesOutput{},
			Usage:             chatUsageToResponsesUsage(state.Usage),
			IncompleteDetails: incompleteDetails,
		},
	})
}

func chatResponsesEvent(state *ChatCompletionsToResponsesState, eventType string, template *ResponsesStreamEvent) ResponsesStreamEvent {
	seq := state.SequenceNumber
	state.SequenceNumber++
	evt := *template
	evt.Type = eventType
	evt.SequenceNumber = seq
	return evt
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
