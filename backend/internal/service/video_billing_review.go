package service

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrVideoReviewRequired             = infraerrors.BadRequest("VIDEO_REVIEW_REQUIRED", "an attributed billing review with reason, evidence and idempotency key is required")
	ErrVideoReviewConflict             = infraerrors.Conflict("VIDEO_REVIEW_CONFLICT", "billing review changed or idempotency key conflicts; refresh before retrying")
	ErrVideoReviewForbidden            = infraerrors.Forbidden("VIDEO_REVIEW_FORBIDDEN", "an active administrator with independent approval is required")
	ErrVideoReviewIntentExists         = infraerrors.Conflict("VIDEO_REVIEW_INTENT_EXISTS", "an immutable financial intent already exists; recover it instead of changing the decision")
	ErrVideoReviewQuoteAcknowledgement = infraerrors.BadRequest("VIDEO_REVIEW_QUOTE_ACKNOWLEDGEMENT", "explicitly acknowledge honoring the frozen quote despite the specification conflict")
)

const VideoBillingReviewDefaultThresholdUSD = 100.0

var videoReviewOpaqueReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{2,127}$`)
var videoReviewCredentialPattern = regexp.MustCompile(`(?i)(^|[^a-z0-9])sk-[a-z0-9_-]{8,}`)

type VideoBillingReviewRequest struct {
	ActorID              int64
	OperationKey         string
	ExpectedVersion      int64
	Action               BalanceSettlementAction
	ActualUnits          float64
	Reason               string
	EvidenceRef          string
	HonorFrozenQuote     bool
	ApprovalThresholdUSD float64
}

type VideoBillingReviewDecision struct {
	ActorID         int64
	OperationKey    string
	ExpectedVersion int64
	Approve         bool
	Reason          string
}

type VideoBillingReview struct {
	ID                   int64                   `json:"id"`
	SubmissionReviewID   *int64                  `json:"submission_review_id,omitempty"`
	TaskID               int64                   `json:"task_id"`
	Action               BalanceSettlementAction `json:"action"`
	Status               string                  `json:"status"`
	ProposedBy           int64                   `json:"proposed_by"`
	DecidedBy            *int64                  `json:"decided_by,omitempty"`
	TaskVersion          int64                   `json:"task_version"`
	BillingModel         string                  `json:"billing_model"`
	ActualUnits          float64                 `json:"actual_units"`
	ActualCost           float64                 `json:"actual_cost"`
	HoldAmount           float64                 `json:"hold_amount"`
	Reason               string                  `json:"reason"`
	EvidenceRef          string                  `json:"evidence_ref"`
	HonorFrozenQuote     bool                    `json:"honor_frozen_quote"`
	RequiresSecondActor  bool                    `json:"requires_second_actor"`
	ApprovalThresholdUSD float64                 `json:"approval_threshold_usd"`
	DecisionReason       *string                 `json:"decision_reason,omitempty"`
	CreatedAt            time.Time               `json:"created_at"`
	DecidedAt            *time.Time              `json:"decided_at,omitempty"`
	Facts                json.RawMessage         `json:"facts"`
}

type VideoBillingReviewResult struct {
	Review   *VideoBillingReview
	Task     *VideoTask
	Replayed bool
}

type VideoBillingReviewRepository interface {
	ProposeVideoBillingReview(context.Context, string, VideoBillingReviewRequest) (*VideoBillingReviewResult, error)
	DecideVideoBillingReview(context.Context, string, int64, VideoBillingReviewDecision) (*VideoBillingReviewResult, error)
	ListVideoBillingReviews(context.Context, string) ([]*VideoBillingReview, error)
}

type VideoBillingReviewAuthorizationRepository interface {
	VerifyVideoBillingReview(context.Context, *VideoTask) (*VideoBillingReview, error)
}

type videoBillingReviewContextKey struct{}

func WithVideoBillingReviewRequest(ctx context.Context, request VideoBillingReviewRequest) context.Context {
	return context.WithValue(ctx, videoBillingReviewContextKey{}, request)
}

func validVideoReviewText(value string) bool {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 4 || len(value) > 1024 || strings.Contains(value, "://") ||
		strings.Contains(strings.ToLower(value), "bearer ") || videoReviewCredentialPattern.MatchString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func ValidateVideoBillingReviewRequest(request VideoBillingReviewRequest) error {
	if request.ActorID <= 0 || request.ExpectedVersion < 0 || !videoReviewOpaqueReference.MatchString(request.OperationKey) ||
		!videoReviewOpaqueReference.MatchString(request.EvidenceRef) || strings.Contains(request.EvidenceRef, "://") ||
		videoReviewCredentialPattern.MatchString(request.EvidenceRef) || videoReviewCredentialPattern.MatchString(request.OperationKey) ||
		!validVideoReviewText(request.Reason) || !finiteNonNegative(request.ApprovalThresholdUSD) || request.ApprovalThresholdUSD >= 1e10 ||
		!finiteNonNegative(request.ActualUnits) || request.ActualUnits > 1e9 ||
		(request.Action != BalanceSettlementCapture && request.Action != BalanceSettlementRelease) {
		return ErrVideoReviewRequired
	}
	return nil
}

func ValidateVideoBillingReviewDecision(decision VideoBillingReviewDecision) error {
	if decision.ActorID <= 0 || decision.ExpectedVersion < 0 || !videoReviewOpaqueReference.MatchString(decision.OperationKey) || !validVideoReviewText(decision.Reason) {
		return ErrVideoReviewRequired
	}
	if videoReviewCredentialPattern.MatchString(decision.OperationKey) {
		return ErrVideoReviewRequired
	}
	return nil
}

func PlanVideoBillingReview(task *VideoTask, request VideoBillingReviewRequest) (*VideoBillingReview, error) {
	if err := ValidateVideoBillingReviewRequest(request); err != nil {
		return nil, err
	}
	if task == nil || task.BillingState != VideoBillingManualReview || !IsVideoGenerationTerminal(task.GenerationState) ||
		task.APIKeyID == nil || task.AccountID == nil || task.HoldAmount == nil || !finiteNonNegative(*task.HoldAmount) || *task.HoldAmount >= 1e10 {
		return nil, ErrVideoInvalidTransition
	}
	if task.Operation == VideoOperationCharacterCreate &&
		(videoStringValue(task.LastErrorCode) == "resource_persistence_pending" || videoStringValue(task.LastErrorCode) == "resource_persistence_failed") {
		return nil, ErrVideoInvalidTransition
	}
	review := &VideoBillingReview{TaskID: task.ID, Action: request.Action, Status: "pending", ProposedBy: request.ActorID,
		TaskVersion: task.Version, HoldAmount: *task.HoldAmount, Reason: strings.TrimSpace(request.Reason), EvidenceRef: strings.TrimSpace(request.EvidenceRef),
		HonorFrozenQuote: request.HonorFrozenQuote, ApprovalThresholdUSD: request.ApprovalThresholdUSD}
	if request.Action == BalanceSettlementRelease {
		if task.GenerationState == VideoGenerationCompleted || request.ActualUnits != 0 || request.HonorFrozenQuote {
			return nil, ErrVideoInvalidTransition
		}
	} else {
		if task.BillingUnit == nil {
			return nil, ErrVideoInvalidRequest
		}
		unit := *task.BillingUnit
		if (unit != VideoBillingUnitRequest && unit != VideoBillingUnitSecond && unit != VideoBillingUnitVideoToken) ||
			(unit == VideoBillingUnitRequest && request.ActualUnits != 0 && request.ActualUnits != 1) ||
			(unit == VideoBillingUnitVideoToken && math.Trunc(request.ActualUnits) != request.ActualUnits) {
			return nil, ErrVideoInvalidRequest
		}
		accountMultiplier, err := videoAccountSettlementMultiplier(task)
		if err != nil {
			return nil, err
		}
		if err := videoCheckObservedSpecification(task, task.ResponseMetadata); err != nil {
			if err != ErrVideoSourceSpecConflict {
				return nil, err
			}
			if !request.HonorFrozenQuote {
				return nil, ErrVideoReviewQuoteAcknowledgement
			}
		}
		price, validPrice := numericMapValue(task.PriceSnapshot, "unit_price")
		multiplier, validMultiplier := numericMapValue(task.PriceSnapshot, "customer_multiplier")
		if !validPrice || !validMultiplier || !finiteNonNegative(price) || !finiteNonNegative(multiplier) {
			return nil, ErrVideoPricingInvalid
		}
		review.ActualUnits = QuantizeUsageBillingAmount(request.ActualUnits)
		baseCost := price * review.ActualUnits
		accountCost := QuantizeUsageBillingAmount(baseCost * accountMultiplier)
		if !finiteNonNegative(baseCost) || baseCost >= 1e10 || !finiteNonNegative(accountCost) || accountCost >= 1e10 {
			return nil, ErrVideoInvalidRequest
		}
		review.ActualCost = QuantizeUsageBillingAmount(baseCost * multiplier)
		if !finiteNonNegative(review.ActualCost) || review.ActualCost >= 1e10 {
			return nil, ErrVideoInvalidRequest
		}
	}
	review.RequiresSecondActor = review.HonorFrozenQuote || review.ActualCost > review.HoldAmount ||
		review.HoldAmount >= review.ApprovalThresholdUSD || review.ActualCost >= review.ApprovalThresholdUSD ||
		(request.Action == BalanceSettlementCapture && review.ActualUnits == 0 && review.HoldAmount > 0)
	return review, nil
}

func (s *VideoAdminService) proposeBillingReview(ctx context.Context, publicID string, action BalanceSettlementAction, units float64) (*VideoTask, error) {
	request, ok := ctx.Value(videoBillingReviewContextKey{}).(VideoBillingReviewRequest)
	expected, versioned := ctx.Value(videoAdminVersionContextKey{}).(videoAdminVersion)
	if !ok || !versioned || expected.PublicID != publicID {
		return nil, ErrVideoReviewRequired
	}
	repository, available := s.repository.(VideoBillingReviewRepository)
	if !available {
		return nil, ErrVideoReviewRequired
	}
	request.Action, request.ActualUnits, request.ExpectedVersion = action, units, expected.Version
	request.ApprovalThresholdUSD = VideoBillingReviewDefaultThresholdUSD
	if s.taskSvc != nil && s.taskSvc.cfg != nil {
		request.ApprovalThresholdUSD = s.taskSvc.cfg.Gateway.Video.ManualReviewThresholdUSD
	}
	request.ApprovalThresholdUSD = QuantizeUsageBillingAmount(request.ApprovalThresholdUSD)
	result, err := repository.ProposeVideoBillingReview(ctx, publicID, request)
	if err != nil {
		return nil, err
	}
	if result.Review.Status == "approved" && s.queue != nil {
		_, _ = s.queue.Enqueue(context.WithoutCancel(ctx), publicID)
	}
	return result.Task, nil
}

func (s *VideoAdminService) ListBillingReviews(ctx context.Context, publicID string) ([]*VideoBillingReview, error) {
	if s == nil || !IsValidVideoTaskID(publicID) {
		return nil, ErrVideoInvalidRequest
	}
	repository, available := s.repository.(VideoBillingReviewRepository)
	if !available {
		return nil, ErrVideoReviewRequired
	}
	return repository.ListVideoBillingReviews(ctx, publicID)
}

func (s *VideoAdminService) DecideBillingReview(ctx context.Context, publicID string, reviewID int64, approve bool) (*VideoTask, error) {
	request, ok := ctx.Value(videoBillingReviewContextKey{}).(VideoBillingReviewRequest)
	expected, versioned := ctx.Value(videoAdminVersionContextKey{}).(videoAdminVersion)
	if s == nil || !ok || !versioned || expected.PublicID != publicID || reviewID <= 0 {
		return nil, ErrVideoReviewRequired
	}
	repository, available := s.repository.(VideoBillingReviewRepository)
	if !available {
		return nil, ErrVideoReviewRequired
	}
	result, err := repository.DecideVideoBillingReview(ctx, publicID, reviewID, VideoBillingReviewDecision{
		ActorID: request.ActorID, OperationKey: request.OperationKey, ExpectedVersion: expected.Version, Approve: approve, Reason: request.Reason,
	})
	if err != nil {
		return nil, err
	}
	if result.Review.Status == "approved" && s.queue != nil {
		_, _ = s.queue.Enqueue(context.WithoutCancel(ctx), publicID)
	}
	return result.Task, nil
}
