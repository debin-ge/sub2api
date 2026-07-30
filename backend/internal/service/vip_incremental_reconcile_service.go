package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	vipIncrementalReconcileLeaderLockKey       = "vip:incremental-reconcile:leader"
	vipIncrementalReconcileHeartbeatJobName    = "vip_incremental_reconcile"
	vipIncrementalReconcileHeartbeatTimeout    = 2 * time.Second
	vipIncrementalReconcileHeartbeatResultSize = 2048
)

type VIPIncrementalReconcileConfig struct {
	Enabled                     bool
	Interval                    time.Duration
	RunTimeout                  time.Duration
	SafetyDelay                 time.Duration
	BatchSize                   int
	BatchPause                  time.Duration
	MaxBatchesPerRun            int
	Overlap                     time.Duration
	OverlapMargin               time.Duration
	PaymentFulfillmentDBTimeout time.Duration
}

func (c VIPIncrementalReconcileConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Interval <= 0 {
		return fmt.Errorf("vip_reconcile_interval must be positive")
	}
	if c.RunTimeout <= 0 {
		return fmt.Errorf("vip_reconcile_run_timeout must be positive")
	}
	if c.SafetyDelay < 0 {
		return fmt.Errorf("vip_reconcile_safety_delay must be non-negative")
	}
	if c.BatchSize < 1 || c.BatchSize > 5000 {
		return fmt.Errorf("vip_reconcile_batch_size must be between 1 and 5000")
	}
	if c.BatchPause < 0 || c.BatchPause > 10*time.Second {
		return fmt.Errorf("vip_reconcile_batch_pause must be between 0 and 10s")
	}
	if c.MaxBatchesPerRun < 0 || c.MaxBatchesPerRun > 10000 {
		return fmt.Errorf("vip_reconcile_max_batches_per_run must be between 0 and 10000")
	}
	if c.PaymentFulfillmentDBTimeout <= 0 {
		return fmt.Errorf("payment_fulfillment_db_tx_timeout must be positive")
	}
	if c.OverlapMargin < 0 {
		return fmt.Errorf("vip_reconcile_overlap_margin must be non-negative")
	}
	minimumOverlap := c.PaymentFulfillmentDBTimeout + c.OverlapMargin
	if minimumOverlap < c.PaymentFulfillmentDBTimeout {
		return fmt.Errorf("payment_fulfillment_db_tx_timeout + vip_reconcile_overlap_margin overflows")
	}
	if c.Overlap < minimumOverlap {
		return fmt.Errorf(
			"vip_reconcile_overlap must be >= payment_fulfillment_db_tx_timeout + vip_reconcile_overlap_margin",
		)
	}
	return nil
}

type VIPIncrementalReconcileCursor struct {
	CompletedAt time.Time
	OrderID     int64
}

type VIPIncrementalReconcileWatermark struct {
	Cursor         VIPIncrementalReconcileCursor
	BackfillCutoff time.Time
	UpdatedAt      time.Time
}

type VIPIncrementalReconcileBatchResult struct {
	Cursor            VIPIncrementalReconcileCursor
	Scanned           int
	Repaired          int
	BackfillRepaired  int
	ReconcileRepaired int
	EffectiveChanged  int
	ForceOffUnchanged int
	Full              bool
	ChangedUserIDs    []int64
}

type VIPIncrementalReconcileRepository interface {
	// InitializeBackfillCutoff atomically stores database time only when the
	// singleton watermark has no cutoff yet. It must never reset an existing
	// cutoff on process restart or leader change.
	InitializeBackfillCutoff(ctx context.Context) (VIPIncrementalReconcileWatermark, error)
	DatabaseNow(ctx context.Context) (time.Time, error)
	// ProcessNextBatch owns the transaction boundary and locks the watermark
	// before reading its cursor. The page must contain every qualifying payment
	// fact, including events whose user projection is already correct.
	ProcessNextBatch(
		ctx context.Context,
		scanBefore time.Time,
		limit int,
	) (VIPIncrementalReconcileBatchResult, error)
	// RepairOverlap runs in an independent transaction and must not advance the
	// main watermark.
	RepairOverlap(
		ctx context.Context,
		overlap time.Duration,
		limit int,
	) (VIPIncrementalReconcileBatchResult, error)
}

