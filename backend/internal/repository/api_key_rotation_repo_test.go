package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRotationRepositoryListDueFiltersPendingAndDisabledKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewAPIKeyRotationRepository(db)
	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)FROM api_keys k.*notification_email IS NOT NULL\s+AND k.validity_duration_seconds > 0.*status IN \('active', 'quota_exhausted', 'expired'\).*NOT EXISTS.*notification_email_outbox`).
		WithArgs(now, service.NotificationEmailEventAPIKeyRotated, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "key", "name", "notification_email", "expires_at", "validity_duration_seconds", "rotation_version"}).
			AddRow(int64(3), int64(7), "sk-old", "production", "owner@example.com", now.Add(-time.Minute), int64(3600), int64(4)))

	items, err := repo.ListDue(context.Background(), now, 100)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(4), items[0].RotationVersion)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyRotationRepositoryAtomicallyUpdatesAndEnqueues(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewAPIKeyRotationRepository(db)
	now := time.Now().UTC()
	candidate := service.DueAPIKeyRotation{ID: 3, UserID: 7, OldKey: "sk-old", RotationVersion: 4}
	mock.ExpectQuery(regexp.QuoteMeta("WITH updated AS (")+`(?s).*UPDATE api_keys.*status = CASE WHEN status = 'expired' THEN 'active' ELSE status END.*notification_email IS NOT NULL\s+AND validity_duration_seconds > 0.*INSERT INTO notification_email_outbox.*SELECT rotation_version FROM inserted`).
		WithArgs(candidate.ID, "sk-new", now, candidate.RotationVersion, service.NotificationEmailEventAPIKeyRotated, sqlmock.AnyArg(), "api_key.rotated:3:5").
		WillReturnRows(sqlmock.NewRows([]string{"rotation_version"}).AddRow(int64(5)))

	version, rotated, err := repo.RotateIfDue(context.Background(), candidate, "sk-new", now)
	require.NoError(t, err)
	require.True(t, rotated)
	require.Equal(t, int64(5), version)
	require.NoError(t, mock.ExpectationsWereMet())
}
