package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type videoSettlementRepoStub struct {
	settlement *BalanceSettlementCommand
	usageLog   *UsageLog
	acked      bool
	onSettle   func(*BalanceSettlementCommand)
}

func (r *videoSettlementRepoStub) ReserveBalanceHold(context.Context, *BalanceHoldCommand) (*BalanceHoldResult, error) {
	return &BalanceHoldResult{}, nil
}
func (r *videoSettlementRepoStub) CaptureBalanceHold(context.Context, *BalanceHoldCommand) (*BalanceHoldResult, error) {
	return &BalanceHoldResult{}, nil
}
func (r *videoSettlementRepoStub) ReleaseBalanceHold(context.Context, *BalanceHoldCommand) (*BalanceHoldResult, error) {
	return &BalanceHoldResult{}, nil
}
func (r *videoSettlementRepoStub) SettleVideoBalance(_ context.Context, settlement *BalanceSettlementCommand, usageLog *UsageLog) (*UsageBillingApplyResult, error) {
	r.settlement, r.usageLog = settlement, usageLog
	if r.onSettle != nil {
		r.onSettle(settlement)
	}
	return &UsageBillingApplyResult{
		Applied: true, UsageLogRecorded: usageLog != nil,
		NewBalance: floatPointer(8), FrozenBalance: floatPointer(0),
		OutboxReceipt: &UsageBillingOutboxReceipt{ID: 9, WorkerID: "settlement-worker"},
	}, nil
}
func (r *videoSettlementRepoStub) AcknowledgeVideoBalanceSettlement(context.Context, string, int64) error {
	r.acked = true
	return nil
}

func floatPointer(value float64) *float64 { return &value }

func newVideoWorkerForTest(task *VideoTask, observed *ProviderVideoTask) (*VideoTaskWorker, *videoTaskRepoStub, *videoSettlementRepoStub, *videoQueueStub) {
	owner := "worker-test"
	expiry := time.Now().Add(90 * time.Second)
	task.LeaseOwner, task.LeaseExpiresAt, task.LeaseEpoch = &owner, &expiry, 1
	taskRepo := &videoTaskRepoStub{task: task, sources: make(map[string]*VideoTask)}
	provider := &videoProviderStub{result: observed}
	account := Account{
		ID: *task.AccountID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "sk-test"},
	}
	accounts := &videoAccountRepoStub{accounts: []Account{account}}
	queue := &videoQueueStub{}
	settlements := &videoSettlementRepoStub{}
	cfg := &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{
		Enabled: true, PollIntervalSeconds: 10, LeaseSeconds: 90,
	}}}
	providers := NewVideoProviderRegistry(provider)
	taskService := &VideoTaskService{tasks: taskRepo, queue: queue, accounts: accounts, providers: providers, cfg: cfg, now: time.Now}
	taskService.settlements = settlements
	taskService.admission = &videoAdmissionStub{}
	settlements.onSettle = func(settlement *BalanceSettlementCommand) {
		if settlement.Action == BalanceSettlementRelease {
			taskRepo.task.BillingState = VideoBillingReleased
			taskRepo.task.ActualCost = floatPointer(0)
		}
	}
	worker := NewVideoTaskWorker(taskRepo, queue, accounts, providers, taskService, settlements, nil, nil, cfg)
	worker.workerID = "worker-test"
	worker.finalize = func(context.Context, *UsageBillingCommand, *UsageBillingApplyResult) error { return nil }
	return worker, taskRepo, settlements, queue
}

