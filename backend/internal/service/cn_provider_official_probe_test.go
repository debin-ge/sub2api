package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCNProviderOfficialBalanceProbeSupported(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		credentials map[string]any
		want        bool
	}{
		{name: "deepseek default", platform: PlatformDeepseek, credentials: map[string]any{"api_key": "sk"}, want: true},
		{name: "deepseek explicit official", platform: PlatformDeepseek, credentials: map[string]any{"api_key": "sk", "base_url": "HTTPS://API.DEEPSEEK.COM.:443/v1"}, want: true},
		{name: "deepseek third party", platform: PlatformDeepseek, credentials: map[string]any{"api_key": "sk", "base_url": "https://relay.example/v1"}, want: false},
		{name: "kimi default payg", platform: PlatformKimi, credentials: map[string]any{"api_key": "sk", "account_mode": AccountModePayG}, want: true},
		{name: "kimi third party payg", platform: PlatformKimi, credentials: map[string]any{"api_key": "sk", "account_mode": AccountModePayG, "base_url": "https://relay.example/v1"}, want: false},
		{
			name:     "deepseek adaptive mixed",
			platform: PlatformDeepseek,
			credentials: map[string]any{
				"api_key": "sk", "api_protocol": APIProtocolAdaptive,
				"api_base_urls": map[string]any{
					APIProtocolChatCompletions: DefaultDeepseekBaseURL,
					APIProtocolAnthropic:       "https://relay.example/anthropic",
					APIProtocolResponses:       DefaultDeepseekBaseURL,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: tt.platform, Type: AccountTypeAPIKey, Credentials: tt.credentials}
			require.Equal(t, tt.want, cnProviderOfficialBalanceProbeSupported(account))
		})
	}
}

func TestCNProviderOfficialQuotaProbeSupported(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		credentials map[string]any
		want        bool
	}{
		{name: "kimi official", platform: PlatformKimi, credentials: map[string]any{"api_key": "sk", "account_mode": AccountModeCoding}, want: true},
		{name: "kimi custom", platform: PlatformKimi, credentials: map[string]any{"api_key": "sk", "account_mode": AccountModeCoding, "base_url": "https://relay.example/kimi"}, want: false},
		{name: "zhipu official cn", platform: PlatformZhipu, credentials: map[string]any{"api_key": "sk", "account_mode": AccountModeCoding}, want: true},
		{name: "zhipu official global", platform: PlatformZhipu, credentials: map[string]any{"api_key": "sk", "account_mode": AccountModeCoding, "base_url": "https://api.z.ai/api/coding/paas/v4"}, want: true},
		{name: "zhipu custom", platform: PlatformZhipu, credentials: map[string]any{"api_key": "sk", "account_mode": AccountModeCoding, "base_url": "https://relay.example/zhipu"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: tt.platform, Type: AccountTypeAPIKey, Credentials: tt.credentials}
			require.Equal(t, tt.want, cnProviderOfficialQuotaProbeSupported(account))
		})
	}
}
