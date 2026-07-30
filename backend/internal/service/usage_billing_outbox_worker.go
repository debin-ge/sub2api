package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	usageBillingOutboxBatchSize    = 32
	usageBillingOutboxConcurrency  = 8
	usageBillingOutboxPollInterval = time.Second
	usageBillingOutboxLease        = 2 * time.Minute
	usageBillingOutboxItemTimeout  = 20 * time.Second
)

// UsageBillingOutboxWorker drains billing intents left behind by a failed
// post-usage transaction. Claims are leased in PostgreSQL, so every process may
// run a worker without requiring a separate leader election.
type UsageBillingOutboxWorker struct {
	repo        DurableUsageBillingRepository
	postEffects *UsageBillingPostEffectsService
	workerID    string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	start  sync.Once
	stop   sync.Once

	running   atomic.Bool
	processed atomic.Uint64
	failures  atomic.Uint64
}

func NewUsageBillingOutboxWorker(
	repo DurableUsageBillingRepository,
	postEffects *UsageBillingPostEffectsService,
) *UsageBillingOutboxWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &UsageBillingOutboxWorker{
		repo:        repo,
		postEffects: postEffects,
		workerID:    "usage-billing-" + uuid.NewString(),
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (w *UsageBillingOutboxWorker) Start() {
	if w == nil {
		return
	}
	if w.repo == nil {
		w.failures.Add(1)
		slog.Error("usage billing outbox worker disabled because repository is nil")
		return
	}
	w.start.Do(func() {
		w.running.Store(true)
		w.wg.Add(1)
		go w.run()
	})
}

func (w *UsageBillingOutboxWorker) Stop() {
	if w == nil {
		return
	}
	w.stop.Do(func() {
		w.cancel()
		w.wg.Wait()
		w.running.Store(false)
	})
}

func (w *UsageBillingOutboxWorker) run() {
	defer w.wg.Done()
	defer w.running.Store(false)

	ticker := time.NewTicker(usageBillingOutboxPollInterval)
	defer ticker.Stop()

	for {
		if err := w.processBatch(w.ctx); err != nil && w.ctx.Err() == nil {
			w.failures.Add(1)
			slog.Error("usage billing outbox batch failed", "error", err)
		}
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *UsageBillingOutboxWorker) processBatch(ctx context.Context) error {
	if w == nil {
		return errors.New("usage billing outbox worker is nil")
	}
	if w.repo == nil {
		return errors.New("usage billing outbox repository is nil")
	}
	events, err := w.repo.ClaimUsageBillingOutbox(ctx, w.workerID, usageBillingOutboxBatchSize, usageBillingOutboxLease)
	if err != nil {
		return fmt.Errorf("claim usage billing outbox: %w", err)
	}

	semaphore := make(chan struct{}, usageBillingOutboxConcurrency)
	var wg sync.WaitGroup
	for i := range events {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case semaphore <- struct{}{}:
		}
		event := events[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-semaphore }()
			w.processEvent(ctx, event)
		}()
	}
	wg.Wait()
	return nil
}

