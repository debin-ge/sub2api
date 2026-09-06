//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newVideoRepositoryFixture(t *testing.T, balance float64) (*videoTaskRepository, *videoResourceRepository, *videoCallbackRepository, *service.User, *service.APIKey, *service.Account) {
	t.Helper()
	client := testEntClient(t)
	billing := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("video-repo-%d-%s@example.com", time.Now().UnixNano(), uuid.NewString()),
		PasswordHash: "hash",
		Balance:      balance,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-video-repo-" + uuid.NewString(),
		Name:   "video-repo",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name:     "video-repo-" + uuid.NewString(),
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
	})
	cleanupVideoIntegrationFixture(t, user.ID, apiKey.ID, account.ID)
	return &videoTaskRepository{db: integrationDB, billing: billing},
		&videoResourceRepository{db: integrationDB},
		&videoCallbackRepository{db: integrationDB},
		user, apiKey, account
}

func cleanupVideoIntegrationFixture(t *testing.T, userID, apiKeyID, accountID int64) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		tx, err := integrationDB.BeginTx(ctx, nil)
		require.NoError(t, err)
		deletions := []struct {
			statement string
			argument  int64
		}{
			{`DELETE FROM video_create_intents WHERE user_id = $1`, userID},
			{`DELETE FROM video_billing_review_actions WHERE task_id IN (SELECT id FROM video_tasks WHERE user_id = $1)`, userID},
			{`DELETE FROM video_billing_reviews WHERE task_id IN (SELECT id FROM video_tasks WHERE user_id = $1)`, userID},
			{`DELETE FROM video_submission_review_actions WHERE task_id IN (SELECT id FROM video_tasks WHERE user_id = $1)`, userID},
			{`DELETE FROM video_submission_reviews WHERE task_id IN (SELECT id FROM video_tasks WHERE user_id = $1)`, userID},
			{`DELETE FROM video_callback_deliveries WHERE task_id IN (SELECT id FROM video_tasks WHERE user_id = $1)`, userID},
			{`DELETE FROM video_resources WHERE user_id = $1`, userID},
			{`DELETE FROM video_task_events WHERE task_id IN (SELECT id FROM video_tasks WHERE user_id = $1)`, userID},
			{`DELETE FROM video_tasks WHERE user_id = $1`, userID},
			{`DELETE FROM usage_logs WHERE api_key_id = $1`, apiKeyID},
			{`DELETE FROM usage_billing_outbox WHERE api_key_id = $1`, apiKeyID},
			{`DELETE FROM usage_billing_dedup WHERE api_key_id = $1`, apiKeyID},
			{`DELETE FROM usage_billing_dedup_archive WHERE api_key_id = $1`, apiKeyID},
			{`DELETE FROM api_keys WHERE id = $1`, apiKeyID},
			{`DELETE FROM scheduler_outbox WHERE account_id = $1`, accountID},
			{`DELETE FROM accounts WHERE id = $1`, accountID},
			{`DELETE FROM users WHERE id = $1`, userID},
		}
		for _, deletion := range deletions {
			_, err = tx.ExecContext(ctx, deletion.statement, deletion.argument)
			require.NoError(t, err, deletion.statement)
		}
		require.NoError(t, tx.Commit())
	})
}

func videoCreateParams(user *service.User, apiKey *service.APIKey, account *service.Account, publicID, key, hash string, hold float64) service.VideoCreateTaskParams {
	return service.VideoCreateTaskParams{
		PublicID:       publicID,
		Owner:          service.VideoOwner{UserID: user.ID, APIKeyID: apiKey.ID},
		AccountID:      account.ID,
		Provider:       service.VideoProviderOpenAI,
		Operation:      service.VideoOperationGenerate,
		Endpoint:       service.CompositeRouteEndpointVideos,
		RequestedModel: service.OpenAIVideoModelSora2,
		PublicModel:    service.OpenAIVideoModelSora2,
		ChannelModel:   service.OpenAIVideoModelSora2,
		UpstreamModel:  service.OpenAIVideoModelSora2,
		RequestHash:    hash,
		IdempotencyKey: key,
		InputManifest: []service.VideoInputManifestEntry{{
			Role: service.VideoInputRoleReferenceImage, MIMEType: "image/png", Size: 100, SHA256: "abc",
		}},
		RequestAttributes: map[string]any{"seconds": 8, "size": "1280x720"},
		BillingUnit:       service.VideoBillingUnitSecond,
		EstimatedUnits:    8,
		PriceSnapshot:     map[string]any{"rule_id": 1},
		Currency:          "USD",
		HoldAmount:        hold,
	}
}

