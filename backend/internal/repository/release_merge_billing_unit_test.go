//go:build unit

package repository

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMergedCacheStorageSurvivesDurablePayloadAndConflictChecks(t *testing.T) {
	cmd, log, _, _ := newUsageBillingOutboxTestPayload(t)
	log.CacheStorageTokenHours = 12345.25
	log.CacheStorageCost = 0.25
	log.InputCost = 0.75
	_, raw, err := marshalUsageBillingOutboxPayload(cmd, log)
	require.NoError(t, err)
	var payload usageLogPayloadV1
	require.NoError(t, json.Unmarshal(raw, &payload))
	restored := payload.usageLog()
	require.Equal(t, log.CacheStorageTokenHours, restored.CacheStorageTokenHours)
	require.Equal(t, log.CacheStorageCost, restored.CacheStorageCost)
	require.Equal(t, usageLogPayloadBillingComparable(payload), usageLogPayloadBillingComparable(usageLogToPayloadV1(restored)))
	payload.CacheStorageTokenHours++
	require.False(t, usageLogPayloadsBillingEquivalent(payload, usageLogToPayloadV1(restored)), "same monetary total cannot hide changed storage quantity")
}