type VIPIncrementalReconcileRunResult struct {
	LeaderSkipped     bool
	MainBatches       int
	OverlapBatches    int
	BatchFull         int
	Scanned           int
	Repaired          int
	BackfillRepaired  int
	ReconcileRepaired int
	OverlapRepaired   int
	EffectiveChanged  int
	ForceOffUnchanged int
	Cursor            VIPIncrementalReconcileCursor
	WatermarkLag      time.Duration
}

type vipIncrementalReconcileHeartbeatWriter interface {
	UpsertJobHeartbeat(ctx context.Context, input *OpsUpsertJobHeartbeatInput) error
}

type VIPIncrementalReconcileService struct {
	repository           VIPIncrementalReconcileRepository
	authCacheInvalidator APIKeyAuthCacheInvalidator
	config               VIPIncrementalReconcileConfig
	lockCache            LeaderLockCache
	db                   *sql.DB
	instanceID           string
	heartbeatWriter      vipIncrementalReconcileHeartbeatWriter

	lifecycleMu sync.Mutex
	started     bool
	stopped     bool
	stopCh      chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
}

func NewVIPIncrementalReconcileService(
	repository VIPIncrementalReconcileRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	lockCache LeaderLockCache,
	db *sql.DB,
	cfg VIPIncrementalReconcileConfig,
) *VIPIncrementalReconcileService {
	return &VIPIncrementalReconcileService{
		repository:           repository,
		authCacheInvalidator: authCacheInvalidator,
		config:               cfg,
		lockCache:            lockCache,
		db:                   db,
		instanceID:           uuid.NewString(),
		stopCh:               make(chan struct{}),
	}
}

func (s *VIPIncrementalReconcileService) SetHeartbeatWriter(
	writer vipIncrementalReconcileHeartbeatWriter,
) {
	if s != nil {
		s.heartbeatWriter = writer
	}
}

func (s *VIPIncrementalReconcileService) Start() error {
	if s == nil {
		return fmt.Errorf("VIP incremental reconcile service is nil")
	}
	if err := s.config.Validate(); err != nil {
		return err
	}
	if !s.config.Enabled {
		return nil
	}
	if s.repository == nil {
		return fmt.Errorf("VIP incremental reconcile repository is not configured")
	}

	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.stopped {
		return fmt.Errorf("VIP incremental reconcile service has already stopped")
	}
	if s.started {
		return nil
	}
	s.started = true
	s.wg.Add(1)
	go s.runLoop()
	return nil
}

func (s *VIPIncrementalReconcileService) Stop() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	s.stopped = true
	s.lifecycleMu.Unlock()
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *VIPIncrementalReconcileService) runLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	s.runScheduledOnce()
	for {
		select {
		case <-ticker.C:
			s.runScheduledOnce()
		case <-s.stopCh:
			return
		}
	}
}

func (s *VIPIncrementalReconcileService) runScheduledOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), s.config.RunTimeout)
	defer cancel()

	startedAt := time.Now().UTC()
	result, err := s.runOnce(ctx)
	finishedAt := time.Now().UTC()
	duration := finishedAt.Sub(startedAt)

	resultLabel := "success"
	switch {
	case err != nil && (errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(ctx.Err(), context.DeadlineExceeded)):
		resultLabel = "timeout"
	case err != nil:
		resultLabel = "error"
	case result.LeaderSkipped:
		resultLabel = "leader_skip"
	}
	s.recordHeartbeat(startedAt, finishedAt, duration, resultLabel, result, err)

	if err != nil {
		slog.Error("VIP incremental reconcile failed",
			"duration", duration,
			"error", err,
		)
		return
	}
	if result.LeaderSkipped {
		slog.Debug("VIP incremental reconcile skipped; another instance is leader")
		return
	}
	slog.Info("VIP incremental reconcile completed",
		"duration", duration,
		"main_batches", result.MainBatches,
		"overlap_batches", result.OverlapBatches,
		"batch_full", result.BatchFull,
		"scanned", result.Scanned,
		"repaired", result.Repaired,
		"backfill_repaired", result.BackfillRepaired,
		"reconcile_repaired", result.ReconcileRepaired,
		"overlap_repaired", result.OverlapRepaired,
		"effective_changed", result.EffectiveChanged,
		"force_off_unchanged", result.ForceOffUnchanged,
		"cursor_completed_at", result.Cursor.CompletedAt,
		"cursor_order_id", result.Cursor.OrderID,
		"watermark_lag", result.WatermarkLag,
	)
}

