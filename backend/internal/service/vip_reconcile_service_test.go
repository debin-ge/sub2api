package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type vipReconcileRepositoryStub struct {
	mu sync.Mutex

	now                   time.Time
	nowCalls              int
	previewAfterIDs       []int64
	previewLimits         []int
	previewAsOf           []time.Time
	previewItems          []VIPReconcilePreviewItem
	previewTotal          int64
	createJob             *VIPReconcileJob
	createErr             error
	activeJob             *VIPReconcileJob
	activeErr             error
	processResults        []VIPReconcileBatchResult
	processErr            error
	processWaitForContext bool
	processCalled         chan struct{}
	requeueReason         string
	requeueCalls          int
	markFailed            string
	markFailedCalls       int
	markFailedDeadline    time.Time
	markFailedErrors      []error
}

type vipReconcileInvalidatorStub struct {
	mu      sync.Mutex
	userIDs []int64
}

type vipReconcileLeaderLockStub struct {
	mu       sync.Mutex
	acquired bool
	calls    int
	ttl      time.Duration
}

func (s *vipReconcileLeaderLockStub) TryAcquireLeaderLock(
	_ context.Context,
	_ string,
	_ string,
	ttl time.Duration,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.ttl = ttl
	return s.acquired, nil
}

func (*vipReconcileLeaderLockStub) ReleaseLeaderLock(
	context.Context,
	string,
	string,
) error {
	return nil
}

func (*vipReconcileInvalidatorStub) InvalidateAuthCacheByKey(context.Context, string) {}

func (s *vipReconcileInvalidatorStub) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userIDs = append(s.userIDs, userID)
}

func (*vipReconcileInvalidatorStub) InvalidateAuthCacheByGroupID(context.Context, int64) {}

func (s *vipReconcileInvalidatorStub) IDs() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.userIDs...)
}

func (r *vipReconcileRepositoryStub) DatabaseNow(context.Context) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nowCalls++
	return r.now, nil
}

func (r *vipReconcileRepositoryStub) Preview(
	_ context.Context,
	asOf time.Time,
	afterOrderID int64,
	limit int,
) (*VIPReconcilePreview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.previewAfterIDs = append(r.previewAfterIDs, afterOrderID)
	r.previewLimits = append(r.previewLimits, limit)
	r.previewAsOf = append(r.previewAsOf, asOf)
	items := make([]VIPReconcilePreviewItem, 0, limit)
	for _, item := range r.previewItems {
		if item.OrderID <= afterOrderID {
			continue
		}
		items = append(items, item)
		if len(items) == limit {
			break
		}
	}
	return &VIPReconcilePreview{
		Total: r.previewTotal,
		Items: items,
	}, nil
}

func (r *vipReconcileRepositoryStub) CreateOrResumeJob(
	context.Context,
	string,
	int64,
	string,
) (*VIPReconcileJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return nil, r.createErr
	}
	copy := *r.createJob
	r.activeJob = &copy
	return &copy, nil
}

func (r *vipReconcileRepositoryStub) GetActiveJob(context.Context) (*VIPReconcileJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeErr != nil {
		return nil, r.activeErr
	}
	if r.activeJob == nil {
		return nil, nil
	}
	copy := *r.activeJob
	return &copy, nil
}

func (r *vipReconcileRepositoryStub) GetJob(context.Context, int64) (*VIPReconcileJob, error) {
	copy := *r.createJob
	return &copy, nil
}

func (r *vipReconcileRepositoryStub) ProcessJobBatch(
	ctx context.Context,
	_ int64,
	_ int,
) (VIPReconcileBatchResult, error) {
	r.mu.Lock()
	select {
	case r.processCalled <- struct{}{}:
	default:
	}
	if r.processWaitForContext {
		r.mu.Unlock()
		<-ctx.Done()
		return VIPReconcileBatchResult{}, ctx.Err()
	}
	defer r.mu.Unlock()
	if r.processErr != nil {
		return VIPReconcileBatchResult{}, r.processErr
	}
	if len(r.processResults) == 0 {
		r.activeJob = nil
		return VIPReconcileBatchResult{Done: true}, nil
	}
	result := r.processResults[0]
	r.processResults = r.processResults[1:]
	if result.Done {
		r.activeJob = nil
	}
	return result, nil
}

func (r *vipReconcileRepositoryStub) RequeueJob(_ context.Context, _ int64, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requeueReason = reason
	r.requeueCalls++
	return nil
}

