package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestGatewayServiceGetAvailableModelsDomesticDefaultsWhenNoAccounts(t *testing.T) {
	svc := &GatewayService{
		accountRepo: &modelsListAccountRepoStub{},
		cfg:         &config.Config{},
	}

	models := svc.GetAvailableModels(context.Background(), nil, PlatformMiniMax)

	assertStringSlicesEqual(t, models, []string{"MiniMax-M2.7", "MiniMax-M2.7-highspeed"})
}

func TestGatewayServiceGetAvailableModelsDomesticMergesAccountMappings(t *testing.T) {
	svc := &GatewayService{
		accountRepo: &modelsListAccountRepoStub{
			all: []Account{
				{
					Platform: PlatformMiniMax,
					Credentials: map[string]any{
						"model_mapping": map[string]any{
							"custom-haiku":  "MiniMax-M2.7-highspeed",
							"custom-sonnet": "MiniMax-M2.7",
							"wildcard-*":    "MiniMax-M2.7",
							"bad-target":    "not-a-minimax-model",
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
			},
		},
		cfg: &config.Config{},
	}

	models := svc.GetAvailableModels(context.Background(), nil, PlatformMiniMax)

	assertStringSlicesEqual(t, models, []string{
		"MiniMax-M2.7",
		"MiniMax-M2.7-highspeed",
		"custom-haiku",
		"custom-sonnet",
	})
}

func TestGatewayServiceGetAvailableModelsDomesticIncludesAliasesWhenConfigured(t *testing.T) {
	svc := &GatewayService{
		accountRepo: &modelsListAccountRepoStub{},
		cfg: &config.Config{Gateway: config.GatewayConfig{
			ModelAliases: config.GatewayModelAliasConfig{
				Enabled:         true,
				IncludeInModels: true,
			},
		}},
	}

	models := svc.GetAvailableModels(context.Background(), nil, PlatformDeepSeek)

	assertStringSlicesEqual(t, models, []string{
		"deepseek-chat",
		"deepseek-r1",
		"deepseek-reasoner",
		"deepseek-v3",
		"deepseek-v4-flash",
		"deepseek-v4-pro",
	})
}
