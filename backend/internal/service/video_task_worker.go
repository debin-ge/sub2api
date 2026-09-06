package service

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"math/rand/v2"

	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/google/uuid"
)

type VideoTaskWorker struct {
	tasks       VideoTaskRepository
	queue       VideoTaskQueue
	accounts    AccountRepository
	providers   *VideoProviderRegistry
	service     *VideoTaskService
	settlements VideoBalanceSettlementRepository
	callbacks   VideoCallbackRepository
	postEffects *UsageBillingPostEffectsService
	finalize    func(context.Context, *UsageBillingCommand, *UsageBillingApplyResult) error
	cfg         *config.Config
	workerID    string
	batchMu     sync.Mutex
	metricsMu   sync.Mutex
	metricsAt   time.Time
}

func NewVideoTaskWorker(
	tasks VideoTaskRepository,
	queue VideoTaskQueue,
	accounts AccountRepository,
	providers *VideoProviderRegistry,
	service *VideoTaskService,
	settlements VideoBalanceSettlementRepository,
	callbacks VideoCallbackRepository,
	postEffects *UsageBillingPostEffectsService,
	cfg *config.Config,
) *VideoTaskWorker {
	return &VideoTaskWorker{
		tasks: tasks, queue: queue, accounts: accounts, providers: providers,
		service: service, settlements: settlements, callbacks: callbacks, postEffects: postEffects, cfg: cfg,
		workerID: "video-worker-" + uuid.NewString(),
	}
}

func (w *VideoTaskWorker) ProcessBatch(ctx context.Context, limit int) error {
	if w == nil || w.tasks == nil {
		return errors.New("video task worker is not configured")
	}
	w.batchMu.Lock()
	defer w.batchMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if limit <= 0 {
		limit = 32
	}
	if limit > 1000 {
		limit = 1000
	}
	defer func() {
		metricsCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		w.refreshOperationalMetrics(metricsCtx)
	}()
	cleanupCtx, cleanupCancel := context.WithTimeout(ctx, 5*time.Second)
	_, cleanupErr := w.tasks.ClearExpiredVideoProviderAccess(cleanupCtx, limit)
	cleanupCancel()
	if cleanupErr != nil {
		return cleanupErr
	}
	lease := w.leaseDuration()
	concurrency := 4
	if w.cfg != nil && w.cfg.Gateway.Video.WorkerConcurrency > 0 {
		concurrency = w.cfg.Gateway.Video.WorkerConcurrency
	}
	if concurrency > 64 {
		concurrency = 64
	}
	if concurrency > limit {
		concurrency = limit
	}
	queueAvailable := w.queue != nil
	if queueAvailable {
		if promoted, err := w.queue.MoveDueToReady(ctx, limit); err != nil {
			observability.DefaultVideoMetrics().RecordWorkerRecovery("delayed_promotion", "error", 0)
			slog.Warn("video task delayed promotion failed; using database sweep", "error", err)
			queueAvailable = false
		} else {
			observability.DefaultVideoMetrics().RecordWorkerRecovery("delayed_promotion", "success", promoted)
		}
		if recovered, err := w.queue.RecoverStale(ctx, lease, limit); err != nil {
			observability.DefaultVideoMetrics().RecordWorkerRecovery("lease_expiry", "error", 0)
			slog.Warn("video task queue recovery failed; using database sweep", "error", err)
			queueAvailable = false
		} else {
			observability.DefaultVideoMetrics().RecordWorkerRecovery("lease_expiry", "success", recovered)
		}
	}
	remainingHints := limit
	finished := make(chan error, concurrency)
	active, claimed := 0, 0
	exhausted := false
	var batchErr error
	for active > 0 || (!exhausted && claimed < limit) {
		for ctx.Err() == nil && !exhausted && active < concurrency && claimed < limit {
			task, err := w.claimOneTask(ctx, lease, &queueAvailable, &remainingHints)
			if err != nil {
				batchErr = errors.Join(batchErr, err)
				exhausted = true
				break
			}
			if task == nil {
				exhausted = true
				break
			}
			claimed++
			active++
			go func(task *VideoTask) { finished <- w.processTask(ctx, task) }(task)
		}
		if active == 0 {
			break
		}
		if err := <-finished; err != nil && ctx.Err() == nil {
			slog.Error("video task worker item failed", "error", err)
		}
		active--
	}
	return errors.Join(batchErr, ctx.Err())
}

