package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type imageTaskMemoryStore struct {
	task    *ImageTaskRecord
	ttl     time.Duration
	saveErr error
	getErr  error
}

func (s *imageTaskMemoryStore) Save(_ context.Context, task *ImageTaskRecord, ttl time.Duration) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	copy := *task
	s.task = &copy
	s.ttl = ttl
	return nil
}

func (s *imageTaskMemoryStore) Get(_ context.Context, _ string) (*ImageTaskRecord, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.task == nil {
		return nil, ErrImageTaskNotFound
	}
	copy := *s.task
	return &copy, nil
}

func TestImageTaskServiceLifecycleAndOwnership(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, 10*time.Minute)
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}

	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusProcessing, created.Status)
	require.Equal(t, created.ID, created.TaskID)
	require.Equal(t, "image.generation.task", created.Object)
	require.Equal(t, time.Hour, store.ttl)
	require.Equal(t, owner.UserID, store.task.UserID)
	require.Equal(t, owner.APIKeyID, store.task.APIKeyID)

	_, err = svc.Get(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 10}, created.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)

	result := json.RawMessage(`{"created":123,"data":[{"url":"https://example.test/image.png"}]}`)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, result))

	completed, err := svc.Get(context.Background(), owner, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusCompleted, completed.Status)
	require.Equal(t, http.StatusOK, completed.HTTPStatus)
	require.Equal(t, "https://example.test/image.png", completed.ImageURL)
	require.JSONEq(t, string(result), string(completed.Result))
	require.NotNil(t, completed.CompletedAt)
}

func TestImageTaskServiceInvalidResultBecomesFailed(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	created, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.NoError(t, err)

	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, json.RawMessage(`not-json`)))
	got, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2}, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusFailed, got.Status)
	require.Equal(t, http.StatusBadGateway, got.HTTPStatus)
	require.Contains(t, string(got.Error), "non-JSON")
}

func TestImageTaskServiceMapsStoreFailures(t *testing.T) {
	store := &imageTaskMemoryStore{saveErr: errors.New("redis down")}
	svc := NewImageTaskService(store)

	_, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
}

func TestImageTaskServiceCountPendingForUserTracksLifecycle(t *testing.T) {
	store := &imageTaskMultiMemoryStore{tasks: map[string]*ImageTaskRecord{}}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, 10*time.Minute)
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}

	first, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	second, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	_, err = svc.Create(context.Background(), ImageTaskOwner{UserID: 8, APIKeyID: 1})
	require.NoError(t, err)

	pending, err := svc.CountPendingForUser(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 2, pending)
	pending, err = svc.CountPendingForUser(context.Background(), 8)
	require.NoError(t, err)
	require.Equal(t, 1, pending)
	pending, err = svc.CountPendingForUser(context.Background(), 99)
	require.NoError(t, err)
	require.Equal(t, 0, pending)

	require.NoError(t, svc.Complete(context.Background(), first.ID, http.StatusOK, json.RawMessage(`{"data":[]}`)))
	pending, err = svc.CountPendingForUser(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 1, pending)

	require.NoError(t, svc.Fail(context.Background(), second.ID, http.StatusBadGateway, json.RawMessage(`{"type":"api_error","message":"x"}`)))
	pending, err = svc.CountPendingForUser(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 0, pending)
}

func TestImageTaskServiceCountPendingForUserPrunesStaleEntries(t *testing.T) {
	store := &imageTaskMultiMemoryStore{tasks: map[string]*ImageTaskRecord{}}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, 10*time.Minute)
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	_, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 9})
	require.NoError(t, err)
	pending, err := svc.CountPendingForUser(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 1, pending)

	// 执行超时 + 宽限之内仍计入。
	now = now.Add(10*time.Minute + imageTaskPendingGrace - time.Second)
	pending, err = svc.CountPendingForUser(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 1, pending)

	// 超过后视为已不再占用配额（如 finish 落库失败导致终态未回写）。
	now = now.Add(2 * time.Second)
	pending, err = svc.CountPendingForUser(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 0, pending)
}

func TestImageTaskServiceCountPendingForUserPrefersStoreCounter(t *testing.T) {
	store := &imageTaskCountingStore{imageTaskMultiMemoryStore: imageTaskMultiMemoryStore{tasks: map[string]*ImageTaskRecord{}}, pending: 42}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)

	_, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 9})
	require.NoError(t, err)
	pending, err := svc.CountPendingForUser(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 42, pending, "store-provided count wins over local tracking")

	store.err = errors.New("redis down")
	_, err = svc.CountPendingForUser(context.Background(), 7)
	require.Error(t, err)
}

func TestImageTaskServiceCountPendingForUserUnavailableWithoutStore(t *testing.T) {
	var svc *ImageTaskService
	_, err := svc.CountPendingForUser(context.Background(), 7)
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
	_, err = NewImageTaskServiceWithOptions(nil, time.Hour, time.Minute).CountPendingForUser(context.Background(), 7)
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
}

// imageTaskMultiMemoryStore 保存多条任务记录（imageTaskMemoryStore 只保留最后一条）。
type imageTaskMultiMemoryStore struct {
	tasks map[string]*ImageTaskRecord
}

func (s *imageTaskMultiMemoryStore) Save(_ context.Context, task *ImageTaskRecord, _ time.Duration) error {
	copy := *task
	s.tasks[task.ID] = &copy
	return nil
}

func (s *imageTaskMultiMemoryStore) Get(_ context.Context, id string) (*ImageTaskRecord, error) {
	task, ok := s.tasks[id]
	if !ok {
		return nil, ErrImageTaskNotFound
	}
	copy := *task
	return &copy, nil
}

// imageTaskCountingStore 实现 ImageTaskPendingCounter，模拟带 per-user 索引的存储。
type imageTaskCountingStore struct {
	imageTaskMultiMemoryStore
	pending int
	err     error
}

func (s *imageTaskCountingStore) CountPendingForUser(_ context.Context, _ int64) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.pending, nil
}
