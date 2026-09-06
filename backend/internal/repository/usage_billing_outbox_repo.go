package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	usageBillingOutboxPayloadVersionV1 = 1
	usageBillingOutboxPayloadVersionV2 = 2
	usageBillingOutboxPayloadVersionV3 = 3
	usageBillingOutboxPayloadVersionV4 = 4
	// Legacy alias retained for v1 golden tests and compatibility helpers.
	usageBillingOutboxPayloadVersion = usageBillingOutboxPayloadVersionV1
	usageBillingInlineRetryDelay     = time.Second
	usageBillingOutboxStageBilling   = int8(0)
	usageBillingOutboxStageEffects   = int8(1)

	usageBillingPostgresIntegerMax         = int64(1<<31 - 1)
	usageBillingNumeric20Scale10UpperBound = 1e10
	usageBillingNumeric10Scale4Max         = 999999.9999
)

type usageBillingCommandPayloadV1 struct {
	RequestID          string `json:"request_id"`
	APIKeyID           int64  `json:"api_key_id"`
	RequestFingerprint string `json:"request_fingerprint"`
	RequestPayloadHash string `json:"request_payload_hash,omitempty"`

	UserID              int64  `json:"user_id"`
	AccountID           int64  `json:"account_id"`
	SubscriptionID      *int64 `json:"subscription_id,omitempty"`
	AccountType         string `json:"account_type,omitempty"`
	Model               string `json:"model,omitempty"`
	ServiceTier         string `json:"service_tier,omitempty"`
	ReasoningEffort     string `json:"reasoning_effort,omitempty"`
	BillingType         int8   `json:"billing_type"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	CacheCreationTokens int    `json:"cache_creation_tokens"`
	CacheReadTokens     int    `json:"cache_read_tokens"`
	ImageCount          int    `json:"image_count"`
	MediaType           string `json:"media_type,omitempty"`

	GroupID                     *int64                                     `json:"group_id,omitempty"`
	Platform                    string                                     `json:"platform,omitempty"`
	PlatformQuotaCost           float64                                    `json:"platform_quota_cost"`
	PlatformQuotaSnapshot       *service.UsageBillingPlatformQuotaSnapshot `json:"platform_quota_snapshot,omitempty"`
	PlatformQuotaSnapshotNeeded bool                                       `json:"platform_quota_snapshot_needed,omitempty"`
	ActualCost                  float64                                    `json:"actual_cost"`
	TotalCost                   float64                                    `json:"total_cost"`
	IsSubscriptionBilling       bool                                       `json:"is_subscription_billing"`
	OccurredAt                  time.Time                                  `json:"occurred_at"`
	QuotaTime                   *service.UsageBillingQuotaTime             `json:"quota_time,omitempty"`

	BalanceCost         float64 `json:"balance_cost"`
	SubscriptionCost    float64 `json:"subscription_cost"`
	APIKeyQuotaCost     float64 `json:"api_key_quota_cost"`
	APIKeyRateLimitCost float64 `json:"api_key_rate_limit_cost"`
	AccountQuotaCost    float64 `json:"account_quota_cost"`

	InvalidNumerics []usageBillingNumericIssueV1 `json:"invalid_numerics,omitempty"`
}

type balanceSettlementPayloadV2 struct {
	BillingReviewID    int64                           `json:"billing_review_id,omitempty"`
	TaskID             int64                           `json:"video_task_id"`
	Action             service.BalanceSettlementAction `json:"action"`
	SettlementScope    service.BalanceHoldScope        `json:"settlement_scope"`
	SettlementRefID    string                          `json:"settlement_ref_id"`
	RequestID          string                          `json:"request_id"`
	APIKeyID           int64                           `json:"api_key_id"`
	RequestFingerprint string                          `json:"request_fingerprint"`
	RequestPayloadHash string                          `json:"request_payload_hash,omitempty"`
	UserID             int64                           `json:"user_id"`
	HoldAmount         float64                         `json:"hold_amount"`
	ActualAmount       float64                         `json:"actual_amount"`
	AllowOverCapture   bool                            `json:"allow_over_capture"`
	Billing            *usageBillingCommandPayloadV1   `json:"billing,omitempty"`
	InvalidNumerics    []usageBillingNumericIssueV1    `json:"invalid_numerics,omitempty"`
}

type usageBillingNumericIssueV1 struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
	Value  string `json:"value"`
}

type usageLogPayloadV1 struct {
	UserID    int64  `json:"user_id"`
	APIKeyID  int64  `json:"api_key_id"`
	AccountID int64  `json:"account_id"`
	RequestID string `json:"request_id"`
	Model     string `json:"model"`

	RequestedModel    string  `json:"requested_model,omitempty"`
	UpstreamModel     *string `json:"upstream_model,omitempty"`
	ChannelID         *int64  `json:"channel_id,omitempty"`
	ModelMappingChain *string `json:"model_mapping_chain,omitempty"`
	BillingTier       *string `json:"billing_tier,omitempty"`
	BillingMode       *string `json:"billing_mode,omitempty"`
	ServiceTier       *string `json:"service_tier,omitempty"`
	ReasoningEffort   *string `json:"reasoning_effort,omitempty"`
	InboundEndpoint   *string `json:"inbound_endpoint,omitempty"`
	UpstreamEndpoint  *string `json:"upstream_endpoint,omitempty"`

	GroupID        *int64 `json:"group_id,omitempty"`
	SubscriptionID *int64 `json:"subscription_id,omitempty"`

	InputTokens           int `json:"input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	CacheCreationTokens   int `json:"cache_creation_tokens"`
	CacheReadTokens       int `json:"cache_read_tokens"`
	CacheCreation5mTokens int `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int `json:"cache_creation_1h_tokens"`

	ImageInputTokens  int     `json:"image_input_tokens"`
	ImageInputCost    float64 `json:"image_input_cost"`
	ImageOutputTokens int     `json:"image_output_tokens"`
	ImageOutputCost   float64 `json:"image_output_cost"`

	InputCost                 float64  `json:"input_cost"`
	OutputCost                float64  `json:"output_cost"`
	CacheCreationCost         float64  `json:"cache_creation_cost"`
	CacheReadCost             float64  `json:"cache_read_cost"`
	TotalCost                 float64  `json:"total_cost"`
	ActualCost                float64  `json:"actual_cost"`
	RateMultiplier            float64  `json:"rate_multiplier"`
	LongContextBillingApplied bool     `json:"long_context_billing_applied"`
	AccountRateMultiplier     *float64 `json:"account_rate_multiplier,omitempty"`
	AccountStatsCost          *float64 `json:"account_stats_cost,omitempty"`

	BillingType  int8                `json:"billing_type"`
	BillingState int8                `json:"billing_state"`
	RequestType  service.RequestType `json:"request_type"`
	Stream       bool                `json:"stream"`
	OpenAIWSMode bool                `json:"openai_ws_mode"`
	DurationMs   *int                `json:"duration_ms,omitempty"`
	FirstTokenMs *int                `json:"first_token_ms,omitempty"`
	UserAgent    *string             `json:"user_agent,omitempty"`
	IPAddress    *string             `json:"ip_address,omitempty"`
	SessionID    *string             `json:"session_id,omitempty"`

	CacheTTLOverridden bool `json:"cache_ttl_overridden"`

	ImageCount         int            `json:"image_count"`
	ImageSize          *string        `json:"image_size,omitempty"`
	ImageInputSize     *string        `json:"image_input_size,omitempty"`
	ImageOutputSize    *string        `json:"image_output_size,omitempty"`
	ImageSizeSource    *string        `json:"image_size_source,omitempty"`
	ImageSizeBreakdown map[string]int `json:"image_size_breakdown,omitempty"`
	MediaType          *string        `json:"media_type,omitempty"`

	VideoCount           int     `json:"video_count"`
	VideoResolution      *string `json:"video_resolution,omitempty"`
	VideoDurationSeconds *int    `json:"video_duration_seconds,omitempty"`

	CreatedAt time.Time `json:"created_at"`

	InvalidNumerics []usageBillingNumericIssueV1 `json:"invalid_numerics,omitempty"`
}

func commandToUsageBillingPayloadV1(cmd *service.UsageBillingCommand) usageBillingCommandPayloadV1 {
	payload := usageBillingCommandPayloadV1{
		RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID,
		RequestFingerprint: cmd.RequestFingerprint, RequestPayloadHash: cmd.RequestPayloadHash,
		UserID: cmd.UserID, AccountID: cmd.AccountID, SubscriptionID: cmd.SubscriptionID,
		AccountType: cmd.AccountType, Model: cmd.Model, ServiceTier: cmd.ServiceTier,
		ReasoningEffort: cmd.ReasoningEffort, BillingType: cmd.BillingType,
		InputTokens: cmd.InputTokens, OutputTokens: cmd.OutputTokens,
		CacheCreationTokens: cmd.CacheCreationTokens, CacheReadTokens: cmd.CacheReadTokens,
		ImageCount: cmd.ImageCount, MediaType: cmd.MediaType,
		GroupID: cmd.GroupID, Platform: cmd.Platform, PlatformQuotaCost: cmd.PlatformQuotaCost,
		PlatformQuotaSnapshot:       cmd.PlatformQuotaSnapshot,
		PlatformQuotaSnapshotNeeded: cmd.PlatformQuotaSnapshotNeeded,
		ActualCost:                  cmd.ActualCost, TotalCost: cmd.TotalCost,
		IsSubscriptionBilling: cmd.IsSubscriptionBilling, OccurredAt: cmd.OccurredAt,
		QuotaTime:   cmd.QuotaTime,
		BalanceCost: cmd.BalanceCost, SubscriptionCost: cmd.SubscriptionCost,
		APIKeyQuotaCost: cmd.APIKeyQuotaCost, APIKeyRateLimitCost: cmd.APIKeyRateLimitCost,
		AccountQuotaCost: cmd.AccountQuotaCost,
	}
	return sanitizeUsageBillingCommandPayloadV1(payload)
}

func (p usageBillingCommandPayloadV1) command() *service.UsageBillingCommand {
	return &service.UsageBillingCommand{
		RequestID: canonicalizeUsageBillingIdentity(p.RequestID, 255), APIKeyID: p.APIKeyID,
		RequestFingerprint: p.RequestFingerprint, RequestPayloadHash: p.RequestPayloadHash,
		UserID: p.UserID, AccountID: p.AccountID, SubscriptionID: p.SubscriptionID,
		AccountType: p.AccountType, Model: canonicalizeUsageBillingIdentity(p.Model, 100), ServiceTier: p.ServiceTier,
		ReasoningEffort: p.ReasoningEffort, BillingType: p.BillingType,
		InputTokens: p.InputTokens, OutputTokens: p.OutputTokens,
		CacheCreationTokens: p.CacheCreationTokens, CacheReadTokens: p.CacheReadTokens,
		ImageCount: p.ImageCount, MediaType: p.MediaType,
		GroupID: p.GroupID, Platform: p.Platform, PlatformQuotaCost: p.PlatformQuotaCost,
		PlatformQuotaSnapshot:       p.PlatformQuotaSnapshot,
		PlatformQuotaSnapshotNeeded: p.PlatformQuotaSnapshotNeeded,
		ActualCost:                  p.ActualCost, TotalCost: p.TotalCost,
		IsSubscriptionBilling: p.IsSubscriptionBilling, OccurredAt: p.OccurredAt,
		QuotaTime:   p.QuotaTime,
		BalanceCost: p.BalanceCost, SubscriptionCost: p.SubscriptionCost,
		APIKeyQuotaCost: p.APIKeyQuotaCost, APIKeyRateLimitCost: p.APIKeyRateLimitCost,
		AccountQuotaCost: p.AccountQuotaCost,
	}
}

func balanceSettlementToPayloadV2(settlement *service.BalanceSettlementCommand) balanceSettlementPayloadV2 {
	if settlement == nil {
		return balanceSettlementPayloadV2{}
	}
	settlement.Normalize()
	payload := balanceSettlementPayloadV2{
		BillingReviewID:    settlement.Hold.BillingReviewID,
		TaskID:             settlement.TaskID,
		Action:             settlement.Action,
		SettlementScope:    settlement.Hold.Scope,
		SettlementRefID:    canonicalizeUsageBillingIdentity(settlement.Hold.RefID, 64),
		RequestID:          canonicalizeUsageBillingIdentity(settlement.Hold.RequestID, 255),
		APIKeyID:           settlement.Hold.APIKeyID,
		RequestFingerprint: strings.TrimSpace(sanitizeUsageBillingPostgresText(settlement.Hold.RequestFingerprint)),
		RequestPayloadHash: strings.TrimSpace(sanitizeUsageBillingPostgresText(settlement.Hold.RequestPayloadHash)),
		UserID:             settlement.Hold.UserID,
		HoldAmount:         settlement.Hold.HoldAmount,
		ActualAmount:       settlement.Hold.ActualAmount,
		AllowOverCapture:   settlement.Hold.Scope == service.BalanceHoldScopeVideoTask,
	}
	if settlement.Billing != nil {
		billing := commandToUsageBillingPayloadV1(settlement.Billing)
		payload.Billing = &billing
	}
	payload.HoldAmount = usageBillingJSONSafeFloat(payload.HoldAmount, "hold_amount", &payload.InvalidNumerics)
	payload.ActualAmount = usageBillingJSONSafeFloat(payload.ActualAmount, "actual_amount", &payload.InvalidNumerics)
	payload.InvalidNumerics = sanitizeUsageBillingNumericIssues(payload.InvalidNumerics)
	return payload
}

func (p balanceSettlementPayloadV2) settlement() *service.BalanceSettlementCommand {
	settlement := &service.BalanceSettlementCommand{
		TaskID: p.TaskID,
		Action: p.Action,
		Hold: service.BalanceHoldCommand{
			BillingReviewID:    p.BillingReviewID,
			RequestID:          p.RequestID,
			APIKeyID:           p.APIKeyID,
			RequestFingerprint: p.RequestFingerprint,
			RequestPayloadHash: p.RequestPayloadHash,
			UserID:             p.UserID,
			Scope:              p.SettlementScope,
			RefID:              p.SettlementRefID,
			HoldAmount:         p.HoldAmount,
			ActualAmount:       p.ActualAmount,
		},
	}
	if p.Billing != nil {
		settlement.Billing = p.Billing.command()
	}
	settlement.Normalize()
	return settlement
}

func usageLogToPayloadV1(log *service.UsageLog) usageLogPayloadV1 {
	copyLog := *log
	copyLog.RequestID = canonicalizeUsageBillingIdentity(copyLog.RequestID, 255)
	copyLog.Model = canonicalizeUsageBillingIdentity(copyLog.Model, 100)
	copyLog.RequestedModel = strings.TrimSpace(strings.ToValidUTF8(copyLog.RequestedModel, ""))
	copyLog.SyncRequestTypeAndLegacyFields()
	if copyLog.CreatedAt.IsZero() {
		copyLog.CreatedAt = time.Now().UTC()
	}
	return sanitizeUsageBillingLogPayloadV1(usageLogPayloadV1{
		UserID: copyLog.UserID, APIKeyID: copyLog.APIKeyID, AccountID: copyLog.AccountID,
		RequestID: copyLog.RequestID, Model: copyLog.Model, RequestedModel: copyLog.RequestedModel,
		UpstreamModel: copyLog.UpstreamModel, ChannelID: copyLog.ChannelID,
		ModelMappingChain: copyLog.ModelMappingChain, BillingTier: copyLog.BillingTier,
		BillingMode: copyLog.BillingMode, ServiceTier: copyLog.ServiceTier,
		ReasoningEffort: copyLog.ReasoningEffort, InboundEndpoint: copyLog.InboundEndpoint,
		UpstreamEndpoint: copyLog.UpstreamEndpoint, GroupID: copyLog.GroupID,
		SubscriptionID: copyLog.SubscriptionID, InputTokens: copyLog.InputTokens,
		OutputTokens: copyLog.OutputTokens, CacheCreationTokens: copyLog.CacheCreationTokens,
		CacheReadTokens:       copyLog.CacheReadTokens,
		CacheCreation5mTokens: copyLog.CacheCreation5mTokens,
		CacheCreation1hTokens: copyLog.CacheCreation1hTokens,
		ImageInputTokens:      copyLog.ImageInputTokens, ImageInputCost: copyLog.ImageInputCost,
		ImageOutputTokens: copyLog.ImageOutputTokens, ImageOutputCost: copyLog.ImageOutputCost,
		InputCost: copyLog.InputCost, OutputCost: copyLog.OutputCost,
		CacheCreationCost: copyLog.CacheCreationCost, CacheReadCost: copyLog.CacheReadCost,
		TotalCost: copyLog.TotalCost, ActualCost: copyLog.ActualCost,
		RateMultiplier:            copyLog.RateMultiplier,
		LongContextBillingApplied: copyLog.LongContextBillingApplied,
		AccountRateMultiplier:     copyLog.AccountRateMultiplier, AccountStatsCost: copyLog.AccountStatsCost,
		BillingType: copyLog.BillingType, BillingState: copyLog.BillingState,
		RequestType: copyLog.RequestType, Stream: copyLog.Stream, OpenAIWSMode: copyLog.OpenAIWSMode,
		DurationMs: copyLog.DurationMs, FirstTokenMs: copyLog.FirstTokenMs,
		UserAgent: copyLog.UserAgent, IPAddress: copyLog.IPAddress, SessionID: copyLog.SessionID,
		CacheTTLOverridden: copyLog.CacheTTLOverridden, ImageCount: copyLog.ImageCount,
		ImageSize: copyLog.ImageSize, ImageInputSize: copyLog.ImageInputSize,
		ImageOutputSize: copyLog.ImageOutputSize, ImageSizeSource: copyLog.ImageSizeSource,
		ImageSizeBreakdown: copyLog.ImageSizeBreakdown, MediaType: copyLog.MediaType,
		VideoCount: copyLog.VideoCount, VideoResolution: copyLog.VideoResolution,
		VideoDurationSeconds: copyLog.VideoDurationSeconds, CreatedAt: copyLog.CreatedAt,
	})
}

func (p usageLogPayloadV1) usageLog() *service.UsageLog {
	p = sanitizeUsageBillingLogPayloadV1(p)
	return &service.UsageLog{
		UserID: p.UserID, APIKeyID: p.APIKeyID, AccountID: p.AccountID,
		RequestID: p.RequestID, Model: p.Model, RequestedModel: p.RequestedModel,
		UpstreamModel: p.UpstreamModel, ChannelID: p.ChannelID,
		ModelMappingChain: p.ModelMappingChain, BillingTier: p.BillingTier,
		BillingMode: p.BillingMode, ServiceTier: p.ServiceTier,
		ReasoningEffort: p.ReasoningEffort, InboundEndpoint: p.InboundEndpoint,
		UpstreamEndpoint: p.UpstreamEndpoint, GroupID: p.GroupID,
		SubscriptionID: p.SubscriptionID, InputTokens: p.InputTokens,
		OutputTokens: p.OutputTokens, CacheCreationTokens: p.CacheCreationTokens,
		CacheReadTokens: p.CacheReadTokens, CacheCreation5mTokens: p.CacheCreation5mTokens,
		CacheCreation1hTokens: p.CacheCreation1hTokens,
		ImageInputTokens:      p.ImageInputTokens, ImageInputCost: p.ImageInputCost,
		ImageOutputTokens: p.ImageOutputTokens, ImageOutputCost: p.ImageOutputCost,
		InputCost: p.InputCost, OutputCost: p.OutputCost,
		CacheCreationCost: p.CacheCreationCost, CacheReadCost: p.CacheReadCost,
		TotalCost: p.TotalCost, ActualCost: p.ActualCost, RateMultiplier: p.RateMultiplier,
		LongContextBillingApplied: p.LongContextBillingApplied,
		AccountRateMultiplier:     p.AccountRateMultiplier, AccountStatsCost: p.AccountStatsCost,
		BillingType: p.BillingType, BillingState: p.BillingState, RequestType: p.RequestType,
		Stream: p.Stream, OpenAIWSMode: p.OpenAIWSMode, DurationMs: p.DurationMs,
		FirstTokenMs: p.FirstTokenMs, UserAgent: p.UserAgent, IPAddress: p.IPAddress,
		SessionID: p.SessionID, CacheTTLOverridden: p.CacheTTLOverridden,
		ImageCount: p.ImageCount, ImageSize: p.ImageSize, ImageInputSize: p.ImageInputSize,
		ImageOutputSize: p.ImageOutputSize, ImageSizeSource: p.ImageSizeSource,
		ImageSizeBreakdown: p.ImageSizeBreakdown, MediaType: p.MediaType,
		VideoCount: p.VideoCount, VideoResolution: p.VideoResolution,
		VideoDurationSeconds: p.VideoDurationSeconds, CreatedAt: p.CreatedAt,
	}
}

// sanitizeUsageBillingPostgresText returns UTF-8 text that PostgreSQL can store
// in both text/varchar columns and JSONB string values. U+0000 is valid in a Go
// string and in JSON, but PostgreSQL rejects the corresponding \u0000 escape
// while converting JSON input to jsonb.
func sanitizeUsageBillingPostgresText(value string) string {
	value = strings.ToValidUTF8(value, "")
	return strings.ReplaceAll(value, "\x00", "\uFFFD")
}

func truncateUsageBillingUTF8(value string, maxBytes int) string {
	value = sanitizeUsageBillingPostgresText(value)
	if maxBytes < 1 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

func truncateUsageBillingStringPtr(value *string, maxBytes int) *string {
	if value == nil {
		return nil
	}
	truncated := truncateUsageBillingUTF8(*value, maxBytes)
	return &truncated
}

// canonicalizeUsageBillingIdentity returns the exact value used by the durable
// payload, request fingerprint, and SQL insert. Identifiers that exceed their
// schema width retain a readable prefix plus a collision-resistant digest
// instead of being naively truncated (which could merge two billable calls).
func canonicalizeUsageBillingIdentity(value string, maxBytes int) string {
	if maxBytes < 1 {
		return ""
	}
	raw := strings.TrimSpace(value)
	value = sanitizeUsageBillingPostgresText(raw)
	if raw == value && len(value) <= maxBytes {
		return value
	}
	// Hash the trimmed pre-repair bytes so distinct invalid UTF-8 or U+0000
	// identifiers cannot collapse to the same PostgreSQL-safe string.
	suffix := "#sha256:" + service.HashUsageRequestPayload([]byte(raw))
	if len(suffix) >= maxBytes {
		return suffix[len(suffix)-maxBytes:]
	}
	return truncateUsageBillingUTF8(value, maxBytes-len(suffix)) + suffix
}

func truncateUsageBillingTrimmedStringPtr(value *string, maxBytes int) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(sanitizeUsageBillingPostgresText(*value))
	truncated := truncateUsageBillingUTF8(trimmed, maxBytes)
	return &truncated
}

func sanitizeUsageBillingStringIntMap(value map[string]int) map[string]int {
	if len(value) == 0 {
		return value
	}
	safe := make(map[string]int, len(value))
	for key, count := range value {
		rawKey := key
		key = sanitizeUsageBillingPostgresText(rawKey)
		if key != rawKey {
			// Preserve distinct repaired keys. Image-size breakdown keys are
			// currently server-generated, but this keeps future upstream-derived
			// keys collision-resistant as well.
			key += "#sha256:" + service.HashUsageRequestPayload([]byte(rawKey))
		}
		safe[key] = count
	}
	return safe
}

func sanitizeUsageBillingNumericIssues(issues []usageBillingNumericIssueV1) []usageBillingNumericIssueV1 {
	for i := range issues {
		issues[i].Field = sanitizeUsageBillingPostgresText(issues[i].Field)
		issues[i].Reason = sanitizeUsageBillingPostgresText(issues[i].Reason)
		issues[i].Value = sanitizeUsageBillingPostgresText(issues[i].Value)
	}
	return issues
}

func sanitizeUsageBillingCommandText(cmd *service.UsageBillingCommand) {
	if cmd == nil {
		return
	}
	cmd.RequestID = canonicalizeUsageBillingIdentity(cmd.RequestID, 255)
	cmd.Model = canonicalizeUsageBillingIdentity(cmd.Model, 100)
	cmd.RequestPayloadHash = strings.TrimSpace(sanitizeUsageBillingPostgresText(cmd.RequestPayloadHash))
	cmd.AccountType = strings.TrimSpace(sanitizeUsageBillingPostgresText(cmd.AccountType))
	cmd.ServiceTier = strings.TrimSpace(sanitizeUsageBillingPostgresText(cmd.ServiceTier))
	cmd.ReasoningEffort = strings.TrimSpace(sanitizeUsageBillingPostgresText(cmd.ReasoningEffort))
	cmd.MediaType = strings.TrimSpace(sanitizeUsageBillingPostgresText(cmd.MediaType))
	cmd.Platform = strings.TrimSpace(sanitizeUsageBillingPostgresText(cmd.Platform))
}

func usageBillingJSONSafeFloat(
	value float64,
	field string,
	issues *[]usageBillingNumericIssueV1,
) float64 {
	if !math.IsNaN(value) && !math.IsInf(value, 0) {
		return value
	}
	*issues = append(*issues, usageBillingNumericIssueV1{
		Field:  field,
		Reason: "non_finite",
		Value:  fmt.Sprintf("%v", value),
	})
	// PostgreSQL JSONB does not support NaN or infinities. The durable marker
	// above is authoritative: this placeholder exists only to make stage 0
	// persistable and is rejected before any billing effect runs.
	return 0
}

func usageBillingJSONSafeFloatPtr(
	value *float64,
	field string,
	issues *[]usageBillingNumericIssueV1,
) *float64 {
	if value == nil {
		return nil
	}
	safe := usageBillingJSONSafeFloat(*value, field, issues)
	return &safe
}

func sanitizeUsageBillingCommandPayloadV1(p usageBillingCommandPayloadV1) usageBillingCommandPayloadV1 {
	p.RequestID = canonicalizeUsageBillingIdentity(p.RequestID, 255)
	p.Model = canonicalizeUsageBillingIdentity(p.Model, 100)
	p.RequestFingerprint = strings.TrimSpace(sanitizeUsageBillingPostgresText(p.RequestFingerprint))
	p.RequestPayloadHash = strings.TrimSpace(sanitizeUsageBillingPostgresText(p.RequestPayloadHash))
	p.AccountType = strings.TrimSpace(sanitizeUsageBillingPostgresText(p.AccountType))
	p.ServiceTier = strings.TrimSpace(sanitizeUsageBillingPostgresText(p.ServiceTier))
	p.ReasoningEffort = strings.TrimSpace(sanitizeUsageBillingPostgresText(p.ReasoningEffort))
	p.MediaType = strings.TrimSpace(sanitizeUsageBillingPostgresText(p.MediaType))
	p.Platform = strings.TrimSpace(sanitizeUsageBillingPostgresText(p.Platform))
	p.PlatformQuotaCost = usageBillingJSONSafeFloat(
		p.PlatformQuotaCost,
		"platform_quota_cost",
		&p.InvalidNumerics,
	)
	p.ActualCost = usageBillingJSONSafeFloat(p.ActualCost, "actual_cost", &p.InvalidNumerics)
	p.TotalCost = usageBillingJSONSafeFloat(p.TotalCost, "total_cost", &p.InvalidNumerics)
	p.BalanceCost = usageBillingJSONSafeFloat(p.BalanceCost, "balance_cost", &p.InvalidNumerics)
	p.SubscriptionCost = usageBillingJSONSafeFloat(
		p.SubscriptionCost,
		"subscription_cost",
		&p.InvalidNumerics,
	)
	p.APIKeyQuotaCost = usageBillingJSONSafeFloat(
		p.APIKeyQuotaCost,
		"api_key_quota_cost",
		&p.InvalidNumerics,
	)
	p.APIKeyRateLimitCost = usageBillingJSONSafeFloat(
		p.APIKeyRateLimitCost,
		"api_key_rate_limit_cost",
		&p.InvalidNumerics,
	)
	p.AccountQuotaCost = usageBillingJSONSafeFloat(
		p.AccountQuotaCost,
		"account_quota_cost",
		&p.InvalidNumerics,
	)
	if p.PlatformQuotaSnapshot != nil {
		snapshot := *p.PlatformQuotaSnapshot
		snapshot.DailyUsageUSD = usageBillingJSONSafeFloat(
			snapshot.DailyUsageUSD,
			"platform_quota_snapshot.daily_usage_usd",
			&p.InvalidNumerics,
		)
		snapshot.WeeklyUsageUSD = usageBillingJSONSafeFloat(
			snapshot.WeeklyUsageUSD,
			"platform_quota_snapshot.weekly_usage_usd",
			&p.InvalidNumerics,
		)
		snapshot.MonthlyUsageUSD = usageBillingJSONSafeFloat(
			snapshot.MonthlyUsageUSD,
			"platform_quota_snapshot.monthly_usage_usd",
			&p.InvalidNumerics,
		)
		p.PlatformQuotaSnapshot = &snapshot
	}
	p.InvalidNumerics = sanitizeUsageBillingNumericIssues(p.InvalidNumerics)
	return p
}

// sanitizeUsageBillingLogPayloadV1 bounds lossy, non-accounting metadata before
// it enters the durable intent and again when it is decoded. Client-controlled
// fields such as User-Agent must never turn an already-successful upstream
// request into a permanently quarantined, unbilled event.
func sanitizeUsageBillingLogPayloadV1(p usageLogPayloadV1) usageLogPayloadV1 {
	p.RequestID = canonicalizeUsageBillingIdentity(p.RequestID, 255)
	p.Model = canonicalizeUsageBillingIdentity(p.Model, 100)
	p.RequestedModel = truncateUsageBillingUTF8(
		strings.TrimSpace(strings.ToValidUTF8(p.RequestedModel, "")),
		100,
	)
	p.UpstreamModel = truncateUsageBillingTrimmedStringPtr(p.UpstreamModel, 100)
	p.ModelMappingChain = truncateUsageBillingStringPtr(p.ModelMappingChain, 500)
	p.BillingTier = truncateUsageBillingStringPtr(p.BillingTier, 50)
	p.BillingMode = truncateUsageBillingStringPtr(p.BillingMode, 20)
	p.ServiceTier = truncateUsageBillingStringPtr(p.ServiceTier, 16)
	p.ReasoningEffort = truncateUsageBillingStringPtr(p.ReasoningEffort, 20)
	p.InboundEndpoint = truncateUsageBillingStringPtr(p.InboundEndpoint, 128)
	p.UpstreamEndpoint = truncateUsageBillingStringPtr(p.UpstreamEndpoint, 128)
	p.UserAgent = truncateUsageBillingStringPtr(p.UserAgent, 512)
	p.IPAddress = truncateUsageBillingStringPtr(p.IPAddress, 45)
	p.SessionID = truncateUsageBillingStringPtr(p.SessionID, 255)
	p.ImageSize = truncateUsageBillingStringPtr(p.ImageSize, 10)
	p.ImageInputSize = truncateUsageBillingStringPtr(p.ImageInputSize, 32)
	p.ImageOutputSize = truncateUsageBillingStringPtr(p.ImageOutputSize, 32)
	p.ImageSizeSource = truncateUsageBillingStringPtr(p.ImageSizeSource, 16)
	p.MediaType = truncateUsageBillingStringPtr(p.MediaType, 16)
	p.VideoResolution = truncateUsageBillingStringPtr(p.VideoResolution, 10)
	p.ImageSizeBreakdown = sanitizeUsageBillingStringIntMap(p.ImageSizeBreakdown)
	p.ImageInputCost = usageBillingJSONSafeFloat(
		p.ImageInputCost,
		"image_input_cost",
		&p.InvalidNumerics,
	)
	p.ImageOutputCost = usageBillingJSONSafeFloat(
		p.ImageOutputCost,
		"image_output_cost",
		&p.InvalidNumerics,
	)
	p.InputCost = usageBillingJSONSafeFloat(p.InputCost, "input_cost", &p.InvalidNumerics)
	p.OutputCost = usageBillingJSONSafeFloat(p.OutputCost, "output_cost", &p.InvalidNumerics)
	p.CacheCreationCost = usageBillingJSONSafeFloat(
		p.CacheCreationCost,
		"cache_creation_cost",
		&p.InvalidNumerics,
	)
	p.CacheReadCost = usageBillingJSONSafeFloat(
		p.CacheReadCost,
		"cache_read_cost",
		&p.InvalidNumerics,
	)
	p.TotalCost = usageBillingJSONSafeFloat(p.TotalCost, "total_cost", &p.InvalidNumerics)
	p.ActualCost = usageBillingJSONSafeFloat(p.ActualCost, "actual_cost", &p.InvalidNumerics)
	p.RateMultiplier = usageBillingJSONSafeFloat(
		p.RateMultiplier,
		"rate_multiplier",
		&p.InvalidNumerics,
	)
	p.AccountRateMultiplier = usageBillingJSONSafeFloatPtr(
		p.AccountRateMultiplier,
		"account_rate_multiplier",
		&p.InvalidNumerics,
	)
	p.AccountStatsCost = usageBillingJSONSafeFloatPtr(
		p.AccountStatsCost,
		"account_stats_cost",
		&p.InvalidNumerics,
	)
	p.InvalidNumerics = sanitizeUsageBillingNumericIssues(p.InvalidNumerics)
	return p
}

type usageBillingResultPayloadV1 struct {
	Applied                  bool                       `json:"applied"`
	UsageLogRecorded         bool                       `json:"usage_log_recorded"`
	ProjectionRepairRequired bool                       `json:"projection_repair_required,omitempty"`
	APIKeyQuotaExhausted     bool                       `json:"api_key_quota_exhausted"`
	NewBalance               *float64                   `json:"new_balance,omitempty"`
	FrozenBalance            *float64                   `json:"frozen_balance,omitempty"`
	BalanceOverdrafted       bool                       `json:"balance_overdrafted"`
	QuotaState               *service.AccountQuotaState `json:"quota_state,omitempty"`
	QuotaPostedAt            *time.Time                 `json:"quota_posted_at,omitempty"`
}

func usageBillingResultToPayloadV1(result *service.UsageBillingApplyResult) usageBillingResultPayloadV1 {
	if result == nil {
		return usageBillingResultPayloadV1{}
	}
	return usageBillingResultPayloadV1{
		Applied:                  result.Applied,
		UsageLogRecorded:         result.UsageLogRecorded,
		ProjectionRepairRequired: result.ProjectionRepairRequired,
		APIKeyQuotaExhausted:     result.APIKeyQuotaExhausted,
		NewBalance:               result.NewBalance,
		FrozenBalance:            result.FrozenBalance,
		BalanceOverdrafted:       result.BalanceOverdrafted,
		QuotaState:               result.QuotaState,
		QuotaPostedAt:            result.QuotaPostedAt,
	}
}

func (p usageBillingResultPayloadV1) result() *service.UsageBillingApplyResult {
	return &service.UsageBillingApplyResult{
		Applied:                  p.Applied,
		UsageLogRecorded:         p.UsageLogRecorded,
		ProjectionRepairRequired: p.ProjectionRepairRequired,
		APIKeyQuotaExhausted:     p.APIKeyQuotaExhausted,
		NewBalance:               p.NewBalance,
		FrozenBalance:            p.FrozenBalance,
		BalanceOverdrafted:       p.BalanceOverdrafted,
		QuotaState:               p.QuotaState,
		QuotaPostedAt:            p.QuotaPostedAt,
	}
}

type usageLogBillingComparableV1 struct {
	UserID    int64
	APIKeyID  int64
	AccountID int64
	RequestID string
	Model     string

	RequestedModel    string
	UpstreamModel     string
	ChannelID         int64
	ModelMappingChain string
	BillingTier       string
	BillingMode       string
	ServiceTier       string
	ReasoningEffort   string

	GroupID        int64
	SubscriptionID int64

	InputTokens           int
	OutputTokens          int
	CacheCreationTokens   int
	CacheReadTokens       int
	CacheCreation5mTokens int
	CacheCreation1hTokens int
	ImageInputTokens      int
	ImageInputCost        float64
	ImageOutputTokens     int
	ImageOutputCost       float64

	InputCost                 float64
	OutputCost                float64
	CacheCreationCost         float64
	CacheReadCost             float64
	TotalCost                 float64
	ActualCost                float64
	RateMultiplier            float64
	LongContextBillingApplied bool
	AccountRateMultiplier     *float64
	AccountStatsCost          *float64

	BillingType  int8
	BillingState int8
	RequestType  service.RequestType

	CacheTTLOverridden bool
	ImageCount         int
	ImageSize          string
	ImageInputSize     string
	ImageOutputSize    string
	ImageSizeSource    string
	ImageSizeBreakdown map[string]int
	MediaType          string
	VideoCount         int
	VideoResolution    string
	VideoDuration      int
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalIntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// canonicalUsageBillingNumeric mirrors the precision PostgreSQL persists for
// usage_logs numeric columns. Comparing the pre-insert float bit pattern with a
// value loaded back from NUMERIC is incorrect: PostgreSQL has already rounded
// it to the declared scale (notably RateMultiplier can be the product of two
// NUMERIC(10,4) values and therefore carry eight fractional digits in Go).
//
// decimal.NewFromFloat uses the same shortest decimal representation sent by
// database drivers for a float64; rounding that representation before the
// comparison makes replay equivalence match the durable database fact.
func canonicalUsageBillingNumeric(value float64, scale int32) float64 {
	return decimal.NewFromFloat(value).Round(scale).InexactFloat64()
}

func canonicalUsageBillingNumericPtr(value *float64, scale int32) *float64 {
	if value == nil {
		return nil
	}
	canonical := canonicalUsageBillingNumeric(*value, scale)
	return &canonical
}

func usageLogPayloadBillingComparable(p usageLogPayloadV1) usageLogBillingComparableV1 {
	requestedModel := strings.TrimSpace(p.RequestedModel)
	if requestedModel == "" {
		requestedModel = strings.TrimSpace(p.Model)
	}
	requestType := p.RequestType.Normalize()
	if requestType == service.RequestTypeUnknown {
		requestType = service.RequestTypeFromLegacy(p.Stream, p.OpenAIWSMode)
	}
	return usageLogBillingComparableV1{
		UserID: p.UserID, APIKeyID: p.APIKeyID, AccountID: p.AccountID,
		RequestID: strings.TrimSpace(p.RequestID), Model: strings.TrimSpace(p.Model),
		RequestedModel: requestedModel, UpstreamModel: optionalStringValue(p.UpstreamModel),
		ChannelID: valueOrZeroInt64(p.ChannelID), ModelMappingChain: optionalStringValue(p.ModelMappingChain),
		BillingTier: optionalStringValue(p.BillingTier), BillingMode: optionalStringValue(p.BillingMode),
		ServiceTier: optionalStringValue(p.ServiceTier), ReasoningEffort: optionalStringValue(p.ReasoningEffort),
		GroupID: valueOrZeroInt64(p.GroupID), SubscriptionID: valueOrZeroInt64(p.SubscriptionID),
		InputTokens: p.InputTokens, OutputTokens: p.OutputTokens,
		CacheCreationTokens: p.CacheCreationTokens, CacheReadTokens: p.CacheReadTokens,
		CacheCreation5mTokens: p.CacheCreation5mTokens, CacheCreation1hTokens: p.CacheCreation1hTokens,
		ImageInputTokens:          p.ImageInputTokens,
		ImageInputCost:            canonicalUsageBillingNumeric(p.ImageInputCost, 10),
		ImageOutputTokens:         p.ImageOutputTokens,
		ImageOutputCost:           canonicalUsageBillingNumeric(p.ImageOutputCost, 10),
		InputCost:                 canonicalUsageBillingNumeric(p.InputCost, 10),
		OutputCost:                canonicalUsageBillingNumeric(p.OutputCost, 10),
		CacheCreationCost:         canonicalUsageBillingNumeric(p.CacheCreationCost, 10),
		CacheReadCost:             canonicalUsageBillingNumeric(p.CacheReadCost, 10),
		TotalCost:                 canonicalUsageBillingNumeric(p.TotalCost, 10),
		ActualCost:                canonicalUsageBillingNumeric(p.ActualCost, 10),
		RateMultiplier:            canonicalUsageBillingNumeric(p.RateMultiplier, 4),
		LongContextBillingApplied: p.LongContextBillingApplied,
		AccountRateMultiplier:     canonicalUsageBillingNumericPtr(p.AccountRateMultiplier, 4),
		AccountStatsCost:          canonicalUsageBillingNumericPtr(p.AccountStatsCost, 10),
		BillingType:               p.BillingType, BillingState: p.BillingState, RequestType: requestType,
		CacheTTLOverridden: p.CacheTTLOverridden, ImageCount: p.ImageCount,
		ImageSize: optionalStringValue(p.ImageSize), ImageInputSize: optionalStringValue(p.ImageInputSize),
		ImageOutputSize: optionalStringValue(p.ImageOutputSize), ImageSizeSource: optionalStringValue(p.ImageSizeSource),
		ImageSizeBreakdown: p.ImageSizeBreakdown, MediaType: optionalStringValue(p.MediaType),
		VideoCount: p.VideoCount, VideoResolution: optionalStringValue(p.VideoResolution),
		VideoDuration: optionalIntValue(p.VideoDurationSeconds),
	}
}

func valueOrZeroInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func usageLogPayloadsBillingEquivalent(existing, intended usageLogPayloadV1) bool {
	return reflect.DeepEqual(
		usageLogPayloadBillingComparable(existing),
		usageLogPayloadBillingComparable(intended),
	)
}

func usageLogPayloadsRecoverableUnchargedFallback(
	existing,
	intended usageLogPayloadV1,
) bool {
	existingComparable := usageLogPayloadBillingComparable(existing)
	intendedComparable := usageLogPayloadBillingComparable(intended)
	if existingComparable.ActualCost != 0 || intendedComparable.ActualCost <= 0 {
		return false
	}
	existingComparable.ActualCost = intendedComparable.ActualCost
	return reflect.DeepEqual(existingComparable, intendedComparable)
}

func canonicalizeUsageBillingChargeAmounts(
	cmd *service.UsageBillingCommand,
	usageLog *service.UsageLog,
) {
	if cmd != nil {
		cmd.ActualCost = service.QuantizeUsageBillingAmount(cmd.ActualCost)
		cmd.PlatformQuotaCost = service.QuantizeUsageBillingAmount(cmd.PlatformQuotaCost)
	}
	if usageLog != nil {
		usageLog.ActualCost = service.QuantizeUsageBillingAmount(usageLog.ActualCost)
	}
}

func usageBillingPayloadNumericError(
	commandIssues []usageBillingNumericIssueV1,
	logIssues []usageBillingNumericIssueV1,
) string {
	parts := make([]string, 0, len(commandIssues)+len(logIssues))
	appendIssues := func(prefix string, issues []usageBillingNumericIssueV1) {
		for _, issue := range issues {
			parts = append(parts, fmt.Sprintf(
				"%s.%s=%s (%s)",
				prefix,
				issue.Field,
				issue.Value,
				issue.Reason,
			))
		}
	}
	appendIssues("command", commandIssues)
	appendIssues("usage_log", logIssues)
	return strings.Join(parts, "; ")
}

func validateUsageBillingOutboxEvent(event service.UsageBillingOutboxEvent) error {
	if reason := strings.TrimSpace(event.PayloadValidationError); reason != "" {
		return fmt.Errorf("%w: %s", service.ErrUsageBillingPayloadInvalid, reason)
	}
	if event.BalanceSettlement != nil {
		return validateBalanceSettlementPayload(event.BalanceSettlement, event.UsageLog)
	}
	return validateUsageBillingOutboxPayload(event.Command, event.UsageLog)
}

func validateUsageBillingOutboxPayload(cmd *service.UsageBillingCommand, log *service.UsageLog) error {
	if cmd == nil || log == nil {
		return fmt.Errorf("%w: command and usage log are required", service.ErrUsageBillingPayloadInvalid)
	}
	if err := cmd.ValidateQuotaTime(); err != nil {
		return err
	}
	if cmd.QuotaTime != nil && !cmd.OccurredAt.Equal(log.CreatedAt) {
		return fmt.Errorf("%w: video quota event time and usage time differ", service.ErrUsageBillingPayloadInvalid)
	}
	if log.RequestID == "" || len(log.RequestID) > 255 {
		return fmt.Errorf("%w: usage log request_id length must be 1..255", service.ErrUsageBillingPayloadInvalid)
	}
	if log.Model == "" || len(log.Model) > 100 {
		return fmt.Errorf("%w: usage log model length must be 1..100", service.ErrUsageBillingPayloadInvalid)
	}
	if cmd.RequestID != log.RequestID ||
		cmd.APIKeyID != log.APIKeyID ||
		cmd.UserID != log.UserID ||
		cmd.AccountID != log.AccountID ||
		cmd.Model != log.Model ||
		cmd.BillingType != log.BillingType ||
		cmd.InputTokens != log.InputTokens ||
		cmd.OutputTokens != log.OutputTokens ||
		cmd.CacheCreationTokens != log.CacheCreationTokens ||
		cmd.CacheReadTokens != log.CacheReadTokens ||
		cmd.ImageCount != log.ImageCount ||
		cmd.ActualCost != log.ActualCost ||
		cmd.TotalCost != log.TotalCost ||
		valueOrZeroInt64(cmd.SubscriptionID) != valueOrZeroInt64(log.SubscriptionID) ||
		valueOrZeroInt64(cmd.GroupID) != valueOrZeroInt64(log.GroupID) {
		return fmt.Errorf("%w: command and usage log billing facts differ", service.ErrUsageBillingPayloadInvalid)
	}
	if cmd.IsSubscriptionBilling != (log.BillingType == service.BillingTypeSubscription) {
		return fmt.Errorf("%w: billing mode and billing_type differ", service.ErrUsageBillingPayloadInvalid)
	}
	if cmd.IsSubscriptionBilling {
		if cmd.SubscriptionID == nil || cmd.GroupID == nil || cmd.BalanceCost != 0 ||
			cmd.SubscriptionCost != cmd.ActualCost {
			return fmt.Errorf("%w: invalid subscription billing allocation", service.ErrUsageBillingPayloadInvalid)
		}
	} else if cmd.SubscriptionCost != 0 ||
		cmd.BalanceCost != cmd.ActualCost {
		return fmt.Errorf("%w: invalid balance billing allocation", service.ErrUsageBillingPayloadInvalid)
	}
	stringWidths := []struct {
		name  string
		value *string
		max   int
	}{
		{"requested_model", &log.RequestedModel, 100},
		{"upstream_model", log.UpstreamModel, 100},
		{"model_mapping_chain", log.ModelMappingChain, 500},
		{"billing_tier", log.BillingTier, 50},
		{"billing_mode", log.BillingMode, 20},
		{"service_tier", log.ServiceTier, 16},
		{"reasoning_effort", log.ReasoningEffort, 20},
		{"inbound_endpoint", log.InboundEndpoint, 128},
		{"upstream_endpoint", log.UpstreamEndpoint, 128},
		{"user_agent", log.UserAgent, 512},
		{"ip_address", log.IPAddress, 45},
		{"session_id", log.SessionID, 255},
		{"image_size", log.ImageSize, 10},
		{"image_input_size", log.ImageInputSize, 32},
		{"image_output_size", log.ImageOutputSize, 32},
		{"image_size_source", log.ImageSizeSource, 16},
		{"media_type", log.MediaType, 16},
		{"video_resolution", log.VideoResolution, 10},
	}
	for _, field := range stringWidths {
		if field.value != nil && len(*field.value) > field.max {
			return fmt.Errorf("%w: usage log %s exceeds %d bytes", service.ErrUsageBillingPayloadInvalid, field.name, field.max)
		}
	}
	integerFields := []struct {
		name  string
		value int
	}{
		{"command.input_tokens", cmd.InputTokens},
		{"command.output_tokens", cmd.OutputTokens},
		{"command.cache_creation_tokens", cmd.CacheCreationTokens},
		{"command.cache_read_tokens", cmd.CacheReadTokens},
		{"command.image_count", cmd.ImageCount},
		{"usage_log.input_tokens", log.InputTokens},
		{"usage_log.output_tokens", log.OutputTokens},
		{"usage_log.cache_creation_tokens", log.CacheCreationTokens},
		{"usage_log.cache_read_tokens", log.CacheReadTokens},
		{"usage_log.cache_creation_5m_tokens", log.CacheCreation5mTokens},
		{"usage_log.cache_creation_1h_tokens", log.CacheCreation1hTokens},
		{"usage_log.image_input_tokens", log.ImageInputTokens},
		{"usage_log.image_output_tokens", log.ImageOutputTokens},
		{"usage_log.image_count", log.ImageCount},
		{"usage_log.video_count", log.VideoCount},
	}
	if log.DurationMs != nil {
		integerFields = append(integerFields, struct {
			name  string
			value int
		}{"usage_log.duration_ms", *log.DurationMs})
	}
	if log.FirstTokenMs != nil {
		integerFields = append(integerFields, struct {
			name  string
			value int
		}{"usage_log.first_token_ms", *log.FirstTokenMs})
	}
	if log.VideoDurationSeconds != nil {
		integerFields = append(integerFields, struct {
			name  string
			value int
		}{"usage_log.video_duration_seconds", *log.VideoDurationSeconds})
	}
	for _, field := range integerFields {
		if field.value < 0 || int64(field.value) > usageBillingPostgresIntegerMax {
			return fmt.Errorf(
				"%w: %s must fit PostgreSQL INTEGER and be non-negative",
				service.ErrUsageBillingPayloadInvalid,
				field.name,
			)
		}
	}
	for key, value := range log.ImageSizeBreakdown {
		if value < 0 || int64(value) > usageBillingPostgresIntegerMax {
			return fmt.Errorf(
				"%w: usage_log.image_size_breakdown[%q] must be non-negative and at most %d",
				service.ErrUsageBillingPayloadInvalid,
				key,
				usageBillingPostgresIntegerMax,
			)
		}
	}

	numeric20Scale10Fields := []struct {
		name  string
		value float64
	}{
		{"command.actual_cost", cmd.ActualCost},
		{"command.total_cost", cmd.TotalCost},
		{"command.platform_quota_cost", cmd.PlatformQuotaCost},
		{"command.balance_cost", cmd.BalanceCost},
		{"command.subscription_cost", cmd.SubscriptionCost},
		{"command.api_key_quota_cost", cmd.APIKeyQuotaCost},
		{"command.api_key_rate_limit_cost", cmd.APIKeyRateLimitCost},
		{"command.account_quota_cost", cmd.AccountQuotaCost},
		{"usage_log.input_cost", log.InputCost},
		{"usage_log.output_cost", log.OutputCost},
		{"usage_log.cache_creation_cost", log.CacheCreationCost},
		{"usage_log.cache_read_cost", log.CacheReadCost},
		{"usage_log.image_input_cost", log.ImageInputCost},
		{"usage_log.image_output_cost", log.ImageOutputCost},
		{"usage_log.total_cost", log.TotalCost},
		{"usage_log.actual_cost", log.ActualCost},
	}
	if log.AccountStatsCost != nil {
		numeric20Scale10Fields = append(numeric20Scale10Fields, struct {
			name  string
			value float64
		}{"usage_log.account_stats_cost", *log.AccountStatsCost})
	}
	if cmd.PlatformQuotaSnapshot != nil {
		numeric20Scale10Fields = append(
			numeric20Scale10Fields,
			struct {
				name  string
				value float64
			}{"command.platform_quota_snapshot.daily_usage_usd", cmd.PlatformQuotaSnapshot.DailyUsageUSD},
			struct {
				name  string
				value float64
			}{"command.platform_quota_snapshot.weekly_usage_usd", cmd.PlatformQuotaSnapshot.WeeklyUsageUSD},
			struct {
				name  string
				value float64
			}{"command.platform_quota_snapshot.monthly_usage_usd", cmd.PlatformQuotaSnapshot.MonthlyUsageUSD},
		)
	}
	for _, field := range numeric20Scale10Fields {
		if field.value < 0 ||
			field.value >= usageBillingNumeric20Scale10UpperBound ||
			math.IsNaN(field.value) ||
			math.IsInf(field.value, 0) {
			return fmt.Errorf(
				"%w: %s must fit PostgreSQL NUMERIC(20,10), be finite, and be non-negative",
				service.ErrUsageBillingPayloadInvalid,
				field.name,
			)
		}
	}

	numeric10Scale4Fields := []struct {
		name  string
		value float64
	}{
		{"usage_log.rate_multiplier", log.RateMultiplier},
	}
	if log.AccountRateMultiplier != nil {
		numeric10Scale4Fields = append(numeric10Scale4Fields, struct {
			name  string
			value float64
		}{"usage_log.account_rate_multiplier", *log.AccountRateMultiplier})
	}
	for _, field := range numeric10Scale4Fields {
		if field.value < 0 ||
			field.value > usageBillingNumeric10Scale4Max ||
			math.IsNaN(field.value) ||
			math.IsInf(field.value, 0) {
			return fmt.Errorf(
				"%w: %s must fit PostgreSQL NUMERIC(10,4), be finite, and be non-negative",
				service.ErrUsageBillingPayloadInvalid,
				field.name,
			)
		}
	}
	return nil
}

func marshalUsageBillingOutboxPayload(cmd *service.UsageBillingCommand, usageLog *service.UsageLog) ([]byte, []byte, error) {
	if cmd == nil || usageLog == nil {
		return nil, nil, errors.New("usage billing outbox command and usage log are required")
	}
	if cmd.QuotaTime != nil {
		return nil, nil, fmt.Errorf("%w: video quota time requires hold-backed settlement", service.ErrUsageBillingPayloadInvalid)
	}
	sanitizeUsageBillingCommandText(cmd)
	usageLog.RequestID = canonicalizeUsageBillingIdentity(usageLog.RequestID, 255)
	usageLog.Model = canonicalizeUsageBillingIdentity(usageLog.Model, 100)
	usageLog.RequestedModel = strings.TrimSpace(sanitizeUsageBillingPostgresText(usageLog.RequestedModel))
	// Normalize once before rebuilding the fingerprint so direct repository
	// callers receive the same allocation quantization as service callers.
	// ActualCost intentionally remains raw until after serialization below.
	cmd.Normalize()
	// Never trust a fingerprint that may have been computed over a
	// pre-canonicalized command. Rebuild it from the values that will actually
	// be persisted and charged.
	cmd.RequestFingerprint = ""
	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, nil, service.ErrUsageBillingRequestIDRequired
	}
	if usageLog.RequestID != cmd.RequestID || usageLog.APIKeyID != cmd.APIKeyID {
		return nil, nil, errors.New("usage billing command and usage log idempotency keys differ")
	}
	commandJSON, err := json.Marshal(commandToUsageBillingPayloadV1(cmd))
	if err != nil {
		return nil, nil, fmt.Errorf("marshal usage billing command: %w", err)
	}
	usageLogJSON, err := json.Marshal(usageLogToPayloadV1(usageLog))
	if err != nil {
		return nil, nil, fmt.Errorf("marshal usage billing log: %w", err)
	}
	// Keep the immutable payload on the pre-quantized amount used to build its
	// fingerprint. The decoder verifies that historical fact first, then
	// canonicalizes the charge before validation and mutation. The caller still
	// receives the exact NUMERIC(20,8) amount used by PostgreSQL so cache and
	// notification projections stay in sync with the durable balance change.
	canonicalizeUsageBillingChargeAmounts(cmd, usageLog)
	return commandJSON, usageLogJSON, nil
}

func marshalBalanceSettlementOutboxPayload(
	settlement *service.BalanceSettlementCommand,
	usageLog *service.UsageLog,
) ([]byte, []byte, error) {
	if settlement == nil {
		return nil, nil, errors.New("balance settlement command is required")
	}
	settlement.Normalize()
	settlement.Hold.RequestID = canonicalizeUsageBillingIdentity(settlement.Hold.RequestID, 255)
	settlement.Hold.RefID = canonicalizeUsageBillingIdentity(settlement.Hold.RefID, 64)
	settlement.Hold.RequestPayloadHash = strings.TrimSpace(sanitizeUsageBillingPostgresText(settlement.Hold.RequestPayloadHash))
	settlement.Hold.RequestFingerprint = ""
	settlement.Hold.Normalize()
	if settlement.Billing != nil {
		sanitizeUsageBillingCommandText(settlement.Billing)
		if settlement.Billing.QuotaTime != nil || settlement.Hold.BillingReviewID > 0 {
			settlement.Billing.RequestFingerprint = ""
		}
		settlement.Billing.Normalize()
	}
	if usageLog != nil {
		usageLog.RequestID = canonicalizeUsageBillingIdentity(usageLog.RequestID, 255)
		usageLog.Model = canonicalizeUsageBillingIdentity(usageLog.Model, 100)
		usageLog.RequestedModel = strings.TrimSpace(sanitizeUsageBillingPostgresText(usageLog.RequestedModel))
		if usageLog.RequestedModel == "" {
			usageLog.RequestedModel = usageLog.Model
		}
	}
	if err := validateBalanceSettlementPayload(settlement, usageLog); err != nil {
		return nil, nil, err
	}

	commandJSON, err := json.Marshal(balanceSettlementToPayloadV2(settlement))
	if err != nil {
		return nil, nil, fmt.Errorf("marshal balance settlement command: %w", err)
	}
	usageLogJSON := []byte("{}")
	if usageLog != nil {
		usageLogJSON, err = json.Marshal(usageLogToPayloadV1(usageLog))
		if err != nil {
			return nil, nil, fmt.Errorf("marshal balance settlement usage log: %w", err)
		}
	}
	return commandJSON, usageLogJSON, nil
}

func validateBalanceSettlementPayload(
	settlement *service.BalanceSettlementCommand,
	usageLog *service.UsageLog,
) error {
	if settlement == nil {
		return fmt.Errorf("%w: balance settlement command is required", service.ErrUsageBillingPayloadInvalid)
	}
	settlement.Normalize()
	if err := settlement.Validate(); err != nil {
		return err
	}
	for name, value := range map[string]float64{
		"hold_amount":   settlement.Hold.HoldAmount,
		"actual_amount": settlement.Hold.ActualAmount,
	} {
		if value < 0 || value >= usageBillingNumeric20Scale10UpperBound || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("%w: %s must fit PostgreSQL NUMERIC(20,10)", service.ErrUsageBillingPayloadInvalid, name)
		}
	}

	if settlement.Action == service.BalanceSettlementRelease {
		if usageLog != nil {
			return fmt.Errorf("%w: release settlement must not contain a usage log", service.ErrUsageBillingPayloadInvalid)
		}
		return nil
	}
	if settlement.Billing == nil || usageLog == nil {
		return fmt.Errorf("%w: capture settlement requires billing and usage log snapshots", service.ErrUsageBillingPayloadInvalid)
	}
	if settlement.Billing.RequestID != settlement.Hold.RequestID ||
		settlement.Billing.APIKeyID != settlement.Hold.APIKeyID ||
		settlement.Billing.UserID != settlement.Hold.UserID {
		return fmt.Errorf("%w: capture settlement identities differ", service.ErrUsageBillingPayloadInvalid)
	}
	// Reuse every v1 validation rule except its balance allocation rule. The
	// actual balance movement is performed from the hold, so v2 requires the
	// persisted command to keep BalanceCost at zero.
	validationCommand := *settlement.Billing
	validationCommand.BalanceCost = validationCommand.ActualCost
	if err := validateUsageBillingOutboxPayload(&validationCommand, usageLog); err != nil {
		return err
	}
	return nil
}

func decodeUsageBillingOutboxEvent(
	id int64,
	attempts int,
	createdAt time.Time,
	requestID string,
	apiKeyID int64,
	fingerprint string,
	payloadVersion int,
	stage int8,
	commandJSON []byte,
	usageLogJSON []byte,
	resultJSON []byte,
) (service.UsageBillingOutboxEvent, error) {
	switch payloadVersion {
	case usageBillingOutboxPayloadVersionV1:
		return decodeUsageBillingOutboxEventV1(
			id, attempts, createdAt, requestID, apiKeyID, fingerprint,
			payloadVersion, stage, commandJSON, usageLogJSON, resultJSON,
		)
	case usageBillingOutboxPayloadVersionV2, usageBillingOutboxPayloadVersionV3, usageBillingOutboxPayloadVersionV4:
		return decodeUsageBillingOutboxEventV2(
			id, attempts, createdAt, requestID, apiKeyID, fingerprint,
			payloadVersion, stage, commandJSON, usageLogJSON, resultJSON,
		)
	default:
		return service.UsageBillingOutboxEvent{}, fmt.Errorf("unsupported usage billing outbox payload version %d", payloadVersion)
	}
}

func decodeUsageBillingOutboxEventV1(
	id int64,
	attempts int,
	createdAt time.Time,
	requestID string,
	apiKeyID int64,
	fingerprint string,
	payloadVersion int,
	stage int8,
	commandJSON []byte,
	usageLogJSON []byte,
	resultJSON []byte,
) (service.UsageBillingOutboxEvent, error) {
	if stage != usageBillingOutboxStageBilling && stage != usageBillingOutboxStageEffects {
		return service.UsageBillingOutboxEvent{}, fmt.Errorf("unsupported usage billing outbox stage %d", stage)
	}
	var commandPayload usageBillingCommandPayloadV1
	if err := json.Unmarshal(commandJSON, &commandPayload); err != nil {
		return service.UsageBillingOutboxEvent{}, fmt.Errorf("decode usage billing command payload: %w", err)
	}
	var logPayload usageLogPayloadV1
	if err := json.Unmarshal(usageLogJSON, &logPayload); err != nil {
		return service.UsageBillingOutboxEvent{}, fmt.Errorf("decode usage billing log payload: %w", err)
	}
	cmd := commandPayload.command()
	if cmd.QuotaTime != nil {
		return service.UsageBillingOutboxEvent{}, fmt.Errorf("%w: video quota time requires payload v3", service.ErrUsageBillingPayloadInvalid)
	}
	cmd.Normalize()
	usageLog := logPayload.usageLog()
	payloadValidationError := usageBillingPayloadNumericError(
		commandPayload.InvalidNumerics,
		logPayload.InvalidNumerics,
	)
	recomputed := *cmd
	recomputed.RequestFingerprint = ""
	recomputed.Normalize()
	if cmd.RequestID != requestID || cmd.APIKeyID != apiKeyID ||
		cmd.RequestFingerprint != fingerprint ||
		strings.TrimSpace(usageLog.RequestID) != requestID || usageLog.APIKeyID != apiKeyID {
		return service.UsageBillingOutboxEvent{}, errors.New("usage billing outbox payload identity mismatch")
	}
	// Non-finite values are replaced only so PostgreSQL JSONB can durably hold
	// the intent. They intentionally cannot reproduce the original
	// fingerprint and are rejected by validateUsageBillingOutboxEvent before
	// any billing mutation.
	if payloadValidationError == "" && recomputed.RequestFingerprint != fingerprint {
		return service.UsageBillingOutboxEvent{}, errors.New("usage billing outbox payload identity mismatch")
	}
	// Historical v1 payloads can contain the original float in ActualCost while
	// their balance/subscription allocation was already rounded to NUMERIC(20,8).
	// Only canonicalize after the immutable fingerprint has been verified.
	canonicalizeUsageBillingChargeAmounts(cmd, usageLog)
	var result *service.UsageBillingApplyResult
	if stage == usageBillingOutboxStageEffects {
		if len(resultJSON) == 0 {
			return service.UsageBillingOutboxEvent{}, errors.New("usage billing outbox effects stage result is missing")
		}
		var payload usageBillingResultPayloadV1
		if err := json.Unmarshal(resultJSON, &payload); err != nil {
			return service.UsageBillingOutboxEvent{}, fmt.Errorf("decode usage billing result payload: %w", err)
		}
		result = payload.result()
	}
	return service.UsageBillingOutboxEvent{
		ID: id, Attempts: attempts, Stage: stage, PayloadVersion: payloadVersion,
		Command: cmd, UsageLog: usageLog,
		Result: result, PayloadValidationError: payloadValidationError, CreatedAt: createdAt,
	}, nil
}

func decodeUsageBillingOutboxEventV2(
	id int64,
	attempts int,
	createdAt time.Time,
	requestID string,
	apiKeyID int64,
	fingerprint string,
	payloadVersion int,
	stage int8,
	commandJSON []byte,
	usageLogJSON []byte,
	resultJSON []byte,
) (service.UsageBillingOutboxEvent, error) {
	if stage != usageBillingOutboxStageBilling && stage != usageBillingOutboxStageEffects {
		return service.UsageBillingOutboxEvent{}, fmt.Errorf("unsupported usage billing outbox stage %d", stage)
	}
	var payload balanceSettlementPayloadV2
	if err := json.Unmarshal(commandJSON, &payload); err != nil {
		return service.UsageBillingOutboxEvent{}, fmt.Errorf("decode balance settlement payload: %w", err)
	}
	settlement := payload.settlement()
	if (payloadVersion == usageBillingOutboxPayloadVersionV4) != (settlement.Hold.BillingReviewID > 0) {
		return service.UsageBillingOutboxEvent{}, fmt.Errorf("%w: reviewed video settlement requires payload v4", service.ErrUsageBillingPayloadInvalid)
	}
	if payloadVersion == usageBillingOutboxPayloadVersionV4 && settlement.Billing != nil {
		verified := *settlement.Billing
		verified.RequestFingerprint = ""
		verified.Normalize()
		if verified.RequestFingerprint != settlement.Billing.RequestFingerprint {
			return service.UsageBillingOutboxEvent{}, service.ErrUsageBillingPayloadInvalid
		}
	}
	if payloadVersion == usageBillingOutboxPayloadVersionV3 || (payloadVersion == usageBillingOutboxPayloadVersionV4 && settlement.Billing != nil && settlement.Billing.QuotaTime != nil) {
		if settlement.Action != service.BalanceSettlementCapture || settlement.Billing == nil || settlement.Billing.QuotaTime == nil {
			return service.UsageBillingOutboxEvent{}, fmt.Errorf("%w: v3 video quota time contract is missing", service.ErrUsageBillingPayloadInvalid)
		}
		billing := *settlement.Billing
		billing.RequestFingerprint = ""
		billing.Normalize()
		if billing.RequestFingerprint != settlement.Billing.RequestFingerprint {
			return service.UsageBillingOutboxEvent{}, fmt.Errorf("%w: v3 video billing fingerprint mismatch", service.ErrUsageBillingPayloadInvalid)
		}
	} else if payloadVersion != usageBillingOutboxPayloadVersionV4 && settlement.Billing != nil && settlement.Billing.QuotaTime != nil {
		return service.UsageBillingOutboxEvent{}, fmt.Errorf("%w: video quota time requires payload v3", service.ErrUsageBillingPayloadInvalid)
	}
	if settlement.Hold.RequestID != requestID || settlement.Hold.APIKeyID != apiKeyID ||
		settlement.Hold.RequestFingerprint != fingerprint {
		return service.UsageBillingOutboxEvent{}, errors.New("balance settlement outbox payload identity mismatch")
	}
	recomputed := settlement.Hold
	recomputed.RequestFingerprint = ""
	recomputed.Normalize()
	payloadValidationError := usageBillingPayloadNumericError(payload.InvalidNumerics, nil)
	if payloadValidationError == "" && recomputed.RequestFingerprint != fingerprint {
		return service.UsageBillingOutboxEvent{}, errors.New("balance settlement outbox payload identity mismatch")
	}
	if payload.AllowOverCapture != (settlement.Hold.Scope == service.BalanceHoldScopeVideoTask) {
		return service.UsageBillingOutboxEvent{}, errors.New("balance settlement outbox over-capture policy mismatch")
	}

	var usageLog *service.UsageLog
	if settlement.Action == service.BalanceSettlementCapture {
		var logPayload usageLogPayloadV1
		if err := json.Unmarshal(usageLogJSON, &logPayload); err != nil {
			return service.UsageBillingOutboxEvent{}, fmt.Errorf("decode balance settlement usage log payload: %w", err)
		}
		usageLog = logPayload.usageLog()
		payloadValidationError = usageBillingPayloadNumericError(payload.InvalidNumerics, logPayload.InvalidNumerics)
	}

	var result *service.UsageBillingApplyResult
	if stage == usageBillingOutboxStageEffects {
		if len(resultJSON) == 0 {
			return service.UsageBillingOutboxEvent{}, errors.New("balance settlement outbox effects stage result is missing")
		}
		var resultPayload usageBillingResultPayloadV1
		if err := json.Unmarshal(resultJSON, &resultPayload); err != nil {
			return service.UsageBillingOutboxEvent{}, fmt.Errorf("decode balance settlement result payload: %w", err)
		}
		result = resultPayload.result()
		if settlement.Billing != nil && settlement.Billing.QuotaTime != nil && (result.QuotaPostedAt == nil || result.QuotaPostedAt.Before(settlement.Billing.OccurredAt)) {
			return service.UsageBillingOutboxEvent{}, fmt.Errorf("%w: video quota posting receipt is missing or invalid", service.ErrUsageBillingPayloadInvalid)
		}
	}

	return service.UsageBillingOutboxEvent{
		ID: id, Attempts: attempts, Stage: stage, PayloadVersion: payloadVersion,
		Command: settlement.Billing, UsageLog: usageLog, BalanceSettlement: settlement,
		Result: result, PayloadValidationError: payloadValidationError, CreatedAt: createdAt,
	}, nil
}

func (r *usageBillingRepository) ApplyAndRecord(
	ctx context.Context,
	cmd *service.UsageBillingCommand,
	usageLog *service.UsageLog,
) (*service.UsageBillingApplyResult, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	commandJSON, usageLogJSON, err := marshalUsageBillingOutboxPayload(cmd, usageLog)
	if err != nil {
		return nil, err
	}

	workerID := "inline-" + uuid.NewString()
	event, err := r.enqueueAndClaimUsageBillingOutbox(ctx, workerID, cmd, commandJSON, usageLogJSON)
	if err != nil {
		return nil, err
	}
	result, err := r.CompleteUsageBillingOutbox(ctx, workerID, event)
	if err == nil {
		return result, nil
	}

	retryCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	var retryErr error
	if isPermanentUsageBillingOutboxError(err) {
		retryErr = r.QuarantineUsageBillingOutbox(
			retryCtx,
			workerID,
			event.ID,
			boundedUsageBillingRepositoryError(err),
		)
	} else {
		retryErr = r.RetryUsageBillingOutbox(
			retryCtx,
			workerID,
			event.ID,
			time.Now().UTC().Add(usageBillingInlineRetryDelay),
			boundedUsageBillingRepositoryError(err),
		)
	}
	cancel()
	if retryErr != nil {
		return nil, errors.Join(
			service.ErrUsageBillingIntentPending,
			err,
			fmt.Errorf("release durable usage billing intent: %w", retryErr),
		)
	}
	if !isPermanentUsageBillingOutboxError(err) {
		return nil, errors.Join(service.ErrUsageBillingIntentPending, err)
	}
	return nil, err
}

func (r *usageBillingRepository) ResumeVideoBalanceSettlement(ctx context.Context, task *service.VideoTask) (*service.UsageBillingApplyResult, *service.UsageBillingCommand, bool, error) {
	if r == nil || r.db == nil {
		return nil, nil, false, errors.New("video settlement recovery repository is not configured")
	}
	if task == nil || task.ID <= 0 || task.APIKeyID == nil || !service.IsValidVideoTaskID(task.PublicID) {
		return nil, nil, false, service.ErrVideoInvalidRequest
	}
	requestID := service.VideoTaskCaptureRequestID(task.PublicID)
	if task.BillingState == service.VideoBillingReleasePending {
		requestID = service.VideoTaskReleaseRequestID(task.PublicID)
	} else if task.BillingState != service.VideoBillingCapturePending {
		return nil, nil, false, service.ErrVideoInvalidTransition
	}
	var id int64
	var attempts, version int
	var stage int8
	var createdAt time.Time
	var fingerprint string
	var commandJSON, usageJSON, resultJSON []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT id, attempts, created_at, request_fingerprint, payload_version, stage,
			command_payload, usage_log_payload, result_payload
		FROM usage_billing_outbox WHERE request_id = $1 AND api_key_id = $2
	`, requestID, *task.APIKeyID).Scan(&id, &attempts, &createdAt, &fingerprint, &version, &stage, &commandJSON, &usageJSON, &resultJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	stored, err := decodeUsageBillingOutboxEvent(id, attempts, createdAt, requestID, *task.APIKeyID, fingerprint, version, stage, commandJSON, usageJSON, resultJSON)
	if err != nil {
		return nil, nil, true, err
	}
	if stored.BalanceSettlement == nil || stored.BalanceSettlement.TaskID != task.ID ||
		stored.BalanceSettlement.Hold.UserID != task.UserID || stored.BalanceSettlement.Hold.RefID != task.PublicID {
		return nil, nil, true, service.ErrUsageBillingRequestConflict
	}
	workerID := "inline-video-recovery-" + uuid.NewString()
	event, err := r.enqueueAndClaimUsageBillingOutboxPayload(ctx, workerID, requestID, *task.APIKeyID, fingerprint, version, commandJSON, usageJSON)
	if err != nil {
		return nil, nil, true, err
	}
	result, err := r.completeClaimedVideoSettlement(ctx, workerID, event)
	return result, event.Command, true, err
}

