//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newVideoExecutionWriteGuardFixture(t *testing.T) (*videoTaskRepository, *service.VideoTask) {
	t.Helper()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "execution-guard", "execution-body", 4)
	spec := service.ResolvedVideoExecutionSpec{Version: 2, Provider: params.Provider, AccountID: account.ID,
		Operation: params.Operation, Model: params.UpstreamModel, Size: "1280x720", Seconds: 8, DurationSemantics: "output"}
	fingerprint, err := service.HashVideoRequest(spec)
	require.NoError(t, err)
	params.RequestAttributes["execution_spec"], params.RequestAttributes["execution_spec_hash"], params.RequestAttributes["execution_spec_version"] = spec, fingerprint, 2
	params.PriceSnapshot["unit_price"], params.PriceSnapshot["customer_multiplier"] = 0.5, 1
	task, _, err := repo.CreateHeldVideoTask(context.Background(), params)
	require.NoError(t, err)
	return repo, task
}

func TestVideoExecutionWriteGuardFreezesSnapshotsAndUnsettledIdentity(t *testing.T) {
	ctx := context.Background()
	repo, task := newVideoExecutionWriteGuardFixture(t)
	mutations := []string{
		`request_attributes = request_attributes - 'execution_spec_hash'`,
		`request_attributes = request_attributes - 'execution_spec'`,
		`request_attributes = jsonb_set(request_attributes, '{execution_spec_version}', '1')`,
		`request_attributes = jsonb_set(request_attributes, '{execution_spec,seconds}', '4')`,
		`request_attributes = '{}'::jsonb`,
		`price_snapshot = jsonb_set(price_snapshot, '{unit_price}', '0')`,
		`provider_cost_snapshot = '{"account_rate_multiplier":0}'::jsonb`,
		`upstream_model = 'different-model'`,
		`operation = 'edit'`,
		`provider = 'different-provider'`,
		`billing_unit = 'request'`,
		`hold_amount = 0`,
		`currency = 'EUR'`,
		`account_id = NULL`,
		`api_key_id = NULL`,
		`user_id = NULL`,
		`request_hash = 'different-hash'`,
	}
	for _, mutation := range mutations {
		t.Run(mutation, func(t *testing.T) {
			_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET `+mutation+` WHERE id = $1`, task.ID)
			require.ErrorContains(t, err, "immutable")
		})
	}
	_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET request_attributes = request_attributes || '{"callback_contract_version":1}',
		response_metadata = '{"model":"sora-2","seconds":8}', progress = 25 WHERE id = $1`, task.ID)
	require.NoError(t, err)
	updated, err := repo.GetVideoTaskByPublicID(ctx, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, task.RequestAttributes["execution_spec_hash"], updated.RequestAttributes["execution_spec_hash"])
	require.Equal(t, float64(1), updated.RequestAttributes["callback_contract_version"])
	assertVideoBudgetTotals(t, task.UserID, 1, 96, 4)
}

func TestVideoExecutionWriteGuardPreservesConflictAgainstOldWriters(t *testing.T) {
	for _, marker := range []string{`{"execution_spec_conflict":1}`, `{"specification_invalid":1}`, `{"execution_spec_conflict":"invalid"}`} {
		t.Run(marker, func(t *testing.T) {
			ctx := context.Background()
			repo, task := newVideoExecutionWriteGuardFixture(t)
			_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET response_metadata = $2::jsonb WHERE id = $1`, task.ID, marker)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET response_metadata = '{"execution_spec_conflict":0,"seconds":8}' WHERE id = $1`, task.ID)
			require.NoError(t, err)
			updated, err := repo.GetVideoTaskByPublicID(ctx, task.PublicID)
			require.NoError(t, err)
			require.Equal(t, float64(1), updated.ResponseMetadata["execution_spec_conflict"])
			for _, state := range []string{service.VideoBillingCapturePending, service.VideoBillingReleasePending} {
				_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET generation_state = 'failed', billing_state = $2,
					actual_units = 3, actual_cost = 3 WHERE id = $1`, task.ID, state)
				require.ErrorContains(t, err, "approved review")
			}
			assertVideoBudgetTotals(t, task.UserID, 1, 96, 4)
		})
	}
}

func TestVideoExecutionWriteGuardAllowsFailedZeroCostAutoRelease(t *testing.T) {
	ctx := context.Background()
	repo, task := newVideoExecutionWriteGuardFixture(t)
	manual, err := repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoTaskTransition{
		GenerationState: service.VideoGenerationFailed,
		BillingState:    service.VideoBillingManualReview,
		ResponseMetadata: map[string]any{
			"execution_spec_conflict": 1,
		},
		ErrorKind: "upstream", ErrorCode: "content_policy", ErrorMessage: "provider rejected the video",
		EventType: "provider_failed_before_auto_release",
	})
	require.NoError(t, err)
	task = manual
	zero := 0.0
	pending, err := repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoTaskTransition{
		GenerationState: service.VideoGenerationFailed, BillingState: service.VideoBillingReleasePending,
		ActualUnits: &zero, ActualCost: &zero,
		ErrorKind: "upstream", ErrorCode: "content_policy", ErrorMessage: "provider rejected the video",
		EventType: "provider_failed_auto_release",
	})
	require.NoError(t, err)
	require.Equal(t, service.VideoBillingReleasePending, pending.BillingState)
	require.Equal(t, "content_policy", *pending.LastErrorCode)

	result, err := repo.billing.SettleVideoBalance(ctx, &service.BalanceSettlementCommand{
		TaskID: pending.ID, Action: service.BalanceSettlementRelease,
		Hold: service.BalanceHoldCommand{
			RequestID: service.VideoTaskReleaseRequestID(pending.PublicID), APIKeyID: *pending.APIKeyID,
			RequestPayloadHash: pending.RequestHash, UserID: pending.UserID,
			Scope: service.BalanceHoldScopeVideoTask, RefID: pending.PublicID,
			HoldAmount: *pending.HoldAmount, ActualAmount: 0,
		},
	}, nil)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID))
	settled, err := repo.GetVideoTaskByPublicID(ctx, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.VideoBillingReleased, settled.BillingState)
	require.Equal(t, "content_policy", *settled.LastErrorCode)
	assertVideoBudgetTotals(t, task.UserID, 1, 100, 0)
}

func legacyVideoExecutionSettlementFixture(t *testing.T, action service.BalanceSettlementAction) (*videoTaskRepository, *service.VideoTask, *service.BalanceSettlementCommand, *service.UsageLog) {
	t.Helper()
	ctx := context.Background()
	repo, _, task, _, _ := newVideoBillingReviewFixture(t, 5)
	_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET billing_state = 'held' WHERE id = $1`, task.ID)
	require.NoError(t, err)
	state := service.VideoBillingCapturePending
	cost, units := 3.0, 3.0
	if action == service.BalanceSettlementRelease {
		state, cost, units = service.VideoBillingReleasePending, 0, 0
	}
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET billing_state = $2, actual_cost = $3, actual_units = $4 WHERE id = $1`, task.ID, state, cost, units)
	require.NoError(t, err)
	task, err = repo.GetVideoTaskByPublicID(ctx, task.PublicID)
	require.NoError(t, err)
	command, usage := reviewedVideoSettlement(task, &service.VideoBillingReview{Action: action, ActualCost: cost, ActualUnits: units})
	return repo, task, command, usage
}

func TestVideoExecutionWriteGuardRejectsUnreviewedOldSettlement(t *testing.T) {
	for _, action := range []service.BalanceSettlementAction{service.BalanceSettlementCapture, service.BalanceSettlementRelease} {
		t.Run(string(action), func(t *testing.T) {
			ctx := context.Background()
			repo, task, command, usage := legacyVideoExecutionSettlementFixture(t, action)
			_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET response_metadata = '{"specification_invalid":1}' WHERE id = $1`, task.ID)
			require.NoError(t, err)
			if action == service.BalanceSettlementRelease {
				// Failed zero-cost releases are intentionally exempt since migration 265.
				// A cancelled task with conflicting evidence still requires review.
				_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET generation_state = 'cancelled' WHERE id = $1`, task.ID)
				require.NoError(t, err)
			}
			_, err = repo.billing.SettleVideoBalance(ctx, command, usage)
			require.ErrorContains(t, err, "reviewed financial intent")
			var intents int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_outbox WHERE api_key_id=$1`, task.APIKeyID).Scan(&intents))
			require.Zero(t, intents)
			assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
		})
	}
}

