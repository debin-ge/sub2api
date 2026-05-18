package service

import "testing"

func TestGatewayModelListProviderDefaultsAndAccountMappings(t *testing.T) {
	provider := NewGatewayModelListProvider(GatewayModelListOptions{})
	accounts := []Account{
		{
			Platform: PlatformMiniMax,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"custom-sonnet": "MiniMax-M2.7",
					"custom-haiku":  "MiniMax-M2.7-highspeed",
					"wildcard-*":    "MiniMax-M2.7",
					"bad-target":    "unknown-model",
				},
			},
		},
		{
			Platform: PlatformGLM,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"glm-custom": "GLM-5.1",
				},
			},
		},
	}

	models := provider.ModelsForProvider(PlatformMiniMax, accounts)
	assertStringSlicesEqual(t, models, []string{
		"MiniMax-M2.7",
		"MiniMax-M2.7-highspeed",
		"custom-haiku",
		"custom-sonnet",
	})
}

func TestGatewayModelListProviderIncludesAliasesWhenConfigured(t *testing.T) {
	provider := NewGatewayModelListProvider(GatewayModelListOptions{IncludeAliases: true})

	models := provider.ModelsForProvider(PlatformDeepSeek, nil)

	assertStringSlicesEqual(t, models, []string{
		"deepseek-chat",
		"deepseek-r1",
		"deepseek-reasoner",
		"deepseek-v3",
		"deepseek-v4-flash",
		"deepseek-v4-pro",
	})
}

func TestGatewayModelListProviderWindsurfDefaultsAndAccountMappings(t *testing.T) {
	provider := NewGatewayModelListProvider(GatewayModelListOptions{})
	accounts := []Account{
		{
			Platform: PlatformWindsurf,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-3-5-sonnet-latest": "claude-sonnet-4.6",
					"opus":                     "claude-opus-4.6",
					"wildcard-*":               "claude-sonnet-4.6",
					"bad-target":               "swe-grep",
				},
			},
		},
	}

	models := provider.ModelsForProvider(PlatformWindsurf, accounts)
	mustContainStrings(t, models, []string{
		"claude-sonnet-4-6",
		"claude-opus-4-7-xhigh",
		"claude-opus-4-7-max",
		"gpt-5-5-high",
		"gpt-5-5-xhigh-priority",
		"gpt-5-4-high",
		"swe-1-6",
		"claude-3-5-sonnet-latest",
		"opus",
	})
	mustNotContainStrings(t, models, []string{"swe-grep", "swe-1-mini", "wildcard-*", "bad-target"})
}

func TestGatewayModelListProviderOpenCodeDefaultsAndAccountMappings(t *testing.T) {
	provider := NewGatewayModelListProvider(GatewayModelListOptions{})
	accounts := []Account{
		{
			Platform: PlatformOpenCode,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5":      "opencode/gpt5-nano",
					"fast":       "gpt5-nano",
					"wildcard-*": "opencode/gpt5-nano",
					"bad-target": "claude-sonnet-4-6",
				},
			},
		},
	}

	models := provider.ModelsForProvider(PlatformOpenCode, accounts)
	assertStringSlicesEqual(t, models, []string{
		"fast",
		"gpt-5",
		"gpt5-nano",
		"opencode/big-pickle",
		"opencode/gpt5-nano",
	})
}

func TestGatewayModelListProviderUnknownProvider(t *testing.T) {
	provider := NewGatewayModelListProvider(GatewayModelListOptions{})

	if models := provider.ModelsForProvider("unknown", nil); models != nil {
		t.Fatalf("ModelsForProvider unknown = %#v, want nil", models)
	}
}

func mustContainStrings(t *testing.T, got []string, want []string) {
	t.Helper()
	set := make(map[string]struct{}, len(got))
	for _, item := range got {
		set[item] = struct{}{}
	}
	for _, item := range want {
		if _, ok := set[item]; !ok {
			t.Fatalf("missing %q in %#v", item, got)
		}
	}
}

func mustNotContainStrings(t *testing.T, got []string, blocked []string) {
	t.Helper()
	set := make(map[string]struct{}, len(got))
	for _, item := range got {
		set[item] = struct{}{}
	}
	for _, item := range blocked {
		if _, ok := set[item]; ok {
			t.Fatalf("unexpected %q in %#v", item, got)
		}
	}
}
