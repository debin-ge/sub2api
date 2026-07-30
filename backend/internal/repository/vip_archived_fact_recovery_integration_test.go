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

type archivedVIPFactFixture struct {
	user        *service.User
	orderID     int64
	completedAt time.Time
}

func TestVIPArchivedFactRecoveryPostgreSQL(t *testing.T) {
	t.Run("AUTO restores paid eligibility from the archived fact", func(t *testing.T) {
		ctx := context.Background()
		actor := createArchivedVIPActor(t, "auto")
		fixture := createArchivedVIPFact(t, "auto")
		repo := NewVIPEntitlementRepository(integrationDB)

		_, err := repo.SetManualMode(
			ctx,
			fixture.user.ID,
			service.VIPModeForceOff,
			actor.ID,
			"archive recovery setup",
		)
		require.NoError(t, err)
		resetArchivedVIPPaidProjectionPreservingOverride(t, fixture.user.ID)

		result, err := repo.SetManualMode(
			ctx,
			fixture.user.ID,
			service.VIPModeAuto,
			actor.ID,
			"recover canonical archived payment fact",
		)
		require.NoError(t, err)
		require.True(t, result.EligibilityChanged)
		require.True(t, result.EffectiveChanged)
		require.Equal(t, service.VIPModeAuto, result.ManualMode)

		assertArchivedVIPProjection(
			t,
			fixture.user.ID,
			fixture.completedAt,
			string(service.VIPPaidSourceReconcile),
		)
		assertArchivedVIPAuditCount(t, fixture.user.ID, fixture.orderID, "reconcile", 1)
	})

	t.Run("L1 restores an archived fact from a reset watermark", func(t *testing.T) {
		ctx := context.Background()
		fixture := createArchivedVIPFact(t, "l1")
		resetArchivedVIPProjection(t, fixture.user.ID)
		restoreArchivedVIPWatermark := resetArchivedVIPWatermark(
			t,
			fixture.completedAt.Add(time.Hour),
		)
		t.Cleanup(restoreArchivedVIPWatermark)

		repo := NewVIPIncrementalReconcileRepository(integrationDB)
		result, err := repo.ProcessNextBatch(
			ctx,
			fixture.completedAt.Add(2*time.Hour),
			1000,
		)
		require.NoError(t, err)
		require.GreaterOrEqual(t, result.Scanned, 1)
		require.GreaterOrEqual(t, result.Repaired, 1)
		require.Contains(t, result.ChangedUserIDs, fixture.user.ID)

		assertArchivedVIPProjection(
			t,
			fixture.user.ID,
			fixture.completedAt,
			string(service.VIPPaidSourceBackfill),
		)
		assertArchivedVIPAuditCount(t, fixture.user.ID, fixture.orderID, "backfill", 1)
	})

	t.Run("L2 preview and job restore an archived fact", func(t *testing.T) {
		ctx := context.Background()
		actor := createArchivedVIPActor(t, "l2")
		fixture := createArchivedVIPFact(t, "l2")
		resetArchivedVIPProjection(t, fixture.user.ID)

		repo := NewVIPReconcileRepository(integrationDB)
		asOf, err := repo.DatabaseNow(ctx)
		require.NoError(t, err)
		preview, err := repo.Preview(ctx, asOf, 0, 100)
		require.NoError(t, err)
		requireArchivedVIPPreviewItem(t, preview, fixture.orderID)

		requestID := fmt.Sprintf("vip-archive-recovery-%d", time.Now().UnixNano())
		job, err := repo.CreateOrResumeJob(
			ctx,
			requestID,
			actor.ID,
			"recover canonical archived payment fact",
		)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, cleanupErr := integrationDB.ExecContext(
				context.Background(),
				"DELETE FROM vip_reconcile_jobs WHERE request_id = $1",
				requestID,
			)
			if cleanupErr != nil {
				t.Errorf("cleanup archived VIP L2 job: %v", cleanupErr)
			}
		})

		done := false
		for attempt := 0; attempt < 20 && !done; attempt++ {
			batch, processErr := repo.ProcessJobBatch(ctx, job.ID, 1000)
			require.NoError(t, processErr)
			done = batch.Done
		}
		require.True(t, done, "archived VIP L2 recovery did not finish")

		persisted, err := repo.GetJob(ctx, job.ID)
		require.NoError(t, err)
		require.Equal(t, "succeeded", persisted.Status)
		require.GreaterOrEqual(t, persisted.EligibilityRepaired, int64(1))

		assertArchivedVIPProjection(
			t,
			fixture.user.ID,
			fixture.completedAt,
			string(service.VIPPaidSourceReconcile),
		)
		assertArchivedVIPAuditCount(t, fixture.user.ID, fixture.orderID, "reconcile", 1)
	})

	t.Run("user reads stay pending and archived ids reject duplicate live orders", func(t *testing.T) {
		ctx := context.Background()
		fixture := createArchivedVIPFact(t, "pending")
		resetArchivedVIPProjection(t, fixture.user.ID)

		userRepo := newUserRepositoryWithSQL(integrationEntClient, integrationDB)
		userRepo.SetVIPActivationPendingWindow(48 * time.Hour)
		byID, err := userRepo.GetByID(ctx, fixture.user.ID)
		require.NoError(t, err)
		require.True(t, byID.ActivationPending)
		require.Equal(t, service.VIPAccessStateActivationPending, byID.AccessState())

		byEmail, err := userRepo.GetByEmail(ctx, fixture.user.Email)
		require.NoError(t, err)
		require.True(t, byEmail.ActivationPending)
		require.Equal(t, service.VIPAccessStateActivationPending, byEmail.AccessState())

		assertCanonicalArchivedVIPFact(
			t,
			fixture.orderID,
			fixture.user.ID,
			fixture.completedAt,
			1,
		)
		err = insertDuplicateLiveArchivedVIPOrder(t, fixture)
		require.ErrorContains(t, err, "VIP_PAYMENT_FACT_ARCHIVED_ID_REUSE")
		assertCanonicalArchivedVIPFact(
			t,
			fixture.orderID,
			fixture.user.ID,
			fixture.completedAt,
			1,
		)

		byID, err = userRepo.GetByID(ctx, fixture.user.ID)
		require.NoError(t, err)
		require.True(t, byID.ActivationPending)
		require.Equal(t, service.VIPAccessStateActivationPending, byID.AccessState())

		reconcileRepo := NewVIPReconcileRepository(integrationDB)
		asOf, err := reconcileRepo.DatabaseNow(ctx)
		require.NoError(t, err)
		preview, err := reconcileRepo.Preview(ctx, asOf, 0, 100)
		require.NoError(t, err)
		requireArchivedVIPPreviewItem(t, preview, fixture.orderID)
	})
}