func (s *VIPIncrementalReconcileService) recordHeartbeat(
	startedAt time.Time,
	finishedAt time.Time,
	duration time.Duration,
	resultLabel string,
	result VIPIncrementalReconcileRunResult,
	runErr error,
) {
	if s == nil || s.heartbeatWriter == nil {
		return
	}

	durationMs := duration.Milliseconds()
	input := &OpsUpsertJobHeartbeatInput{
		JobName:        vipIncrementalReconcileHeartbeatJobName,
		LastRunAt:      &startedAt,
		LastDurationMs: &durationMs,
	}
	switch resultLabel {
	case "success":
		input.LastSuccessAt = &finishedAt
		summary := vipIncrementalReconcileHeartbeatSummary(result)
		input.LastResult = &summary
	case "error", "timeout":
		input.LastErrorAt = &finishedAt
		message := resultLabel
		if runErr != nil {
			message = truncateString(runErr.Error(), vipIncrementalReconcileHeartbeatResultSize)
		}
		input.LastError = &message
	}

	heartbeatCtx, cancel := context.WithTimeout(
		context.Background(),
		vipIncrementalReconcileHeartbeatTimeout,
	)
	defer cancel()
	if err := s.heartbeatWriter.UpsertJobHeartbeat(heartbeatCtx, input); err != nil {
		slog.Warn("VIP incremental reconcile heartbeat update failed", "error", err)
	}
}

