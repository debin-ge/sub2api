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

func TestGatewayModelListProviderUnknownProvider(t *testing.T) {
	provider := NewGatewayModelListProvider(GatewayModelListOptions{})

	if models := provider.ModelsForProvider("unknown", nil); models != nil {
		t.Fatalf("ModelsForProvider unknown = %#v, want nil", models)
	}
}
