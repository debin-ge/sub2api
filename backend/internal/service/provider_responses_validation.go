package service

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// ProviderResponsesCompatibilityError describes an OpenAI-compatible validation error.
type ProviderResponsesCompatibilityError struct {
	StatusCode int
	Type       string
	Message    string
}

func (e *ProviderResponsesCompatibilityError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func ValidateProviderResponsesCompatibilityRequest(path string, body []byte) error {
	if isProviderResponsesCompactPath(path) {
		return newProviderResponsesCompatibilityError("provider Responses compatibility does not support /responses/compact")
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return newProviderResponsesCompatibilityError("invalid JSON request body")
	}

	model, ok := payload["model"].(string)
	model = strings.TrimSpace(model)
	if !ok || model == "" {
		return newProviderResponsesCompatibilityError("model must be a non-empty string")
	}

	if hasPreviousResponseID(payload["previous_response_id"]) {
		return newProviderResponsesCompatibilityError("previous_response_id is not supported for this provider Responses compatibility path")
	}

	if IsImageGenerationIntent("/v1/responses", model, body) {
		return newProviderResponsesCompatibilityError("image_generation and image edit requests are not supported for provider Responses compatibility")
	}

	if containsInputImage(payload["input"]) {
		return newProviderResponsesCompatibilityError("input_image content is not supported for provider Responses compatibility")
	}

	if toolType := unsupportedProviderResponsesToolType(payload["tools"]); toolType != "" {
		return newProviderResponsesCompatibilityError("unsupported Responses tool type: " + toolType)
	}

	if toolChoiceType := unsupportedProviderResponsesToolChoiceType(payload["tool_choice"]); toolChoiceType != "" {
		return newProviderResponsesCompatibilityError("unsupported Responses tool_choice type: " + toolChoiceType)
	}

	return nil
}

func newProviderResponsesCompatibilityError(message string) *ProviderResponsesCompatibilityError {
	return &ProviderResponsesCompatibilityError{
		StatusCode: http.StatusBadRequest,
		Type:       "invalid_request_error",
		Message:    message,
	}
}

func isProviderResponsesCompactPath(rawPath string) bool {
	normalized := strings.TrimSpace(rawPath)
	if normalized == "" {
		return false
	}
	if parsed, err := url.Parse(normalized); err == nil && parsed.Path != "" {
		normalized = parsed.Path
	} else if idx := strings.IndexAny(normalized, "?#"); idx >= 0 {
		normalized = normalized[:idx]
	}

	segments := strings.Split(strings.Trim(strings.ToLower(normalized), "/"), "/")
	for i := 0; i+1 < len(segments); i++ {
		if segments[i] == "responses" && segments[i+1] == "compact" {
			return true
		}
	}
	return false
}

func hasPreviousResponseID(value any) bool {
	if value == nil {
		return false
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s) != ""
	}
	return true
}

func containsInputImage(value any) bool {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if containsInputImage(item) {
				return true
			}
		}
	case map[string]any:
		if strings.TrimSpace(firstNonEmptyString(v["type"])) == "input_image" {
			return true
		}
		for _, item := range v {
			if containsInputImage(item) {
				return true
			}
		}
	}
	return false
}

func unsupportedProviderResponsesToolType(value any) string {
	tools, ok := value.([]any)
	if !ok {
		return ""
	}
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			return "unknown"
		}
		toolType := strings.TrimSpace(firstNonEmptyString(tool["type"]))
		if toolType != "function" {
			if toolType == "" {
				return "unknown"
			}
			return toolType
		}
	}
	return ""
}

func unsupportedProviderResponsesToolChoiceType(value any) string {
	if value == nil {
		return ""
	}
	switch choice := value.(type) {
	case string:
		choice = strings.TrimSpace(choice)
		switch choice {
		case "", "auto", "none", "required":
			return ""
		default:
			return choice
		}
	case map[string]any:
		choiceType := strings.TrimSpace(firstNonEmptyString(choice["type"]))
		if choiceType == "function" {
			if firstNonEmptyString(choice["name"]) == "" {
				return "function tool_choice name is required"
			}
			return ""
		}
		if choiceType == "" {
			return "unknown"
		}
		return choiceType
	default:
		return "unknown"
	}
}
