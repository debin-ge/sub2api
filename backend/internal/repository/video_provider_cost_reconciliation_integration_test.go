//go:build integration

package repository

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Uses only the disposable integration database. No provider is called.
func TestVideoProviderCostAdmissionReservesSlotsBeforeConcurrentSubmission(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	const requests = 12
	var wg sync.WaitGroup
	results := make(chan error, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account,
				service.NewVideoTaskID(), uuid.NewString(), uuid.NewString(), 1))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
			continue
		}
		require.True(t, errors.Is(err, service.ErrVideoProviderCostLimited), "unexpected admission failure: %v", err)
	}
	require.Equal(t, service.VideoProviderCostMaxUnresolvedPerUser, accepted)
}

func TestVideoProviderCostLocalExpiryAlwaysRecordsExposure(t *testing.T) {
	for _, cancelResult := range []string{"acknowledged", "unsupported", "failed"} {
		t.Run(cancelResult, func(t *testing.T) {
			ctx := context.Background()
			repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
			task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account,
				service.NewVideoTaskID(), uuid.NewString(), uuid.NewString(), 1))
			require.NoError(t, err)
			tx, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			// Model the persisted worker result from all three cancellation outcomes.
			// Even acknowledged cancellation does not establish supplier no-charge.
			_, err = tx.ExecContext(ctx, `UPDATE video_tasks SET generation_state = 'expired', billing_state = 'release_pending',
				provider_task_id = 'isolated-expiry-task', last_error_code = NULL,
				request_attributes = request_attributes || '{"execution_expiry_enforced_locally":true}'::jsonb WHERE id = $1`, task.ID)
			require.NoError(t, err)
			require.NoError(t, recordUncertainVideoProviderCostTx(ctx, tx, task.ID, service.BalanceSettlementRelease))
			var reason, state string
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT reason, state FROM video_provider_costs WHERE task_id = $1`, task.ID).Scan(&reason, &state))
			require.Equal(t, "local_expiry_cost_unverified", reason)
			require.Equal(t, "unresolved", state)
		})
	}
}

func TestVideoProviderCostBackfillUsesHistoricalEventsAfterErrorWasCleared(t *testing.T) {
	raw, err := migrations.FS.ReadFile("280_video_provider_cost_reconciliation.sql")
	require.NoError(t, err)
	start := strings.Index(string(raw), "INSERT INTO video_provider_costs")
	require.Greater(t, start, 0)
	backfill := string(raw)[start:]
	for _, event := range []string{"submission_unknown_auto_failed", "submission_reconciliation_expired"} {
		t.Run(event, func(t *testing.T) {
			ctx := context.Background()
			repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
			task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account,
				service.NewVideoTaskID(), uuid.NewString(), uuid.NewString(), 1))
			require.NoError(t, err)
			tx, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, `UPDATE video_tasks SET generation_state = 'failed', billing_state = 'released',
				last_error_code = NULL, submission_unknown_at = NULL, delete_state = 'deleted' WHERE id = $1`, task.ID)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `INSERT INTO video_task_events (task_id, event_type) VALUES ($1, $2)`, task.ID, event)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, backfill)
			require.NoError(t, err)
			var state, billingState string
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT c.state, vt.billing_state FROM video_provider_costs c
				JOIN video_tasks vt ON vt.id = c.task_id WHERE c.task_id = $1`, task.ID).Scan(&state, &billingState))
			require.Equal(t, "unresolved", state)
			require.Equal(t, "released", billingState)
		})
	}
}

func TestVideoProviderCostDatabaseRejectsIncompleteFinancialResolution(t *testing.T) {
	ctx := context.Background()
	for _, fragment := range []string{
		"'resolved_no_charge', NULL, 'USD', 1, 'a verified supplier invoice reference', 'resolution-1'",
		"'resolved_cost_recorded', 1, NULL, 1, 'a verified supplier invoice reference', 'resolution-1'",
		"'resolved_cost_recorded', 1, 'USD', NULL, 'a verified supplier invoice reference', 'resolution-1'",
		"'resolved_cost_recorded', 1, 'USD', 1, '', 'resolution-1'",
		"'resolved_cost_recorded', 1, 'USD', 1, 'a verified supplier invoice reference', NULL",
	} {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO video_provider_costs
			(task_public_id, provider, reason, state, amount, currency, resolved_by, evidence, resolution_key, resolved_at)
			VALUES ($1, 'test', 'test', `+fragment+`, NOW())`, "constraint-test-"+uuid.NewString())
		require.Error(t, err, "a resolved record must have complete, non-NULL financial evidence")
	}
}
