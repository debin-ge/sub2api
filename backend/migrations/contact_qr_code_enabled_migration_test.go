package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContactQRCodeEnabledMigrationBackfillsWithoutCopyingImageData(t *testing.T) {
	content, err := FS.ReadFile("234_contact_qr_code_enabled.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "'contact_qr_code_enabled'")
	require.Contains(t, sql, "WHERE key = 'contact_qr_code'")
	require.Contains(t, sql, "BTRIM(value) <> ''")
	require.Contains(t, sql, "ON CONFLICT (key) DO NOTHING")
	require.NotContains(t, sql, "UPDATE settings")
}
