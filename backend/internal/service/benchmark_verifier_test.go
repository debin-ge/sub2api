package service

import "testing"

func TestBenchmarkVerifierExactMatch(t *testing.T) {
	t.Parallel()

	result, err := VerifyBenchmarkResponse("exact_match", map[string]any{"expected": "Paris"}, " Paris ")
	if err != nil {
		t.Fatalf("VerifyBenchmarkResponse error = %v", err)
	}
	if result.NormalizedScore != 100 || result.Status != BenchmarkResultStatusScored {
		t.Fatalf("result = %#v", result)
	}
}

func TestBenchmarkVerifierJSONSchemaObject(t *testing.T) {
	t.Parallel()

	result, err := VerifyBenchmarkResponse("json_object", map[string]any{"required_keys": []any{"answer", "confidence"}}, `{"answer":"A","confidence":0.9}`)
	if err != nil {
		t.Fatalf("VerifyBenchmarkResponse error = %v", err)
	}
	if result.NormalizedScore != 100 {
		t.Fatalf("score = %v", result.NormalizedScore)
	}
}

func TestBenchmarkVerifierJSONSchemaObjectParseError(t *testing.T) {
	t.Parallel()

	result, err := VerifyBenchmarkResponse("json_object", map[string]any{"required_keys": []any{"answer"}}, `not-json`)
	if err != nil {
		t.Fatalf("VerifyBenchmarkResponse returned unexpected hard error = %v", err)
	}
	if result.Status != BenchmarkResultStatusParseError {
		t.Fatalf("status = %s", result.Status)
	}
}