func (w *VideoTaskWorker) claimOneTask(ctx context.Context, lease time.Duration, queueAvailable *bool, remainingHints *int) (*VideoTask, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for *queueAvailable && *remainingHints > 0 {
		*remainingHints--
		publicID, err := w.queue.Reserve(ctx)
		if err != nil {
			*queueAvailable = false
			if !errors.Is(err, ErrVideoQueueEmpty) {
				slog.Warn("video task queue reserve failed; using database sweep", "error", err)
			}
			break
		}
		task, err := w.tasks.ClaimVideoTask(ctx, publicID, w.workerID, lease)
		if err != nil {
			return nil, err
		}
		if task != nil {
			return task, nil
		}
		_ = w.queue.Ack(ctx, publicID)
	}
	tasks, err := w.tasks.ClaimDueVideoTasks(ctx, w.workerID, 1, lease)
	if err != nil {
		observability.DefaultVideoMetrics().RecordWorkerRecovery("db_sweep", "error", 0)
		return nil, err
	}
	observability.DefaultVideoMetrics().RecordWorkerRecovery("db_sweep", "success", len(tasks))
	if len(tasks) == 0 {
		return nil, nil
	}
	return tasks[0], nil
}

func (w *VideoTaskWorker) leaseDuration() time.Duration {
	if w.cfg != nil && w.cfg.Gateway.Video.LeaseSeconds > 0 {
		return time.Duration(w.cfg.Gateway.Video.LeaseSeconds) * time.Second
	}
	return 90 * time.Second
}

func (w *VideoTaskWorker) processTask(ctx context.Context, task *VideoTask) error {
	if task == nil {
		return nil
	}
	lease := VideoTaskLeaseFromTask(task)
	if lease.Owner != w.workerID || lease.TaskID <= 0 || lease.Epoch <= 0 {
		return ErrVideoLeaseLost
	}
	initialTimeout := w.leaseDuration() / 3
	if initialTimeout > 5*time.Second {
		initialTimeout = 5 * time.Second
	}
	initialCtx, initialCancel := context.WithTimeout(ctx, initialTimeout)
	_, initialErr := w.tasks.RenewVideoTaskLease(initialCtx, lease, w.leaseDuration())
	initialCancel()
	if initialErr != nil {
		return errors.Join(ErrVideoLeaseLost, initialErr)
	}
	workCtx, cancelWork := context.WithCancelCause(WithVideoTaskLease(ctx, lease))
	defer cancelWork(nil)
	heartbeatCtx, stopHeartbeat := context.WithCancel(workCtx)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		interval := w.leaseDuration() / 3
		if interval > 20*time.Second {
			interval = 20 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				timeout := interval
				if timeout > 5*time.Second {
					timeout = 5 * time.Second
				}
				renewCtx, renewCancel := context.WithTimeout(heartbeatCtx, timeout)
				_, err := w.tasks.RenewVideoTaskLease(renewCtx, lease, w.leaseDuration())
				renewCancel()
				if err != nil {
					if heartbeatCtx.Err() == nil {
						cancelWork(errors.Join(ErrVideoLeaseLost, err))
					}
					return
				}
			}
		}
	}()
	ctx = workCtx
	var err error
	switch NextVideoAction(task) {
	case VideoActionSettle:
		err = w.settle(ctx, task)
	case VideoActionRecoverTerminalBilling:
		err = w.recoverTerminalBilling(ctx, task)
	case VideoActionObserve:
		err = w.poll(ctx, task)
	case VideoActionRecoverHeld:
		err = w.recoverAbandonedHeld(ctx, task)
	case VideoActionRecoverSubmitting:
		err = w.recoverStaleSubmitting(ctx, task)
	case VideoActionQuarantineUnknown:
		err = w.quarantineUnknown(ctx, task)
	case VideoActionDeleteContent:
		_, err = w.service.RetryDeleteTask(ctx, task)
	}
	stopHeartbeat()
	<-heartbeatDone
	if cause := context.Cause(workCtx); cause != nil {
		err = errors.Join(err, cause)
	}
	if errors.Is(err, ErrVideoLeaseLost) {
		return err
	}
	releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer releaseCancel()
	if err != nil {
		next := time.Now().UTC().Add(videoWorkerRetryDelay(task.PollAttempts + 1))
		var scheduled *videoTaskScheduledRetry
		if errors.As(err, &scheduled) {
			next = scheduled.next
		}
		if releaseErr := w.tasks.ReleaseVideoTaskLease(releaseCtx, lease, &next); releaseErr != nil {
			return errors.Join(err, releaseErr)
		}
		if w.queue != nil {
			_ = w.queue.RequeueAfter(releaseCtx, task.PublicID, time.Until(next))
		}
		return err
	}
	if err := w.tasks.ReleaseVideoTaskLease(releaseCtx, lease, nil); err != nil {
		return err
	}
	if w.queue != nil {
		_ = w.queue.Ack(releaseCtx, task.PublicID)
	}
	return nil
}

