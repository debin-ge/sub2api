//go:build unit

package service

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestBuildMethodDistributionKeepsGooglePaySeparateFromStripe(t *testing.T) {
	methods := buildMethodDistribution([]*dbent.PaymentOrder{
		{PaymentType: payment.TypeStripe, PayAmount: 10},
		{PaymentType: payment.TypeGooglePay, PayAmount: 20},
		{PaymentType: payment.TypeGooglePay, PayAmount: 30},
	})

	byType := make(map[string]PaymentMethodStat, len(methods))
	for _, method := range methods {
		byType[method.Type] = method
	}
	require.Equal(t, PaymentMethodStat{Type: payment.TypeStripe, Amount: 10, Count: 1}, byType[payment.TypeStripe])
	require.Equal(t, PaymentMethodStat{Type: payment.TypeGooglePay, Amount: 50, Count: 2}, byType[payment.TypeGooglePay])
}

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
