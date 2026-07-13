package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type gatewayServiceModelCatalogStub struct {
	testT  *testing.T
	models []string
	err    error
	calls  int
}

func (s *gatewayServiceModelCatalogStub) ListForPlatform(_ context.Context, _ *int64, platform string, wait bool) ([]string, error) {
	s.calls++
	require.Equal(s.testT, PlatformOpenAI, platform)
	require.True(s.testT, wait)
	return append([]string(nil), s.models...), s.err
}

func TestGatewayServiceGetAvailableModels_FallsBackWhenCatalogErrors(t *testing.T) {
	stub := &gatewayServiceModelCatalogStub{testT: t, err: errors.New("catalog unavailable")}
	groupID := int64(21)
	svc := &GatewayService{
		modelCatalog: stub,
		accountRepo: &modelsListAccountRepoStub{byGroup: map[int64][]Account{
			groupID: {
				{
					Platform: PlatformOpenAI,
					Credentials: map[string]any{"model_mapping": map[string]any{
						"legacy-model": "legacy-model",
					}},
				},
			},
		}},
	}

	got := svc.GetAvailableModels(context.Background(), &groupID, PlatformOpenAI)

	require.Equal(t, []string{"legacy-model"}, got)
	require.Equal(t, 1, stub.calls)
}

func TestGatewayServiceGetAvailableModels_DelegatesCatalog(t *testing.T) {
	stub := &gatewayServiceModelCatalogStub{testT: t, models: []string{"alias-old", "gpt-live-new"}}
	svc := &GatewayService{modelCatalog: stub}
	groupID := int64(20)
	got := svc.GetAvailableModels(context.Background(), &groupID, PlatformOpenAI)
	require.Equal(t, []string{"alias-old", "gpt-live-new"}, got)
	require.Equal(t, 1, stub.calls)
}

func TestGatewayServiceGetAvailableModelsDomesticDefaultsWhenNoAccounts(t *testing.T) {
	svc := &GatewayService{
		accountRepo: &modelsListAccountRepoStub{},
		cfg:         &config.Config{},
	}

	models := svc.GetAvailableModels(context.Background(), nil, PlatformMiniMax)

	assertStringSlicesEqual(t, models, []string{"MiniMax-M2.7", "MiniMax-M2.7-highspeed", "MiniMax-M3"})
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
		"MiniMax-M3",
		"bad-target",
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

func TestGatewayServiceGetAvailableModelsOpenCodeDefaultsWhenNoAccounts(t *testing.T) {
	svc := &GatewayService{
		accountRepo: &modelsListAccountRepoStub{},
		cfg:         &config.Config{},
	}

	models := svc.GetAvailableModels(context.Background(), nil, PlatformOpenCode)

	assertStringSlicesEqual(t, models, []string{"gpt5-nano", "opencode/big-pickle", "opencode/gpt5-nano"})
}
