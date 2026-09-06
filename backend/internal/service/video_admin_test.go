package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type videoAdminRepositoryStub struct {
	VideoAdminRepository
	task  *VideoTask
	tasks *videoTaskRepoStub
}

func (r *videoAdminRepositoryStub) ProposeVideoBillingReview(ctx context.Context, _ string, request VideoBillingReviewRequest) (*VideoBillingReviewResult, error) {
	if request.ExpectedVersion != r.task.Version {
		return nil, ErrVideoVersionConflict
	}
	review, err := PlanVideoBillingReview(r.task, request)
	if err != nil {
		return nil, err
	}
	if review.RequiresSecondActor {
		return &VideoBillingReviewResult{Review: review, Task: r.task}, nil
	}
	review.Status = "approved"
	state := VideoBillingCapturePending
	if request.Action == BalanceSettlementRelease {
		state = VideoBillingReleasePending
	}
	updated, err := r.tasks.TransitionVideoTask(ctx, r.task.PublicID, VideoTaskTransition{BillingState: state, ActualUnits: &review.ActualUnits, ActualCost: &review.ActualCost, EventType: "admin_billing_review_propose"})
	return &VideoBillingReviewResult{Review: review, Task: updated}, err
}
func (*videoAdminRepositoryStub) DecideVideoBillingReview(context.Context, string, int64, VideoBillingReviewDecision) (*VideoBillingReviewResult, error) {
	return nil, ErrVideoReviewRequired
}
func (*videoAdminRepositoryStub) ListVideoBillingReviews(context.Context, string) ([]*VideoBillingReview, error) {
	return nil, nil
}

func videoAdminReviewContextForTest(task *VideoTask, version int64) context.Context {
	return WithVideoBillingReviewRequest(WithVideoAdminExpectedVersion(context.Background(), task.PublicID, version),
		VideoBillingReviewRequest{ActorID: 99, OperationKey: "review:test", Reason: "Verified provider evidence", EvidenceRef: "ticket:TEST"})
}

func (r *videoAdminRepositoryStub) GetVideoTaskAdmin(context.Context, string) (*VideoTask, error) {
	return r.task, nil
}

func newVideoAdminServiceForBillingTest(task *VideoTask) (*VideoAdminService, *videoTaskRepoStub, *videoQueueStub) {
	tasks := &videoTaskRepoStub{task: task}
	queue := &videoQueueStub{}
	service := NewVideoAdminService(&videoAdminRepositoryStub{task: task, tasks: tasks}, tasks, nil, queue, nil, nil, nil)
	return service, tasks, queue
}

func TestVideoAdminResolveBillingCaptureUsesImmutablePriceSnapshot(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationFailed
	task.BillingState = VideoBillingManualReview
	admin, tasks, queue := newVideoAdminServiceForBillingTest(task)

	updated, err := admin.ResolveBillingCapture(videoAdminReviewContextForTest(task, task.Version), task.PublicID, 3)

	require.NoError(t, err)
	require.Equal(t, VideoBillingCapturePending, updated.BillingState)
	require.InDelta(t, 3, *updated.ActualUnits, 0.000001)
	require.InDelta(t, 3, *updated.ActualCost, 0.000001)
	require.Equal(t, "admin_billing_review_propose", tasks.transitions[0].EventType)
	require.NotEmpty(t, queue.enqueued)
}

func TestVideoAdminRejectsStaleClientVersionBeforeBillingMutation(t *testing.T) {
	task := baseVideoWorkerTask()
	task.Version = 7
	task.GenerationState = VideoGenerationFailed
	task.BillingState = VideoBillingManualReview
	admin, tasks, queue := newVideoAdminServiceForBillingTest(task)
	ctx := videoAdminReviewContextForTest(task, 6)
	_, err := admin.ResolveBillingCapture(ctx, task.PublicID, 3)
	require.ErrorIs(t, err, ErrVideoVersionConflict)
	require.Empty(t, tasks.transitions)
	require.Empty(t, queue.enqueued)
	ctx = videoAdminReviewContextForTest(task, 7)
	_, err = admin.ResolveBillingCapture(ctx, task.PublicID, 3)
	require.NoError(t, err)
	require.Len(t, tasks.transitions, 1)
}

func TestVideoAdminRejectsCharacterCaptureBeforeResourcePersistence(t *testing.T) {
	task := baseVideoWorkerTask()
	task.Operation = VideoOperationCharacterCreate
	task.GenerationState = VideoGenerationCompleted
	task.BillingState = VideoBillingManualReview
	code := "resource_persistence_failed"
	task.LastErrorCode = &code
	admin, tasks, queue := newVideoAdminServiceForBillingTest(task)

	updated, err := admin.ResolveBillingCapture(videoAdminReviewContextForTest(task, task.Version), task.PublicID, 1)

	require.ErrorIs(t, err, ErrVideoInvalidTransition)
	require.Nil(t, updated)
	require.Empty(t, tasks.transitions)
	require.Empty(t, queue.enqueued)
}

func TestVideoAdminResolveBillingReleaseRejectsCompletedTask(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationCompleted
	task.BillingState = VideoBillingManualReview
	admin, tasks, _ := newVideoAdminServiceForBillingTest(task)

	_, err := admin.ResolveBillingRelease(videoAdminReviewContextForTest(task, task.Version), task.PublicID)

	require.ErrorIs(t, err, ErrVideoInvalidTransition)
	require.Empty(t, tasks.transitions)
}

func TestVideoAdminResolveBillingReleaseQueuesFailedTask(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationFailed
	task.BillingState = VideoBillingManualReview
	admin, tasks, queue := newVideoAdminServiceForBillingTest(task)

	updated, err := admin.ResolveBillingRelease(videoAdminReviewContextForTest(task, task.Version), task.PublicID)

	require.NoError(t, err)
	require.Equal(t, VideoBillingReleasePending, updated.BillingState)
	require.Equal(t, "admin_billing_review_propose", tasks.transitions[0].EventType)
	require.NotEmpty(t, queue.enqueued)
}