func createArchivedVIPActor(t *testing.T, label string) *service.User {
	t.Helper()
	actor := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf(
			"vip-archive-%s-actor-%d@example.com",
			label,
			time.Now().UnixNano(),
		),
		Role: service.RoleAdmin,
	})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM users WHERE id = $1",
			actor.ID,
		)
		if err != nil {
			t.Errorf("cleanup archived VIP actor: %v", err)
		}
	})
	return actor
}

func createArchivedVIPFact(t *testing.T, label string) archivedVIPFactFixture {
	t.Helper()
	ctx := context.Background()
	completedAt := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Microsecond)
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf(
			"vip-archive-%s-user-%d@example.com",
			label,
			time.Now().UnixNano(),
		),
	})

	var orderID int64
	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO payment_orders (
			user_id,
			amount,
			pay_amount,
			order_type,
			status,
			expires_at,
			paid_at,
			completed_at,
			created_at,
			updated_at
		)
		VALUES (
			$1,
			10,
			10,
			'balance',
			'COMPLETED',
			$2,
			$3,
			$4,
			$3,
			$3
		)
		RETURNING id
	`,
		user.ID,
		completedAt.Add(time.Hour),
		completedAt.Add(-time.Minute),
		completedAt,
	).Scan(&orderID)
	require.NoError(t, err)

	entitlementRepo := NewVIPEntitlementRepository(integrationDB)
	_, err = entitlementRepo.ApplyPaidEligibility(
		ctx,
		user.ID,
		orderID,
		completedAt,
		service.VIPPaidSourcePayment,
	)
	require.NoError(t, err)

	fixture := archivedVIPFactFixture{
		user:        user,
		orderID:     orderID,
		completedAt: completedAt,
	}
	t.Cleanup(func() {
		cleanupArchivedVIPFact(t, fixture)
	})

	result, err := integrationDB.ExecContext(
		ctx,
		"DELETE FROM payment_orders WHERE id = $1",
		orderID,
	)
	require.NoError(t, err)
	deleted, err := result.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var archiveCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM vip_payment_order_fact_archive
		WHERE order_id = $1
	`, orderID).Scan(&archiveCount))
	require.Equal(t, 1, archiveCount)

	return fixture
}

