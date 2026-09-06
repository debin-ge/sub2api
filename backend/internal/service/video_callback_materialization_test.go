package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type videoMaterializationRepoStub struct {
	videoCallbackRepositoryStub
	task  *VideoTask
	count int
}

func (repo *videoMaterializationRepoStub) ListVideoCallbackIntents(context.Context, int) ([]*VideoTask, error) {
	if repo.task.CallbackIntentState == "materialized" {
		return nil, nil
	}
	return []*VideoTask{repo.task}, nil
}

func (repo *videoMaterializationRepoStub) MaterializeVideoCallback(_ context.Context, delivery *VideoCallbackDelivery) error {
	repo.count++
	repo.enqueued = delivery
	repo.task.CallbackIntentState = "materialized"
	return nil
}

func TestVideoCallbackRecoversCommittedTerminalTaskWithoutVideoTaskWorker(t *testing.T) {
	settled := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)
	task := baseVideoWorkerTask()
	task.GenerationState, task.BillingState = VideoGenerationCompleted, VideoBillingCaptured
	task.SettledAt = &settled
	task.CallbackIntentState = "pending"
	target := "encrypted-callback-target"
	task.CallbackURLEnc = &target
	task.RequestAttributes["callback_contract_version"] = 1
	task.RequestAttributes["callback_retry_hours"] = 24
	task.RequestAttributes["callback_disclosure_policy"] = config.VideoDisclosureNone
	repo := &videoMaterializationRepoStub{task: task}
	cfg := videoCallbackTestConfig()
	cfg.Gateway.Video.Callback.RetryHours = 72
	worker := NewVideoCallbackWorker(repo, videoEncryptorStub{}, nil, cfg)
	worker.now = func() time.Time { return settled.Add(time.Hour) }
	require.NoError(t, worker.materializeCallbacks(context.Background(), 10))
	require.Equal(t, "pending", repo.enqueued.Status)
	require.Equal(t, settled, repo.enqueued.CreatedAt)
	require.Equal(t, settled.Add(24*time.Hour), repo.enqueued.ExpiresAt)
	require.NoError(t, worker.materializeCallbacks(context.Background(), 10))
	require.Equal(t, 1, repo.count)
}

func TestVideoCallbackMaterializationDoesNotReviveUnknownOrExpiredContracts(t *testing.T) {
	now := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"missing_contract", "expired", "delivery_paused"} {
		t.Run(scenario, func(t *testing.T) {
			task := baseVideoWorkerTask()
			task.GenerationState, task.BillingState = VideoGenerationCompleted, VideoBillingCaptured
			target := "encrypted-callback-target"
			task.CallbackURLEnc = &target
			settled := now.Add(-time.Hour)
			task.SettledAt = &settled
			task.RequestAttributes["callback_contract_version"] = 1
			task.RequestAttributes["callback_retry_hours"] = 24
			cfg := videoCallbackTestConfig()
			switch scenario {
			case "missing_contract":
				delete(task.RequestAttributes, "callback_contract_version")
			case "expired":
				settled = now.Add(-25 * time.Hour)
			case "delivery_paused":
				cfg.Gateway.Video.Callback.Enabled = false
			}
			delivery, err := buildDurableVideoCallback(task, cfg, now, config.VideoDisclosureNone)
			require.NoError(t, err)
			if scenario == "delivery_paused" {
				require.Equal(t, "pending", delivery.Status)
			} else {
				require.Equal(t, "quarantined", delivery.Status)
				require.False(t, delivery.ExpiresAt.After(now))
			}
		})
	}
}
