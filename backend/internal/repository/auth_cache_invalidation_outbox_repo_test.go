package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestAuthCacheInvalidationOutboxRepository_ClaimUsesLeaseAndSkipLocked(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	created := time.Now().UTC()
	mock.ExpectQuery("(?s)claimed_at < NOW\\(\\) - .*FOR UPDATE SKIP LOCKED.*RETURNING").
		WithArgs("worker-a", 100, int64(30)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "cache_key", "attempts", "delivery_stage", "created_at"}).
			AddRow(int64(4), strings.Repeat("a", 64), 2, 1, created))

	repo := NewAuthCacheInvalidationOutboxRepository(db)
	events, err := repo.Claim(context.Background(), "worker-a", 100, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(4), events[0].ID)
	require.Equal(t, 1, events[0].Stage)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthCacheInvalidationOutboxRepository_ClaimIsBoundedByDefault(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("(?s)FROM auth_cache_invalidation_outbox.*LIMIT \\$2.*SKIP LOCKED").
		WithArgs("worker", 100, int64(30)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "cache_key", "attempts", "delivery_stage", "created_at"}))
	repo := NewAuthCacheInvalidationOutboxRepository(db)
	_, err = repo.Claim(context.Background(), "worker", 0, 0)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthCacheInvalidationOutboxRepository_ClaimOwnershipTransitions(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := NewAuthCacheInvalidationOutboxRepository(db)

	next := time.Now().UTC().Add(time.Minute)
	mock.ExpectExec("UPDATE auth_cache_invalidation_outbox").
		WithArgs(int64(1), "worker", next).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.ScheduleSecondPass(context.Background(), 1, "worker", next))

	retryAt := next.Add(time.Minute)
	mock.ExpectExec("UPDATE auth_cache_invalidation_outbox").
		WithArgs(int64(2), "worker", retryAt, "publish failed").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.RetryClaimed(context.Background(), 2, "worker", retryAt, "publish failed"))

	mock.ExpectExec("DELETE FROM auth_cache_invalidation_outbox").
		WithArgs(int64(3), "worker").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.DeleteClaimed(context.Background(), 3, "worker"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthCacheInvalidationOutboxRepository_RejectsLostClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectExec("DELETE FROM auth_cache_invalidation_outbox").
		WithArgs(int64(3), "old-worker").
		WillReturnResult(sqlmock.NewResult(0, 0))
	repo := NewAuthCacheInvalidationOutboxRepository(db)
	err = repo.DeleteClaimed(context.Background(), 3, "old-worker")
	require.ErrorContains(t, err, "no longer owned")
}

func TestAuthCacheInvalidationOutboxRepository_StatsExposeDurableLagAndFailures(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	oldest := time.Now().UTC().Add(-time.Minute)
	mock.ExpectQuery("(?s)SELECT COUNT\\(\\*\\), MIN\\(created_at\\), COALESCE\\(MAX\\(attempts\\), 0\\)").
		WillReturnRows(sqlmock.NewRows([]string{"count", "min", "max", "last_error"}).AddRow(5, oldest, 7, "redis down"))
	repo := NewAuthCacheInvalidationOutboxRepository(db)
	stats, err := repo.Stats(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(5), stats.Pending)
	require.Equal(t, 7, stats.MaxAttempts)
	require.Equal(t, "redis down", stats.LastError)
	require.NotNil(t, stats.OldestCreatedAt)
}

func TestAuthCacheInvalidationMigration_SecurityCoverageAndNoPlaintextPayload(t *testing.T) {
	content, err := migrations.FS.ReadFile("184_auth_cache_invalidation_outbox.sql")
	require.NoError(t, err)
	sqlText := string(content)
	for _, required := range []string{
		"encode(sha256(convert_to(raw_key, 'UTF8')), 'hex')",
		"OLD.key", "OLD.status", "OLD.deleted_at", "OLD.user_id", "OLD.group_id",
		"OLD.ip_whitelist", "OLD.ip_blacklist", "OLD.expires_at",
		"trg_users_auth_cache_invalidation", "trg_groups_auth_cache_invalidation",
		"trg_user_allowed_groups_auth_cache_invalidation", "FOR EACH ROW",
		"delivery_stage", "claimed_at", "available_at",
	} {
		require.Contains(t, sqlText, required)
	}
	require.NotContains(t, sqlText, "quota_used IS DISTINCT")
	require.NotContains(t, sqlText, "last_used_at IS DISTINCT")

	plaintext := "sk-plaintext-must-not-be-stored"
	sum := sha256.Sum256([]byte(plaintext))
	require.Len(t, hex.EncodeToString(sum[:]), 64)
	require.NotContains(t, sqlText, plaintext)
}

func TestAuthCacheInvalidationMigration_LatestGroupTriggerCoversCompleteSnapshot(t *testing.T) {
	entries, err := migrations.FS.ReadDir(".")
	require.NoError(t, err)

	var latestName string
	var latestSQL string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		content, readErr := migrations.FS.ReadFile(entry.Name())
		require.NoError(t, readErr)
		if strings.Contains(string(content), "CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()") &&
			entry.Name() > latestName {
			latestName = entry.Name()
			latestSQL = string(content)
		}
	}

	require.NotEmpty(t, latestName, "group invalidation trigger migration must exist")
	snapshotType := reflect.TypeOf(service.APIKeyAuthGroupSnapshot{})
	for i := 0; i < snapshotType.NumField(); i++ {
		field := strings.Split(snapshotType.Field(i).Tag.Get("json"), ",")[0]
		if field == "" || field == "-" || field == "id" {
			continue
		}
		require.Contains(t, latestSQL, "OLD."+field+" IS NOT DISTINCT FROM NEW."+field,
			"latest group invalidation trigger migration %s is missing cached field %s", latestName, field)
	}
	require.Contains(t, latestSQL, "OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at",
		"soft deletion must invalidate every cached key bound to the group")
}