func (r *usageBillingRepository) SettleVideoBalance(
	ctx context.Context,
	settlement *service.BalanceSettlementCommand,
	usageLog *service.UsageLog,
) (*service.UsageBillingApplyResult, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	commandJSON, usageLogJSON, err := marshalBalanceSettlementOutboxPayload(settlement, usageLog)
	if err != nil {
		return nil, err
	}
	workerID := "inline-video-" + uuid.NewString()
	payloadVersion := usageBillingOutboxPayloadVersionV2
	if settlement.Billing != nil && settlement.Billing.QuotaTime != nil {
		payloadVersion = usageBillingOutboxPayloadVersionV3
	}
	if settlement.Hold.BillingReviewID > 0 {
		payloadVersion = usageBillingOutboxPayloadVersionV4
	}
	event, err := r.enqueueAndClaimUsageBillingOutboxPayload(
		ctx,
		workerID,
		settlement.Hold.RequestID,
		settlement.Hold.APIKeyID,
		settlement.Hold.RequestFingerprint,
		payloadVersion,
		commandJSON,
		usageLogJSON,
	)
	if err != nil {
		return nil, err
	}
	return r.completeClaimedVideoSettlement(ctx, workerID, event)
}

func (r *usageBillingRepository) completeClaimedVideoSettlement(ctx context.Context, workerID string, event service.UsageBillingOutboxEvent) (*service.UsageBillingApplyResult, error) {
	result, err := r.CompleteUsageBillingOutbox(ctx, workerID, event)
	if err == nil {
		return result, nil
	}

	retryCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var retryErr error
	if isPermanentUsageBillingOutboxError(err) {
		retryErr = r.QuarantineUsageBillingOutbox(retryCtx, workerID, event.ID, boundedUsageBillingRepositoryError(err))
	} else {
		retryErr = r.RetryUsageBillingOutbox(
			retryCtx,
			workerID,
			event.ID,
			time.Now().UTC().Add(usageBillingInlineRetryDelay),
			boundedUsageBillingRepositoryError(err),
		)
	}
	if retryErr != nil {
		return nil, errors.Join(
			service.ErrUsageBillingIntentPending,
			err,
			fmt.Errorf("release durable video settlement intent: %w", retryErr),
		)
	}
	if !isPermanentUsageBillingOutboxError(err) {
		return nil, errors.Join(service.ErrUsageBillingIntentPending, err)
	}
	return nil, err
}

