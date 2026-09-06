//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newVideoBillingReviewFixture(t *testing.T, hold float64) (*videoTaskRepository, *videoAdminRepository, *service.VideoTask, int64, int64) {
	t.Helper()
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 1000)
	actors := make([]int64, 2)
	for index := range actors {
		actor := mustCreateUser(t, testEntClient(t), &service.User{Email: "video-review-" + uuid.NewString() + "@example.test", PasswordHash: "hash", Role: service.RoleAdmin, Status: service.StatusActive})
		actors[index] = actor.ID
		t.Cleanup(func() {
			_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, actor.ID)
			require.NoError(t, err)
		})
	}
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "review-fixture", "review-body", hold)
	params.PriceSnapshot["unit_price"], params.PriceSnapshot["customer_multiplier"] = 0.5, 2
	task, _, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.NoError(t, err)
	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationFailed, BillingState: service.VideoBillingManualReview})
	require.NoError(t, err)
	return repo, &videoAdminRepository{db: integrationDB}, task, actors[0], actors[1]
}

func videoBillingReviewRequest(task *service.VideoTask, actor int64) service.VideoBillingReviewRequest {
	return service.VideoBillingReviewRequest{ActorID: actor, OperationKey: "review:propose", ExpectedVersion: task.Version,
		Action: service.BalanceSettlementCapture, ActualUnits: 3, Reason: "Provider invoice verified", EvidenceRef: "ticket:REVIEW-1", ApprovalThresholdUSD: 100}
}

func reviewedVideoSettlement(task *service.VideoTask, review *service.VideoBillingReview) (*service.BalanceSettlementCommand, *service.UsageLog) {
	requestID := service.VideoTaskCaptureRequestID(task.PublicID)
	if review.Action == service.BalanceSettlementRelease {
		requestID = service.VideoTaskReleaseRequestID(task.PublicID)
	}
	settlement := &service.BalanceSettlementCommand{TaskID: task.ID, Action: review.Action, Hold: service.BalanceHoldCommand{
		BillingReviewID: review.ID, RequestID: requestID, APIKeyID: *task.APIKeyID, UserID: task.UserID,
		RequestPayloadHash: task.RequestHash, Scope: service.BalanceHoldScopeVideoTask, RefID: task.PublicID, HoldAmount: *task.HoldAmount, ActualAmount: review.ActualCost,
	}}
	if review.Action == service.BalanceSettlementRelease {
		return settlement, nil
	}
	baseCost := review.ActualUnits * 0.5
	settlement.Billing = &service.UsageBillingCommand{RequestID: requestID, APIKeyID: *task.APIKeyID, UserID: task.UserID, AccountID: *task.AccountID,
		AccountType: service.AccountTypeAPIKey, Model: task.UpstreamModel, BillingType: service.BillingTypeBalance, MediaType: "video",
		ActualCost: review.ActualCost, TotalCost: baseCost, APIKeyQuotaCost: review.ActualCost, APIKeyRateLimitCost: review.ActualCost,
		AccountQuotaCost: baseCost, Platform: task.Provider, PlatformQuotaCost: review.ActualCost, OccurredAt: *task.FinishedAt}
	rate := 1.0
	usage := &service.UsageLog{RequestID: requestID, APIKeyID: *task.APIKeyID, UserID: task.UserID, AccountID: *task.AccountID,
		Model: task.UpstreamModel, BillingType: service.BillingTypeBalance, ActualCost: review.ActualCost, TotalCost: baseCost,
		RateMultiplier: 2, AccountRateMultiplier: &rate, CreatedAt: *task.FinishedAt, VideoCount: 1}
	return settlement, usage
}