func TestVideoExecutionWriteGuardAllowsExplicitReviewedConflict(t *testing.T) {
	for _, action := range []service.BalanceSettlementAction{service.BalanceSettlementCapture, service.BalanceSettlementRelease} {
		t.Run(string(action), func(t *testing.T) {
			ctx := context.Background()
			repo, admin, task, proposer, approver := newVideoBillingReviewFixture(t, 5)
			_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET response_metadata = '{"specification_invalid":1}' WHERE id = $1`, task.ID)
			require.NoError(t, err)
			task, err = repo.GetVideoTaskByPublicID(ctx, task.PublicID)
			require.NoError(t, err)
			request := videoBillingReviewRequest(task, proposer)
			request.Action, request.HonorFrozenQuote = action, action == service.BalanceSettlementCapture
			if action == service.BalanceSettlementRelease {
				request.ActualUnits = 0
			}
			result, err := admin.ProposeVideoBillingReview(ctx, task.PublicID, request)
			require.NoError(t, err)
			if result.Review.RequiresSecondActor {
				result, err = admin.DecideVideoBillingReview(ctx, task.PublicID, result.Review.ID, service.VideoBillingReviewDecision{
					ActorID: approver, OperationKey: "execution:approve", ExpectedVersion: result.Task.Version, Approve: true, Reason: "Conflict evidence verified independently"})
				require.NoError(t, err)
			}
			command, usage := reviewedVideoSettlement(result.Task, result.Review)
			paid, err := repo.billing.SettleVideoBalance(ctx, command, usage)
			require.NoError(t, err)
			require.True(t, paid.Applied)
			assertVideoBudgetTotals(t, task.UserID, 1, 1000-result.Review.ActualCost, 0)
			require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, paid.OutboxReceipt.WorkerID, paid.OutboxReceipt.ID))
		})
	}
}

func TestVideoExecutionWriteGuardMarkerContract(t *testing.T) {
	for _, candidate := range []struct {
		metadata string
		conflict bool
	}{
		{metadata: `{}`},
		{metadata: `{"execution_spec_conflict":null}`},
		{metadata: `{"execution_spec_conflict":0}`},
		{metadata: `{"specification_invalid":"0.0"}`},
		{metadata: `{"execution_spec_conflict":1}`, conflict: true},
		{metadata: `{"specification_invalid":-1}`, conflict: true},
		{metadata: `{"execution_spec_conflict":"invalid"}`, conflict: true},
		{metadata: `{"specification_invalid":false}`, conflict: true},
		{metadata: `{"execution_spec_conflict":{}}`, conflict: true},
	} {
		t.Run(candidate.metadata, func(t *testing.T) {
			var conflict bool
			require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT video_execution_has_conflict($1::jsonb)`, candidate.metadata).Scan(&conflict))
			require.Equal(t, candidate.conflict, conflict)
		})
	}
}

