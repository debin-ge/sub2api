//go:build integration

package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func videoRepositoryWriteContext(t *testing.T, repo *videoTaskRepository, ctx context.Context, publicID string) context.Context {
	t.Helper()
	task, err := repo.GetVideoTaskByPublicID(ctx, publicID)
	require.NoError(t, err)
	return service.WithVideoTaskWriteGuard(ctx, task.ID, task.Version)
}

func TestVideoTaskLeaseEpochFencesExpiredOwnerAndSameNameTakeover(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "lease-fence", "lease-hash", 2))
	require.NoError(t, err)
	first, err := repo.ClaimVideoTask(ctx, task.PublicID, "same-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, first)
	firstLease := service.VideoTaskLeaseFromTask(first)
	require.Equal(t, int64(1), firstLease.Epoch)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET lease_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE id = $1`, task.ID)
	require.NoError(t, err)
	_, err = repo.RenewVideoTaskLease(ctx, firstLease, time.Minute)
	require.ErrorIs(t, err, service.ErrVideoLeaseLost)
	_, err = repo.AppendVideoTaskEvent(service.WithVideoTaskLease(ctx, firstLease), service.VideoTaskEvent{TaskID: &task.ID, EventType: "stale_worker_observation", EventHash: "stale-event"})
	require.ErrorIs(t, err, service.ErrVideoLeaseLost)
	oldCtx := service.WithVideoTaskWriteGuard(service.WithVideoTaskLease(ctx, firstLease), first.ID, first.Version)
	_, err = repo.TransitionVideoTask(oldCtx, task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.ErrorIs(t, err, service.ErrVideoLeaseLost)
	second, err := repo.ClaimVideoTask(ctx, task.PublicID, "same-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, second)
	secondLease := service.VideoTaskLeaseFromTask(second)
	require.Equal(t, int64(2), secondLease.Epoch)
	require.Equal(t, first.Version, second.Version)
	retryAt := time.Now().Add(time.Hour)
	require.ErrorIs(t, repo.ReleaseVideoTaskLease(ctx, firstLease, &retryAt), service.ErrVideoLeaseLost)
	_, err = repo.TransitionVideoTask(oldCtx, task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.ErrorIs(t, err, service.ErrVideoLeaseLost)
	_, err = repo.RenewVideoTaskLease(ctx, firstLease, time.Minute)
	require.ErrorIs(t, err, service.ErrVideoLeaseLost)
	secondCtx := service.WithVideoTaskWriteGuard(service.WithVideoTaskLease(ctx, secondLease), second.ID, second.Version)
	updated, err := repo.TransitionVideoTask(secondCtx, task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.NoError(t, err)
	require.Equal(t, second.Version+1, updated.Version)
	_, err = repo.TransitionVideoTask(secondCtx, task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmissionUnknown})
	require.ErrorIs(t, err, service.ErrVideoVersionConflict)
	expiry, err := repo.RenewVideoTaskLease(ctx, secondLease, 2*time.Minute)
	require.NoError(t, err)
	require.True(t, expiry.After(*second.LeaseExpiresAt))
	require.NoError(t, repo.ReleaseVideoTaskLease(ctx, secondLease, &retryAt))
	loaded, err := repo.GetVideoTaskByPublicID(ctx, task.PublicID)
	require.NoError(t, err)
	require.Nil(t, loaded.LeaseOwner)
	require.Nil(t, loaded.NextActionAt)
	require.Equal(t, updated.Version, loaded.Version)
}

func TestVideoTaskWriteRequiresVersionAndWakeupDoesNotRewriteState(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "guarded", "guard-hash", 2))
	require.NoError(t, err)
	_, err = repo.TransitionVideoTask(ctx, task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.ErrorIs(t, err, service.ErrVideoInvalidRequest)
	writeCtx := service.WithVideoTaskWriteGuard(ctx, task.ID, task.Version)
	task, err = repo.TransitionVideoTask(writeCtx, task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.NoError(t, err)
	_, err = repo.SaveVideoProviderAccepted(writeCtx, task.PublicID, service.VideoProviderAcceptance{ProviderTaskID: "provider-guarded", GenerationState: service.VideoGenerationQueued})
	require.ErrorIs(t, err, service.ErrVideoVersionConflict)
	writeCtx = service.WithVideoTaskWriteGuard(ctx, task.ID, task.Version)
	future := time.Now().Add(time.Hour)
	task, err = repo.SaveVideoProviderAccepted(writeCtx, task.PublicID, service.VideoProviderAcceptance{ProviderTaskID: "provider-guarded", GenerationState: service.VideoGenerationQueued, NextActionAt: &future})
	require.NoError(t, err)
	version := task.Version
	task, err = repo.WakeVideoTask(ctx, task.PublicID, time.Now())
	require.NoError(t, err)
	require.Equal(t, version, task.Version)
	require.Equal(t, service.VideoGenerationQueued, task.GenerationState)
	require.True(t, task.NextActionAt.Before(future))
}

func TestVideoTaskLostLeaseCannotCreateIntentButDurableIntentSurvivesTakeover(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "intent-lease", "intent-hash", 4))
	require.NoError(t, err)
	settlement, usage := prepareVideoBudgetSettlement(t, repo, task, service.BalanceSettlementCapture, 2)
	first, err := repo.ClaimVideoTask(ctx, task.PublicID, "stale-intent-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, first)
	oldCtx := service.WithVideoTaskWriteGuard(service.WithVideoTaskLease(ctx, service.VideoTaskLeaseFromTask(first)), first.ID, first.Version)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET lease_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE id = $1`, task.ID)
	require.NoError(t, err)
	_, err = repo.billing.SettleVideoBalance(oldCtx, settlement, usage)
	require.ErrorIs(t, err, service.ErrVideoLeaseLost)
	var intents int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_outbox WHERE api_key_id = $1`, key.ID).Scan(&intents))
	require.Zero(t, intents)
	current, err := repo.ClaimVideoTask(ctx, task.PublicID, "current-intent-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, current)
	currentCtx := service.WithVideoTaskWriteGuard(service.WithVideoTaskLease(ctx, service.VideoTaskLeaseFromTask(current)), current.ID, current.Version)
	commandJSON, usageJSON, err := marshalBalanceSettlementOutboxPayload(settlement, usage)
	require.NoError(t, err)
	event, err := repo.billing.enqueueAndClaimUsageBillingOutboxPayload(currentCtx, "durable-finance-worker", settlement.Hold.RequestID, key.ID, settlement.Hold.RequestFingerprint, usageBillingOutboxPayloadVersionV2, commandJSON, usageJSON)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET lease_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE id = $1`, task.ID)
	require.NoError(t, err)
	_, err = repo.ClaimVideoTask(ctx, task.PublicID, "third-worker", time.Minute)
	require.NoError(t, err)
	result, err := repo.billing.CompleteUsageBillingOutbox(ctx, "durable-finance-worker", event)
	require.NoError(t, err)
	require.True(t, result.Applied)
	repeated, err := repo.billing.CompleteUsageBillingOutbox(ctx, "durable-finance-worker", event)
	require.NoError(t, err)
	require.Equal(t, result.NewBalance, repeated.NewBalance)
	assertVideoBudgetTotals(t, user.ID, 1, 8, 0)
	var usageCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE api_key_id = $1`, key.ID).Scan(&usageCount))
	require.Equal(t, 1, usageCount)
}

func TestVideoTaskLeaseCannotOverwriteAdminVersionOrSchedule(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "admin-fence", "admin-hash", 2))
	require.NoError(t, err)
	claimed, err := repo.ClaimVideoTask(ctx, task.PublicID, "worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	lease := service.VideoTaskLeaseFromTask(claimed)
	future := time.Now().Add(5 * time.Hour)
	adminCtx := service.WithVideoTaskWriteGuard(ctx, task.ID, task.Version)
	_, err = repo.TransitionVideoTask(adminCtx, task.PublicID, service.VideoTaskTransition{NextActionAt: &future, EventType: "admin_rescheduled"})
	require.NoError(t, err)
	workerCtx := service.WithVideoTaskWriteGuard(service.WithVideoTaskLease(ctx, lease), task.ID, task.Version)
	_, err = repo.TransitionVideoTask(workerCtx, task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.ErrorIs(t, err, service.ErrVideoVersionConflict)
	retryAt := time.Now().Add(time.Minute)
	require.NoError(t, repo.ReleaseVideoTaskLease(ctx, lease, &retryAt))
	latest, err := repo.GetVideoTaskByPublicID(ctx, task.PublicID)
	require.NoError(t, err)
	require.WithinDuration(t, future, *latest.NextActionAt, time.Millisecond)
	require.Equal(t, service.VideoGenerationHeld, latest.GenerationState)
}

func TestVideoTaskSubmitAndPreSubmitDeleteHaveOneCASWinner(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "submit-delete", "body", 2))
	require.NoError(t, err)
	writeCtx := service.WithVideoTaskWriteGuard(ctx, task.ID, task.Version)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, transition := range []service.VideoTaskTransition{
		{GenerationState: service.VideoGenerationSubmitting},
		{GenerationState: service.VideoGenerationCancelled, BillingState: service.VideoBillingReleasePending, DeleteState: service.VideoDeleteDeleted},
	} {
		wait.Add(1)
		go func(transition service.VideoTaskTransition) {
			defer wait.Done()
			<-start
			_, err := repo.TransitionVideoTask(writeCtx, task.PublicID, transition)
			results <- err
		}(transition)
	}
	close(start)
	wait.Wait()
	close(results)
	winners, conflicts := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, service.ErrVideoVersionConflict) {
			conflicts++
		} else {
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, winners)
	require.Equal(t, 1, conflicts)
	assertVideoBudgetTotals(t, user.ID, 1, 8, 2)
}

func TestVideoTaskLocalCancellationAtomicallyRecordsTombstoneAndReleaseIntent(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "local-cancel", "body", 2))
	require.NoError(t, err)
	task, err = repo.TransitionVideoTask(service.WithVideoTaskWriteGuard(ctx, task.ID, task.Version), task.PublicID, service.VideoTaskTransition{
		GenerationState: service.VideoGenerationCancelled, BillingState: service.VideoBillingReleasePending,
		DeleteState: service.VideoDeleteDeleted, EventType: "delete_pre_submit",
	})
	require.NoError(t, err)
	require.Equal(t, service.VideoDeleteDeleted, task.DeleteState)
	require.NotNil(t, task.DeletedAt)
	require.Equal(t, service.VideoBillingReleasePending, task.BillingState)
	assertVideoBudgetTotals(t, user.ID, 1, 8, 2)
}
