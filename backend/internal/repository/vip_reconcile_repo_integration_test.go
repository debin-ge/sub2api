//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestVIPReconcileRepositoryPreviewAndJobPostgreSQL(t *testing.T) {
	ctx := context.Background()
	repo := NewVIPReconcileRepository(integrationDB)
	suffix := time.Now().UnixNano()
	base := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	asOf := base.Add(24 * time.Hour)
	requestID := fmt.Sprintf("vip-reconcile-integration-%d", suffix)

	actor := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-reconcile-actor-%d@example.com", suffix),
		Role:  service.RoleAdmin,
	})
	autoUser := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-reconcile-auto-%d@example.com", suffix),
	})
	forceOffUser := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-reconcile-off-%d@example.com", suffix),
	})
	correctUser := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-reconcile-correct-%d@example.com", suffix),
	})
	deletedUser := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-reconcile-deleted-%d@example.com", suffix),
	})
	userIDs := []int64{
		actor.ID,
		autoUser.ID,
		forceOffUser.ID,
		correctUser.ID,
		deletedUser.ID,
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		// Materialize every qualifying fixture before deletion so the
		// payment-fact retention trigger can archive even the already-correct
		// and soft-deleted cases that the L2 job intentionally does not write.
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			`
				UPDATE users u
				SET vip_paid_eligible = TRUE,
				    vip_paid_eligible_at = COALESCE(u.vip_paid_eligible_at, facts.completed_at),
				    vip_paid_source = CASE
						WHEN BTRIM(u.vip_paid_source) = '' THEN 'reconcile'
						ELSE u.vip_paid_source
					END,
				    is_vip = COALESCE(u.vip_manual_override, TRUE),
				    vip_granted_at = CASE
						WHEN COALESCE(u.vip_manual_override, TRUE)
						THEN COALESCE(u.vip_granted_at, facts.completed_at)
						ELSE NULL
					END,
				    vip_effective_source = CASE
						WHEN u.vip_manual_override IS TRUE THEN 'manual_on'
						WHEN u.vip_manual_override IS FALSE THEN 'manual_off'
						WHEN BTRIM(u.vip_paid_source) = '' THEN 'reconcile'
						ELSE u.vip_paid_source
					END
				FROM (
					SELECT user_id, MIN(completed_at) AS completed_at
					FROM payment_orders
					WHERE user_id = ANY($1)
					  AND completed_at IS NOT NULL
					GROUP BY user_id
				) facts
				WHERE u.id = facts.user_id
			`,
			pq.Array(userIDs),
		)
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			`
				INSERT INTO user_vip_audit_events (
					user_id,
					actor_type,
					actor_snapshot,
					action,
					order_id,
					old_paid_eligible,
					new_paid_eligible,
					old_manual_override,
					new_manual_override,
					old_is_vip,
					new_is_vip,
					source
				)
				SELECT
					po.user_id,
					'system',
					'system',
					'reconcile',
					po.id,
					FALSE,
					TRUE,
					u.vip_manual_override,
					u.vip_manual_override,
					FALSE,
					COALESCE(u.vip_manual_override, TRUE),
					'reconcile'
				FROM payment_orders po
				JOIN users u ON u.id = po.user_id
				WHERE po.user_id = ANY($1)
				  AND po.completed_at IS NOT NULL
				ON CONFLICT (order_id, action)
					WHERE order_id IS NOT NULL
					DO NOTHING
			`,
			pq.Array(userIDs),
		)
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			"DELETE FROM payment_orders WHERE user_id = ANY($1)",
			pq.Array(userIDs),
		)
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			"DELETE FROM user_vip_audit_events WHERE user_id = ANY($1)",
			pq.Array(userIDs),
		)
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			"DELETE FROM vip_payment_order_fact_archive WHERE user_id = ANY($1)",
			pq.Array(userIDs),
		)
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			"DELETE FROM vip_reconcile_jobs WHERE request_id = $1",
			requestID,
		)
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			"DELETE FROM users WHERE id = ANY($1)",
			pq.Array(userIDs),
		)
	})

	_, err := integrationDB.ExecContext(ctx, `
		UPDATE users
		SET vip_manual_override = FALSE,
		    vip_override_at = $2,
		    vip_override_by = $1,
		    vip_override_reason = 'integration force off',
		    is_vip = FALSE,
		    vip_granted_at = NULL,
		    vip_effective_source = 'manual_off'
		WHERE id = $1
	`, forceOffUser.ID, base)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE users
		SET vip_paid_eligible = TRUE,
		    vip_paid_eligible_at = $2,
		    vip_paid_source = 'payment',
		    is_vip = TRUE,
		    vip_granted_at = $2,
		    vip_effective_source = 'payment'
		WHERE id = $1
	`, correctUser.ID, base)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(
		ctx,
		"UPDATE users SET deleted_at = $2 WHERE id = $1",
		deletedUser.ID,
		base,
	)
	require.NoError(t, err)

	completedAt := base.Add(time.Hour)
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
	correctOrderID := insertVIPReconcileOrder(
		t,
		ctx,
		correctUser.ID,
		completedAt,
		&completedAt,
	)
	deletedOrderID := insertVIPReconcileOrder(
		t,
		ctx,
		deletedUser.ID,
		completedAt,
		&completedAt,
	)
	invalidOrderID := insertVIPReconcileOrder(
		t,
		ctx,
		autoUser.ID,
		completedAt,
		nil,
	)

	preview, err := repo.Preview(ctx, asOf, 0, 20)
	require.NoError(t, err)
	require.Equal(t, int64(4), preview.Total)
	require.Equal(t, int64(2), preview.Stats.EligibilityRepair)
	require.Equal(t, int64(1), preview.Stats.EffectiveChange)
	require.Equal(t, int64(1), preview.Stats.ForceOffUnchanged)
	require.Equal(t, int64(1), preview.Stats.InvalidOrder)
	require.Equal(t, int64(1), preview.Stats.DeletedUser)
	require.Len(t, preview.Items, 4)
	categories := make(map[int64]string, len(preview.Items))
	for _, item := range preview.Items {
		categories[item.OrderID] = item.Category
	}
	require.Equal(t, "EFFECTIVE_CHANGE", categories[autoOrderID])
	require.Equal(t, "FORCE_OFF_UNCHANGED", categories[forceOffOrderID])
	require.Equal(t, "DELETED_USER", categories[deletedOrderID])
	require.Equal(t, "INVALID_ORDER", categories[invalidOrderID])
	require.NotContains(t, categories, correctOrderID)

	var jobID int64
	err = integrationDB.QueryRowContext(ctx, `
		INSERT INTO vip_reconcile_jobs (
			request_id,
			actor_user_id,
			actor_snapshot,
			reason,
			status,
			as_of
		)
		VALUES ($1, $2, $3, 'integration repair', 'queued', $4)
		RETURNING id
	`, requestID, actor.ID, actor.Email, asOf).Scan(&jobID)
	require.NoError(t, err)

	done := false
	for attempt := 0; attempt < 10 && !done; attempt++ {
		batch, processErr := repo.ProcessJobBatch(ctx, jobID, 2)
		require.NoError(t, processErr)
		done = batch.Done
	}
	require.True(t, done, "L2 job did not reach a terminal state")

	job, err := repo.GetJob(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", job.Status)
	require.Equal(t, int64(4), job.Scanned)
	require.Equal(t, int64(2), job.EligibilityRepaired)
	require.Equal(t, int64(1), job.EffectiveChanged)
	require.Equal(t, int64(1), job.ForceOffUnchanged)
	require.Equal(t, int64(1), job.AlreadyCorrect)
	require.Equal(t, int64(1), job.Deleted)
	require.Equal(t, int64(1), job.InvalidOrder)
	require.Zero(t, job.Failed)
	require.Equal(t, 1, job.Attempts)

	var (
		autoPaid     bool
		autoIsVIP    bool
		autoSource   string
		offPaid      bool
		offOverride  *bool
		offIsVIP     bool
		offEffective string
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT vip_paid_eligible, is_vip, vip_paid_source
		FROM users
		WHERE id = $1
	`, autoUser.ID).Scan(&autoPaid, &autoIsVIP, &autoSource))
	require.True(t, autoPaid)
	require.True(t, autoIsVIP)
	require.Equal(t, "reconcile", autoSource)

	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT
			vip_paid_eligible,
			vip_manual_override,
			is_vip,
			vip_effective_source
		FROM users
		WHERE id = $1
	`, forceOffUser.ID).Scan(
		&offPaid,
		&offOverride,
		&offIsVIP,
		&offEffective,
	))
	require.True(t, offPaid)
	require.NotNil(t, offOverride)
	require.False(t, *offOverride)
	require.False(t, offIsVIP)
	require.Equal(t, "manual_off", offEffective)

	var auditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM user_vip_audit_events
		WHERE request_id = $1
		  AND actor_user_id = $2
		  AND action = 'reconcile'
	`, requestID, actor.ID).Scan(&auditCount))
	require.Equal(t, 2, auditCount)
}

