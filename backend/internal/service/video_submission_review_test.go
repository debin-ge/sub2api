package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoSubmissionReviewRejectsCredentialsAndURLsAsProviderIDs(t *testing.T) {
	request := VideoSubmissionReviewRequest{ActorID: 99, ExpectedVersion: 0, OperationKey: "submission:validate", Action: VideoSubmissionCreated,
		Reason: "Original ownership evidence verified", EvidenceRef: "ticket:UNKNOWN"}
	for _, identifier := range []string{"", "sk-provider-secret", "https://provider.test/video?token=secret", "../videos", "video_1?token=secret", "video_1/child", strings.Repeat("a", 256)} {
		request.ProviderTaskID = identifier
		require.ErrorIs(t, ValidateVideoSubmissionReviewRequest(request), ErrVideoInvalidRequest)
	}
	for _, identifier := range []string{"video_exact", "char_123", "job:part.1-test"} {
		request.ProviderTaskID = identifier
		require.NoError(t, ValidateVideoSubmissionReviewRequest(request))
	}
}

func TestVideoSubmissionReviewObservationIsReadOnlyAndExact(t *testing.T) {
	for _, scenario := range []string{"video", "character", "wrong_id", "changed_identity", "wrong_owner", "specification_conflict"} {
		t.Run(scenario, func(t *testing.T) {
			provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video_confirmed", Status: VideoGenerationCompleted, Metadata: map[string]any{"seconds": 8}},
				character: &ProviderVideoResource{ProviderResourceID: "char_confirmed", Status: "ready", Metadata: map[string]any{"name": "Character"}}}
			svc, tasks, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
			svc.accounts.(*videoAccountRepoStub).accounts[0].ProviderIdentityVersion = 1
			task := baseVideoWorkerTask()
			task.GenerationState, task.BillingState, task.ProviderTaskID = VideoGenerationSubmissionUnknown, VideoBillingManualReview, nil
			tasks.task = task
			providerID := "video_confirmed"
			review := &VideoSubmissionReview{Action: VideoSubmissionCreated, ProviderTaskID: &providerID, AccountIdentityVersion: 1}
			switch scenario {
			case "character":
				providerID, task.Operation, task.BillingUnit = "char_confirmed", VideoOperationCharacterCreate, videoUnit(VideoBillingUnitRequest)
			case "wrong_id":
				provider.result.ProviderTaskID = "some_other_task"
			case "changed_identity":
				review.AccountIdentityVersion = 2
			case "wrong_owner":
				owner := int64(888)
				svc.accounts.(*videoAccountRepoStub).accounts[0].OwnerUserID = &owner
			case "specification_conflict":
				bindVideoExecutionSpecForTest(t, task, 0)
				provider.result.Metadata["model"] = "unexpected-model"
			}
			observed, err := svc.observeSubmissionCreated(context.Background(), task, review)
			if scenario == "wrong_id" || scenario == "changed_identity" || scenario == "wrong_owner" {
				require.Error(t, err)
				require.Nil(t, observed)
			} else {
				require.NoError(t, err)
				require.Equal(t, providerID, observed.Acceptance.ProviderTaskID)
				if scenario == "specification_conflict" {
					require.Equal(t, VideoBillingManualReview, observed.Acceptance.BillingState)
				} else {
					require.Equal(t, VideoBillingCapturePending, observed.Acceptance.BillingState)
				}
			}
			require.Zero(t, provider.createCalls)
			require.Nil(t, tasks.task.ProviderTaskID)
			require.Empty(t, queue.enqueued)
			require.Empty(t, svc.resources.(*videoResourceRepoStub).bySource)
			if scenario == "changed_identity" || scenario == "wrong_owner" {
				require.Zero(t, provider.getCalls)
			}
			if scenario == "character" {
				require.Equal(t, 1, provider.characterGets)
				require.Zero(t, provider.getCalls)
			}
		})
	}
}

type videoSubmissionReviewServiceRepoStub struct {
	VideoAdminRepository
	VideoSubmissionReviewRepository
	prepared   *VideoSubmissionReviewResult
	prepareErr error
	decisions  int
}

func (r *videoSubmissionReviewServiceRepoStub) PrepareVideoSubmissionDecision(context.Context, string, int64, VideoBillingReviewDecision) (*VideoSubmissionReviewResult, error) {
	return r.prepared, r.prepareErr
}
func (r *videoSubmissionReviewServiceRepoStub) DecideVideoSubmissionReview(context.Context, string, int64, VideoBillingReviewDecision, *VideoSubmissionObservation) (*VideoSubmissionReviewResult, error) {
	r.decisions++
	return r.prepared, nil
}

func TestVideoSubmissionReviewReplayAndDenialDoNotCallProvider(t *testing.T) {
	provider := &videoProviderStub{}
	svc, _, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	repository := &videoSubmissionReviewServiceRepoStub{prepared: &VideoSubmissionReviewResult{Task: task, Replayed: true}}
	admin := &VideoAdminService{repository: repository, taskSvc: svc}
	ctx := WithVideoAdminExpectedVersion(context.Background(), task.PublicID, task.Version)
	ctx = WithVideoBillingReviewRequest(ctx, VideoBillingReviewRequest{ActorID: 2, OperationKey: "submission:decision", Reason: "Independent original evidence verified"})
	updated, err := admin.DecideSubmissionReview(ctx, task.PublicID, 1, true)
	require.NoError(t, err)
	require.Equal(t, task, updated)
	require.Zero(t, provider.getCalls)
	require.Zero(t, repository.decisions)
	repository.prepareErr = ErrVideoReviewForbidden
	_, err = admin.DecideSubmissionReview(ctx, task.PublicID, 1, true)
	require.ErrorIs(t, err, ErrVideoReviewForbidden)
	require.Zero(t, provider.getCalls)
	require.Zero(t, repository.decisions)
}
