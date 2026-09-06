package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func videoReviewPlanFixture() (*VideoTask, VideoBillingReviewRequest) {
	task := baseVideoWorkerTask()
	task.Operation, task.GenerationState, task.BillingState = VideoOperationGenerate, VideoGenerationFailed, VideoBillingManualReview
	return task, VideoBillingReviewRequest{ActorID: 99, ExpectedVersion: task.Version, OperationKey: "review:plan", Action: BalanceSettlementCapture,
		ActualUnits: 3, Reason: "Provider invoice verified", EvidenceRef: "invoice:TEST", ApprovalThresholdUSD: 100}
}

func TestVideoBillingReviewRiskAndFrozenPrice(t *testing.T) {
	for _, risk := range []string{"ordinary", "threshold", "over_hold", "zero_capture", "specification"} {
		t.Run(risk, func(t *testing.T) {
			task, request := videoReviewPlanFixture()
			switch risk {
			case "threshold":
				task.HoldAmount = floatPointer(100)
			case "over_hold":
				request.ActualUnits = 6
			case "zero_capture":
				request.ActualUnits = 0
			case "specification":
				task.ResponseMetadata = map[string]any{"execution_spec_conflict": 1}
				_, err := PlanVideoBillingReview(task, request)
				require.ErrorIs(t, err, ErrVideoReviewQuoteAcknowledgement)
				request.HonorFrozenQuote = true
			}
			review, err := PlanVideoBillingReview(task, request)
			require.NoError(t, err)
			require.Equal(t, risk != "ordinary", review.RequiresSecondActor)
			require.Equal(t, request.ActualUnits, review.ActualCost)
			require.Equal(t, VideoBillingManualReview, task.BillingState)
		})
	}
}

func TestVideoBillingReviewRejectsUnattributedOrUnsafeRequests(t *testing.T) {
	for _, invalid := range []string{"actor", "reason", "credential", "evidence_credential", "url", "key", "nan", "token_fraction"} {
		t.Run(invalid, func(t *testing.T) {
			task, request := videoReviewPlanFixture()
			switch invalid {
			case "actor":
				request.ActorID = 0
			case "reason":
				request.Reason = ""
			case "credential":
				request.Reason = "Copied sk-provider-secret"
			case "evidence_credential":
				request.EvidenceRef = "sk-provider-secret"
			case "url":
				request.EvidenceRef = "https://example.test/signed"
			case "key":
				request.OperationKey = ""
			case "nan":
				request.ActualUnits = math.NaN()
			case "token_fraction":
				task.BillingUnit = videoUnit(VideoBillingUnitVideoToken)
				request.ActualUnits = 1.5
			}
			_, err := PlanVideoBillingReview(task, request)
			require.Error(t, err)
		})
	}
	task, _ := videoReviewPlanFixture()
	admin, tasks, _ := newVideoAdminServiceForBillingTest(task)
	_, err := admin.ResolveBillingCapture(context.Background(), task.PublicID, 3)
	require.ErrorIs(t, err, ErrVideoReviewRequired)
	_, err = admin.ResolveBillingRelease(context.Background(), task.PublicID)
	require.ErrorIs(t, err, ErrVideoReviewRequired)
	require.Empty(t, tasks.transitions)
}

type reviewedVideoTaskRepoStub struct {
	*videoTaskRepoStub
	review *VideoBillingReview
	err    error
}

func (r *reviewedVideoTaskRepoStub) VerifyVideoBillingReview(context.Context, *VideoTask) (*VideoBillingReview, error) {
	return r.review, r.err
}

func TestVideoBillingReviewWorkerUsesApprovedQuoteException(t *testing.T) {
	task, _ := videoReviewPlanFixture()
	identifier := int64(44)
	task.BillingReviewID, task.BillingState, task.ActualUnits, task.ActualCost = &identifier, VideoBillingCapturePending, floatPointer(3), floatPointer(3)
	task.ResponseMetadata = map[string]any{"execution_spec_conflict": 1}
	worker, tasks, settlements, _ := newVideoWorkerForTest(task, nil)
	worker.tasks = &reviewedVideoTaskRepoStub{videoTaskRepoStub: tasks, review: &VideoBillingReview{ID: 44, Status: "approved", HonorFrozenQuote: true}}
	require.NoError(t, worker.settle(context.Background(), task))
	require.NotNil(t, settlements.settlement)
	require.Equal(t, int64(44), settlements.settlement.Hold.BillingReviewID)
	require.Equal(t, 3.0, settlements.settlement.Hold.ActualAmount)
}

func TestVideoBillingReviewZeroUsageDoesNotRestoreEstimatedAccountCost(t *testing.T) {
	task, _ := videoReviewPlanFixture()
	task.ActualUnits = floatPointer(0)
	command, usage, err := buildVideoUsageSettlement(task, VideoTaskCaptureRequestID(task.PublicID), 0)
	require.NoError(t, err)
	require.Zero(t, command.AccountQuotaCost)
	require.Zero(t, command.TotalCost)
	require.Zero(t, usage.TotalCost)
}

func TestVideoBillingReviewWorkerReturnsStaleAuthorizationToReview(t *testing.T) {
	task, _ := videoReviewPlanFixture()
	identifier := int64(44)
	task.BillingReviewID, task.BillingState, task.ActualUnits, task.ActualCost = &identifier, VideoBillingCapturePending, floatPointer(3), floatPointer(3)
	worker, tasks, settlements, _ := newVideoWorkerForTest(task, nil)
	worker.tasks = &reviewedVideoTaskRepoStub{videoTaskRepoStub: tasks, err: ErrVideoReviewConflict}
	require.NoError(t, worker.settle(context.Background(), task))
	require.Equal(t, VideoBillingManualReview, tasks.task.BillingState)
	require.Nil(t, settlements.settlement)
}

func TestVideoBillingReviewEvidenceRefreshDoesNotBypassApproval(t *testing.T) {
	task, _ := videoReviewPlanFixture()
	task.GenerationState = VideoGenerationCompleted
	observed := &ProviderVideoTask{Status: VideoGenerationCompleted, Metadata: map[string]any{"seconds": 8}}
	decision := videoObservedBillingDecision(task, VideoGenerationCompleted, observed)
	require.Equal(t, VideoBillingManualReview, decision.state)
	require.Equal(t, "billing_review_required", decision.errorCode)
	require.Nil(t, decision.actualCost)
}
