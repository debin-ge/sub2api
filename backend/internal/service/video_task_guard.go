package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrVideoDeletePending   = infraerrors.Conflict("VIDEO_DELETE_PENDING", "video deletion is queued; retry after completion")
	ErrVideoLeaseLost       = infraerrors.Conflict("VIDEO_LEASE_LOST", "video task execution lease is no longer owned")
	ErrVideoVersionConflict = infraerrors.Conflict("VIDEO_VERSION_CONFLICT", "video task changed; refresh before retrying")
)

type VideoTaskLease struct {
	TaskID  int64
	Owner   string
	Epoch   int64
	Version int64
}

type VideoTaskWriteGuard struct {
	TaskID  int64
	Version int64
	Lease   *VideoTaskLease
}

type videoTaskGuardContextKey struct{}
type videoTaskLeaseContextKey struct{}
type videoAdminVersionContextKey struct{}

type videoAdminVersion struct {
	PublicID string
	Version  int64
}

func WithVideoAdminExpectedVersion(ctx context.Context, publicID string, version int64) context.Context {
	return context.WithValue(ctx, videoAdminVersionContextKey{}, videoAdminVersion{PublicID: publicID, Version: version})
}

func validateVideoAdminExpectedVersion(ctx context.Context, task *VideoTask) error {
	expected, exists := ctx.Value(videoAdminVersionContextKey{}).(videoAdminVersion)
	if !exists {
		return nil
	}
	if task == nil || expected.Version < 0 || expected.PublicID != task.PublicID || expected.Version != task.Version {
		return ErrVideoVersionConflict
	}
	return nil
}

func VideoTaskLeaseFromTask(task *VideoTask) VideoTaskLease {
	if task == nil || task.LeaseOwner == nil {
		return VideoTaskLease{}
	}
	return VideoTaskLease{TaskID: task.ID, Owner: *task.LeaseOwner, Epoch: task.LeaseEpoch, Version: task.Version}
}

func WithVideoTaskLease(ctx context.Context, lease VideoTaskLease) context.Context {
	return context.WithValue(ctx, videoTaskLeaseContextKey{}, lease)
}

func VideoTaskLeaseFromContext(ctx context.Context) (VideoTaskLease, bool) {
	lease, ok := ctx.Value(videoTaskLeaseContextKey{}).(VideoTaskLease)
	return lease, ok
}

func WithVideoTaskWriteGuard(ctx context.Context, taskID, version int64) context.Context {
	guard := VideoTaskWriteGuard{TaskID: taskID, Version: version}
	if lease, ok := VideoTaskLeaseFromContext(ctx); ok {
		guard.Lease = &lease
	}
	return context.WithValue(ctx, videoTaskGuardContextKey{}, guard)
}

func VideoTaskWriteGuardFromContext(ctx context.Context) (VideoTaskWriteGuard, bool) {
	guard, ok := ctx.Value(videoTaskGuardContextKey{}).(VideoTaskWriteGuard)
	return guard, ok
}

func videoTaskWriteContext(ctx context.Context, task *VideoTask) context.Context {
	if task == nil {
		return ctx
	}
	return WithVideoTaskWriteGuard(ctx, task.ID, task.Version)
}

type VideoTaskWakeupRepository interface {
	WakeVideoTask(context.Context, string, time.Time) (*VideoTask, error)
}

func videoWorkerRequestTimeout(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.Gateway.Video.WorkerRequestTimeoutSeconds > 0 {
		return time.Duration(cfg.Gateway.Video.WorkerRequestTimeoutSeconds) * time.Second
	}
	return 30 * time.Second
}
