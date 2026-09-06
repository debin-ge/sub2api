package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountSupportsOpenAIVideosCapability(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{
			name: "official OpenAI API key remains disabled while probe is unknown",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
			},
			want: false,
		},
		{
			name: "official OpenAI API key is enabled after a supported probe",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Extra: map[string]any{VideoCapabilityProbeExtraKey: VideoCapabilityProbeResult{
					Provider: VideoProviderOpenAI, Capability: string(OpenAIEndpointCapabilityVideos),
					Status: VideoCapabilityProbeSupported, CheckedAt: time.Now().UTC(),
				}},
			},
			want: true,
		},
		{
			name: "explicit official capability remains enabled",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"base_url":            "https://api.openai.com/v1",
					"openai_capabilities": []any{"videos"},
				},
			},
			want: true,
		},
		{
			name: "official account can explicitly omit videos",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"openai_capabilities": []any{"chat_completions"},
				},
			},
			want: false,
		},
		{
			name: "custom base URL fails closed without explicit capability",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"base_url": "https://openai-compatible.example/v1",
				},
			},
			want: false,
		},
		{
			name: "custom base URL opts in explicitly",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"base_url":            "https://openai-compatible.example/v1",
					"openai_capabilities": map[string]any{"videos": true},
				},
			},
			want: true,
		},
		{
			name: "Azure is excluded even when explicitly enabled",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"base_url":            "https://example.openai.azure.com",
					"openai_capabilities": []any{"videos"},
				},
			},
			want: false,
		},
		{
			name:    "OAuth is excluded",
			account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			want:    false,
		},
		{
			name:    "setup token is excluded",
			account: &Account{Platform: PlatformOpenAI, Type: AccountTypeSetupToken},
			want:    false,
		},
		{
			name:    "Grok remains on its legacy path",
			account: &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideos))
		})
	}
}
