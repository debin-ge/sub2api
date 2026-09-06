//go:build integration

package repository

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Only the integration-test binary enters this branch; the parent supplies the
// isolated container DSN and kills the child after its durable commit.
func runVideoReleaseCrashChild() int {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", os.Getenv("SUB2API_VIDEO_RELEASE_DSN"))
	if err != nil {
		return 2
	}
	defer db.Close()
	repo := &videoTaskRepository{db: db, billing: NewUsageBillingRepository(nil, db)}
	userID, _ := strconv.ParseInt(os.Getenv("SUB2API_VIDEO_RELEASE_USER"), 10, 64)
	task, err := repo.GetVideoTaskForOwner(ctx, userID, os.Getenv("SUB2API_VIDEO_RELEASE_TASK"))
	if err != nil {
		return 3
	}
	if os.Getenv("SUB2API_VIDEO_RELEASE_MODE") == "submit" {
		task, err = repo.ClaimVideoTask(ctx, task.PublicID, "release-crash-child", time.Second)
		if err != nil || task == nil {
			return 4
		}
		past := time.Now().Add(-time.Second)
		writeCtx := service.WithVideoTaskWriteGuard(service.WithVideoTaskLease(ctx, service.VideoTaskLeaseFromTask(task)), task.ID, task.Version)
		_, err = repo.TransitionVideoTask(writeCtx, task.PublicID, service.VideoTaskTransition{
			GenerationState: service.VideoGenerationSubmitting, NextActionAt: &past, EventType: "provider_submitting",
		})
		if err != nil {
			return 5
		}
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, os.Getenv("SUB2API_VIDEO_RELEASE_PROVIDER"), nil)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return 6
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return 7
		}
	} else {
		requestID, now := service.VideoTaskCaptureRequestID(task.PublicID), time.Now().UTC()
		_, err = repo.billing.SettleVideoBalance(ctx, &service.BalanceSettlementCommand{
			TaskID: task.ID, Action: service.BalanceSettlementCapture,
			Hold: service.BalanceHoldCommand{RequestID: requestID, APIKeyID: *task.APIKeyID, UserID: task.UserID,
				Scope: service.BalanceHoldScopeVideoTask, RefID: task.PublicID, HoldAmount: 1, ActualAmount: 1},
			Billing: &service.UsageBillingCommand{RequestID: requestID, APIKeyID: *task.APIKeyID, UserID: task.UserID,
				AccountID: *task.AccountID, AccountType: service.AccountTypeAPIKey, Model: task.UpstreamModel,
				ActualCost: 1, TotalCost: 1, APIKeyQuotaCost: 1, APIKeyRateLimitCost: 1,
				Platform: task.Provider, PlatformQuotaCost: 1, OccurredAt: now},
		}, &service.UsageLog{UserID: task.UserID, APIKeyID: *task.APIKeyID, AccountID: *task.AccountID,
			RequestID: requestID, Model: task.UpstreamModel, ActualCost: 1, TotalCost: 1, VideoCount: 1, CreatedAt: now})
		if err != nil {
			return 8
		}
	}
	fmt.Println("video-release-commit-ready")
	<-ctx.Done()
	return 9
}

func killVideoReleaseChild(t *testing.T, mode string, task *service.VideoTask, providerURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "SUB2API_VIDEO_RELEASE_CHILD=1", "SUB2API_VIDEO_RELEASE_DSN="+integrationDSN,
		"SUB2API_VIDEO_RELEASE_USER="+strconv.FormatInt(task.UserID, 10), "SUB2API_VIDEO_RELEASE_TASK="+task.PublicID,
		"SUB2API_VIDEO_RELEASE_MODE="+mode, "SUB2API_VIDEO_RELEASE_PROVIDER="+providerURL)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "video-release-commit-ready" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		require.True(t, ok, "child did not reach the durable fault point")
	case <-ctx.Done():
		t.Fatal("child fault-point timeout")
	}
	require.NoError(t, cmd.Process.Kill())
	require.Error(t, cmd.Wait(), "child must be killed rather than shut down gracefully")
}

func TestVideoReleaseProcessCrashAfterUpstreamSubmitKeepsHold(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), uuid.NewString(), "process-submit", 1))
	require.NoError(t, err)
	var creates atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { creates.Add(1); w.WriteHeader(http.StatusOK) }))
	defer provider.Close()
	killVideoReleaseChild(t, "submit", task, provider.URL)
	time.Sleep(1100 * time.Millisecond)
	cfg := &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{Enabled: true, LeaseSeconds: 3, WorkerConcurrency: 1}}}
	worker := service.NewVideoTaskWorker(repo, nil, nil, service.NewVideoProviderRegistry(), nil, nil, nil, nil, cfg)
	require.NoError(t, worker.ProcessBatch(ctx, 1))
	updated, err := repo.GetVideoTaskForOwner(ctx, user.ID, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.VideoGenerationSubmissionUnknown, updated.GenerationState)
	require.Equal(t, int64(1), creates.Load())
	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance,frozen_balance FROM users WHERE id=$1`, user.ID).Scan(&balance, &frozen))
	require.Equal(t, 9.0, balance)
	require.Equal(t, 1.0, frozen)
}

func TestVideoReleaseProcessCrashAfterSettlementDoesNotDoubleCharge(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), uuid.NewString(), "process-settle", 1))
	require.NoError(t, err)
	task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID,
		service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.NoError(t, err)
	one := 1.0
	task, err = repo.SaveVideoProviderAccepted(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID,
		service.VideoProviderAcceptance{ProviderTaskID: "video_" + uuid.NewString(), GenerationState: service.VideoGenerationCompleted,
			BillingState: service.VideoBillingCapturePending, ActualUnits: &one, ActualCost: &one})
	require.NoError(t, err)
	killVideoReleaseChild(t, "settle", task, "")
	_, _, _, err = repo.billing.ResumeVideoBalanceSettlement(ctx, task)
	require.Error(t, err, "the crashed writer's unexpired outbox lease must remain exclusive")
	// Advance only this isolated fixture's claim age instead of waiting two minutes.
	_, err = integrationDB.ExecContext(ctx, `UPDATE usage_billing_outbox SET claimed_at=NOW()-INTERVAL '3 minutes' WHERE api_key_id=$1 AND request_id=$2`, key.ID, service.VideoTaskCaptureRequestID(task.PublicID))
	require.NoError(t, err)
	recovered, _, found, err := repo.billing.ResumeVideoBalanceSettlement(ctx, task)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, recovered.OutboxReceipt)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, recovered.OutboxReceipt.WorkerID, recovered.OutboxReceipt.ID))
	var count int
	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE user_id=$1 AND request_id=$2`, user.ID, service.VideoTaskCaptureRequestID(task.PublicID)).Scan(&count))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance,frozen_balance FROM users WHERE id=$1`, user.ID).Scan(&balance, &frozen))
	require.Equal(t, 1, count)
	require.Equal(t, 9.0, balance)
	require.Zero(t, frozen)
	svc := service.NewVideoTaskService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err = svc.DeleteForOwner(ctx, user.ID, task.PublicID)
	require.NoError(t, err)
	snapshot, err := repo.GetVideoOperationalSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), snapshot.DeletePending)
	require.NotNil(t, snapshot.OldestDeletePending)
	_, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID,
		service.VideoTaskTransition{DeleteState: service.VideoDeleteFailed, EventType: "provider_delete_failed"})
	require.NoError(t, err)
	retried, err := repo.GetVideoOperationalSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, snapshot.OldestDeletePending, retried.OldestDeletePending, "retry must not reset deletion age")
}