func TestVideoTaskRepositoryCreateHeldIsAtomicIdempotentAndOwnerScoped(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	publicID := service.NewVideoTaskID()
	params := videoCreateParams(user, apiKey, account, publicID, "idem-"+uuid.NewString(), "hash-a", 2)

	task, created, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	require.True(t, created)
	byIdempotency, err := repo.GetVideoTaskByIdempotency(ctx, user.ID, params.Endpoint, params.IdempotencyKey)
	require.NoError(t, err)
	require.Equal(t, task.ID, byIdempotency.ID)
	require.Equal(t, publicID, task.PublicID)
	require.Equal(t, service.VideoGenerationHeld, task.GenerationState)
	require.Equal(t, service.VideoBillingHeld, task.BillingState)
	require.Equal(t, account.ID, *task.AccountID)
	require.Equal(t, params.InputManifest, task.InputManifest)

	replayed, created, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, task.ID, replayed.ID)

	conflict := params
	conflict.RequestHash = "hash-b"
	_, _, err = repo.CreateHeldVideoTask(ctx, conflict)
	require.ErrorIs(t, err, service.ErrVideoIdempotencyConflict)

	owned, err := repo.GetVideoTaskForOwner(ctx, user.ID, publicID)
	require.NoError(t, err)
	require.Equal(t, task.ID, owned.ID)
	_, err = repo.GetVideoTaskForOwner(ctx, user.ID+9999, publicID)
	require.ErrorIs(t, err, service.ErrVideoTaskNotFound)

	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance, frozen_balance FROM users WHERE id = $1`, user.ID).Scan(&balance, &frozen))
	require.InDelta(t, 8, balance, 0.000001)
	require.InDelta(t, 2, frozen, 0.000001)
	var heldEvents int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_task_events WHERE task_id = $1 AND event_type = 'balance_held'`, task.ID).Scan(&heldEvents))
	require.Equal(t, 1, heldEvents)
}

func TestVideoTaskRepositoryFindsProviderIDOnlyWithinOwnerScope(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	params := videoCreateParams(user, apiKey, account, service.NewVideoTaskID(), "idem-"+uuid.NewString(), "hash-provider-owner", 2)
	task, created, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	require.True(t, created)
	_, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.NoError(t, err)
	providerID := "video_provider_" + uuid.NewString()
	task, err = repo.SaveVideoProviderAccepted(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoProviderAcceptance{
		ProviderTaskID: providerID, GenerationState: service.VideoGenerationQueued,
	})
	require.NoError(t, err)

	owned, err := repo.GetVideoTaskByProviderIDForOwner(ctx, user.ID, providerID)
	require.NoError(t, err)
	require.Equal(t, task.ID, owned.ID)
	_, err = repo.GetVideoTaskByProviderIDForOwner(ctx, user.ID+9999, providerID)
	require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
}

func TestVideoTaskRepositoryConcurrentIdempotencyReturnsSameTask(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 20)
	params := videoCreateParams(user, apiKey, account, service.NewVideoTaskID(), "idem-"+uuid.NewString(), "hash-concurrent", 2)

	const callers = 8
	tasks := make(chan *service.VideoTask, callers)
	errs := make(chan error, callers)
	var wait sync.WaitGroup
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			candidate := params
			candidate.PublicID = service.NewVideoTaskID()
			task, _, err := repo.CreateHeldVideoTask(ctx, candidate)
			if err != nil {
				errs <- err
				return
			}
			tasks <- task
		}()
	}
	wait.Wait()
	close(tasks)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var taskID int64
	for task := range tasks {
		if taskID == 0 {
			taskID = task.ID
		}
		require.Equal(t, taskID, task.ID)
	}
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_tasks WHERE user_id = $1 AND endpoint = $2 AND idempotency_key = $3`, user.ID, params.Endpoint, params.IdempotencyKey).Scan(&count))
	require.Equal(t, 1, count)
	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance, frozen_balance FROM users WHERE id = $1`, user.ID).Scan(&balance, &frozen))
	require.InDelta(t, 18, balance, 0.000001)
	require.InDelta(t, 2, frozen, 0.000001)
}

