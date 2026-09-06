package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/observability"
)

type VideoWebhookService struct {
	tasks     VideoTaskRepository
	accounts  AccountRepository
	providers *VideoProviderRegistry
	queue     VideoTaskQueue
}

func NewVideoWebhookService(
	tasks VideoTaskRepository,
	accounts AccountRepository,
	providers *VideoProviderRegistry,
	_ *VideoTaskService,
	queue VideoTaskQueue,
) *VideoWebhookService {
	return &VideoWebhookService{tasks: tasks, accounts: accounts, providers: providers, queue: queue}
}

func (s *VideoWebhookService) Handle(ctx context.Context, providerName string, accountID int64, request ProviderWebhookRequest) (*VideoTask, error) {
	if s == nil || s.tasks == nil || s.accounts == nil || s.providers == nil || accountID <= 0 {
		return nil, ErrVideoInvalidRequest
	}
	account, err := s.accounts.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	provider, ok := s.providers.Get(providerName)
	if !ok {
		observability.DefaultVideoMetrics().RecordWebhook(providerName, "invalid", 0)
		return nil, ErrVideoProviderUnsupported
	}
	if !provider.Capabilities().Supports(VideoCapabilityWebhook) {
		return nil, ErrVideoCapabilityUnsupported
	}
	verifier, ok := provider.(VideoWebhookVerifier)
	if !ok {
		return nil, ErrVideoCapabilityUnsupported
	}
	event, err := verifier.VerifyWebhook(ctx, account, request)
	if err != nil {
		observability.DefaultVideoMetrics().RecordWebhook(provider.Name(), "verify_error", 0)
		return nil, err
	}
	if event == nil || !validVideoProviderIdentifier(event.ProviderEventID) || !validVideoProviderIdentifier(event.ProviderTaskID) {
		observability.DefaultVideoMetrics().RecordWebhook(provider.Name(), "invalid", 0)
		return nil, ErrVideoInvalidRequest
	}
	event.ProviderEventID = strings.TrimSpace(event.ProviderEventID)
	event.ProviderTaskID = strings.TrimSpace(event.ProviderTaskID)
	event.Status = boundedVideoProviderStatus(event.Status)
	event.Payload = sanitizeVideoProviderEventPayload(event.Payload)
	task, lookupErr := s.tasks.GetVideoTaskByProviderID(ctx, provider.Name(), accountID, event.ProviderTaskID)
	if lookupErr != nil && !errors.Is(lookupErr, ErrVideoTaskNotFound) {
		return nil, lookupErr
	}
	var taskID *int64
	if task != nil {
		taskID = &task.ID
	}
	created, err := s.tasks.AppendVideoTaskEvent(ctx, VideoTaskEvent{
		TaskID: taskID, EventType: "provider_webhook", Provider: provider.Name(),
		AccountID: &accountID, ProviderTaskID: event.ProviderTaskID,
		ProviderEventID: event.ProviderEventID, Payload: event.Payload,
	})
	if err != nil {
		return nil, err
	}
	delay := time.Since(event.OccurredAt)
	if task == nil {
		observability.DefaultVideoMetrics().RecordWebhook(provider.Name(), "unmatched", delay)
		return nil, nil
	}
	if !created {
		observability.DefaultVideoMetrics().RecordWebhook(provider.Name(), "duplicate", delay)
	} else if IsVideoGenerationTerminal(task.GenerationState) {
		observability.DefaultVideoMetrics().RecordWebhook(provider.Name(), "ignored_terminal", delay)
	} else {
		observability.DefaultVideoMetrics().RecordWebhook(provider.Name(), "accepted", delay)
	}
	if IsVideoGenerationTerminal(task.GenerationState) {
		return task, nil
	}

	now := time.Now().UTC()
	updated := task
	if created || task.NextActionAt == nil || task.NextActionAt.After(now) {
		if wakeups, supported := s.tasks.(VideoTaskWakeupRepository); supported {
			updated, err = wakeups.WakeVideoTask(ctx, task.PublicID, now)
		} else {
			updated, err = s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
				NextActionAt: &now,
				EventType:    "provider_webhook_wakeup",
				EventPayload: map[string]any{"provider_event_id": event.ProviderEventID, "status": event.Status},
			})
			if errors.Is(err, ErrVideoVersionConflict) {
				updated, err = s.tasks.GetVideoTaskByPublicID(ctx, task.PublicID)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	if s.queue != nil {
		_, _ = s.queue.Enqueue(context.WithoutCancel(ctx), updated.PublicID)
	}
	return updated, nil
}