func TestVIPReconcilePreviewKeysetDoesNotSkipWhenCandidatesShrink(t *testing.T) {
	ctx := context.Background()
	repo := NewVIPReconcileRepository(integrationDB)
	svc := service.NewVIPReconcileService(repo, nil)
	suffix := time.Now().UnixNano()
	completedAt := time.Date(2002, 1, 1, 0, 0, 0, 0, time.UTC)

	userIDs := make([]int64, 0, 4)
	orderIDs := make([]int64, 0, 4)
	orderUserIDs := make(map[int64]int64, 4)
	for index := range 4 {
		user := mustCreateUser(t, integrationEntClient, &service.User{
			Email: fmt.Sprintf("vip-preview-keyset-%d-%d@example.com", suffix, index),
		})
		orderID := insertVIPReconcileOrder(
			t,
			ctx,
			user.ID,
			completedAt,
			&completedAt,
		)
		userIDs = append(userIDs, user.ID)
		orderIDs = append(orderIDs, orderID)
		orderUserIDs[orderID] = user.ID
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		// Materialize every test fact so the retention trigger permits cleanup
		// even if an assertion fails before all candidates are repaired.
		_, _ = integrationDB.ExecContext(cleanupCtx, `
			UPDATE users
			SET vip_paid_eligible = TRUE,
			    vip_paid_eligible_at = COALESCE(vip_paid_eligible_at, $2),
			    vip_paid_source = 'payment',
			    is_vip = COALESCE(vip_manual_override, TRUE),
			    vip_granted_at = CASE
					WHEN COALESCE(vip_manual_override, TRUE) THEN $2
					ELSE NULL
				END,
			    vip_effective_source = CASE
					WHEN vip_manual_override IS TRUE THEN 'manual_on'
					WHEN vip_manual_override IS FALSE THEN 'manual_off'
					ELSE 'payment'
				END
			WHERE id = ANY($1)
		`, pq.Array(userIDs), completedAt)
		_, _ = integrationDB.ExecContext(cleanupCtx, `
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
			SELECT
				po.user_id,
				'system',
				'system',
				'paid_grant',
				po.id,
				FALSE,
				TRUE,
				FALSE,
				TRUE,
				'payment'
			FROM payment_orders po
			WHERE po.id = ANY($1)
			ON CONFLICT (order_id, action)
				WHERE order_id IS NOT NULL
				DO NOTHING
		`, pq.Array(orderIDs))
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			"DELETE FROM payment_orders WHERE id = ANY($1)",
			pq.Array(orderIDs),
		)
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			"DELETE FROM user_vip_audit_events WHERE user_id = ANY($1)",
			pq.Array(userIDs),
		)
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			"DELETE FROM vip_payment_order_fact_archive WHERE order_id = ANY($1)",
			pq.Array(orderIDs),
		)
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			"DELETE FROM users WHERE id = ANY($1)",
			pq.Array(userIDs),
		)
	})

	first, err := svc.Preview(ctx, "", 2)
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	require.NotEmpty(t, first.NextCursor)
	firstOrderIDs := []int64{first.Items[0].OrderID, first.Items[1].OrderID}
	require.Equal(t, orderIDs[:2], firstOrderIDs)

	firstUserIDs := []int64{
		orderUserIDs[firstOrderIDs[0]],
		orderUserIDs[firstOrderIDs[1]],
	}
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE users
		SET vip_paid_eligible = TRUE,
		    vip_paid_eligible_at = $2,
		    vip_paid_source = 'payment',
		    is_vip = TRUE,
		    vip_granted_at = $2,
		    vip_effective_source = 'payment'
		WHERE id = ANY($1)
	`, pq.Array(firstUserIDs), completedAt)
	require.NoError(t, err)

	// Move an unreturned candidate between display categories. Because the
	// cursor is the immutable order id rather than category or OFFSET, it must
	// still appear exactly once on the next page.
	thirdUserID := orderUserIDs[orderIDs[2]]
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE users
		SET vip_manual_override = FALSE,
		    vip_override_at = CURRENT_TIMESTAMP,
		    vip_override_by = $1,
		    vip_override_reason = 'keyset category change',
		    is_vip = FALSE,
		    vip_granted_at = NULL,
		    vip_effective_source = 'manual_off'
		WHERE id = $1
	`, thirdUserID)
	require.NoError(t, err)

	second, err := svc.Preview(ctx, first.NextCursor, 2)
	require.NoError(t, err)
	require.Equal(t, first.AsOf, second.AsOf)
	require.Len(t, second.Items, 2)
	require.Equal(t, orderIDs[2], second.Items[0].OrderID)
	require.Equal(t, "FORCE_OFF_UNCHANGED", second.Items[0].Category)
	require.Equal(t, orderIDs[3], second.Items[1].OrderID)
	require.Empty(t, second.NextCursor)
}

