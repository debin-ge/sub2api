//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func accountWithPricingIdentityMapping(platform string, accountType string) *Account {
	return &Account{
		Platform: platform,
		Type:     accountType,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"public-model": "provider-priced-model",
			},
		},
	}
}

func TestResolveAccountUpstreamModelMatchesGeminiForwardMappingPolicy(t *testing.T) {
	t.Run("OAuth forwards requested model verbatim", func(t *testing.T) {
		account := accountWithPricingIdentityMapping(PlatformGemini, AccountTypeOAuth)
		require.Equal(t, "public-model", resolveAccountUpstreamModel(account, "public-model"))
	})

	t.Run("API key applies account mapping", func(t *testing.T) {
		account := accountWithPricingIdentityMapping(PlatformGemini, AccountTypeAPIKey)
		require.Equal(t, "provider-priced-model", resolveAccountUpstreamModel(account, "public-model"))
	})

	t.Run("service account applies account mapping", func(t *testing.T) {
		account := accountWithPricingIdentityMapping(PlatformGemini, AccountTypeServiceAccount)
		require.Equal(t, "provider-priced-model", resolveAccountUpstreamModel(account, "public-model"))
	})
}

func TestResolveAccountUpstreamModelIgnoresAnthropicOAuthAccountMapping(t *testing.T) {
	account := accountWithPricingIdentityMapping(PlatformAnthropic, AccountTypeOAuth)
	require.Equal(t, "public-model", resolveAccountUpstreamModel(account, "public-model"))
}

func TestGatewayPricingGuardDoesNotValidateGeminiOAuthMappingItWillNotForward(t *testing.T) {
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	svc := &GatewayService{
		cfg:            cfg,
		billingService: NewBillingService(cfg, nil),
	}
	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gemini-future-unpriced-v99": "gemini-3.1-pro",
			},
		},
	}

	err := svc.ValidateUsagePricing(
		context.Background(),
		nil,
		account,
		"gemini-future-unpriced-v99",
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	account.Type = AccountTypeAPIKey
	require.NoError(t, svc.ValidateUsagePricing(
		context.Background(),
		nil,
		account,
		"gemini-future-unpriced-v99",
	))
}
