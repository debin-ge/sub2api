//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVideoBalanceSettlementV2CaptureIsAtomicAndReplaySafe(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("video-settle-capture-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      5,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-video-settle-capture-" + uuid.NewString(),
		Name:   "video-settle-capture",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name:     "video-settle-capture-" + uuid.NewString(),
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
	})
	cleanupVideoIntegrationFixture(t, user.ID, apiKey.ID, account.ID)
	publicID := service.NewVideoTaskID()
	holdAmount := 4.0

	_, err := repo.ReserveBalanceHold(ctx, &service.BalanceHoldCommand{
		RequestID:  service.VideoTaskHoldRequestID(publicID),
		APIKeyID:   apiKey.ID,
		UserID:     user.ID,
		Scope:      service.BalanceHoldScopeVideoTask,
		RefID:      publicID,
		HoldAmount: holdAmount,
	})
	require.NoError(t, err)
	taskID := insertVideoSettlementTask(t, publicID, user.ID, apiKey.ID, account.ID, service.VideoBillingCapturePending, holdAmount)

	requestID := service.VideoTaskCaptureRequestID(publicID)
	settlement := &service.BalanceSettlementCommand{
		TaskID: taskID,
		Action: service.BalanceSettlementCapture,
		Hold: service.BalanceHoldCommand{
			RequestID:    requestID,
			APIKeyID:     apiKey.ID,
			UserID:       user.ID,
			Scope:        service.BalanceHoldScopeVideoTask,
			RefID:        publicID,
			HoldAmount:   holdAmount,
			ActualAmount: 6,
		},
		Billing: &service.UsageBillingCommand{
			RequestID:           requestID,
			APIKeyID:            apiKey.ID,
			UserID:              user.ID,
			AccountID:           account.ID,
			AccountType:         account.Type,
			Model:               "sora-2",
			BillingType:         service.BillingTypeBalance,
			ActualCost:          6,
			TotalCost:           6,
			BalanceCost:         0,
			APIKeyQuotaCost:     6,
			APIKeyRateLimitCost: 6,
			OccurredAt:          time.Now().UTC(),
		},
	}
	duration := 8
	resolution := "1280x720"
	usageLog := &service.UsageLog{
		UserID:               user.ID,
		APIKeyID:             apiKey.ID,
		AccountID:            account.ID,
		RequestID:            requestID,
		Model:                "sora-2",
		BillingType:          service.BillingTypeBalance,
		TotalCost:            6,
		ActualCost:           6,
		VideoCount:           1,
		VideoDurationSeconds: &duration,
		VideoResolution:      &resolution,
		CreatedAt:            time.Now().UTC(),
	}

	result, err := repo.SettleVideoBalance(ctx, settlement, usageLog)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.UsageLogRecorded)
	require.True(t, result.BalanceOverdrafted)
	require.InDelta(t, -1, *result.NewBalance, 0.000001)
	require.InDelta(t, 0, *result.FrozenBalance, 0.000001)
	require.NoError(t, repo.AcknowledgeUsageBillingOutbox(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID))

	assertVideoSettlementState(t, user.ID, taskID, requestID, apiKey.ID, -1, 0, service.VideoBillingCaptured, 1, 1)
	var storedFingerprint string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT request_fingerprint FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKey.ID).Scan(&storedFingerprint))
	_, _, err = marshalBalanceSettlementOutboxPayload(settlement, usageLog)
	require.NoError(t, err)
	require.Equal(t, storedFingerprint, settlement.Hold.RequestFingerprint)
	compareTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	existingLog, exists, err := loadUsageBillingExistingLogPayload(ctx, compareTx, requestID, apiKey.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, usageLogPayloadBillingComparable(usageLogToPayloadV1(usageLog)), usageLogPayloadBillingComparable(existingLog))
	require.NoError(t, compareTx.Rollback())

	replayed, err := repo.SettleVideoBalance(ctx, settlement, usageLog)
	require.NoError(t, err)
	require.False(t, replayed.Applied)
	require.True(t, replayed.UsageLogRecorded)
	require.NoError(t, repo.AcknowledgeUsageBillingOutbox(ctx, replayed.OutboxReceipt.WorkerID, replayed.OutboxReceipt.ID))
	assertVideoSettlementState(t, user.ID, taskID, requestID, apiKey.ID, -1, 0, service.VideoBillingCaptured, 1, 1)
}

