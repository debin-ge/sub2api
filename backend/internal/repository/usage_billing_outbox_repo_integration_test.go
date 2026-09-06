//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUsageBillingOutbox_RestartRecoveryChargesExactlyOnce(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-recovery-%s@example.com", uuid.NewString()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-recovery-" + uuid.NewString(),
		Name:   "billing-recovery",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-recovery-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	requestID := "req-" + uuid.NewString() + strings.Repeat("x", 160)
	require.Greater(t, len(requestID), 64)
	require.LessOrEqual(t, len(requestID), 255)
	cmd := &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		AccountID:   account.ID,
		AccountType: account.Type,
		Model:       "priced-model",
		InputTokens: 10,
		BalanceCost: 1.25,
		ActualCost:  1.25,
		TotalCost:   1.25,
		OccurredAt:  time.Now().UTC(),
	}
	usageLog := &service.UsageLog{
		UserID:      user.ID,
		APIKeyID:    apiKey.ID,
		AccountID:   account.ID,
		RequestID:   requestID,
		Model:       cmd.Model,
		InputTokens: cmd.InputTokens,
		InputCost:   1.25,
		TotalCost:   1.25,
		ActualCost:  1.25,
		CreatedAt:   time.Now().UTC(),
	}
	commandJSON, usageLogJSON, err := marshalUsageBillingOutboxPayload(cmd, usageLog)
	require.NoError(t, err)

	// Process one persists the intent and exits before applying it.
	firstProcessRepo := NewUsageBillingRepository(client, integrationDB)
	event, err := firstProcessRepo.enqueueAndClaimUsageBillingOutbox(
		ctx, "dead-process", cmd, commandJSON, usageLogJSON,
	)
	require.NoError(t, err)
	require.Positive(t, event.ID)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE usage_billing_outbox
		SET claimed_at = NOW() - INTERVAL '5 minutes', available_at = NOW()
		WHERE id = $1
	`, event.ID)
	require.NoError(t, err)

	// A fresh repository instance represents a restarted process. Its worker
	// claims and commits the retained intent.
	restartedRepo := NewUsageBillingRepository(client, integrationDB)
	events, err := restartedRepo.ClaimUsageBillingOutbox(ctx, "restart-worker", 10, 2*time.Minute)
	require.NoError(t, err)
	require.Len(t, events, 1)
	result, err := restartedRepo.CompleteUsageBillingOutbox(ctx, "restart-worker", events[0])
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.UsageLogRecorded)
	require.NotNil(t, result.OutboxReceipt)
	require.NoError(t, restartedRepo.AcknowledgeUsageBillingOutbox(
		ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID,
	))

	assertBillingState := func(wantBalance float64) {
		t.Helper()
		var balance float64
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT balance FROM users WHERE id = $1", user.ID,
		).Scan(&balance))
		require.InDelta(t, wantBalance, balance, 0.000001)

		var usageCount, dedupCount, outboxCount int
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM usage_logs WHERE request_id = $1 AND api_key_id = $2",
			requestID, apiKey.ID,
		).Scan(&usageCount))
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2",
			requestID, apiKey.ID,
		).Scan(&dedupCount))
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM usage_billing_outbox WHERE request_id = $1 AND api_key_id = $2",
			requestID, apiKey.ID,
		).Scan(&outboxCount))
		require.Equal(t, 1, usageCount)
		require.Equal(t, 1, dedupCount)
		require.Zero(t, outboxCount)
	}
	assertBillingState(98.75)

	// Retrying after an ambiguous client outcome creates a fresh intent, but
	// both database idempotency keys already exist. It only acknowledges the
	// intent and cannot debit the user twice.
	duplicateResult, err := restartedRepo.ApplyAndRecord(ctx, cmd, usageLog)
	require.NoError(t, err)
	require.False(t, duplicateResult.Applied)
	require.True(t, duplicateResult.UsageLogRecorded)
	require.NotNil(t, duplicateResult.OutboxReceipt)
	require.NoError(t, restartedRepo.AcknowledgeUsageBillingOutbox(
		ctx, duplicateResult.OutboxReceipt.WorkerID, duplicateResult.OutboxReceipt.ID,
	))
	assertBillingState(98.75)
}

func TestUsageBillingOutbox_RepairsUnchargedFallbackAndFloatTail(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-fallback-%s@example.com", uuid.NewString()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-fallback-" + uuid.NewString(),
		Name:   "billing-fallback",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-fallback-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	const rawCost = 0.17918979999999998
	requestID := "fallback-" + uuid.NewString()
	occurredAt := time.Now().UTC()
	cmd := &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		AccountID:   account.ID,
		AccountType: account.Type,
		Model:       "priced-model",
		InputTokens: 10,
		BalanceCost: rawCost,
		ActualCost:  rawCost,
		TotalCost:   rawCost,
		OccurredAt:  occurredAt,
	}
	usageLog := &service.UsageLog{
		UserID:      user.ID,
		APIKeyID:    apiKey.ID,
		AccountID:   account.ID,
		RequestID:   requestID,
		Model:       cmd.Model,
		InputTokens: cmd.InputTokens,
		InputCost:   rawCost,
		TotalCost:   rawCost,
		ActualCost:  rawCost,
		CreatedAt:   occurredAt,
	}

	commandJSON, usageLogJSON, err := marshalUsageBillingOutboxPayload(cmd, usageLog)
	require.NoError(t, err)
	canonicalCost := service.QuantizeUsageBillingAmount(rawCost)
	require.Equal(t, canonicalCost, cmd.ActualCost)
	require.Equal(t, canonicalCost, usageLog.ActualCost)

	repo := NewUsageBillingRepository(client, integrationDB)
	event, err := repo.enqueueAndClaimUsageBillingOutbox(
		ctx,
		"fallback-repair-worker",
		cmd,
		commandJSON,
		usageLogJSON,
	)
	require.NoError(t, err)

	// Reproduce the old error path: the durable billing transaction failed,
	// then the caller inserted the same immutable usage facts with cost zero.
	fallback := *usageLog
	fallback.ActualCost = 0
	inserted, err := (&usageLogRepository{}).createSingle(ctx, integrationDB, &fallback)
	require.NoError(t, err)
	require.True(t, inserted)

	result, err := repo.CompleteUsageBillingOutbox(ctx, "fallback-repair-worker", event)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.UsageLogRecorded)
	require.False(t, result.ProjectionRepairRequired)
	require.NoError(t, repo.AcknowledgeUsageBillingOutbox(
		ctx,
		result.OutboxReceipt.WorkerID,
		result.OutboxReceipt.ID,
	))

	var balance, actualCost float64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT balance FROM users WHERE id = $1", user.ID,
	).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT actual_cost
		FROM usage_logs
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKey.ID).Scan(&actualCost))
	require.InDelta(t, 100-canonicalCost, balance, 1e-9)
	require.Equal(t, canonicalCost, actualCost)
}

func TestUsageBillingOutbox_ReplayUsesPersistedNumericPrecision(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-numeric-replay-%s@example.com", uuid.NewString()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-numeric-replay-" + uuid.NewString(),
		Name:   "billing-numeric-replay",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-numeric-replay-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	requestID := "numeric-replay-" + uuid.NewString()
	occurredAt := time.Now().UTC()
	rateMultiplier := 1.2345 * 1.2345
	accountRateMultiplier := 1.00004
	cmd := &service.UsageBillingCommand{
		RequestID:          requestID,
		APIKeyID:           apiKey.ID,
		UserID:             user.ID,
		AccountID:          account.ID,
		AccountType:        account.Type,
		Model:              "priced-model",
		InputTokens:        10,
		BalanceCost:        0.5,
		ActualCost:         0.5,
		TotalCost:          0.5,
		OccurredAt:         occurredAt,
		RequestPayloadHash: service.HashUsageRequestPayload([]byte(`{"model":"priced-model","input":"same"}`)),
	}
	usageLog := &service.UsageLog{
		UserID:                user.ID,
		APIKeyID:              apiKey.ID,
		AccountID:             account.ID,
		RequestID:             requestID,
		Model:                 cmd.Model,
		InputTokens:           cmd.InputTokens,
		InputCost:             0.12345678904,
		OutputCost:            0.37654321096,
		TotalCost:             cmd.TotalCost,
		ActualCost:            cmd.ActualCost,
		RateMultiplier:        rateMultiplier,
		AccountRateMultiplier: &accountRateMultiplier,
		CreatedAt:             occurredAt,
	}

	repo := NewUsageBillingRepository(client, integrationDB)
	first, err := repo.ApplyAndRecord(ctx, cmd, usageLog)
	require.NoError(t, err)
	require.True(t, first.Applied)
	require.NotNil(t, first.OutboxReceipt)
	require.NoError(t, repo.AcknowledgeUsageBillingOutbox(
		ctx,
		first.OutboxReceipt.WorkerID,
		first.OutboxReceipt.ID,
	))

	var persistedRate, persistedAccountRate float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT rate_multiplier, account_rate_multiplier
		FROM usage_logs
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKey.ID).Scan(&persistedRate, &persistedAccountRate))
	require.InDelta(t, 1.5240, persistedRate, 0.0000001)
	require.InDelta(t, 1.0000, persistedAccountRate, 0.0000001)

	// Replaying the exact pre-insert payload must compare against PostgreSQL's
	// NUMERIC-rounded values rather than the original float bit patterns.
	replayed, err := repo.ApplyAndRecord(ctx, cmd, usageLog)
	require.NoError(t, err)
	require.False(t, replayed.Applied)
	require.True(t, replayed.UsageLogRecorded)
	require.NotNil(t, replayed.OutboxReceipt)
	require.NoError(t, repo.AcknowledgeUsageBillingOutbox(
		ctx,
		replayed.OutboxReceipt.WorkerID,
		replayed.OutboxReceipt.ID,
	))

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT balance FROM users WHERE id = $1",
		user.ID,
	).Scan(&balance))
	require.InDelta(t, 99.5, balance, 0.000001)
}

func TestUsageBillingOutbox_SoftDeletedAPIKeyDoesNotCancelPrimaryCharge(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-deleted-key-%s@example.com", uuid.NewString()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-deleted-key-" + uuid.NewString(),
		Name:   "billing-deleted-key",
		Status: service.StatusAPIKeyActive,
		Quota:  100,
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-deleted-key-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	requestID := "deleted-key-" + uuid.NewString()
	occurredAt := time.Now().UTC()
	cmd := &service.UsageBillingCommand{
		RequestID:           requestID,
		APIKeyID:            apiKey.ID,
		UserID:              user.ID,
		AccountID:           account.ID,
		AccountType:         account.Type,
		Model:               "priced-model",
		InputTokens:         10,
		BalanceCost:         1.25,
		APIKeyQuotaCost:     1.25,
		APIKeyRateLimitCost: 1.25,
		ActualCost:          1.25,
		TotalCost:           1.25,
		OccurredAt:          occurredAt,
	}
	usageLog := &service.UsageLog{
		UserID:      user.ID,
		APIKeyID:    apiKey.ID,
		AccountID:   account.ID,
		RequestID:   requestID,
		Model:       cmd.Model,
		InputTokens: cmd.InputTokens,
		InputCost:   1.25,
		TotalCost:   1.25,
		ActualCost:  1.25,
		CreatedAt:   occurredAt,
	}
	commandJSON, usageLogJSON, err := marshalUsageBillingOutboxPayload(cmd, usageLog)
	require.NoError(t, err)

	repo := NewUsageBillingRepository(client, integrationDB)
	event, err := repo.enqueueAndClaimUsageBillingOutbox(
		ctx,
		"soft-delete-worker",
		cmd,
		commandJSON,
		usageLogJSON,
	)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE api_keys
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, apiKey.ID)
	require.NoError(t, err)

	result, err := repo.CompleteUsageBillingOutbox(ctx, "soft-delete-worker", event)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.UsageLogRecorded)
	require.NotNil(t, result.OutboxReceipt)

	var balance, quotaUsed, usage5h, usage1d, usage7d float64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT balance FROM users WHERE id = $1",
		user.ID,
	).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT quota_used, usage_5h, usage_1d, usage_7d
		FROM api_keys
		WHERE id = $1 AND deleted_at IS NOT NULL
	`, apiKey.ID).Scan(&quotaUsed, &usage5h, &usage1d, &usage7d))
	require.InDelta(t, 98.75, balance, 1e-9)
	require.InDelta(t, 1.25, quotaUsed, 1e-9)
	require.InDelta(t, 1.25, usage5h, 1e-9)
	require.InDelta(t, 1.25, usage1d, 1e-9)
	require.InDelta(t, 1.25, usage7d, 1e-9)

	var usageCount, dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM usage_logs WHERE request_id = $1 AND api_key_id = $2",
		requestID, apiKey.ID,
	).Scan(&usageCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2",
		requestID, apiKey.ID,
	).Scan(&dedupCount))
	require.Equal(t, 1, usageCount)
	require.Equal(t, 1, dedupCount)

	require.NoError(t, repo.AcknowledgeUsageBillingOutbox(
		ctx,
		result.OutboxReceipt.WorkerID,
		result.OutboxReceipt.ID,
	))
}

