//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVideoCallbackLeaseRenewalAndExpiredWritesAreFenced(t *testing.T) {
	ctx := context.Background()
	repo, _, callbacks, user, key, account := newVideoRepositoryFixture(t, 10)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), uuid.NewString(), "callback-fence", 1))
	require.NoError(t, err)
	delivery, _, err := callbacks.EnqueueVideoCallback(ctx, service.VideoCallbackDelivery{
		TaskID: task.ID, EventID: uuid.NewString(), EventType: "video.completed", EventFingerprint: uuid.NewString(),
		Payload: map[string]any{"id": task.PublicID}, TargetURLEnc: "encrypted-target",
		NextAttemptAt: time.Now().Add(-time.Second), ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	first, err := callbacks.ClaimVideoCallbacks(ctx, "worker:attempt-one", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.NoError(t, callbacks.RenewVideoCallbackLease(ctx, delivery.ID, "worker:attempt-one", 2*time.Minute))
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_callback_deliveries SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, delivery.ID)
	require.NoError(t, err)
	require.ErrorIs(t, callbacks.RenewVideoCallbackLease(ctx, delivery.ID, "worker:attempt-one", time.Minute), service.ErrVideoCallbackLeaseLost)
	require.ErrorIs(t, callbacks.MarkVideoCallbackDelivered(ctx, delivery.ID, "worker:attempt-one", 204), service.ErrVideoCallbackLeaseLost)
	require.ErrorIs(t, callbacks.RetryVideoCallback(ctx, delivery.ID, "worker:attempt-one", time.Now(), 500, "late"), service.ErrVideoCallbackLeaseLost)
	require.ErrorIs(t, callbacks.QuarantineVideoCallback(ctx, delivery.ID, "worker:attempt-one", "late"), service.ErrVideoCallbackLeaseLost)
	second, err := callbacks.ClaimVideoCallbacks(ctx, "worker:attempt-two", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.ErrorIs(t, callbacks.MarkVideoCallbackDelivered(ctx, delivery.ID, "worker:attempt-one", 204), service.ErrVideoCallbackLeaseLost)
	require.NoError(t, callbacks.MarkVideoCallbackDelivered(ctx, delivery.ID, "worker:attempt-two", 204))
	require.ErrorIs(t, callbacks.RenewVideoCallbackLease(ctx, delivery.ID, "worker:attempt-two", time.Minute), service.ErrVideoCallbackLeaseLost)
	quarantine := *delivery
	quarantine.EventID, quarantine.EventFingerprint = uuid.NewString(), uuid.NewString()
	stored, _, err := callbacks.EnqueueVideoCallback(ctx, quarantine)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_callback_deliveries SET last_status_code=500 WHERE id=$1`, stored.ID)
	require.NoError(t, err)
	claimed, err := callbacks.ClaimVideoCallbacks(ctx, "worker:quarantine", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, callbacks.QuarantineVideoCallback(ctx, stored.ID, "worker:quarantine", "security validation failed"))
	var status string
	var lastStatus int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status,last_status_code FROM video_callback_deliveries WHERE id=$1`, stored.ID).Scan(&status, &lastStatus))
	require.Equal(t, "quarantined", status)
	require.Equal(t, 500, lastStatus)
}