func (w *VideoTaskWorker) recoverAbandonedHeld(ctx context.Context, task *VideoTask) error {
	now := time.Now().UTC()
	updated, err := w.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		GenerationState: VideoGenerationFailed, BillingState: VideoBillingReleasePending,
		NextActionAt: &now, ErrorKind: "submission", ErrorCode: "held_submission_abandoned",
		ErrorMessage: "video submission did not start before its recovery deadline",
		EventType:    "held_submission_recovered",
	})
	if err != nil {
		return err
	}
	return w.settle(ctx, updated)
}

func (w *VideoTaskWorker) recoverTerminalBilling(ctx context.Context, task *VideoTask) error {
	providerTask := &ProviderVideoTask{
		ProviderTaskID: videoStringValue(task.ProviderTaskID), Status: task.GenerationState,
		Usage: task.UsageSnapshot, Metadata: task.ResponseMetadata,
	}
	updated, err := w.service.prepareTerminalBilling(ctx, task, providerTask)
	if err != nil || updated == nil {
		return err
	}
	if updated.BillingState == VideoBillingCapturePending || updated.BillingState == VideoBillingReleasePending {
		return w.settle(ctx, updated)
	}
	return nil
}

func (w *VideoTaskWorker) recoverStaleSubmitting(ctx context.Context, task *VideoTask) error {
	delay := 60 * time.Minute
	if w.cfg != nil && w.cfg.Gateway.Video.SubmissionUnknownQuarantineMinutes > 0 {
		delay = time.Duration(w.cfg.Gateway.Video.SubmissionUnknownQuarantineMinutes) * time.Minute
	}
	next := time.Now().UTC().Add(delay)
	_, err := w.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		GenerationState:   VideoGenerationSubmissionUnknown,
		NextActionAt:      &next,
		SubmissionUnknown: true,
		ErrorKind:         "submission",
		ErrorCode:         "stale_submitting",
		ErrorMessage:      "provider submission may have been accepted before local persistence completed",
		EventType:         "stale_submitting_recovered",
	})
	return err
}

func (w *VideoTaskWorker) poll(ctx context.Context, task *VideoTask) error {
	account, provider, ref, err := w.controlDependencies(ctx, task)
	if err != nil {
		return err
	}
	startedAt := time.Now()
	requestCtx, requestCancel := context.WithTimeout(ctx, videoWorkerRequestTimeout(w.cfg))
	defer requestCancel()
	observed, err := provider.Get(requestCtx, account, ref)
	if err != nil {
		observability.DefaultVideoMetrics().RecordProviderGet(task.Provider, "worker", "error")
		observability.DefaultVideoMetrics().RecordPoll(task.Provider, "error", time.Since(startedAt))
		var providerErr *VideoProviderError
		if errors.As(err, &providerErr) && !providerErr.Retryable {
			providerErr = sanitizedVideoProviderError(providerErr, "upstream", "poll_failed", "video provider poll failed")
			_, transitionErr := w.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
				BillingState: VideoBillingManualReview, Quarantine: true,
				ErrorKind: providerErr.Kind, ErrorCode: providerErr.Code,
				ErrorMessage: providerErr.Message, IncrementPollAttempts: true, EventType: "poll_manual_review",
			})
			return transitionErr
		}
		return w.recordRetryablePollFailure(ctx, task, err)
	}
	observability.DefaultVideoMetrics().RecordProviderGet(task.Provider, "worker", "success")
	observability.DefaultVideoMetrics().RecordPoll(task.Provider, "success", time.Since(startedAt))
	if observed == nil {
		return w.recordRetryablePollFailure(ctx, task, errors.New("video provider returned no task observation"))
	}
	_, err = w.service.ReconcileProviderObservation(ctx, task, observed, "provider_polled")
	return err
}

