//go:build unit

package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoBillingReviewV4FingerprintAndLegacyCompatibility(t *testing.T) {
	settlement, usage, _, _ := newVideoSettlementTestPayload(t)
	legacy := settlement.Hold.RequestFingerprint
	settlement.Hold.BillingReviewID = 44
	commandJSON, usageJSON, err := marshalBalanceSettlementOutboxPayload(settlement, usage)
	require.NoError(t, err)
	require.NotEqual(t, legacy, settlement.Hold.RequestFingerprint)
	event, err := decodeUsageBillingOutboxEvent(9, 1, usage.CreatedAt, settlement.Hold.RequestID, settlement.Hold.APIKeyID,
		settlement.Hold.RequestFingerprint, usageBillingOutboxPayloadVersionV4, usageBillingOutboxStageBilling, commandJSON, usageJSON, nil)
	require.NoError(t, err)
	require.NoError(t, validateUsageBillingOutboxEvent(event))
	require.Equal(t, int64(44), event.BalanceSettlement.Hold.BillingReviewID)
	_, err = decodeUsageBillingOutboxEvent(9, 1, usage.CreatedAt, settlement.Hold.RequestID, settlement.Hold.APIKeyID,
		settlement.Hold.RequestFingerprint, usageBillingOutboxPayloadVersionV2, usageBillingOutboxStageBilling, commandJSON, usageJSON, nil)
	require.ErrorIs(t, err, service.ErrUsageBillingPayloadInvalid)
	settlement.Hold.BillingReviewID = 0
	_, _, err = marshalBalanceSettlementOutboxPayload(settlement, usage)
	require.NoError(t, err)
	require.Equal(t, legacy, settlement.Hold.RequestFingerprint)
}
