package service

import "testing"

func TestAccountKimiHelpersUseEditableEndpointsAndSingleModel(t *testing.T) {
	acc := &Account{
		Platform: PlatformKimi,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":            "  sk-kimi-test \n",
			"base_url_anthropic": " https://proxy.example/anthropic/ ",
			"base_url_openai":    " https://proxy.example/openai/ ",
			"model_mapping": map[string]any{
				"claude-sonnet-4-5": "kimi-for-coding",
			},
		},
	}

	if !acc.IsKimi() {
		t.Fatalf("expected IsKimi true")
	}
	if !acc.IsKimiCode() {
		t.Fatalf("expected IsKimiCode true")
	}
	if got := acc.GetKimiAPIKey(); got != "sk-kimi-test" {
		t.Fatalf("api key = %q", got)
	}
	if got := acc.GetKimiAnthropicBaseURL(); got != "https://proxy.example/anthropic" {
		t.Fatalf("anthropic base url = %q", got)
	}
	if got := acc.GetKimiOpenAIBaseURL(); got != "https://proxy.example/openai" {
		t.Fatalf("openai base url = %q", got)
	}
	if got := DefaultKimiModelIDs(); len(got) != 1 || got[0] != "kimi-for-coding" {
		t.Fatalf("default kimi models = %#v", got)
	}
	if !acc.IsKimiModelSupported("kimi-for-coding") {
		t.Fatalf("expected kimi-for-coding to be supported")
	}
	for _, model := range []string{"claude-sonnet-4-5", "claude-opus-4-5", "claude-haiku-4-5", "kimi-latest", "moonshot-v1-128k", " kimi-for-coding "} {
		if acc.IsKimiModelSupported(model) {
			t.Fatalf("model %q should not be supported", model)
		}
	}
}

func TestAccountKimiHelpersDefaultEndpoints(t *testing.T) {
	acc := &Account{
		Platform:    PlatformKimi,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-kimi-test"},
	}

	if got := acc.GetKimiAnthropicBaseURL(); got != "https://api.kimi.com/coding" {
		t.Fatalf("anthropic base url = %q", got)
	}
	if got := acc.GetKimiOpenAIBaseURL(); got != "https://api.kimi.com/coding/v1" {
		t.Fatalf("openai base url = %q", got)
	}
}

func TestAccountKimiInvalidAccountHelpers(t *testing.T) {
	oauth := &Account{Platform: PlatformKimi, Type: AccountTypeOAuth, Credentials: map[string]any{"api_key": "sk-kimi"}}
	if oauth.IsKimiCode() {
		t.Fatalf("expected OAuth Kimi account not to be coding plan")
	}

	missingKey := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": " "}}
	if missingKey.IsKimiCode() {
		t.Fatalf("expected missing API key not to be coding plan")
	}

	other := &Account{Platform: PlatformGLM, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-glm"}}
	if other.IsKimi() {
		t.Fatalf("expected non-Kimi account IsKimi false")
	}
	if got := other.GetKimiAPIKey(); got != "" {
		t.Fatalf("non-Kimi api key = %q", got)
	}
}