func baseVideoWorkerTask() *VideoTask {
	apiKeyID, accountID := int64(3), int64(11)
	billingUnit := VideoBillingUnitSecond
	holdAmount := 5.0
	providerID := "video_upstream"
	return &VideoTask{
		ID: 1, PublicID: "video_0123456789abcdef0123456789abcdef", UserID: 42,
		APIKeyID: &apiKeyID, AccountID: &accountID, Provider: VideoProviderOpenAI,
		ProviderTaskID: &providerID, GenerationState: VideoGenerationQueued,
		BillingState: VideoBillingHeld, DeleteState: VideoDeleteNone,
		BillingUnit: &billingUnit, HoldAmount: &holdAmount, RequestHash: "request-hash",
		RequestedModel: OpenAIVideoModelSora2, ChannelModel: OpenAIVideoModelSora2,
		UpstreamModel:     OpenAIVideoModelSora2,
		RequestAttributes: map[string]any{"seconds": float64(8), "size": "1280x720"},
		PriceSnapshot:     map[string]any{"unit_price": 0.5, "customer_multiplier": 2.0},
	}
}

func TestVideoTaskWorkerPollCompletedCreatesCaptureIntent(t *testing.T) {
	task := baseVideoWorkerTask()
	observed := &ProviderVideoTask{
		ProviderTaskID: "video_upstream", Status: VideoGenerationCompleted,
		RawStatus: "completed", Progress: floatPointer(100),
		Metadata: map[string]any{"seconds": "8"}, ContentVariants: []string{"video"},
	}
	worker, tasks, _, _ := newVideoWorkerForTest(task, observed)
	require.NoError(t, worker.processTask(context.Background(), task))
	require.Equal(t, VideoGenerationCompleted, tasks.task.GenerationState)
	require.Equal(t, VideoBillingCapturePending, tasks.task.BillingState)
	require.NotNil(t, tasks.task.ActualCost)
	require.InDelta(t, 8, *tasks.task.ActualCost, 0.000001)
	require.Equal(t, VideoGenerationInProgress, ProjectVideoStatus(tasks.task))
	require.Len(t, tasks.transitions, 1)
	require.True(t, tasks.transitions[0].IncrementPollAttempts)
}

func TestVideoTaskWorkerOldActiveDeleteDoesNotSuppressObservationOrSettlement(t *testing.T) {
	for _, deleteState := range []string{VideoDeleteRequested, VideoDeleteDeleting, VideoDeleteFailed} {
		t.Run(deleteState, func(t *testing.T) {
			task := baseVideoWorkerTask()
			task.DeleteState = deleteState
			observed := &ProviderVideoTask{ProviderTaskID: *task.ProviderTaskID, Status: VideoGenerationCompleted,
				RawStatus: "completed", Usage: map[string]any{"seconds": 8}, Metadata: map[string]any{"seconds": 8}}
			worker, tasks, settlements, _ := newVideoWorkerForTest(task, observed)
			provider, _ := worker.providers.Get(VideoProviderOpenAI)
			providerStub := provider.(*videoProviderStub)
			require.NoError(t, worker.processTask(context.Background(), task))
			require.Equal(t, 1, providerStub.getCalls)
			require.Zero(t, providerStub.deleteCalls)
			require.Equal(t, VideoGenerationCompleted, tasks.task.GenerationState)
			require.Equal(t, VideoBillingCapturePending, tasks.task.BillingState)
			require.Equal(t, deleteState, tasks.task.DeleteState)
			settlements.onSettle = func(*BalanceSettlementCommand) { tasks.task.BillingState = VideoBillingCaptured }
			require.NoError(t, worker.processTask(context.Background(), tasks.task))
			require.Equal(t, BalanceSettlementCapture, settlements.settlement.Action)
			require.Zero(t, providerStub.deleteCalls)
			require.NoError(t, worker.processTask(context.Background(), tasks.task))
			require.Equal(t, 1, providerStub.deleteCalls)
			require.Equal(t, VideoGenerationCompleted, tasks.task.GenerationState)
			require.Equal(t, VideoBillingCaptured, tasks.task.BillingState)
			require.Equal(t, VideoDeleteDeleted, tasks.task.DeleteState)
		})
	}
}

