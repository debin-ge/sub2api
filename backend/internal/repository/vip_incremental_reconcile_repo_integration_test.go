//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVIPIncrementalReconcileRepositoryPostgreSQL(t *testing.T) {
	ctx := context.Background()
	repo := &vipIncrementalReconcileRepository{db: integrationDB}
	suffix := time.Now().UnixNano()
	completedAt := time.Date(2000, 1, 1, 0, 10, 0, 0, time.UTC)

	autoUser := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-l1-auto-%d@example.com", suffix),
	})
	forceOffUser := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-l1-force-off-%d@example.com", suffix),
	})
	lateUser := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-l1-late-%d@example.com", suffix),
	})

	var (
		originalCompletedAt time.Time
		originalOrderID     int64
		originalCutoff      sql.NullTime
		originalUpdatedAt   time.Time
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT completed_at_cursor, order_id_cursor, backfill_cutoff, updated_at
		FROM vip_reconcile_watermark
		WHERE id = 1
	`).Scan(
		&originalCompletedAt,
		&originalOrderID,
		&originalCutoff,
		&originalUpdatedAt,
	))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM payment_orders WHERE user_id IN ($1, $2, $3)",
			autoUser.ID,
			forceOffUser.ID,
			lateUser.ID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM user_vip_audit_events WHERE user_id IN ($1, $2, $3)",
			autoUser.ID,
			forceOffUser.ID,
			lateUser.ID,
		)
		_, _ = integrationDB.ExecContext(context.Background(), `
			UPDATE vip_reconcile_watermark
			SET completed_at_cursor = $1,
			    order_id_cursor = $2,
			    backfill_cutoff = $3,
			    updated_at = $4
			WHERE id = 1
		`, originalCompletedAt, originalOrderID, originalCutoff, originalUpdatedAt)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM vip_payment_order_fact_archive WHERE user_id IN ($1, $2, $3)",
			autoUser.ID,
			forceOffUser.ID,
			lateUser.ID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM users WHERE id IN ($1, $2, $3)",
			autoUser.ID,
			forceOffUser.ID,
			lateUser.ID,
		)
	})

	_, err := integrationDB.ExecContext(ctx, `
		UPDATE vip_reconcile_watermark
		SET completed_at_cursor = 'epoch',
		    order_id_cursor = 0,
		    backfill_cutoff = NULL,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE users
		SET vip_manual_override = FALSE,
		    vip_override_at = $2,
		    vip_override_by = $1,
		    vip_override_reason = 'integration force off',
		    is_vip = FALSE,
		    vip_granted_at = NULL,
		    vip_effective_source = 'manual_off'
		WHERE id = $1
	`, forceOffUser.ID, completedAt)
	require.NoError(t, err)

	autoOrderID := insertVIPReconcileOrder(
		t,
		ctx,
		autoUser.ID,
		completedAt,
		&completedAt,
	)
	forceOffOrderID := insertVIPReconcileOrder(
		t,
		ctx,
		forceOffUser.ID,
		completedAt,
		&completedAt,
	)

	watermark, err := repo.InitializeBackfillCutoff(ctx)
	require.NoError(t, err)
	require.False(t, watermark.BackfillCutoff.IsZero())

	result, err := repo.ProcessNextBatch(ctx, watermark.BackfillCutoff, 10)
	require.NoError(t, err)
	require.Equal(t, 2, result.Scanned)
	require.Equal(t, 2, result.Repaired)
	require.Equal(t, 2, result.BackfillRepaired)
	require.Zero(t, result.ReconcileRepaired)
	require.Equal(t, 1, result.EffectiveChanged)
	require.Equal(t, 1, result.ForceOffUnchanged)
	require.Equal(t, []int64{autoUser.ID}, result.ChangedUserIDs)
	require.Equal(t, completedAt, result.Cursor.CompletedAt)
	require.Equal(t, forceOffOrderID, result.Cursor.OrderID)
	require.Less(t, autoOrderID, forceOffOrderID)

	var (
		autoPaidEligible  bool
		autoIsVIP         bool
		autoSource        string
		forcePaidEligible bool
		forceIsVIP        bool
		forceSource       string
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT
			a.vip_paid_eligible,
			a.is_vip,
			a.vip_paid_source,
			f.vip_paid_eligible,
			f.is_vip,
			f.vip_paid_source
		FROM users a
		CROSS JOIN users f
		WHERE a.id = $1 AND f.id = $2
	`, autoUser.ID, forceOffUser.ID).Scan(
		&autoPaidEligible,
		&autoIsVIP,
		&autoSource,
		&forcePaidEligible,
		&forceIsVIP,
		&forceSource,
	))
	require.True(t, autoPaidEligible)
	require.True(t, autoIsVIP)
	require.Equal(t, "backfill", autoSource)
	require.True(t, forcePaidEligible)
	require.False(t, forceIsVIP)
	require.Equal(t, "backfill", forceSource)

	lateCompletedAt := completedAt.Add(-time.Minute)
	lateOrderID := insertVIPReconcileOrder(
		t,
		ctx,
		lateUser.ID,
		lateCompletedAt,
		&lateCompletedAt,
	)
	earlierAutoCompletedAt := completedAt.Add(-2 * time.Minute)
	earlierAutoOrderID := insertVIPReconcileOrder(
		t,
		ctx,
		autoUser.ID,
		earlierAutoCompletedAt,
		&earlierAutoCompletedAt,
	)
	earlierForceOffCompletedAt := completedAt.Add(-3 * time.Minute)
	earlierForceOffOrderID := insertVIPReconcileOrder(
		t,
		ctx,
		forceOffUser.ID,
		earlierForceOffCompletedAt,
		&earlierForceOffCompletedAt,
	)
	overlapResult, err := repo.RepairOverlap(ctx, 5*time.Minute, 10)
	require.NoError(t, err)
	require.Equal(t, 3, overlapResult.Scanned)
	require.Equal(t, 3, overlapResult.Repaired)
	require.Equal(t, 3, overlapResult.BackfillRepaired)
	require.Zero(t, overlapResult.ReconcileRepaired)
	require.Equal(t, 1, overlapResult.EffectiveChanged)
	require.Equal(t, 1, overlapResult.ForceOffUnchanged)
	require.Equal(t, []int64{lateUser.ID}, overlapResult.ChangedUserIDs)
	require.Equal(t, result.Cursor, overlapResult.Cursor)

	var (
		latePaidEligible    bool
		autoPaidEligibleAt  time.Time
		forcePaidEligibleAt time.Time
		forceStillOff       bool
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT
			l.vip_paid_eligible,
			a.vip_paid_eligible_at,
			f.vip_paid_eligible_at,
			f.is_vip
		FROM users l
		CROSS JOIN users a
		CROSS JOIN users f
		WHERE l.id = $1
		  AND a.id = $2
		  AND f.id = $3
	`, lateUser.ID, autoUser.ID, forceOffUser.ID).Scan(
		&latePaidEligible,
		&autoPaidEligibleAt,
		&forcePaidEligibleAt,
		&forceStillOff,
	))
	require.True(t, latePaidEligible)
	require.WithinDuration(t, earlierAutoCompletedAt, autoPaidEligibleAt, time.Microsecond)
	require.WithinDuration(t, earlierForceOffCompletedAt, forcePaidEligibleAt, time.Microsecond)
	require.False(t, forceStillOff, "overlap repair must preserve FORCE_OFF")

	secondOverlap, err := repo.RepairOverlap(ctx, 5*time.Minute, 1)
	require.NoError(t, err)
	require.Zero(t, secondOverlap.Scanned, "repaired overlap candidates must self-disappear")
	require.Zero(t, secondOverlap.Repaired)

	var auditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM user_vip_audit_events
		WHERE order_id IN ($1, $2, $3, $4, $5)
		  AND action = 'backfill'
	`,
		autoOrderID,
		forceOffOrderID,
		lateOrderID,
		earlierAutoOrderID,
		earlierForceOffOrderID,
	).Scan(&auditCount))
	require.Equal(t, 5, auditCount)

	var (
		cursorCompletedAt time.Time
		cursorOrderID     int64
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT completed_at_cursor, order_id_cursor
		FROM vip_reconcile_watermark
		WHERE id = 1
	`).Scan(&cursorCompletedAt, &cursorOrderID))
	require.Equal(t, result.Cursor.CompletedAt, cursorCompletedAt)
	require.Equal(t, result.Cursor.OrderID, cursorOrderID)
}
