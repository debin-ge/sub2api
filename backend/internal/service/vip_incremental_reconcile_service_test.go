package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type vipIncrementalReconcileRepositoryStub struct {
	mu sync.Mutex

	now             time.Time
	initializeCalls int
	processCalls    []time.Time
	processResults  []VIPIncrementalReconcileBatchResult
	overlapCalls    []time.Duration
	overlapResults  []VIPIncrementalReconcileBatchResult
	processErr      error
}

func (s *vipIncrementalReconcileRepositoryStub) InitializeBackfillCutoff(
	context.Context,
) (VIPIncrementalReconcileWatermark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initializeCalls++
	return VIPIncrementalReconcileWatermark{
		BackfillCutoff: s.now,
	}, nil
}

func (s *vipIncrementalReconcileRepositoryStub) DatabaseNow(
	context.Context,
) (time.Time, error) {
	return s.now, nil
}

func (s *vipIncrementalReconcileRepositoryStub) ProcessNextBatch(
	_ context.Context,
	scanBefore time.Time,
	_ int,
) (VIPIncrementalReconcileBatchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processCalls = append(s.processCalls, scanBefore)
	if s.processErr != nil {
		return VIPIncrementalReconcileBatchResult{}, s.processErr
	}
	result := s.processResults[0]
	s.processResults = s.processResults[1:]
	return result, nil
}

func (s *vipIncrementalReconcileRepositoryStub) RepairOverlap(
	_ context.Context,
	overlap time.Duration,
	_ int,
) (VIPIncrementalReconcileBatchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overlapCalls = append(s.overlapCalls, overlap)
	result := s.overlapResults[0]
	s.overlapResults = s.overlapResults[1:]
	return result, nil
}

type vipIncrementalLeaderLockStub struct {
	acquired bool
	released int
}

func (s *vipIncrementalLeaderLockStub) TryAcquireLeaderLock(
	context.Context,
	string,
	string,
	time.Duration,
) (bool, error) {
	return s.acquired, nil
}

func (s *vipIncrementalLeaderLockStub) ReleaseLeaderLock(
	context.Context,
	string,
	string,
) error {
	s.released++
	return nil
}

type vipIncrementalInvalidatorStub struct {
	userIDs []int64
}

type vipIncrementalHeartbeatStub struct {
	mu     sync.Mutex
	inputs []OpsUpsertJobHeartbeatInput
}

func (s *vipIncrementalHeartbeatStub) UpsertJobHeartbeat(
	_ context.Context,
	input *OpsUpsertJobHeartbeatInput,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input != nil {
		s.inputs = append(s.inputs, *input)
	}
	return nil
}

func (s *vipIncrementalHeartbeatStub) last() OpsUpsertJobHeartbeatInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.inputs) == 0 {
		return OpsUpsertJobHeartbeatInput{}
	}
	return s.inputs[len(s.inputs)-1]
}

func (*vipIncrementalInvalidatorStub) InvalidateAuthCacheByKey(
	context.Context,
	string,
) {
}

func (s *vipIncrementalInvalidatorStub) InvalidateAuthCacheByUserID(
	_ context.Context,
	userID int64,
) {
	s.userIDs = append(s.userIDs, userID)
}

func (*vipIncrementalInvalidatorStub) InvalidateAuthCacheByGroupID(
	context.Context,
	int64,
) {
}

func validVIPIncrementalReconcileTestConfig() VIPIncrementalReconcileConfig {
	return VIPIncrementalReconcileConfig{
		Enabled:                     true,
		Interval:                    time.Minute,
		RunTimeout:                  30 * time.Second,
		SafetyDelay:                 5 * time.Second,
		BatchSize:                   2,
		Overlap:                     5 * time.Minute,
		OverlapMargin:               time.Minute,
		PaymentFulfillmentDBTimeout: 2 * time.Minute,
	}
}

func TestVIPIncrementalReconcileConfigRequiresSafeOverlap(t *testing.T) {
	cfg := validVIPIncrementalReconcileTestConfig()
	cfg.Overlap = cfg.PaymentFulfillmentDBTimeout + cfg.OverlapMargin - time.Nanosecond

	err := cfg.Validate()

	require.EqualError(
		t,
		err,
		"vip_reconcile_overlap must be >= payment_fulfillment_db_tx_timeout + vip_reconcile_overlap_margin",
	)
}

