package service

import (
	"encoding/json"
	"errors"
	"strings"
)

type BenchmarkVerifierResult struct {
	Status          string
	Score           float64
	MaxScore        float64
	NormalizedScore float64
	Output          map[string]any
}

func VerifyBenchmarkResponse(verifierType string, config map[string]any, rawResponse string) (BenchmarkVerifierResult, error) {
	switch verifierType {
	case "exact_match":
		expected, _ := config["expected"].(string)
		pass := strings.TrimSpace(rawResponse) == strings.TrimSpace(expected)
		return verifierScore(pass), nil
	case "normalized_match":
		expected, _ := config["expected"].(string)
		pass := strings.EqualFold(strings.TrimSpace(rawResponse), strings.TrimSpace(expected))
		return verifierScore(pass), nil
	case "json_object":
		var parsed map[string]any
		if err := json.Unmarshal([]byte(rawResponse), &parsed); err != nil {
			return BenchmarkVerifierResult{Status: BenchmarkResultStatusParseError, MaxScore: 1, Output: map[string]any{"error": err.Error()}}, nil
		}
		for _, key := range anyStringSlice(config["required_keys"]) {
			if _, ok := parsed[key]; !ok {
				return verifierScore(false), nil
			}
		}
		return verifierScore(true), nil
	default:
		return BenchmarkVerifierResult{}, errors.New("unsupported benchmark verifier type")
	}
}

func verifierScore(pass bool) BenchmarkVerifierResult {
	if pass {
		return BenchmarkVerifierResult{Status: BenchmarkResultStatusScored, Score: 1, MaxScore: 1, NormalizedScore: 100}
	}
	return BenchmarkVerifierResult{Status: BenchmarkResultStatusScored, Score: 0, MaxScore: 1, NormalizedScore: 0}
}

func anyStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		stringsValue, ok := value.([]string)
		if ok {
			return stringsValue
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