func (w *VideoTaskWorker) recordRetryablePollFailure(ctx context.Context, task *VideoTask, cause error) error {
	retryAfter := time.Duration(0)
	var providerErr *VideoProviderError
	if errors.As(cause, &providerErr) {
		retryAfter = providerErr.RetryAfter
	}
	next := time.Now().UTC().Add(videoPollInterval(w.cfg, retryAfter))
	_, transitionErr := w.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		GenerationState: task.GenerationState, NextActionAt: &next,
		IncrementPollAttempts: true, ErrorKind: "upstream", ErrorCode: "poll_failed",
		ErrorMessage: boundedProviderMessage(cause.Error(), ""), EventType: "provider_poll_failed",
	})
	if transitionErr != nil {
		return errors.Join(cause, transitionErr)
	}
	return &videoTaskScheduledRetry{cause: cause, next: next}
}

type videoTaskScheduledRetry struct {
	cause error
	next  time.Time
}

func (e *videoTaskScheduledRetry) Error() string { return e.cause.Error() }
func (e *videoTaskScheduledRetry) Unwrap() error { return e.cause }

func (w *VideoTaskWorker) quarantineUnknown(ctx context.Context, task *VideoTask) error {
	if task.NextActionAt != nil && time.Now().UTC().Before(*task.NextActionAt) {
		return nil
	}
	_, err := w.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		GenerationState: VideoGenerationSubmissionUnknown,
		BillingState:    VideoBillingManualReview,
		Quarantine:      true,
		ErrorKind:       "submission", ErrorCode: "submission_unknown_quarantined",
		ErrorMessage: "provider submission outcome requires exact manual reconciliation",
		EventType:    "submission_unknown_quarantined",
	})
	return err
}