func TestVIPIncrementalReconcileRunUsesDatabaseBoundAndDrainsBatches(t *testing.T) {
	databaseNow := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	finalCursor := VIPIncrementalReconcileCursor{
		CompletedAt: databaseNow.Add(-time.Minute),
		OrderID:     22,
	}
	repo := &vipIncrementalReconcileRepositoryStub{
		now: databaseNow,
		processResults: []VIPIncrementalReconcileBatchResult{
			{
				Scanned:          2,
				Repaired:         1,
				BackfillRepaired: 1,
				EffectiveChanged: 1,
				Full:             true,
				ChangedUserIDs:   []int64{7},
			},
			{
				Cursor:            finalCursor,
				Scanned:           1,
				Repaired:          1,
				ReconcileRepaired: 1,
				ForceOffUnchanged: 1,
				Full:              false,
			},
		},
		overlapResults: []VIPIncrementalReconcileBatchResult{
			{Cursor: finalCursor, Full: false},
		},
	}
	lock := &vipIncrementalLeaderLockStub{acquired: true}
	invalidator := &vipIncrementalInvalidatorStub{}
	svc := NewVIPIncrementalReconcileService(
		repo,
		invalidator,
		lock,
		(*sql.DB)(nil),
		validVIPIncrementalReconcileTestConfig(),
	)

	result, err := svc.runOnce(context.Background())

	require.NoError(t, err)
	require.False(t, result.LeaderSkipped)
	require.Equal(t, 2, result.MainBatches)
	require.Equal(t, 1, result.OverlapBatches)
	require.Equal(t, 3, result.Scanned)
	require.Equal(t, 2, result.Repaired)
	require.Equal(t, 1, result.BackfillRepaired)
	require.Equal(t, 1, result.ReconcileRepaired)
	require.Equal(t, 1, result.EffectiveChanged)
	require.Equal(t, 1, result.ForceOffUnchanged)
	require.Equal(t, finalCursor, result.Cursor)
	require.Equal(t, []int64{7}, invalidator.userIDs)
	require.Equal(t, 1, repo.initializeCalls)
	require.Equal(t, []time.Time{
		databaseNow.Add(-5 * time.Second),
		databaseNow.Add(-5 * time.Second),
	}, repo.processCalls)
	require.Equal(t, []time.Duration{5 * time.Minute}, repo.overlapCalls)
	require.Equal(t, 1, lock.released)
}

func TestVIPIncrementalReconcileRunBoundsMainAndOverlapBatches(t *testing.T) {
	repo := &vipIncrementalReconcileRepositoryStub{
		now: time.Now().UTC(),
		processResults: []VIPIncrementalReconcileBatchResult{
			{Full: true},
			{Full: true},
		},
		overlapResults: []VIPIncrementalReconcileBatchResult{
			{Full: true},
			{Full: true},
		},
	}
	cfg := validVIPIncrementalReconcileTestConfig()
	cfg.MaxBatchesPerRun = 1
	cfg.BatchPause = 0
	svc := NewVIPIncrementalReconcileService(
		repo,
		nil,
		&vipIncrementalLeaderLockStub{acquired: true},
		nil,
		cfg,
	)

	result, err := svc.runOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, result.MainBatches)
	require.Equal(t, 1, result.OverlapBatches)
	require.Len(t, repo.processCalls, 1)
	require.Len(t, repo.overlapCalls, 1)
}

func TestVIPIncrementalReconcileLeaderSkipDoesNotTouchWatermark(t *testing.T) {
	repo := &vipIncrementalReconcileRepositoryStub{
		now: time.Now(),
	}
	lock := &vipIncrementalLeaderLockStub{acquired: false}
	svc := NewVIPIncrementalReconcileService(
		repo,
		nil,
		lock,
		nil,
		validVIPIncrementalReconcileTestConfig(),
	)

	result, err := svc.runOnce(context.Background())

	require.NoError(t, err)
	require.True(t, result.LeaderSkipped)
	require.Zero(t, repo.initializeCalls)
	require.Empty(t, repo.processCalls)
	require.Zero(t, lock.released)
}

