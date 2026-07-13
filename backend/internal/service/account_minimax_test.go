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

func TestAccountMiniMaxExplicitMappingRestrictsModelsAndAllowsNewTargets(t *testing.T) {
	acc := &Account{
		Platform: PlatformMiniMax,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-cp-test",
			"model_mapping": map[string]any{
				"MiniMax-M3": "MiniMax-M3",
			},
		},
	}

	svc := &GatewayService{}

	if !svc.isModelSupportedByAccount(acc, "MiniMax-M3") {
		t.Fatalf("expected explicitly configured MiniMax-M3 to be schedulable")
	}
	if got := acc.GetMiniMaxMappedModel("MiniMax-M3"); got != "MiniMax-M3" {
		t.Fatalf("mapped MiniMax-M3 model = %q", got)
	}
	if svc.isModelSupportedByAccount(acc, "MiniMax-M2.7-highspeed") {
		t.Fatalf("expected unconfigured model to remain unsupported when mapping is configured")
	}
	if svc.isModelSupportedByAccount(acc, "unknown-model") {
		t.Fatalf("expected unknown model to remain unsupported when mapping is configured")
	}
}

func TestAccountMiniMaxWithoutMappingAllowsFutureModels(t *testing.T) {
	acc := &Account{
		Platform:    PlatformMiniMax,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-cp-test"},
	}

	for _, model := range []string{"MiniMax-M3", "MiniMax-future"} {
		if !acc.IsMiniMaxModelSupported(model) {
			t.Fatalf("expected future model %q to pass through", model)
		}
		if got := acc.GetMiniMaxMappedModel(model); got != model {
			t.Fatalf("GetMiniMaxMappedModel(%q) = %q, want passthrough", model, got)
		}
	}
}

func TestAccountMiniMaxDefaultAliases(t *testing.T) {
	acc := &Account{
		Platform:    PlatformMiniMax,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-cp-test"},
	}

	cases := []struct {
		model string
		want  string
	}{
		{model: "claude-sonnet-4-5", want: "MiniMax-M2.7"},
		{model: "claude-3-5-sonnet-latest", want: "MiniMax-M2.7"},
		{model: "claude-haiku-3-5", want: "MiniMax-M2.7-highspeed"},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			if !acc.IsMiniMaxModelSupported(tc.model) {
				t.Fatalf("expected %q to be supported", tc.model)
			}
			if got := acc.GetMiniMaxMappedModel(tc.model); got != tc.want {
				t.Fatalf("GetMiniMaxMappedModel(%q) = %q, want %q", tc.model, got, tc.want)
			}
		})
	}
}
