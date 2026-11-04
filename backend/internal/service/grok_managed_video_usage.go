package service

import (
	"encoding/json"
	"strings"
)

// These are supplier diagnostics, not the video-token quantities used by the
// shared billing engine. In particular, xAI output_tokens may count rewritten
// text and must never become the engine's canonical video_tokens.
var grokManagedUsageFields = []string{
	"cost_in_usd_ticks", "input_tokens", "output_tokens", "total_tokens", "prompt_tokens", "completion_tokens",
	"input_tokens_details.cached_tokens", "input_tokens_details.text_tokens", "input_tokens_details.image_tokens", "input_tokens_details.audio_tokens", "input_tokens_details.video_tokens",
	"output_tokens_details.text_tokens", "output_tokens_details.audio_tokens", "output_tokens_details.reasoning_tokens",
}

func grokManagedUsageKey(path string) string {
	if path == "cost_in_usd_ticks" {
		return "provider_cost_usd_ticks"
	}
	return "grok_" + strings.ReplaceAll(path, ".", "_")
}

func grokManagedUsageSnapshot(raw json.RawMessage) map[string]any {
	result := make(map[string]any)
	for _, path := range grokManagedUsageFields {
		value := raw
		for _, part := range strings.Split(path, ".") {
			var object map[string]json.RawMessage
			if json.Unmarshal(value, &object) != nil {
				value = nil
				break
			}
			value = object[part]
		}
		// Only JSON numbers are retained. Malformed optional diagnostics must not
		// hide a completed video or delay release/capture of its funds.
		if len(value) == 0 || value[0] < '0' || value[0] > '9' {
			continue
		}
		var number json.Number
		if json.Unmarshal(value, &number) != nil {
			continue
		}
		if n, err := number.Int64(); err == nil && n >= 0 {
			result[grokManagedUsageKey(path)] = n
		}
	}
	return result
}

// GrokManagedPublicUsage reconstructs the allowlisted supplier wire shape from
// persisted numeric diagnostics without exposing generic billing internals.
func GrokManagedPublicUsage(snapshot map[string]any) map[string]any {
	result := make(map[string]any)
	for _, path := range grokManagedUsageFields {
		value, ok := numericMapValue(snapshot, grokManagedUsageKey(path))
		if !ok || !finiteNonNegative(value) {
			continue
		}
		parts := strings.Split(path, ".")
		if len(parts) == 1 {
			result[path] = value
			continue
		}
		nested, _ := result[parts[0]].(map[string]any)
		if nested == nil {
			nested = make(map[string]any)
			result[parts[0]] = nested
		}
		nested[parts[1]] = value
	}
	return result
}
