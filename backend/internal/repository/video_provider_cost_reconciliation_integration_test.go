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

// Flova 账号上的未结清成本占着串行名额；稳定余额不释放，凭据核销后释放。
func TestFlovaUnresolvedCostSerializesAccountUntilVerifiedResolution(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	flovaParams := func() service.VideoCreateTaskParams {
		params := videoCreateParams(user, key, account, service.NewVideoTaskID(), uuid.NewString(), uuid.NewString(), 1)
		params.Provider, params.MaxAccountConcurrency = service.VideoProviderFlova, 1
		return params
	}
	publicID := service.NewVideoTaskID()
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO video_provider_costs (task_public_id, user_id, account_id, provider, reason, account_identity_version)
		VALUES ($1, $2, $3, 'flova', 'flova_spend_unverified', 0)`, publicID, user.ID, account.ID)
	require.NoError(t, err)

	_, _, err = repo.CreateHeldVideoTask(ctx, flovaParams())
	require.ErrorIs(t, err, service.ErrVideoAccountConcurrencyLimited, "an unresolved Flova cost keeps the account busy")
	// 其它 provider 不受 Flova 成本记录影响。
	other := videoCreateParams(user, key, account, service.NewVideoTaskID(), uuid.NewString(), uuid.NewString(), 1)
	other.MaxAccountConcurrency = 1
	_, created, err := repo.CreateHeldVideoTask(ctx, other)
	require.NoError(t, err)
	require.True(t, created)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET generation_state = 'failed', billing_state = 'released' WHERE public_id = $1`, other.PublicID)
	require.NoError(t, err)

	claim := func() *service.VideoProviderCost {
		_, err := integrationDB.ExecContext(ctx, `UPDATE video_provider_costs SET next_action_at = NOW() - INTERVAL '1 second' WHERE task_public_id = $1`, publicID)
		require.NoError(t, err)
		items, err := repo.ClaimVideoProviderCosts(ctx, "worker-a", 100)
		require.NoError(t, err)
		for _, item := range items {
			if item.TaskPublicID == publicID {
				return item
			}
		}
		t.Fatalf("cost record %s was not claimed", publicID)
		return nil
	}
	balance := 1700.5
	item := claim()
	require.NoError(t, repo.RecordVideoProviderCostBalance(ctx, item, "worker-a", &balance, true, "awaiting_stable_balance", nil))
	item = claim()
	require.NotNil(t, item.ObservedBalance)
	require.Equal(t, balance, *item.ObservedBalance)
	require.NotNil(t, item.ObservedBalanceAt)
	firstAt := *item.ObservedBalanceAt
	require.NoError(t, repo.RecordVideoProviderCostBalance(ctx, item, "worker-a", &balance, false, "awaiting_stable_balance", nil))
	item = claim()
	require.True(t, item.ObservedBalanceAt.Equal(firstAt), "an equal reading keeps the original observation time")

	require.ErrorIs(t, repo.RecordVideoProviderCostBalance(ctx, item, "worker-b", &balance, false, "final_settlement_unverified", nil),
		service.ErrVideoProviderCostConflict, "only the lease holder can record an observation")
	require.NoError(t, repo.RecordVideoProviderCostBalance(ctx, item, "worker-a", &balance, false, "final_settlement_unverified", nil))
	_, _, err = repo.CreateHeldVideoTask(ctx, flovaParams())
	require.ErrorIs(t, err, service.ErrVideoAccountConcurrencyLimited, "stable balances must retain supplier exposure")
	amount := 308.5
	_, err = repo.ResolveVideoProviderCost(ctx, item.ID, service.VideoProviderCostResolution{
		ExpectedVersion: item.Version + 1, ActorID: user.ID, State: "resolved_cost_recorded", Amount: &amount,
		Currency: "CREDITS", Evidence: "synthetic supplier final invoice and termination evidence", IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	var state string
	var resolvedBy *int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT state, resolved_by FROM video_provider_costs WHERE task_public_id = $1`, publicID).Scan(&state, &resolvedBy))
	require.Equal(t, "resolved_cost_recorded", state)
	require.NotNil(t, resolvedBy)
	require.Equal(t, user.ID, *resolvedBy)

	next := flovaParams()
	task, created, err := repo.CreateHeldVideoTask(ctx, next)
	require.NoError(t, err)
	require.True(t, created, "the account is free once the cost is reconciled")

	// 迁移 283：新策略的执行状态与超预算阻断原因能落库，未知策略仍被拒绝。
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET provider_execution_state = $2::jsonb WHERE id = $1`, task.ID,
		`{"version":1,"provider":"flova","project_id":"p1","stream_chat_id":"s1","resume_action_key":"`+strings.Repeat("a", 64)+`","resumed_stream_chat_id":"s2","spending_policy":"serialized_balance_delta","cost_uncertain":true}`)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET provider_execution_state = $2::jsonb WHERE id = $1`, task.ID,
		`{"version":1,"provider":"flova","project_id":"p1","stream_chat_id":"s1","spending_policy":"unbounded","cost_uncertain":true}`)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO video_provider_blocked_actions (task_id, action_key, action_type, reason) VALUES ($1, $2, 'high_cost_model_confirm', 'exceeds_task_budget')`,
		task.ID, strings.Repeat("b", 64))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO video_provider_blocked_actions (task_id, action_key, action_type, reason) VALUES ($1, $2, 'high_cost_model_confirm', 'approved')`,
		task.ID, strings.Repeat("c", 64))
	require.Error(t, err)

	// 约束：没有操作者的结清必须带 auto: 键。
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO video_provider_costs (task_public_id, user_id, account_id, provider, reason, state, amount, currency, evidence, resolution_key, resolved_at)
		VALUES ($1, $2, $3, 'flova', 'flova_spend_unverified', 'resolved_no_charge', 0, 'CREDITS', $4, 'manual-key', NOW())`,
		service.NewVideoTaskID(), user.ID, account.ID, strings.Repeat("e", 30))
	require.Error(t, err)
}