func (r *vipReconcileRepositoryStub) MarkJobFailed(ctx context.Context, _ int64, failure string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markFailed = failure
	r.markFailedCalls++
	if deadline, ok := ctx.Deadline(); ok {
		r.markFailedDeadline = deadline
	}
	if len(r.markFailedErrors) > 0 {
		err := r.markFailedErrors[0]
		r.markFailedErrors = r.markFailedErrors[1:]
		return err
	}
	return nil
}

func TestVIPReconcilePreviewCursorPinsDatabaseAsOf(t *testing.T) {
	asOf := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	repo := &vipReconcileRepositoryStub{
		now: asOf,
		previewItems: []VIPReconcilePreviewItem{
			{OrderID: 1},
			{OrderID: 2},
			{OrderID: 3},
			{OrderID: 4},
		},
		previewTotal: 4,
	}
	svc := NewVIPReconcileService(repo, nil)

	first, err := svc.Preview(context.Background(), "", 2)
	require.NoError(t, err)
	require.Equal(t, asOf, first.AsOf)
	require.Len(t, first.Items, 2)
	require.NotEmpty(t, first.NextCursor)

	// Simulate L1 repairing the rows returned on page one. Keyset pagination
	// must continue after order 2 instead of applying OFFSET 2 to the smaller
	// candidate set and skipping orders 3 and 4.
	repo.mu.Lock()
	repo.previewItems = append([]VIPReconcilePreviewItem(nil), repo.previewItems[2:]...)
	repo.mu.Unlock()

	second, err := svc.Preview(context.Background(), first.NextCursor, 2)
	require.NoError(t, err)
	require.Equal(t, []VIPReconcilePreviewItem{{OrderID: 3}, {OrderID: 4}}, second.Items)
	require.Empty(t, second.NextCursor)

	require.Equal(t, 1, repo.nowCalls)
	require.Equal(t, []int64{0, 2}, repo.previewAfterIDs)
	require.Equal(t, []int{3, 3}, repo.previewLimits)
	require.Equal(t, []time.Time{asOf, asOf}, repo.previewAsOf)
}

func TestVIPReconcilePreviewRejectsInvalidCursor(t *testing.T) {
	svc := NewVIPReconcileService(&vipReconcileRepositoryStub{}, nil)
	_, err := svc.Preview(context.Background(), "not-a-cursor", 50)
	require.ErrorContains(t, err, "invalid VIP reconcile cursor")
}

func TestVIPReconcileResumeActiveJob(t *testing.T) {
	for _, status := range []string{"queued", "running"} {
		t.Run(status, func(t *testing.T) {
			repo := &vipReconcileRepositoryStub{
				activeJob:     &VIPReconcileJob{ID: 41, Status: status},
				processCalled: make(chan struct{}, 1),
				processResults: []VIPReconcileBatchResult{{
					Done: true,
				}},
			}
			svc := NewVIPReconcileService(repo, nil)

			require.NoError(t, svc.ResumeActiveJob(context.Background()))

			select {
			case <-repo.processCalled:
			case <-time.After(time.Second):
				t.Fatal("active reconcile job did not resume")
			}
		})
	}
}