func TestUsageBillingOutbox_UnknownImageSizePersistsAsPricingUnavailable(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-unpriced-image-%s@example.com", uuid.NewString()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-unpriced-image-" + uuid.NewString(),
		Name:   "billing-unpriced-image",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-unpriced-image-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	requestID := "unpriced-image-" + uuid.NewString()
	occurredAt := time.Now().UTC()
	imageSize := "8K"
	imageSizeSource := service.ImageSizeSourceOutput
	billingMode := string(service.BillingModeImage)
	cmd := &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		AccountID:   account.ID,
		AccountType: account.Type,
		Model:       "future-image-model",
		ImageCount:  1,
		OccurredAt:  occurredAt,
	}
	usageLog := &service.UsageLog{
		UserID:          user.ID,
		APIKeyID:        apiKey.ID,
		AccountID:       account.ID,
		RequestID:       requestID,
		Model:           cmd.Model,
		BillingState:    service.BillingStatePricingUnavailable,
		BillingMode:     &billingMode,
		ImageCount:      1,
		ImageSize:       &imageSize,
		ImageOutputSize: &imageSize,
		ImageSizeSource: &imageSizeSource,
		CreatedAt:       occurredAt,
	}

	repo := NewUsageBillingRepository(client, integrationDB)
	result, err := repo.ApplyAndRecord(ctx, cmd, usageLog)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.UsageLogRecorded)
	require.NotNil(t, result.OutboxReceipt)
	require.NoError(t, repo.AcknowledgeUsageBillingOutbox(
		ctx,
		result.OutboxReceipt.WorkerID,
		result.OutboxReceipt.ID,
	))

	var (
		storedState int16
		storedSize  string
		balance     float64
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT billing_state, image_size
		FROM usage_logs
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKey.ID).Scan(&storedState, &storedSize))
	require.Equal(t, int16(service.BillingStatePricingUnavailable), storedState)
	require.Equal(t, imageSize, storedSize)
	require.NoError(t, integrationDB.QueryRowContext(
		ctx,
		"SELECT balance FROM users WHERE id = $1",
		user.ID,
	).Scan(&balance))
	require.InDelta(t, 100, balance, 1e-9)
}

