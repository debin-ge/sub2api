package service

import (
	"context"
	"encoding/json"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"time"
)

var (
	ErrGrokVideoNotFound    = infraerrors.NotFound("GROK_VIDEO_NOT_FOUND", "grok video task not found")
	ErrGrokVideoConflict    = infraerrors.Conflict("GROK_VIDEO_CONFLICT", "grok video idempotency or ownership conflict")
	ErrGrokVideoBusy        = infraerrors.Conflict("GROK_VIDEO_BUSY", "grok video task is being processed")
	ErrGrokVideoUnavailable = infraerrors.ServiceUnavailable("GROK_VIDEO_UNAVAILABLE", "grok video task account is unavailable")
)

type GrokVideoTask struct {
	ID                     int64                     `json:"id"`
	PublicID               string                    `json:"public_id"`
	UserID                 int64                     `json:"user_id"`
	APIKeyID               int64                     `json:"api_key_id"`
	GroupID                int64                     `json:"group_id"`
	AccountID              int64                     `json:"account_id"`
	Operation              string                    `json:"operation"`
	Endpoint               string                    `json:"endpoint"`
	RequestHash            string                    `json:"request_hash"`
	IdempotencyKey         *string                   `json:"idempotency_key"`
	ProviderTaskID         *string                   `json:"provider_task_id"`
	RequestedModel         string                    `json:"requested_model"`
	UpstreamModel          string                    `json:"upstream_model"`
	Prompt                 string                    `json:"prompt"`
	Resolution             string                    `json:"resolution"`
	Duration               int                       `json:"duration"`
	AspectRatio            string                    `json:"aspect_ratio"`
	GenerateAudio          *bool                     `json:"generate_audio,omitempty"`
	InputManifest          []VideoInputManifestEntry `json:"input_manifest,omitempty"`
	InputCount             int                       `json:"input_count"`
	GenerationState        string                    `json:"generation_state"`
	ProviderStatus         string                    `json:"provider_status"`
	SyncStatus             string                    `json:"sync_status"`
	SettlementStatus       string                    `json:"settlement_status"`
	Progress               float64                   `json:"progress"`
	ActualDuration         int                       `json:"actual_duration"`
	UpstreamCostTicks      json.Number               `json:"upstream_cost_ticks,omitempty"`
	ActualCost             *float64                  `json:"actual_cost,omitempty"`
	UsageLogID             *int64                    `json:"usage_log_id,omitempty"`
	PrivateEnc             string                    `json:"private_enc"`
	SnapshotEnc            string                    `json:"snapshot_enc,omitempty"`
	ResponseEnc            string                    `json:"response_enc,omitempty"`
	CreateResponseEnc      string                    `json:"create_response_enc,omitempty"`
	CreateStatus           int                       `json:"create_status"`
	ImportedFromLegacy     bool                      `json:"imported_from_legacy"`
	BillingSnapshotMissing bool                      `json:"billing_snapshot_missing"`
	ObservationDeadline    *time.Time                `json:"observation_deadline,omitempty"`
	PollAttempts           int                       `json:"poll_attempts"`
	LastErrorCode          string                    `json:"last_error_code"`
	CreatedAt              time.Time                 `json:"created_at"`
	UpdatedAt              time.Time                 `json:"updated_at"`
	SubmittedAt            *time.Time                `json:"submitted_at"`
	FinishedAt             *time.Time                `json:"finished_at"`
	SettledAt              *time.Time                `json:"settled_at"`
	LastPolledAt           *time.Time                `json:"last_polled_at"`
	NextActionAt           *time.Time                `json:"next_action_at"`
	Version                int64                     `json:"version"`
	LeaseEpoch             int64                     `json:"lease_epoch"`
	LeaseOwner             *string                   `json:"lease_owner"`
	LeaseExpiresAt         *time.Time                `json:"lease_expires_at"`
}

// Sensitive creation attribution is encrypted, never exposed through admin DTOs.
type grokVideoPrivate struct {
	Group         *Group
	Subscription  *UserSubscription
	QuotaPlatform string
	ChannelUsageFields
	UserAgent string
	IPAddress string
	SessionID string
}

type PreparedGrokVideoBilling struct {
	Command  *UsageBillingCommand
	UsageLog *UsageLog
	Simple   bool
}

type GrokVideoTaskRepository interface {
	Create(context.Context, *GrokVideoTask) (*GrokVideoTask, bool, error)
	Get(context.Context, string) (*GrokVideoTask, error)
	FindOwned(context.Context, int64, int64, int64, string) (*GrokVideoTask, error)
	FindIdempotent(context.Context, int64, int64, string, string) (*GrokVideoTask, error)
	Save(context.Context, *GrokVideoTask, string) error
	Claim(context.Context, string, string, bool) (*GrokVideoTask, error)
	Due(context.Context, int) ([]string, error)
	ExistingBilling(context.Context, string, int64) (*PreparedGrokVideoBilling, error)
	RecordedUsage(context.Context, string, int64) (*UsageLog, error)
	RecordSimpleUsage(context.Context, *UsageLog) error
}
