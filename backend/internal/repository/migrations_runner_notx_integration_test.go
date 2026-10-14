//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMigrationsRunner_NonTransactionalQuotedSemicolonsExecute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Only the disposable integration database is used. Generated identifiers
	// keep this fixture separate from application tables and other tests.
	table := fmt.Sprintf("migration_lexer_%d", time.Now().UnixNano())
	_, err := integrationDB.ExecContext(ctx, "CREATE TABLE "+table+" (payload TEXT)")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
		require.NoError(t, err)
	})
	content := fmt.Sprintf(`
-- A comment separator; must not produce another statement.
CREATE INDEX CONCURRENTLY IF NOT EXISTS %[1]s_a ON %[1]s ((payload || 'it''s;safe'));
/* outer comment; /* nested comment; */ still a comment */
CREATE INDEX CONCURRENTLY IF NOT EXISTS "%[1]s;b" ON %[1]s ((payload || $body$dollar;body$body$));
CREATE INDEX CONCURRENTLY IF NOT EXISTS %[1]s_c ON %[1]s ((payload || E'quote\';tail'));
`, table)
	nonTx, err := validateMigrationExecutionMode("lexer_regression_notx.sql", content)
	require.NoError(t, err)
	require.True(t, nonTx)
	statements := splitSQLStatements(content)
	require.Len(t, statements, 3)
	for _, statement := range statements {
		_, err := integrationDB.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	var count int
	var valid bool
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*), bool_and(indisvalid AND indisready) FROM pg_index WHERE indrelid = $1::regclass`, table).Scan(&count, &valid))
	require.Equal(t, 3, count)
	require.True(t, valid)
}

func TestMigrationsRunner_ProviderCostIndexesAppliedAndRepeatable(t *testing.T) {
	ctx := context.Background()
	// TestMain applies the unchanged migration 280 against a real PostgreSQL
	// server; reapplying it must retain both usable indexes.
	require.NoError(t, ApplyMigrations(ctx, integrationDB))
	for _, name := range videoProviderCostIndexNames {
		var valid, ready bool
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			`SELECT indisvalid, indisready FROM pg_index WHERE indexrelid = $1::regclass`, name).Scan(&valid, &ready))
		require.True(t, valid, name)
		require.True(t, ready, name)
	}
}