func TestVideoTaskRepositoryInsufficientBalanceRollsBackTask(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 1)
	publicID := service.NewVideoTaskID()
	params := videoCreateParams(user, apiKey, account, publicID, "idem-"+uuid.NewString(), "hash-low", 2)

	_, _, err := repo.CreateHeldVideoTask(ctx, params)
	require.ErrorIs(t, err, service.ErrVideoInsufficientBalance)
	var tasks, holds int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_tasks WHERE public_id = $1`, publicID).Scan(&tasks))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2`, service.VideoTaskHoldRequestID(publicID), apiKey.ID).Scan(&holds))
	require.Zero(t, tasks)
	require.Zero(t, holds)
}

func TestVideoTaskRepositoryListUsesStableOwnerScopedCursor(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	created := make([]*service.VideoTask, 0, 3)
	for i := 0; i < 3; i++ {
		publicID := service.NewVideoTaskID()
		task, wasCreated, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(
			user, apiKey, account, publicID, "idem-"+uuid.NewString(), fmt.Sprintf("hash-list-%d", i), 1,
		))
		require.NoError(t, err)
		require.True(t, wasCreated)
		created = append(created, task)
	}

	first, err := repo.ListVideoTasksForOwner(ctx, user.ID, service.VideoTaskFilter{
		Status: service.VideoGenerationHeld, Model: service.OpenAIVideoModelSora2,
		Operation: service.VideoOperationGenerate, Limit: 1, Order: "desc",
	})
	require.NoError(t, err)
	require.Len(t, first.Data, 1)
	require.True(t, first.HasMore)
	require.NotEmpty(t, first.After)

	second, err := repo.ListVideoTasksForOwner(ctx, user.ID, service.VideoTaskFilter{
		Status: service.VideoGenerationHeld, Model: service.OpenAIVideoModelSora2,
		Operation: service.VideoOperationGenerate, Limit: 1, Order: "desc", After: first.After,
	})
	require.NoError(t, err)
	require.Len(t, second.Data, 1)
	require.NotEqual(t, first.Data[0].ID, second.Data[0].ID)
	require.Greater(t, first.Data[0].ID, second.Data[0].ID)
	secondByLastID, err := repo.ListVideoTasksForOwner(ctx, user.ID, service.VideoTaskFilter{
		Status: service.VideoGenerationHeld, Model: service.OpenAIVideoModelSora2,
		Operation: service.VideoOperationGenerate, Limit: 1, Order: "desc", After: first.Data[0].PublicID,
	})
	require.NoError(t, err)
	require.Len(t, secondByLastID.Data, 1)
	require.Equal(t, second.Data[0].ID, secondByLastID.Data[0].ID)

	otherOwner, err := repo.ListVideoTasksForOwner(ctx, user.ID+9999, service.VideoTaskFilter{Limit: 10})
	require.NoError(t, err)
	require.Empty(t, otherOwner.Data)

	_, err = repo.ListVideoTasksForOwner(ctx, user.ID, service.VideoTaskFilter{After: "not-a-cursor"})
	require.ErrorIs(t, err, service.ErrVideoInvalidRequest)
	require.Len(t, created, 3)
}

