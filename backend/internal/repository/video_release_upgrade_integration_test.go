//go:build integration

package repository

import (
	"context"
	"database/sql"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestVideoReleaseUpgradeFromPreVideoSchemaPreservesExistingData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	name := "video_release_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := integrationDB.ExecContext(ctx, `CREATE DATABASE `+pq.QuoteIdentifier(name))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, release := context.WithTimeout(context.Background(), 10*time.Second)
		defer release()
		_, err := integrationDB.ExecContext(cleanupCtx, `DROP DATABASE `+pq.QuoteIdentifier(name)+` WITH (FORCE)`)
		require.NoError(t, err)
	})
	dsn, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	dsn.Path = "/" + name
	db, err := sql.Open("postgres", dsn.String())
	require.NoError(t, err)
	defer db.Close()
	baseline := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	require.NoError(t, err)
	for _, entry := range entries {
		// Match the runner's filename ordering, including suffixes such as 108a.
		if !strings.HasSuffix(entry.Name(), ".sql") || entry.Name() >= "238_" {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, entry.Name())
		require.NoError(t, err)
		baseline[entry.Name()] = &fstest.MapFile{Data: body}
	}
	require.NotEmpty(t, baseline)
	require.NoError(t, applyMigrationsFS(ctx, db, baseline))
	var userID, accountID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,balance) VALUES('release@example.invalid','synthetic',25) RETURNING id`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials) VALUES('existing-grok','grok','apikey','{"api_key":"synthetic"}') RETURNING id`).Scan(&accountID))
	var before string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT credentials::text FROM accounts WHERE id=$1`, accountID).Scan(&before))
	require.NoError(t, ApplyMigrations(ctx, db))
	require.NoError(t, ApplyMigrations(ctx, db), "a repeated migration run must be a no-op")
	var balance float64
	var platform, credentials string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=$1`, userID).Scan(&balance))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT platform,credentials::text FROM accounts WHERE id=$1`, accountID).Scan(&platform, &credentials))
	require.Equal(t, 25.0, balance)
	require.Equal(t, "grok", platform)
	require.Equal(t, before, credentials)
	var tables int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('video_tasks','video_create_intents')`).Scan(&tables))
	require.Equal(t, 2, tables)
	var version string
	require.NoError(t, db.QueryRowContext(ctx, `SHOW server_version`).Scan(&version))
	t.Logf("isolated upgrade from <=237 baseline passed on PostgreSQL %s; production baseline not asserted", version)
}
