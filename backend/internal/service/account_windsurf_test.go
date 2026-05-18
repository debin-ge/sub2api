package service

import "testing"

func TestAccountWindsurfHelpersUseSingleBaseURLAndMappedModels(t *testing.T) {
	acc := &Account{
		Platform: PlatformWindsurf,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "  sk-windsurf-test \n",
			"base_url": " https://proxy.example/windsurf/ ",
			"model_mapping": map[string]any{
				"claude-3-5-sonnet-latest": "claude-sonnet-4.6",
				"opus":                     "claude-opus-4.6",
			},
		},
	}

	if !acc.IsWindsurf() {
		t.Fatalf("expected IsWindsurf true")
	}
	if !acc.IsWindsurfAPIKey() {
		t.Fatalf("expected IsWindsurfAPIKey true")
	}
	if got := acc.GetWindsurfAPIKey(); got != "sk-windsurf-test" {
		t.Fatalf("api key = %q", got)
	}
	if got := acc.GetWindsurfBaseURL(); got != "https://proxy.example/windsurf" {
		t.Fatalf("base url = %q", got)
	}
	if got := DefaultWindsurfModelIDs(); len(got) < 4 || got[0] != "claude-sonnet-4-6" || got[1] != "claude-sonnet-4-6-thinking" {
		t.Fatalf("default windsurf models = %#v", got)
	}

	for _, model := range []string{
		"claude-sonnet-4-6",
		"claude-opus-4-7-xhigh",
		"gpt-5-5-xhigh-priority",
		"claude-sonnet-4.6",
		" claude-opus-4.6 ",
		"gpt-5.4",
		"swe-1.6",
		"claude-3-5-sonnet-latest",
		"opus",
	} {
		if !acc.IsWindsurfModelSupported(model) {
			t.Fatalf("model %q should be supported", model)
		}
	}
	for _, model := range []string{"swe-1-mini", "swe-grep", "deepseek-chat", "kimi-for-coding"} {
		if acc.IsWindsurfModelSupported(model) {
			t.Fatalf("model %q should not be supported", model)
		}
	}
	if got := acc.GetWindsurfMappedModel("claude-3-5-sonnet-latest"); got != "claude-sonnet-4.6" {
		t.Fatalf("GetWindsurfMappedModel(claude-3-5-sonnet-latest) = %q", got)
	}
	if got := acc.GetWindsurfMappedModel("opus"); got != "claude-opus-4.6" {
		t.Fatalf("GetWindsurfMappedModel(opus) = %q", got)
	}
	if got := acc.GetWindsurfMappedModel("gpt-5.4"); got != "gpt-5.4" {
		t.Fatalf("GetWindsurfMappedModel(gpt-5.4) = %q", got)
	}
}

func TestAccountWindsurfHelpersDefaultBaseURL(t *testing.T) {
	acc := &Account{
		Platform:    PlatformWindsurf,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-windsurf-test"},
	}

	if got := acc.GetWindsurfBaseURL(); got != "https://tik.frontech.dev:3003" {
		t.Fatalf("base url = %q", got)
	}
}

func TestAccountWindsurfInvalidAccountHelpers(t *testing.T) {
	oauth := &Account{Platform: PlatformWindsurf, Type: AccountTypeOAuth, Credentials: map[string]any{"api_key": "sk-windsurf"}}
	if oauth.IsWindsurfAPIKey() {
		t.Fatalf("expected OAuth Windsurf account not to be API key gateway")
	}

	missingKey := &Account{Platform: PlatformWindsurf, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": " "}}
	if missingKey.IsWindsurfAPIKey() {
		t.Fatalf("expected missing API key not to be Windsurf API key gateway")
	}

	other := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-kimi"}}
	if other.IsWindsurf() {
		t.Fatalf("expected non-Windsurf account IsWindsurf false")
	}
	if got := other.GetWindsurfAPIKey(); got != "" {
		t.Fatalf("non-Windsurf api key = %q", got)
	}
}