func TestVideoTaskRepositoryTransitionsProviderAcceptanceAndLease(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	publicID := service.NewVideoTaskID()
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, apiKey, account, publicID, "idem-"+uuid.NewString(), "hash-flow", 2))
	require.NoError(t, err)

	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, publicID), publicID, service.VideoTaskTransition{
		GenerationState: service.VideoGenerationSubmitting,
		EventType:       "submitting",
	})
	require.NoError(t, err)
	require.Equal(t, service.VideoGenerationSubmitting, task.GenerationState)
	next := time.Now().UTC().Add(-time.Second)
	progress := 0.0
	task, err = repo.SaveVideoProviderAccepted(videoRepositoryWriteContext(t, repo, ctx, publicID), publicID, service.VideoProviderAcceptance{
		ProviderTaskID:  "video_upstream_123",
		ProviderStatus:  "queued",
		GenerationState: service.VideoGenerationQueued,
		Progress:        &progress,
		ContentVariants: []string{"video", "thumbnail", "spritesheet"},
		NextActionAt:    &next,
	})
	require.NoError(t, err)
	require.Equal(t, "video_upstream_123", *task.ProviderTaskID)

	byProvider, err := repo.GetVideoTaskByProviderID(ctx, service.VideoProviderOpenAI, account.ID, "video_upstream_123")
	require.NoError(t, err)
	require.Equal(t, task.ID, byProvider.ID)

	claimed, err := repo.ClaimDueVideoTasks(ctx, "worker-test", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, task.ID, claimed[0].ID)
	require.Equal(t, "worker-test", *claimed[0].LeaseOwner)
	future := time.Now().UTC().Add(time.Minute)
	require.NoError(t, repo.ReleaseVideoTaskLease(ctx, service.VideoTaskLeaseFromTask(claimed[0]), &future))

	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, publicID), publicID, service.VideoTaskTransition{
		GenerationState: service.VideoGenerationCompleted,
		BillingState:    service.VideoBillingCapturePending,
		ProviderStatus:  "completed",
		EventType:       "provider_completed",
	})
	require.NoError(t, err)
	require.Equal(t, service.VideoGenerationCompleted, task.GenerationState)
	require.Equal(t, service.VideoBillingCapturePending, task.BillingState)
	require.Equal(t, service.VideoGenerationInProgress, service.ProjectVideoStatus(task))

	_, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, publicID), publicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationQueued})
	require.ErrorIs(t, err, service.ErrVideoInvalidTransition)
}

func TestVideoTaskRepositoryProviderAcceptancePersistsTerminalBillingAtomically(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	publicID := service.NewVideoTaskID()
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, apiKey, account, publicID, "idem-"+uuid.NewString(), "hash-terminal-acceptance", 2))
	require.NoError(t, err)
	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, publicID), publicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.NoError(t, err)
	finished := time.Now().UTC().Add(-time.Second)
	next := time.Now().UTC().Add(-time.Millisecond)
	units, cost := 8.0, 2.0

	task, err = repo.SaveVideoProviderAccepted(videoRepositoryWriteContext(t, repo, ctx, publicID), publicID, service.VideoProviderAcceptance{
		ProviderTaskID: "video_terminal_" + uuid.NewString(), ProviderStatus: "completed",
		ProviderFinishedAt: &finished, GenerationState: service.VideoGenerationCompleted,
		BillingState: service.VideoBillingCapturePending, ActualUnits: &units, ActualCost: &cost,
		UsageSnapshot: map[string]any{"seconds": 8}, ResponseMetadata: map[string]any{"seconds": 8},
		ContentVariants: []string{"video"}, NextActionAt: &next,
	})

	require.NoError(t, err)
	require.Equal(t, service.VideoGenerationCompleted, task.GenerationState)
	require.Equal(t, service.VideoBillingCapturePending, task.BillingState)
	require.Equal(t, finished.Unix(), task.ProviderFinishedAt.Unix())
	require.InDelta(t, units, *task.ActualUnits, 0.000001)
	require.InDelta(t, cost, *task.ActualCost, 0.000001)
	require.NotNil(t, task.FinishedAt)
}

