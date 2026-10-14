package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

var (
	ErrImageTaskPendingLimit       = infraerrors.New(http.StatusTooManyRequests, "IMAGE_TASKS_PENDING_LIMIT", "too many pending image tasks for this user")
	ErrImageTaskReservationExpired = infraerrors.New(http.StatusServiceUnavailable, "IMAGE_TASK_RESERVATION_EXPIRED", "image task admission expired")
)

// ImageTaskPendingStore provides atomic, shared admission. A counter alone is
// insufficient: the admission and reservation must be one storage operation.
// The UUID/token pair is never reused, including after expiry or worker death.
type ImageTaskPendingStore interface {
	AcquireImageTaskPending(context.Context, ImageTaskReservation, int, time.Duration) (bool, error)
	CommitImageTaskPending(context.Context, *ImageTaskRecord, time.Duration) error
	ReleaseImageTaskPending(context.Context, ImageTaskReservation) error
}

// ImageTaskTerminalStore atomically preserves the first terminal result and
// releases its admission slot. It also supports older records without a lease.
type ImageTaskTerminalStore interface {
	FinishImageTask(context.Context, *ImageTaskRecord, time.Duration) error
}

type ImageTaskReservation struct {
	ID       string
	Token    string
	Owner    ImageTaskOwner
	Deadline time.Time
}

// Reserve starts the total task deadline before body parsing or moderation.
// A zero limit explicitly disables the admission cap, but not the deadline.
func (s *ImageTaskService) Reserve(ctx context.Context, owner ImageTaskOwner, limit int) (*ImageTaskReservation, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	if owner.UserID <= 0 || owner.APIKeyID <= 0 {
		return nil, ErrImageTaskForbidden
	}
	reservation := &ImageTaskReservation{
		ID:       "imgtask_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		Owner:    owner,
		Deadline: s.clock().Add(s.ExecutionTimeout()),
	}
	if limit <= 0 {
		return reservation, nil
	}
	store, ok := s.store.(ImageTaskPendingStore)
	if !ok {
		// Never silently weaken a configured limit to per-process accounting.
		return nil, ErrImageTaskUnavailable
	}
	if _, ok := s.store.(ImageTaskTerminalStore); !ok {
		return nil, ErrImageTaskUnavailable
	}
	reservation.Token = uuid.NewString()
	admissionCtx, cancel := context.WithDeadline(ctx, reservation.Deadline)
	defer cancel()
	acquired, err := store.AcquireImageTaskPending(admissionCtx, *reservation, limit, s.ExecutionTimeout()+imageTaskPendingGrace)
	if err != nil {
		// The write may have succeeded before its reply was lost. Releasing our
		// unique token cannot release another request's admission.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cleanupCancel()
		_ = store.ReleaseImageTaskPending(cleanupCtx, *reservation)
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	if !acquired {
		return nil, ErrImageTaskPendingLimit
	}
	return reservation, nil
}

func (s *ImageTaskService) ReleaseReservation(ctx context.Context, reservation *ImageTaskReservation) error {
	if reservation == nil || reservation.Token == "" {
		return nil
	}
	if s == nil || s.store == nil {
		return ErrImageTaskUnavailable
	}
	store, ok := s.store.(ImageTaskPendingStore)
	if !ok {
		return ErrImageTaskUnavailable
	}
	if err := store.ReleaseImageTaskPending(ctx, *reservation); err != nil {
		return ErrImageTaskUnavailable.WithCause(err)
	}
	return nil
}

func (s *ImageTaskService) CreateReserved(ctx context.Context, reservation *ImageTaskReservation) (*ImageTask, error) {
	if s == nil || s.store == nil || reservation == nil {
		return nil, ErrImageTaskUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	if !s.clock().Before(reservation.Deadline) {
		return nil, ErrImageTaskReservationExpired
	}
	now := s.clock().UTC()
	task := &ImageTaskRecord{
		ID: reservation.ID, UserID: reservation.Owner.UserID, APIKeyID: reservation.Owner.APIKeyID,
		Status: ImageTaskStatusProcessing, CreatedAt: now.Unix(), ExpiresAt: now.Add(s.ttl).Unix(),
		PendingToken: reservation.Token, ExecutionDeadline: reservation.Deadline.UnixMilli(),
	}
	var err error
	if reservation.Token != "" {
		store, ok := s.store.(ImageTaskPendingStore)
		if !ok {
			return nil, ErrImageTaskUnavailable
		}
		err = store.CommitImageTaskPending(ctx, task, s.ttl)
	} else {
		err = s.store.Save(ctx, task, s.ttl)
	}
	if err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	return imageTaskToPublic(task), nil
}
