//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVIPPaymentOrderRetentionBlocksUnmaterializedDeleteAndArchivesMaterializedFact(
	t *testing.T,
) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	completedAt := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-retention-%d@example.com", suffix),
	})
	orderID := insertVIPReconcileOrder(t, ctx, user.ID, completedAt, &completedAt)

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM user_vip_audit_events WHERE user_id = $1",
			user.ID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM vip_payment_order_fact_archive WHERE order_id = $1",
			orderID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM payment_orders WHERE id = $1",
			orderID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM users WHERE id = $1",
			user.ID,
		)
	})

	_, err := integrationDB.ExecContext(
		ctx,
		"DELETE FROM payment_orders WHERE id = $1",
		orderID,
	)
	require.ErrorContains(t, err, "VIP_PAYMENT_FACT_NOT_MATERIALIZED")

	var orderStillPresent bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM payment_orders WHERE id = $1)
	`, orderID).Scan(&orderStillPresent))
	require.True(t, orderStillPresent)

	_, err = integrationDB.ExecContext(
		ctx,
		"UPDATE payment_orders SET status = 'CANCELLED' WHERE id = $1",
		orderID,
	)
	require.ErrorContains(t, err, "VIP_PAYMENT_FACT_NOT_MATERIALIZED")

	var retainedStatus string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status
		FROM payment_orders
		WHERE id = $1
	`, orderID).Scan(&retainedStatus))
	require.Equal(t, "COMPLETED", retainedStatus)

	_, err = integrationDB.ExecContext(ctx, `
		UPDATE users
		SET vip_paid_eligible = TRUE,
		    vip_paid_eligible_at = $2,
		    vip_paid_source = 'payment',
		    is_vip = TRUE,
		    vip_granted_at = $2,
		    vip_effective_source = 'payment'
		WHERE id = $1
	`, user.ID, completedAt)
	require.NoError(t, err)

	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO user_vip_audit_events (
			user_id,
			actor_type,
			actor_snapshot,
			action,
			order_id,
			old_paid_eligible,
			new_paid_eligible,
			old_is_vip,
			new_is_vip,
			source
		)
		VALUES ($1, 'system', 'system', 'paid_grant', $2, FALSE, TRUE, FALSE, TRUE, 'payment')
	`, user.ID, orderID)
	require.NoError(t, err)

	_, err = integrationDB.ExecContext(
		ctx,
		"UPDATE payment_orders SET status = 'REFUNDED' WHERE id = $1",
		orderID,
	)
	require.NoError(t, err)

	var paidEligible, isVIP bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT vip_paid_eligible, is_vip
		FROM users
		WHERE id = $1
	`, user.ID).Scan(&paidEligible, &isVIP))
	require.True(t, paidEligible, "refund must not revoke paid eligibility")
	require.True(t, isVIP, "refund must not revoke effective VIP")

	result, err := integrationDB.ExecContext(
		ctx,
		"DELETE FROM payment_orders WHERE id = $1",
		orderID,
	)
	require.NoError(t, err)
	deleted, err := result.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var (
		archivedUserID      int64
		archivedAmount      float64
		archivedPayAmount   float64
		archivedOrderType   string
		archivedStatus      string
		archivedCompletedAt time.Time
		snapshotOrderID     int64
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT
			user_id,
			amount,
			pay_amount,
			order_type,
			status,
			completed_at,
			(order_snapshot ->> 'id')::bigint
		FROM vip_payment_order_fact_archive
		WHERE order_id = $1
	`, orderID).Scan(
		&archivedUserID,
		&archivedAmount,
		&archivedPayAmount,
		&archivedOrderType,
		&archivedStatus,
		&archivedCompletedAt,
		&snapshotOrderID,
	))
	require.Equal(t, user.ID, archivedUserID)
	require.Equal(t, 10.0, archivedAmount)
	require.Equal(t, 10.0, archivedPayAmount)
	require.Equal(t, "balance", archivedOrderType)
	require.Equal(t, "REFUNDED", archivedStatus)
	require.WithinDuration(t, completedAt, archivedCompletedAt, time.Microsecond)
	require.Equal(t, orderID, snapshotOrderID)

	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT vip_paid_eligible, is_vip
		FROM users
		WHERE id = $1
	`, user.ID).Scan(&paidEligible, &isVIP))
	require.True(t, paidEligible, "archival must not revoke paid eligibility")
	require.True(t, isVIP, "archival must not revoke effective VIP")
}