func TestVideoTaskRepositoryCapturePendingConsumesAccountConcurrency(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	firstID := service.NewVideoTaskID()
	firstParams := videoCreateParams(user, apiKey, account, firstID, "idem-"+uuid.NewString(), "hash-active-first", 1)
	firstParams.MaxAccountConcurrency = 1
	task, _, err := repo.CreateHeldVideoTask(ctx, firstParams)
	require.NoError(t, err)
	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.NoError(t, err)
	cost := 1.0
	_, err = repo.SaveVideoProviderAccepted(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoProviderAcceptance{
		ProviderTaskID: "video_active_" + uuid.NewString(), GenerationState: service.VideoGenerationCompleted,
		BillingState: service.VideoBillingCapturePending, ActualCost: &cost, NextActionAt: timePointerForRepositoryTest(time.Now().UTC()),
	})
	require.NoError(t, err)

	secondParams := videoCreateParams(user, apiKey, account, service.NewVideoTaskID(), "idem-"+uuid.NewString(), "hash-active-second", 1)
	secondParams.MaxAccountConcurrency = 1
	_, _, err = repo.CreateHeldVideoTask(ctx, secondParams)
	require.ErrorIs(t, err, service.ErrVideoAccountConcurrencyLimited)
}

func TestVideoTaskRepositoryHeldTaskConsumesAccountConcurrency(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	firstParams := videoCreateParams(user, apiKey, account, service.NewVideoTaskID(), "idem-"+uuid.NewString(), "hash-held-first", 1)
	firstParams.MaxAccountConcurrency = 1
	first, created, err := repo.CreateHeldVideoTask(ctx, firstParams)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, service.VideoGenerationHeld, first.GenerationState)
	require.Equal(t, service.VideoBillingHeld, first.BillingState)

	secondParams := videoCreateParams(user, apiKey, account, service.NewVideoTaskID(), "idem-"+uuid.NewString(), "hash-held-second", 1)
	secondParams.MaxAccountConcurrency = 1
	_, _, err = repo.CreateHeldVideoTask(ctx, secondParams)

	require.ErrorIs(t, err, service.ErrVideoAccountConcurrencyLimited)
}

func TestVideoTaskRepositoryClaimsHeldOnlyAfterSubmissionRecoveryDeadline(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	params := videoCreateParams(user, apiKey, account, service.NewVideoTaskID(), "idem-"+uuid.NewString(), "hash-held-deadline", 1)
	future := time.Now().UTC().Add(time.Minute)
	params.NextActionAt = &future
	task, _, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)

	claimed, err := repo.ClaimVideoTask(ctx, task.PublicID, "held-recovery-worker", time.Minute)
	require.NoError(t, err)
	require.Nil(t, claimed)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET next_action_at = NOW() - INTERVAL '1 second' WHERE id = $1`, task.ID)
	require.NoError(t, err)
	claimed, err = repo.ClaimVideoTask(ctx, task.PublicID, "held-recovery-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, service.VideoGenerationHeld, claimed.GenerationState)
}

func TestVideoTaskRepositoryClaimsLegacyTerminalHeldWithoutNextAction(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, apiKey, account, service.NewVideoTaskID(), "idem-"+uuid.NewString(), "hash-legacy-terminal", 1))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET generation_state = 'completed', billing_state = 'held', next_action_at = NULL WHERE id = $1`, task.ID)
	require.NoError(t, err)

	claimed, err := repo.ClaimDueVideoTasks(ctx, "legacy-terminal-worker", 10, time.Minute)

	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, task.ID, claimed[0].ID)
}

func timePointerForRepositoryTest(value time.Time) *time.Time { return &value }

func TestVideoTaskRepositoryClaimsStaleSubmittingForRecovery(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	publicID := service.NewVideoTaskID()
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, apiKey, account, publicID, "idem-"+uuid.NewString(), "hash-stale-submitting", 2))
	require.NoError(t, err)

	past := time.Now().UTC().Add(-time.Second)
	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, publicID), publicID, service.VideoTaskTransition{
		GenerationState: service.VideoGenerationSubmitting,
		NextActionAt:    &past,
		EventType:       "provider_submitting",
	})
	require.NoError(t, err)
	require.Equal(t, service.VideoGenerationSubmitting, task.GenerationState)

	claimed, err := repo.ClaimDueVideoTasks(ctx, "stale-submitting-worker", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, task.ID, claimed[0].ID)
	require.Equal(t, service.VideoGenerationSubmitting, claimed[0].GenerationState)
	require.Equal(t, "stale-submitting-worker", *claimed[0].LeaseOwner)
}

