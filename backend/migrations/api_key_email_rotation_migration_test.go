package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyEmailRotationMigrationsContainDurableContracts(t *testing.T) {
	apiKeyMigration, err := FS.ReadFile("235_api_key_email_rotation.sql")
	require.NoError(t, err)
	outboxMigration, err := FS.ReadFile("236_notification_email_outbox.sql")
	require.NoError(t, err)

	apiKeySQL := string(apiKeyMigration)
	for _, required := range []string{"notification_email", "change_notify_enabled", "rotate_on_expiry", "validity_duration_seconds", "rotation_version", "idx_api_keys_due_rotation"} {
		require.Contains(t, apiKeySQL, required)
	}
	outboxSQL := string(outboxMigration)
	for _, required := range []string{"notification_email_outbox", "dedup_key", "rotation_version"} {
		require.Contains(t, outboxSQL, required)
	}
	require.NotContains(t, strings.ToLower(outboxSQL), "new_api_key")
}