func TestVideoBalanceSettlementV2ReleaseIsAtomicAndWritesNoUsageLog(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("video-settle-release-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      5,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-video-settle-release-" + uuid.NewString(),
		Name:   "video-settle-release",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name:     "video-settle-release-" + uuid.NewString(),
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
	})
	cleanupVideoIntegrationFixture(t, user.ID, apiKey.ID, account.ID)
	publicID := service.NewVideoTaskID()
	holdAmount := 3.0
	_, err := repo.ReserveBalanceHold(ctx, &service.BalanceHoldCommand{
		RequestID:  service.VideoTaskHoldRequestID(publicID),
		APIKeyID:   apiKey.ID,
		UserID:     user.ID,
		Scope:      service.BalanceHoldScopeVideoTask,
		RefID:      publicID,
		HoldAmount: holdAmount,
	})
	require.NoError(t, err)
	taskID := insertVideoSettlementTask(t, publicID, user.ID, apiKey.ID, account.ID, service.VideoBillingReleasePending, holdAmount)
	requestID := service.VideoTaskReleaseRequestID(publicID)
	settlement := &service.BalanceSettlementCommand{
		TaskID: taskID,
		Action: service.BalanceSettlementRelease,
		Hold: service.BalanceHoldCommand{
			RequestID:  requestID,
			APIKeyID:   apiKey.ID,
			UserID:     user.ID,
			Scope:      service.BalanceHoldScopeVideoTask,
			RefID:      publicID,
			HoldAmount: holdAmount,
		},
	}

	result, err := repo.SettleVideoBalance(ctx, settlement, nil)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.False(t, result.UsageLogRecorded)
	require.InDelta(t, 5, *result.NewBalance, 0.000001)
	require.InDelta(t, 0, *result.FrozenBalance, 0.000001)
	require.NoError(t, repo.AcknowledgeUsageBillingOutbox(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID))

	assertVideoSettlementState(t, user.ID, taskID, requestID, apiKey.ID, 5, 0, service.VideoBillingReleased, 0, 1)
}

func insertVideoSettlementTask(
	t *testing.T,
	publicID string,
	userID, apiKeyID, accountID int64,
	billingState string,
	holdAmount float64,
) int64 {
	t.Helper()
	var taskID int64
	err := integrationDB.QueryRowContext(context.Background(), `
		INSERT INTO video_tasks (
			public_id, user_id, api_key_id, account_id, provider, operation,
			request_hash, generation_state, billing_state, hold_id, hold_amount
		)
		VALUES ($1, $2, $3, $4, 'openai', 'generate', $5, 'completed', $6, $7, $8)
		RETURNING id
	`, publicID, userID, apiKeyID, accountID, uuid.NewString(), billingState, service.VideoTaskHoldRequestID(publicID), holdAmount).Scan(&taskID)
	require.NoError(t, err)
	return taskID
}

func assertVideoSettlementState(
	t *testing.T,
	userID, taskID int64,
	requestID string,
	apiKeyID int64,
	wantBalance, wantFrozen float64,
	wantBillingState string,
	wantUsageLogs, wantEvents int,
) {
	t.Helper()
	ctx := context.Background()
	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT balance, frozen_balance FROM users WHERE id = $1
	`, userID).Scan(&balance, &frozen))
	require.InDelta(t, wantBalance, balance, 0.000001)
	require.InDelta(t, wantFrozen, frozen, 0.000001)

	var billingState string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT billing_state FROM video_tasks WHERE id = $1
	`, taskID).Scan(&billingState))
	require.Equal(t, wantBillingState, billingState)

	var usageCount, eventCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM usage_logs WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKeyID).Scan(&usageCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM video_task_events WHERE task_id = $1
	`, taskID).Scan(&eventCount))
	require.Equal(t, wantUsageLogs, usageCount)
	require.Equal(t, wantEvents, eventCount)
}