func cleanupArchivedVIPFact(t *testing.T, fixture archivedVIPFactFixture) {
	t.Helper()
	ctx := context.Background()

	// A duplicate qualifying live row may have been inserted after archival.
	// Restore the materialized entitlement before deleting it so the retention
	// trigger can verify both the projection and the still-present paid audit.
	if _, err := integrationDB.ExecContext(ctx, `
		UPDATE users
		SET vip_paid_eligible = TRUE,
		    vip_paid_eligible_at = COALESCE(vip_paid_eligible_at, $2),
		    vip_paid_source = CASE
				WHEN BTRIM(vip_paid_source) = '' THEN 'payment'
				ELSE vip_paid_source
			END,
		    is_vip = COALESCE(vip_manual_override, TRUE),
		    vip_granted_at = CASE
				WHEN COALESCE(vip_manual_override, TRUE)
				THEN COALESCE(vip_granted_at, $2)
				ELSE NULL
			END,
		    vip_effective_source = CASE
				WHEN vip_manual_override IS TRUE THEN 'manual_on'
				WHEN vip_manual_override IS FALSE THEN 'manual_off'
				ELSE CASE
					WHEN BTRIM(vip_paid_source) = '' THEN 'payment'
					ELSE vip_paid_source
				END
			END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, fixture.user.ID, fixture.completedAt); err != nil {
		t.Errorf("restore archived VIP projection for cleanup: %v", err)
	}
	if _, err := integrationDB.ExecContext(
		ctx,
		"DELETE FROM payment_orders WHERE id = $1",
		fixture.orderID,
	); err != nil {
		t.Errorf("cleanup archived VIP live order: %v", err)
	}
	if _, err := integrationDB.ExecContext(
		ctx,
		"DELETE FROM user_vip_audit_events WHERE user_id = $1",
		fixture.user.ID,
	); err != nil {
		t.Errorf("cleanup archived VIP audits: %v", err)
	}
	if _, err := integrationDB.ExecContext(
		ctx,
		"DELETE FROM vip_payment_order_fact_archive WHERE order_id = $1",
		fixture.orderID,
	); err != nil {
		t.Errorf("cleanup archived VIP fact: %v", err)
	}
	if _, err := integrationDB.ExecContext(
		ctx,
		"DELETE FROM users WHERE id = $1",
		fixture.user.ID,
	); err != nil {
		t.Errorf("cleanup archived VIP user: %v", err)
	}
}

func resetArchivedVIPProjection(t *testing.T, userID int64) {
	t.Helper()
	_, err := integrationDB.ExecContext(context.Background(), `
		UPDATE users
		SET vip_paid_eligible = FALSE,
		    vip_paid_eligible_at = NULL,
		    vip_paid_source = '',
		    vip_manual_override = NULL,
		    vip_override_at = NULL,
		    vip_override_by = NULL,
		    vip_override_reason = '',
		    is_vip = FALSE,
		    vip_granted_at = NULL,
		    vip_effective_source = '',
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, userID)
	require.NoError(t, err)
}

