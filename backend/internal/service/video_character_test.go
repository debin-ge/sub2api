package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func characterAccessFixture() (*VideoTaskService, *videoTaskRepoStub, *videoProviderStub, *VideoResource, *VideoTask) {
	resource := &VideoResource{ID: 2, PublicID: NewVideoResourceID(), UserID: 42, AccountID: 11,
		Provider: VideoProviderOpenAI, ProviderResourceID: "char_upstream", Status: "ready"}
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), map[string]*VideoResource{resource.PublicID: resource})
	for _, task := range tasks.sources {
		tasks.task = task
		return svc, tasks, provider, resource, task
	}
	panic("missing character fixture")
}

func TestVideoCharacterAccessRequiresSettledSource(t *testing.T) {
	for _, billing := range []string{VideoBillingHeld, VideoBillingCapturePending, VideoBillingManualReview, VideoBillingReleased, VideoBillingCaptured} {
		t.Run(billing, func(t *testing.T) {
			svc, _, _, resource, task := characterAccessFixture()
			svc.cfg.Gateway.Video.DisclosurePolicy = config.VideoDisclosureIdentity
			task.BillingState = billing
			_, readErr := svc.GetCharacterForOwner(context.Background(), resource.UserID, resource.PublicID)
			_, disclosureErr := svc.ResourceDisclosureForOwner(context.Background(), resource.UserID, resource.PublicID)
			refs, _, reuseErr := svc.resolveCharacters(context.Background(), resource.UserID, []string{resource.PublicID})
			taskDisclosure, taskDisclosureErr := svc.DisclosureForOwner(context.Background(), resource.UserID, task.PublicID)
			require.NoError(t, taskDisclosureErr)
			if billing == VideoBillingCaptured {
				require.NoError(t, readErr)
				require.NoError(t, disclosureErr)
				require.NoError(t, reuseErr)
				require.Len(t, refs, 1)
				require.Equal(t, resource.ProviderResourceID, taskDisclosure.ProviderTaskID)
			} else {
				require.ErrorIs(t, readErr, ErrVideoSettlementPending)
				require.ErrorIs(t, disclosureErr, ErrVideoSettlementPending)
				require.ErrorIs(t, reuseErr, ErrVideoSettlementPending)
				require.Empty(t, refs)
				require.Equal(t, config.VideoDisclosureNone, taskDisclosure.Policy)
				require.Empty(t, taskDisclosure.ProviderTaskID)
			}
		})
	}
}

func TestVideoCharacterAccessRejectsInvalidBindingAndDeletion(t *testing.T) {
	for _, scenario := range []string{"missing_source", "wrong_source", "wrong_owner", "wrong_operation", "deleted", "expired", "delete_pending"} {
		t.Run(scenario, func(t *testing.T) {
			svc, _, _, resource, task := characterAccessFixture()
			want := ErrVideoResourceNotFound
			switch scenario {
			case "missing_source":
				resource.SourceTaskID = nil
			case "wrong_source":
				resource.SourceTaskID = videoInt64Ptr(task.ID + 1)
			case "wrong_owner":
				task.UserID++
			case "wrong_operation":
				task.Operation = VideoOperationGenerate
			case "deleted":
				resource.DeletedAt = timePointer(svc.now())
			case "expired":
				resource.ExpiresAt = timePointer(svc.now().Add(-time.Second))
			case "delete_pending":
				task.DeleteState = VideoDeleteRequested
				want = ErrVideoDeletePending
			}
			_, err := svc.GetCharacterForOwner(context.Background(), resource.UserID, resource.PublicID)
			require.ErrorIs(t, err, want)
		})
	}
}

func TestVideoCharacterDeletionUsesDurableTaskAndCorrectProviderEndpoint(t *testing.T) {
	svc, _, provider, resource, task := characterAccessFixture()
	ctx := context.Background()
	require.ErrorIs(t, svc.DeleteCharacterForOwner(ctx, resource.UserID, resource.PublicID), ErrVideoDeletePending)
	require.Equal(t, VideoDeleteRequested, task.DeleteState)
	require.Zero(t, provider.characterDeletes)
	ctx = WithVideoTaskLease(ctx, VideoTaskLease{TaskID: task.ID, Owner: "character-worker", Epoch: 1})
	provider.deleteErr = errors.New("provider unavailable")
	_, err := svc.RetryDeleteTask(ctx, task)
	require.Error(t, err)
	require.Equal(t, VideoDeleteFailed, task.DeleteState)
	provider.deleteErr = nil
	updated, err := svc.RetryDeleteTask(ctx, task)
	require.NoError(t, err)
	require.Equal(t, VideoDeleteDeleted, updated.DeleteState)
	require.Equal(t, 2, provider.characterDeletes)
	require.Zero(t, provider.deleteCalls)
	resource.Status, resource.DeletedAt = "deleted", timePointer(svc.now())
	require.NoError(t, svc.DeleteCharacterForOwner(ctx, resource.UserID, resource.PublicID))
	require.ErrorIs(t, svc.DeleteCharacterForOwner(ctx, resource.UserID+1, resource.PublicID), ErrVideoResourceNotFound)
}

func TestVideoCharacterDeleteCannotBypassSettlement(t *testing.T) {
	svc, _, provider, resource, task := characterAccessFixture()
	task.BillingState = VideoBillingManualReview
	require.ErrorIs(t, svc.DeleteCharacterForOwner(context.Background(), resource.UserID, resource.PublicID), ErrVideoSettlementPending)
	require.Equal(t, VideoDeleteNone, task.DeleteState)
	require.Zero(t, provider.characterDeletes)
}
