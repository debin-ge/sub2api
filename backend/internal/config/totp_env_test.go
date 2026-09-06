package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadTOTPLegacyEncryptionKeysFromEnv(t *testing.T) {
	current := strings.Repeat("ab", 32)
	firstLegacy := strings.Repeat("cd", 32)
	secondLegacy := strings.Repeat("ef", 32)

	for _, test := range []struct {
		name string
		env  string
		want []string
	}{
		{name: "empty", want: []string{}},
		{name: "single", env: firstLegacy, want: []string{firstLegacy}},
		{
			name: "normalize and deduplicate",
			env:  strings.Join([]string{firstLegacy, " " + secondLegacy + " ", firstLegacy, current, ""}, ","),
			want: []string{firstLegacy, secondLegacy},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv("TOTP_ENCRYPTION_KEY", current)
			t.Setenv("TOTP_LEGACY_ENCRYPTION_KEYS", test.env)

			cfg, err := Load()
			require.NoError(t, err)
			require.Equal(t, current, cfg.Totp.EncryptionKey)
			require.True(t, cfg.Totp.EncryptionKeyConfigured)
			require.Equal(t, test.want, cfg.Totp.LegacyEncryptionKeys)
		})
	}
}
