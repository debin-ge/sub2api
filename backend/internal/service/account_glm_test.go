package service

import "testing"

func TestAccountGLMHelpersUseFixedEndpoints(t *testing.T) {
	acc := &Account{
		Platform: PlatformGLM,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":            "  sk-glm-test \n",
			"base_url_anthropic": "https://proxy.example/anthropic",
			"base_url_openai":    "https://proxy.example/openai",
			"base_url":           "https://proxy.example/base",
		},
		Extra: map[string]any{
			"base_url_anthropic": "https://extra.example/anthropic",
			"base_url_openai":    "https://extra.example/openai",
		},
	}

	if !acc.IsGLM() {
		t.Fatalf("expected IsGLM true")
	}
	if !acc.IsGLMCodingPlan() {
		t.Fatalf("expected IsGLMCodingPlan true")
	}
	if got := acc.GetGLMAPIKey(); got != "sk-glm-test" {
		t.Fatalf("api key = %q", got)
	}
	if got := acc.GetGLMAnthropicBaseURL(); got != "https://open.bigmodel.cn/api/anthropic" {
		t.Fatalf("anthropic base url = %q", got)
	}
	if got := acc.GetGLMOpenAIBaseURL(); got != "https://open.bigmodel.cn/api/coding/paas/v4" {
		t.Fatalf("openai base url = %q", got)
	}
}

func TestAccountGLMModelMappingAndNormalization(t *testing.T) {
	acc := &Account{
		Platform: PlatformGLM,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-glm-test",
			"model_mapping": map[string]any{
				"custom-model": "GLM-custom",
			},
		},
	}

	cases := []struct {
		name  string
		model string
		want  string
	}{
		{name: "sonnet default", model: "claude-sonnet-4-5", want: "GLM-5.1"},
		{name: "opus default", model: "claude-opus-4-5", want: "GLM-5.1"},
		{name: "haiku default", model: "claude-haiku-4-5", want: "GLM-4.5-air"},
		{name: "glm 5.1 lower", model: " glm-5.1 ", want: "GLM-5.1"},
		{name: "glm 4.7 lower", model: "glm-4.7", want: "GLM-4.7"},
		{name: "glm 4.5 air lower", model: "glm-4.5-air", want: "GLM-4.5-air"},
		{name: "explicit mapping", model: "custom-model", want: "GLM-custom"},
		{name: "unknown passthrough trimmed", model: "  other-model  ", want: "other-model"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acc.GetGLMMappedModel(tc.model); got != tc.want {
				t.Fatalf("GetGLMMappedModel(%q) = %q, want %q", tc.model, got, tc.want)
			}
		})
	}
}

func TestAccountGLMInvalidAccountHelpers(t *testing.T) {
	acc := &Account{Platform: PlatformGLM, Type: AccountTypeOAuth, Credentials: map[string]any{"api_key": "sk-glm-test"}}
	if acc.IsGLMCodingPlan() {
		t.Fatalf("expected OAuth GLM account not to be coding plan")
	}

	other := &Account{Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-minimax"}}
	if other.IsGLM() {
		t.Fatalf("expected non-GLM account IsGLM false")
	}
	if got := other.GetGLMAPIKey(); got != "" {
		t.Fatalf("non-GLM api key = %q", got)
	}
}