func (w *VideoTaskWorker) settle(ctx context.Context, task *VideoTask) (returnErr error) {
	ctx = videoTaskWriteContext(ctx, task)
	actionLabel := "release"
	holdAmount := 0.0
	actualAmount := 0.0
	if task != nil && task.HoldAmount != nil {
		holdAmount = *task.HoldAmount
	}
	if task != nil && task.BillingState == VideoBillingCapturePending {
		actionLabel = "capture"
		if task.ActualCost != nil {
			actualAmount = *task.ActualCost
		}
	}
	defer func() {
		result := "success"
		if returnErr != nil {
			result = "error"
		}
		observability.DefaultVideoMetrics().RecordSettlement(actionLabel, result, holdAmount, actualAmount)
	}()
	if w.settlements == nil {
		return errors.New("video balance settlement repository is not configured")
	}
	if task.APIKeyID == nil || task.AccountID == nil || task.HoldAmount == nil {
		return w.markSettlementReview(ctx, task, "video settlement identity is incomplete")
	}
	if recovery, supported := w.settlements.(VideoBalanceSettlementRecovery); supported {
		result, command, found, err := recovery.ResumeVideoBalanceSettlement(ctx, task)
		if err != nil {
			return err
		}
		if found {
			return w.finalizeVideoSettlement(ctx, task, command, result)
		}
	}
	var review *VideoBillingReview
	if task.BillingReviewID != nil {
		authorizer, available := w.tasks.(VideoBillingReviewAuthorizationRepository)
		if !available {
			return ErrVideoReviewRequired
		}
		var err error
		review, err = authorizer.VerifyVideoBillingReview(ctx, task)
		if err != nil {
			if errors.Is(err, ErrVideoReviewConflict) || errors.Is(err, ErrVideoReviewRequired) {
				return w.markSettlementReview(ctx, task, "approved billing review no longer matches the current task facts")
			}
			return err
		}
	}
	if task.BillingState == VideoBillingCapturePending {
		if err := videoCheckObservedSpecification(task, task.ResponseMetadata); err != nil {
			canHonorFrozenQuote := errors.Is(err, ErrVideoSourceSpecConflict) && review != nil && review.HonorFrozenQuote
			if !canHonorFrozenQuote {
				return w.markSettlementReview(ctx, task, "provider output conflicts with the frozen execution specification")
			}
		}
	}
	action := BalanceSettlementRelease
	requestID := VideoTaskReleaseRequestID(task.PublicID)
	actualAmount = 0.0
	var command *UsageBillingCommand
	var usageLog *UsageLog
	if task.BillingState == VideoBillingCapturePending {
		if task.ActualCost == nil || *task.ActualCost < 0 || math.IsNaN(*task.ActualCost) || math.IsInf(*task.ActualCost, 0) {
			return w.markSettlementReview(ctx, task, "video actual cost is missing")
		}
		action = BalanceSettlementCapture
		requestID = VideoTaskCaptureRequestID(task.PublicID)
		actualAmount = *task.ActualCost
		var buildErr error
		command, usageLog, buildErr = buildVideoUsageSettlement(task, requestID, actualAmount)
		if buildErr != nil {
			return w.markSettlementReview(ctx, task, buildErr.Error())
		}
	}
	settlement := &BalanceSettlementCommand{
		TaskID: task.ID, Action: action,
		Hold: BalanceHoldCommand{
			BillingReviewID: valueOrZero(task.BillingReviewID),
			RequestID:       requestID, APIKeyID: *task.APIKeyID,
			RequestPayloadHash: task.RequestHash, UserID: task.UserID,
			Scope: BalanceHoldScopeVideoTask, RefID: task.PublicID,
			HoldAmount: *task.HoldAmount, ActualAmount: actualAmount,
		},
		Billing: command,
	}
	result, err := w.settlements.SettleVideoBalance(ctx, settlement, usageLog)
	if err != nil {
		return err
	}
	return w.finalizeVideoSettlement(ctx, task, command, result)
}

func (w *VideoTaskWorker) finalizeVideoSettlement(ctx context.Context, task *VideoTask, command *UsageBillingCommand, result *UsageBillingApplyResult) error {
	if result == nil {
		return errors.New("video settlement result is missing")
	}
	finalizeCommand := command
	if finalizeCommand == nil {
		finalizeCommand = &UsageBillingCommand{
			RequestID: VideoTaskReleaseRequestID(task.PublicID), APIKeyID: *task.APIKeyID, UserID: task.UserID,
			AccountID: *task.AccountID, MediaType: "video",
		}
	}
	finalize := w.finalize
	if finalize == nil && w.postEffects != nil {
		finalize = w.postEffects.Finalize
	}
	if finalize == nil {
		return errors.New("video billing post-effects service is not configured")
	}
	if err := finalize(ctx, finalizeCommand, result); err != nil {
		return err
	}
	if result.OutboxReceipt == nil {
		return errors.New("video settlement outbox receipt is missing")
	}
	if _, durable := w.callbacks.(VideoCallbackMaterializationRepository); !durable {
		if err := w.enqueueTerminalCallback(ctx, task.PublicID); err != nil {
			return err
		}
	}
	return w.settlements.AcknowledgeVideoBalanceSettlement(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID)
}

const videoOperationalMetricsInterval = 30 * time.Second

