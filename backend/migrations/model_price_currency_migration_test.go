package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelPriceCurrencyMigration(t *testing.T) {
	content, err := FS.ReadFile("234_model_price_currency.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS currency VARCHAR(3) NOT NULL DEFAULT 'USD'")
	require.Contains(t, sql, "CHECK (currency IN ('USD', 'CNY'))")
	require.Contains(t, sql, "SET currency = UPPER(TRIM(currency))")
}