func TestVideoTaskWorkerRecoversTerminalHeldAndSettles(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationCompleted
	task.ResponseMetadata = map[string]any{"seconds": "8", "size": "1280x720"}
	worker, tasks, settlements, _ := newVideoWorkerForTest(task, nil)

	require.NoError(t, worker.processTask(context.Background(), task))
	require.Equal(t, VideoBillingCapturePending, tasks.transitions[0].BillingState)
	require.Equal(t, "terminal_billing_recovered", tasks.transitions[0].EventType)
	require.NotNil(t, settlements.settlement)
	require.Equal(t, BalanceSettlementCapture, settlements.settlement.Action)
}

func TestVideoTaskWorkerReleasesAbandonedHeldSubmissionWithoutProviderCall(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationHeld
	task.ProviderTaskID = nil
	worker, tasks, settlements, _ := newVideoWorkerForTest(task, nil)

	require.NoError(t, worker.processTask(context.Background(), task))
	require.Equal(t, VideoGenerationFailed, tasks.task.GenerationState)
	require.Equal(t, VideoBillingReleased, tasks.task.BillingState)
	require.Equal(t, "held_submission_abandoned", *tasks.task.LastErrorCode)
	require.NotNil(t, settlements.settlement)
	require.Equal(t, BalanceSettlementRelease, settlements.settlement.Action)
	provider := worker.providers.providers[VideoProviderOpenAI].(*videoProviderStub)
	require.Zero(t, provider.createCalls)
	require.Zero(t, provider.getCalls)
}

func TestVideoTaskWorkerCountsRetryablePollFailure(t *testing.T) {
	task := baseVideoWorkerTask()
	worker, tasks, _, queue := newVideoWorkerForTest(task, nil)
	provider := worker.providers.providers[VideoProviderOpenAI].(*videoProviderStub)
	provider.err = &VideoProviderError{Kind: "transport", Code: "timeout", Message: "timeout", Retryable: true}

	err := worker.processTask(context.Background(), task)

	require.Error(t, err)
	require.Equal(t, 1, tasks.task.PollAttempts)
	require.Len(t, tasks.transitions, 1)
	require.True(t, tasks.transitions[0].IncrementPollAttempts)
	require.Equal(t, "provider_poll_failed", tasks.transitions[0].EventType)
	require.NotNil(t, tasks.transitions[0].NextActionAt)
	require.NotEmpty(t, queue.requeued)
}

func TestVideoTaskWorkerPollRetryUsesFixedIntervalAndHonorsRetryAfter(t *testing.T) {
	task := baseVideoWorkerTask()
	worker, tasks, _, _ := newVideoWorkerForTest(task, nil)
	provider := worker.providers.providers[VideoProviderOpenAI].(*videoProviderStub)
	provider.err = &VideoProviderError{
		Kind: "rate_limit", Code: "rate_limited", Message: "rate limited",
		Retryable: true, RetryAfter: 45 * time.Second,
	}
	started := time.Now().UTC()

	err := worker.processTask(context.Background(), task)

	require.Error(t, err)
	require.NotNil(t, tasks.transitions[0].NextActionAt)
	require.False(t, tasks.transitions[0].NextActionAt.Before(started.Add(45*time.Second)))
	require.True(t, tasks.transitions[0].NextActionAt.Before(started.Add(55*time.Second)))

	require.Equal(t, 10*time.Second, videoPollInterval(worker.cfg, 0))
	require.Equal(t, 45*time.Second, videoPollInterval(worker.cfg, 45*time.Second))
}

func TestVideoTaskWorkerCountsPermanentPollFailureBeforeReview(t *testing.T) {
	task := baseVideoWorkerTask()
	worker, tasks, settlements, _ := newVideoWorkerForTest(task, nil)
	provider := worker.providers.providers[VideoProviderOpenAI].(*videoProviderStub)
	provider.err = &VideoProviderError{Kind: "permission", Code: "forbidden", Message: "forbidden", Retryable: false}

	require.NoError(t, worker.processTask(context.Background(), task))
	require.Equal(t, 1, tasks.task.PollAttempts)
	require.Equal(t, VideoBillingManualReview, tasks.task.BillingState)
	require.Nil(t, settlements.settlement)
}