func TestVideoBillingReviewLowRiskIsAuditedAndSettlesOnce(t *testing.T) {
	ctx := context.Background()
	repo, admin, task, actor, _ := newVideoBillingReviewFixture(t, 5)
	request := videoBillingReviewRequest(task, actor)
	result, err := admin.ProposeVideoBillingReview(ctx, task.PublicID, request)
	require.NoError(t, err)
	require.Equal(t, "approved", result.Review.Status)
	require.False(t, result.Review.RequiresSecondActor)
	require.Equal(t, service.VideoBillingCapturePending, result.Task.BillingState)
	require.NotNil(t, result.Task.BillingReviewID)
	authorization, err := repo.VerifyVideoBillingReview(ctx, result.Task)
	require.NoError(t, err)
	require.Equal(t, result.Review.ID, authorization.ID)
	request.ApprovalThresholdUSD = 0
	replayed, err := admin.ProposeVideoBillingReview(ctx, task.PublicID, request)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, result.Review.ID, replayed.Review.ID)
	require.Equal(t, 100.0, replayed.Review.ApprovalThresholdUSD)
	request.ActualUnits = 4
	_, err = admin.ProposeVideoBillingReview(ctx, task.PublicID, request)
	require.ErrorIs(t, err, service.ErrVideoReviewConflict)
	settlement, usage := reviewedVideoSettlement(result.Task, result.Review)
	settlement.Hold.BillingReviewID = 0
	_, err = repo.billing.SettleVideoBalance(ctx, settlement, usage)
	require.ErrorContains(t, err, "outbox v4")
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
	settlement.Hold.BillingReviewID, settlement.Hold.RequestFingerprint = result.Review.ID, ""
	paid, err := repo.billing.SettleVideoBalance(ctx, settlement, usage)
	require.NoError(t, err)
	require.True(t, paid.Applied)
	var version int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT payload_version FROM usage_billing_outbox WHERE id = $1`, paid.OutboxReceipt.ID).Scan(&version))
	require.Equal(t, usageBillingOutboxPayloadVersionV4, version)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, paid.OutboxReceipt.WorkerID, paid.OutboxReceipt.ID))
	repeated, err := repo.billing.SettleVideoBalance(ctx, settlement, usage)
	require.NoError(t, err)
	require.False(t, repeated.Applied)
	assertVideoBudgetTotals(t, task.UserID, 1, 997, 0)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, repeated.OutboxReceipt.WorkerID, repeated.OutboxReceipt.ID))
	var decisions, operations int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_billing_reviews WHERE task_id=$1`, task.ID).Scan(&decisions))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_billing_review_actions WHERE task_id=$1`, task.ID).Scan(&operations))
	require.Equal(t, 1, decisions)
	require.Equal(t, 1, operations)
}

func TestVideoBillingReviewHighRiskRequiresIndependentApproval(t *testing.T) {
	ctx := context.Background()
	repo, admin, task, proposer, approver := newVideoBillingReviewFixture(t, 150)
	result, err := admin.ProposeVideoBillingReview(ctx, task.PublicID, videoBillingReviewRequest(task, proposer))
	require.NoError(t, err)
	require.Equal(t, "pending", result.Review.Status)
	require.Nil(t, result.Task.BillingReviewID)
	assertVideoBudgetTotals(t, task.UserID, 1, 850, 150)
	decision := service.VideoBillingReviewDecision{ActorID: proposer, OperationKey: "review:approve", ExpectedVersion: result.Task.Version, Approve: true, Reason: "Second review verified"}
	_, err = admin.DecideVideoBillingReview(ctx, task.PublicID, result.Review.ID, decision)
	require.ErrorIs(t, err, service.ErrVideoReviewForbidden)
	decision.ActorID = approver
	approved, err := admin.DecideVideoBillingReview(ctx, task.PublicID, result.Review.ID, decision)
	require.NoError(t, err)
	require.Equal(t, "approved", approved.Review.Status)
	require.Equal(t, approver, *approved.Review.DecidedBy)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET status = 'disabled' WHERE id = $1`, proposer)
	require.NoError(t, err)
	replayed, err := admin.DecideVideoBillingReview(ctx, task.PublicID, result.Review.ID, decision)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	_, err = repo.VerifyVideoBillingReview(ctx, approved.Task)
	require.NoError(t, err)
}

