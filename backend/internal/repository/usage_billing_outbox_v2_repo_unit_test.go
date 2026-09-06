//go:build unit

package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newVideoSettlementTestPayload(t *testing.T) (*service.BalanceSettlementCommand, *service.UsageLog, []byte, []byte) {
	t.Helper()
	createdAt := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	publicID := "video_0123456789abcdef0123456789abcdef"
	requestID := service.VideoTaskCaptureRequestID(publicID)
	settlement := &service.BalanceSettlementCommand{
		TaskID: 101,
		Action: service.BalanceSettlementCapture,
		Hold: service.BalanceHoldCommand{
			RequestID:          requestID,
			APIKeyID:           7,
			RequestPayloadHash: "video-payload-hash",
			UserID:             42,
			Scope:              service.BalanceHoldScopeVideoTask,
			RefID:              publicID,
			HoldAmount:         2,
			ActualAmount:       1.5,
		},
		Billing: &service.UsageBillingCommand{
			RequestID:           requestID,
			APIKeyID:            7,
			RequestPayloadHash:  "video-payload-hash",
			UserID:              42,
			AccountID:           11,
			AccountType:         service.AccountTypeAPIKey,
			Model:               "sora-2",
			BillingType:         service.BillingTypeBalance,
			ActualCost:          1.5,
			TotalCost:           1.5,
			BalanceCost:         0,
			APIKeyQuotaCost:     1.5,
			APIKeyRateLimitCost: 1.5,
			OccurredAt:          createdAt,
		},
	}
	duration := 8
	resolution := "1280x720"
	mediaType := "video"
	usageLog := &service.UsageLog{
		UserID:               42,
		APIKeyID:             7,
		AccountID:            11,
		RequestID:            requestID,
		Model:                "sora-2",
		BillingType:          service.BillingTypeBalance,
		TotalCost:            1.5,
		ActualCost:           1.5,
		VideoCount:           1,
		VideoDurationSeconds: &duration,
		VideoResolution:      &resolution,
		MediaType:            &mediaType,
		CreatedAt:            createdAt,
	}
	commandJSON, usageLogJSON, err := marshalBalanceSettlementOutboxPayload(settlement, usageLog)
	require.NoError(t, err)
	return settlement, usageLog, commandJSON, usageLogJSON
}

func TestBalanceSettlementPayloadV2CaptureRoundTrip(t *testing.T) {
	settlement, usageLog, commandJSON, usageLogJSON := newVideoSettlementTestPayload(t)

	decoded, err := decodeUsageBillingOutboxEvent(
		99,
		2,
		usageLog.CreatedAt,
		settlement.Hold.RequestID,
		settlement.Hold.APIKeyID,
		settlement.Hold.RequestFingerprint,
		usageBillingOutboxPayloadVersionV2,
		usageBillingOutboxStageBilling,
		commandJSON,
		usageLogJSON,
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, usageBillingOutboxPayloadVersionV2, decoded.PayloadVersion)
	require.NotNil(t, decoded.BalanceSettlement)
	require.Equal(t, service.BalanceSettlementCapture, decoded.BalanceSettlement.Action)
	require.Zero(t, decoded.Command.BalanceCost)
	require.Equal(t, 1.5, decoded.BalanceSettlement.Hold.ActualAmount)
	require.NoError(t, validateUsageBillingOutboxEvent(decoded))
}

func TestBalanceSettlementPayloadV2ReleaseRoundTrip(t *testing.T) {
	publicID := "video_fedcba9876543210fedcba9876543210"
	settlement := &service.BalanceSettlementCommand{
		TaskID: 102,
		Action: service.BalanceSettlementRelease,
		Hold: service.BalanceHoldCommand{
			RequestID:  service.VideoTaskReleaseRequestID(publicID),
			APIKeyID:   8,
			UserID:     43,
			Scope:      service.BalanceHoldScopeVideoTask,
			RefID:      publicID,
			HoldAmount: 2,
		},
	}
	commandJSON, usageLogJSON, err := marshalBalanceSettlementOutboxPayload(settlement, nil)
	require.NoError(t, err)

	decoded, err := decodeUsageBillingOutboxEvent(
		100,
		0,
		time.Now().UTC(),
		settlement.Hold.RequestID,
		settlement.Hold.APIKeyID,
		settlement.Hold.RequestFingerprint,
		usageBillingOutboxPayloadVersionV2,
		usageBillingOutboxStageBilling,
		commandJSON,
		usageLogJSON,
		nil,
	)
	require.NoError(t, err)
	require.Nil(t, decoded.Command)
	require.Nil(t, decoded.UsageLog)
	require.Equal(t, service.BalanceSettlementRelease, decoded.BalanceSettlement.Action)
	require.NoError(t, validateUsageBillingOutboxEvent(decoded))
}

func TestBalanceSettlementPayloadV2RejectsOrdinaryBalanceCost(t *testing.T) {
	settlement, usageLog, _, _ := newVideoSettlementTestPayload(t)
	settlement.Billing.BalanceCost = settlement.Billing.ActualCost

	_, _, err := marshalBalanceSettlementOutboxPayload(settlement, usageLog)
	require.ErrorIs(t, err, service.ErrUsageBillingPayloadInvalid)
}

func TestBalanceSettlementPayloadV2RejectsTamperedIdentity(t *testing.T) {
	settlement, usageLog, commandJSON, usageLogJSON := newVideoSettlementTestPayload(t)
	var payload balanceSettlementPayloadV2
	require.NoError(t, json.Unmarshal(commandJSON, &payload))
	payload.SettlementRefID = "video_tampered"
	tampered, err := json.Marshal(payload)
	require.NoError(t, err)

	_, err = decodeUsageBillingOutboxEvent(
		99,
		0,
		usageLog.CreatedAt,
		settlement.Hold.RequestID,
		settlement.Hold.APIKeyID,
		settlement.Hold.RequestFingerprint,
		usageBillingOutboxPayloadVersionV2,
		usageBillingOutboxStageBilling,
		tampered,
		usageLogJSON,
		nil,
	)
	require.Error(t, err)
}
