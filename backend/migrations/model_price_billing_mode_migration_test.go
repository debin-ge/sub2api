package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelPriceBillingModeMigration(t *testing.T) {
	content, err := FS.ReadFile("275_model_price_override_billing_mode.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	// 存量行必须落在 token 档：token 档不抑制任何目录价，行为与迁移前一致。
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS billing_mode VARCHAR(20) NOT NULL DEFAULT 'token'")
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS model_price_overrides_billing_mode_check")
	require.Contains(t, sql, "CHECK (billing_mode IN ('token', 'image', 'video'))")
}
