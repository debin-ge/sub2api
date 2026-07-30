package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultVIPReconcilePreviewLimit  = 50
	maxVIPReconcilePreviewLimit      = 200
	defaultVIPReconcileJobBatchSize  = 200
	defaultVIPReconcileJobRunTimeout = 30 * time.Minute
	defaultVIPReconcilePollInterval  = 5 * time.Second
	vipReconcileLeaderLockKey        = "vip:full-reconcile:leader"
	vipReconcileLeaderLockTTLBuffer  = time.Minute
	vipReconcileFailureWriteTimeout  = 5 * time.Second
	vipReconcileFailureRetryDelay    = time.Second
)

type VIPReconcilePreviewStats struct {
	EligibilityRepair int64 `json:"eligibility_repair"`
	EffectiveChange   int64 `json:"effective_change"`
	ForceOffUnchanged int64 `json:"force_off_unchanged"`
	InvalidOrder      int64 `json:"invalid_order"`
	DeletedUser       int64 `json:"deleted_user"`
}

type VIPReconcilePreviewItem struct {
	Category            string     `json:"category"`
	UserID              *int64     `json:"user_id"`
	OrderID             int64      `json:"order_id"`
	CompletedAt         *time.Time `json:"completed_at"`
	CurrentVIPMode      VIPMode    `json:"current_vip_mode"`
	CurrentIsVIP        bool       `json:"current_is_vip"`
	WillEffectiveChange bool       `json:"will_effective_change"`
}

type VIPReconcilePreview struct {
	AsOf       time.Time                 `json:"as_of"`
	Total      int64                     `json:"total"`
	Stats      VIPReconcilePreviewStats  `json:"stats"`
	Items      []VIPReconcilePreviewItem `json:"items"`
	NextCursor string                    `json:"next_cursor"`
}

