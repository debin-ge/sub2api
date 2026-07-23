package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistrationEmailSuffixBlacklistMigrationStartsEmptyAndDoesNotInvertLegacyPolicy(t *testing.T) {
	content, err := FS.ReadFile("186_registration_email_suffix_blacklist.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "registration_email_suffix_blacklist")
	require.Contains(t, sql, "'[]'")
	require.Contains(t, sql, "ON CONFLICT (key) DO NOTHING")
	// The blacklist must be seeded empty, never populated from (inverted) legacy
	// whitelist values. Reading the legacy value read-only for a warning is allowed;
	// copying it into a settings row via SELECT or rewriting settings is not.
	require.NotContains(t, sql, "INSERT INTO settings (key, value, updated_at) SELECT")
	require.NotContains(t, sql, "UPDATE settings")
}
