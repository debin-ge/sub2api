package service

import (
	"context"
	"errors"
)

func (s *VideoTaskService) characterSourceTask(ctx context.Context, userID int64, resource *VideoResource) (*VideoTask, error) {
	if s == nil || s.tasks == nil || resource == nil || resource.UserID != userID ||
		resource.SourceTaskID == nil || resource.ProviderResourceID == "" {
		return nil, ErrVideoResourceNotFound
	}
	task, err := s.tasks.GetVideoTaskByProviderID(ctx, resource.Provider, resource.AccountID, resource.ProviderResourceID)
	if errors.Is(err, ErrVideoTaskNotFound) {
		return nil, ErrVideoResourceNotFound
	}
	if err != nil {
		return nil, err
	}
	if task == nil || task.ID != *resource.SourceTaskID || task.UserID != userID ||
		task.Operation != VideoOperationCharacterCreate || task.Provider != resource.Provider ||
		task.AccountID == nil || *task.AccountID != resource.AccountID ||
		task.ProviderTaskID == nil || *task.ProviderTaskID != resource.ProviderResourceID {
		return nil, ErrVideoResourceNotFound
	}
	return task, nil
}

func (s *VideoTaskService) requireSettledCharacter(ctx context.Context, userID int64, resource *VideoResource) error {
	if resource == nil || resource.Status != "ready" || resource.DeletedAt != nil ||
		(resource.ExpiresAt != nil && !s.now().Before(*resource.ExpiresAt)) {
		return ErrVideoResourceNotFound
	}
	task, err := s.characterSourceTask(ctx, userID, resource)
	if err != nil {
		return err
	}
	if task.GenerationState != VideoGenerationCompleted || task.DeleteState == VideoDeleteDeleted || task.DeletedAt != nil {
		return ErrVideoResourceNotFound
	}
	if task.BillingState != VideoBillingCaptured {
		if task.BillingState == VideoBillingCapturePending {
			s.enqueueBestEffort(ctx, task.PublicID)
		}
		return ErrVideoSettlementPending
	}
	if task.DeleteState != VideoDeleteNone {
		return ErrVideoDeletePending
	}
	return nil
}
