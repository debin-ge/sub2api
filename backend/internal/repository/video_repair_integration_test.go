//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoRepairHoldRejectsRemovedMembershipAtomically(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
	var groupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO groups(name, platform) VALUES ($1, 'openai') RETURNING id`, fmt.Sprintf("video-repair-%d", time.Now().UnixNano())).Scan(&groupID))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM groups WHERE id = $1`, groupID)
	})
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "membership-revoked", "membership-hash", 2)
	params.Owner.GroupID = &groupID
	_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET group_id = $1 WHERE id = $2`, groupID, key.ID)
	require.NoError(t, err)
	_, _, err = repo.CreateHeldVideoTask(ctx, params)
	require.ErrorIs(t, err, service.ErrVideoNoAccountAvailable)
	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance, frozen_balance FROM users WHERE id = $1`, user.ID).Scan(&balance, &frozen))
	require.Equal(t, 10.0, balance)
	require.Zero(t, frozen)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO account_groups(account_id, group_id) VALUES ($1, $2)`, account.ID, groupID)
	require.NoError(t, err)
	_, created, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	require.True(t, created)
}

func TestVideoRepairRecoveryReusesFrozenSettlementPayload(t *testing.T) {
	ctx := context.Background()
	tasks, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
	publicID := service.NewVideoTaskID()
	task, _, err := tasks.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, publicID, "cost-recovery", "cost-hash", 4))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET generation_state = 'completed', billing_state = 'capture_pending', actual_cost = 3 WHERE id = $1`, task.ID)
	require.NoError(t, err)
	task.GenerationState, task.BillingState = service.VideoGenerationCompleted, service.VideoBillingCapturePending
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET callback_url_enc = 'enc:https://callback.invalid/hook', request_attributes = request_attributes || '{"callback_contract_version":1,"callback_retry_hours":24,"callback_disclosure_policy":"none"}'::jsonb WHERE id = $1`, task.ID)
	require.NoError(t, err)
	requestID := service.VideoTaskCaptureRequestID(publicID)
	command := &service.UsageBillingCommand{
		RequestID: requestID, APIKeyID: key.ID, UserID: user.ID, AccountID: account.ID,
		AccountType: account.Type, Model: "sora-2", BillingType: service.BillingTypeBalance,
		ActualCost: 3, TotalCost: 3, AccountQuotaCost: 1, OccurredAt: time.Now().UTC(),
	}
	settlement := &service.BalanceSettlementCommand{
		TaskID: task.ID, Action: service.BalanceSettlementCapture, Billing: command,
		Hold: service.BalanceHoldCommand{
			RequestID: requestID, APIKeyID: key.ID, UserID: user.ID,
			Scope: service.BalanceHoldScopeVideoTask, RefID: publicID, HoldAmount: 4, ActualAmount: 3,
		},
	}
	usage := &service.UsageLog{
		UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: requestID,
		Model: "sora-2", BillingType: service.BillingTypeBalance, TotalCost: 3, ActualCost: 3,
		VideoCount: 1, CreatedAt: command.OccurredAt,
	}
	commandJSON, usageJSON, err := marshalBalanceSettlementOutboxPayload(settlement, usage)
	require.NoError(t, err)
	event, err := tasks.billing.enqueueAndClaimUsageBillingOutboxPayload(ctx, "old-video-worker", requestID, key.ID, settlement.Hold.RequestFingerprint, usageBillingOutboxPayloadVersionV2, commandJSON, usageJSON)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE usage_billing_outbox SET claimed_at = NOW() - INTERVAL '3 minutes' WHERE id = $1`, event.ID)
	require.NoError(t, err)
	task.ProviderCostSnapshot = map[string]any{"account_rate_multiplier": 99}
	result, original, found, err := tasks.billing.ResumeVideoBalanceSettlement(ctx, task)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 1.0, original.AccountQuotaCost)
	require.Equal(t, 3.0, original.ActualCost)
	require.Equal(t, 7.0, *result.NewBalance)
	require.Zero(t, *result.FrozenBalance)
	require.NoError(t, tasks.billing.AcknowledgeVideoBalanceSettlement(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID))
	var usageCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE request_id = $1 AND api_key_id = $2`, requestID, key.ID).Scan(&usageCount))
	require.Equal(t, 1, usageCount)
	committed, err := tasks.GetVideoTaskByPublicID(ctx, publicID)
	require.NoError(t, err)
	require.Equal(t, "pending", committed.CallbackIntentState)
	callbackRepo := &videoCallbackRepository{db: integrationDB}
	intents, err := callbackRepo.ListVideoCallbackIntents(ctx, 100)
	require.NoError(t, err)
	foundIntent := false
	for _, intent := range intents {
		foundIntent = foundIntent || intent.ID == task.ID
	}
	require.True(t, foundIntent)
	cfg := &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{Callback: config.GatewayVideoCallbackConfig{Enabled: true, RetryHours: 24}}}}
	delivery, needed, err := service.BuildVideoCallbackDelivery(committed, cfg, time.Now().UTC(), config.VideoDisclosureNone)
	require.NoError(t, err)
	require.True(t, needed)
	invalid := *delivery
	invalid.EventID = strings.Repeat("x", 129)
	require.Error(t, callbackRepo.MaterializeVideoCallback(ctx, &invalid))
	committed, err = tasks.GetVideoTaskByPublicID(ctx, publicID)
	require.NoError(t, err)
	require.Equal(t, "pending", committed.CallbackIntentState)
	require.NoError(t, callbackRepo.MaterializeVideoCallback(ctx, delivery))
	require.NoError(t, callbackRepo.MaterializeVideoCallback(ctx, delivery))
	committed, err = tasks.GetVideoTaskByPublicID(ctx, publicID)
	require.NoError(t, err)
	require.Equal(t, "materialized", committed.CallbackIntentState)
	var callbackCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_callback_deliveries WHERE task_id = $1`, task.ID).Scan(&callbackCount))
	require.Equal(t, 1, callbackCount)
}
