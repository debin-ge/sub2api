package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNotificationEmailOutboxLeaseCoversWorstCaseBatch(t *testing.T) {
	rounds := (notificationEmailOutboxBatchSize + notificationEmailOutboxConcurrency - 1) / notificationEmailOutboxConcurrency
	worstCase := time.Duration(rounds) * (notificationEmailOutboxSendTimeout + 3*time.Second)

	require.Greater(t, notificationEmailOutboxLease, worstCase)
}
