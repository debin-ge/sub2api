package service

import (
	"context"
	"strings"
	"time"
)

type VideoAdminTaskFilter struct {
	Page            int
	PageSize        int
	UserID          *int64
	GroupID         *int64
	AccountID       *int64
	Provider        string
	Operation       string
	GenerationState string
	BillingState    string
	DeleteState     string
	Query           string
	CreatedAfter    *time.Time
	CreatedBefore   *time.Time
}

type VideoAdminTaskPage struct {
	Tasks    []*VideoTask
	Total    int
	Page     int
	PageSize int
}

type VideoAdminResourceFilter struct {
	Page      int
	PageSize  int
	UserID    *int64
	AccountID *int64
	Provider  string
	Status    string
	Query     string
}

type VideoAdminResourcePage struct {
	Resources []*VideoResource
	Total     int
	Page      int
	PageSize  int
}

type VideoAdminEventPage struct {
	Events   []*VideoTaskEvent `json:"items"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

type VideoAdminCallbackFilter struct {
	Page         int
	PageSize     int
	TaskPublicID string
	Status       string
}

type VideoAdminCallbackPage struct {
	Callbacks []*VideoCallbackDelivery
	Total     int
	Page      int
	PageSize  int
}

type VideoAdminOverview struct {
	TasksByGeneration    map[string]int64         `json:"tasks_by_generation"`
	TasksByBilling       map[string]int64         `json:"tasks_by_billing"`
	TasksByDelete        map[string]int64         `json:"tasks_by_delete"`
	CallbacksByStatus    map[string]int64         `json:"callbacks_by_status"`
	TaskStates           []VideoTaskStateSnapshot `json:"task_states"`
	SubmissionUnknown    int64                    `json:"submission_unknown"`
	UnknownHoldAmount    float64                  `json:"unknown_hold_amount"`
	HeldAmount           float64                  `json:"held_amount"`
	UnmatchedWebhooks    int64                    `json:"unmatched_webhooks"`
	OldestTaskPendingAt  *time.Time               `json:"oldest_task_pending_at,omitempty"`
	OldestBillingAt      *time.Time               `json:"oldest_billing_at,omitempty"`
	OldestManualReviewAt *time.Time               `json:"oldest_manual_review_at,omitempty"`
	OldestCallbackAt     *time.Time               `json:"oldest_callback_at,omitempty"`
	Queue                *VideoTaskQueueStats     `json:"queue,omitempty"`
	QueueStatus          string                   `json:"queue_status"`
	Spool                VideoSpoolHealth         `json:"spool"`
}

type VideoAdminRepository interface {
	ListVideoTasksAdmin(context.Context, VideoAdminTaskFilter) (*VideoAdminTaskPage, error)
	GetVideoTaskAdmin(context.Context, string) (*VideoTask, error)
	ListVideoResourcesAdmin(context.Context, VideoAdminResourceFilter) (*VideoAdminResourcePage, error)
	GetVideoResourceAdmin(context.Context, string) (*VideoResource, error)
	ListVideoTaskEventsAdmin(context.Context, string, int, int) (*VideoAdminEventPage, error)
	ListUnmatchedVideoEventsAdmin(context.Context, int, int) (*VideoAdminEventPage, error)
	ListVideoCallbacksAdmin(context.Context, VideoAdminCallbackFilter) (*VideoAdminCallbackPage, error)
	RetryVideoCallbackAdmin(context.Context, int64) (*VideoCallbackDelivery, error)
	GetVideoAdminOverview(context.Context) (*VideoAdminOverview, error)
}

type VideoAdminService struct {
	repository VideoAdminRepository
	tasks      VideoTaskRepository
	taskSvc    *VideoTaskService
	queue      VideoTaskQueue
	catalog    *VideoCapabilityCatalog
	spool      *VideoSubmissionSpool
	probe      *VideoCapabilityProbeService
}

func NewVideoAdminService(repository VideoAdminRepository, tasks VideoTaskRepository, taskSvc *VideoTaskService, queue VideoTaskQueue, catalog *VideoCapabilityCatalog, spool *VideoSubmissionSpool, probe *VideoCapabilityProbeService) *VideoAdminService {
	return &VideoAdminService{repository: repository, tasks: tasks, taskSvc: taskSvc, queue: queue, catalog: catalog, spool: spool, probe: probe}
}

func (s *VideoAdminService) GetAccountCapability(ctx context.Context, accountID int64) (*VideoAccountCapabilityStatus, error) {
	if s == nil || s.probe == nil {
		return nil, ErrVideoInvalidRequest
	}
	return s.probe.Status(ctx, accountID)
}

func (s *VideoAdminService) ProbeAccountCapability(ctx context.Context, accountID int64) (*VideoAccountCapabilityStatus, error) {
	if s == nil || s.probe == nil {
		return nil, ErrVideoInvalidRequest
	}
	return s.probe.ProbeAccount(ctx, accountID)
}

func (s *VideoAdminService) GetCapabilityCatalog(ctx context.Context) (*VideoCapabilityCatalogView, error) {
	if s == nil || s.catalog == nil {
		return nil, ErrVideoInvalidRequest
	}
	err := s.catalog.Refresh(ctx)
	view := s.catalog.View()
	if view != nil {
		if err != nil {
			view.LastRefreshError = err.Error()
		}
		return view, nil
	}
	return nil, err
}

func (s *VideoAdminService) UpdateCapabilityCatalog(ctx context.Context, document VideoCapabilityCatalogDocument) (*VideoCapabilityCatalogView, error) {
	if s == nil || s.catalog == nil {
		return nil, ErrVideoInvalidRequest
	}
	return s.catalog.Update(ctx, document)
}

func (s *VideoAdminService) ListTasks(ctx context.Context, filter VideoAdminTaskFilter) (*VideoAdminTaskPage, error) {
	if s == nil || s.repository == nil {
		return nil, ErrVideoInvalidRequest
	}
	return s.repository.ListVideoTasksAdmin(ctx, filter)
}

func (s *VideoAdminService) GetTask(ctx context.Context, publicID string) (*VideoTask, error) {
	if s == nil || s.repository == nil || !IsValidVideoTaskID(publicID) {
		return nil, ErrVideoInvalidRequest
	}
	task, err := s.repository.GetVideoTaskAdmin(ctx, strings.TrimSpace(publicID))
	if err != nil {
		return nil, err
	}
	if err := validateVideoAdminExpectedVersion(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *VideoAdminService) ListResources(ctx context.Context, filter VideoAdminResourceFilter) (*VideoAdminResourcePage, error) {
	if s == nil || s.repository == nil {
		return nil, ErrVideoInvalidRequest
	}
	return s.repository.ListVideoResourcesAdmin(ctx, filter)
}

func (s *VideoAdminService) GetResource(ctx context.Context, publicID string) (*VideoResource, error) {
	if s == nil || s.repository == nil || !IsValidVideoResourceID(publicID) {
		return nil, ErrVideoInvalidRequest
	}
	return s.repository.GetVideoResourceAdmin(ctx, strings.TrimSpace(publicID))
}

func (s *VideoAdminService) ListEvents(ctx context.Context, publicID string, page, pageSize int) (*VideoAdminEventPage, error) {
	if s == nil || s.repository == nil || !IsValidVideoTaskID(publicID) {
		return nil, ErrVideoInvalidRequest
	}
	return s.repository.ListVideoTaskEventsAdmin(ctx, strings.TrimSpace(publicID), page, pageSize)
}

func (s *VideoAdminService) ListUnmatchedEvents(ctx context.Context, page, pageSize int) (*VideoAdminEventPage, error) {
	if s == nil || s.repository == nil {
		return nil, ErrVideoInvalidRequest
	}
	return s.repository.ListUnmatchedVideoEventsAdmin(ctx, page, pageSize)
}

func (s *VideoAdminService) ListCallbacks(ctx context.Context, filter VideoAdminCallbackFilter) (*VideoAdminCallbackPage, error) {
	if s == nil || s.repository == nil {
		return nil, ErrVideoInvalidRequest
	}
	return s.repository.ListVideoCallbacksAdmin(ctx, filter)
}

func (s *VideoAdminService) Overview(ctx context.Context) (*VideoAdminOverview, error) {
	if s == nil || s.repository == nil {
		return nil, ErrVideoInvalidRequest
	}
	overview, err := s.repository.GetVideoAdminOverview(ctx)
	if err != nil {
		return nil, err
	}
	if reader, ok := s.queue.(VideoTaskQueueStatsReader); ok {
		stats, statsErr := reader.VideoTaskQueueStats(ctx)
		if statsErr == nil {
			overview.Queue = &stats
			overview.QueueStatus = "available"
		} else {
			overview.QueueStatus = "unavailable"
		}
	} else {
		overview.QueueStatus = "unavailable"
	}
	if s.spool != nil {
		overview.Spool = s.spool.Health()
	} else {
		overview.Spool = VideoSpoolHealth{LastSweepResult: "unavailable"}
	}
	return overview, nil
}

func (s *VideoAdminService) ResolveNotCreated(ctx context.Context, publicID string) (*VideoTask, error) {
	return s.proposeSubmissionReview(ctx, strings.TrimSpace(publicID), VideoSubmissionNotCreated, "")
}

func (s *VideoAdminService) ResolveCreated(ctx context.Context, publicID, providerTaskID string) (*VideoTask, error) {
	return s.proposeSubmissionReview(ctx, strings.TrimSpace(publicID), VideoSubmissionCreated, providerTaskID)
}

func (s *VideoAdminService) RetryProviderGet(ctx context.Context, publicID string) (*VideoTask, error) {
	if s == nil || s.taskSvc == nil || !IsValidVideoTaskID(publicID) {
		return nil, ErrVideoInvalidRequest
	}
	task, err := s.GetTask(ctx, publicID)
	if err != nil {
		return nil, err
	}
	if task.BillingState != VideoBillingHeld && task.BillingState != VideoBillingManualReview {
		return nil, ErrVideoInvalidTransition
	}
	return s.taskSvc.RefreshProviderTask(ctx, task)
}

func (s *VideoAdminService) RetrySettlement(ctx context.Context, publicID string) (*VideoTask, error) {
	if s == nil || s.tasks == nil || !IsValidVideoTaskID(publicID) {
		return nil, ErrVideoInvalidRequest
	}
	task, err := s.GetTask(ctx, publicID)
	if err != nil {
		return nil, err
	}
	if task.BillingState != VideoBillingCapturePending && task.BillingState != VideoBillingReleasePending {
		return nil, ErrVideoInvalidTransition
	}
	now := time.Now().UTC()
	task, err = s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		GenerationState: task.GenerationState,
		BillingState:    task.BillingState,
		DeleteState:     task.DeleteState,
		NextActionAt:    &now,
		EventType:       "admin_settlement_retry_requested",
	})
	if err == nil && s.queue != nil {
		_, _ = s.queue.Enqueue(context.WithoutCancel(ctx), task.PublicID)
	}
	return task, err
}

func (s *VideoAdminService) ResolveBillingCapture(ctx context.Context, publicID string, actualUnits float64) (*VideoTask, error) {
	if s == nil || !IsValidVideoTaskID(publicID) {
		return nil, ErrVideoInvalidRequest
	}
	return s.proposeBillingReview(ctx, publicID, BalanceSettlementCapture, actualUnits)
}

func (s *VideoAdminService) ResolveBillingRelease(ctx context.Context, publicID string) (*VideoTask, error) {
	if s == nil || !IsValidVideoTaskID(publicID) {
		return nil, ErrVideoInvalidRequest
	}
	return s.proposeBillingReview(ctx, publicID, BalanceSettlementRelease, 0)
}

func (s *VideoAdminService) RetryDelete(ctx context.Context, publicID string) (*VideoTask, error) {
	if s == nil || s.taskSvc == nil || !IsValidVideoTaskID(publicID) {
		return nil, ErrVideoInvalidRequest
	}
	task, err := s.GetTask(ctx, publicID)
	if err != nil {
		return nil, err
	}
	switch task.DeleteState {
	case VideoDeleteRequested, VideoDeleteDeleting, VideoDeleteFailed:
		now := time.Now().UTC()
		updated, err := s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
			NextActionAt: &now, EventType: "admin_delete_retry_requested",
		})
		if err == nil && s.queue != nil {
			_, _ = s.queue.Enqueue(ctx, task.PublicID)
		}
		return updated, err
	default:
		return nil, ErrVideoInvalidTransition
	}
}

func (s *VideoAdminService) RetryCallback(ctx context.Context, callbackID int64) (*VideoCallbackDelivery, error) {
	if !VideoCallbacksAvailable() {
		return nil, ErrVideoCallbacksDisabled
	}
	if s == nil || s.repository == nil || callbackID <= 0 {
		return nil, ErrVideoInvalidRequest
	}
	return s.repository.RetryVideoCallbackAdmin(ctx, callbackID)
}