type VIPReconcileJob struct {
	ID                  int64      `json:"id"`
	RequestID           string     `json:"request_id"`
	ActorUserID         int64      `json:"actor_user_id"`
	ActorSnapshot       string     `json:"actor_snapshot"`
	Reason              string     `json:"reason"`
	Status              string     `json:"status"`
	AsOf                time.Time  `json:"as_of"`
	CursorCompletedAt   time.Time  `json:"cursor_completed_at"`
	CursorOrderID       int64      `json:"cursor_order_id"`
	Scanned             int64      `json:"scanned"`
	EligibilityRepaired int64      `json:"eligibility_repaired"`
	EffectiveChanged    int64      `json:"effective_changed"`
	ForceOffUnchanged   int64      `json:"force_off_unchanged"`
	AlreadyCorrect      int64      `json:"already_correct"`
	Deleted             int64      `json:"deleted"`
	InvalidOrder        int64      `json:"invalid_order"`
	Failed              int64      `json:"failed"`
	Attempts            int        `json:"attempts"`
	LastError           string     `json:"last_error"`
	StartedAt           *time.Time `json:"started_at"`
	FinishedAt          *time.Time `json:"finished_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type VIPReconcileBatchResult struct {
	Done           bool
	Repaired       int
	ChangedUserIDs []int64
}

type VIPReconcileRepository interface {
	DatabaseNow(ctx context.Context) (time.Time, error)
	Preview(
		ctx context.Context,
		asOf time.Time,
		afterOrderID int64,
		limit int,
	) (*VIPReconcilePreview, error)
	CreateOrResumeJob(
		ctx context.Context,
		requestID string,
		actorUserID int64,
		reason string,
	) (*VIPReconcileJob, error)
	GetActiveJob(ctx context.Context) (*VIPReconcileJob, error)
	GetJob(ctx context.Context, jobID int64) (*VIPReconcileJob, error)
	ProcessJobBatch(
		ctx context.Context,
		jobID int64,
		batchSize int,
	) (VIPReconcileBatchResult, error)
	RequeueJob(ctx context.Context, jobID int64, reason string) error
	MarkJobFailed(ctx context.Context, jobID int64, failure string) error
}

type vipReconcileCursor struct {
	AsOf         time.Time `json:"as_of"`
	AfterOrderID int64     `json:"after_order_id"`
}

type VIPReconcileService struct {
	repository           VIPReconcileRepository
	authCacheInvalidator APIKeyAuthCacheInvalidator
	jobBatchSize         int
	jobRunTimeout        time.Duration
	pollInterval         time.Duration
	lockCache            LeaderLockCache
	db                   *sql.DB
	instanceID           string

	running sync.Map

	lifecycleMu     sync.Mutex
	started         bool
	stopped         bool
	wakeCh          chan struct{}
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	wg              sync.WaitGroup
}

func NewVIPReconcileService(
	repository VIPReconcileRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
) *VIPReconcileService {
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	return &VIPReconcileService{
		repository:           repository,
		authCacheInvalidator: authCacheInvalidator,
		jobBatchSize:         defaultVIPReconcileJobBatchSize,
		jobRunTimeout:        defaultVIPReconcileJobRunTimeout,
		pollInterval:         defaultVIPReconcilePollInterval,
		instanceID:           uuid.NewString(),
		wakeCh:               make(chan struct{}, 1),
		lifecycleCtx:         lifecycleCtx,
		lifecycleCancel:      lifecycleCancel,
	}
}

func (s *VIPReconcileService) ConfigureRuntime(
	lockCache LeaderLockCache,
	db *sql.DB,
	runTimeout time.Duration,
) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
	if runTimeout > 0 {
		s.jobRunTimeout = runTimeout
	}
}

// Start launches a durable-job supervisor on every application instance. Each
// poll competes for a cross-instance leader lock before processing, so a job
// created on one instance can be taken over by another after a crash.
func (s *VIPReconcileService) Start() error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("VIP reconcile repository is not configured")
	}
	if s.jobRunTimeout <= 0 {
		return fmt.Errorf("vip_reconcile_job_run_timeout must be positive")
	}
	if s.pollInterval <= 0 {
		return fmt.Errorf("VIP reconcile poll interval must be positive")
	}

	s.lifecycleMu.Lock()
	if s.stopped {
		s.lifecycleMu.Unlock()
		return fmt.Errorf("VIP reconcile service has already stopped")
	}
	if s.started {
		s.lifecycleMu.Unlock()
		return nil
	}
	s.started = true
	s.lifecycleMu.Unlock()

	startupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.ResumeActiveJob(startupCtx); err != nil {
		s.lifecycleMu.Lock()
		s.started = false
		s.lifecycleMu.Unlock()
		return err
	}

	s.wg.Add(1)
	go s.runSupervisor()
	return nil
}

func (s *VIPReconcileService) Stop() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	if s.stopped {
		s.lifecycleMu.Unlock()
		return
	}
	s.stopped = true
	s.lifecycleMu.Unlock()
	s.lifecycleCancel()
	s.wg.Wait()
}

func (s *VIPReconcileService) runSupervisor() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-s.wakeCh:
		case <-s.lifecycleCtx.Done():
			return
		}
		ctx, cancel := context.WithTimeout(s.lifecycleCtx, 10*time.Second)
		err := s.ResumeActiveJob(ctx)
		cancel()
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("VIP reconcile supervisor poll failed", "error", err)
		}
	}
}

// ResumeActiveJob reconnects this process to the single durable queued/running
// job, if one exists. startJob acquires the leader lease before any batch work.
func (s *VIPReconcileService) ResumeActiveJob(ctx context.Context) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("VIP reconcile repository is not configured")
	}
	job, err := s.repository.GetActiveJob(ctx)
	if err != nil {
		return fmt.Errorf("load active VIP reconcile job: %w", err)
	}
	if job != nil {
		s.startJob(job.ID)
	}
	return nil
}

func (s *VIPReconcileService) Preview(
	ctx context.Context,
	cursor string,
	limit int,
) (*VIPReconcilePreview, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("VIP reconcile repository is not configured")
	}
	if limit <= 0 {
		limit = defaultVIPReconcilePreviewLimit
	}
	if limit > maxVIPReconcilePreviewLimit {
		limit = maxVIPReconcilePreviewLimit
	}

	state, err := decodeVIPReconcileCursor(cursor)
	if err != nil {
		return nil, err
	}
	if state.AsOf.IsZero() {
		state.AsOf, err = s.repository.DatabaseNow(ctx)
		if err != nil {
			return nil, fmt.Errorf("load database time: %w", err)
		}
	}
	// Fetch one extra immutable order-id key to determine whether another page
	// exists. OFFSET would skip candidates if L1 repairs earlier rows between
	// preview requests.
	preview, err := s.repository.Preview(
		ctx,
		state.AsOf.UTC(),
		state.AfterOrderID,
		limit+1,
	)
	if err != nil {
		return nil, err
	}
	if preview == nil {
		return nil, fmt.Errorf("VIP reconcile preview is unavailable")
	}
	preview.AsOf = state.AsOf.UTC()
	hasMore := len(preview.Items) > limit
	if hasMore {
		preview.Items = preview.Items[:limit]
	}
	if hasMore && len(preview.Items) > 0 {
		preview.NextCursor, err = encodeVIPReconcileCursor(vipReconcileCursor{
			AsOf:         state.AsOf.UTC(),
			AfterOrderID: preview.Items[len(preview.Items)-1].OrderID,
		})
		if err != nil {
			return nil, err
		}
	}
	return preview, nil
}

func (s *VIPReconcileService) CreateJob(
	ctx context.Context,
	requestID string,
	actorUserID int64,
	reason string,
) (*VIPReconcileJob, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("VIP reconcile repository is not configured")
	}
	requestID = strings.TrimSpace(requestID)
	reason = strings.TrimSpace(reason)
	if requestID == "" {
		return nil, fmt.Errorf("request ID is required")
	}
	if len(requestID) > 128 {
		return nil, fmt.Errorf("request ID is too long")
	}
	if actorUserID <= 0 {
		return nil, fmt.Errorf("actor user ID must be positive")
	}
	if reason == "" {
		return nil, fmt.Errorf("VIP reconcile reason is required")
	}
	job, err := s.repository.CreateOrResumeJob(ctx, requestID, actorUserID, reason)
	if err != nil {
		return nil, err
	}
	s.wakeSupervisor()
	return job, nil
}

func (s *VIPReconcileService) GetJob(ctx context.Context, jobID int64) (*VIPReconcileJob, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("VIP reconcile repository is not configured")
	}
	if jobID <= 0 {
		return nil, fmt.Errorf("job ID must be positive")
	}
	return s.repository.GetJob(ctx, jobID)
}

func (s *VIPReconcileService) startJob(jobID int64) {
	if jobID <= 0 {
		return
	}
	s.lifecycleMu.Lock()
	stopped := s.stopped
	s.lifecycleMu.Unlock()
	if stopped {
		return
	}
	if _, loaded := s.running.LoadOrStore(jobID, struct{}{}); loaded {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.running.Delete(jobID)
		ctx, cancel := context.WithTimeout(s.lifecycleCtx, s.jobRunTimeout)
		defer cancel()
		lockTTL := s.jobRunTimeout + vipReconcileLeaderLockTTLBuffer
		release, acquired := tryAcquireSingletonLeaderLock(
			ctx,
			s.lockCache,
			s.db,
			vipReconcileLeaderLockKey,
			s.instanceID,
			lockTTL,
		)
		if !acquired {
			return
		}
		defer release()

		for {
			result, err := s.repository.ProcessJobBatch(ctx, jobID, s.jobBatchSize)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) ||
					errors.Is(ctx.Err(), context.DeadlineExceeded) {
					s.retryJobStateWrite(func(writeCtx context.Context) error {
						return s.repository.RequeueJob(
							writeCtx,
							jobID,
							"worker run timeout; queued for automatic resume",
						)
					})
					return
				}
				if errors.Is(ctx.Err(), context.Canceled) {
					s.retryJobStateWrite(func(writeCtx context.Context) error {
						return s.repository.RequeueJob(
							writeCtx,
							jobID,
							"worker stopped; queued for automatic resume",
						)
					})
					return
				}
				if s.markJobFailed(jobID, err.Error()) == nil {
					return
				}
				// If the terminal status write failed (for example during a
				// transient database outage), keep supervising the durable
				// active job instead of abandoning it in running forever.
				time.Sleep(vipReconcileFailureRetryDelay)
				continue
			}
			for _, userID := range result.ChangedUserIDs {
				if s.authCacheInvalidator != nil {
					s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
				}
			}
			if result.Done {
				return
			}
		}
	}()
}

func (s *VIPReconcileService) wakeSupervisor() {
	if s == nil {
		return
	}
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

func (s *VIPReconcileService) retryJobStateWrite(write func(context.Context) error) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), vipReconcileFailureWriteTimeout)
		err := write(ctx)
		cancel()
		if err == nil {
			return
		}
		select {
		case <-time.After(vipReconcileFailureRetryDelay):
		case <-s.lifecycleCtx.Done():
			return
		}
	}
}

func (s *VIPReconcileService) markJobFailed(jobID int64, failure string) error {
	ctx, cancel := context.WithTimeout(context.Background(), vipReconcileFailureWriteTimeout)
	defer cancel()
	return s.repository.MarkJobFailed(ctx, jobID, failure)
}

func decodeVIPReconcileCursor(raw string) (vipReconcileCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return vipReconcileCursor{}, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return vipReconcileCursor{}, fmt.Errorf("invalid VIP reconcile cursor")
	}
	var cursor vipReconcileCursor
	if err := json.Unmarshal(payload, &cursor); err != nil ||
		cursor.AsOf.IsZero() ||
		cursor.AfterOrderID < 0 {
		return vipReconcileCursor{}, fmt.Errorf("invalid VIP reconcile cursor")
	}
	return cursor, nil
}

func encodeVIPReconcileCursor(cursor vipReconcileCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}
