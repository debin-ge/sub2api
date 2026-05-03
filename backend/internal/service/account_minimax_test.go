package service

import "testing"

func TestAccountMiniMaxHelpers(t *testing.T) {
	acc := &Account{
		Platform: PlatformMiniMax,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":            "  sk-cp-test \n",
			"base_url_anthropic": " https://proxy.example/minimax/anthropic/ ",
			"base_url_openai":    " https://proxy.example/minimax/openai/ ",
			"model_mapping": map[string]any{
				"claude-sonnet-4-5": "MiniMax-M2.7",
			},
		},
	}

	if !acc.IsMiniMax() {
		t.Fatalf("expected IsMiniMax true")
	}
	if !acc.IsMiniMaxTokenPlan() {
		t.Fatalf("expected IsMiniMaxTokenPlan true")
	}
	if got := acc.GetMiniMaxAPIKey(); got != "sk-cp-test" {
		t.Fatalf("api key = %q", got)
	}
	if got := acc.GetMiniMaxAnthropicBaseURL(); got != "https://proxy.example/minimax/anthropic" {
		t.Fatalf("anthropic base url = %q", got)
	}
	if got := acc.GetMiniMaxOpenAIBaseURL(); got != "https://proxy.example/minimax/openai" {
		t.Fatalf("openai base url = %q", got)
	}
	if got := acc.GetMiniMaxMappedModel("claude-sonnet-4-5"); got != "MiniMax-M2.7" {
		t.Fatalf("mapped model = %q", got)
	}
}

func TestAccountMiniMaxDefaultBaseURLs(t *testing.T) {
	acc := &Account{
		Platform:    PlatformMiniMax,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-cp-test"},
	}

	if got := acc.GetMiniMaxAnthropicBaseURL(); got != "https://api.minimax.io/anthropic" {
		t.Fatalf("anthropic base url = %q", got)
	}
	if got := acc.GetMiniMaxOpenAIBaseURL(); got != "https://api.minimax.io/v1" {
		t.Fatalf("openai base url = %q", got)
	}
}
