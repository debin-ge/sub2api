package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccount_IsCodexCLIOnlyAppServerAllowed(t *testing.T) {
	t.Run("codex_cli_only 开 + allow_app_server=true → true", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Extra:    map[string]any{"codex_cli_only": true, "codex_cli_only_allow_app_server": true},
		}
		require.True(t, account.IsCodexCLIOnlyAppServerAllowed())
	})

	t.Run("codex_cli_only 开 + allow_app_server=false → false", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Extra:    map[string]any{"codex_cli_only": true, "codex_cli_only_allow_app_server": false},
		}
		require.False(t, account.IsCodexCLIOnlyAppServerAllowed())
	})

	t.Run("codex_cli_only 开 + 字段缺失 → false", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Extra:    map[string]any{"codex_cli_only": true},
		}
		require.False(t, account.IsCodexCLIOnlyAppServerAllowed())
	})

	t.Run("codex_cli_only 关 → 即便 allow_app_server=true 也 false", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Extra:    map[string]any{"codex_cli_only_allow_app_server": true},
		}
		require.False(t, account.IsCodexCLIOnlyAppServerAllowed())
	})
}

func TestAccount_IsCodexCLIOnlyClaudeCodeAllowed(t *testing.T) {
	t.Run("supports JSON-decoded and typed lists", func(t *testing.T) {
		for _, allowedClients := range []any{
			[]any{"claude_code"},
			[]string{" CLAUDE_CODE "},
		} {
			account := &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					"codex_cli_only":                 true,
					"codex_cli_only_allowed_clients": allowedClients,
				},
			}
			require.True(t, account.IsCodexCLIOnlyClaudeCodeAllowed())
		}
	})

	t.Run("requires codex-only OpenAI OAuth and the named preset", func(t *testing.T) {
		for _, account := range []*Account{
			{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"codex_cli_only_allowed_clients": []any{"claude_code"}}},
			{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"codex_cli_only": true, "codex_cli_only_allowed_clients": []any{"claude_code"}}},
			{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"codex_cli_only": true, "codex_cli_only_allowed_clients": []any{"unknown"}}},
			{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"codex_cli_only": true, "codex_cli_only_allowed_clients": "claude_code"}},
		} {
			require.False(t, account.IsCodexCLIOnlyClaudeCodeAllowed())
		}
	})
}
