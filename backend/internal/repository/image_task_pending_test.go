package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func pendingImageTestStore(t *testing.T) (*miniredis.Miniredis, *redis.Client, *imageTaskStore) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1, DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb, &imageTaskStore{rdb: rdb}
}

func TestImageTaskPendingAdmissionSharedAndAtomic(t *testing.T) {
	_, rdb, store := pendingImageTestStore(t)
	ctx := context.Background()
	services := []*service.ImageTaskService{
		service.NewImageTaskService(store),
		service.NewImageTaskService(&imageTaskStore{rdb: rdb}),
	}
	const limit, requests = 3, 40
	start := make(chan struct{})
	reservations := make(chan *service.ImageTaskReservation, requests)
	failures := make(chan error, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			reservation, err := services[i%2].Reserve(ctx, service.ImageTaskOwner{UserID: 7, APIKeyID: int64(i + 1)}, limit)
			if err != nil {
				failures <- err
				return
			}
			reservations <- reservation
		}(i)
	}
	close(start)
	wg.Wait()
	close(reservations)
	close(failures)
	require.Len(t, reservations, limit, "all API keys and instances share one user's limit")
	for err := range failures {
		require.ErrorIs(t, err, service.ErrImageTaskPendingLimit)
	}
	first := <-reservations
	forged := *first
	forged.Token = "another-token"
	require.NoError(t, services[0].ReleaseReservation(ctx, &forged))
	_, err := services[0].Reserve(ctx, first.Owner, limit)
	require.ErrorIs(t, err, service.ErrImageTaskPendingLimit)
	require.NoError(t, services[0].ReleaseReservation(ctx, first))
	require.NoError(t, services[1].ReleaseReservation(ctx, first))
	_, err = services[1].Reserve(ctx, first.Owner, limit)
	require.NoError(t, err)
	_, err = services[0].Reserve(ctx, first.Owner, limit)
	require.ErrorIs(t, err, service.ErrImageTaskPendingLimit)
	_, err = services[0].Reserve(ctx, service.ImageTaskOwner{UserID: 8, APIKeyID: 100}, limit)
	require.NoError(t, err, "another user has its own admission cap")
}

func TestImageTaskPendingCommitAndTerminalCAS(t *testing.T) {
	_, rdb, store := pendingImageTestStore(t)
	ctx := context.Background()
	svc := service.NewImageTaskService(store)
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	reservation, err := svc.Reserve(ctx, owner, 1)
	require.NoError(t, err)
	task, err := svc.CreateReserved(ctx, reservation)
	require.NoError(t, err)
	record, err := store.Get(ctx, task.ID)
	require.NoError(t, err)
	record.Status = service.ImageTaskStatusCompleted
	record.Result = json.RawMessage(`{"data":[]}`)
	forged := *record
	forged.UserID++
	require.ErrorIs(t, store.FinishImageTask(ctx, &forged, time.Hour), service.ErrImageTaskForbidden)
	forged = *record
	forged.APIKeyID++
	require.ErrorIs(t, store.FinishImageTask(ctx, &forged, time.Hour), service.ErrImageTaskForbidden)
	forged = *record
	forged.PendingToken = "old-token"
	require.ErrorIs(t, store.FinishImageTask(ctx, &forged, time.Hour), service.ErrImageTaskForbidden)
	require.Equal(t, int64(1), rdb.ZCard(ctx, imageTaskPendingKey(owner.UserID)).Val())
	require.NoError(t, store.FinishImageTask(ctx, record, time.Hour))
	record.Status = service.ImageTaskStatusFailed
	record.Result = nil
	record.Error = json.RawMessage(`{"message":"late failure"}`)
	require.NoError(t, store.FinishImageTask(ctx, record, time.Hour))
	got, err := svc.Get(ctx, owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusCompleted, got.Status, "first terminal state wins")
	require.Equal(t, int64(0), rdb.ZCard(ctx, imageTaskPendingKey(owner.UserID)).Val())
	_, err = svc.Reserve(ctx, owner, 1)
	require.NoError(t, err)
	require.NoError(t, svc.ReleaseReservation(ctx, reservation))
	require.Equal(t, int64(1), rdb.ZCard(ctx, imageTaskPendingKey(owner.UserID)).Val(), "late old release does not free the replacement slot")
}

func TestImageTaskPendingExpiryAndCommitFailure(t *testing.T) {
	mr, _, store := pendingImageTestStore(t)
	ctx := context.Background()
	svc := service.NewImageTaskServiceWithOptions(store, 24*time.Hour, time.Minute)
	owner := service.ImageTaskOwner{UserID: 1, APIKeyID: 2}
	reservation, err := svc.Reserve(ctx, owner, 1)
	require.NoError(t, err)
	mr.FastForward(3 * time.Minute)
	_, err = svc.CreateReserved(ctx, reservation)
	require.Error(t, err, "a lost/expired lease must not launch a worker")
	_, err = store.Get(ctx, reservation.ID)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	_, err = svc.Reserve(ctx, owner, 1)
	require.NoError(t, err, "crashed submitters do not hold capacity forever")
}

func TestImageTaskPendingRedisFailureIsClosed(t *testing.T) {
	mr, _, store := pendingImageTestStore(t)
	svc := service.NewImageTaskService(store)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	mr.Close()
	reservation, err := svc.Reserve(ctx, service.ImageTaskOwner{UserID: 1, APIKeyID: 2}, 1)
	require.ErrorIs(t, err, service.ErrImageTaskUnavailable)
	require.Nil(t, reservation)
}

func TestImageTaskPendingLegacyRecordStillCompletes(t *testing.T) {
	_, _, store := pendingImageTestStore(t)
	svc := service.NewImageTaskService(store)
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 1, APIKeyID: 2}
	legacy, err := svc.Create(ctx, owner)
	require.NoError(t, err)
	require.NoError(t, svc.Complete(ctx, legacy.ID, http.StatusOK, json.RawMessage(`{"data":[]}`)))
	require.NoError(t, svc.Fail(ctx, legacy.ID, http.StatusBadGateway, json.RawMessage(`{"error":"late"}`)))
	got, err := svc.Get(ctx, owner, legacy.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusCompleted, got.Status)
	_, err = svc.Get(ctx, service.ImageTaskOwner{UserID: 1, APIKeyID: 3}, legacy.ID)
	require.True(t, errors.Is(err, service.ErrImageTaskNotFound))
}
