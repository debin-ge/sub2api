package service

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// These admission slots cover unconfirmed submissions plus unresolved costs,
// across API keys and instances. Acceptance frees a submission slot; only
// independently verified supplier accounting can close a cost record.
const (
	VideoProviderCostMaxUnresolvedPerUser    = 3
	VideoProviderCostMaxUnresolvedPerAccount = 20
	videoProviderCostMaxLookupAttempts       = 12
)

var (
	ErrVideoProviderCostLimited  = infraerrors.New(http.StatusTooManyRequests, "VIDEO_PROVIDER_COST_UNRESOLVED", "video provider costs require reconciliation before more tasks can be created")
	ErrVideoProviderCostConflict = infraerrors.New(http.StatusConflict, "VIDEO_PROVIDER_COST_CONFLICT", "provider cost record changed or was already resolved differently")
)

// VideoProviderCost is a supplier accounting record, not a customer debt or a
// task execution state. Sensitive provider handles are excluded from admin JSON.
type VideoProviderCost struct {
	ID                     int64      `json:"id"`
	TaskID                 *int64     `json:"task_id,omitempty"`
	TaskPublicID           string     `json:"task_public_id"`
	UserID                 int64      `json:"user_id"`
	AccountID              int64      `json:"account_id"`
	Provider               string     `json:"provider"`
	Reason                 string     `json:"reason"`
	State                  string     `json:"state"`
	Version                int64      `json:"version"`
	ProviderTaskID         string     `json:"-"`
	StableClientToken      string     `json:"-"`
	AccountIdentityVersion *int64     `json:"-"`
	Attempts               int        `json:"attempts"`
	ObservationStatus      string     `json:"observation_status,omitempty"`
	LastError              string     `json:"last_error,omitempty"`
	Amount                 *float64   `json:"amount,omitempty"`
	Currency               string     `json:"currency,omitempty"`
	Evidence               string     `json:"evidence,omitempty"`
	ResolvedBy             *int64     `json:"resolved_by,omitempty"`
	ResolutionKey          string     `json:"-"`
	NextActionAt           *time.Time `json:"next_action_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	ResolvedAt             *time.Time `json:"resolved_at,omitempty"`
}

type VideoProviderCostPage struct {
	Items     []*VideoProviderCost `json:"items"`
	NextAfter int64                `json:"next_after"`
	HasMore   bool                 `json:"has_more"`
}

type VideoProviderCostResolution struct {
	ExpectedVersion int64    `json:"expected_version"`
	State           string   `json:"state"`
	Amount          *float64 `json:"amount"`
	Currency        string   `json:"currency"`
	Evidence        string   `json:"evidence"`
	IdempotencyKey  string   `json:"-"`
	ActorID         int64    `json:"-"`
}

func ValidateVideoProviderCostResolution(in VideoProviderCostResolution) error {
	if in.ExpectedVersion < 1 || in.ActorID < 1 || len(strings.TrimSpace(in.IdempotencyKey)) < 1 || len(in.IdempotencyKey) > 128 ||
		in.Amount == nil || math.IsNaN(*in.Amount) || math.IsInf(*in.Amount, 0) || *in.Amount < 0 || *in.Amount > 1e10 ||
		utf8.RuneCountInString(strings.TrimSpace(in.Evidence)) < 20 || len(in.Evidence) > 2048 {
		return ErrVideoInvalidRequest
	}
	if in.Currency != "USD" && in.Currency != "CNY" && in.Currency != "CREDITS" {
		return ErrVideoInvalidRequest
	}
	if (in.State != "resolved_no_charge" && in.State != "resolved_cost_recorded") ||
		(in.State == "resolved_no_charge" && *in.Amount != 0) ||
		(in.State == "resolved_cost_recorded" && QuantizeUsageBillingAmount(*in.Amount) <= 0) {
		return ErrVideoInvalidRequest
	}
	return nil
}

type VideoProviderCostRepository interface {
	ClaimVideoProviderCosts(context.Context, string, int) ([]*VideoProviderCost, error)
	RecordVideoProviderCostObservation(context.Context, *VideoProviderCost, string, string, string, string, *time.Time) error
	ListVideoProviderCosts(context.Context, string, int64, int) (*VideoProviderCostPage, error)
	ResolveVideoProviderCost(context.Context, int64, VideoProviderCostResolution) (*VideoProviderCost, error)
}

func (s *VideoAdminService) ListProviderCosts(ctx context.Context, state string, after int64, limit int) (*VideoProviderCostPage, error) {
	if s == nil {
		return nil, ErrVideoInvalidRequest
	}
	if state != "" && state != "unresolved" && state != "resolved_no_charge" && state != "resolved_cost_recorded" {
		return nil, ErrVideoInvalidRequest
	}
	if after < 0 || limit < 1 || limit > 100 {
		return nil, ErrVideoInvalidRequest
	}
	repo, ok := s.tasks.(VideoProviderCostRepository)
	if !ok {
		return nil, ErrVideoInvalidRequest
	}
	return repo.ListVideoProviderCosts(ctx, state, after, limit)
}

func (s *VideoAdminService) ResolveProviderCost(ctx context.Context, id int64, in VideoProviderCostResolution) (*VideoProviderCost, error) {
	if s == nil {
		return nil, ErrVideoInvalidRequest
	}
	if id <= 0 {
		return nil, ErrVideoInvalidRequest
	}
	if err := ValidateVideoProviderCostResolution(in); err != nil {
		return nil, err
	}
	repo, ok := s.tasks.(VideoProviderCostRepository)
	if !ok {
		return nil, ErrVideoInvalidRequest
	}
	return repo.ResolveVideoProviderCost(ctx, id, in)
}

// Supplier recovery is deliberately isolated from the terminal task state and
// billing settlement. Get/Search may add evidence but can never charge a user,
// create/resume work, deliver a refunded result, or infer zero cost from absence.
func (w *VideoTaskWorker) reconcileProviderCosts(ctx context.Context, limit int) error {
	repo, ok := w.tasks.(VideoProviderCostRepository)
	if !ok {
		return nil
	}
	if limit > 4 {
		limit = 4
	}
	items, err := repo.ClaimVideoProviderCosts(ctx, w.workerID, limit)
	if err != nil {
		return err
	}
	var combined error
	for _, item := range items {
		itemCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		providerID, status, reason := w.observeProviderCost(itemCtx, item)
		cancel()
		var next *time.Time
		if item.Attempts < videoProviderCostMaxLookupAttempts && !IsVideoGenerationTerminal(status) &&
			reason != "lookup_unsupported" && reason != "account_identity_unverified" {
			at := time.Now().UTC().Add(30 * time.Minute)
			next = &at
		}
		if reason == "" && next == nil && status == "" {
			reason = "lookup_exhausted"
		}
		if err := repo.RecordVideoProviderCostObservation(ctx, item, w.workerID, providerID, status, reason, next); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func (w *VideoTaskWorker) observeProviderCost(ctx context.Context, item *VideoProviderCost) (string, string, string) {
	if item == nil || w.accounts == nil || w.providers == nil {
		return "", "", "recovery_unavailable"
	}
	account, err := w.accounts.GetByID(ctx, item.AccountID)
	if err != nil || account == nil {
		return "", "", "account_unavailable"
	}
	if item.AccountIdentityVersion == nil || *item.AccountIdentityVersion != account.ProviderIdentityVersion {
		return "", "", "account_identity_unverified"
	}
	provider, ok := w.providers.Get(item.Provider)
	if !ok {
		return "", "", "lookup_unsupported"
	}
	var observed *ProviderVideoTask
	if item.ProviderTaskID != "" {
		observed, err = provider.Get(ctx, account, ProviderTaskRef{Provider: item.Provider, AccountID: item.AccountID, ProviderTaskID: item.ProviderTaskID})
	} else if searcher, ok := provider.(VideoTaskSearcher); ok && item.StableClientToken != "" && provider.Capabilities().Operations[VideoCapabilityTaskSearch] {
		observed, err = searcher.SearchByClientToken(ctx, account, item.StableClientToken)
	} else {
		return "", "", "lookup_unsupported"
	}
	if err != nil {
		return "", "", "lookup_failed"
	}
	if observed == nil {
		return "", "", "not_observed"
	}
	if !validVideoProviderIdentifier(observed.ProviderTaskID) {
		return "", "", "invalid_provider_identity"
	}
	return observed.ProviderTaskID, normalizedVideoProviderGenerationState(observed.Status, VideoGenerationInProgress), ""
}