func TestVideoExecutionWriteGuardPreservesDurableLegacyIntent(t *testing.T) {
	ctx := context.Background()
	repo, task, command, usage := legacyVideoExecutionSettlementFixture(t, service.BalanceSettlementCapture)
	commandJSON, usageJSON, err := marshalBalanceSettlementOutboxPayload(command, usage)
	require.NoError(t, err)
	worker := "execution-before-crash"
	event, err := repo.billing.enqueueAndClaimUsageBillingOutboxPayload(ctx, worker, command.Hold.RequestID, *task.APIKeyID,
		command.Hold.RequestFingerprint, usageBillingOutboxPayloadVersionV2, commandJSON, usageJSON)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET response_metadata = '{"execution_spec_conflict":1}' WHERE id = $1`, task.ID)
	require.NoError(t, err)
	event.Command.PlatformQuotaSnapshot = &service.UsageBillingPlatformQuotaSnapshot{}
	require.NoError(t, repo.billing.UpdateUsageBillingOutboxCommand(ctx, worker, event.ID, event.Command))
	for _, mutation := range []string{
		`command_payload = jsonb_set(command_payload, '{billing,account_quota_cost}', '0')`,
		`command_payload = command_payload - 'settlement_scope'`,
		`usage_log_payload = jsonb_set(usage_log_payload, '{actual_cost}', '0')`,
		`request_id = 'different-financial-intent'`,
		`request_fingerprint = repeat('0',64)`,
		`payload_version = 1`,
	} {
		_, err := integrationDB.ExecContext(ctx, `UPDATE usage_billing_outbox SET `+mutation+` WHERE id=$1`, event.ID)
		require.ErrorContains(t, err, "immutable", mutation)
	}
	_, err = integrationDB.ExecContext(ctx, `UPDATE usage_billing_outbox SET claimed_at = NOW() - INTERVAL '3 minutes' WHERE id=$1`, event.ID)
	require.NoError(t, err)
	replayed, err := repo.billing.enqueueAndClaimUsageBillingOutboxPayload(ctx, "execution-recovery", command.Hold.RequestID, *task.APIKeyID,
		command.Hold.RequestFingerprint, usageBillingOutboxPayloadVersionV2, commandJSON, usageJSON)
	require.NoError(t, err)
	require.Equal(t, event.ID, replayed.ID)
	paid, err := repo.billing.CompleteUsageBillingOutbox(ctx, "execution-recovery", replayed)
	require.NoError(t, err)
	require.True(t, paid.Applied)
	assertVideoBudgetTotals(t, task.UserID, 1, 997, 0)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, "execution-recovery", event.ID))
}

func TestVideoExecutionWriteGuardConcurrentEnqueueDoesNotDeadlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo, task, command, usage := legacyVideoExecutionSettlementFixture(t, service.BalanceSettlementCapture)
	commandJSON, usageJSON, err := marshalBalanceSettlementOutboxPayload(command, usage)
	require.NoError(t, err)
	var wait sync.WaitGroup
	results := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			_, err := repo.billing.enqueueAndClaimUsageBillingOutboxPayload(ctx, fmt.Sprintf("execution-contender-%d", worker), command.Hold.RequestID,
				*task.APIKeyID, command.Hold.RequestFingerprint, usageBillingOutboxPayloadVersionV2, commandJSON, usageJSON)
			results <- err
		}(index)
	}
	wait.Wait()
	close(results)
	successes := 0
	for result := range results {
		if result == nil {
			successes++
		} else {
			require.ErrorContains(t, result, "already claimed")
		}
	}
	require.Equal(t, 1, successes)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
}