func TestVIPIncrementalReconcileDrainsFullOverlapPages(t *testing.T) {
	repo := &vipIncrementalReconcileRepositoryStub{
		now:            time.Now().UTC(),
		processResults: []VIPIncrementalReconcileBatchResult{{Full: false}},
		overlapResults: []VIPIncrementalReconcileBatchResult{
			{Scanned: 2, Full: true},
			{Scanned: 1, Full: false},
		},
	}
	lock := &vipIncrementalLeaderLockStub{acquired: true}
	svc := NewVIPIncrementalReconcileService(
		repo,
		nil,
		lock,
		nil,
		validVIPIncrementalReconcileTestConfig(),
	)

	result, err := svc.runOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, result.MainBatches)
	require.Equal(t, 2, result.OverlapBatches)
	require.Equal(t, 3, result.Scanned)
	require.Len(t, repo.overlapCalls, 2)
	require.Equal(t, 1, lock.released)
}

func TestVIPIncrementalReconcileBatchFailureStopsBeforeOverlap(t *testing.T) {
	repo := &vipIncrementalReconcileRepositoryStub{
		now:        time.Now(),
		processErr: errors.New("transaction rolled back"),
	}
	lock := &vipIncrementalLeaderLockStub{acquired: true}
	svc := NewVIPIncrementalReconcileService(
		repo,
		nil,
		lock,
		nil,
		validVIPIncrementalReconcileTestConfig(),
	)

	_, err := svc.runOnce(context.Background())

	require.ErrorContains(t, err, "process VIP reconcile batch")
	require.Empty(t, repo.overlapCalls)
	require.Equal(t, 1, lock.released)
}

func TestVIPIncrementalReconcileAggregatesBatchResults(t *testing.T) {
	databaseNow := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	scanBefore := databaseNow.Add(-5 * time.Second)
	repo := &vipIncrementalReconcileRepositoryStub{
		now: databaseNow,
		processResults: []VIPIncrementalReconcileBatchResult{
			{
				Cursor:            VIPIncrementalReconcileCursor{CompletedAt: scanBefore.Add(-time.Second), OrderID: 2},
				Repaired:          2,
				BackfillRepaired:  1,
				ReconcileRepaired: 1,
				Full:              true,
			},
			{
				Cursor: VIPIncrementalReconcileCursor{CompletedAt: scanBefore, OrderID: 0},
			},
		},
		overlapResults: []VIPIncrementalReconcileBatchResult{{
			Repaired: 3,
		}},
	}
	svc := NewVIPIncrementalReconcileService(
		repo,
		nil,
		&vipIncrementalLeaderLockStub{acquired: true},
		nil,
		validVIPIncrementalReconcileTestConfig(),
	)

	result, err := svc.runOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 3, result.OverlapRepaired)
	require.Equal(t, 1, result.BatchFull)
	require.Zero(t, result.WatermarkLag)
}

func TestVIPIncrementalReconcileScheduledRunRecordsSuccessHeartbeat(t *testing.T) {
	databaseNow := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	scanBefore := databaseNow.Add(-5 * time.Second)
	repo := &vipIncrementalReconcileRepositoryStub{
		now: databaseNow,
		processResults: []VIPIncrementalReconcileBatchResult{{
			Cursor:            VIPIncrementalReconcileCursor{CompletedAt: scanBefore, OrderID: 42},
			Scanned:           2,
			Repaired:          1,
			BackfillRepaired:  1,
			EffectiveChanged:  1,
			ChangedUserIDs:    []int64{7},
			ForceOffUnchanged: 0,
		}},
		overlapResults: []VIPIncrementalReconcileBatchResult{{}},
	}
	heartbeat := &vipIncrementalHeartbeatStub{}
	svc := NewVIPIncrementalReconcileService(
		repo,
		nil,
		&vipIncrementalLeaderLockStub{acquired: true},
		nil,
		validVIPIncrementalReconcileTestConfig(),
	)
	svc.SetHeartbeatWriter(heartbeat)

	svc.runScheduledOnce()

	input := heartbeat.last()
	require.Equal(t, vipIncrementalReconcileHeartbeatJobName, input.JobName)
	require.NotNil(t, input.LastRunAt)
	require.NotNil(t, input.LastSuccessAt)
	require.Nil(t, input.LastErrorAt)
	require.Nil(t, input.LastError)
	require.NotNil(t, input.LastDurationMs)
	require.NotNil(t, input.LastResult)

	var summary map[string]any
	require.NoError(t, json.Unmarshal([]byte(*input.LastResult), &summary))
	require.Equal(t, float64(42), summary["cursor_order_id"])
	require.Equal(t, float64(1), summary["repaired"])
	require.Equal(t, float64(0), summary["watermark_lag_seconds"])
}