func (w *VideoTaskWorker) refreshOperationalMetrics(ctx context.Context) {
	if w == nil || ctx.Err() != nil {
		return
	}
	now := time.Now().UTC()
	w.metricsMu.Lock()
	if !w.metricsAt.IsZero() && now.Sub(w.metricsAt) < videoOperationalMetricsInterval {
		w.metricsMu.Unlock()
		return
	}
	w.metricsAt = now
	w.metricsMu.Unlock()

	if reader, ok := w.tasks.(VideoOperationalMetricsReader); ok {
		snapshot, err := reader.GetVideoOperationalSnapshot(ctx)
		if err != nil {
			slog.Warn("video operational metrics database snapshot failed", "error", err)
		} else if snapshot != nil {
			states := make([]observability.VideoTaskStateMetric, 0, len(snapshot.TaskStates))
			for _, state := range snapshot.TaskStates {
				states = append(states, observability.VideoTaskStateMetric{
					Provider: state.Provider, Operation: state.Operation, State: state.State,
					Count: state.Count, OldestEnteredAt: state.OldestEnteredAt,
				})
			}
			observability.DefaultVideoMetrics().UpdateOperational(observability.VideoOperationalMetrics{
				TaskStates: states, SubmissionUnknown: snapshot.SubmissionUnknown,
				UnknownHoldAmount: snapshot.UnknownHoldAmount, HeldAmount: snapshot.HeldAmount,
				OldestSettlementPending: snapshot.OldestSettlementPending,
				OldestManualReview:      snapshot.OldestManualReview,
				DeletePending:           snapshot.DeletePending,
				OldestDeletePending:     snapshot.OldestDeletePending,
			}, now)
		}
	}
	if reader, ok := w.queue.(VideoTaskQueueStatsReader); ok {
		stats, err := reader.VideoTaskQueueStats(ctx)
		if err != nil {
			slog.Warn("video task queue metrics snapshot failed", "error", err)
		} else {
			observability.DefaultVideoMetrics().SetQueueDepth(stats.Ready, stats.Delayed, stats.Active)
		}
	}
}

func (w *VideoTaskWorker) enqueueTerminalCallback(ctx context.Context, publicID string) error {
	if w == nil || w.callbacks == nil || w.cfg == nil || !w.cfg.Gateway.Video.Callback.Enabled {
		return nil
	}
	task, err := w.tasks.GetVideoTaskByPublicID(ctx, publicID)
	if err != nil {
		return err
	}
	disclosurePolicy := config.VideoDisclosureNone
	if w.service != nil {
		disclosurePolicy, _ = w.service.videoDisclosurePolicy(ctx, task)
	}
	delivery, needed, err := BuildVideoCallbackDelivery(task, w.cfg, time.Now().UTC(), disclosurePolicy)
	if err != nil || !needed {
		return err
	}
	_, _, err = w.callbacks.EnqueueVideoCallback(ctx, *delivery)
	return err
}

func (w *VideoTaskWorker) markSettlementReview(ctx context.Context, task *VideoTask, message string) error {
	_, err := w.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		BillingState: VideoBillingManualReview, Quarantine: true,
		ErrorKind: "billing", ErrorCode: "settlement_invalid", ErrorMessage: message,
		EventType: "settlement_manual_review",
	})
	return err
}

func (w *VideoTaskWorker) controlDependencies(ctx context.Context, task *VideoTask) (*Account, VideoProvider, ProviderTaskRef, error) {
	if task.AccountID == nil || task.ProviderTaskID == nil || w.accounts == nil || w.providers == nil {
		return nil, nil, ProviderTaskRef{}, ErrVideoInvalidRequest
	}
	account, err := w.accounts.GetByID(ctx, *task.AccountID)
	if err != nil {
		return nil, nil, ProviderTaskRef{}, err
	}
	provider, ok := w.providers.Get(task.Provider)
	if !ok {
		return nil, nil, ProviderTaskRef{}, ErrVideoProviderUnsupported
	}
	return account, provider, ProviderTaskRef{
		Provider: task.Provider, AccountID: *task.AccountID, ProviderTaskID: *task.ProviderTaskID,
	}, nil
}

