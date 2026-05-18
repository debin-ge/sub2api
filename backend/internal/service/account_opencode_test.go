package service

import "testing"

func TestAccountOpenCodeHelpersUseAPIKeyBaseURLAndMappedModels(t *testing.T) {
	acc := &Account{
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "  sk-opencode-test \n",
			"base_url": " https://proxy.example/opencode/ ",
			"model_mapping": map[string]any{
				"gpt-5": "opencode/gpt5-nano",
			},
		},
	}

	if !acc.IsOpenCode() {
		t.Fatalf("expected IsOpenCode true")
	}
	if !acc.IsOpenCodeAPIKey() {
		t.Fatalf("expected IsOpenCodeAPIKey true")
	}
	if got := acc.GetOpenCodeAPIKey(); got != "sk-opencode-test" {
		t.Fatalf("api key = %q", got)
	}
	if got := acc.GetOpenCodeBaseURL(); got != "https://proxy.example/opencode" {
		t.Fatalf("base url = %q", got)
	}
	assertStringSlicesEqual(t, DefaultOpenCodeModelIDs(), []string{"opencode/big-pickle", "opencode/gpt5-nano", "gpt5-nano"})

	for _, model := range []string{"opencode/big-pickle", "opencode/gpt5-nano", "gpt5-nano", "gpt-5"} {
		if !acc.IsOpenCodeModelSupported(model) {
			t.Fatalf("model %q should be supported", model)
		}
	}
	for _, model := range []string{"deepseek-chat", "claude-sonnet-4-6"} {
		if acc.IsOpenCodeModelSupported(model) {
			t.Fatalf("model %q should not be supported", model)
		}
	}
	if got := acc.GetOpenCodeMappedModel("gpt-5"); got != "opencode/gpt5-nano" {
		t.Fatalf("GetOpenCodeMappedModel(gpt-5) = %q", got)
	}
	if got := acc.GetOpenCodeMappedModel("gpt5-nano"); got != "gpt5-nano" {
		t.Fatalf("GetOpenCodeMappedModel(gpt5-nano) = %q", got)
	}
}

func TestAccountOpenCodeRequiresConfiguredBaseURL(t *testing.T) {
	acc := &Account{
		Platform:    PlatformOpenCode,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-opencode-test"},
	}

	if got := acc.GetOpenCodeBaseURL(); got != "" {
		t.Fatalf("base url = %q, want empty", got)
	}
}

func TestAccountOpenCodeInvalidAccountHelpers(t *testing.T) {
	oauth := &Account{Platform: PlatformOpenCode, Type: AccountTypeOAuth, Credentials: map[string]any{"api_key": "sk-opencode"}}
	if oauth.IsOpenCodeAPIKey() {
		t.Fatalf("expected OAuth OpenCode account not to be API key gateway")
	}

	missingKey := &Account{Platform: PlatformOpenCode, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": " "}}
	if missingKey.IsOpenCodeAPIKey() {
		t.Fatalf("expected missing API key not to be OpenCode API key gateway")
	}

	other := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-kimi"}}
	if other.IsOpenCode() {
		t.Fatalf("expected non-OpenCode account IsOpenCode false")
	}
	if got := other.GetOpenCodeAPIKey(); got != "" {
		t.Fatalf("non-OpenCode api key = %q", got)
	}
}
