package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVideoWebhookServiceDuplicateEventDoesNotRefetchProvider(t *testing.T) {
	created := false
	task := baseVideoWorkerTask()
	tasks := &videoTaskRepoStub{task: task, eventCreated: &created}
	provider := &videoProviderStub{webhookEvent: &ProviderWebhookEvent{
		ProviderEventID: "evt_duplicate", ProviderTaskID: *task.ProviderTaskID,
		Status: VideoGenerationCompleted, OccurredAt: time.Now().UTC(),
	}}
	accounts := &videoAccountRepoStub{accounts: []Account{{
		ID: *task.AccountID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
	}}}
	taskService := &VideoTaskService{tasks: tasks, now: time.Now}
	service := NewVideoWebhookService(tasks, accounts, NewVideoProviderRegistry(provider), taskService, nil)

	result, err := service.Handle(context.Background(), VideoProviderOpenAI, *task.AccountID, ProviderWebhookRequest{})

	require.NoError(t, err)
	require.Same(t, task, result)
	require.Zero(t, provider.getCalls)
}

func TestVideoWebhookServiceDuplicateRetriesWakeAfterPriorTransitionFailure(t *testing.T) {
	created := false
	task := baseVideoWorkerTask()
	future := time.Now().UTC().Add(time.Minute)
	task.NextActionAt = &future
	tasks := &videoTaskRepoStub{task: task, eventCreated: &created}
	provider := &videoProviderStub{webhookEvent: &ProviderWebhookEvent{
		ProviderEventID: "evt_duplicate", ProviderTaskID: *task.ProviderTaskID,
		Status: VideoGenerationCompleted, OccurredAt: time.Now().UTC(),
	}}
	accounts := &videoAccountRepoStub{accounts: []Account{{
		ID: *task.AccountID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
	}}}
	queue := &videoQueueStub{}
	service := NewVideoWebhookService(tasks, accounts, NewVideoProviderRegistry(provider), nil, queue)

	result, err := service.Handle(context.Background(), VideoProviderOpenAI, *task.AccountID, ProviderWebhookRequest{})

	require.NoError(t, err)
	require.Same(t, task, result)
	require.Len(t, tasks.transitions, 1)
	require.Equal(t, "provider_webhook_wakeup", tasks.transitions[0].EventType)
	require.NotEmpty(t, queue.enqueued)
	require.Zero(t, provider.getCalls)
}

func TestVideoWebhookServicePersistsUnmatchedEventWithoutProviderLookup(t *testing.T) {
	tasks := &videoTaskRepoStub{}
	provider := &videoProviderStub{webhookEvent: &ProviderWebhookEvent{
		ProviderEventID: "evt_unmatched", ProviderTaskID: "video_not_yet_linked",
		Status: VideoGenerationCompleted, OccurredAt: time.Now().UTC(),
		Payload: map[string]any{"type": "video.completed"},
	}}
	accounts := &videoAccountRepoStub{accounts: []Account{{
		ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
	}}}
	taskService := &VideoTaskService{tasks: tasks, now: time.Now}
	service := NewVideoWebhookService(tasks, accounts, NewVideoProviderRegistry(provider), taskService, nil)

	result, err := service.Handle(context.Background(), VideoProviderOpenAI, 11, ProviderWebhookRequest{})

	require.NoError(t, err)
	require.Nil(t, result)
	require.Zero(t, provider.getCalls)
	require.Len(t, tasks.events, 1)
	require.Nil(t, tasks.events[0].TaskID)
	require.Equal(t, "evt_unmatched", tasks.events[0].ProviderEventID)
	require.Equal(t, "video_not_yet_linked", tasks.events[0].ProviderTaskID)
}

func TestVideoWebhookServiceRejectsInvalidVerifiedIdentifiersBeforePersistence(t *testing.T) {
	tasks := &videoTaskRepoStub{}
	provider := &videoProviderStub{webhookEvent: &ProviderWebhookEvent{
		ProviderEventID: strings.Repeat("x", 256), ProviderTaskID: "video_upstream",
		Status: VideoGenerationCompleted, OccurredAt: time.Now().UTC(),
	}}
	accounts := &videoAccountRepoStub{accounts: []Account{{
		ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
	}}}
	service := NewVideoWebhookService(tasks, accounts, NewVideoProviderRegistry(provider), nil, nil)

	result, err := service.Handle(context.Background(), VideoProviderOpenAI, 11, ProviderWebhookRequest{})

	require.ErrorIs(t, err, ErrVideoInvalidRequest)
	require.Nil(t, result)
	require.Empty(t, tasks.events)
}

func TestVideoWebhookServiceSanitizesGenericProviderPayload(t *testing.T) {
	created := true
	task := baseVideoWorkerTask()
	tasks := &videoTaskRepoStub{task: task, eventCreated: &created}
	provider := &videoProviderStub{webhookEvent: &ProviderWebhookEvent{
		ProviderEventID: " evt_safe ", ProviderTaskID: " video_upstream ",
		Status: strings.Repeat("x", 65), Payload: map[string]any{
			"type": "video.completed", "token_value": "provider-secret",
			"callback": "https://provider.example/content?signature=secret", "nested": map[string]any{"secret": true},
		},
	}}
	accounts := &videoAccountRepoStub{accounts: []Account{{
		ID: *task.AccountID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
	}}}
	svc := NewVideoWebhookService(tasks, accounts, NewVideoProviderRegistry(provider), nil, nil)

	_, err := svc.Handle(context.Background(), VideoProviderOpenAI, *task.AccountID, ProviderWebhookRequest{})

	require.NoError(t, err)
	require.Len(t, tasks.events, 1)
	require.Equal(t, "evt_safe", tasks.events[0].ProviderEventID)
	require.Equal(t, map[string]any{"type": "video.completed"}, tasks.events[0].Payload)
	require.Equal(t, "unknown", tasks.transitions[0].EventPayload["status"])
}

func TestVideoWebhookServiceWakesMatchedTaskWithoutSynchronousProviderGet(t *testing.T) {
	created := true
	task := baseVideoWorkerTask()
	tasks := &videoTaskRepoStub{task: task, eventCreated: &created}
	provider := &videoProviderStub{webhookEvent: &ProviderWebhookEvent{
		ProviderEventID: "evt_completed", ProviderTaskID: *task.ProviderTaskID,
		Status: VideoGenerationCompleted, OccurredAt: time.Now().UTC(),
	}}
	accounts := &videoAccountRepoStub{accounts: []Account{{
		ID: *task.AccountID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
	}}}
	queue := &videoQueueStub{}
	svc := NewVideoWebhookService(tasks, accounts, NewVideoProviderRegistry(provider), nil, queue)

	result, err := svc.Handle(context.Background(), VideoProviderOpenAI, *task.AccountID, ProviderWebhookRequest{})

	require.NoError(t, err)
	require.Same(t, task, result)
	require.Zero(t, provider.getCalls)
	require.Len(t, tasks.transitions, 1)
	require.Equal(t, "provider_webhook_wakeup", tasks.transitions[0].EventType)
	require.NotNil(t, tasks.transitions[0].NextActionAt)
	require.NotEmpty(t, queue.enqueued)
}
