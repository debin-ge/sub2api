package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type videoLeaseTestRepo struct {
	VideoTaskRepository
	mu                                                        sync.Mutex
	pending                                                   []*VideoTask
	tasks                                                     map[int64]*VideoTask
	claims, active, maximumActive, writes, releases, renewals int
	claimLimits                                               []int
	failRenewAfter                                            int
}

func (repo *videoLeaseTestRepo) ClearExpiredVideoProviderAccess(context.Context, int) (int64, error) {
	return 0, nil
}

func (repo *videoLeaseTestRepo) ClaimDueVideoTasks(_ context.Context, owner string, limit int, duration time.Duration) ([]*VideoTask, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.claimLimits = append(repo.claimLimits, limit)
	if len(repo.pending) == 0 {
		return nil, nil
	}
	task := repo.pending[0]
	repo.pending = repo.pending[1:]
	expiry := time.Now().Add(duration)
	task.LeaseOwner, task.LeaseExpiresAt = &owner, &expiry
	task.LeaseEpoch++
	repo.claims++
	repo.active++
	if repo.active > repo.maximumActive {
		repo.maximumActive = repo.active
	}
	copy := *task
	return []*VideoTask{&copy}, nil
}

func (repo *videoLeaseTestRepo) RenewVideoTaskLease(_ context.Context, lease VideoTaskLease, duration time.Duration) (time.Time, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.renewals++
	if repo.failRenewAfter > 0 && repo.renewals > repo.failRenewAfter {
		return time.Time{}, errors.New("lease database unavailable")
	}
	task := repo.tasks[lease.TaskID]
	if task.LeaseOwner == nil || *task.LeaseOwner != lease.Owner || task.LeaseEpoch != lease.Epoch || !time.Now().Before(*task.LeaseExpiresAt) {
		return time.Time{}, ErrVideoLeaseLost
	}
	expiry := time.Now().Add(duration)
	task.LeaseExpiresAt = &expiry
	return expiry, nil
}

func (repo *videoLeaseTestRepo) ReleaseVideoTaskLease(_ context.Context, lease VideoTaskLease, _ *time.Time) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	task := repo.tasks[lease.TaskID]
	if task.LeaseOwner == nil || *task.LeaseOwner != lease.Owner || task.LeaseEpoch != lease.Epoch || !time.Now().Before(*task.LeaseExpiresAt) {
		return ErrVideoLeaseLost
	}
	repo.releases++
	repo.active--
	task.LeaseOwner = nil
	return nil
}

func (repo *videoLeaseTestRepo) TransitionVideoTask(ctx context.Context, publicID string, transition VideoTaskTransition) (*VideoTask, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	guard, ok := VideoTaskWriteGuardFromContext(ctx)
	if !ok {
		return nil, ErrVideoInvalidRequest
	}
	task := repo.tasks[guard.TaskID]
	if task.PublicID != publicID || guard.Lease == nil || task.LeaseEpoch != guard.Lease.Epoch || !time.Now().Before(*task.LeaseExpiresAt) {
		return nil, ErrVideoLeaseLost
	}
	if task.Version != guard.Version {
		return nil, ErrVideoVersionConflict
	}
	repo.writes++
	if transition.GenerationState != "" {
		task.GenerationState = transition.GenerationState
	}
	if transition.BillingState != "" {
		task.BillingState = transition.BillingState
	}
	task.NextActionAt = transition.NextActionAt
	task.Version++
	copy := *task
	return &copy, nil
}

type videoLeaseTestProvider struct {
	videoProviderStub
	get func(context.Context, ProviderTaskRef) (*ProviderVideoTask, error)
}

func (provider *videoLeaseTestProvider) Get(ctx context.Context, _ *Account, ref ProviderTaskRef) (*ProviderVideoTask, error) {
	return provider.get(ctx, ref)
}

