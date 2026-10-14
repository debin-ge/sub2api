package handler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// asyncImageAdmissionStore supplies the service storage contracts to handler
// tests. It models reservation ownership and completion under one mutex, not
// Redis scripts or TTL behavior; those belong to repository tests.
type asyncImageAdmissionStore struct {
	*asyncImageMemoryStore
	reservations map[string]service.ImageTaskReservation
}

func newAsyncImageAdmissionStore() *asyncImageAdmissionStore {
	return &asyncImageAdmissionStore{
		asyncImageMemoryStore: &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)},
		reservations:          make(map[string]service.ImageTaskReservation),
	}
}

func (s *asyncImageAdmissionStore) pendingCount(userID int64) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingCountLocked(userID)
}

func (s *asyncImageAdmissionStore) pendingCountLocked(userID int64) int {
	count := 0
	for _, reservation := range s.reservations {
		if reservation.Owner.UserID == userID {
			count++
		}
	}
	return count
}

func (s *asyncImageAdmissionStore) AcquireImageTaskPending(ctx context.Context, reservation service.ImageTaskReservation, limit int, _ time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, exists := s.reservations[reservation.ID]; exists {
		return current.Token == reservation.Token && current.Owner == reservation.Owner, nil
	}
	if s.pendingCountLocked(reservation.Owner.UserID) >= limit {
		return false, nil
	}
	s.reservations[reservation.ID] = reservation
	return true, nil
}

func (s *asyncImageAdmissionStore) CommitImageTaskPending(ctx context.Context, task *service.ImageTaskRecord, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reservation, exists := s.reservations[task.ID]
	if !exists || reservation.Token != task.PendingToken || reservation.Owner.UserID != task.UserID || reservation.Owner.APIKeyID != task.APIKeyID {
		return service.ErrImageTaskReservationExpired
	}
	s.tasks[task.ID] = cloneAsyncImageAdmissionRecord(task)
	return nil
}

func (s *asyncImageAdmissionStore) ReleaseImageTaskPending(ctx context.Context, reservation service.ImageTaskReservation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, exists := s.reservations[reservation.ID]; exists && current.Token == reservation.Token && current.Owner == reservation.Owner {
		delete(s.reservations, reservation.ID)
	}
	return nil
}

func (s *asyncImageAdmissionStore) FinishImageTask(ctx context.Context, task *service.ImageTaskRecord, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.tasks[task.ID]
	if !exists {
		return service.ErrImageTaskNotFound
	}
	if current.UserID != task.UserID || current.APIKeyID != task.APIKeyID || current.PendingToken != task.PendingToken {
		return service.ErrImageTaskForbidden
	}
	if current.Status != service.ImageTaskStatusProcessing {
		return nil
	}
	s.tasks[task.ID] = cloneAsyncImageAdmissionRecord(task)
	delete(s.reservations, task.ID)
	return nil
}

func cloneAsyncImageAdmissionRecord(task *service.ImageTaskRecord) *service.ImageTaskRecord {
	copy := *task
	copy.Result = append(json.RawMessage(nil), task.Result...)
	copy.Error = append(json.RawMessage(nil), task.Error...)
	if task.CompletedAt != nil {
		completedAt := *task.CompletedAt
		copy.CompletedAt = &completedAt
	}
	return &copy
}

var (
	_ service.ImageTaskStore         = (*asyncImageAdmissionStore)(nil)
	_ service.ImageTaskPendingStore  = (*asyncImageAdmissionStore)(nil)
	_ service.ImageTaskTerminalStore = (*asyncImageAdmissionStore)(nil)
)