func buildVideoUsageSettlement(task *VideoTask, requestID string, actualCost float64) (*UsageBillingCommand, *UsageLog, error) {
	if task == nil || !finiteNonNegative(actualCost) {
		return nil, nil, errors.New("video settlement task or actual cost is invalid")
	}
	accountMultiplier, err := videoAccountSettlementMultiplier(task)
	if err != nil {
		return nil, nil, err
	}
	quotaTime, err := videoUsageQuotaTime(task)
	if err != nil {
		return nil, nil, err
	}
	billingUnit := strings.TrimSpace(videoStringValue(task.BillingUnit))
	actualUnits := videoUsageUnits(task, billingUnit)
	unitPrice := numericMapValueOrZero(task.PriceSnapshot, "unit_price")
	customerMultiplier := numericMapValueOrZero(task.PriceSnapshot, "customer_multiplier")
	baseCost := unitPrice * actualUnits
	accountCost := QuantizeUsageBillingAmount(baseCost * accountMultiplier)
	if !finiteNonNegative(baseCost) || !finiteNonNegative(accountCost) {
		return nil, nil, errors.New("video account settlement cost is invalid")
	}
	occurredAt := task.CreatedAt
	if task.FinishedAt != nil {
		occurredAt = *task.FinishedAt
	}
	if occurredAt.IsZero() {
		occurredAt = time.Unix(0, 0).UTC()
	}
	seconds := videoUsageDurationSeconds(task)
	resolution := videoUsageResolution(task)
	var videoResolution *string
	if resolution != "" {
		videoResolution = &resolution
	}
	modelMappingChain := videoUsageModelMappingChain(task)
	billingMode := "video"
	outputTokens := 0
	outputCost := 0.0
	if billingUnit == VideoBillingUnitVideoToken && actualUnits <= float64(math.MaxInt) {
		outputTokens = int(math.Round(actualUnits))
		outputCost = baseCost
	}
	command := &UsageBillingCommand{
		RequestID: requestID, APIKeyID: valueOrZero(task.APIKeyID), UserID: task.UserID,
		AccountID: valueOrZero(task.AccountID), AccountType: AccountTypeAPIKey,
		Model: task.UpstreamModel, BillingType: BillingTypeBalance, OutputTokens: outputTokens,
		GroupID: task.GroupID, Platform: task.Provider, PlatformQuotaCost: actualCost,
		ActualCost: actualCost, TotalCost: baseCost, BalanceCost: 0,
		APIKeyQuotaCost: actualCost, APIKeyRateLimitCost: actualCost,
		AccountQuotaCost: accountCost, MediaType: "video", OccurredAt: occurredAt,
		QuotaTime: quotaTime,
	}
	usageLog := &UsageLog{
		UserID: task.UserID, APIKeyID: valueOrZero(task.APIKeyID), AccountID: valueOrZero(task.AccountID),
		RequestID: requestID, Model: task.UpstreamModel, RequestedModel: task.RequestedModel,
		ChannelID: task.ChannelID, ModelMappingChain: &modelMappingChain,
		BillingTier: &billingUnit, BillingMode: &billingMode, GroupID: task.GroupID,
		OutputTokens: outputTokens, OutputCost: outputCost,
		TotalCost: baseCost, ActualCost: actualCost, RateMultiplier: customerMultiplier, AccountRateMultiplier: &accountMultiplier, BillingType: BillingTypeBalance,
		BillingState: BillingStateSettled, VideoCount: 1,
		VideoResolution: videoResolution, VideoDurationSeconds: &seconds, CreatedAt: occurredAt,
	}
	if task.UpstreamModel != "" {
		usageLog.UpstreamModel = &task.UpstreamModel
	}
	return command, usageLog, nil
}

func videoAccountSettlementMultiplier(task *VideoTask) (float64, error) {
	if raw, exists := task.ProviderCostSnapshot["account_rate_multiplier"]; exists {
		value, valid := numericMapValue(map[string]any{"rate": raw}, "rate")
		if !valid || !finiteNonNegative(value) {
			return 0, errors.New("video account multiplier snapshot is invalid")
		}
		return value, nil
	}
	if version, _ := numericMapValue(task.PriceSnapshot, "billing_contract_version"); version >= 2 {
		return 0, errors.New("video account multiplier snapshot is missing")
	}
	return 1, nil
}

