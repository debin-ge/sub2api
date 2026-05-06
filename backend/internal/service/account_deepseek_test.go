package service

import "testing"

func TestAccountDeepSeekHelpersUseFixedEndpointsAndOfficialModels(t *testing.T) {
	acc := &Account{
		Platform: PlatformDeepSeek,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":            "  sk-deepseek-test \n",
			"base_url":           "https://proxy.example",
			"base_url_anthropic": "https://proxy.example/anthropic",
			"base_url_openai":    "https://proxy.example/openai",
			"model_mapping": map[string]any{
				"claude-sonnet-4-5": "deepseek-v4-pro",
			},
		},
	}

	if !acc.IsDeepSeek() {
		t.Fatalf("expected IsDeepSeek true")
	}
	if !acc.IsDeepSeekAPIKey() {
		t.Fatalf("expected IsDeepSeekAPIKey true")
	}
	if got := acc.GetDeepSeekAPIKey(); got != "sk-deepseek-test" {
		t.Fatalf("api key = %q", got)
	}
	if got := acc.GetDeepSeekOpenAIBaseURL(); got != "https://api.deepseek.com" {
		t.Fatalf("openai base url = %q", got)
	}
	if got := acc.GetDeepSeekAnthropicBaseURL(); got != "https://api.deepseek.com/anthropic" {
		t.Fatalf("anthropic base url = %q", got)
	}
	if got := DefaultDeepSeekModelIDs(); len(got) != 2 || got[0] != "deepseek-v4-flash" || got[1] != "deepseek-v4-pro" {
		t.Fatalf("default deepseek models = %#v", got)
	}

	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-pro"} {
		if !acc.IsDeepSeekModelSupported(model) {
			t.Fatalf("model %q should be supported", model)
		}
	}
	for _, model := range []string{"deepseek-chat", "deepseek-reasoner", "claude-sonnet-4-5", "gpt-5.4", "kimi-for-coding", " deepseek-v4-flash "} {
		if acc.IsDeepSeekModelSupported(model) {
			t.Fatalf("model %q should not be supported", model)
		}
	}
}

func TestAccountDeepSeekInvalidAccountHelpers(t *testing.T) {
	oauth := &Account{Platform: PlatformDeepSeek, Type: AccountTypeOAuth, Credentials: map[string]any{"api_key": "sk-deepseek"}}
	if oauth.IsDeepSeekAPIKey() {
		t.Fatalf("expected OAuth DeepSeek account not to be API key gateway")
	}

	missingKey := &Account{Platform: PlatformDeepSeek, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": " "}}
	if missingKey.IsDeepSeekAPIKey() {
		t.Fatalf("expected missing API key not to be DeepSeek API key gateway")
	}

	other := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-kimi"}}
	if other.IsDeepSeek() {
		t.Fatalf("expected non-DeepSeek account IsDeepSeek false")
	}
	if got := other.GetDeepSeekAPIKey(); got != "" {
		t.Fatalf("non-DeepSeek api key = %q", got)
	}
}
