package service

import (
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"
)

const fallbackUsageMinimumTokens = 1

// EstimateOpenAIUsageFallback estimates billable text usage when an upstream
// success response omitted usage. It deliberately leaves cache buckets empty:
// cache hit/write cannot be inferred safely from a response without usage.
func EstimateOpenAIUsageFallback(model string, requestBody, semanticOutput []byte) (OpenAIUsage, UsageSource, string) {
	model = strings.TrimSpace(model)
	inputTokens, inputMethod, inputOK := estimateFallbackInputTokens(model, requestBody)
	if inputTokens < fallbackUsageMinimumTokens {
		inputTokens = fallbackUsageMinimumTokens
	}

	outputTokens, outputMethod := estimateFallbackTextTokens(model, semanticOutput)
	if !inputOK {
		return OpenAIUsage{InputTokens: inputTokens, OutputTokens: outputTokens}, UsageSourceMinimum, "minimum"
	}
	method := inputMethod
	if outputMethod == "heuristic" && method == "tokenizer" {
		method = "heuristic"
	}
	return OpenAIUsage{InputTokens: inputTokens, OutputTokens: outputTokens}, UsageSourceEstimated, method
}

func applyOpenAIUsageFallback(result *OpenAIForwardResult, model string, requestBody []byte) {
	if result == nil || result.UsageSource != UsageSourceUnknown {
		return
	}
	if !openAIResultNeedsTokenUsage(result) {
		return
	}
	if openAIUsageHasTokens(&result.Usage) {
		result.UsageSource = UsageSourceUpstream
		result.UsageEstimationMethod = "upstream"
		return
	}
	result.FallbackRequestBody = append(result.FallbackRequestBody[:0], requestBody...)
	usage, source, method := EstimateOpenAIUsageFallback(model, requestBody, result.FallbackSemanticOutput)
	result.Usage = usage
	result.UsageSource = source
	result.UsageEstimationMethod = method
}

func requiresTokenUsage(result *OpenAIForwardResult, billingKind BillingKind) bool {
	if result == nil {
		return false
	}
	switch billingKind {
	case BillingKindNone, BillingKindWebSearch, BillingKindAudio, BillingKindVideo:
		return false
	case BillingKindImage:
		return result.ImageBillingPlan != nil && result.ImageBillingPlan.Mode == BillingModeToken
	}
	if result.AudioUsage != nil || result.SearchCount > 0 || result.WebSearchCalls > 0 || result.VideoCount > 0 {
		return false
	}
	if result.ImageCount > 0 {
		return result.ImageBillingPlan != nil && result.ImageBillingPlan.Mode == BillingModeToken
	}
	return true
}

func openAIResultNeedsTokenUsage(result *OpenAIForwardResult) bool {
	return requiresTokenUsage(result, BillingKindToken)
}

// extractOpenAISemanticOutputForBilling collects response strings that can
// represent generated text or tool arguments. It intentionally ignores the
// protocol envelope and identifiers; the result is only a billing fallback.
func extractOpenAISemanticOutputForBilling(body []byte) []byte {
	var value any
	if len(body) == 0 || json.Unmarshal(body, &value) != nil {
		return nil
	}
	parts := make([]string, 0, 8)
	var walk func(any, string)
	walk = func(node any, key string) {
		switch value := node.(type) {
		case map[string]any:
			for childKey, child := range value {
				switch childKey {
				case "text", "delta", "arguments", "reasoning", "summary_text":
					walk(child, childKey)
				case "content":
					walk(child, childKey)
				default:
					if childKey != "usage" && childKey != "id" && childKey != "model" {
						walk(child, childKey)
					}
				}
			}
		case []any:
			for _, child := range value {
				walk(child, key)
			}
		case string:
			if strings.TrimSpace(value) != "" && (key == "text" || key == "delta" || key == "arguments" || key == "reasoning" || key == "summary_text" || key == "content") {
				parts = append(parts, value)
			}
		}
	}
	walk(value, "")
	return []byte(strings.Join(parts, "\n"))
}

func applyGatewayUsageFallback(result *ForwardResult, requestBody []byte) {
	if result == nil || result.UsageSource != UsageSourceUnknown {
		return
	}
	if result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 ||
		result.Usage.CacheCreationInputTokens > 0 || result.Usage.CacheReadInputTokens > 0 ||
		result.Usage.ImageOutputTokens > 0 {
		result.UsageSource = UsageSourceUpstream
		result.UsageEstimationMethod = "upstream"
		return
	}
	if len(requestBody) > 0 {
		usage, source, method := EstimateOpenAIUsageFallback(result.Model, requestBody, nil)
		result.Usage.InputTokens = usage.InputTokens
		result.Usage.OutputTokens = usage.OutputTokens
		result.UsageSource = source
		result.UsageEstimationMethod = method
		return
	}
	result.Usage.InputTokens = fallbackUsageMinimumTokens
	result.UsageSource = UsageSourceMinimum
	result.UsageEstimationMethod = "minimum"
}

func estimateFallbackInputTokens(model string, body []byte) (int, string, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return fallbackHeuristicTokens(body), "heuristic", false
	}

	request := openAIInputTokensCountRequest{Model: model}
	if value, ok := raw["instructions"]; ok {
		_ = json.Unmarshal(value, &request.Instructions)
	}
	if value, ok := raw["input"]; ok {
		request.Input = value
	}
	if value, ok := raw["tools"]; ok {
		_ = json.Unmarshal(value, &request.Tools)
	}
	if value, ok := raw["tool_choice"]; ok {
		request.ToolChoice = value
	}
	if request.Input != nil || request.Instructions != "" || request.Tools != nil || request.ToolChoice != nil {
		if estimated, err := estimateOpenAIInputTokens(request); err == nil && estimated > 0 {
			return estimated, "tokenizer", true
		}
	}

	codec, err := openAIInputTokensCodecForModel(model)
	if err == nil {
		if estimated, countErr := codec.Count(string(body)); countErr == nil && estimated > 0 {
			return estimated, "tokenizer", true
		}
	}
	return fallbackHeuristicTokens(body), "heuristic", len(body) > 0
}

func estimateFallbackTextTokens(model string, output []byte) (int, string) {
	if len(strings.TrimSpace(string(output))) == 0 {
		return 0, "tokenizer"
	}
	codec, err := openAIInputTokensCodecForModel(model)
	if err == nil {
		if estimated, countErr := codec.Count(string(output)); countErr == nil {
			return fallbackMaxInt(0, estimated), "tokenizer"
		}
	}
	return fallbackHeuristicTokens(output), "heuristic"
}

func fallbackHeuristicTokens(data []byte) int {
	if len(data) == 0 {
		return fallbackUsageMinimumTokens
	}
	runes := utf8.RuneCount(data)
	if runes <= 0 {
		return fallbackUsageMinimumTokens
	}
	return fallbackMaxInt(fallbackUsageMinimumTokens, int(math.Ceil(float64(runes)/4)))
}

func fallbackMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