func videoUsageUnits(task *VideoTask, billingUnit string) float64 {
	if task != nil && task.ActualUnits != nil && *task.ActualUnits >= 0 &&
		!math.IsNaN(*task.ActualUnits) && !math.IsInf(*task.ActualUnits, 0) {
		return *task.ActualUnits
	}
	if task == nil {
		return 0
	}
	switch billingUnit {
	case VideoBillingUnitSecond:
		return float64(videoUsageDurationSeconds(task))
	case VideoBillingUnitVideoToken:
		if tokens, present, conflict := canonicalVideoTokenUsage(task.UsageSnapshot); present && !conflict {
			return tokens
		}
	case VideoBillingUnitRequest:
		return 1
	}
	return 0
}

func videoUsageDurationSeconds(task *VideoTask) int {
	if task == nil {
		return 0
	}
	for _, values := range []map[string]any{task.ResponseMetadata, task.PriceSnapshot, task.RequestAttributes} {
		if value, ok := numericMapValue(values, "seconds"); ok && value > 0 && value <= float64(math.MaxInt) {
			return int(math.Round(value))
		}
	}
	return 0
}

func videoUsageResolution(task *VideoTask) string {
	if task == nil {
		return ""
	}
	for _, candidate := range []struct {
		values map[string]any
		keys   []string
	}{
		{task.ResponseMetadata, []string{"size", "resolution"}},
		{task.PriceSnapshot, []string{"usage_resolution", "raw_size", "resolution"}},
		{task.RequestAttributes, []string{"size", "raw_size", "resolution"}},
	} {
		for _, key := range candidate.keys {
			if value, ok := candidate.values[key].(string); ok {
				if normalized := canonicalVideoUsageResolution(value); normalized != "" {
					return normalized
				}
			}
		}
	}
	return videoUsageResolutionFromModels(task.UpstreamModel, task.ChannelModel, task.RequestedModel)
}

func videoUsageModelMappingChain(task *VideoTask) string {
	if task == nil {
		return ""
	}
	chain := make([]string, 0, 3)
	for _, model := range []string{task.RequestedModel, task.ChannelModel, task.UpstreamModel} {
		model = strings.TrimSpace(model)
		if model != "" && (len(chain) == 0 || chain[len(chain)-1] != model) {
			chain = append(chain, model)
		}
	}
	return strings.Join(chain, "→")
}

func numericMapValueOrZero(values map[string]any, key string) float64 {
	value, _ := numericMapValue(values, key)
	return value
}

func videoWorkerRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	base := time.Second * time.Duration(1<<(attempt-1))
	delay := time.Duration(float64(base) * (0.8 + rand.Float64()*0.4))
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func videoPollInterval(cfg *config.Config, retryAfter time.Duration) time.Duration {
	interval := 10 * time.Second
	if cfg != nil {
		if cfg.Gateway.Video.PollIntervalSeconds > 0 {
			interval = time.Duration(cfg.Gateway.Video.PollIntervalSeconds) * time.Second
		}
	}
	if retryAfter > interval {
		return retryAfter
	}
	return interval
}

type VideoTaskRuntime struct {
	worker *VideoTaskWorker
	cfg    *config.Config
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
}

func NewVideoTaskRuntime(worker *VideoTaskWorker, cfg *config.Config) *VideoTaskRuntime {
	return &VideoTaskRuntime{worker: worker, cfg: cfg}
}

func (r *VideoTaskRuntime) Start() {
	if r == nil || r.worker == nil || r.cfg == nil || !r.cfg.Gateway.Video.Enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.cancel, r.done = cancel, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		batchSize := 32
		if r.cfg.Gateway.Video.WorkerBatchSize > 0 {
			batchSize = r.cfg.Gateway.Video.WorkerBatchSize
		}
		for {
			if err := r.worker.ProcessBatch(ctx, batchSize); err != nil && ctx.Err() == nil {
				slog.Error("video task worker batch failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (r *VideoTaskRuntime) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.cancel, r.done = nil, nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func ProvideVideoTaskRuntime(worker *VideoTaskWorker, cfg *config.Config) *VideoTaskRuntime {
	runtime := NewVideoTaskRuntime(worker, cfg)
	runtime.Start()
	return runtime
}