func TestVideoTaskWorkerReleasesFailedObservationInSamePoll(t *testing.T) {
	task := baseVideoWorkerTask()
	observed := &ProviderVideoTask{
		ProviderTaskID: *task.ProviderTaskID, Status: VideoGenerationFailed, RawStatus: "failed",
		ErrorCode: "content_policy", ErrorMessage: "video generation was rejected by content policy",
		Usage: map[string]any{"seconds": 3},
	}
	worker, tasks, settlements, _ := newVideoWorkerForTest(task, observed)

	require.NoError(t, worker.processTask(context.Background(), task))
	require.Equal(t, VideoGenerationFailed, tasks.task.GenerationState)
	require.Equal(t, VideoBillingReleased, tasks.task.BillingState)
	require.Equal(t, "content_policy", *tasks.task.LastErrorCode)
	require.Equal(t, "video generation was rejected by content policy", *tasks.task.LastErrorMessage)
	require.Equal(t, BalanceSettlementRelease, settlements.settlement.Action)
	require.True(t, settlements.acked)
}

func TestVideoTaskWorkerSanitizesPermanentPollFailure(t *testing.T) {
	task := baseVideoWorkerTask()
	worker, tasks, _, _ := newVideoWorkerForTest(task, nil)
	provider := worker.providers.providers[VideoProviderOpenAI].(*videoProviderStub)
	provider.err = &VideoProviderError{
		Kind: strings.Repeat("x", 33), Code: strings.Repeat("x", 129),
		Message: "Bearer provider-secret", Retryable: false,
	}

	require.NoError(t, worker.processTask(context.Background(), task))
	require.Equal(t, "upstream", tasks.transitions[0].ErrorKind)
	require.Equal(t, "upstream_error", tasks.transitions[0].ErrorCode)
	require.Equal(t, "video provider poll failed", tasks.transitions[0].ErrorMessage)
}

func TestVideoTaskReconcileIgnoresLateNonMonotonicObservation(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationCompleted
	task.BillingState = VideoBillingCaptured
	repo := &videoTaskRepoStub{task: task}
	svc := &VideoTaskService{tasks: repo, cfg: &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{PollIntervalSeconds: 10}}}, now: time.Now}
	updated, err := svc.ReconcileProviderObservation(context.Background(), task, &ProviderVideoTask{Status: VideoGenerationQueued, RawStatus: "queued"}, "webhook")
	require.NoError(t, err)
	require.Same(t, task, updated)
	require.Empty(t, repo.transitions)
}

func TestVideoTaskReconcileTreatsConcurrentTerminalAdvanceAsIgnored(t *testing.T) {
	stale := baseVideoWorkerTask()
	latest := *stale
	latest.GenerationState = VideoGenerationCompleted
	latest.BillingState = VideoBillingCaptured
	latest.Version++
	repo := &videoTaskRepoStub{
		task: stale, transitionErr: ErrVideoInvalidTransition, transitionErrTask: &latest,
	}
	svc := &VideoTaskService{
		tasks: repo,
		cfg: &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{
			PollIntervalSeconds: 10,
		}}},
		now: time.Now,
	}

	updated, err := svc.ReconcileProviderObservation(context.Background(), stale, &ProviderVideoTask{
		Status: VideoGenerationCompleted, RawStatus: "completed", Metadata: map[string]any{"seconds": 8},
	}, "provider_polled")

	require.NoError(t, err)
	require.Same(t, &latest, updated)
	require.Len(t, repo.events, 1)
	require.Equal(t, "concurrent_state_advanced", repo.events[0].Payload["reason"])
}