func vipIncrementalReconcileHeartbeatSummary(
	result VIPIncrementalReconcileRunResult,
) string {
	cursorCompletedAt := ""
	if !result.Cursor.CompletedAt.IsZero() {
		cursorCompletedAt = result.Cursor.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	lagSeconds := int64(result.WatermarkLag / time.Second)
	if lagSeconds < 0 {
		lagSeconds = 0
	}
	summary := struct {
		CursorCompletedAt string `json:"cursor_completed_at"`
		CursorOrderID     int64  `json:"cursor_order_id"`
		MainBatches       int    `json:"main_batches"`
		OverlapBatches    int    `json:"overlap_batches"`
		BatchFull         int    `json:"batch_full"`
		Scanned           int    `json:"scanned"`
		Repaired          int    `json:"repaired"`
		BackfillRepaired  int    `json:"backfill_repaired"`
		ReconcileRepaired int    `json:"reconcile_repaired"`
		OverlapRepaired   int    `json:"overlap_repaired"`
		EffectiveChanged  int    `json:"effective_changed"`
		ForceOffUnchanged int    `json:"force_off_unchanged"`
		WatermarkLag      int64  `json:"watermark_lag_seconds"`
	}{
		CursorCompletedAt: cursorCompletedAt,
		CursorOrderID:     result.Cursor.OrderID,
		MainBatches:       result.MainBatches,
		OverlapBatches:    result.OverlapBatches,
		BatchFull:         result.BatchFull,
		Scanned:           result.Scanned,
		Repaired:          result.Repaired,
		BackfillRepaired:  result.BackfillRepaired,
		ReconcileRepaired: result.ReconcileRepaired,
		OverlapRepaired:   result.OverlapRepaired,
		EffectiveChanged:  result.EffectiveChanged,
		ForceOffUnchanged: result.ForceOffUnchanged,
		WatermarkLag:      lagSeconds,
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return `{"result":"summary_unavailable"}`
	}
	return truncateString(string(encoded), vipIncrementalReconcileHeartbeatResultSize)
}

func (s *VIPIncrementalReconcileService) runOnce(
	ctx context.Context,
) (VIPIncrementalReconcileRunResult, error) {
	if s == nil || s.repository == nil {
		return VIPIncrementalReconcileRunResult{}, fmt.Errorf(
			"VIP incremental reconcile repository is not configured",
		)
	}
	if err := s.config.Validate(); err != nil {
		return VIPIncrementalReconcileRunResult{}, err
	}
	if !s.config.Enabled {
		return VIPIncrementalReconcileRunResult{}, nil
	}

	lockTTL := s.config.RunTimeout + time.Minute
	if lockTTL < s.config.RunTimeout {
		return VIPIncrementalReconcileRunResult{}, fmt.Errorf(
			"VIP incremental reconcile leader lock TTL overflows",
		)
	}
	release, acquired := tryAcquireSingletonLeaderLock(
		ctx,
		s.lockCache,
		s.db,
		vipIncrementalReconcileLeaderLockKey,
		s.instanceID,
		lockTTL,
	)
	if !acquired {
		return VIPIncrementalReconcileRunResult{LeaderSkipped: true}, nil
	}
	defer release()

	if _, err := s.repository.InitializeBackfillCutoff(ctx); err != nil {
		return VIPIncrementalReconcileRunResult{}, fmt.Errorf(
			"initialize VIP reconcile backfill cutoff: %w",
			err,
		)
	}
	databaseNow, err := s.repository.DatabaseNow(ctx)
	if err != nil {
		return VIPIncrementalReconcileRunResult{}, fmt.Errorf(
			"load VIP reconcile database time: %w",
			err,
		)
	}
	scanBefore := databaseNow.UTC().Add(-s.config.SafetyDelay)

	var out VIPIncrementalReconcileRunResult
	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		batch, batchErr := s.repository.ProcessNextBatch(
			ctx,
			scanBefore,
			s.config.BatchSize,
		)
		if batchErr != nil {
			return out, fmt.Errorf("process VIP reconcile batch: %w", batchErr)
		}
		out.MainBatches++
		accumulateVIPIncrementalReconcileBatch(&out, batch)
		if batch.Full {
			out.BatchFull++
		}
		if !batch.Cursor.CompletedAt.IsZero() {
			out.WatermarkLag = scanBefore.Sub(batch.Cursor.CompletedAt)
			if out.WatermarkLag < 0 {
				out.WatermarkLag = 0
			}
		}
		s.invalidateVIPIncrementalReconcileUsers(ctx, batch.ChangedUserIDs)
		if !batch.Full {
			break
		}
		if s.config.MaxBatchesPerRun > 0 &&
			out.MainBatches >= s.config.MaxBatchesPerRun {
			break
		}
		if err := s.waitForNextBatch(ctx); err != nil {
			return out, err
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		batch, batchErr := s.repository.RepairOverlap(
			ctx,
			s.config.Overlap,
			s.config.BatchSize,
		)
		if batchErr != nil {
			return out, fmt.Errorf("repair VIP reconcile overlap: %w", batchErr)
		}
		out.OverlapBatches++
		accumulateVIPIncrementalReconcileBatch(&out, batch)
		out.OverlapRepaired += batch.Repaired
		if batch.Full {
			out.BatchFull++
		}
		s.invalidateVIPIncrementalReconcileUsers(ctx, batch.ChangedUserIDs)
		if !batch.Full {
			break
		}
		if s.config.MaxBatchesPerRun > 0 &&
			out.OverlapBatches >= s.config.MaxBatchesPerRun {
			break
		}
		if err := s.waitForNextBatch(ctx); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *VIPIncrementalReconcileService) waitForNextBatch(ctx context.Context) error {
	if s == nil || s.config.BatchPause <= 0 {
		return nil
	}
	timer := time.NewTimer(s.config.BatchPause)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.stopCh:
		return context.Canceled
	}
}

func accumulateVIPIncrementalReconcileBatch(
	run *VIPIncrementalReconcileRunResult,
	batch VIPIncrementalReconcileBatchResult,
) {
	run.Scanned += batch.Scanned
	run.Repaired += batch.Repaired
	run.BackfillRepaired += batch.BackfillRepaired
	run.ReconcileRepaired += batch.ReconcileRepaired
	run.EffectiveChanged += batch.EffectiveChanged
	run.ForceOffUnchanged += batch.ForceOffUnchanged
	if !batch.Cursor.CompletedAt.IsZero() {
		run.Cursor = batch.Cursor
	}
}

func (s *VIPIncrementalReconcileService) invalidateVIPIncrementalReconcileUsers(
	ctx context.Context,
	userIDs []int64,
) {
	if s.authCacheInvalidator == nil {
		return
	}
	for _, userID := range userIDs {
		if userID > 0 {
			s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
		}
	}
}
