package service

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

var videoProviderMetadataKeys = []string{
	"model",
	"quality",
	"region",
	"seconds",
	"service_tier",
	"size",
}

func sanitizeVideoProviderMetadata(values map[string]any) map[string]any {
	clean := make(map[string]any)
	for _, key := range videoProviderMetadataKeys {
		value, ok := sanitizeVideoProviderScalar(values[key])
		if text, isText := value.(string); isText && strings.Contains(text, "://") {
			ok = false
		}
		if ok {
			clean[key] = value
		}
	}
	for _, key := range []string{"specification_invalid", "execution_spec_conflict"} {
		if videoSpecificationConflictMarker(values, key) {
			clean[key] = float64(1)
		}
	}
	for _, key := range []string{"model", "size", "seconds"} {
		if value, exists := values[key]; exists && value != nil && value != "" {
			if _, retained := clean[key]; !retained {
				clean["specification_invalid"] = float64(1)
			}
		}
	}
	return clean
}

func sanitizeVideoProviderUsage(values map[string]any) map[string]any {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	clean := make(map[string]any)
	for _, key := range keys {
		if len(clean) >= 32 || !isSafeVideoProviderUsageKey(key) {
			continue
		}
		value, ok := numericMapValue(values, key)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			continue
		}
		clean[key] = value
	}
	for _, alias := range []string{"video_tokens", "completion_tokens", "output_tokens"} {
		delete(clean, alias)
	}
	if tokens, present, conflict := canonicalVideoTokenUsage(values); conflict {
		clean["video_tokens_conflict"] = float64(1)
	} else if present {
		clean["video_tokens"] = tokens
	}
	for _, measure := range []struct {
		name    string
		aliases []string
	}{
		{"seconds", []string{"billable_seconds", "seconds"}},
		{"requests", []string{"billable_requests", "requests"}},
	} {
		for _, alias := range measure.aliases {
			delete(clean, alias)
		}
		marker := "video_" + measure.name + "_conflict"
		if quantity, present, conflict := canonicalVideoUsageMeasure(values, measure.aliases, marker); conflict {
			clean[marker] = float64(1)
		} else if present {
			clean[measure.name] = quantity
		}
	}
	return clean
}

func canonicalVideoTokenUsage(values map[string]any) (tokens float64, present, conflict bool) {
	return canonicalVideoUsageMeasure(values, []string{"video_tokens", "completion_tokens", "output_tokens"}, "video_tokens_conflict")
}

func canonicalVideoUsageMeasure(values map[string]any, aliases []string, conflictKey string) (quantity float64, present, conflict bool) {
	if marker, exists := numericMapValue(values, conflictKey); exists && marker != 0 {
		return 0, false, true
	}
	first := true
	for _, key := range aliases {
		raw, exists := values[key]
		if !exists {
			continue
		}
		value, ok := numericMapValue(map[string]any{key: raw}, key)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return 0, false, true
		}
		if first {
			quantity, present, first = value, true, false
			continue
		}
		if value != quantity {
			return 0, false, true
		}
	}
	return quantity, present, false
}

func sanitizeVideoProviderScalar(value any) (any, bool) {
	switch value := value.(type) {
	case string:
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 256 || !utf8.ValidString(value) || containsVideoProviderSecret(value) {
			return nil, false
		}
		for _, character := range value {
			if unicode.IsControl(character) {
				return nil, false
			}
		}
		return value, true
	case bool:
		return value, true
	case float64:
		return value, !math.IsNaN(value) && !math.IsInf(value, 0)
	case float32:
		converted := float64(value)
		return converted, !math.IsNaN(converted) && !math.IsInf(converted, 0)
	case int:
		return value, true
	case int64:
		return value, true
	case json.Number:
		converted, err := value.Float64()
		return converted, err == nil && !math.IsNaN(converted) && !math.IsInf(converted, 0)
	default:
		return nil, false
	}
}

func isSafeVideoProviderUsageKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 64 {
		return false
	}
	lower := strings.ToLower(key)
	for _, fragment := range []string{"auth", "cookie", "credential", "key", "password", "secret", "token_value", "url"} {
		if strings.Contains(lower, fragment) {
			return false
		}
	}
	for _, character := range key {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func containsVideoProviderSecret(value string) bool {
	lower := strings.ToLower(value)
	for _, fragment := range []string{"bearer ", "basic ", "sk-", "api_key=", "apikey=", "access_token=", "refresh_token=", "signature=", "x-amz-signature="} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return strings.Contains(value, "?") && (strings.Contains(lower, "http://") || strings.Contains(lower, "https://"))
}

func validVideoProviderIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}

func validVideoProviderName(value string) bool {
	if value == "" || len(value) > 32 || value != strings.ToLower(strings.TrimSpace(value)) || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		character := value[i]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func boundedVideoProviderStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 64 {
		return "unknown"
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && character != '_' && character != '-' {
			return "unknown"
		}
	}
	return value
}

func boundedVideoProviderKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 32 {
		return "upstream"
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') &&
			character != '_' && character != '-' {
			return "upstream"
		}
	}
	return value
}

func boundedVideoProviderCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 128 || !utf8.ValidString(value) {
		return "upstream_error"
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '_' && character != '-' && character != '.' {
			return "upstream_error"
		}
	}
	return value
}

func boundedVideoProviderMessage(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	value = boundedProviderMessage(value)
	if containsVideoProviderSecret(value) {
		return fallback
	}
	return value
}

func sanitizedVideoProviderError(value *VideoProviderError, fallbackKind, fallbackCode, fallbackMessage string) *VideoProviderError {
	if value == nil {
		return &VideoProviderError{
			Kind: boundedVideoProviderKind(fallbackKind), Code: boundedVideoProviderCode(fallbackCode),
			Message: fallbackMessage,
		}
	}
	clean := *value
	clean.Kind = boundedVideoProviderKind(value.Kind)
	if strings.TrimSpace(value.Kind) == "" {
		clean.Kind = boundedVideoProviderKind(fallbackKind)
	}
	clean.Code = boundedVideoProviderCode(value.Code)
	if clean.Code == "" {
		clean.Code = boundedVideoProviderCode(fallbackCode)
	}
	clean.Message = boundedVideoProviderMessage(value.Message, fallbackMessage)
	if clean.Message == "" {
		clean.Message = fallbackMessage
	}
	return &clean
}

func normalizedVideoProviderGenerationState(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case VideoGenerationQueued:
		return VideoGenerationQueued
	case VideoGenerationInProgress:
		return VideoGenerationInProgress
	case VideoGenerationCompleted:
		return VideoGenerationCompleted
	case VideoGenerationFailed:
		return VideoGenerationFailed
	case VideoGenerationCancelled:
		return VideoGenerationCancelled
	case VideoGenerationExpired:
		return VideoGenerationExpired
	default:
		switch fallback {
		case VideoGenerationQueued, VideoGenerationInProgress, VideoGenerationCompleted,
			VideoGenerationFailed, VideoGenerationCancelled, VideoGenerationExpired:
			return fallback
		default:
			return VideoGenerationInProgress
		}
	}
}

func boundedVideoProviderProgress(value *float64) *float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 100 {
		return nil
	}
	progress := *value
	return &progress
}

func sanitizeVideoContentVariants(values []string) []string {
	if values == nil {
		return nil
	}
	clean := make([]string, 0, min(len(values), 16))
	seen := make(map[string]struct{}, min(len(values), 16))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || len(value) > 64 {
			continue
		}
		valid := true
		for _, character := range value {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') &&
				character != '_' && character != '-' {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		clean = append(clean, value)
		if len(clean) == 16 {
			break
		}
	}
	return clean
}

func sanitizeVideoProviderEventPayload(values map[string]any) map[string]any {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	clean := make(map[string]any)
	for _, key := range keys {
		if len(clean) >= 32 || !isSafeVideoProviderUsageKey(key) {
			continue
		}
		value, ok := sanitizeVideoProviderScalar(values[key])
		if ok {
			clean[key] = value
		}
	}
	return clean
}
