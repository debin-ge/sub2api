package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestImageTaskReservationRequiresAtomicStore(t *testing.T) {
	svc := NewImageTaskService(&imageTaskMemoryStore{})
	reservation, err := svc.Reserve(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2}, 1)
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
	require.Nil(t, reservation)
}

func TestImageTaskReservationKeepsAdmissionDeadline(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	now := time.Now()
	svc.now = func() time.Time { return now }
	owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
	reservation, err := svc.Reserve(context.Background(), owner, 0)
	require.NoError(t, err)
	deadline := reservation.Deadline
	now = now.Add(40 * time.Second)
	_, err = svc.CreateReserved(context.Background(), reservation)
	require.NoError(t, err)
	require.Equal(t, deadline.UnixMilli(), store.task.ExecutionDeadline, "preprocessing must not reset the execution timeout")
	now = deadline
	_, err = svc.CreateReserved(context.Background(), reservation)
	require.ErrorIs(t, err, ErrImageTaskReservationExpired)
}

func TestImageTaskReservationExpiredTaskRecoversOnOwnerPoll(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	now := time.Now()
	svc.now = func() time.Time { return now }
	owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
	reservation, err := svc.Reserve(context.Background(), owner, 0)
	require.NoError(t, err)
	task, err := svc.CreateReserved(context.Background(), reservation)
	require.NoError(t, err)
	now = reservation.Deadline.Add(imageTaskPendingGrace + time.Second)
	_, err = svc.Get(context.Background(), ImageTaskOwner{UserID: 2, APIKeyID: 2}, task.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)
	require.Equal(t, ImageTaskStatusProcessing, store.task.Status, "other owners cannot mutate the task")
	recovered, err := svc.Get(context.Background(), owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusFailed, recovered.Status)
	require.Contains(t, string(recovered.Error), "expired before completion")
}