func TestUsageBillingOutbox_LegacyDedupWithoutLogRepairsAfterRestartWithoutRecharging(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-legacy-repair-%s@example.com", uuid.NewString()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-legacy-repair-" + uuid.NewString(),
		Name:   "billing-legacy-repair",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-legacy-repair-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	requestID := "legacy-repair-" + uuid.NewString()
	occurredAt := time.Now().UTC()
	cmd := &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		AccountID:   account.ID,
		AccountType: account.Type,
		Model:       "priced-model",
		InputTokens: 10,
		BalanceCost: 1.25,
		ActualCost:  1.25,
		TotalCost:   1.25,
		OccurredAt:  occurredAt,
	}
	usageLog := &service.UsageLog{
		UserID:      user.ID,
		APIKeyID:    apiKey.ID,
		AccountID:   account.ID,
		RequestID:   requestID,
		Model:       cmd.Model,
		InputTokens: cmd.InputTokens,
		InputCost:   1.25,
		TotalCost:   1.25,
		ActualCost:  1.25,
		CreatedAt:   occurredAt,
	}

	repo := NewUsageBillingRepository(client, integrationDB)
	// This is the pre-outbox crash window: billing/dedup commits, while the
	// process dies before usage logging and cache post-effects.
	legacyResult, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, legacyResult.Applied)

	recovered, err := repo.ApplyAndRecord(ctx, cmd, usageLog)
	require.NoError(t, err)
	require.False(t, recovered.Applied)
	require.True(t, recovered.UsageLogRecorded)
	require.True(t, recovered.ProjectionRepairRequired)
	require.NotNil(t, recovered.OutboxReceipt)

	// Simulate the inline process dying before Finalize/Acknowledge. A fresh
	// worker must recover the stage-1 projection marker from result_payload.
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE usage_billing_outbox
		SET claimed_at = NOW() - INTERVAL '5 minutes', available_at = NOW()
		WHERE id = $1
	`, recovered.OutboxReceipt.ID)
	require.NoError(t, err)

	restartedRepo := NewUsageBillingRepository(client, integrationDB)
	events, err := restartedRepo.ClaimUsageBillingOutbox(
		ctx,
		"legacy-repair-restarted-worker",
		10,
		2*time.Minute,
	)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int8(1), events[0].Stage)
	require.NotNil(t, events[0].Result)
	require.True(t, events[0].Result.ProjectionRepairRequired)

	replayed, err := restartedRepo.CompleteUsageBillingOutbox(
		ctx,
		"legacy-repair-restarted-worker",
		events[0],
	)
	require.NoError(t, err)
	require.False(t, replayed.Applied)
	require.True(t, replayed.ProjectionRepairRequired)

	billingCache := service.NewBillingCacheService(NewBillingCache(testRedis(t)), nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(billingCache.Stop)
	postEffects := service.NewUsageBillingPostEffectsService(billingCache, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, postEffects.Finalize(ctx, events[0].Command, replayed))
	require.NoError(t, restartedRepo.AcknowledgeUsageBillingOutbox(
		ctx,
		replayed.OutboxReceipt.WorkerID,
		replayed.OutboxReceipt.ID,
	))

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT balance FROM users WHERE id = $1", user.ID,
	).Scan(&balance))
	require.InDelta(t, 98.75, balance, 1e-9, "legacy billing must not be charged twice")

	var usageCount, outboxCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM usage_logs WHERE request_id = $1 AND api_key_id = $2",
		requestID, apiKey.ID,
	).Scan(&usageCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM usage_billing_outbox WHERE request_id = $1 AND api_key_id = $2",
		requestID, apiKey.ID,
	).Scan(&outboxCount))
	require.Equal(t, 1, usageCount)
	require.Zero(t, outboxCount)
}