func TestVIPReconcileRepositoryCreateIsConcurrentIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := NewVIPReconcileRepository(integrationDB)
	suffix := time.Now().UnixNano()
	requestID := fmt.Sprintf("vip-reconcile-idempotency-%d", suffix)
	otherRequestID := requestID + "-other"
	actor := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-reconcile-idem-actor-%d@example.com", suffix),
		Role:  service.RoleAdmin,
	})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM vip_reconcile_jobs WHERE request_id IN ($1, $2)",
			requestID,
			otherRequestID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM users WHERE id = $1",
			actor.ID,
		)
	})

	type result struct {
		job *service.VIPReconcileJob
		err error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			job, err := repo.CreateOrResumeJob(
				ctx,
				requestID,
				actor.ID,
				"integration idempotency",
			)
			results <- result{job: job, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var jobIDs []int64
	for got := range results {
		require.NoError(t, got.err)
		require.NotNil(t, got.job)
		jobIDs = append(jobIDs, got.job.ID)
	}
	require.Len(t, jobIDs, 2)
	require.Equal(t, jobIDs[0], jobIDs[1])

	_, err := repo.CreateOrResumeJob(
		ctx,
		requestID,
		actor.ID,
		"different input",
	)
	require.Error(t, err)
	require.Equal(t, "VIP_RECONCILE_IDEMPOTENCY_CONFLICT", infraerrors.Reason(err))

	_, err = repo.CreateOrResumeJob(
		ctx,
		otherRequestID,
		actor.ID,
		"integration idempotency",
	)
	require.Error(t, err)
	require.Equal(t, "VIP_RECONCILE_ACTIVE_JOB", infraerrors.Reason(err))

	require.NoError(t, repo.MarkJobFailed(ctx, jobIDs[0], "simulated failure"))
	resumed, err := repo.CreateOrResumeJob(
		ctx,
		requestID,
		actor.ID,
		"integration idempotency",
	)
	require.NoError(t, err)
	require.Equal(t, jobIDs[0], resumed.ID)
	require.Equal(t, "queued", resumed.Status)
}

func TestVIPReconcileRepositoryResumesRunningJobFromPersistedCursor(t *testing.T) {
	ctx := context.Background()
	repo := NewVIPReconcileRepository(integrationDB)
	suffix := time.Now().UnixNano()
	base := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	asOf := base.Add(24 * time.Hour)
	requestID := fmt.Sprintf("vip-reconcile-restart-%d", suffix)

	actor := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-reconcile-restart-actor-%d@example.com", suffix),
		Role:  service.RoleAdmin,
	})
	firstUser := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-reconcile-restart-first-%d@example.com", suffix),
	})
	secondUser := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("vip-reconcile-restart-second-%d@example.com", suffix),
	})
	userIDs := []int64{actor.ID, firstUser.ID, secondUser.ID}
	completedAt := base.Add(time.Hour)
	firstOrderID := insertVIPReconcileOrder(t, ctx, firstUser.ID, completedAt, &completedAt)
	secondOrderID := insertVIPReconcileOrder(t, ctx, secondUser.ID, completedAt, &completedAt)
	orderIDs := []int64{firstOrderID, secondOrderID}
	t.Cleanup(func() {
		// Delete qualifying orders while their materialized entitlement and
		// audit still exist so the retention trigger can archive the facts.
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM payment_orders WHERE id = ANY($1)",
			pq.Array(orderIDs),
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM user_vip_audit_events WHERE user_id = ANY($1)",
			pq.Array(userIDs),
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM vip_reconcile_jobs WHERE request_id = $1",
			requestID,
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM vip_payment_order_fact_archive WHERE order_id = ANY($1)",
			pq.Array(orderIDs),
		)
		_, _ = integrationDB.ExecContext(
			context.Background(),
			"DELETE FROM users WHERE id = ANY($1)",
			pq.Array(userIDs),
		)
	})

	var jobID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO vip_reconcile_jobs (
			request_id,
			actor_user_id,
			actor_snapshot,
			reason,
			status,
			as_of
		)
		VALUES ($1, $2, $3, 'restart recovery integration', 'queued', $4)
		RETURNING id
	`, requestID, actor.ID, actor.Email, asOf).Scan(&jobID))

	firstBatch, err := repo.ProcessJobBatch(ctx, jobID, 1)
	require.NoError(t, err)
	require.False(t, firstBatch.Done)
	require.Equal(t, 1, firstBatch.Repaired)

	running, err := repo.GetActiveJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, running)
	require.Equal(t, jobID, running.ID)
	require.Equal(t, "running", running.Status)
	require.Equal(t, int64(1), running.Scanned)
	require.Positive(t, running.CursorOrderID)

	// A fresh service instance represents a replacement process. Startup
	// recovery must take over the durable active job without another POST.
	restarted := service.NewVIPReconcileService(repo, nil)
	require.NoError(t, restarted.ResumeActiveJob(ctx))

	require.Eventually(t, func() bool {
		job, loadErr := repo.GetJob(ctx, jobID)
		return loadErr == nil && job.Status == "succeeded"
	}, 5*time.Second, 25*time.Millisecond)

	job, err := repo.GetJob(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, int64(2), job.Scanned)
	require.Equal(t, int64(2), job.EligibilityRepaired)
	require.Equal(t, 1, job.Attempts)

	active, err := repo.GetActiveJob(ctx)
	require.NoError(t, err)
	require.Nil(t, active)
}

func insertVIPReconcileOrder(
	t *testing.T,
	ctx context.Context,
	userID int64,
	updatedAt time.Time,
	completedAt *time.Time,
) int64 {
	t.Helper()
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
		userID,
		updatedAt.Add(time.Hour),
		updatedAt,
		completedAt,
	).Scan(&orderID)
	require.NoError(t, err)
	return orderID
}