func TestVideoTaskRepositoryClaimsQueuedHintWithDatabaseLease(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	publicID := service.NewVideoTaskID()
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, apiKey, account, publicID, "idem-"+uuid.NewString(), "hash-queue-hint", 2))
	require.NoError(t, err)
	past := time.Now().UTC().Add(-time.Second)
	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, publicID), publicID, service.VideoTaskTransition{
		GenerationState: service.VideoGenerationSubmitting,
		NextActionAt:    &past,
		EventType:       "provider_submitting",
	})
	require.NoError(t, err)

	claimed, err := repo.ClaimVideoTask(ctx, publicID, "queue-worker", time.Minute)

	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, task.ID, claimed.ID)
	require.Equal(t, "queue-worker", *claimed.LeaseOwner)
	claimedAgain, err := repo.ClaimVideoTask(ctx, publicID, "other-worker", time.Minute)
	require.NoError(t, err)
	require.Nil(t, claimedAgain)
}

func TestVideoTaskRepositoryClearsExpiredProviderAccessCiphertext(t *testing.T) {
	ctx := context.Background()
	taskRepo, resourceRepo, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	publicID := service.NewVideoTaskID()
	task, _, err := taskRepo.CreateHeldVideoTask(ctx, videoCreateParams(user, apiKey, account, publicID, "idem-"+uuid.NewString(), "hash-access-cleanup", 2))
	require.NoError(t, err)
	resource, err := resourceRepo.CreateVideoResource(ctx, service.VideoCreateResourceParams{
		Owner:    service.VideoOwner{UserID: user.ID, APIKeyID: apiKey.ID},
		Provider: service.VideoProviderOpenAI, AccountID: account.ID,
		ProviderResourceID: "char_" + uuid.NewString(), Status: "ready",
	})
	require.NoError(t, err)
	past := time.Now().UTC().Add(-time.Minute)
	for table, id := range map[string]int64{"video_tasks": task.ID, "video_resources": resource.ID} {
		_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`
			UPDATE %s SET provider_access_kind = 'token', provider_access_scope = 'video_content',
				provider_access_enc = 'ciphertext', provider_access_expires_at = $2 WHERE id = $1
		`, table), id, past)
		require.NoError(t, err)
	}

	cleared, err := taskRepo.ClearExpiredVideoProviderAccess(ctx, 10)

	require.NoError(t, err)
	require.Equal(t, int64(2), cleared)
	for table, id := range map[string]int64{"video_tasks": task.ID, "video_resources": resource.ID} {
		var ciphertext *string
		require.NoError(t, integrationDB.QueryRowContext(ctx, fmt.Sprintf(`SELECT provider_access_enc FROM %s WHERE id = $1`, table), id).Scan(&ciphertext))
		require.Nil(t, ciphertext)
	}
}