func newVideoLeaseTestWorker(count int, get func(context.Context, ProviderTaskRef) (*ProviderVideoTask, error)) (*VideoTaskWorker, *videoLeaseTestRepo) {
	task := baseVideoWorkerTask()
	worker, _, _, _ := newVideoWorkerForTest(task, nil)
	repo := &videoLeaseTestRepo{tasks: make(map[int64]*VideoTask)}
	for index := 1; index <= count; index++ {
		copy := *task
		copy.ID, copy.PublicID = int64(index), NewVideoTaskID()
		providerID := fmt.Sprintf("provider-%d", index)
		copy.ProviderTaskID = &providerID
		copy.LeaseOwner, copy.LeaseExpiresAt, copy.LeaseEpoch = nil, nil, 0
		repo.tasks[copy.ID] = &copy
		repo.pending = append(repo.pending, &copy)
	}
	provider := &videoLeaseTestProvider{get: get}
	worker.tasks, worker.queue = repo, nil
	worker.providers = NewVideoProviderRegistry(provider)
	worker.service.tasks, worker.service.queue, worker.service.providers = repo, nil, worker.providers
	return worker, repo
}

func TestVideoTaskWorkerClaimsOnlyAvailableExecutionSlots(t *testing.T) {
	started := make(chan string, 5)
	proceed := make(chan struct{}, 5)
	worker, repo := newVideoLeaseTestWorker(5, func(ctx context.Context, ref ProviderTaskRef) (*ProviderVideoTask, error) {
		started <- ref.ProviderTaskID
		select {
		case <-proceed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &ProviderVideoTask{ProviderTaskID: ref.ProviderTaskID, Status: VideoGenerationInProgress, RawStatus: "in_progress"}, nil
	})
	worker.cfg.Gateway.Video.WorkerConcurrency = 2
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.ProcessBatch(ctx, 5) }()
	for index := 0; index < 2; index++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("initial execution slots did not start")
		}
	}
	repo.mu.Lock()
	claims := repo.claims
	repo.mu.Unlock()
	require.Equal(t, 2, claims)
	for index := 0; index < 5; index++ {
		proceed <- struct{}{}
	}
	require.NoError(t, <-done)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, 5, repo.claims)
	require.Equal(t, 5, repo.writes)
	require.Equal(t, 5, repo.releases)
	require.Equal(t, 2, repo.maximumActive)
	for _, limit := range repo.claimLimits {
		require.Equal(t, 1, limit)
	}
}

func TestVideoTaskWorkerHeartbeatKeepsSlowRequestLease(t *testing.T) {
	worker, repo := newVideoLeaseTestWorker(1, func(ctx context.Context, ref ProviderTaskRef) (*ProviderVideoTask, error) {
		timer := time.NewTimer(1200 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &ProviderVideoTask{ProviderTaskID: ref.ProviderTaskID, Status: VideoGenerationInProgress}, nil
	})
	worker.cfg.Gateway.Video.LeaseSeconds = 1
	require.NoError(t, worker.ProcessBatch(context.Background(), 1))
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.GreaterOrEqual(t, repo.renewals, 3)
	require.Equal(t, 1, repo.writes)
	require.Equal(t, 1, repo.releases)
}

func TestVideoTaskWorkerHeartbeatFailureDiscardsLateObservation(t *testing.T) {
	cancelled := make(chan struct{})
	worker, repo := newVideoLeaseTestWorker(1, func(ctx context.Context, ref ProviderTaskRef) (*ProviderVideoTask, error) {
		<-ctx.Done()
		close(cancelled)
		return &ProviderVideoTask{ProviderTaskID: ref.ProviderTaskID, Status: VideoGenerationCompleted, Metadata: map[string]any{"seconds": 8}}, nil
	})
	worker.cfg.Gateway.Video.LeaseSeconds = 1
	repo.failRenewAfter = 1
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, worker.ProcessBatch(ctx, 1))
	select {
	case <-cancelled:
	default:
		t.Fatal("upstream context was not cancelled")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, 2, repo.renewals)
	require.Zero(t, repo.writes)
	require.Zero(t, repo.releases)
	require.Equal(t, VideoGenerationQueued, repo.tasks[1].GenerationState)
}

func TestVideoTaskWorkerProviderRequestHasHardTimeout(t *testing.T) {
	worker, repo := newVideoLeaseTestWorker(1, func(ctx context.Context, _ ProviderTaskRef) (*ProviderVideoTask, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	worker.cfg.Gateway.Video.WorkerRequestTimeoutSeconds = 1
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, worker.ProcessBatch(ctx, 1))
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, 1, repo.writes)
	require.Equal(t, 1, repo.releases)
	require.Equal(t, VideoGenerationQueued, repo.tasks[1].GenerationState)
}
