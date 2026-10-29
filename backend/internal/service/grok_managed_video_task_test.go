package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func newManagedGrokTaskTest(t *testing.T) (*VideoTaskService, *videoTaskRepoStub, *grokManagedHTTPStub, VideoSubmitRequest) {
	t.Helper()
	provider, account, upstream := grokManagedTestProvider()
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.Platform = PlatformGrok
	group.ModelPricing[0].Platform = PlatformGrok
	group.ModelPricing[0].Models = []string{"grok-imagine-video-1.5", "grok-imagine-video"}
	svc, repo, _ := newVideoTaskServiceForTest(&videoProviderStub{}, group, nil)
	svc.providers = NewVideoProviderRegistry(provider)
	svc.accounts = &videoAccountRepoStub{accounts: []Account{*account}}
	svc.cfg.Gateway.Video.GrokManagedEnabled = true
	request := videoSubmitRequestForTest()
	request.Model = "grok-imagine-video-1.5"
	request.Size = ""
	request.APIKey.Group = group
	request.Grok = &GrokVideoOptions{ClientRequestHash: "canonical-client-hash"}
	return svc, repo, upstream, request
}
func TestGrokManagedTaskHoldsBeforeSubmitting(t *testing.T) {
	svc, repo, upstream, request := newManagedGrokTaskTest(t)
	upstream.before = func() {
		require.NotNil(t, repo.task)
		require.Equal(t, VideoBillingHeld, repo.task.BillingState)
		require.Equal(t, VideoGenerationSubmitting, repo.task.GenerationState)
		require.InDelta(t, 8, repo.create.HoldAmount, 1e-8)
	}
	result, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, VideoProviderGrok, result.Task.Provider)
	require.Equal(t, VideoGenerationQueued, result.Task.GenerationState)
	require.Equal(t, VideoBillingHeld, result.Task.BillingState)
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, request.Grok.ClientRequestHash, repo.create.RequestAttributes["grok_client_request_hash"])
	require.Equal(t, true, *repo.create.RequestAttributes["audio_enabled"].(*bool))
}
func TestGrokManagedTaskRejectsBeforeUpstream(t *testing.T) {
	for _, scenario := range []string{"disabled", "subscription", "pricing missing", "admission denied"} {
		t.Run(scenario, func(t *testing.T) {
			svc, repo, upstream, request := newManagedGrokTaskTest(t)
			switch scenario {
			case "disabled":
				svc.cfg.Gateway.Video.GrokManagedEnabled = false
			case "subscription":
				svc.groups.(*videoGroupRepoStub).group.SubscriptionType = SubscriptionTypeSubscription
			case "pricing missing":
				svc.groups.(*videoGroupRepoStub).group.ModelPricing = nil
			case "admission denied":
				svc.admission.(*videoAdmissionStub).err = ErrVideoInsufficientBalance
			}
			_, err := svc.Submit(context.Background(), request)
			require.Error(t, err)
			require.Zero(t, upstream.calls)
			require.Nil(t, repo.task)
		})
	}
}
func TestGrokManagedTaskUnknownSubmissionRemainsHeld(t *testing.T) {
	svc, repo, upstream, request := newManagedGrokTaskTest(t)
	upstream.status = 502
	result, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, VideoGenerationSubmitting, result.Task.GenerationState)
	require.Equal(t, VideoBillingHeld, result.Task.BillingState)
	require.NotNil(t, repo.task.SubmissionUnknownAt)
	require.Equal(t, 1, upstream.calls)
}
func TestGrokManagedTaskRejectionReleasesHold(t *testing.T) {
	svc, repo, upstream, request := newManagedGrokTaskTest(t)
	upstream.status = 400
	_, err := svc.Submit(context.Background(), request)
	require.Error(t, err)
	require.Equal(t, VideoGenerationFailed, repo.task.GenerationState)
	require.Equal(t, VideoBillingReleased, repo.task.BillingState)
	require.Equal(t, 1, upstream.calls)
}
func TestGrokManagedTaskFractionalEditHold(t *testing.T) {
	svc, repo, upstream, request := newManagedGrokTaskTest(t)
	request.Operation = VideoOperationEdit
	request.Model = "grok-imagine-video"
	request.Seconds = 0
	source := grokVerifiedSourceForTest(8.25)
	request.Grok.Source = source
	request.Inputs = []VideoInput{source.Input}
	_, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)
	require.InDelta(t, 9, repo.create.HoldAmount, 1e-8)
	require.InDelta(t, 8.25, repo.create.EstimatedUnits, 1e-8)
	require.Equal(t, 1, upstream.calls)
}

type grokScopedIngressRepo struct {
	*videoTaskRepoStub
	expected string
	receipt  *VideoTask
}

func (r *grokScopedIngressRepo) GetVideoTaskByIdempotency(_ context.Context, _ int64, _ string, key string) (*VideoTask, error) {
	if key == r.expected {
		return r.receipt, nil
	}
	return nil, ErrVideoTaskNotFound
}
func TestGrokManagedReplayPrecedesChangedCompositeRoute(t *testing.T) {
	svc, repo, _, request := newManagedGrokTaskTest(t)
	request.APIKey.Group.Platform = PlatformComposite
	internal := GrokManagedIdempotencyKey(request.APIKey, VideoOperationGenerate, "client-key")
	receipt := &VideoTask{UserID: request.APIKey.UserID, APIKeyID: &request.APIKey.ID, GroupID: request.APIKey.GroupID, Provider: VideoProviderGrok}
	svc.tasks = &grokScopedIngressRepo{videoTaskRepoStub: repo, expected: internal, receipt: receipt}
	// No composite resolver is installed: successful replay must use the receipt.
	route, err := svc.ResolveVideoIngress(context.Background(), request.APIKey, VideoOperationGenerate, "changed-alias", "", "client-key")
	require.NoError(t, err)
	require.True(t, route.ManagedReplay)
	require.Equal(t, PlatformGrok, route.Decision.TargetPlatform)
	changedKey := *request.APIKey
	groupCopy := *request.APIKey.Group
	groupCopy.ID++
	changedKey.Group = &groupCopy
	changedKey.GroupID = &groupCopy.ID
	_, err = svc.ResolveVideoIngress(context.Background(), &changedKey, VideoOperationGenerate, "changed-alias", "", "client-key")
	require.ErrorIs(t, err, ErrVideoIdempotencyConflict)
}

func TestGrokPreparationAndSubmissionShareOneRPMToken(t *testing.T) {
	for _, key := range []string{"", "stable-key"} {
		t.Run(key, func(t *testing.T) {
			svc, _, upstream, request := newManagedGrokTaskTest(t)
			request.IdempotencyKey = key
			ctx, err := svc.PrepareGrokSubmission(context.Background(), request.APIKey, request.Operation, key)
			require.NoError(t, err)
			_, err = svc.Submit(ctx, request)
			require.NoError(t, err)
			admission := svc.admission.(*videoAdmissionStub)
			require.Len(t, admission.tokens, 2)
			require.NotEmpty(t, admission.tokens[0])
			require.Equal(t, admission.tokens[0], admission.tokens[1])
			require.Equal(t, 1, upstream.calls)
		})
	}
}