func TestVIPPaymentOrderRetentionEnforcesArchivedIDAndIdentityInvariants(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	completedAt := time.Date(2002, 3, 4, 5, 6, 7, 0, time.UTC)
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-retention-invariants-%d@example.com", suffix),
	})
	orderID := insertVIPReconcileOrder(t, ctx, user.ID, completedAt, &completedAt)
	var pendingOrderID int64

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM payment_orders WHERE id IN ($1, $2)",
			orderID,
			pendingOrderID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM user_vip_audit_events WHERE user_id = $1",
			user.ID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM vip_payment_order_fact_archive WHERE order_id = $1",
			orderID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM users WHERE id = $1",
			user.ID,
		)
	})

	entitlementRepo := NewVIPEntitlementRepository(integrationDB)
	_, err := entitlementRepo.ApplyPaidEligibility(
		ctx,
		user.ID,
		orderID,
		completedAt,
		service.VIPPaidSourcePayment,
	)
	require.NoError(t, err)

	identityUpdates := map[string]string{
		"id":           "id = id + 900000000000",
		"user_id":      "user_id = user_id + 900000000000",
		"amount":       "amount = amount + 1",
		"pay_amount":   "pay_amount = pay_amount + 1",
		"order_type":   "order_type = 'subscription'",
		"payment_type": "payment_type = 'identity-rewrite'",
		"paid_at":      "paid_at = paid_at + INTERVAL '1 second'",
		"completed_at": "completed_at = completed_at + INTERVAL '1 second'",
	}
	for field, assignment := range identityUpdates {
		t.Run("rejects qualifying identity rewrite "+field, func(t *testing.T) {
			_, updateErr := integrationDB.ExecContext(
				ctx,
				"UPDATE payment_orders SET "+assignment+" WHERE id = $1",
				orderID,
			)
			require.ErrorContains(t, updateErr, "VIP_PAYMENT_FACT_IDENTITY_IMMUTABLE")
		})
	}

	_, err = integrationDB.ExecContext(
		ctx,
		"UPDATE payment_orders SET status = 'CANCELLED' WHERE id = $1",
		orderID,
	)
	require.NoError(t, err)
	assertCanonicalArchivedVIPFact(t, orderID, user.ID, completedAt, 1)

	var activeStatus string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status
		FROM payment_orders
		WHERE id = $1
	`, orderID).Scan(&activeStatus))
	require.Equal(t, "CANCELLED", activeStatus)

	_, err = integrationDB.ExecContext(
		ctx,
		"UPDATE payment_orders SET status = 'COMPLETED' WHERE id = $1",
		orderID,
	)
	require.ErrorContains(t, err, "VIP_PAYMENT_FACT_ARCHIVED_ID_REUSE")
	assertCanonicalArchivedVIPFact(t, orderID, user.ID, completedAt, 1)

	_, err = integrationDB.ExecContext(
		ctx,
		"DELETE FROM payment_orders WHERE id = $1",
		orderID,
	)
	require.NoError(t, err)
	assertCanonicalArchivedVIPFact(t, orderID, user.ID, completedAt, 1)

	_, err = integrationDB.ExecContext(ctx, `
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
			10,
			10,
			'balance',
			'COMPLETED',
			$3,
			$4,
			$4,
			$4,
			$4
		)
	`, orderID, user.ID, completedAt.Add(time.Hour), completedAt)
	require.ErrorContains(t, err, "VIP_PAYMENT_FACT_ARCHIVED_ID_REUSE")

	err = integrationDB.QueryRowContext(ctx, `
		INSERT INTO payment_orders (
			user_id,
			amount,
			pay_amount,
			order_type,
			status,
			expires_at,
			created_at,
			updated_at
		)
		VALUES ($1, 10, 10, 'balance', 'PENDING', $2, $3, $3)
		RETURNING id
	`, user.ID, completedAt.Add(time.Hour), completedAt).Scan(&pendingOrderID)
	require.NoError(t, err)

	_, err = integrationDB.ExecContext(
		ctx,
		"UPDATE payment_orders SET id = $1 WHERE id = $2",
		orderID,
		pendingOrderID,
	)
	require.ErrorContains(t, err, "VIP_PAYMENT_FACT_ARCHIVED_ID_REUSE")
}

func TestVIPPaymentOrderRetentionSerializesArchiveAgainstSameIDInsert(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	completedAt := time.Date(2003, 4, 5, 6, 7, 8, 0, time.UTC)
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-retention-race-%d@example.com", suffix),
	})
	orderID := insertVIPReconcileOrder(t, ctx, user.ID, completedAt, &completedAt)

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM payment_orders WHERE id = $1",
			orderID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM user_vip_audit_events WHERE user_id = $1",
			user.ID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM vip_payment_order_fact_archive WHERE order_id = $1",
			orderID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM users WHERE id = $1",
			user.ID,
		)
	})

	entitlementRepo := NewVIPEntitlementRepository(integrationDB)
	_, err := entitlementRepo.ApplyPaidEligibility(
		ctx,
		user.ID,
		orderID,
		completedAt,
		service.VIPPaidSourcePayment,
	)
	require.NoError(t, err)

	archiveTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = archiveTx.Rollback() }()

	_, err = archiveTx.ExecContext(
		ctx,
		"DELETE FROM payment_orders WHERE id = $1",
		orderID,
	)
	require.NoError(t, err)

	insertErr := make(chan error, 1)
	go func() {
		insertCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, insertSameIDErr := integrationDB.ExecContext(insertCtx, `
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
				10,
				10,
				'balance',
				'COMPLETED',
				$3,
				$4,
				$4,
				$4,
				$4
			)
		`, orderID, user.ID, completedAt.Add(time.Hour), completedAt)
		insertErr <- insertSameIDErr
	}()

	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := integrationDB.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_locks
				WHERE locktype = 'advisory'
				  AND NOT granted
			)
		`).Scan(&waiting)
		return queryErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "same-id insert did not block on archive lock")

	select {
	case earlyErr := <-insertErr:
		require.Failf(t, "same-id insert returned before archive commit", "error: %v", earlyErr)
	default:
	}

	require.NoError(t, archiveTx.Commit())

	select {
	case err = <-insertErr:
		require.ErrorContains(t, err, "VIP_PAYMENT_FACT_ARCHIVED_ID_REUSE")
	case <-time.After(5 * time.Second):
		require.Fail(t, "same-id insert did not finish after archive commit")
	}
	assertCanonicalArchivedVIPFact(t, orderID, user.ID, completedAt, 1)
}
