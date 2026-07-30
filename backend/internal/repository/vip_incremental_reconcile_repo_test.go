package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func newVIPIncrementalSQLMock(
	t *testing.T,
) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL == "VIP_INCREMENTAL_ORDER_PAGE" {
			normalized := strings.ToLower(actualSQL)
			compact := strings.Join(strings.Fields(normalized), " ")
			if !strings.Contains(compact, "from vip_qualifying_payment_order_facts") ||
				!strings.Contains(compact, "(fact.completed_at, fact.order_id) > ($2, $3)") ||
				!strings.Contains(compact, "order by fact.completed_at asc, fact.order_id asc") {
				return fmt.Errorf("unexpected order page query: %s", actualSQL)
			}
			if strings.Contains(normalized, "vip_paid_eligible") ||
				strings.Contains(normalized, "is_vip") {
				return fmt.Errorf(
					"main cursor page must not filter projection state: %s",
					actualSQL,
				)
			}
			return nil
		}
		if expectedSQL == "VIP_INCREMENTAL_OVERLAP_PAGE" {
			compact := strings.Join(strings.Fields(strings.ToLower(actualSQL)), " ")
			if !strings.Contains(compact, "from vip_qualifying_payment_order_facts") ||
				!strings.Contains(compact, "join users u") ||
				!strings.Contains(compact, "not u.vip_paid_eligible") ||
				!strings.Contains(compact, "u.vip_paid_eligible_at is null") ||
				!strings.Contains(compact, "fact.completed_at < u.vip_paid_eligible_at") ||
				!strings.Contains(compact, "fact.completed_at >= $1") ||
				!strings.Contains(compact, "fact.completed_at <= $2") {
				return fmt.Errorf("unexpected overlap page query: %s", actualSQL)
			}
			return nil
		}
		return sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func TestVIPIncrementalProcessBatchScansAllEventsAndAdvancesWatermark(t *testing.T) {
	db, mock := newVIPIncrementalSQLMock(t)
	cursor := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	cutoff := cursor.Add(30 * time.Minute)
	scanBefore := cursor.Add(time.Hour)
	completedAt := cursor.Add(time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT completed_at_cursor.*FROM vip_reconcile_watermark.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"completed_at_cursor", "order_id_cursor", "backfill_cutoff", "updated_at",
		}).AddRow(cursor, int64(4), cutoff, cursor))
	mock.ExpectQuery("VIP_INCREMENTAL_ORDER_PAGE").
		WithArgs(scanBefore, cursor, int64(4), 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "completed_at"}).
			AddRow(int64(11), int64(7), completedAt))
	mock.ExpectQuery(`(?s)WITH input.*UPDATE users.*INSERT INTO user_vip_audit_events`).
		WithArgs(
			int64(7), int64(11), completedAt, "backfill",
			"system", nil, "system", "", "", "",
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "source", "old_paid_eligible", "new_paid_eligible",
			"old_manual_override", "new_manual_override",
			"old_is_vip", "new_is_vip", "was_updated",
		}).AddRow(
			int64(7),
			"backfill",
			false,
			true,
			false,
			false,
			false,
			false,
			true,
		))
	mock.ExpectExec(`UPDATE vip_reconcile_watermark`).
		WithArgs(completedAt, int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := &vipIncrementalReconcileRepository{db: db}
	result, err := repo.ProcessNextBatch(context.Background(), scanBefore, 2)

	require.NoError(t, err)
	require.Equal(t, 1, result.Scanned)
	require.Equal(t, 1, result.Repaired)
	require.Equal(t, 1, result.BackfillRepaired)
	require.Zero(t, result.ReconcileRepaired)
	require.Equal(t, 1, result.ForceOffUnchanged)
	require.False(t, result.EffectiveChanged > 0)
	require.Equal(t, completedAt, result.Cursor.CompletedAt)
	require.Equal(t, int64(11), result.Cursor.OrderID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVIPIncrementalEmptyBatchAdvancesToDatabaseSafeBound(t *testing.T) {
	db, mock := newVIPIncrementalSQLMock(t)
	cursor := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	scanBefore := cursor.Add(time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT completed_at_cursor.*FROM vip_reconcile_watermark.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"completed_at_cursor", "order_id_cursor", "backfill_cutoff", "updated_at",
		}).AddRow(cursor, int64(4), cursor, cursor))
	mock.ExpectQuery("VIP_INCREMENTAL_ORDER_PAGE").
		WithArgs(scanBefore, cursor, int64(4), 200).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "completed_at"}))
	mock.ExpectExec(`UPDATE vip_reconcile_watermark`).
		WithArgs(scanBefore, int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := &vipIncrementalReconcileRepository{db: db}
	result, err := repo.ProcessNextBatch(context.Background(), scanBefore, 200)

	require.NoError(t, err)
	require.Zero(t, result.Scanned)
	require.Equal(t, scanBefore, result.Cursor.CompletedAt)
	require.Zero(t, result.Cursor.OrderID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVIPIncrementalAlreadyCorrectPageStillAdvancesWatermark(t *testing.T) {
	db, mock := newVIPIncrementalSQLMock(t)
	cursor := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	cutoff := cursor.Add(30 * time.Minute)
	scanBefore := cursor.Add(time.Hour)
	completedAt := cursor.Add(time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT completed_at_cursor.*FROM vip_reconcile_watermark.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"completed_at_cursor", "order_id_cursor", "backfill_cutoff", "updated_at",
		}).AddRow(cursor, int64(4), cutoff, cursor))
	mock.ExpectQuery("VIP_INCREMENTAL_ORDER_PAGE").
		WithArgs(scanBefore, cursor, int64(4), 200).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "completed_at"}).
			AddRow(int64(11), int64(7), completedAt))
	mock.ExpectQuery(`(?s)WITH input.*UPDATE users.*INSERT INTO user_vip_audit_events`).
		WithArgs(
			int64(7), int64(11), completedAt, "backfill",
			"system", nil, "system", "", "", "",
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "source", "old_paid_eligible", "new_paid_eligible",
			"old_manual_override", "new_manual_override",
			"old_is_vip", "new_is_vip", "was_updated",
		}).AddRow(
			int64(7),
			"backfill",
			true,
			nil,
			nil,
			nil,
			true,
			nil,
			false,
		))
	mock.ExpectExec(`UPDATE vip_reconcile_watermark`).
		WithArgs(completedAt, int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := &vipIncrementalReconcileRepository{db: db}
	result, err := repo.ProcessNextBatch(context.Background(), scanBefore, 200)

	require.NoError(t, err)
	require.Equal(t, 1, result.Scanned)
	require.Zero(t, result.Repaired)
	require.Equal(t, completedAt, result.Cursor.CompletedAt)
	require.Equal(t, int64(11), result.Cursor.OrderID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVIPIncrementalOverlapDoesNotAdvanceMainWatermark(t *testing.T) {
	db, mock := newVIPIncrementalSQLMock(t)
	cursor := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	overlap := 5 * time.Minute

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT completed_at_cursor.*FROM vip_reconcile_watermark`).
		WillReturnRows(sqlmock.NewRows([]string{
			"completed_at_cursor", "order_id_cursor", "backfill_cutoff", "updated_at",
		}).AddRow(cursor, int64(4), cursor.Add(-time.Hour), cursor))
	mock.ExpectQuery("VIP_INCREMENTAL_OVERLAP_PAGE").
		WithArgs(cursor.Add(-overlap), cursor, 200).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "completed_at"}))
	mock.ExpectCommit()

	repo := &vipIncrementalReconcileRepository{db: db}
	result, err := repo.RepairOverlap(context.Background(), overlap, 200)

	require.NoError(t, err)
	require.Zero(t, result.Scanned)
	require.Equal(t, cursor, result.Cursor.CompletedAt)
	require.Equal(t, int64(4), result.Cursor.OrderID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVIPIncrementalMutationFailureRollsBackBeforeWatermark(t *testing.T) {
	db, mock := newVIPIncrementalSQLMock(t)
	cursor := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	scanBefore := cursor.Add(time.Hour)
	completedAt := cursor.Add(time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT completed_at_cursor.*FROM vip_reconcile_watermark.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"completed_at_cursor", "order_id_cursor", "backfill_cutoff", "updated_at",
		}).AddRow(cursor, int64(0), scanBefore, cursor))
	mock.ExpectQuery("VIP_INCREMENTAL_ORDER_PAGE").
		WithArgs(scanBefore, cursor, int64(0), 10).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "completed_at"}).
			AddRow(int64(12), int64(8), completedAt))
	mock.ExpectQuery(`(?s)WITH input.*UPDATE users.*INSERT INTO user_vip_audit_events`).
		WithArgs(
			int64(8), int64(12), completedAt, "backfill",
			"system", nil, "system", "", "", "",
		).
		WillReturnError(fmt.Errorf("audit insert failed"))
	mock.ExpectRollback()

	repo := &vipIncrementalReconcileRepository{db: db}
	_, err := repo.ProcessNextBatch(context.Background(), scanBefore, 10)

	require.ErrorContains(t, err, "apply VIP reconcile candidates")
	require.NoError(t, mock.ExpectationsWereMet())
}