func TestVideoCharacterSettlementAndAtomicDeletion(t *testing.T) {
	ctx := context.Background()
	repo, resources, _, user, key, account := newVideoRepositoryFixture(t, 10)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), uuid.NewString(), "character-retirement", 1)
	params.Operation, params.Endpoint, params.BillingUnit = service.VideoOperationCharacterCreate, service.CompositeRouteEndpointVideoCharacters, service.VideoBillingUnitRequest
	task, _, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID,
		service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.NoError(t, err)
	one := 1.0
	task, err = repo.SaveVideoProviderAccepted(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoProviderAcceptance{
		ProviderTaskID: "char_" + uuid.NewString(), GenerationState: service.VideoGenerationCompleted,
		BillingState: service.VideoBillingCapturePending, ActualUnits: &one, ActualCost: &one,
	})
	require.NoError(t, err)
	resource, err := resources.CreateVideoResource(ctx, service.VideoCreateResourceParams{
		Owner: service.VideoOwner{UserID: user.ID, APIKeyID: key.ID}, Provider: task.Provider, AccountID: account.ID,
		SourceTaskID: &task.ID, ProviderResourceID: *task.ProviderTaskID, Model: task.UpstreamModel,
	})
	require.NoError(t, err)
	taskService := service.NewVideoTaskService(repo, resources, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err = taskService.GetCharacterForOwner(ctx, user.ID, resource.PublicID)
	require.ErrorIs(t, err, service.ErrVideoSettlementPending)
	requestID := service.VideoTaskCaptureRequestID(task.PublicID)
	now := time.Now().UTC()
	result, err := repo.billing.SettleVideoBalance(ctx, &service.BalanceSettlementCommand{
		TaskID: task.ID, Action: service.BalanceSettlementCapture,
		Hold: service.BalanceHoldCommand{RequestID: requestID, APIKeyID: key.ID, UserID: user.ID,
			Scope: service.BalanceHoldScopeVideoTask, RefID: task.PublicID, HoldAmount: 1, ActualAmount: 1},
		Billing: &service.UsageBillingCommand{RequestID: requestID, APIKeyID: key.ID, UserID: user.ID,
			AccountID: account.ID, AccountType: service.AccountTypeAPIKey, Model: task.UpstreamModel,
			ActualCost: 1, TotalCost: 1, APIKeyQuotaCost: 1, APIKeyRateLimitCost: 1,
			Platform: task.Provider, PlatformQuotaCost: 1, OccurredAt: now},
	}, &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: requestID,
		Model: task.UpstreamModel, ActualCost: 1, TotalCost: 1, VideoCount: 1, CreatedAt: now})
	require.NoError(t, err)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID))
	_, err = taskService.GetCharacterForOwner(ctx, user.ID, resource.PublicID)
	require.NoError(t, err)
	require.ErrorIs(t, taskService.DeleteCharacterForOwner(ctx, user.ID, resource.PublicID), service.ErrVideoDeletePending)
	_, err = taskService.GetCharacterForOwner(ctx, user.ID, resource.PublicID)
	require.ErrorIs(t, err, service.ErrVideoDeletePending)
	task, err = repo.ClaimVideoTask(ctx, task.PublicID, "character-delete-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	writeCtx := service.WithVideoTaskWriteGuard(service.WithVideoTaskLease(ctx, service.VideoTaskLeaseFromTask(task)), task.ID, task.Version)
	_, err = repo.TransitionVideoTask(writeCtx, task.PublicID, service.VideoTaskTransition{
		DeleteState: service.VideoDeleteDeleted, EventPayload: map[string]any{"invalid": make(chan int)},
	})
	require.Error(t, err, "event failure must roll back task and resource retirement")
	retained, err := resources.GetVideoResourceForOwner(ctx, user.ID, resource.PublicID)
	require.NoError(t, err)
	require.Equal(t, "ready", retained.Status)
	task, err = repo.TransitionVideoTask(writeCtx, task.PublicID, service.VideoTaskTransition{DeleteState: service.VideoDeleteDeleted})
	require.NoError(t, err)
	require.Equal(t, service.VideoDeleteDeleted, task.DeleteState)
	_, err = resources.GetVideoResourceForOwner(ctx, user.ID, resource.PublicID)
	require.ErrorIs(t, err, service.ErrVideoResourceNotFound)
	deleted, err := resources.GetVideoResourceForOwnerIncludingDeleted(ctx, user.ID, resource.PublicID)
	require.NoError(t, err)
	require.NotNil(t, deleted.DeletedAt)
	require.Equal(t, "deleted", deleted.Status)
	require.NoError(t, taskService.DeleteCharacterForOwner(ctx, user.ID, resource.PublicID))
	_, err = resources.GetVideoResourceForOwnerIncludingDeleted(ctx, user.ID+1, resource.PublicID)
	require.ErrorIs(t, err, service.ErrVideoResourceNotFound)
}