func (r *usageBillingRepository) AcknowledgeVideoBalanceSettlement(ctx context.Context, workerID string, eventID int64) error {
	return r.AcknowledgeUsageBillingOutbox(ctx, workerID, eventID)
}

func isPermanentUsageBillingOutboxError(err error) bool {
	return errors.Is(err, service.ErrUsageBillingRequestConflict) ||
		errors.Is(err, service.ErrUsageBillingPayloadInvalid)
}

func (r *usageBillingRepository) enqueueAndClaimUsageBillingOutbox(
	ctx context.Context,
	workerID string,
	cmd *service.UsageBillingCommand,
	commandJSON []byte,
	usageLogJSON []byte,
) (event service.UsageBillingOutboxEvent, err error) {
	return r.enqueueAndClaimUsageBillingOutboxPayload(
		ctx,
		workerID,
		cmd.RequestID,
		cmd.APIKeyID,
		cmd.RequestFingerprint,
		usageBillingOutboxPayloadVersionV1,
		commandJSON,
		usageLogJSON,
	)
}

func (r *usageBillingRepository) enqueueAndClaimUsageBillingOutboxPayload(
	ctx context.Context,
	workerID string,
	requestID string,
	apiKeyID int64,
	requestFingerprint string,
	payloadVersionToInsert int,
	commandJSON []byte,
	usageLogJSON []byte,
) (event service.UsageBillingOutboxEvent, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return event, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if err = r.validateUsageBillingFingerprintIdentityBeforeEnqueue(ctx, tx, requestID, apiKeyID, requestFingerprint); err != nil {
		return event, err
	}

	var (
		id             int64
		attempts       int
		payloadVersion int
		stage          int8
		createdAt      time.Time
		storedCmd      []byte
		storedLog      []byte
		resultJSON     []byte
	)
	err = tx.QueryRowContext(ctx, `
		INSERT INTO usage_billing_outbox (
			request_id, api_key_id, request_fingerprint, payload_version,
			command_payload, usage_log_payload, claimed_at, claimed_by
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, NOW(), $7)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id, attempts, created_at, payload_version, stage,
			command_payload, usage_log_payload, result_payload
	`, requestID, apiKeyID, requestFingerprint, payloadVersionToInsert,
		string(commandJSON), string(usageLogJSON), workerID,
	).Scan(&id, &attempts, &createdAt, &payloadVersion, &stage, &storedCmd, &storedLog, &resultJSON)
	newIntent := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
			UPDATE usage_billing_outbox
			SET claimed_at = NOW(), claimed_by = $4, updated_at = NOW()
			WHERE request_id = $1
			  AND api_key_id = $2
			  AND request_fingerprint = $3
			  AND terminal_at IS NULL
			  AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '2 minutes')
			RETURNING id, attempts, created_at, payload_version, stage,
				command_payload, usage_log_payload, result_payload
		`, requestID, apiKeyID, requestFingerprint, workerID).
			Scan(&id, &attempts, &createdAt, &payloadVersion, &stage, &storedCmd, &storedLog, &resultJSON)
		if errors.Is(err, sql.ErrNoRows) {
			var (
				existingFingerprint string
				terminalAt          sql.NullTime
			)
			if lookupErr := tx.QueryRowContext(ctx, `
				SELECT request_fingerprint, terminal_at
				FROM usage_billing_outbox
				WHERE request_id = $1 AND api_key_id = $2
			`, requestID, apiKeyID).Scan(&existingFingerprint, &terminalAt); lookupErr != nil {
				return event, lookupErr
			}
			if existingFingerprint != requestFingerprint {
				return event, service.ErrUsageBillingRequestConflict
			}
			if terminalAt.Valid {
				return event, fmt.Errorf("%w: existing usage billing intent is quarantined", service.ErrUsageBillingPayloadInvalid)
			}
			return event, errors.New("usage billing intent is already claimed for recovery")
		}
	}
	if err != nil {
		return event, err
	}
	event, err = decodeUsageBillingOutboxEvent(
		id, attempts, createdAt, requestID, apiKeyID, requestFingerprint,
		payloadVersion, stage, storedCmd, storedLog, resultJSON,
	)
	if err != nil {
		return event, err
	}
	if guard, guarded := service.VideoTaskWriteGuardFromContext(ctx); newIntent && guarded && event.BalanceSettlement != nil {
		if event.BalanceSettlement.TaskID != guard.TaskID {
			return event, service.ErrVideoInvalidRequest
		}
		task, err := scanVideoTask(tx.QueryRowContext(ctx, `SELECT to_jsonb(vt) FROM video_tasks vt WHERE id = $1 FOR UPDATE`, guard.TaskID))
		if err != nil {
			return event, err
		}
		if _, err := videoTaskGuardTx(ctx, tx, task); err != nil {
			return event, err
		}
	}
	if err = tx.Commit(); err != nil {
		return event, err
	}
	tx = nil
	return event, nil
}

func (r *usageBillingRepository) validateUsageBillingFingerprintIdentityBeforeEnqueue(
	ctx context.Context,
	tx *sql.Tx,
	requestID string,
	apiKeyID int64,
	requestFingerprint string,
) error {
	for _, table := range []string{"usage_billing_dedup", "usage_billing_dedup_archive"} {
		var existingFingerprint string
		query := fmt.Sprintf(`
			SELECT request_fingerprint
			FROM %s
			WHERE request_id = $1 AND api_key_id = $2
		`, table)
		err := tx.QueryRowContext(ctx, query, requestID, apiKeyID).Scan(&existingFingerprint)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if strings.TrimSpace(existingFingerprint) != strings.TrimSpace(requestFingerprint) {
			return service.ErrUsageBillingRequestConflict
		}
	}
	return nil
}

func (r *usageBillingRepository) ClaimUsageBillingOutbox(
	ctx context.Context,
	workerID string,
	limit int,
	lease time.Duration,
) (_ []service.UsageBillingOutboxEvent, err error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	if strings.TrimSpace(workerID) == "" {
		return nil, errors.New("usage billing outbox worker id is required")
	}
	if limit <= 0 {
		limit = 32
	}
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 1 {
		leaseSeconds = 120
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id
			FROM usage_billing_outbox
			WHERE available_at <= NOW()
			  AND terminal_at IS NULL
			  AND payload_version IN ($4, $5, $6, $7)
			  AND (claimed_at IS NULL OR claimed_at < NOW() - ($3 * INTERVAL '1 second'))
			ORDER BY id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE usage_billing_outbox AS o
		SET claimed_at = NOW(), claimed_by = $1, updated_at = NOW()
		FROM candidates AS c
		WHERE o.id = c.id
		RETURNING o.id, o.attempts, o.created_at, o.request_id, o.api_key_id,
			o.request_fingerprint, o.payload_version, o.stage,
			o.command_payload, o.usage_log_payload, o.result_payload
	`, workerID, limit, leaseSeconds, usageBillingOutboxPayloadVersionV1, usageBillingOutboxPayloadVersionV2, usageBillingOutboxPayloadVersionV3, usageBillingOutboxPayloadVersionV4)
	if err != nil {
		return nil, err
	}

	type rawUsageBillingOutboxRow struct {
		id, apiKeyID                  int64
		attempts, payloadVersion      int
		stage                         int8
		createdAt                     time.Time
		requestID, requestFingerprint string
		commandJSON, usageLogJSON     []byte
		resultJSON                    []byte
	}
	rawRows := make([]rawUsageBillingOutboxRow, 0, limit)
	for rows.Next() {
		var raw rawUsageBillingOutboxRow
		if err := rows.Scan(
			&raw.id, &raw.attempts, &raw.createdAt, &raw.requestID, &raw.apiKeyID,
			&raw.requestFingerprint, &raw.payloadVersion, &raw.stage,
			&raw.commandJSON, &raw.usageLogJSON, &raw.resultJSON,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		rawRows = append(rawRows, raw)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	events := make([]service.UsageBillingOutboxEvent, 0, len(rawRows))
	for _, raw := range rawRows {
		event, err := decodeUsageBillingOutboxEvent(
			raw.id, raw.attempts, raw.createdAt, raw.requestID, raw.apiKeyID, raw.requestFingerprint,
			raw.payloadVersion, raw.stage, raw.commandJSON, raw.usageLogJSON, raw.resultJSON,
		)
		if err != nil {
			reason := boundedUsageBillingRepositoryError(
				fmt.Errorf("decode usage billing outbox row %d: %w", raw.id, err),
			)
			result, terminalErr := tx.ExecContext(ctx, `
				UPDATE usage_billing_outbox
				SET terminal_at = NOW(),
					terminal_reason = $3,
					last_error = $3,
					claimed_at = NULL,
					claimed_by = NULL,
					updated_at = NOW()
				WHERE id = $1 AND claimed_by = $2
			`, raw.id, workerID, reason)
			if terminalErr != nil {
				return nil, terminalErr
			}
			affected, terminalErr := result.RowsAffected()
			if terminalErr != nil {
				return nil, terminalErr
			}
			if affected != 1 {
				return nil, errors.New("usage billing poison row claim was lost")
			}
			slog.Error("usage billing outbox row quarantined during claim",
				"outbox_id", raw.id,
				"request_id", raw.requestID,
				"api_key_id", raw.apiKeyID,
				"payload_version", raw.payloadVersion,
				"stage", raw.stage,
				"error", err,
			)
			continue
		}
		events = append(events, event)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return events, nil
}

func (r *usageBillingRepository) CompleteUsageBillingOutbox(
	ctx context.Context,
	workerID string,
	event service.UsageBillingOutboxEvent,
) (_ *service.UsageBillingApplyResult, err error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	if event.ID <= 0 || strings.TrimSpace(workerID) == "" {
		return nil, errors.New("usage billing outbox claim identity is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var (
		attempts, payloadVersion      int
		stage                         int8
		createdAt                     time.Time
		requestID, requestFingerprint string
		apiKeyID                      int64
		commandJSON, usageLogJSON     []byte
		resultJSON                    []byte
	)
	err = tx.QueryRowContext(ctx, `
		SELECT attempts, created_at, request_id, api_key_id, request_fingerprint,
			payload_version, stage, command_payload, usage_log_payload, result_payload
		FROM usage_billing_outbox
		WHERE id = $1 AND claimed_by = $2 AND terminal_at IS NULL
		FOR UPDATE
	`, event.ID, workerID).Scan(
		&attempts, &createdAt, &requestID, &apiKeyID, &requestFingerprint,
		&payloadVersion, &stage, &commandJSON, &usageLogJSON, &resultJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("usage billing outbox claim is no longer owned by worker")
	}
	if err != nil {
		return nil, err
	}
	storedEvent, err := decodeUsageBillingOutboxEvent(
		event.ID, attempts, createdAt, requestID, apiKeyID, requestFingerprint,
		payloadVersion, stage, commandJSON, usageLogJSON, resultJSON,
	)
	if err != nil {
		return nil, err
	}
	if storedEvent.Stage == usageBillingOutboxStageEffects {
		if storedEvent.Result == nil {
			return nil, errors.New("usage billing outbox effects result is missing")
		}
		storedEvent.Result.OutboxReceipt = &service.UsageBillingOutboxReceipt{
			ID: event.ID, WorkerID: workerID,
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		tx = nil
		return storedEvent.Result, nil
	}
	if err := validateUsageBillingOutboxEvent(storedEvent); err != nil {
		return nil, err
	}
	if storedEvent.BalanceSettlement != nil {
		result, err := r.completeVideoBalanceSettlementTx(ctx, tx, workerID, storedEvent)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		tx = nil
		result.OutboxReceipt = &service.UsageBillingOutboxReceipt{ID: event.ID, WorkerID: workerID}
		return result, nil
	}
	if storedEvent.Command.PlatformQuotaCost > 0 &&
		storedEvent.Command.PlatformQuotaSnapshotNeeded {
		return nil, service.ErrUsageBillingPlatformQuotaSnapshotRequired
	}

	existingLog, exists, err := loadUsageBillingExistingLogPayload(ctx, tx, requestID, apiKeyID)
	if err != nil {
		return nil, err
	}
	intendedLog := usageLogToPayloadV1(storedEvent.UsageLog)
	recoverableFallback := exists && usageLogPayloadsRecoverableUnchargedFallback(existingLog, intendedLog)
	if exists &&
		!usageLogPayloadsBillingEquivalent(existingLog, intendedLog) &&
		!recoverableFallback {
		return nil, service.ErrUsageBillingRequestConflict
	}

	claimed, err := r.claimUsageBillingKey(ctx, tx, storedEvent.Command)
	if err != nil {
		return nil, err
	}
	// A legacy usage_log without the newer billing dedup row is evidence that
	// the pre-upgrade request was already finalized. Backfill the fingerprint
	// but never debit it again. The sole exception is the old unsettled fallback
	// row: actual_cost=0 with every other immutable billing fact matching the
	// durable intent proves that the request was logged but never charged.
	applied := claimed && (!exists || recoverableFallback)
	result := &service.UsageBillingApplyResult{
		Applied: applied,
		// A dedup key without a usage log is the exact crash window of the
		// pre-outbox flow: billing committed, while usage logging and possibly
		// cache post-effects did not. Persist this bit in result_payload so a
		// restarted worker repairs projections without debiting or notifying.
		ProjectionRepairRequired: !claimed && (!exists || recoverableFallback),
	}
	if applied {
		if err := r.applyUsageBillingEffects(ctx, tx, storedEvent.Command, result); err != nil {
			return nil, err
		}
	}

	if recoverableFallback {
		updateLogResult, updateErr := tx.ExecContext(ctx, `
			UPDATE usage_logs
			SET actual_cost = $3
			WHERE request_id = $1
			  AND api_key_id = $2
			  AND actual_cost = 0
		`, requestID, apiKeyID, storedEvent.UsageLog.ActualCost)
		if updateErr != nil {
			return nil, fmt.Errorf("repair unsettled usage log in billing transaction: %w", updateErr)
		}
		updated, rowsErr := updateLogResult.RowsAffected()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if updated != 1 {
			return nil, service.ErrUsageBillingRequestConflict
		}
	} else {
		logRepo := &usageLogRepository{}
		inserted, insertErr := logRepo.createSingle(ctx, tx, storedEvent.UsageLog)
		if insertErr != nil {
			return nil, fmt.Errorf("insert usage log in billing transaction: %w", insertErr)
		}
		if !inserted && !exists {
			racedLog, racedExists, loadErr := loadUsageBillingExistingLogPayload(ctx, tx, requestID, apiKeyID)
			if loadErr != nil {
				return nil, loadErr
			}
			if !racedExists || !usageLogPayloadsBillingEquivalent(racedLog, intendedLog) {
				return nil, service.ErrUsageBillingRequestConflict
			}
		}
	}
	result.UsageLogRecorded = true

	resultPayload, err := json.Marshal(usageBillingResultToPayloadV1(result))
	if err != nil {
		return nil, err
	}
	updateResult, err := tx.ExecContext(ctx, `
		UPDATE usage_billing_outbox
		SET stage = $3,
			result_payload = $4::jsonb,
			last_error = NULL,
			updated_at = NOW()
		WHERE id = $1 AND claimed_by = $2 AND stage = $5 AND terminal_at IS NULL
	`, event.ID, workerID, usageBillingOutboxStageEffects, string(resultPayload), usageBillingOutboxStageBilling)
	if err != nil {
		return nil, err
	}
	affected, err := updateResult.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, errors.New("usage billing outbox claim was lost before stage commit")
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	result.OutboxReceipt = &service.UsageBillingOutboxReceipt{ID: event.ID, WorkerID: workerID}
	return result, nil
}

func (r *usageBillingRepository) completeVideoBalanceSettlementTx(
	ctx context.Context,
	tx *sql.Tx,
	workerID string,
	event service.UsageBillingOutboxEvent,
) (*service.UsageBillingApplyResult, error) {
	settlement := event.BalanceSettlement
	if settlement == nil {
		return nil, fmt.Errorf("%w: balance settlement is missing", service.ErrUsageBillingPayloadInvalid)
	}
	if _, err := lockVideoBudgetOwnerTx(ctx, tx, settlement.Hold.UserID); err != nil {
		return nil, err
	}
	var (
		publicID      string
		billingState  string
		taskUserID    sql.NullInt64
		taskAPIKeyID  sql.NullInt64
		taskHold      sql.NullFloat64
		taskSettledAt sql.NullTime
	)
	err := tx.QueryRowContext(ctx, `
		SELECT public_id, billing_state, user_id, api_key_id, hold_amount, settled_at
		FROM video_tasks
		WHERE id = $1
		FOR UPDATE
	`, settlement.TaskID).Scan(&publicID, &billingState, &taskUserID, &taskAPIKeyID, &taskHold, &taskSettledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	if publicID != settlement.Hold.RefID || !taskUserID.Valid || !taskAPIKeyID.Valid ||
		taskUserID.Int64 != settlement.Hold.UserID || taskAPIKeyID.Int64 != settlement.Hold.APIKeyID ||
		!taskHold.Valid || math.Abs(taskHold.Float64-settlement.Hold.HoldAmount) > 0.00000001 {
		return nil, fmt.Errorf("%w: video task and settlement facts differ", service.ErrUsageBillingPayloadInvalid)
	}

	expectedState := service.VideoBillingCapturePending
	targetState := service.VideoBillingCaptured
	eventType := "billing_captured"
	if settlement.Action == service.BalanceSettlementRelease {
		expectedState = service.VideoBillingReleasePending
		targetState = service.VideoBillingReleased
		eventType = "billing_released"
	}
	if billingState != expectedState && billingState != targetState {
		return nil, fmt.Errorf(
			"%w: expected video billing state %s or %s, got %s",
			service.ErrVideoInvalidTransition,
			expectedState,
			targetState,
			billingState,
		)
	}

	claimed, err := r.claimUsageBillingRequest(
		ctx,
		tx,
		settlement.Hold.RequestID,
		settlement.Hold.APIKeyID,
		settlement.Hold.RequestFingerprint,
	)
	if err != nil {
		return nil, err
	}
	if billingState == targetState {
		if claimed {
			return nil, fmt.Errorf("%w: terminal video billing state has no prior settlement claim", service.ErrUsageBillingPayloadInvalid)
		}
		result := &service.UsageBillingApplyResult{Applied: false}
		if settlement.Billing != nil && settlement.Billing.QuotaTime != nil {
			if !taskSettledAt.Valid || taskSettledAt.Time.Before(settlement.Billing.OccurredAt) {
				return nil, fmt.Errorf("%w: video quota posting time is missing", service.ErrUsageBillingPayloadInvalid)
			}
			result.QuotaPostedAt = &taskSettledAt.Time
		}
		if settlement.Action == service.BalanceSettlementCapture {
			existingLog, exists, err := loadUsageBillingExistingLogPayload(
				ctx, tx, settlement.Hold.RequestID, settlement.Hold.APIKeyID,
			)
			if err != nil {
				return nil, err
			}
			if !exists || !usageLogPayloadsBillingEquivalent(existingLog, usageLogToPayloadV1(event.UsageLog)) {
				return nil, service.ErrUsageBillingRequestConflict
			}
			result.UsageLogRecorded = true
		}
		if err := stageUsageBillingOutboxResultTx(ctx, tx, workerID, event.ID, result); err != nil {
			return nil, err
		}
		return result, nil
	}
	if !claimed {
		return nil, service.ErrUsageBillingRequestConflict
	}

	result := &service.UsageBillingApplyResult{Applied: true}
	if settlement.Action == service.BalanceSettlementCapture {
		holdResult, err := captureUsageBillingBalance(ctx, tx, &settlement.Hold)
		if err != nil {
			return nil, err
		}
		result.NewBalance = holdResult.NewBalance
		result.FrozenBalance = holdResult.FrozenBalance
		result.BalanceOverdrafted = holdResult.BalanceOverdrafted
		if err := r.applyUsageBillingEffects(ctx, tx, settlement.Billing, result); err != nil {
			return nil, err
		}

		existingLog, exists, err := loadUsageBillingExistingLogPayload(
			ctx, tx, settlement.Hold.RequestID, settlement.Hold.APIKeyID,
		)
		if err != nil {
			return nil, err
		}
		intendedLog := usageLogToPayloadV1(event.UsageLog)
		if exists && !usageLogPayloadsBillingEquivalent(existingLog, intendedLog) {
			return nil, service.ErrUsageBillingRequestConflict
		}
		if !exists {
			logRepo := &usageLogRepository{}
			inserted, err := logRepo.createSingle(ctx, tx, event.UsageLog)
			if err != nil {
				return nil, fmt.Errorf("insert video settlement usage log: %w", err)
			}
			if !inserted {
				return nil, service.ErrUsageBillingRequestConflict
			}
		}
		result.UsageLogRecorded = true
	} else {
		holdResult, err := releaseUsageBillingBalance(ctx, tx, &settlement.Hold)
		if err != nil {
			return nil, err
		}
		result.NewBalance = holdResult.NewBalance
		result.FrozenBalance = holdResult.FrozenBalance
	}

	updateResult, err := tx.ExecContext(ctx, `
		UPDATE video_tasks
		SET billing_state = $2,
			actual_cost = $3,
			settled_at = COALESCE($5::timestamptz, NOW()),
			callback_intent_state = CASE WHEN callback_url_enc IS NOT NULL AND callback_intent_state = 'none' THEN 'pending' ELSE callback_intent_state END,
			updated_at = NOW(),
			version = version + 1
		WHERE id = $1 AND billing_state = $4
	`, settlement.TaskID, targetState, settlement.Hold.ActualAmount, expectedState, result.QuotaPostedAt)
	if err != nil {
		return nil, err
	}
	updated, err := updateResult.RowsAffected()
	if err != nil {
		return nil, err
	}
	if updated != 1 {
		return nil, service.ErrVideoInvalidTransition
	}

	eventFacts := map[string]any{
		"action": settlement.Action, "hold_amount": settlement.Hold.HoldAmount,
		"actual_amount": settlement.Hold.ActualAmount,
	}
	if result.QuotaPostedAt != nil {
		eventFacts["quota_posted_at"] = result.QuotaPostedAt
	}
	if settlement.Hold.BillingReviewID > 0 {
		eventFacts["billing_review_id"] = settlement.Hold.BillingReviewID
	}
	eventPayload, err := json.Marshal(eventFacts)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO video_task_events (
			task_id, event_type, from_billing_state, to_billing_state, payload, event_hash
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		ON CONFLICT (task_id, event_hash) WHERE task_id IS NOT NULL AND event_hash IS NOT NULL AND event_hash <> ''
		DO NOTHING
	`, settlement.TaskID, eventType, expectedState, targetState, string(eventPayload), settlement.Hold.RequestFingerprint); err != nil {
		return nil, err
	}

	if err := stageUsageBillingOutboxResultTx(ctx, tx, workerID, event.ID, result); err != nil {
		return nil, err
	}
	return result, nil
}

func stageUsageBillingOutboxResultTx(
	ctx context.Context,
	tx *sql.Tx,
	workerID string,
	eventID int64,
	result *service.UsageBillingApplyResult,
) error {
	resultPayload, err := json.Marshal(usageBillingResultToPayloadV1(result))
	if err != nil {
		return err
	}
	stageResult, err := tx.ExecContext(ctx, `
		UPDATE usage_billing_outbox
		SET stage = $3,
			result_payload = $4::jsonb,
			last_error = NULL,
			updated_at = NOW()
		WHERE id = $1 AND claimed_by = $2 AND stage = $5 AND terminal_at IS NULL
	`, eventID, workerID, usageBillingOutboxStageEffects, string(resultPayload), usageBillingOutboxStageBilling)
	if err != nil {
		return err
	}
	staged, err := stageResult.RowsAffected()
	if err != nil {
		return err
	}
	if staged != 1 {
		return errors.New("video settlement outbox claim was lost before stage commit")
	}
	return nil
}

func loadUsageBillingExistingLogPayload(
	ctx context.Context,
	tx *sql.Tx,
	requestID string,
	apiKeyID int64,
) (usageLogPayloadV1, bool, error) {
	var payloadJSON []byte
	err := tx.QueryRowContext(ctx, `
		SELECT to_jsonb(ul)
		FROM usage_logs AS ul
		WHERE request_id = $1 AND api_key_id = $2
		FOR UPDATE
	`, requestID, apiKeyID).Scan(&payloadJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return usageLogPayloadV1{}, false, nil
	}
	if err != nil {
		return usageLogPayloadV1{}, false, err
	}
	var payload usageLogPayloadV1
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return usageLogPayloadV1{}, false, fmt.Errorf("decode existing usage log payload: %w", err)
	}
	return payload, true, nil
}

func (r *usageBillingRepository) UpdateUsageBillingOutboxCommand(
	ctx context.Context,
	workerID string,
	eventID int64,
	cmd *service.UsageBillingCommand,
) error {
	if r == nil || r.db == nil {
		return errors.New("usage billing repository db is nil")
	}
	if eventID <= 0 || strings.TrimSpace(workerID) == "" || cmd == nil {
		return errors.New("usage billing outbox command update identity is required")
	}
	if err := cmd.ValidateQuotaTime(); err != nil {
		return err
	}
	cmd.Normalize()
	if cmd.QuotaTime != nil {
		verified := *cmd
		verified.RequestFingerprint = ""
		verified.Normalize()
		if verified.RequestFingerprint != cmd.RequestFingerprint {
			return service.ErrUsageBillingRequestConflict
		}
	}
	commandJSON, err := json.Marshal(commandToUsageBillingPayloadV1(cmd))
	if err != nil {
		return fmt.Errorf("marshal updated usage billing command: %w", err)
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE usage_billing_outbox
		SET command_payload = CASE
				WHEN payload_version IN (2, 3, 4) THEN jsonb_set(command_payload, '{billing}', $5::jsonb, true)
				ELSE $5::jsonb
			END,
			updated_at = NOW()
		WHERE id = $1
		  AND claimed_by = $2
		  AND request_id = $3
		  AND api_key_id = $4
		  AND ((payload_version = 1 AND request_fingerprint = $6)
			OR (payload_version IN (2, 3, 4) AND command_payload #>> '{billing,request_fingerprint}' = $6))
		  AND stage = $7
		  AND terminal_at IS NULL
	`, eventID, workerID, cmd.RequestID, cmd.APIKeyID, string(commandJSON),
		cmd.RequestFingerprint, usageBillingOutboxStageBilling)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("usage billing outbox command update claim is no longer owned by worker")
	}
	return nil
}

func (r *usageBillingRepository) AcknowledgeUsageBillingOutbox(
	ctx context.Context,
	workerID string,
	eventID int64,
) error {
	if r == nil || r.db == nil {
		return errors.New("usage billing repository db is nil")
	}
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM usage_billing_outbox
		WHERE id = $1
		  AND claimed_by = $2
		  AND stage = $3
		  AND terminal_at IS NULL
	`, eventID, workerID, usageBillingOutboxStageEffects)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("usage billing outbox acknowledgement claim is no longer owned by worker")
	}
	return nil
}

func (r *usageBillingRepository) QuarantineUsageBillingOutbox(
	ctx context.Context,
	workerID string,
	eventID int64,
	reason string,
) error {
	if r == nil || r.db == nil {
		return errors.New("usage billing repository db is nil")
	}
	reason = boundedUsageBillingRepositoryError(errors.New(reason))
	result, err := r.db.ExecContext(ctx, `
		UPDATE usage_billing_outbox
		SET terminal_at = NOW(),
			terminal_reason = $3,
			last_error = $3,
			claimed_at = NULL,
			claimed_by = NULL,
			updated_at = NOW()
		WHERE id = $1 AND claimed_by = $2 AND terminal_at IS NULL
	`, eventID, workerID, reason)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("usage billing outbox quarantine claim is no longer owned by worker")
	}
	return nil
}

func (r *usageBillingRepository) RetryUsageBillingOutbox(
	ctx context.Context,
	workerID string,
	eventID int64,
	availableAt time.Time,
	lastError string,
) error {
	if r == nil || r.db == nil {
		return errors.New("usage billing repository db is nil")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE usage_billing_outbox
		SET attempts = attempts + 1,
			available_at = $3,
			last_error = $4,
			claimed_at = NULL,
			claimed_by = NULL,
			updated_at = NOW()
		WHERE id = $1 AND claimed_by = $2 AND terminal_at IS NULL
	`, eventID, workerID, availableAt, boundedUsageBillingRepositoryError(errors.New(lastError)))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("usage billing outbox claim is no longer owned by worker")
	}
	return nil
}

func boundedUsageBillingRepositoryError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToValidUTF8(err.Error(), "\uFFFD")
	message = strings.ReplaceAll(message, "\x00", "\uFFFD")
	const maxBytes = 1024
	if len(message) <= maxBytes {
		return message
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut]
}
