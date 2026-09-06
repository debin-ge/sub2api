package service

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

const (
	VideoSubmissionCreated    = "created"
	VideoSubmissionNotCreated = "not_created"
)

var videoSubmissionProviderID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$`)

func validVideoSubmissionProviderID(value string) bool {
	return videoSubmissionProviderID.MatchString(value) && !videoReviewCredentialPattern.MatchString(value)
}

type VideoSubmissionReviewRequest struct {
	ActorID         int64
	OperationKey    string
	ExpectedVersion int64
	Action          string
	ProviderTaskID  string
	Reason          string
	EvidenceRef     string
}

type VideoSubmissionReview struct {
	ID                     int64           `json:"id"`
	TaskID                 int64           `json:"task_id"`
	Action                 string          `json:"action"`
	ProviderTaskID         *string         `json:"provider_task_id,omitempty"`
	Status                 string          `json:"status"`
	ProposedBy             int64           `json:"proposed_by"`
	DecidedBy              *int64          `json:"decided_by,omitempty"`
	TaskVersion            int64           `json:"task_version"`
	AccountIdentityVersion int64           `json:"account_identity_version"`
	Reason                 string          `json:"reason"`
	EvidenceRef            string          `json:"evidence_ref"`
	Facts                  json.RawMessage `json:"facts"`
	ProviderObservation    json.RawMessage `json:"provider_observation,omitempty"`
	DecisionReason         *string         `json:"decision_reason,omitempty"`
	CreatedAt              time.Time       `json:"created_at"`
	DecidedAt              *time.Time      `json:"decided_at,omitempty"`
}

type VideoSubmissionObservation struct {
	AccountIdentityVersion int64
	Acceptance             VideoProviderAcceptance
	CharacterName          string
}

type VideoSubmissionReviewResult struct {
	Task     *VideoTask
	Review   *VideoSubmissionReview
	Replayed bool
}

type VideoSubmissionReviewRepository interface {
	ProposeVideoSubmissionReview(context.Context, string, VideoSubmissionReviewRequest) (*VideoSubmissionReviewResult, error)
	PrepareVideoSubmissionDecision(context.Context, string, int64, VideoBillingReviewDecision) (*VideoSubmissionReviewResult, error)
	DecideVideoSubmissionReview(context.Context, string, int64, VideoBillingReviewDecision, *VideoSubmissionObservation) (*VideoSubmissionReviewResult, error)
	ListVideoSubmissionReviews(context.Context, string) ([]*VideoSubmissionReview, error)
}

func ValidateVideoSubmissionReviewRequest(request VideoSubmissionReviewRequest) error {
	if err := ValidateVideoBillingReviewRequest(VideoBillingReviewRequest{ActorID: request.ActorID, OperationKey: request.OperationKey,
		ExpectedVersion: request.ExpectedVersion, Reason: request.Reason, EvidenceRef: request.EvidenceRef, Action: BalanceSettlementRelease}); err != nil {
		return err
	}
	if (request.Action != VideoSubmissionCreated && request.Action != VideoSubmissionNotCreated) ||
		(request.Action == VideoSubmissionCreated && !validVideoSubmissionProviderID(request.ProviderTaskID)) ||
		(request.Action == VideoSubmissionNotCreated && request.ProviderTaskID != "") {
		return ErrVideoInvalidRequest
	}
	return nil
}

func (s *VideoAdminService) proposeSubmissionReview(ctx context.Context, publicID, action, providerTaskID string) (*VideoTask, error) {
	if s == nil || !IsValidVideoTaskID(publicID) {
		return nil, ErrVideoInvalidRequest
	}
	audit, ok := ctx.Value(videoBillingReviewContextKey{}).(VideoBillingReviewRequest)
	expected, versioned := ctx.Value(videoAdminVersionContextKey{}).(videoAdminVersion)
	repository, available := s.repository.(VideoSubmissionReviewRepository)
	if !ok || !versioned || expected.PublicID != publicID || !available {
		return nil, ErrVideoReviewRequired
	}
	result, err := repository.ProposeVideoSubmissionReview(ctx, publicID, VideoSubmissionReviewRequest{ActorID: audit.ActorID,
		OperationKey: audit.OperationKey, ExpectedVersion: expected.Version, Action: action, ProviderTaskID: strings.TrimSpace(providerTaskID),
		Reason: audit.Reason, EvidenceRef: audit.EvidenceRef})
	if err != nil {
		return nil, err
	}
	return result.Task, nil
}

func (s *VideoAdminService) ListSubmissionReviews(ctx context.Context, publicID string) ([]*VideoSubmissionReview, error) {
	if s == nil || !IsValidVideoTaskID(publicID) {
		return nil, ErrVideoInvalidRequest
	}
	repository, available := s.repository.(VideoSubmissionReviewRepository)
	if !available {
		return nil, ErrVideoReviewRequired
	}
	return repository.ListVideoSubmissionReviews(ctx, publicID)
}

func (s *VideoAdminService) DecideSubmissionReview(ctx context.Context, publicID string, reviewID int64, approve bool) (*VideoTask, error) {
	if s == nil || !IsValidVideoTaskID(publicID) || reviewID <= 0 {
		return nil, ErrVideoInvalidRequest
	}
	audit, ok := ctx.Value(videoBillingReviewContextKey{}).(VideoBillingReviewRequest)
	expected, versioned := ctx.Value(videoAdminVersionContextKey{}).(videoAdminVersion)
	repository, available := s.repository.(VideoSubmissionReviewRepository)
	if !ok || !versioned || expected.PublicID != publicID || !available {
		return nil, ErrVideoReviewRequired
	}
	decision := VideoBillingReviewDecision{ActorID: audit.ActorID, OperationKey: audit.OperationKey,
		ExpectedVersion: expected.Version, Approve: approve, Reason: audit.Reason}
	prepared, err := repository.PrepareVideoSubmissionDecision(ctx, publicID, reviewID, decision)
	if err != nil {
		return nil, err
	}
	if prepared.Replayed {
		return prepared.Task, nil
	}
	var observation *VideoSubmissionObservation
	if approve && prepared.Review.Action == VideoSubmissionCreated {
		observation, err = s.taskSvc.observeSubmissionCreated(ctx, prepared.Task, prepared.Review)
		if err != nil {
			return nil, err
		}
	}
	result, err := repository.DecideVideoSubmissionReview(ctx, publicID, reviewID, decision, observation)
	if err != nil {
		return nil, err
	}
	if approve && s.queue != nil {
		_, _ = s.queue.Enqueue(context.WithoutCancel(ctx), publicID)
	}
	return result.Task, nil
}

func (s *VideoTaskService) observeSubmissionCreated(ctx context.Context, task *VideoTask, review *VideoSubmissionReview) (*VideoSubmissionObservation, error) {
	if s == nil || s.accounts == nil || s.providers == nil || task == nil || task.AccountID == nil || review == nil || review.ProviderTaskID == nil {
		return nil, ErrVideoInvalidRequest
	}
	timeout := 30 * time.Second
	if s.cfg != nil && s.cfg.Gateway.Video.WorkerRequestTimeoutSeconds > 0 {
		timeout = time.Duration(s.cfg.Gateway.Video.WorkerRequestTimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	providerTaskID := *review.ProviderTaskID
	if !validVideoSubmissionProviderID(providerTaskID) {
		return nil, ErrVideoInvalidRequest
	}
	account, err := s.accounts.GetByID(ctx, *task.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.ProviderIdentityVersion != review.AccountIdentityVersion {
		return nil, ErrVideoReviewConflict
	}
	if (task.AccountOwnerUserID != nil && *task.AccountOwnerUserID != task.UserID) ||
		(account.OwnerUserID != nil && *account.OwnerUserID != task.UserID) ||
		(account.VideoOwnerUserID != nil && *account.VideoOwnerUserID != task.UserID) {
		return nil, ErrVideoReviewForbidden
	}
	provider, ok := s.providers.Get(task.Provider)
	if !ok {
		return nil, ErrVideoProviderUnsupported
	}
	var observed *ProviderVideoTask
	name := ""
	if task.Operation == VideoOperationCharacterCreate {
		characterProvider, supported := provider.(VideoCharacterProvider)
		if !supported || !provider.Capabilities().Supports(VideoCapabilityCharacters) {
			return nil, ErrVideoCapabilityUnsupported
		}
		resource, err := characterProvider.GetCharacter(ctx, account, ProviderResourceRef{Provider: task.Provider, AccountID: account.ID, ProviderResourceID: providerTaskID})
		if err != nil {
			return nil, err
		}
		if resource == nil || strings.TrimSpace(resource.ProviderResourceID) != providerTaskID {
			return nil, ErrVideoInvalidRequest
		}
		name, _ = resource.Metadata["name"].(string)
		observed = &ProviderVideoTask{ProviderTaskID: providerTaskID, Status: VideoGenerationCompleted, RawStatus: resource.Status,
			Metadata: resource.Metadata, ContentExpiresAt: resource.ExpiresAt, ProviderFinishedAt: timePointer(s.now().UTC()), Access: resource.Access}
	} else {
		observed, err = provider.Get(ctx, account, ProviderTaskRef{Provider: task.Provider, AccountID: account.ID, ProviderTaskID: providerTaskID})
		if err != nil {
			return nil, err
		}
		if observed == nil || strings.TrimSpace(observed.ProviderTaskID) != providerTaskID {
			return nil, ErrVideoInvalidRequest
		}
	}
	acceptance, err := s.videoProviderAcceptance(observed)
	if err != nil {
		return nil, err
	}
	if !IsVideoGenerationTerminal(acceptance.GenerationState) && updatedBillingCanResume(task) {
		acceptance.BillingState = VideoBillingHeld
	}
	s.applyTerminalBillingToAcceptance(task, observed, &acceptance)
	return &VideoSubmissionObservation{AccountIdentityVersion: account.ProviderIdentityVersion, Acceptance: acceptance, CharacterName: strings.TrimSpace(name)}, nil
}

func (s *VideoAdminService) RetryCharacterResource(ctx context.Context, publicID string) (*VideoTask, error) {
	if s == nil || s.taskSvc == nil {
		return nil, ErrVideoInvalidRequest
	}
	task, err := s.GetTask(ctx, publicID)
	if err != nil {
		return nil, err
	}
	if task.Operation != VideoOperationCharacterCreate || task.ProviderTaskID == nil || task.GenerationState != VideoGenerationCompleted ||
		task.BillingState != VideoBillingManualReview || (videoStringValue(task.LastErrorCode) != "resource_persistence_pending" && videoStringValue(task.LastErrorCode) != "resource_persistence_failed") {
		return nil, ErrVideoInvalidTransition
	}
	return s.taskSvc.ResolveSubmissionUnknownCreated(ctx, publicID, *task.ProviderTaskID)
}