func TestVideoBillingReviewRejectsInvalidActorEvidenceAndOldWrites(t *testing.T) {
	ctx := context.Background()
	_, admin, task, actor, _ := newVideoBillingReviewFixture(t, 5)
	request := videoBillingReviewRequest(task, actor)
	request.ActorID = task.UserID
	_, err := admin.ProposeVideoBillingReview(ctx, task.PublicID, request)
	require.ErrorIs(t, err, service.ErrVideoReviewForbidden)
	request.ActorID, request.EvidenceRef = actor, "https://private.example/secret"
	_, err = admin.ProposeVideoBillingReview(ctx, task.PublicID, request)
	require.ErrorIs(t, err, service.ErrVideoReviewRequired)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET billing_state = 'capture_pending', actual_cost = 3, actual_units = 3 WHERE id = $1`, task.ID)
	require.ErrorContains(t, err, "approved review")
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
}

func TestVideoBillingReviewConcurrentReplayAndApprovalRace(t *testing.T) {
	ctx := context.Background()
	_, admin, task, proposer, approver := newVideoBillingReviewFixture(t, 150)
	request := videoBillingReviewRequest(task, proposer)
	type outcome struct {
		result *service.VideoBillingReviewResult
		err    error
	}
	results := make(chan outcome, 8)
	var wait sync.WaitGroup
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := admin.ProposeVideoBillingReview(ctx, task.PublicID, request)
			results <- outcome{result, err}
		}()
	}
	wait.Wait()
	close(results)
	var proposed *service.VideoBillingReviewResult
	for result := range results {
		require.NoError(t, result.err)
		if proposed == nil {
			proposed = result.result
		}
		require.Equal(t, proposed.Review.ID, result.result.Review.ID)
	}
	decisions := make(chan error, 2)
	for _, approve := range []bool{true, false} {
		wait.Add(1)
		go func(approve bool) {
			defer wait.Done()
			_, err := admin.DecideVideoBillingReview(ctx, task.PublicID, proposed.Review.ID,
				service.VideoBillingReviewDecision{ActorID: approver, OperationKey: fmt.Sprintf("decision:%t", approve), ExpectedVersion: proposed.Task.Version, Approve: approve, Reason: "Independent evidence checked"})
			decisions <- err
		}(approve)
	}
	wait.Wait()
	close(decisions)
	winners := 0
	for err := range decisions {
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, service.ErrVideoVersionConflict)
		}
	}
	require.Equal(t, 1, winners)
	var actions int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_billing_review_actions WHERE task_id = $1`, task.ID).Scan(&actions))
	require.Equal(t, 2, actions)
}