func (w *UsageBillingOutboxWorker) processEvent(parent context.Context, event UsageBillingOutboxEvent) {
	if event.Command == nil || event.UsageLog == nil {
		w.quarantineEvent(event, fmt.Errorf(
			"%w: claimed outbox event is missing command or usage log",
			ErrUsageBillingPayloadInvalid,
		))
		return
	}

	ctx, cancel := context.WithTimeout(parent, usageBillingOutboxItemTimeout)
	if event.Stage == usageBillingOutboxStageBillingService &&
		event.Command != nil &&
		event.Command.PlatformQuotaSnapshotNeeded {
		if w.postEffects == nil {
			cancel()
			w.retryEvent(event, ErrUsageBillingPlatformQuotaSnapshotRequired)
			return
		}
		changed, err := w.postEffects.PreparePlatformQuotaSnapshot(ctx, event.Command)
		if err != nil {
			cancel()
			w.retryEvent(event, err)
			return
		}
		if changed {
			if err := w.repo.UpdateUsageBillingOutboxCommand(ctx, w.workerID, event.ID, event.Command); err != nil {
				cancel()
				w.retryEvent(event, err)
				return
			}
		}
	}

	result, err := w.repo.CompleteUsageBillingOutbox(ctx, w.workerID, event)
	if err == nil && result == nil {
		err = fmt.Errorf(
			"%w: complete usage billing outbox returned a nil result",
			ErrDurableUsageBillingRequired,
		)
	}
	if err == nil && !result.UsageLogRecorded {
		err = fmt.Errorf(
			"%w: complete usage billing outbox did not record the usage log",
			ErrDurableUsageBillingRequired,
		)
	}
	if err == nil {
		if w.postEffects == nil {
			err = errors.New("usage billing post-effects service is not configured")
		} else {
			err = w.postEffects.Finalize(ctx, event.Command, result)
		}
	}
	if err == nil {
		err = w.repo.AcknowledgeUsageBillingOutbox(ctx, w.workerID, event.ID)
	}
	cancel()
	if err == nil {
		w.processed.Add(1)
		return
	}
	if isPermanentUsageBillingWorkerError(err) {
		w.quarantineEvent(event, err)
		return
	}
	w.retryEvent(event, err)
}

const usageBillingOutboxStageBillingService = int8(0)

func isPermanentUsageBillingWorkerError(err error) bool {
	return errors.Is(err, ErrUsageBillingRequestConflict) ||
		errors.Is(err, ErrUsageBillingPayloadInvalid) ||
		errors.Is(err, ErrUserNotFound) ||
		errors.Is(err, ErrAccountNotFound) ||
		errors.Is(err, ErrSubscriptionNotFound)
}

func (w *UsageBillingOutboxWorker) quarantineEvent(event UsageBillingOutboxEvent, err error) {
	w.failures.Add(1)
	slog.Error("usage billing outbox item quarantined",
		"outbox_id", event.ID,
		"attempt", event.Attempts+1,
		"error", err,
	)
	quarantineCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if quarantineErr := w.repo.QuarantineUsageBillingOutbox(
		quarantineCtx,
		w.workerID,
		event.ID,
		boundedUsageBillingOutboxError(err),
	); quarantineErr != nil {
		w.failures.Add(1)
		slog.Error("quarantine usage billing outbox item failed",
			"outbox_id", event.ID,
			"error", quarantineErr,
		)
	}
}

func (w *UsageBillingOutboxWorker) retryEvent(event UsageBillingOutboxEvent, err error) {
	w.failures.Add(1)
	slog.Error("usage billing outbox item failed",
		"outbox_id", event.ID,
		"attempt", event.Attempts+1,
		"error", err,
	)
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 3*time.Second)
	retryErr := w.repo.RetryUsageBillingOutbox(
		retryCtx,
		w.workerID,
		event.ID,
		time.Now().UTC().Add(usageBillingOutboxRetryDelay(event.Attempts+1)),
		boundedUsageBillingOutboxError(err),
	)
	retryCancel()
	if retryErr != nil {
		w.failures.Add(1)
		slog.Error("release usage billing outbox claim failed",
			"outbox_id", event.ID,
			"error", retryErr,
		)
	}
}

func usageBillingOutboxRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 9 {
		attempt = 9
	}
	base := time.Second * time.Duration(1<<(attempt-1))
	delay := time.Duration(float64(base) * (0.8 + rand.Float64()*0.4))
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func boundedUsageBillingOutboxError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToValidUTF8(err.Error(), "\uFFFD")
	message = strings.ReplaceAll(message, "\x00", "\uFFFD")
	const maxBytes = 1024
	if len(message) <= maxBytes {
		return message
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut]
}

func ProvideUsageBillingOutboxWorker(
	repo DurableUsageBillingRepository,
	postEffects *UsageBillingPostEffectsService,
) *UsageBillingOutboxWorker {
	worker := NewUsageBillingOutboxWorker(repo, postEffects)
	worker.Start()
	return worker
}
