package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrUsageBillingRequestIDRequired = errors.New("usage billing request_id is required")
var ErrUsageBillingRequestConflict = errors.New("usage billing request fingerprint conflict")
var ErrUsageBillingPlatformQuotaSnapshotRequired = errors.New("usage billing platform quota snapshot is required")
var ErrUsageBillingPayloadInvalid = errors.New("usage billing payload is not persistable")

type UsageBillingPlatformQuotaSnapshot struct {
	DailyUsageUSD      float64
	WeeklyUsageUSD     float64
	MonthlyUsageUSD    float64
	DailyWindowStart   *time.Time
	WeeklyWindowStart  *time.Time
	MonthlyWindowStart *time.Time
}

// UsageBillingCommand describes one billable request that must be applied at most once.
type UsageBillingCommand struct {
	RequestID          string
	APIKeyID           int64
	RequestFingerprint string
	RequestPayloadHash string

	UserID              int64
	AccountID           int64
	SubscriptionID      *int64
	AccountType         string
	Model               string
	ServiceTier         string
	ReasoningEffort     string
	BillingType         int8
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	ImageCount          int
	MediaType           string

	GroupID                     *int64
	Platform                    string
	PlatformQuotaCost           float64
	PlatformQuotaSnapshot       *UsageBillingPlatformQuotaSnapshot
	PlatformQuotaSnapshotNeeded bool
	ActualCost                  float64
	TotalCost                   float64
	IsSubscriptionBilling       bool
	OccurredAt                  time.Time

	BalanceCost         float64
	SubscriptionCost    float64
	APIKeyQuotaCost     float64
	APIKeyRateLimitCost float64
	AccountQuotaCost    float64
}

func (c *UsageBillingCommand) Normalize() {
	if c == nil {
		return
	}
	c.RequestID = strings.TrimSpace(c.RequestID)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = buildUsageBillingFingerprint(c)
	}
}