func TestVIPReconcileResumeWithNoActiveJobIsNoop(t *testing.T) {
	repo := &vipReconcileRepositoryStub{
		processCalled: make(chan struct{}, 1),
	}
	svc := NewVIPReconcileService(repo, nil)

	require.NoError(t, svc.ResumeActiveJob(context.Background()))

	select {
	case <-repo.processCalled:
		t.Fatal("unexpected reconcile job start")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestVIPReconcileResumePropagatesRepositoryFailure(t *testing.T) {
	svc := NewVIPReconcileService(&vipReconcileRepositoryStub{
		activeErr: errors.New("database unavailable"),
	}, nil)

	err := svc.ResumeActiveJob(context.Background())

	require.ErrorContains(t, err, "load active VIP reconcile job")
	require.ErrorContains(t, err, "database unavailable")
}

func TestVIPReconcileJobRunsAsynchronouslyAndInvalidatesChangedUsers(t *testing.T) {
	repo := &vipReconcileRepositoryStub{
		createJob:     &VIPReconcileJob{ID: 7, Status: "queued"},
		processCalled: make(chan struct{}, 2),
		processResults: []VIPReconcileBatchResult{{
			Done:           true,
			Repaired:       2,
			ChangedUserIDs: []int64{11, 12},
		}},
	}
	cache := &vipReconcileInvalidatorStub{}
	svc := NewVIPReconcileService(repo, cache)
	require.NoError(t, svc.Start())
	defer svc.Stop()

	job, err := svc.CreateJob(context.Background(), "request-1", 9, "repair history")
	require.NoError(t, err)
	require.Equal(t, int64(7), job.ID)

	select {
	case <-repo.processCalled:
	case <-time.After(time.Second):
		t.Fatal("reconcile job did not start")
	}
	require.Eventually(t, func() bool {
		return len(cache.IDs()) == 2
	}, time.Second, 10*time.Millisecond)
	require.ElementsMatch(t, []int64{11, 12}, cache.IDs())
}

func TestVIPReconcileJobPersistsFailure(t *testing.T) {
	repo := &vipReconcileRepositoryStub{
		createJob:     &VIPReconcileJob{ID: 8, Status: "queued"},
		processCalled: make(chan struct{}, 1),
		processErr:    errors.New("batch failed"),
	}
	svc := NewVIPReconcileService(repo, nil)
	require.NoError(t, svc.Start())
	defer svc.Stop()
	_, err := svc.CreateJob(context.Background(), "request-2", 9, "repair history")
	require.NoError(t, err)

	select {
	case <-repo.processCalled:
	case <-time.After(time.Second):
		t.Fatal("reconcile job did not start")
	}
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return repo.markFailed == "batch failed" &&
			!repo.markFailedDeadline.IsZero() &&
			repo.markFailedDeadline.After(time.Now()) &&
			time.Until(repo.markFailedDeadline) <= vipReconcileFailureWriteTimeout
	}, time.Second, 10*time.Millisecond)
}

func TestVIPReconcileJobRetriesTerminalWriteInsteadOfAbandoningActiveJob(t *testing.T) {
	repo := &vipReconcileRepositoryStub{
		createJob:        &VIPReconcileJob{ID: 9, Status: "queued"},
		processCalled:    make(chan struct{}, 3),
		processErr:       errors.New("batch failed"),
		markFailedErrors: []error{errors.New("database temporarily unavailable")},
	}
	svc := NewVIPReconcileService(repo, nil)
	require.NoError(t, svc.Start())
	defer svc.Stop()
	_, err := svc.CreateJob(context.Background(), "request-3", 9, "repair history")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return repo.markFailedCalls >= 2 && repo.markFailed == "batch failed"
	}, 2*time.Second, 10*time.Millisecond)
}

func TestVIPReconcileJobTimeoutRequeuesWithoutTerminalFailure(t *testing.T) {
	repo := &vipReconcileRepositoryStub{
		activeJob:             &VIPReconcileJob{ID: 10, Status: "running"},
		processCalled:         make(chan struct{}, 1),
		processWaitForContext: true,
	}
	svc := NewVIPReconcileService(repo, nil)
	svc.ConfigureRuntime(nil, nil, 25*time.Millisecond)
	defer svc.Stop()

	require.NoError(t, svc.ResumeActiveJob(context.Background()))
	select {
	case <-repo.processCalled:
	case <-time.After(time.Second):
		t.Fatal("reconcile job did not start")
	}
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return repo.requeueCalls == 1 &&
			strings.Contains(repo.requeueReason, "automatic resume") &&
			repo.markFailedCalls == 0
	}, time.Second, 10*time.Millisecond)
}

func TestVIPReconcileWorkerSkipsWhenPeerOwnsLeaderLease(t *testing.T) {
	repo := &vipReconcileRepositoryStub{
		activeJob:     &VIPReconcileJob{ID: 11, Status: "queued"},
		processCalled: make(chan struct{}, 1),
	}
	lock := &vipReconcileLeaderLockStub{acquired: false}
	svc := NewVIPReconcileService(repo, nil)
	svc.ConfigureRuntime(lock, nil, time.Minute)
	defer svc.Stop()

	require.NoError(t, svc.ResumeActiveJob(context.Background()))
	require.Eventually(t, func() bool {
		lock.mu.Lock()
		defer lock.mu.Unlock()
		return lock.calls == 1 &&
			lock.ttl > time.Minute
	}, time.Second, 10*time.Millisecond)
	select {
	case <-repo.processCalled:
		t.Fatal("non-leader instance processed the reconcile job")
	case <-time.After(50 * time.Millisecond):
	}
}
