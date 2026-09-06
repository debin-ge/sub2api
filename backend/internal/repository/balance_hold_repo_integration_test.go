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

func TestBalanceHoldRepositoryVideoLifecycleIsIdempotentAndAllowsOverCapture(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("video-hold-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      5,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-video-hold-" + uuid.NewString(),
		Name:   "video-hold",
	})
	cleanupVideoIntegrationFixture(t, user.ID, apiKey.ID, 0)
	taskID := "video_" + uuid.NewString()

	reserve := &service.BalanceHoldCommand{
		RequestID:  service.VideoTaskHoldRequestID(taskID),
		APIKeyID:   apiKey.ID,
		UserID:     user.ID,
		Scope:      service.BalanceHoldScopeVideoTask,
		RefID:      taskID,
		HoldAmount: 4,
	}
	reserved, err := repo.ReserveBalanceHold(ctx, reserve)
	require.NoError(t, err)
	require.True(t, reserved.Applied)
	require.InDelta(t, 1, *reserved.NewBalance, 0.000001)
	require.InDelta(t, 4, *reserved.FrozenBalance, 0.000001)

	replayedReserve, err := repo.ReserveBalanceHold(ctx, reserve)
	require.NoError(t, err)
	require.False(t, replayedReserve.Applied)

	capture := &service.BalanceHoldCommand{
		RequestID:    service.VideoTaskCaptureRequestID(taskID),
		APIKeyID:     apiKey.ID,
		UserID:       user.ID,
		Scope:        service.BalanceHoldScopeVideoTask,
		RefID:        taskID,
		HoldAmount:   4,
		ActualAmount: 6,
	}
	captured, err := repo.CaptureBalanceHold(ctx, capture)
	require.NoError(t, err)
	require.True(t, captured.Applied)
	require.True(t, captured.BalanceOverdrafted)
	require.InDelta(t, -1, *captured.NewBalance, 0.000001)
	require.InDelta(t, 0, *captured.FrozenBalance, 0.000001)

	replayedCapture, err := repo.CaptureBalanceHold(ctx, capture)
	require.NoError(t, err)
	require.False(t, replayedCapture.Applied)

	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT balance, frozen_balance FROM users WHERE id = $1
	`, user.ID).Scan(&balance, &frozen))
	require.InDelta(t, -1, balance, 0.000001)
	require.InDelta(t, 0, frozen, 0.000001)
}

func TestBalanceHoldRepositoryRejectsInsufficientReserveAndPhantomRelease(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("video-phantom-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      2,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-video-phantom-" + uuid.NewString(),
		Name:   "video-phantom",
	})
	cleanupVideoIntegrationFixture(t, user.ID, apiKey.ID, 0)
	taskID := "video_" + uuid.NewString()

	_, err := repo.ReserveBalanceHold(ctx, &service.BalanceHoldCommand{
		RequestID:  service.VideoTaskHoldRequestID(taskID),
		APIKeyID:   apiKey.ID,
		UserID:     user.ID,
		Scope:      service.BalanceHoldScopeVideoTask,
		RefID:      taskID,
		HoldAmount: 3,
	})
	require.ErrorIs(t, err, service.ErrBalanceHoldInsufficientBalance)

	released, err := repo.ReleaseBalanceHold(ctx, &service.BalanceHoldCommand{
		RequestID:  service.VideoTaskReleaseRequestID(taskID),
		APIKeyID:   apiKey.ID,
		UserID:     user.ID,
		Scope:      service.BalanceHoldScopeVideoTask,
		RefID:      taskID,
		HoldAmount: 3,
	})
	require.NoError(t, err)
	require.True(t, released.Applied, "release intent is deduplicated even when no money moves")
	require.Nil(t, released.NewBalance)

	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT balance, frozen_balance FROM users WHERE id = $1
	`, user.ID).Scan(&balance, &frozen))
	require.InDelta(t, 2, balance, 0.000001)
	require.InDelta(t, 0, frozen, 0.000001)
}

func TestBalanceHoldRepositoryBatchImageWrapperRejectsOverCapture(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("batch-wrapper-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      5,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-batch-wrapper-" + uuid.NewString(),
		Name:   "batch-wrapper",
	})
	cleanupVideoIntegrationFixture(t, user.ID, apiKey.ID, 0)
	batchID := "imgbatch_" + uuid.NewString()

	_, err := repo.ReserveBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{
		RequestID:  service.BatchImageHoldRequestID(batchID),
		APIKeyID:   apiKey.ID,
		UserID:     user.ID,
		BatchID:    batchID,
		HoldAmount: 1,
	})
	require.NoError(t, err)

	_, err = repo.CaptureBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{
		RequestID:    service.BatchImageCaptureRequestID(batchID),
		APIKeyID:     apiKey.ID,
		UserID:       user.ID,
		BatchID:      batchID,
		HoldAmount:   1,
		ActualAmount: 2,
	})
	require.ErrorIs(t, err, service.ErrBatchImageSettlementCostExceedsHold)
}