func resetArchivedVIPPaidProjectionPreservingOverride(t *testing.T, userID int64) {
	t.Helper()
	_, err := integrationDB.ExecContext(context.Background(), `
		UPDATE users
		SET vip_paid_eligible = FALSE,
		    vip_paid_eligible_at = NULL,
		    vip_paid_source = '',
		    is_vip = COALESCE(vip_manual_override, FALSE),
		    vip_granted_at = CASE
				WHEN COALESCE(vip_manual_override, FALSE) THEN vip_granted_at
				ELSE NULL
			END,
		    vip_effective_source = CASE
				WHEN vip_manual_override IS TRUE THEN 'manual_on'
				WHEN vip_manual_override IS FALSE THEN 'manual_off'
				ELSE ''
			END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, userID)
	require.NoError(t, err)
}

func resetArchivedVIPWatermark(t *testing.T, backfillCutoff time.Time) func() {
	t.Helper()
	ctx := context.Background()
	var (
		completedAt time.Time
		orderID     int64
		cutoff      sql.NullTime
		updatedAt   time.Time
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT completed_at_cursor, order_id_cursor, backfill_cutoff, updated_at
		FROM vip_reconcile_watermark
		WHERE id = 1
	`).Scan(&completedAt, &orderID, &cutoff, &updatedAt))

	_, err := integrationDB.ExecContext(ctx, `
		UPDATE vip_reconcile_watermark
		SET completed_at_cursor = 'epoch',
		    order_id_cursor = 0,
		    backfill_cutoff = $1,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`, backfillCutoff.UTC())
	require.NoError(t, err)

	return func() {
		_, restoreErr := integrationDB.ExecContext(context.Background(), `
			UPDATE vip_reconcile_watermark
			SET completed_at_cursor = $1,
			    order_id_cursor = $2,
			    backfill_cutoff = $3,
			    updated_at = $4
			WHERE id = 1
		`, completedAt, orderID, cutoff, updatedAt)
		if restoreErr != nil {
			t.Errorf("restore archived VIP watermark: %v", restoreErr)
		}
	}
}

func assertArchivedVIPProjection(
	t *testing.T,
	userID int64,
	completedAt time.Time,
	wantSource string,
) {
	t.Helper()
	var (
		paidEligible   bool
		paidEligibleAt time.Time
		paidSource     string
		manualOverride sql.NullBool
		isVIP          bool
		effective      string
	)
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `
		SELECT
			vip_paid_eligible,
			vip_paid_eligible_at,
			vip_paid_source,
			vip_manual_override,
			is_vip,
			vip_effective_source
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&paidEligible,
		&paidEligibleAt,
		&paidSource,
		&manualOverride,
		&isVIP,
		&effective,
	))
	require.True(t, paidEligible)
	require.WithinDuration(t, completedAt, paidEligibleAt, time.Microsecond)
	require.Equal(t, wantSource, paidSource)
	require.False(t, manualOverride.Valid)
	require.True(t, isVIP)
	require.Equal(t, wantSource, effective)
}

func assertArchivedVIPAuditCount(
	t *testing.T,
	userID, orderID int64,
	action string,
	want int,
) {
	t.Helper()
	var count int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM user_vip_audit_events
		WHERE user_id = $1
		  AND order_id = $2
		  AND action = $3
	`, userID, orderID, action).Scan(&count))
	require.Equal(t, want, count)
}

func requireArchivedVIPPreviewItem(
	t *testing.T,
	preview *service.VIPReconcilePreview,
	orderID int64,
) {
	t.Helper()
	require.NotNil(t, preview)
	count := 0
	for _, item := range preview.Items {
		if item.OrderID == orderID {
			count++
		}
	}
	require.Equal(t, 1, count, "archived fact must appear exactly once in preview")
}

func assertCanonicalArchivedVIPFact(
	t *testing.T,
	orderID, userID int64,
	completedAt time.Time,
	wantCount int,
) {
	t.Helper()
	rows, err := integrationDB.QueryContext(context.Background(), `
		SELECT user_id, completed_at
		FROM vip_qualifying_payment_order_facts
		WHERE order_id = $1
	`, orderID)
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		var (
			gotUserID      int64
			gotCompletedAt time.Time
		)
		require.NoError(t, rows.Scan(&gotUserID, &gotCompletedAt))
		require.Equal(t, userID, gotUserID)
		require.WithinDuration(t, completedAt, gotCompletedAt, time.Microsecond)
		count++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, wantCount, count)
}

func insertDuplicateLiveArchivedVIPOrder(
	t *testing.T,
	fixture archivedVIPFactFixture,
) error {
	t.Helper()
	_, err := integrationDB.ExecContext(context.Background(), `
		INSERT INTO payment_orders (
			id,
			user_id,
			amount,
			pay_amount,
			order_type,
			status,
			expires_at,
			paid_at,
			completed_at,
			created_at,
			updated_at
		)
		VALUES (
			$1,
			$2,
			20,
			20,
			'balance',
			'COMPLETED',
			$3,
			$4,
			$5,
			$4,
			$4
		)
	`, fixture.orderID,
		fixture.user.ID,
		fixture.completedAt.Add(3*time.Hour),
		fixture.completedAt.Add(time.Hour),
		fixture.completedAt.Add(2*time.Hour),
	)
	return err
}
