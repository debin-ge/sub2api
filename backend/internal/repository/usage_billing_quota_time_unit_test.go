//go:build unit

package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoQuotaTimeOutboxV3RoundTripAndVersionFence(t *testing.T) {
	settlement, usage, _, _ := newVideoSettlementTestPayload(t)
	settlement.Billing.MediaType = "video"
	day := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	settlement.Billing.QuotaTime = &service.UsageBillingQuotaTime{Version: 1, TimeZone: "UTC", DayStart: day, WeekStart: day.AddDate(0, 0, -2)}
	commandJSON, usageJSON, err := marshalBalanceSettlementOutboxPayload(settlement, usage)
	require.NoError(t, err)
	decode := func(version int, payload []byte) (service.UsageBillingOutboxEvent, error) {
		return decodeUsageBillingOutboxEvent(99, 2, usage.CreatedAt.AddDate(0, 0, 40), settlement.Hold.RequestID,
			settlement.Hold.APIKeyID, settlement.Hold.RequestFingerprint, version, usageBillingOutboxStageBilling, payload, usageJSON, nil)
	}
	event, err := decode(usageBillingOutboxPayloadVersionV3, commandJSON)
	require.NoError(t, err)
	require.NoError(t, validateUsageBillingOutboxEvent(event))
	require.Equal(t, settlement.Billing.QuotaTime, event.Command.QuotaTime)
	require.True(t, settlement.Billing.OccurredAt.Equal(event.Command.OccurredAt))
	require.Equal(t, settlement.Billing.RequestFingerprint, event.Command.RequestFingerprint)
	require.Equal(t, usageBillingOutboxPayloadVersionV3, event.PayloadVersion)
	_, err = decode(usageBillingOutboxPayloadVersionV2, commandJSON)
	require.ErrorIs(t, err, service.ErrUsageBillingPayloadInvalid)
	for _, corruption := range []string{"missing_clock", "changed_time", "changed_calendar"} {
		t.Run(corruption, func(t *testing.T) {
			var payload balanceSettlementPayloadV2
			require.NoError(t, json.Unmarshal(commandJSON, &payload))
			switch corruption {
			case "missing_clock":
				payload.Billing.QuotaTime = nil
			case "changed_time":
				payload.Billing.OccurredAt = payload.Billing.OccurredAt.Add(time.Hour)
			case "changed_calendar":
				payload.Billing.QuotaTime.DayStart = day.Add(time.Hour)
			}
			corrupted, err := json.Marshal(payload)
			require.NoError(t, err)
			_, err = decode(usageBillingOutboxPayloadVersionV3, corrupted)
			require.ErrorIs(t, err, service.ErrUsageBillingPayloadInvalid)
		})
	}
	legacy, legacyUsage, legacyJSON, legacyUsageJSON := newVideoSettlementTestPayload(t)
	legacyEvent, err := decodeUsageBillingOutboxEvent(100, 0, legacyUsage.CreatedAt,
		legacy.Hold.RequestID, legacy.Hold.APIKeyID, legacy.Hold.RequestFingerprint,
		usageBillingOutboxPayloadVersionV2, usageBillingOutboxStageBilling, legacyJSON, legacyUsageJSON, nil)
	require.NoError(t, err)
	require.NoError(t, validateUsageBillingOutboxEvent(legacyEvent))
	require.Nil(t, legacyEvent.Command.QuotaTime)
	require.Equal(t, legacy.Billing.RequestFingerprint, legacyEvent.Command.RequestFingerprint)
}

func TestVideoQuotaTimeOutboxRejectsUsageTimestampDrift(t *testing.T) {
	settlement, usage, _, _ := newVideoSettlementTestPayload(t)
	day := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	settlement.Billing.MediaType = "video"
	settlement.Billing.QuotaTime = &service.UsageBillingQuotaTime{Version: 1, TimeZone: "UTC", DayStart: day, WeekStart: day.AddDate(0, 0, -2)}
	usage.CreatedAt = usage.CreatedAt.Add(time.Hour)
	_, _, err := marshalBalanceSettlementOutboxPayload(settlement, usage)
	require.ErrorIs(t, err, service.ErrUsageBillingPayloadInvalid)
}
