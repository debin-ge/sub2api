package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateProviderResponsesCompatibilityRequest(t *testing.T) {
	t.Parallel()

	t.Run("allows common codex shaped request", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{
			"model":"gpt-5",
			"instructions":"answer concisely",
			"input":[
				{"role":"user","content":[{"type":"input_text","text":"lookup weather"}]},
				{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"city\":\"Paris\"}"},
				{"type":"function_call_output","call_id":"call_1","output":"sunny"}
			],
			"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],
			"tool_choice":{"type":"function","name":"lookup"},
			"reasoning":{"effort":"medium"},
			"stream":true
		}`)

		require.NoError(t, ValidateProviderResponsesCompatibilityRequest("/v1/responses", body))
	})

	t.Run("allows string input and normal tool choice values", func(t *testing.T) {
		t.Parallel()

		for _, choice := range []string{"auto", "none", "required"} {
			body := []byte(`{"model":"gpt-5","input":"hello","tool_choice":"` + choice + `"}`)
			require.NoError(t, ValidateProviderResponsesCompatibilityRequest("/responses", body), "choice=%s", choice)
		}
	})

	t.Run("rejects invalid json", func(t *testing.T) {
		t.Parallel()

		err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{"model":"gpt-5"`))

		requireProviderResponsesCompatibilityError(t, err, "invalid JSON")
	})

	t.Run("rejects missing model", func(t *testing.T) {
		t.Parallel()

		err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{"input":"hello"}`))

		requireProviderResponsesCompatibilityError(t, err, "model")
	})

	t.Run("rejects non string model", func(t *testing.T) {
		t.Parallel()

		err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{"model":123,"input":"hello"}`))

		requireProviderResponsesCompatibilityError(t, err, "model")
	})

	t.Run("rejects empty model", func(t *testing.T) {
		t.Parallel()

		err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{"model":"  ","input":"hello"}`))

		requireProviderResponsesCompatibilityError(t, err, "model")
	})

	t.Run("rejects compact paths", func(t *testing.T) {
		t.Parallel()

		paths := []string{
			"/v1/responses/compact",
			"/v1/responses/compact/",
			"/v1/responses/compact?foo=bar",
			"/responses/compact/extra",
			"/backend-api/codex/responses/compact",
			"/backend-api/codex/responses/compact/detail?foo=bar",
		}
		for _, path := range paths {
			err := ValidateProviderResponsesCompatibilityRequest(path, []byte(`{"model":"gpt-5","input":"hello"}`))
			requireProviderResponsesCompatibilityError(t, err, "compact")
		}
	})

	t.Run("rejects previous response id", func(t *testing.T) {
		t.Parallel()

		err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{"model":"gpt-5","previous_response_id":"resp_123","input":"hello"}`))

		requireProviderResponsesCompatibilityError(t, err, "previous_response_id")
	})

	t.Run("allows null and empty previous response id", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{"model":"gpt-5","previous_response_id":null,"input":"hello"}`)))
		require.NoError(t, ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{"model":"gpt-5","previous_response_id":"","input":"hello"}`)))
	})

	t.Run("rejects image generation intent", func(t *testing.T) {
		t.Parallel()

		err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{"model":"gpt-5","input":"draw a logo","tools":[{"type":"image_generation"}]}`))

		requireProviderResponsesCompatibilityError(t, err, "image")
	})

	t.Run("rejects top level input image", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"model":"gpt-5","input":[{"type":"input_image","image_url":"data:image/png;base64,aaa"}]}`)

		err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", body)

		requireProviderResponsesCompatibilityError(t, err, "input_image")
	})

	t.Run("rejects nested input image", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"model":"gpt-5","input":[{"role":"user","content":[{"type":"input_text","text":"describe this"},{"type":"input_image","image_url":"data:image/png;base64,aaa"}]}]}`)

		err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", body)

		requireProviderResponsesCompatibilityError(t, err, "input_image")
	})

	t.Run("rejects unsupported tool types", func(t *testing.T) {
		t.Parallel()

		for _, toolType := range []string{"image_generation", "web_search", "local_shell", "mcp"} {
			body := []byte(`{"model":"gpt-5","input":"hello","tools":[{"type":"` + toolType + `"}]}`)

			err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", body)

			requireProviderResponsesCompatibilityError(t, err, toolType)
		}
	})

	t.Run("rejects unknown tool type", func(t *testing.T) {
		t.Parallel()

		err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{"model":"gpt-5","input":"hello","tools":[{"type":"custom_builtin"}]}`))

		requireProviderResponsesCompatibilityError(t, err, "custom_builtin")
	})

	t.Run("rejects unsupported tool choice object", func(t *testing.T) {
		t.Parallel()

		err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{"model":"gpt-5","input":"hello","tool_choice":{"type":"web_search"}}`))

		requireProviderResponsesCompatibilityError(t, err, "web_search")
	})

	t.Run("rejects malformed function tool choice objects", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			body []byte
		}{
			{
				name: "missing name",
				body: []byte(`{"model":"gpt-5","input":"hello","tool_choice":{"type":"function"}}`),
			},
			{
				name: "blank name",
				body: []byte(`{"model":"gpt-5","input":"hello","tool_choice":{"type":"function","name":"  "}}`),
			},
			{
				name: "nested only name",
				body: []byte(`{"model":"gpt-5","input":"hello","tool_choice":{"type":"function","function":{"name":"lookup"}}}`),
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", tc.body)

				requireProviderResponsesCompatibilityError(t, err, "tool_choice")
				requireProviderResponsesCompatibilityError(t, err, "name")
			})
		}
	})

	t.Run("rejects unsupported tool choice string", func(t *testing.T) {
		t.Parallel()

		err := ValidateProviderResponsesCompatibilityRequest("/v1/responses", []byte(`{"model":"gpt-5","input":"hello","tool_choice":"image_generation"}`))

		requireProviderResponsesCompatibilityError(t, err, "image_generation")
	})
}

func requireProviderResponsesCompatibilityError(t *testing.T, err error, messageContains string) {
	t.Helper()

	var compatErr *ProviderResponsesCompatibilityError
	require.ErrorAs(t, err, &compatErr)
	require.Equal(t, http.StatusBadRequest, compatErr.StatusCode)
	require.Equal(t, "invalid_request_error", compatErr.Type)
	require.Contains(t, compatErr.Message, messageContains)
	require.Equal(t, compatErr.Message, compatErr.Error())
	require.True(t, errors.Is(err, compatErr))
}
