package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderResponsesCompatibilityAllowsCodexCommonShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "string_input", body: `{"model":"glm-5.1","input":"hello"}`},
		{name: "message_array_input", body: `{"model":"glm-5.1","input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]}`},
		{name: "instructions", body: `{"model":"glm-5.1","instructions":"be brief","input":"hello"}`},
		{name: "function_tool", body: `{"model":"glm-5.1","input":"hello","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"tool_choice":"auto"}`},
		{name: "reasoning_effort", body: `{"model":"glm-5.1","input":"hello","reasoning":{"effort":"high","summary":"auto"}}`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(tc.body))
			require.NoError(t, err)
		})
	}
}

func TestProviderResponsesCompatibilityRejectsUnsupportedCodexShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "previous_response_id", body: `{"model":"glm-5.1","previous_response_id":"resp_1","input":[{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`, want: "previous_response_id"},
		{name: "image_tool", body: `{"model":"glm-5.1","input":"draw","tools":[{"type":"image_generation"}]}`, want: "image_generation"},
		{name: "web_search", body: `{"model":"glm-5.1","input":"search","tools":[{"type":"web_search"}]}`, want: "web_search"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(tc.body))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
