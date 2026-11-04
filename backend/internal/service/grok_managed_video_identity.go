package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

func GrokManagedIdempotencyKey(key *APIKey, operation, value string) string {
	if value == "" || key == nil {
		return ""
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("grok-video-v1:%d:%d:%s:%s", key.UserID, key.ID, normalizeVideoOperation(operation), value)))
	return "grok:" + hex.EncodeToString(digest[:])
}

// FindGrokManagedReplay runs before source downloads and any admission/hold. A
// bound task stays on its original engine even when new creation is disabled.
func (s *VideoTaskService) FindGrokManagedReceipt(ctx context.Context, key *APIKey, operation, internalKey string) (*VideoTask, error) {
	if internalKey == "" {
		return nil, nil
	}
	if s == nil || s.tasks == nil || key == nil {
		return nil, ErrVideoInvalidRequest
	}
	task, err := s.tasks.GetVideoTaskByIdempotency(ctx, key.UserID, videoEndpointForOperation(operation), internalKey)
	if errors.Is(err, ErrVideoTaskNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if task.Provider != VideoProviderGrok || task.APIKeyID == nil || *task.APIKeyID != key.ID || task.GroupID == nil || key.GroupID == nil || *task.GroupID != *key.GroupID {
		return nil, ErrVideoTaskNotFound
	}
	return task, nil
}
func (s *VideoTaskService) FindGrokManagedReplay(ctx context.Context, key *APIKey, operation, internalKey, clientHash string) (*VideoTask, error) {
	task, err := s.FindGrokManagedReceipt(ctx, key, operation, internalKey)
	if err != nil || task == nil {
		return task, err
	}
	stored, _ := task.RequestAttributes["grok_client_request_hash"].(string)
	if strings.TrimSpace(stored) == "" || stored != clientHash {
		return nil, ErrVideoIdempotencyConflict
	}
	return task, nil
}

type grokPreparationTokenKey struct{}

// PrepareGrokSubmission applies the same admission/RPM gate before expensive
// media verification. Submit rechecks budgets but reuses the idempotent RPM
// token, so a single creation attempt is never counted twice.
func (s *VideoTaskService) PrepareGrokSubmission(ctx context.Context, key *APIKey, operation, internalKey string) (context.Context, error) {
	if s == nil || s.groups == nil || s.admission == nil || key == nil || key.GroupID == nil || s.cfg == nil || !s.cfg.Gateway.Video.GrokManagedEnabled || !s.cfg.Gateway.Video.CreationEnabled {
		return ctx, ErrVideoCreationDisabled
	}
	group, err := s.groups.GetByID(ctx, *key.GroupID)
	if err != nil || group == nil {
		return ctx, ErrVideoInvalidRequest
	}
	if group.Status != StatusActive || group.ID != *key.GroupID {
		return ctx, ErrVideoInvalidRequest
	}
	ref := internalKey
	if ref == "" {
		ref = NewVideoTaskID()
	}
	token, err := HashVideoRequest(map[string]any{"user_id": key.UserID, "endpoint": videoEndpointForOperation(operation), "operation_ref": ref})
	if err != nil {
		return ctx, ErrVideoInvalidRequest
	}
	if err = s.admission.CheckVideoAdmission(ctx, key, group, PlatformGrok, token); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, grokPreparationTokenKey{}, token), nil
}