func TestVideoTaskWorkerQuarantinesSubmissionUnknownWithoutSettlement(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationSubmissionUnknown
	past := time.Now().UTC().Add(-time.Minute)
	task.NextActionAt = &past
	worker, tasks, settlements, _ := newVideoWorkerForTest(task, nil)
	require.NoError(t, worker.processTask(context.Background(), task))
	require.Equal(t, VideoGenerationSubmissionUnknown, tasks.task.GenerationState)
	require.Equal(t, VideoBillingManualReview, tasks.task.BillingState)
	require.Nil(t, settlements.settlement)
}

func TestVideoTaskWorkerConvertsStaleSubmittingWithoutReplayingCreate(t *testing.T) {
	task := baseVideoWorkerTask()
	task.ProviderTaskID = nil
	task.GenerationState = VideoGenerationSubmitting
	past := time.Now().UTC().Add(-time.Minute)
	task.NextActionAt = &past
	worker, tasks, settlements, _ := newVideoWorkerForTest(task, nil)

	require.NoError(t, worker.processTask(context.Background(), task))
	require.Equal(t, VideoGenerationSubmissionUnknown, tasks.task.GenerationState)
	require.Equal(t, VideoBillingHeld, tasks.task.BillingState)
	require.Equal(t, "stale_submitting", *tasks.task.LastErrorCode)
	require.NotNil(t, tasks.task.NextActionAt)
	require.True(t, tasks.task.NextActionAt.After(time.Now().UTC()))
	provider := worker.providers.providers[VideoProviderOpenAI].(*videoProviderStub)
	require.Zero(t, provider.createCalls)
	require.Zero(t, provider.getCalls)
	require.Nil(t, settlements.settlement)
}

func TestVideoTaskWorkerRunsProviderAccessCleanupBeforeClaim(t *testing.T) {
	task := baseVideoWorkerTask()
	worker, tasks, _, _ := newVideoWorkerForTest(task, nil)

	require.NoError(t, worker.ProcessBatch(context.Background(), 32))
	require.Equal(t, 1, tasks.accessCleanupCalls)
}

func TestVideoTaskWorkerUsesRedisReservationWithDatabaseLease(t *testing.T) {
	task := baseVideoWorkerTask()
	task.ProviderTaskID = nil
	task.GenerationState = VideoGenerationSubmitting
	past := time.Now().UTC().Add(-time.Minute)
	task.NextActionAt = &past
	worker, tasks, _, queue := newVideoWorkerForTest(task, nil)
	tasks.claimedByID = task
	queue.reserved = []string{task.PublicID}

	require.NoError(t, worker.ProcessBatch(context.Background(), 1))
	require.Equal(t, 1, tasks.claimByIDCalls)
	require.Equal(t, VideoGenerationSubmissionUnknown, tasks.task.GenerationState)
	require.Contains(t, queue.acked, task.PublicID)
}

func TestVideoTaskWorkerFallsBackToDatabaseSweepWhenRedisFails(t *testing.T) {
	task := baseVideoWorkerTask()
	task.ProviderTaskID = nil
	task.GenerationState = VideoGenerationSubmitting
	past := time.Now().UTC().Add(-time.Minute)
	task.NextActionAt = &past
	worker, tasks, _, queue := newVideoWorkerForTest(task, nil)
	tasks.dueTasks = []*VideoTask{task}
	queue.reserveErr = errors.New("redis unavailable")

	require.NoError(t, worker.ProcessBatch(context.Background(), 1))
	require.Zero(t, tasks.claimByIDCalls)
	require.Equal(t, VideoGenerationSubmissionUnknown, tasks.task.GenerationState)
}