func TestVideoBillingReviewChangedFactsCannotBeApproved(t *testing.T) {
	ctx := context.Background()
	repo, admin, task, proposer, approver := newVideoBillingReviewFixture(t, 150)
	proposed, err := admin.ProposeVideoBillingReview(ctx, task.PublicID, videoBillingReviewRequest(task, proposer))
	require.NoError(t, err)
	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID,
		service.VideoTaskTransition{UsageSnapshot: map[string]any{"seconds": 99}})
	require.NoError(t, err)
	decision := service.VideoBillingReviewDecision{ActorID: approver, OperationKey: "approve:changed", ExpectedVersion: task.Version, Approve: true, Reason: "Independent review checked"}
	_, err = admin.DecideVideoBillingReview(ctx, task.PublicID, proposed.Review.ID, decision)
	require.ErrorIs(t, err, service.ErrVideoReviewConflict)
	decision.Approve, decision.OperationKey = false, "reject:changed"
	rejected, err := admin.DecideVideoBillingReview(ctx, task.PublicID, proposed.Review.ID, decision)
	require.NoError(t, err)
	require.Equal(t, "rejected", rejected.Review.Status)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_billing_reviews SET reason = 'changed history' WHERE id = $1`, proposed.Review.ID)
	require.ErrorContains(t, err, "immutable")
	assertVideoBudgetTotals(t, task.UserID, 1, 850, 150)
}

func TestVideoBillingReviewReleaseAndZeroCaptureAreRecoverable(t *testing.T) {
	for _, action := range []service.BalanceSettlementAction{service.BalanceSettlementRelease, service.BalanceSettlementCapture} {
		t.Run(string(action), func(t *testing.T) {
			ctx := context.Background()
			repo, admin, task, proposer, approver := newVideoBillingReviewFixture(t, 5)
			request := videoBillingReviewRequest(task, proposer)
			request.Action, request.ActualUnits = action, 0
			result, err := admin.ProposeVideoBillingReview(ctx, task.PublicID, request)
			require.NoError(t, err)
			if result.Review.RequiresSecondActor {
				result, err = admin.DecideVideoBillingReview(ctx, task.PublicID, result.Review.ID,
					service.VideoBillingReviewDecision{ActorID: approver, OperationKey: "zero:approve", ExpectedVersion: result.Task.Version, Approve: true, Reason: "Confirmed zero billable usage"})
				require.NoError(t, err)
			}
			settlement, usage := reviewedVideoSettlement(result.Task, result.Review)
			paid, err := repo.billing.SettleVideoBalance(ctx, settlement, usage)
			require.NoError(t, err)
			assertVideoBudgetTotals(t, task.UserID, 1, 1000, 0)
			require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, paid.OutboxReceipt.WorkerID, paid.OutboxReceipt.ID))
		})
	}
}

func TestVideoBillingReviewFrozenIntentSurvivesLaterFactsAndCannotBeRewritten(t *testing.T) {
	ctx := context.Background()
	repo, admin, task, proposer, _ := newVideoBillingReviewFixture(t, 5)
	approved, err := admin.ProposeVideoBillingReview(ctx, task.PublicID, videoBillingReviewRequest(task, proposer))
	require.NoError(t, err)
	settlement, usage := reviewedVideoSettlement(approved.Task, approved.Review)
	commandJSON, usageJSON, err := marshalBalanceSettlementOutboxPayload(settlement, usage)
	require.NoError(t, err)
	event, err := repo.billing.enqueueAndClaimUsageBillingOutboxPayload(ctx, "review-before-crash", settlement.Hold.RequestID, *task.APIKeyID,
		settlement.Hold.RequestFingerprint, usageBillingOutboxPayloadVersionV4, commandJSON, usageJSON)
	require.NoError(t, err)
	_, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID,
		service.VideoTaskTransition{ResponseMetadata: map[string]any{"execution_spec_conflict": 1}})
	require.NoError(t, err)
	event.Command.PlatformQuotaSnapshot = &service.UsageBillingPlatformQuotaSnapshot{}
	require.NoError(t, repo.billing.UpdateUsageBillingOutboxCommand(ctx, "review-before-crash", event.ID, event.Command))
	_, err = integrationDB.ExecContext(ctx, `UPDATE usage_billing_outbox SET command_payload = jsonb_set(command_payload, '{billing,account_quota_cost}', '0') WHERE id = $1`, event.ID)
	require.ErrorContains(t, err, "immutable")
	paid, err := repo.billing.CompleteUsageBillingOutbox(ctx, "review-before-crash", event)
	require.NoError(t, err)
	require.True(t, paid.Applied)
	assertVideoBudgetTotals(t, task.UserID, 1, 997, 0)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, "review-before-crash", event.ID))
}

func TestVideoBillingReviewCharacterPersistenceProofKeepsAutomaticRecovery(t *testing.T) {
	ctx := context.Background()
	repo, _, task, _, _ := newVideoBillingReviewFixture(t, 5)
	_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET operation = 'character_create', generation_state = 'completed',
		billing_unit = 'request', provider_task_id = 'char_review_recovery', last_error_code = 'resource_persistence_failed' WHERE id = $1`, task.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET billing_state = 'capture_pending', actual_units = 1, actual_cost = 1 WHERE id = $1`, task.ID)
	require.ErrorContains(t, err, "approved review")
	resources := &videoResourceRepository{db: integrationDB}
	_, err = resources.CreateVideoResource(ctx, service.VideoCreateResourceParams{
		Owner: service.VideoOwner{UserID: task.UserID, APIKeyID: *task.APIKeyID}, Provider: task.Provider, AccountID: *task.AccountID,
		SourceTaskID: &task.ID, ProviderResourceID: "char_review_recovery", Model: task.UpstreamModel, Status: "ready", Metadata: map[string]any{"name": "fixture"},
	})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET billing_state = 'capture_pending', actual_units = 1, actual_cost = 1 WHERE id = $1`, task.ID)
	require.NoError(t, err)
	task, err = repo.GetVideoTaskByPublicID(ctx, task.PublicID)
	require.NoError(t, err)
	settlement, usage := reviewedVideoSettlement(task, &service.VideoBillingReview{Action: service.BalanceSettlementCapture, ActualUnits: 1, ActualCost: 1})
	paid, err := repo.billing.SettleVideoBalance(ctx, settlement, usage)
	require.NoError(t, err)
	assertVideoBudgetTotals(t, task.UserID, 1, 999, 0)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, paid.OutboxReceipt.WorkerID, paid.OutboxReceipt.ID))
}

func TestVideoBillingReviewExistingIntentCannotBeReplaced(t *testing.T) {
	ctx := context.Background()
	repo, admin, task, proposer, _ := newVideoBillingReviewFixture(t, 5)
	approved, err := admin.ProposeVideoBillingReview(ctx, task.PublicID, videoBillingReviewRequest(task, proposer))
	require.NoError(t, err)
	settlement, usage := reviewedVideoSettlement(approved.Task, approved.Review)
	commandJSON, usageJSON, err := marshalBalanceSettlementOutboxPayload(settlement, usage)
	require.NoError(t, err)
	_, err = repo.billing.enqueueAndClaimUsageBillingOutboxPayload(ctx, "immutable-review", settlement.Hold.RequestID, *task.APIKeyID,
		settlement.Hold.RequestFingerprint, usageBillingOutboxPayloadVersionV4, commandJSON, usageJSON)
	require.NoError(t, err)
	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID,
		service.VideoTaskTransition{BillingState: service.VideoBillingManualReview})
	require.NoError(t, err)
	request := videoBillingReviewRequest(task, proposer)
	request.OperationKey = "changed:decision"
	_, err = admin.ProposeVideoBillingReview(ctx, task.PublicID, request)
	require.ErrorIs(t, err, service.ErrVideoReviewIntentExists)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
}