func buildUsageBillingFingerprint(c *UsageBillingCommand) string {
	if c == nil {
		return ""
	}
	raw := fmt.Sprintf(
		"%d|%d|%d|%s|%s|%s|%s|%d|%d|%d|%d|%d|%d|%s|%d|%d|%s|%t|%0.10f|%0.10f|%0.10f|%0.10f|%0.10f|%0.10f|%0.10f|%0.10f",
		c.UserID,
		c.AccountID,
		c.APIKeyID,
		strings.TrimSpace(c.AccountType),
		strings.TrimSpace(c.Model),
		strings.TrimSpace(c.ServiceTier),
		strings.TrimSpace(c.ReasoningEffort),
		c.BillingType,
		c.InputTokens,
		c.OutputTokens,
		c.CacheCreationTokens,
		c.CacheReadTokens,
		c.ImageCount,
		strings.TrimSpace(c.MediaType),
		valueOrZero(c.SubscriptionID),
		valueOrZero(c.GroupID),
		strings.TrimSpace(c.Platform),
		c.IsSubscriptionBilling,
		c.ActualCost,
		c.TotalCost,
		c.PlatformQuotaCost,
		c.BalanceCost,
		c.SubscriptionCost,
		c.APIKeyQuotaCost,
		c.APIKeyRateLimitCost,
		c.AccountQuotaCost,
	)
	if payloadHash := strings.TrimSpace(c.RequestPayloadHash); payloadHash != "" {
		raw += "|" + payloadHash
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func HashUsageRequestPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func valueOrZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// AccountQuotaState holds the post-increment quota state returned by the DB transaction.
// All values are post-update (i.e., already include the increment).
type AccountQuotaState struct {
	TotalUsed   float64
	TotalLimit  float64
	DailyUsed   float64
	DailyLimit  float64
	WeeklyUsed  float64
	WeeklyLimit float64
}

type UsageBillingApplyResult struct {
	Applied          bool
	UsageLogRecorded bool // true when billing and usage_log were committed atomically
	// ProjectionRepairRequired marks a legacy recovery where an earlier
	// process committed the billing dedup/effects but died before recording
	// usage or completing cache post-effects. Recovery must replay only the
	// idempotent projections; it must not send notifications again.
	ProjectionRepairRequired bool
	APIKeyQuotaExhausted     bool
	NewBalance               *float64           // post-deduction balance (nil = no balance deduction)
	BalanceOverdrafted       bool               // true when the sufficient-balance guard missed and debt was still recorded
	QuotaState               *AccountQuotaState // post-increment quota state (nil = no quota increment)
	OutboxReceipt            *UsageBillingOutboxReceipt
}

type UsageBillingOutboxReceipt struct {
	ID       int64
	WorkerID string
}

// UsageBillingOutboxEvent is one durably persisted billing intent claimed by a
// recovery worker. Command and UsageLog are immutable snapshots captured after
// the upstream request succeeded.
type UsageBillingOutboxEvent struct {
	ID                     int64
	Attempts               int
	Stage                  int8
	Command                *UsageBillingCommand
	UsageLog               *UsageLog
	Result                 *UsageBillingApplyResult
	PayloadValidationError string
	CreatedAt              time.Time
}

// BatchImageBalanceHoldCommand describes an idempotent balance hold operation.
type BatchImageBalanceHoldCommand struct {
	RequestID          string
	APIKeyID           int64
	RequestFingerprint string
	RequestPayloadHash string
	UserID             int64
	BatchID            string
	HoldAmount         float64
	ActualAmount       float64
}

func (c *BatchImageBalanceHoldCommand) Normalize() {
	if c == nil {
		return
	}
	c.RequestID = strings.TrimSpace(c.RequestID)
	c.BatchID = strings.TrimSpace(c.BatchID)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = buildBatchImageBalanceHoldFingerprint(c)
	}
}

func buildBatchImageBalanceHoldFingerprint(c *BatchImageBalanceHoldCommand) string {
	if c == nil {
		return ""
	}
	raw := fmt.Sprintf(
		"%d|%d|%s|%0.10f|%0.10f",
		c.UserID,
		c.APIKeyID,
		strings.TrimSpace(c.BatchID),
		c.HoldAmount,
		c.ActualAmount,
	)
	if payloadHash := strings.TrimSpace(c.RequestPayloadHash); payloadHash != "" {
		raw += "|" + payloadHash
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type BatchImageBalanceHoldResult struct {
	Applied       bool
	NewBalance    *float64
	FrozenBalance *float64
}

type UsageBillingRepository interface {
	Apply(ctx context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error)
	ReserveBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
	CaptureBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
	ReleaseBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
}

// DurableUsageBillingRepository extends UsageBillingRepository with a
// transactional usage-log write and a leased recovery queue. Production
// repositories implement this interface; lightweight test stubs may keep using
// UsageBillingRepository and exercise the legacy fallback path.
type DurableUsageBillingRepository interface {
	UsageBillingRepository

	// ApplyAndRecord first persists an immutable outbox intent, then attempts to
	// apply billing effects and insert the usage log in one transaction. When
	// the second step fails, the returned error is non-nil and the outbox row is
	// retained for recovery.
	ApplyAndRecord(ctx context.Context, cmd *UsageBillingCommand, usageLog *UsageLog) (*UsageBillingApplyResult, error)

	ClaimUsageBillingOutbox(ctx context.Context, workerID string, limit int, lease time.Duration) ([]UsageBillingOutboxEvent, error)
	UpdateUsageBillingOutboxCommand(ctx context.Context, workerID string, eventID int64, cmd *UsageBillingCommand) error
	CompleteUsageBillingOutbox(ctx context.Context, workerID string, event UsageBillingOutboxEvent) (*UsageBillingApplyResult, error)
	AcknowledgeUsageBillingOutbox(ctx context.Context, workerID string, eventID int64) error
	QuarantineUsageBillingOutbox(ctx context.Context, workerID string, eventID int64, reason string) error
	RetryUsageBillingOutbox(ctx context.Context, workerID string, eventID int64, availableAt time.Time, lastError string) error
}