func TestVideoResourceAndCallbackRepositories(t *testing.T) {
	ctx := context.Background()
	taskRepo, resourceRepo, callbackRepo, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	publicID := service.NewVideoTaskID()
	task, _, err := taskRepo.CreateHeldVideoTask(ctx, videoCreateParams(user, apiKey, account, publicID, "idem-"+uuid.NewString(), "hash-resource", 1))
	require.NoError(t, err)

	resource, err := resourceRepo.CreateVideoResource(ctx, service.VideoCreateResourceParams{
		Owner:              service.VideoOwner{UserID: user.ID, APIKeyID: apiKey.ID},
		Provider:           service.VideoProviderOpenAI,
		AccountID:          account.ID,
		SourceTaskID:       &task.ID,
		ProviderResourceID: "char_upstream_123",
		Model:              service.OpenAIVideoModelSora2,
		Metadata:           map[string]any{"name": "Mossy"},
	})
	require.NoError(t, err)
	require.True(t, service.IsValidVideoResourceID(resource.PublicID))
	require.Equal(t, account.ID, resource.AccountID)
	_, err = resourceRepo.GetVideoResourceForOwner(ctx, user.ID+999, resource.PublicID)
	require.ErrorIs(t, err, service.ErrVideoResourceNotFound)
	byProvider, err := resourceRepo.GetVideoResourceByProviderID(ctx, service.VideoProviderOpenAI, account.ID, "char_upstream_123")
	require.NoError(t, err)
	require.Equal(t, resource.ID, byProvider.ID)

	now := time.Now().UTC()
	delivery := service.VideoCallbackDelivery{
		TaskID: task.ID, EventID: "evt_" + uuid.NewString(), EventType: "video.completed",
		EventFingerprint: "fp_" + uuid.NewString(), Payload: map[string]any{"id": publicID},
		TargetURLEnc: "encrypted-target", NextAttemptAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Hour),
	}
	stored, created, err := callbackRepo.EnqueueVideoCallback(ctx, delivery)
	require.NoError(t, err)
	require.True(t, created)
	replayed, created, err := callbackRepo.EnqueueVideoCallback(ctx, delivery)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, stored.ID, replayed.ID)

	claimed, err := callbackRepo.ClaimVideoCallbacks(ctx, "callback-worker", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, callbackRepo.MarkVideoCallbackDelivered(ctx, claimed[0].ID, "callback-worker", 204))

	require.NoError(t, resourceRepo.MarkVideoResourceDeleted(ctx, user.ID, resource.PublicID))
	_, err = resourceRepo.GetVideoResourceForOwner(ctx, user.ID, resource.PublicID)
	require.ErrorIs(t, err, service.ErrVideoResourceNotFound)
}

func TestVideoCallbackRepositoryReclaimsExpiredDeliveringLease(t *testing.T) {
	ctx := context.Background()
	taskRepo, _, callbackRepo, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	task, _, err := taskRepo.CreateHeldVideoTask(ctx, videoCreateParams(
		user, apiKey, account, service.NewVideoTaskID(), "idem-"+uuid.NewString(), "hash-callback-lease", 1,
	))
	require.NoError(t, err)

	delivery, _, err := callbackRepo.EnqueueVideoCallback(ctx, service.VideoCallbackDelivery{
		TaskID: task.ID, EventID: "evt-stale-" + uuid.NewString(), EventType: "video.completed",
		EventFingerprint: "fingerprint-" + uuid.NewString(), Payload: map[string]any{"type": "video.completed"},
		TargetURLEnc: "encrypted-target", NextAttemptAt: time.Now().UTC().Add(-time.Minute),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE video_callback_deliveries
		SET status = 'delivering', lease_owner = 'dead-worker', lease_expires_at = NOW() - INTERVAL '1 minute'
		WHERE id = $1
	`, delivery.ID)
	require.NoError(t, err)

	claimed, err := callbackRepo.ClaimVideoCallbacks(ctx, "replacement-worker", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, delivery.ID, claimed[0].ID)
	require.Equal(t, "replacement-worker", *claimed[0].LeaseOwner)
}

func TestVideoOperationalAndAdminSnapshotsExecuteAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	taskRepo, _, _, user, apiKey, account := newVideoRepositoryFixture(t, 10)
	task, created, err := taskRepo.CreateHeldVideoTask(ctx, videoCreateParams(
		user, apiKey, account, service.NewVideoTaskID(), "idem-"+uuid.NewString(), "hash-operational-snapshot", 1.25,
	))
	require.NoError(t, err)
	require.True(t, created)

	snapshot, err := taskRepo.GetVideoOperationalSnapshot(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, snapshot.HeldAmount, 1.25)
	found := false
	for _, state := range snapshot.TaskStates {
		if state.Provider == service.VideoProviderOpenAI && state.Operation == service.VideoOperationGenerate &&
			state.State == service.VideoGenerationHeld {
			found = true
			require.Positive(t, state.Count)
			require.NotNil(t, state.OldestEnteredAt)
		}
	}
	require.True(t, found)

	admin := &videoAdminRepository{db: integrationDB}
	overview, err := admin.GetVideoAdminOverview(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, overview.HeldAmount, 1.25)
	require.NotEmpty(t, overview.TaskStates)
	loaded, err := admin.GetVideoTaskAdmin(ctx, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, task.ID, loaded.ID)
}