func TestVideoTaskWorkerCaptureSettlementUsesHoldBackedV2Command(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationCompleted
	task.BillingState = VideoBillingCapturePending
	task.ActualCost = floatPointer(8)
	task.ActualUnits = floatPointer(8)
	worker, _, settlements, _ := newVideoWorkerForTest(task, nil)
	require.NoError(t, worker.processTask(context.Background(), task))
	require.NotNil(t, settlements.settlement)
	require.Equal(t, BalanceSettlementCapture, settlements.settlement.Action)
	require.Equal(t, VideoTaskCaptureRequestID(task.PublicID), settlements.settlement.Hold.RequestID)
	require.Equal(t, 8.0, settlements.settlement.Hold.ActualAmount)
	require.Zero(t, settlements.settlement.Billing.BalanceCost)
	require.Equal(t, 4.0, settlements.settlement.Billing.TotalCost)
	require.Equal(t, 8.0, settlements.settlement.Billing.ActualCost)
	require.NotNil(t, settlements.usageLog)
	require.Equal(t, 1, settlements.usageLog.VideoCount)
	require.Equal(t, VideoBillingUnitSecond, *settlements.usageLog.BillingTier)
	require.Equal(t, "video", *settlements.usageLog.BillingMode)
	require.Equal(t, 8, *settlements.usageLog.VideoDurationSeconds)
	require.Equal(t, "1280x720", *settlements.usageLog.VideoResolution)
	require.Equal(t, 4.0, settlements.usageLog.TotalCost)
	require.Equal(t, 8.0, settlements.usageLog.ActualCost)
	require.Equal(t, 2.0, settlements.usageLog.RateMultiplier)
	require.True(t, settlements.acked)
}

func TestBuildVideoUsageSettlementRecordsVideoTokenPriceAndUsage(t *testing.T) {
	task := baseVideoWorkerTask()
	billingUnit := VideoBillingUnitVideoToken
	task.BillingUnit = &billingUnit
	task.ActualUnits = floatPointer(125_000)
	task.PriceSnapshot = map[string]any{
		"unit_price": 0.000002, "customer_multiplier": 1.5, "resolution": "480p", "seconds": float64(6),
	}
	task.ResponseMetadata = map[string]any{"size": "864x480", "seconds": "7"}

	command, usageLog, err := buildVideoUsageSettlement(task, "video:capture:test", 0.375)
	require.NoError(t, err)

	require.Equal(t, 125_000, command.OutputTokens)
	require.InDelta(t, 0.25, command.TotalCost, 1e-12)
	require.InDelta(t, 0.375, command.ActualCost, 1e-12)
	require.Equal(t, 125_000, usageLog.OutputTokens)
	require.InDelta(t, 0.25, usageLog.OutputCost, 1e-12)
	require.InDelta(t, 0.25, usageLog.TotalCost, 1e-12)
	require.InDelta(t, 0.375, usageLog.ActualCost, 1e-12)
	require.InDelta(t, 1.5, usageLog.RateMultiplier, 1e-12)
	require.Equal(t, VideoBillingUnitVideoToken, *usageLog.BillingTier)
	require.Equal(t, "864x480", *usageLog.VideoResolution)
	require.Equal(t, 7, *usageLog.VideoDurationSeconds)
}

func TestBuildVideoUsageSettlementDoesNotPersistInternalResolutionKey(t *testing.T) {
	task := baseVideoWorkerTask()
	task.UpstreamModel = "doubao-seedance-2.0-mini-480p"
	task.ChannelModel = task.UpstreamModel
	task.RequestedModel = task.UpstreamModel
	task.ResponseMetadata = map[string]any{}
	task.PriceSnapshot = map[string]any{
		"unit_price": 1.0, "customer_multiplier": 1.0,
		"resolution": "resolution-1", "raw_size": "", "seconds": float64(4),
	}
	task.RequestAttributes = map[string]any{"resolution": "resolution-1", "size": "", "seconds": float64(4)}

	_, usageLog, err := buildVideoUsageSettlement(task, "video:capture:resolution-key", 4)
	require.NoError(t, err)

	require.NotNil(t, usageLog.VideoResolution)
	require.Equal(t, "480p", *usageLog.VideoResolution)
}

