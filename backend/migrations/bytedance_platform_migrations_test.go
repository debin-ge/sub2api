package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestByteDancePlatformMigrationsOnlyExtendChecks(t *testing.T) {
	for _, name := range []string{
		"267_user_platform_quotas_add_bytedance.sql",
		"268_composite_routes_add_bytedance.sql",
	} {
		body, err := FS.ReadFile(name)
		require.NoError(t, err)
		sql := strings.ToLower(string(body))
		require.Contains(t, sql, "'bytedance'")
		require.Contains(t, sql, "add constraint")
		require.NotContains(t, sql, " update ")
		require.NotContains(t, sql, " insert ")
		require.NotContains(t, sql, "create table")
	}
}
