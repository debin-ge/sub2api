//go:build unit

package service

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestWriteAuditLogIgnoresDuplicateOrderAction(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	svc := &PaymentService{entClient: client}

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	svc.writeAuditLog(ctx, 1, "PAYMENT_WISE_RECONCILE_NO_MATCH", payment.TypeWise, map[string]any{
		"reason": "no_matching_transaction",
	})
	svc.writeAuditLog(ctx, 1, "PAYMENT_WISE_RECONCILE_NO_MATCH", payment.TypeWise, map[string]any{
		"reason": "no_matching_transaction",
	})

	auditLogs, err := svc.GetOrderAuditLogs(ctx, 1)
	require.NoError(t, err)
	require.Len(t, auditLogs, 1)
	require.NotContains(t, logs.String(), "audit log failed")
}