func TestBuildVideoUsageSettlementOmitsUnknownResolution(t *testing.T) {
	task := baseVideoWorkerTask()
	task.UpstreamModel = "provider-model"
	task.ChannelModel = task.UpstreamModel
	task.RequestedModel = task.UpstreamModel
	task.ResponseMetadata = map[string]any{}
	task.PriceSnapshot = map[string]any{
		"unit_price": 1.0, "customer_multiplier": 1.0,
		"resolution": "resolution-1", "raw_size": "", "seconds": float64(4),
	}
	task.RequestAttributes = map[string]any{"resolution": "resolution-1", "size": "", "seconds": float64(4)}

	_, usageLog, err := buildVideoUsageSettlement(task, "video:capture:unknown-resolution", 4)
	require.NoError(t, err)

	require.Nil(t, usageLog.VideoResolution)
}

func TestVideoTaskWorkerReleaseSettlementWritesNoUsageLog(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationFailed
	task.BillingState = VideoBillingReleasePending
	worker, _, settlements, _ := newVideoWorkerForTest(task, nil)
	require.NoError(t, worker.processTask(context.Background(), task))
	require.Equal(t, BalanceSettlementRelease, settlements.settlement.Action)
	require.Nil(t, settlements.settlement.Billing)
	require.Nil(t, settlements.usageLog)
	require.True(t, settlements.acked)
}

func TestVideoTaskWorkerEnqueuesCallbackBeforeAcknowledgingSettlement(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationCompleted
	task.BillingState = VideoBillingCapturePending
	task.ActualCost = floatPointer(8)
	task.ActualUnits = floatPointer(8)
	target := "enc:https://callback.example/hooks"
	task.CallbackURLEnc = &target
	worker, tasks, settlements, _ := newVideoWorkerForTest(task, nil)
	callbacks := &videoCallbackRepositoryStub{}
	callbacks.onEnqueue = func() { require.False(t, settlements.acked) }
	worker.callbacks = callbacks
	worker.cfg.Gateway.Video.Callback = config.GatewayVideoCallbackConfig{
		Enabled: true, RetryHours: 24, RequestTimeoutSeconds: 5, SigningSecret: "secret",
	}
	settlements.onSettle = func(*BalanceSettlementCommand) {
		tasks.task.BillingState = VideoBillingCaptured
	}

	require.NoError(t, worker.processTask(context.Background(), task))

	require.NotNil(t, callbacks.enqueued)
	require.Equal(t, task.ID, callbacks.enqueued.TaskID)
	require.Equal(t, "video.completed", callbacks.enqueued.EventType)
	payload, err := json.Marshal(callbacks.enqueued.Payload)
	require.NoError(t, err)
	require.NotContains(t, string(payload), task.Provider)
	require.NotContains(t, string(payload), *task.ProviderTaskID)
	require.True(t, settlements.acked)
}

func TestVideoTaskWorkerRecoversDeleteFailedWithoutPolling(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationCompleted
	task.BillingState = VideoBillingCaptured
	task.DeleteState = VideoDeleteFailed
	worker, tasks, _, _ := newVideoWorkerForTest(task, nil)
	provider, ok := worker.providers.Get(VideoProviderOpenAI)
	require.True(t, ok)
	stub := provider.(*videoProviderStub)

	require.NoError(t, worker.processTask(context.Background(), task))

	require.Equal(t, 1, stub.deleteCalls)
	require.Zero(t, stub.getCalls)
	require.Equal(t, VideoDeleteDeleted, tasks.task.DeleteState)
}

func TestVideoTaskWorkerRequeuesRetryableDeleteFailure(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationCompleted
	task.BillingState = VideoBillingCaptured
	task.DeleteState = VideoDeleteFailed
	worker, tasks, _, queue := newVideoWorkerForTest(task, nil)
	provider, _ := worker.providers.Get(VideoProviderOpenAI)
	provider.(*videoProviderStub).deleteErr = errors.New("temporary delete failure")

	err := worker.processTask(context.Background(), task)

	require.Error(t, err)
	require.Equal(t, VideoDeleteFailed, tasks.task.DeleteState)
	require.NotEmpty(t, queue.requeued)
}