func TestVIPIncrementalReconcileScheduledRunRecordsTimeoutHeartbeat(t *testing.T) {
	repo := &vipIncrementalReconcileRepositoryStub{
		now:        time.Now().UTC(),
		processErr: context.DeadlineExceeded,
	}
	heartbeat := &vipIncrementalHeartbeatStub{}
	svc := NewVIPIncrementalReconcileService(
		repo,
		nil,
		&vipIncrementalLeaderLockStub{acquired: true},
		nil,
		validVIPIncrementalReconcileTestConfig(),
	)
	svc.SetHeartbeatWriter(heartbeat)

	svc.runScheduledOnce()

	input := heartbeat.last()
	require.NotNil(t, input.LastRunAt)
	require.Nil(t, input.LastSuccessAt)
	require.NotNil(t, input.LastErrorAt)
	require.NotNil(t, input.LastError)
	require.Contains(t, *input.LastError, "deadline exceeded")
}

func TestVIPIncrementalReconcileLeaderSkipRecordsHeartbeat(t *testing.T) {
	heartbeat := &vipIncrementalHeartbeatStub{}
	svc := NewVIPIncrementalReconcileService(
		&vipIncrementalReconcileRepositoryStub{now: time.Now().UTC()},
		nil,
		&vipIncrementalLeaderLockStub{acquired: false},
		nil,
		validVIPIncrementalReconcileTestConfig(),
	)
	svc.SetHeartbeatWriter(heartbeat)

	svc.runScheduledOnce()

	input := heartbeat.last()
	require.NotNil(t, input.LastRunAt)
	require.Nil(t, input.LastSuccessAt)
	require.Nil(t, input.LastErrorAt)
	require.Nil(t, input.LastError)
}

type vipIncrementalLifecycleRepositoryStub struct {
	ran  chan struct{}
	once sync.Once
}

func (s *vipIncrementalLifecycleRepositoryStub) InitializeBackfillCutoff(
	context.Context,
) (VIPIncrementalReconcileWatermark, error) {
	now := time.Now().UTC()
	return VIPIncrementalReconcileWatermark{
		BackfillCutoff: now,
	}, nil
}

func (s *vipIncrementalLifecycleRepositoryStub) DatabaseNow(
	context.Context,
) (time.Time, error) {
	return time.Now().UTC(), nil
}

func (s *vipIncrementalLifecycleRepositoryStub) ProcessNextBatch(
	context.Context,
	time.Time,
	int,
) (VIPIncrementalReconcileBatchResult, error) {
	s.once.Do(func() { close(s.ran) })
	return VIPIncrementalReconcileBatchResult{}, nil
}

func (*vipIncrementalLifecycleRepositoryStub) RepairOverlap(
	context.Context,
	time.Duration,
	int,
) (VIPIncrementalReconcileBatchResult, error) {
	return VIPIncrementalReconcileBatchResult{}, nil
}

func TestVIPIncrementalReconcileStartRunsImmediatelyAndStopWaits(t *testing.T) {
	repo := &vipIncrementalLifecycleRepositoryStub{ran: make(chan struct{})}
	cfg := validVIPIncrementalReconcileTestConfig()
	cfg.Interval = time.Hour
	svc := NewVIPIncrementalReconcileService(repo, nil, nil, nil, cfg)

	require.NoError(t, svc.Start())
	select {
	case <-repo.ran:
	case <-time.After(2 * time.Second):
		t.Fatal("VIP incremental reconcile did not run immediately")
	}

	stopped := make(chan struct{})
	go func() {
		svc.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("VIP incremental reconcile did not stop")
	}
	require.ErrorContains(t, svc.Start(), "already stopped")
}
